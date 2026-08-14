package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/llmclient"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	eggTranslationWorkerInterval = 2 * time.Second
	eggTranslationWorkerBatch    = 8
	eggTranslationLease          = 45 * time.Second
	eggTranslationErrorMaxLen    = 1024
	eggTranslationMaxAttempts    = 4
)

var (
	eggTranslationWorkerOnce sync.Once
	eggTranslationWakeCh     = make(chan struct{}, 1)

	eggI18nLLMTranslator        = translateEggI18nWithLLM
	eggVersionI18nLLMTranslator = translateEggVersionI18nWithLLM

	errEggTranslationJobSuperseded = errors.New("egg translation job superseded by newer source data")
)

func StartEggTranslationWorker(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	eggTranslationWorkerOnce.Do(func() {
		go runEggTranslationWorker(ctx)
	})
}

func scheduleEggDisplayTranslations(eggID string, version int, locale string) {
	if store.DB == nil {
		return
	}
	targetLocale, ok := normalizeEggLocaleToken(locale)
	if !ok || eggLocaleBase(targetLocale) == "en" {
		return
	}

	if err := enqueueEggI18nTranslationJob(eggID, targetLocale); err != nil && logger.L != nil {
		logger.L.Warnf("enqueue egg i18n translation failed egg=%s locale=%s err=%v", eggID, targetLocale, err)
	}
	if version > 0 {
		if err := enqueueEggVersionI18nTranslationJob(eggID, version, targetLocale); err != nil && logger.L != nil {
			logger.L.Warnf(
				"enqueue egg version translation failed egg=%s version=%d locale=%s err=%v",
				eggID,
				version,
				targetLocale,
				err,
			)
		}
	}
}

func runEggTranslationWorker(ctx context.Context) {
	ticker := time.NewTicker(eggTranslationWorkerInterval)
	defer ticker.Stop()

	for {
		if _, err := processEggTranslationDueBatch(eggTranslationWorkerBatch); err != nil && logger.L != nil {
			logger.L.Warnf("egg translation batch process failed: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-eggTranslationWakeCh:
		}
	}
}

func wakeEggTranslationWorker() {
	select {
	case eggTranslationWakeCh <- struct{}{}:
	default:
	}
}

func enqueueEggI18nTranslationJob(eggID, targetLocale string) error {
	source, err := loadEggI18nTranslationSource(eggID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if strings.EqualFold(source.Locale, targetLocale) {
		return nil
	}

	sourceUpdatedAt := normalizeEggTranslationTime(source.UpdatedAt)
	needsTranslation, err := eggI18nNeedsTranslation(eggID, targetLocale, sourceUpdatedAt)
	if err != nil || !needsTranslation {
		return err
	}
	return upsertEggTranslationJob(
		model.EggTranslationJobTypeEggI18n,
		eggID,
		0,
		source.Locale,
		sourceUpdatedAt,
		targetLocale,
	)
}

func enqueueEggVersionI18nTranslationJob(eggID string, version int, targetLocale string) error {
	if version <= 0 {
		return nil
	}
	source, err := loadEggVersionI18nTranslationSource(eggID, version)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if strings.EqualFold(source.Locale, targetLocale) {
		return nil
	}

	sourceUpdatedAt := normalizeEggTranslationTime(source.UpdatedAt)
	needsTranslation, err := eggVersionI18nNeedsTranslation(eggID, version, targetLocale, sourceUpdatedAt)
	if err != nil || !needsTranslation {
		return err
	}
	return upsertEggTranslationJob(
		model.EggTranslationJobTypeEggVersionI18n,
		eggID,
		version,
		source.Locale,
		sourceUpdatedAt,
		targetLocale,
	)
}

func upsertEggTranslationJob(
	jobType, eggID string,
	version int,
	sourceLocale string,
	sourceUpdatedAt time.Time,
	targetLocale string,
) error {
	if store.DB == nil {
		return errors.New("db unavailable")
	}
	now := time.Now().UTC()
	sourceUpdatedAt = normalizeEggTranslationTime(sourceUpdatedAt)
	job := model.EggTranslationJob{
		JobType:         strings.TrimSpace(jobType),
		EggID:           strings.TrimSpace(eggID),
		Version:         version,
		SourceLocale:    strings.TrimSpace(sourceLocale),
		SourceUpdatedAt: sourceUpdatedAt,
		TargetLocale:    strings.TrimSpace(targetLocale),
		Status:          model.EggTranslationJobStatusPending,
		AttemptCount:    0,
		MaxAttempts:     eggTranslationMaxAttempts,
		NextRetryAt:     now,
		LastError:       "",
	}

	shouldWake := false
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		var existing model.EggTranslationJob
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(
				"job_type = ? AND egg_id = ? AND version = ? AND target_locale = ?",
				job.JobType,
				job.EggID,
				job.Version,
				job.TargetLocale,
			).
			Take(&existing).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			createResult := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&job)
			if createResult.Error != nil {
				return createResult.Error
			}
			if createResult.RowsAffected > 0 {
				shouldWake = true
			}
			return nil
		case err != nil:
			return err
		}

		if !shouldRefreshEggTranslationJob(existing, sourceUpdatedAt) {
			return nil
		}

		shouldWake = true
		return tx.Model(&model.EggTranslationJob{}).
			Where("id = ?", existing.ID).
			Updates(map[string]any{
				"source_locale":     job.SourceLocale,
				"source_updated_at": job.SourceUpdatedAt,
				"status":            model.EggTranslationJobStatusPending,
				"attempt_count":     0,
				"max_attempts":      eggTranslationMaxAttempts,
				"next_retry_at":     now,
				"last_error":        "",
				"updated_at":        now,
			}).Error
	})
	if err != nil {
		return err
	}
	if shouldWake {
		wakeEggTranslationWorker()
	}
	return nil
}

func processEggTranslationDueBatch(limit int) (int, error) {
	if limit <= 0 {
		limit = 1
	}
	processed := 0
	for i := 0; i < limit; i++ {
		hasMore, err := processNextEggTranslationDue()
		if err != nil {
			return processed, err
		}
		if !hasMore {
			return processed, nil
		}
		processed++
	}
	return processed, nil
}

func processNextEggTranslationDue() (bool, error) {
	job, err := claimNextEggTranslationJob(time.Now().UTC())
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}
	if err := executeEggTranslationJob(context.Background(), job); err != nil {
		return true, err
	}
	return true, nil
}

func claimNextEggTranslationJob(now time.Time) (*model.EggTranslationJob, error) {
	if store.DB == nil {
		return nil, errors.New("db unavailable")
	}

	var claimed *model.EggTranslationJob
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		var job model.EggTranslationJob
		result := tx.Where(
			"status IN ? AND next_retry_at <= ?",
			[]int16{model.EggTranslationJobStatusPending, model.EggTranslationJobStatusRunning},
			now,
		).Order("id ASC").
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Limit(1).
			Find(&job)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}

		leaseUntil := now.Add(eggTranslationLease)
		res := tx.Model(&model.EggTranslationJob{}).
			Where(
				"id = ? AND status IN ? AND attempt_count = ?",
				job.ID,
				[]int16{model.EggTranslationJobStatusPending, model.EggTranslationJobStatusRunning},
				job.AttemptCount,
			).
			Updates(map[string]any{
				"status":        model.EggTranslationJobStatusRunning,
				"attempt_count": gorm.Expr("attempt_count + 1"),
				"next_retry_at": leaseUntil,
				"updated_at":    now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}

		job.AttemptCount += 1
		job.NextRetryAt = leaseUntil
		claimed = &job
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func executeEggTranslationJob(ctx context.Context, job *model.EggTranslationJob) error {
	if job == nil {
		return nil
	}

	var processErr error
	switch job.JobType {
	case model.EggTranslationJobTypeEggI18n:
		processErr = executeEggI18nTranslationJob(ctx, job)
	case model.EggTranslationJobTypeEggVersionI18n:
		processErr = executeEggVersionI18nTranslationJob(ctx, job)
	default:
		processErr = fmt.Errorf("unsupported egg translation job type %q", job.JobType)
	}

	if processErr == nil || errors.Is(processErr, errEggTranslationJobSuperseded) {
		return nil
	}
	return markEggTranslationJobFailed(job, processErr, time.Now().UTC())
}

func executeEggI18nTranslationJob(ctx context.Context, job *model.EggTranslationJob) error {
	source, err := loadEggI18nTranslationSource(job.EggID)
	if err != nil {
		return err
	}
	if strings.EqualFold(source.Locale, job.TargetLocale) {
		return markEggTranslationJobSucceeded(job, time.Now().UTC())
	}
	sourceUpdatedAt := normalizeEggTranslationTime(source.UpdatedAt)
	if !sameEggTranslationTime(sourceUpdatedAt, job.SourceUpdatedAt) {
		return errEggTranslationJobSuperseded
	}
	if needsTranslation, err := eggI18nNeedsTranslation(job.EggID, job.TargetLocale, sourceUpdatedAt); err != nil {
		return err
	} else if !needsTranslation {
		return markEggTranslationJobSucceeded(job, time.Now().UTC())
	}

	input, err := eggI18nLLMTranslator(ctx, source, job.TargetLocale)
	if err != nil {
		return err
	}
	input.Locale = job.TargetLocale
	return saveEggI18nTranslationResult(job, input)
}

func executeEggVersionI18nTranslationJob(ctx context.Context, job *model.EggTranslationJob) error {
	source, err := loadEggVersionI18nTranslationSource(job.EggID, job.Version)
	if err != nil {
		return err
	}
	if strings.EqualFold(source.Locale, job.TargetLocale) {
		return markEggTranslationJobSucceeded(job, time.Now().UTC())
	}
	sourceUpdatedAt := normalizeEggTranslationTime(source.UpdatedAt)
	if !sameEggTranslationTime(sourceUpdatedAt, job.SourceUpdatedAt) {
		return errEggTranslationJobSuperseded
	}
	if needsTranslation, err := eggVersionI18nNeedsTranslation(job.EggID, job.Version, job.TargetLocale, sourceUpdatedAt); err != nil {
		return err
	} else if !needsTranslation {
		return markEggTranslationJobSucceeded(job, time.Now().UTC())
	}

	input, err := eggVersionI18nLLMTranslator(ctx, source, job.TargetLocale)
	if err != nil {
		return err
	}
	input.Locale = job.TargetLocale
	return saveEggVersionTranslationResult(job, input)
}

func loadEggI18nTranslationSource(eggID string) (model.EggI18n, error) {
	var rows []model.EggI18n
	if err := store.DB.Where("egg_id = ?", strings.TrimSpace(eggID)).Find(&rows).Error; err != nil {
		return model.EggI18n{}, err
	}
	if len(rows) == 0 {
		return model.EggI18n{}, gorm.ErrRecordNotFound
	}
	source, _ := pickBestEggI18n([]string{"en-US", "en"}, rows)
	return source, nil
}

func loadEggVersionI18nTranslationSource(eggID string, version int) (model.EggVersionI18n, error) {
	var rows []model.EggVersionI18n
	if err := store.DB.Where("egg_id = ? AND version = ?", strings.TrimSpace(eggID), version).Find(&rows).Error; err != nil {
		return model.EggVersionI18n{}, err
	}
	if len(rows) == 0 {
		return model.EggVersionI18n{}, gorm.ErrRecordNotFound
	}
	source, _ := pickBestEggVersionI18n([]string{"en-US", "en"}, rows)
	return source, nil
}

func eggI18nNeedsTranslation(eggID, targetLocale string, sourceUpdatedAt time.Time) (bool, error) {
	var target model.EggI18n
	err := store.DB.Where("egg_id = ? AND locale = ?", strings.TrimSpace(eggID), strings.TrimSpace(targetLocale)).Take(&target).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return eggTranslationTargetStale(target.UpdatedAt, sourceUpdatedAt), nil
}

func eggVersionI18nNeedsTranslation(eggID string, version int, targetLocale string, sourceUpdatedAt time.Time) (bool, error) {
	var target model.EggVersionI18n
	err := store.DB.Where(
		"egg_id = ? AND version = ? AND locale = ?",
		strings.TrimSpace(eggID),
		version,
		strings.TrimSpace(targetLocale),
	).Take(&target).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return eggTranslationTargetStale(target.UpdatedAt, sourceUpdatedAt), nil
}

func markEggTranslationJobSucceeded(job *model.EggTranslationJob, now time.Time) error {
	if job == nil {
		return nil
	}
	return store.DB.Model(&model.EggTranslationJob{}).
		Where("id = ? AND status = ? AND source_updated_at = ?", job.ID, model.EggTranslationJobStatusRunning, normalizeEggTranslationTime(job.SourceUpdatedAt)).
		Updates(map[string]any{
			"status":        model.EggTranslationJobStatusDone,
			"next_retry_at": now,
			"last_error":    "",
			"updated_at":    now,
		}).Error
}

func markEggTranslationJobFailed(job *model.EggTranslationJob, processErr error, now time.Time) error {
	if job == nil {
		return processErr
	}

	status := model.EggTranslationJobStatusPending
	nextRetryAt := now.Add(eggTranslationRetryDelay(job.AttemptCount))
	maxAttempts := job.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = eggTranslationMaxAttempts
	}
	if job.AttemptCount >= maxAttempts {
		status = model.EggTranslationJobStatusFailed
		nextRetryAt = now
	}

	lastError := truncateEggTranslationError(processErr)
	updateErr := store.DB.Model(&model.EggTranslationJob{}).
		Where(
			"id = ? AND status = ? AND source_updated_at = ?",
			job.ID,
			model.EggTranslationJobStatusRunning,
			normalizeEggTranslationTime(job.SourceUpdatedAt),
		).
		Updates(map[string]any{
			"status":        status,
			"next_retry_at": nextRetryAt,
			"last_error":    lastError,
			"updated_at":    now,
		}).Error
	if updateErr != nil {
		return updateErr
	}
	return processErr
}

func saveEggI18nTranslationResult(job *model.EggTranslationJob, input EggI18nInput) error {
	if job == nil {
		return nil
	}
	now := time.Now().UTC()
	return store.DB.Transaction(func(tx *gorm.DB) error {
		current, err := lockCurrentEggTranslationJob(tx, job.ID)
		if err != nil {
			return err
		}
		if !jobCanPersistTranslation(current, job.SourceUpdatedAt) {
			return errEggTranslationJobSuperseded
		}
		if err := upsertEggI18nTx(tx, job.EggID, []EggI18nInput{input}); err != nil {
			return err
		}
		return markEggTranslationJobSucceededTx(tx, job.ID, job.SourceUpdatedAt, now)
	})
}

func saveEggVersionTranslationResult(job *model.EggTranslationJob, input EggVersionI18nInput) error {
	if job == nil {
		return nil
	}
	now := time.Now().UTC()
	return store.DB.Transaction(func(tx *gorm.DB) error {
		current, err := lockCurrentEggTranslationJob(tx, job.ID)
		if err != nil {
			return err
		}
		if !jobCanPersistTranslation(current, job.SourceUpdatedAt) {
			return errEggTranslationJobSuperseded
		}
		if err := upsertEggVersionI18nTx(tx, job.EggID, job.Version, []EggVersionI18nInput{input}); err != nil {
			return err
		}
		return markEggTranslationJobSucceededTx(tx, job.ID, job.SourceUpdatedAt, now)
	})
}

func lockCurrentEggTranslationJob(tx *gorm.DB, jobID int64) (*model.EggTranslationJob, error) {
	if tx == nil {
		return nil, errors.New("transaction is nil")
	}
	var current model.EggTranslationJob
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Take(&current, "id = ?", jobID).Error; err != nil {
		return nil, err
	}
	return &current, nil
}

func jobCanPersistTranslation(job *model.EggTranslationJob, sourceUpdatedAt time.Time) bool {
	if job == nil {
		return false
	}
	if job.Status != model.EggTranslationJobStatusRunning {
		return false
	}
	return sameEggTranslationTime(job.SourceUpdatedAt, sourceUpdatedAt)
}

func markEggTranslationJobSucceededTx(tx *gorm.DB, jobID int64, sourceUpdatedAt time.Time, now time.Time) error {
	if tx == nil {
		return errors.New("transaction is nil")
	}
	return tx.Model(&model.EggTranslationJob{}).
		Where(
			"id = ? AND status = ? AND source_updated_at = ?",
			jobID,
			model.EggTranslationJobStatusRunning,
			normalizeEggTranslationTime(sourceUpdatedAt),
		).
		Updates(map[string]any{
			"status":        model.EggTranslationJobStatusDone,
			"next_retry_at": now,
			"last_error":    "",
			"updated_at":    now,
		}).Error
}

func shouldRefreshEggTranslationJob(existing model.EggTranslationJob, sourceUpdatedAt time.Time) bool {
	sourceUpdatedAt = normalizeEggTranslationTime(sourceUpdatedAt)
	if eggTranslationTargetStale(existing.SourceUpdatedAt, sourceUpdatedAt) {
		return true
	}
	return existing.Status == model.EggTranslationJobStatusDone
}

func eggTranslationTargetStale(targetUpdatedAt, sourceUpdatedAt time.Time) bool {
	targetUpdatedAt = normalizeEggTranslationTime(targetUpdatedAt)
	sourceUpdatedAt = normalizeEggTranslationTime(sourceUpdatedAt)
	if sourceUpdatedAt.IsZero() {
		return false
	}
	if targetUpdatedAt.IsZero() {
		return true
	}
	return targetUpdatedAt.Before(sourceUpdatedAt)
}

func normalizeEggTranslationTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC().Round(0)
}

func sameEggTranslationTime(a, b time.Time) bool {
	a = normalizeEggTranslationTime(a)
	b = normalizeEggTranslationTime(b)
	return a.Equal(b)
}

func eggTranslationRetryDelay(attemptCount int32) time.Duration {
	switch {
	case attemptCount <= 1:
		return 3 * time.Second
	case attemptCount == 2:
		return 10 * time.Second
	case attemptCount == 3:
		return 30 * time.Second
	default:
		return 2 * time.Minute
	}
}

func truncateEggTranslationError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if len(text) <= eggTranslationErrorMaxLen {
		return text
	}
	return text[:eggTranslationErrorMaxLen]
}

func translateEggI18nWithLLM(ctx context.Context, source model.EggI18n, targetLocale string) (EggI18nInput, error) {
	if strings.TrimSpace(source.Name) == "" {
		return EggI18nInput{}, errors.New("source egg name is empty")
	}
	client, request, err := buildEggTranslationJSONRequest(
		"egg_i18n_translation",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"name", "description", "vibe"},
			"properties": map[string]any{
				"name":        map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"vibe":        map[string]any{"type": "string"},
			},
		},
		buildEggI18nTranslationInstructions(source.Locale, targetLocale),
		mustJSON(map[string]string{
			"name":        source.Name,
			"description": source.Description,
			"vibe":        source.Vibe,
		}),
	)
	if err != nil {
		return EggI18nInput{}, err
	}

	var resp struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Vibe        string `json:"vibe"`
	}
	if _, err := client.GenerateJSON(ctx, request, &resp); err != nil {
		return EggI18nInput{}, err
	}
	return EggI18nInput{
		Name:        strings.TrimSpace(resp.Name),
		Description: strings.TrimSpace(resp.Description),
		Vibe:        strings.TrimSpace(resp.Vibe),
	}, nil
}

func translateEggVersionI18nWithLLM(ctx context.Context, source model.EggVersionI18n, targetLocale string) (EggVersionI18nInput, error) {
	if strings.TrimSpace(source.VersionDesc) == "" {
		return EggVersionI18nInput{VersionDesc: ""}, nil
	}
	client, request, err := buildEggTranslationJSONRequest(
		"egg_version_i18n_translation",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"version_desc"},
			"properties": map[string]any{
				"version_desc": map[string]any{"type": "string"},
			},
		},
		buildEggVersionTranslationInstructions(source.Locale, targetLocale),
		mustJSON(map[string]string{
			"version_desc": source.VersionDesc,
		}),
	)
	if err != nil {
		return EggVersionI18nInput{}, err
	}

	var resp struct {
		VersionDesc string `json:"version_desc"`
	}
	if _, err := client.GenerateJSON(ctx, request, &resp); err != nil {
		return EggVersionI18nInput{}, err
	}
	return EggVersionI18nInput{VersionDesc: strings.TrimSpace(resp.VersionDesc)}, nil
}

func buildEggTranslationJSONRequest(
	schemaName string,
	schema map[string]any,
	instructions string,
	input string,
) (*llmclient.Client, llmclient.JSONRequest, error) {
	client, err := llmclient.New(llmclient.Config{
		APIKey:           strings.TrimSpace(config.C.LLM.Translation.APIKey),
		BaseURL:          strings.TrimSpace(config.C.LLM.Translation.BaseURL),
		ProxyURL:         strings.TrimSpace(config.C.LLM.Translation.ProxyURL),
		APIStyle:         strings.TrimSpace(config.C.LLM.Translation.APIStyle),
		DefaultModel:     strings.TrimSpace(config.C.LLM.Translation.Model),
		ReasoningEffort:  strings.TrimSpace(config.C.LLM.Translation.ReasoningEffort),
		RequestTimeout:   time.Duration(config.C.LLM.Translation.RequestTimeoutSec) * time.Second,
		ExtraBodyJSON:    strings.TrimSpace(config.C.LLM.Translation.ExtraBodyJSON),
		ExtraHeadersJSON: strings.TrimSpace(config.C.LLM.Translation.ExtraHeadersJSON),
	})
	if err != nil {
		return nil, llmclient.JSONRequest{}, err
	}
	temperature := config.C.LLM.Translation.Temperature
	request := llmclient.JSONRequest{
		Instructions:    instructions,
		Input:           input,
		SchemaName:      schemaName,
		Schema:          schema,
		Temperature:     &temperature,
		MaxOutputTokens: int64(config.C.LLM.Translation.MaxOutputTokens),
	}
	return client, request, nil
}

func buildEggI18nTranslationInstructions(sourceLocale, targetLocale string) string {
	return fmt.Sprintf(
		"Translate egg marketplace metadata from %s to %s. Return only JSON that matches the schema. Translate every user-facing field naturally for storefront display. Translate the name field too, and do not copy it verbatim unless the whole value is only a code identifier, package name, URL, or established brand token. If the name mixes a brand token with ordinary words, keep the brand token and translate the ordinary words. Preserve code identifiers, URLs, and package names unchanged. Do not add explanations or markdown.",
		strings.TrimSpace(sourceLocale),
		strings.TrimSpace(targetLocale),
	)
}

func buildEggVersionTranslationInstructions(sourceLocale, targetLocale string) string {
	return fmt.Sprintf(
		"Translate egg version release notes from %s to %s. Return only JSON that matches the schema. Keep product names, code identifiers, URLs, and package names unchanged. Do not add explanations or markdown.",
		strings.TrimSpace(sourceLocale),
		strings.TrimSpace(targetLocale),
	)
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

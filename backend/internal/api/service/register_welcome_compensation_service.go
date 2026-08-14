package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	registerWelcomeCompensationWorkerInterval = 2 * time.Second
	registerWelcomeCompensationWorkerBatch    = 32
	registerWelcomeCompensationLease          = 20 * time.Second
	registerWelcomeCompensationErrorMaxLen    = 1024
)

var (
	registerWelcomeCompensationBackoffs = []time.Duration{
		0,
		800 * time.Millisecond,
		2 * time.Second,
		5 * time.Second,
		10 * time.Second,
	}

	registerWelcomeCompensationRunner = runRegisterWelcomeCompensationAsync
	registerWelcomeWorkerOnce         sync.Once
)

func StartRegisterWelcomeCompensationWorker(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	registerWelcomeWorkerOnce.Do(func() {
		go runRegisterWelcomeCompensationWorker(ctx)
	})
}

func runRegisterWelcomeCompensationWorker(ctx context.Context) {
	ticker := time.NewTicker(registerWelcomeCompensationWorkerInterval)
	defer ticker.Stop()

	for {
		if _, err := processRegisterWelcomeCompensationDueBatch(registerWelcomeCompensationWorkerBatch); err != nil {
			logger.L.Warnf("register welcome compensation batch process failed: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func scheduleRegisterWelcomeCompensation(registerUserID, customerUserID int64) {
	if registerUserID <= 0 || customerUserID <= 0 || registerUserID == customerUserID {
		return
	}
	if err := enqueueRegisterWelcomeCompensation(registerUserID, customerUserID); err != nil {
		logger.L.Warnf(
			"register welcome compensation enqueue failed register_user=%d customer_user=%d: %v",
			registerUserID,
			customerUserID,
			err,
		)
		return
	}
	if registerWelcomeCompensationRunner != nil {
		registerWelcomeCompensationRunner(registerUserID, customerUserID)
	}
}

func enqueueRegisterWelcomeCompensation(registerUserID, customerUserID int64) error {
	if registerUserID <= 0 || customerUserID <= 0 || registerUserID == customerUserID {
		return errors.New("invalid register welcome compensation target")
	}
	if store.DB == nil {
		return errors.New("db unavailable")
	}

	now := time.Now().UTC()
	job := model.RegisterWelcomeCompensation{
		RegisterUserID: registerUserID,
		CustomerUserID: customerUserID,
		Status:         model.RegisterWelcomeCompensationStatusPending,
		AttemptCount:   0,
		MaxAttempts:    int32(registerWelcomeCompensateMaxAttempts),
		NextRetryAt:    now,
		LastError:      "",
	}
	return store.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "register_user_id"},
			{Name: "customer_user_id"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"status":        model.RegisterWelcomeCompensationStatusPending,
			"attempt_count": 0,
			"max_attempts":  int32(registerWelcomeCompensateMaxAttempts),
			"next_retry_at": now,
			"last_error":    "",
			"updated_at":    now,
		}),
	}).Create(&job).Error
}

func runRegisterWelcomeCompensationAsync(registerUserID, customerUserID int64) {
	if registerUserID <= 0 || customerUserID <= 0 || registerUserID == customerUserID {
		return
	}

	go func() {
		for i := 0; i < registerWelcomeCompensateMaxAttempts; i++ {
			if i < len(registerWelcomeCompensationBackoffs) && registerWelcomeCompensationBackoffs[i] > 0 {
				time.Sleep(registerWelcomeCompensationBackoffs[i])
			}

			processed, err := processRegisterWelcomeCompensationByTarget(registerUserID, customerUserID)
			if err != nil {
				logger.L.Warnf(
					"register welcome compensation immediate process failed attempt=%d register_user=%d customer_user=%d: %v",
					i+1,
					registerUserID,
					customerUserID,
					err,
				)
				continue
			}
			if processed {
				return
			}
			return
		}
	}()
}

func processRegisterWelcomeCompensationDueBatch(limit int) (int, error) {
	if limit <= 0 {
		limit = 1
	}

	processed := 0
	for i := 0; i < limit; i++ {
		hasMore, err := processNextRegisterWelcomeCompensationDue()
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

func processNextRegisterWelcomeCompensationDue() (bool, error) {
	job, err := claimNextRegisterWelcomeCompensation(time.Now().UTC())
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}
	if err := executeRegisterWelcomeCompensation(job); err != nil {
		return true, err
	}
	return true, nil
}

func processRegisterWelcomeCompensationByTarget(registerUserID, customerUserID int64) (bool, error) {
	job, err := claimRegisterWelcomeCompensationByTarget(registerUserID, customerUserID, time.Now().UTC())
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}
	if err := executeRegisterWelcomeCompensation(job); err != nil {
		return true, err
	}
	return true, nil
}

func claimNextRegisterWelcomeCompensation(now time.Time) (*model.RegisterWelcomeCompensation, error) {
	return claimRegisterWelcomeCompensation(now, func(tx *gorm.DB, current time.Time) *gorm.DB {
		return tx.Where("status = ? AND next_retry_at <= ?", model.RegisterWelcomeCompensationStatusPending, current).
			Order("id ASC")
	})
}

func claimRegisterWelcomeCompensationByTarget(registerUserID, customerUserID int64, now time.Time) (*model.RegisterWelcomeCompensation, error) {
	if registerUserID <= 0 || customerUserID <= 0 || registerUserID == customerUserID {
		return nil, nil
	}
	return claimRegisterWelcomeCompensation(now, func(tx *gorm.DB, current time.Time) *gorm.DB {
		return tx.Where(
			"register_user_id = ? AND customer_user_id = ? AND status = ? AND next_retry_at <= ?",
			registerUserID,
			customerUserID,
			model.RegisterWelcomeCompensationStatusPending,
			current,
		)
	})
}

func claimRegisterWelcomeCompensation(
	now time.Time,
	buildQuery func(tx *gorm.DB, now time.Time) *gorm.DB,
) (*model.RegisterWelcomeCompensation, error) {
	if store.DB == nil {
		return nil, errors.New("db unavailable")
	}

	var claimed *model.RegisterWelcomeCompensation
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		var job model.RegisterWelcomeCompensation
		query := buildQuery(tx, now).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		result := query.Limit(1).Find(&job)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}

		leaseUntil := now.Add(registerWelcomeCompensationLease)
		res := tx.Model(&model.RegisterWelcomeCompensation{}).
			Where(
				"id = ? AND status = ? AND attempt_count = ?",
				job.ID,
				model.RegisterWelcomeCompensationStatusPending,
				job.AttemptCount,
			).
			Updates(map[string]any{
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

func executeRegisterWelcomeCompensation(job *model.RegisterWelcomeCompensation) error {
	if job == nil {
		return nil
	}

	now := time.Now().UTC()
	processErr := compensateRegisterWelcome(job.RegisterUserID, job.CustomerUserID)
	if processErr != nil {
		if err := markRegisterWelcomeCompensationFailed(job, processErr, now); err != nil {
			return err
		}
		return processErr
	}
	return markRegisterWelcomeCompensationSucceeded(job.ID, now)
}

func markRegisterWelcomeCompensationSucceeded(jobID int64, now time.Time) error {
	if store.DB == nil {
		return errors.New("db unavailable")
	}
	return store.DB.Model(&model.RegisterWelcomeCompensation{}).
		Where("id = ?", jobID).
		Updates(map[string]any{
			"status":     model.RegisterWelcomeCompensationStatusDone,
			"last_error": "",
			"updated_at": now,
		}).Error
}

func markRegisterWelcomeCompensationFailed(job *model.RegisterWelcomeCompensation, processErr error, now time.Time) error {
	if job == nil {
		return nil
	}
	if store.DB == nil {
		return errors.New("db unavailable")
	}

	maxAttempts := job.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = int32(registerWelcomeCompensateMaxAttempts)
	}

	status := model.RegisterWelcomeCompensationStatusPending
	nextRetryAt := now.Add(registerWelcomeCompensationRetryDelay(job.AttemptCount))
	if job.AttemptCount >= maxAttempts {
		status = model.RegisterWelcomeCompensationStatusFailed
		nextRetryAt = now
	}

	return store.DB.Model(&model.RegisterWelcomeCompensation{}).
		Where("id = ?", job.ID).
		Updates(map[string]any{
			"status":        status,
			"next_retry_at": nextRetryAt,
			"last_error":    truncateRegisterWelcomeCompensationError(processErr),
			"updated_at":    now,
		}).Error
}

func registerWelcomeCompensationRetryDelay(attemptCount int32) time.Duration {
	if attemptCount <= 0 {
		return 0
	}
	idx := int(attemptCount)
	if idx >= len(registerWelcomeCompensationBackoffs) {
		idx = len(registerWelcomeCompensationBackoffs) - 1
	}
	if idx < 0 {
		idx = 0
	}
	return registerWelcomeCompensationBackoffs[idx]
}

func truncateRegisterWelcomeCompensationError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if len(msg) <= registerWelcomeCompensationErrorMaxLen {
		return msg
	}
	return msg[:registerWelcomeCompensationErrorMaxLen]
}

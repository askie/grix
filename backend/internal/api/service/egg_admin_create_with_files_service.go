package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
	"github.com/minio/minio-go/v7"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	adminEggZipMaxUploadBytes    = 50 * 1024 * 1024
	adminEggFallbackCategoryID   = "uncategorized"
	adminEggFallbackCategoryCode = "uncategorized"
	adminEggFallbackCategorySort = 9999
)

type adminEggCreateWithFilesMetaNormalized struct {
	ID               string
	CategoryID       string
	Color            string
	Emoji            string
	PublishNow       bool
	EggI18n          []EggI18nInput
	VersionI18n      []EggVersionI18nInput
	ArtifactManifest json.RawMessage
}

type adminEggPreparedArtifact struct {
	Role      string
	LocalPath string
	ObjectKey string
	URL       string
	SHA256    string
	Size      int64
}

type adminEggUploadFunc func(objectKey, localPath string, size int64) (string, error)
type adminEggDeleteFunc func(objectKey string) error

var (
	adminEggArtifactUploader adminEggUploadFunc = uploadAdminEggArtifact
	adminEggArtifactDeleter  adminEggDeleteFunc = deleteAdminEggArtifact
)

func AdminEggCreateWithFiles(req AdminEggCreateWithFilesReq) (*AdminEggCreateWithFilesResp, *errcode.ErrCode) {
	meta, ec := normalizeAdminEggCreateWithFilesMeta(req.Meta)
	if ec != nil {
		return nil, ec
	}
	if ec := validateAdminEggCreateWithFilesCategory(meta.CategoryID, meta.PublishNow); ec != nil {
		return nil, ec
	}

	personaArtifact, ec := prepareAdminEggArtifact("persona", req.PersonaZipFile)
	if ec != nil {
		return nil, ec
	}
	skillArtifact, ec := prepareAdminEggArtifact("skill", req.SkillZipFile)
	if ec != nil {
		if personaArtifact != nil {
			cleanupAdminEggArtifacts(personaArtifact)
		}
		return nil, ec
	}
	if personaArtifact == nil && skillArtifact == nil {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "至少需要上传 persona_zip 或 skill_zip"}
	}

	resp, ec := saveAdminEggCreateWithFiles(meta, personaArtifact, skillArtifact)
	if ec != nil {
		cleanupAdminEggArtifacts(personaArtifact, skillArtifact)
		return nil, ec
	}
	releaseAdminEggArtifacts(personaArtifact, skillArtifact)
	return resp, nil
}

func normalizeAdminEggCreateWithFilesMeta(meta AdminEggCreateWithFilesMeta) (*adminEggCreateWithFilesMetaNormalized, *errcode.ErrCode) {
	eggID := strings.TrimSpace(meta.ID)
	if eggID == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "egg id 不能为空"}
	}

	normalizedEggI18n, ec := normalizeEggI18nInputs(meta.EggI18n)
	if ec != nil {
		return nil, ec
	}
	if ec := validateAdminEggCreateWithFilesMeta(normalizedEggI18n); ec != nil {
		return nil, ec
	}
	if len(meta.VersionI18n) == 0 {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "版本 i18n 不能为空"}
	}
	normalizedVersionI18n, ec := normalizeVersionI18nInputs(meta.VersionI18n)
	if ec != nil {
		return nil, ec
	}

	color := strings.TrimSpace(meta.Color)
	if color == "" {
		color = "#D97706"
	}
	emoji := strings.TrimSpace(meta.Emoji)
	if emoji == "" {
		emoji = "🌍"
	}

	publishNow := true
	if meta.PublishNow != nil {
		publishNow = *meta.PublishNow
	}

	return &adminEggCreateWithFilesMetaNormalized{
		ID:               eggID,
		CategoryID:       strings.TrimSpace(meta.CategoryID),
		Color:            color,
		Emoji:            emoji,
		PublishNow:       publishNow,
		EggI18n:          normalizedEggI18n,
		VersionI18n:      normalizedVersionI18n,
		ArtifactManifest: normalizeAdminEggArtifactManifest(meta.ArtifactManifest),
	}, nil
}

func normalizeAdminEggArtifactManifest(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage(`{}`)
	}
	return raw
}

func validateAdminEggCreateWithFilesMeta(i18n []EggI18nInput) *errcode.ErrCode {
	if len(i18n) == 0 {
		return &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "egg i18n 不能为空"}
	}
	for _, item := range i18n {
		if strings.TrimSpace(item.Locale) == "" || strings.TrimSpace(item.Name) == "" {
			return &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "egg i18n 需要 locale 和 name"}
		}
	}
	return nil
}

func validateAdminEggCreateWithFilesCategory(categoryID string, publishNow bool) *errcode.ErrCode {
	if strings.TrimSpace(categoryID) == "" {
		return nil
	}
	var category model.EggCategory
	if err := store.DB.First(&category, "id = ?", strings.TrimSpace(categoryID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "分类不存在"}
		}
		return &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询分类失败"}
	}
	if publishNow && category.Status != model.EggCategoryStatusActive {
		return &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "分类停用时不可发布"}
	}
	return nil
}

func prepareAdminEggArtifact(role string, fileHeader *multipart.FileHeader) (*adminEggPreparedArtifact, *errcode.ErrCode) {
	if fileHeader == nil {
		return nil, nil
	}
	if fileHeader.Size <= 0 {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: role + "_zip 文件不能为空"}
	}
	if fileHeader.Size > adminEggZipMaxUploadBytes {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: role + "_zip 文件过大"}
	}
	if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(fileHeader.Filename)), ".zip") {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: role + "_zip 仅支持 .zip 文件"}
	}

	src, err := fileHeader.Open()
	if err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "读取上传文件失败"}
	}
	defer src.Close()

	tmpFile, err := os.CreateTemp("", fmt.Sprintf("admin-egg-%s-*.zip", role))
	if err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "创建临时文件失败"}
	}
	tmpPath := tmpFile.Name()

	hasher := sha256.New()
	written, err := io.Copy(tmpFile, io.TeeReader(io.LimitReader(src, adminEggZipMaxUploadBytes+1), hasher))
	if err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "保存上传文件失败"}
	}
	if written <= 0 {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: role + "_zip 文件不能为空"}
	}
	if written > adminEggZipMaxUploadBytes {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: role + "_zip 文件过大"}
	}
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "校验上传文件失败"}
	}
	if !isZipMagic(tmpFile) {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: role + "_zip 文件格式错误"}
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "保存上传文件失败"}
	}

	return &adminEggPreparedArtifact{
		Role:      role,
		LocalPath: tmpPath,
		SHA256:    hex.EncodeToString(hasher.Sum(nil)),
		Size:      written,
	}, nil
}

func isZipMagic(reader io.ReadSeeker) bool {
	if reader == nil {
		return false
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return false
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return false
	}
	_, _ = reader.Seek(0, io.SeekStart)
	return string(header) == "PK\x03\x04" || string(header) == "PK\x05\x06" || string(header) == "PK\x07\x08"
}

func uploadAdminEggArtifact(objectKey, localPath string, size int64) (string, error) {
	if err := ensureOSSReady(); err != nil {
		return "", err
	}
	file, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, err = getOSSClient(ossStorageMedia).PutObject(
		context.Background(),
		getOSSConfig(ossStorageMedia).Bucket,
		objectKey,
		file,
		size,
		minio.PutObjectOptions{ContentType: "application/zip"},
	)
	if err != nil {
		return "", err
	}
	return BuildMediaAccessURL(objectKey), nil
}

func deleteAdminEggArtifact(objectKey string) error {
	if err := ensureOSSReady(); err != nil {
		return err
	}
	return getOSSClient(ossStorageMedia).RemoveObject(
		context.Background(),
		getOSSConfig(ossStorageMedia).Bucket,
		objectKey,
		minio.RemoveObjectOptions{},
	)
}

func uploadPreparedAdminEggArtifact(eggID string, version int, artifact *adminEggPreparedArtifact) error {
	if artifact == nil {
		return nil
	}
	if strings.TrimSpace(artifact.LocalPath) == "" {
		return errors.New("artifact local path required")
	}

	objectKey := buildEggVersionArtifactObjectKey(eggID, version, artifact.Role)
	accessURL, err := adminEggArtifactUploader(objectKey, artifact.LocalPath, artifact.Size)
	_ = os.Remove(artifact.LocalPath)
	artifact.LocalPath = ""
	if err != nil {
		return err
	}

	artifact.ObjectKey = objectKey
	artifact.URL = accessURL
	return nil
}

func releaseAdminEggArtifacts(artifacts ...*adminEggPreparedArtifact) {
	for _, artifact := range artifacts {
		if artifact == nil || strings.TrimSpace(artifact.LocalPath) == "" {
			continue
		}
		_ = os.Remove(artifact.LocalPath)
		artifact.LocalPath = ""
	}
}

func cleanupAdminEggArtifacts(artifacts ...*adminEggPreparedArtifact) {
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		if strings.TrimSpace(artifact.LocalPath) != "" {
			_ = os.Remove(artifact.LocalPath)
			artifact.LocalPath = ""
		}
		if strings.TrimSpace(artifact.ObjectKey) == "" {
			continue
		}
		if err := adminEggArtifactDeleter(artifact.ObjectKey); err != nil {
			log.Printf("cleanup admin egg artifact failed: object_key=%s err=%v", artifact.ObjectKey, err)
		}
	}
}

func saveAdminEggCreateWithFiles(
	meta *adminEggCreateWithFilesMetaNormalized,
	personaArtifact *adminEggPreparedArtifact,
	skillArtifact *adminEggPreparedArtifact,
) (*AdminEggCreateWithFilesResp, *errcode.ErrCode) {
	if meta == nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "发布参数无效"}
	}

	var (
		version     int
		status      string
		created     bool
		persistedAt = time.Now()
	)

	err := store.DB.Transaction(func(tx *gorm.DB) error {
		currentStatus, wasCreated, err := upsertAdminEggBaseTx(tx, meta, personaArtifact != nil, skillArtifact != nil)
		if err != nil {
			return err
		}
		created = wasCreated
		version, err = nextAdminEggVersionTx(tx, meta.ID)
		if err != nil {
			return err
		}
		if err := uploadPreparedAdminEggArtifact(meta.ID, version, personaArtifact); err != nil {
			return err
		}
		if err := uploadPreparedAdminEggArtifact(meta.ID, version, skillArtifact); err != nil {
			return err
		}
		if err := createAdminEggVersionTx(tx, meta, version, persistedAt, personaArtifact, skillArtifact); err != nil {
			return err
		}

		status = currentStatus
		if meta.PublishNow {
			status = model.EggStatusPublished
			result := tx.Model(&model.Egg{}).
				Where("id = ?", meta.ID).
				Updates(map[string]any{
					"status":     status,
					"updated_at": persistedAt,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("saveAdminEggCreateWithFiles failed: egg_id=%s err=%v", meta.ID, err)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "egg 不存在"}
		}
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, &errcode.ErrCode{HTTPStatus: 409, BizCode: 10005, Msg: "egg 发布冲突，请重试"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "保存 egg 失败"}
	}

	resp := &AdminEggCreateWithFilesResp{
		EggID:   meta.ID,
		Version: version,
		Status:  status,
		Created: created,
	}
	if personaArtifact != nil {
		resp.PersonaZipURL = personaArtifact.URL
		resp.PersonaZipSHA256 = personaArtifact.SHA256
		resp.PersonaZipSize = personaArtifact.Size
	}
	if skillArtifact != nil {
		resp.SkillZipURL = skillArtifact.URL
		resp.SkillZipSHA256 = skillArtifact.SHA256
		resp.SkillZipSize = skillArtifact.Size
	}
	return resp, nil
}

func upsertAdminEggBaseTx(
	tx *gorm.DB,
	meta *adminEggCreateWithFilesMetaNormalized,
	hasPersonaZip bool,
	hasSkillZip bool,
) (status string, created bool, err error) {
	var egg model.Egg
	queryErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&egg, "id = ?", meta.ID).Error
	var existingEgg *model.Egg
	if queryErr == nil {
		existingEgg = &egg
	}

	resolvedCategoryID, err := resolveAdminEggCategoryTx(tx, meta.CategoryID, meta.EggI18n, meta.PublishNow, existingEgg)
	if err != nil {
		return "", false, err
	}
	meta.CategoryID = resolvedCategoryID

	switch {
	case errors.Is(queryErr, gorm.ErrRecordNotFound):
		legacyPackageType := model.EggPackageTypePersonaZip
		legacyTargetClientType := model.EggTargetClientTypeOpenClaw
		if !hasPersonaZip && hasSkillZip {
			legacyPackageType = model.EggPackageTypeSkillZip
			legacyTargetClientType = model.EggTargetClientTypeClaude
		}

		egg = model.Egg{
			ID:               meta.ID,
			CategoryID:       resolvedCategoryID,
			PackageType:      legacyPackageType,
			TargetClientType: legacyTargetClientType,
			HasPersonaZip:    hasPersonaZip,
			HasSkillZip:      hasSkillZip,
			DefaultColor:     meta.Color,
			DefaultEmoji:     meta.Emoji,
			Status:           model.EggStatusDraft,
			InstallCount:     0,
		}
		if hasSkillZip {
			egg.SkillTargetType = model.EggTargetClientTypeClaude
		}
		if err := tx.Create(&egg).Error; err != nil {
			return "", false, err
		}
		status = egg.Status
		created = true
	case queryErr != nil:
		return "", false, queryErr
	default:
		updates := map[string]any{
			"category_id":        resolvedCategoryID,
			"default_color":      meta.Color,
			"default_emoji":      meta.Emoji,
			"has_persona_zip":    egg.HasPersonaZip || hasPersonaZip,
			"has_skill_zip":      egg.HasSkillZip || hasSkillZip,
			"skill_target_type":  egg.SkillTargetType,
			"package_type":       egg.PackageType,
			"target_client_type": egg.TargetClientType,
			"updated_at":         time.Now(),
		}
		if hasPersonaZip {
			updates["package_type"] = model.EggPackageTypePersonaZip
			updates["target_client_type"] = model.EggTargetClientTypeOpenClaw
		} else if hasSkillZip && strings.TrimSpace(egg.PackageType) == "" {
			updates["package_type"] = model.EggPackageTypeSkillZip
			updates["target_client_type"] = model.EggTargetClientTypeClaude
		}
		if hasSkillZip {
			updates["skill_target_type"] = model.EggTargetClientTypeClaude
		}
		if err := tx.Model(&model.Egg{}).Where("id = ?", meta.ID).Updates(updates).Error; err != nil {
			return "", false, err
		}
		status = egg.Status
	}

	if err := upsertEggI18nTx(tx, meta.ID, meta.EggI18n); err != nil {
		return "", false, err
	}
	return status, created, nil
}

type adminEggCategoryCandidate struct {
	ID           string
	Code         string
	Names        []string
	Descriptions []string
}

type adminEggCategoryTextRow struct {
	ID          string
	Code        string
	Name        string
	Description string
}

func resolveAdminEggCategoryTx(
	tx *gorm.DB,
	requestedCategoryID string,
	eggI18n []EggI18nInput,
	publishNow bool,
	existingEgg *model.Egg,
) (string, error) {
	requestedCategoryID = strings.TrimSpace(requestedCategoryID)
	if requestedCategoryID != "" {
		return requestedCategoryID, nil
	}

	if existingEgg != nil && strings.TrimSpace(existingEgg.CategoryID) != "" {
		category, err := loadAdminEggCategoryTx(tx, existingEgg.CategoryID)
		switch {
		case err == nil && (!publishNow || category.Status == model.EggCategoryStatusActive):
			return category.ID, nil
		case err == nil:
		case errors.Is(err, gorm.ErrRecordNotFound):
		default:
			return "", err
		}
	}

	matchedCategoryID, err := autoMatchAdminEggCategoryTx(tx, eggI18n)
	if err != nil {
		return "", err
	}
	if matchedCategoryID != "" {
		return matchedCategoryID, nil
	}

	return ensureAdminEggFallbackCategoryTx(tx)
}

func loadAdminEggCategoryTx(tx *gorm.DB, categoryID string) (model.EggCategory, error) {
	var category model.EggCategory
	err := tx.First(&category, "id = ?", strings.TrimSpace(categoryID)).Error
	return category, err
}

func autoMatchAdminEggCategoryTx(tx *gorm.DB, eggI18n []EggI18nInput) (string, error) {
	sourceText := buildAdminEggCategoryMatchText(eggI18n)
	if sourceText == "" {
		return "", nil
	}
	sourceTokens := tokenizeAdminEggCategoryText(sourceText)
	if len(sourceTokens) == 0 {
		return "", nil
	}

	var rows []adminEggCategoryTextRow
	if err := tx.Table("egg_categories AS c").
		Select("c.id, c.code, i.name, i.description").
		Joins("LEFT JOIN egg_category_i18n i ON i.category_id = c.id").
		Where("c.status = ?", model.EggCategoryStatusActive).
		Where("c.id <> ?", adminEggFallbackCategoryID).
		Order("c.sort_order ASC").
		Order("c.code ASC").
		Find(&rows).Error; err != nil {
		return "", err
	}

	candidates := make(map[string]*adminEggCategoryCandidate)
	orderedIDs := make([]string, 0)
	for _, row := range rows {
		candidate, exists := candidates[row.ID]
		if !exists {
			candidate = &adminEggCategoryCandidate{
				ID:   row.ID,
				Code: row.Code,
			}
			candidates[row.ID] = candidate
			orderedIDs = append(orderedIDs, row.ID)
		}
		if strings.TrimSpace(row.Name) != "" {
			candidate.Names = append(candidate.Names, row.Name)
		}
		if strings.TrimSpace(row.Description) != "" {
			candidate.Descriptions = append(candidate.Descriptions, row.Description)
		}
	}

	bestCategoryID := ""
	bestScore := 0
	for _, categoryID := range orderedIDs {
		score := scoreAdminEggCategoryCandidate(sourceText, sourceTokens, candidates[categoryID])
		if score > bestScore {
			bestScore = score
			bestCategoryID = categoryID
		}
	}
	if bestScore <= 0 {
		return "", nil
	}
	return bestCategoryID, nil
}

func scoreAdminEggCategoryCandidate(sourceText string, sourceTokens map[string]struct{}, candidate *adminEggCategoryCandidate) int {
	if candidate == nil {
		return 0
	}

	score := scoreAdminEggCategoryTextPair(sourceText, sourceTokens, candidate.Code, 2)
	for _, name := range candidate.Names {
		score += scoreAdminEggCategoryTextPair(sourceText, sourceTokens, name, 4)
	}
	for _, description := range candidate.Descriptions {
		score += scoreAdminEggCategoryTextPair(sourceText, sourceTokens, description, 1)
	}
	return score
}

func scoreAdminEggCategoryTextPair(sourceText string, sourceTokens map[string]struct{}, candidateText string, weight int) int {
	normalizedCandidate := normalizeAdminEggCategoryText(candidateText)
	if normalizedCandidate == "" {
		return 0
	}

	score := 0
	if strings.Contains(sourceText, normalizedCandidate) {
		score += weight * 3
	}
	score += countAdminEggCategoryOverlap(sourceTokens, tokenizeAdminEggCategoryText(normalizedCandidate)) * weight * 2
	return score
}

func buildAdminEggCategoryMatchText(eggI18n []EggI18nInput) string {
	parts := make([]string, 0, len(eggI18n)*3)
	for _, item := range eggI18n {
		if text := strings.TrimSpace(item.Name); text != "" {
			parts = append(parts, text)
		}
		if text := strings.TrimSpace(item.Description); text != "" {
			parts = append(parts, text)
		}
		if text := strings.TrimSpace(item.Vibe); text != "" {
			parts = append(parts, text)
		}
	}
	return normalizeAdminEggCategoryText(strings.Join(parts, " "))
}

func normalizeAdminEggCategoryText(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))

	needsSpace := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			needsSpace = true
			continue
		}
		if needsSpace {
			builder.WriteByte(' ')
			needsSpace = false
		}
	}
	return strings.TrimSpace(builder.String())
}

func tokenizeAdminEggCategoryText(value string) map[string]struct{} {
	normalized := normalizeAdminEggCategoryText(value)
	tokens := make(map[string]struct{})
	for _, field := range strings.Fields(normalized) {
		runes := []rune(field)
		switch {
		case len(runes) == 1 && unicode.IsLetter(runes[0]):
			tokens[field] = struct{}{}
		case len(runes) >= 2:
			tokens[field] = struct{}{}
			for idx := 0; idx < len(runes)-1; idx++ {
				tokens[string(runes[idx:idx+2])] = struct{}{}
			}
		}
	}
	return tokens
}

func countAdminEggCategoryOverlap(sourceTokens map[string]struct{}, candidateTokens map[string]struct{}) int {
	if len(sourceTokens) == 0 || len(candidateTokens) == 0 {
		return 0
	}

	count := 0
	for token := range candidateTokens {
		if _, ok := sourceTokens[token]; ok {
			count++
		}
	}
	return count
}

func ensureAdminEggFallbackCategoryTx(tx *gorm.DB) (string, error) {
	now := time.Now()
	category := model.EggCategory{
		ID:        adminEggFallbackCategoryID,
		Code:      adminEggFallbackCategoryCode,
		Status:    model.EggCategoryStatusActive,
		SortOrder: adminEggFallbackCategorySort,
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"code":       category.Code,
			"status":     category.Status,
			"sort_order": category.SortOrder,
			"updated_at": now,
		}),
	}).Create(&category).Error; err != nil {
		return "", err
	}

	i18nRows := []model.EggCategoryI18n{
		{
			CategoryID:  adminEggFallbackCategoryID,
			Locale:      "zh-CN",
			Name:        "待分类",
			Description: "自动发布时未匹配到合适分类的 egg",
		},
		{
			CategoryID:  adminEggFallbackCategoryID,
			Locale:      "en-US",
			Name:        "Uncategorized",
			Description: "Eggs that were not matched to an existing category during upload",
		},
	}
	for _, row := range i18nRows {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "category_id"}, {Name: "locale"}},
			DoUpdates: clause.Assignments(map[string]any{
				"name":        row.Name,
				"description": row.Description,
				"updated_at":  now,
			}),
		}).Create(&row).Error; err != nil {
			return "", err
		}
	}

	return adminEggFallbackCategoryID, nil
}

func nextAdminEggVersionTx(tx *gorm.DB, eggID string) (int, error) {
	var latest model.EggVersion
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("egg_id = ?", eggID).
		Order("version DESC").
		Take(&latest).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 1, nil
		}
		return 0, err
	}
	return latest.Version + 1, nil
}

func createAdminEggVersionTx(
	tx *gorm.DB,
	meta *adminEggCreateWithFilesMetaNormalized,
	version int,
	publishedAt time.Time,
	personaArtifact *adminEggPreparedArtifact,
	skillArtifact *adminEggPreparedArtifact,
) error {
	legacyURL := ""
	legacySHA := ""
	var legacySize int64
	if personaArtifact != nil {
		legacyURL = personaArtifact.URL
		legacySHA = personaArtifact.SHA256
		legacySize = personaArtifact.Size
	} else if skillArtifact != nil {
		legacyURL = skillArtifact.URL
		legacySHA = skillArtifact.SHA256
		legacySize = skillArtifact.Size
	}

	row := model.EggVersion{
		EggID:                meta.ID,
		Version:              version,
		ZipURL:               legacyURL,
		ZipSHA256:            legacySHA,
		ZipSize:              legacySize,
		ArtifactManifestJSON: datatypes.JSON(meta.ArtifactManifest),
		PublishedAt:          &publishedAt,
	}
	if personaArtifact != nil {
		row.PersonaZipURL = personaArtifact.URL
		row.PersonaZipSHA256 = personaArtifact.SHA256
		row.PersonaZipSize = personaArtifact.Size
	}
	if skillArtifact != nil {
		row.SkillZipURL = skillArtifact.URL
		row.SkillZipSHA256 = skillArtifact.SHA256
		row.SkillZipSize = skillArtifact.Size
	}
	if err := tx.Create(&row).Error; err != nil {
		return err
	}
	return upsertEggVersionI18nTx(tx, meta.ID, version, meta.VersionI18n)
}

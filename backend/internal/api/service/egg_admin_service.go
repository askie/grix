package service

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func AdminEggCategoryList(req AdminEggCategoryListReq) ([]EggCategoryResp, *errcode.ErrCode) {
	query := store.DB.Model(&model.EggCategory{})
	if status := model.NormalizeEggCategoryStatus(req.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword := strings.TrimSpace(req.Keyword); keyword != "" {
		query = query.Joins("JOIN egg_category_i18n ci ON ci.category_id = egg_categories.id").
			Where("ci.name ILIKE ?", "%"+keyword+"%")
	}

	var categories []model.EggCategory
	if err := query.
		Select("egg_categories.*").
		Group("egg_categories.id").
		Order("sort_order ASC").
		Order("code ASC").
		Find(&categories).Error; err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询分类失败"}
	}

	categoryIDs := make([]string, 0, len(categories))
	for _, c := range categories {
		categoryIDs = append(categoryIDs, c.ID)
	}
	var allCatI18n []model.EggCategoryI18n
	if len(categoryIDs) > 0 {
		if err := store.DB.Where("category_id IN ?", categoryIDs).Order("locale ASC").Find(&allCatI18n).Error; err != nil {
			return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询分类文案失败"}
		}
	}
	catI18nMap := make(map[string][]EggCategoryI18nOutput, len(categories))
	for _, row := range allCatI18n {
		catI18nMap[row.CategoryID] = append(catI18nMap[row.CategoryID], EggCategoryI18nOutput{
			Locale:      row.Locale,
			Name:        row.Name,
			Description: row.Description,
		})
	}
	resp := make([]EggCategoryResp, 0, len(categories))
	for _, category := range categories {
		i18n := catI18nMap[category.ID]
		if i18n == nil {
			i18n = []EggCategoryI18nOutput{}
		}
		resp = append(resp, EggCategoryResp{
			ID:        category.ID,
			Code:      category.Code,
			Status:    category.Status,
			SortOrder: category.SortOrder,
			I18n:      i18n,
		})
	}
	return resp, nil
}

func AdminEggCategoryCreate(req AdminEggCategoryCreateReq) (*EggCategoryResp, *errcode.ErrCode) {
	code := strings.TrimSpace(req.Code)
	if code == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "分类 code 不能为空"}
	}
	if len(req.I18n) == 0 {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "分类 i18n 不能为空"}
	}
	normalizedI18n, ec := normalizeCategoryI18nInputs(req.I18n)
	if ec != nil {
		return nil, ec
	}

	categoryID := strings.TrimSpace(req.ID)
	if categoryID == "" {
		categoryID = code
	}
	status := model.NormalizeEggCategoryStatus(req.Status)
	if status == "" {
		status = model.EggCategoryStatusActive
	}
	if !model.IsValidEggCategoryStatus(status) {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "分类状态不支持"}
	}

	err := store.DB.Transaction(func(tx *gorm.DB) error {
		category := model.EggCategory{
			ID:        categoryID,
			Code:      code,
			Status:    status,
			SortOrder: req.SortOrder,
		}
		if err := tx.Create(&category).Error; err != nil {
			return err
		}
		return upsertEggCategoryI18nTx(tx, categoryID, normalizedI18n)
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, &errcode.ErrCode{HTTPStatus: 409, BizCode: 10005, Msg: "分类已存在"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "创建分类失败"}
	}

	return AdminEggCategoryGet(categoryID)
}

func AdminEggCategoryGet(categoryID string) (*EggCategoryResp, *errcode.ErrCode) {
	var category model.EggCategory
	if err := store.DB.First(&category, "id = ?", categoryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "分类不存在"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询分类失败"}
	}

	var i18nRows []model.EggCategoryI18n
	if err := store.DB.Where("category_id = ?", categoryID).Order("locale ASC").Find(&i18nRows).Error; err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询分类文案失败"}
	}
	respI18n := make([]EggCategoryI18nOutput, 0, len(i18nRows))
	for _, row := range i18nRows {
		respI18n = append(respI18n, EggCategoryI18nOutput{
			Locale:      row.Locale,
			Name:        row.Name,
			Description: row.Description,
		})
	}

	return &EggCategoryResp{
		ID:        category.ID,
		Code:      category.Code,
		Status:    category.Status,
		SortOrder: category.SortOrder,
		I18n:      respI18n,
	}, nil
}

func AdminEggCategoryUpdate(categoryID string, req AdminEggCategoryUpdateReq) (*EggCategoryResp, *errcode.ErrCode) {
	if len(req.I18n) == 0 {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "分类 i18n 不能为空"}
	}
	normalizedI18n, ec := normalizeCategoryI18nInputs(req.I18n)
	if ec != nil {
		return nil, ec
	}

	updates := map[string]any{
		"sort_order": req.SortOrder,
		"updated_at": time.Now(),
	}
	if code := strings.TrimSpace(req.Code); code != "" {
		updates["code"] = code
	}

	err := store.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.EggCategory{}).Where("id = ?", categoryID).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return upsertEggCategoryI18nTx(tx, categoryID, normalizedI18n)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "分类不存在"}
		}
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, &errcode.ErrCode{HTTPStatus: 409, BizCode: 10005, Msg: "分类 code 重复"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "更新分类失败"}
	}

	return AdminEggCategoryGet(categoryID)
}

func AdminEggCategoryUpdateStatus(categoryID string, req AdminEggCategoryStatusReq) *errcode.ErrCode {
	status := model.NormalizeEggCategoryStatus(req.Status)
	if !model.IsValidEggCategoryStatus(status) {
		return &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "分类状态不支持"}
	}

	result := store.DB.Model(&model.EggCategory{}).
		Where("id = ?", categoryID).
		Updates(map[string]any{
			"status":     status,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "更新分类状态失败"}
	}
	if result.RowsAffected == 0 {
		return &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "分类不存在"}
	}
	return nil
}

func AdminEggList(req AdminEggListReq) (*AdminEggListResp, *errcode.ErrCode) {
	page := req.Page
	pageSize := req.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	query := store.DB.Model(&model.Egg{})
	if status := model.NormalizeEggStatus(req.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	if categoryID := strings.TrimSpace(req.CategoryID); categoryID != "" {
		query = query.Where("category_id = ?", categoryID)
	}
	if keyword := strings.TrimSpace(req.Keyword); keyword != "" {
		query = query.Joins("JOIN egg_i18n ei ON ei.egg_id = eggs.id").
			Where("ei.name ILIKE ? OR ei.description ILIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}

	var total int64
	if err := query.Distinct("eggs.id").Count(&total).Error; err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询 egg 列表失败"}
	}

	var eggs []model.Egg
	if err := query.
		Select("eggs.*").
		Group("eggs.id").
		Order("updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&eggs).Error; err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询 egg 列表失败"}
	}

	list := make([]AdminEggListItem, 0, len(eggs))
	for _, egg := range eggs {
		item := AdminEggListItem{
			ID:           egg.ID,
			CategoryID:   egg.CategoryID,
			Status:       egg.Status,
			InstallCount: egg.InstallCount,
			Pinned:       egg.PinnedAt != nil,
			UpdatedAt:    egg.UpdatedAt.Unix(),
		}
		if egg.PinnedAt != nil {
			item.PinnedAt = egg.PinnedAt.Unix()
		}
		list = append(list, item)
	}

	return &AdminEggListResp{
		Page:     page,
		PageSize: pageSize,
		Total:    total,
		HasMore:  int64(page*pageSize) < total,
		List:     list,
	}, nil
}

func AdminEggGet(eggID string) (*AdminEggDetailResp, *errcode.ErrCode) {
	eggID = strings.TrimSpace(eggID)
	if eggID == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "egg id 不能为空"}
	}

	var egg model.Egg
	if err := store.DB.First(&egg, "id = ?", eggID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "egg 不存在"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询 egg 失败"}
	}

	var i18nRows []model.EggI18n
	if err := store.DB.Where("egg_id = ?", egg.ID).Order("locale ASC").Find(&i18nRows).Error; err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询 egg 文案失败"}
	}
	respI18n := make([]EggI18nOutput, 0, len(i18nRows))
	for _, row := range i18nRows {
		respI18n = append(respI18n, EggI18nOutput{
			Locale:      row.Locale,
			Name:        row.Name,
			Description: row.Description,
			Vibe:        row.Vibe,
		})
	}

	detail := &AdminEggDetailResp{
		ID:           egg.ID,
		CategoryID:   egg.CategoryID,
		Color:        egg.DefaultColor,
		Emoji:        egg.DefaultEmoji,
		Status:       egg.Status,
		InstallCount: egg.InstallCount,
		Pinned:       egg.PinnedAt != nil,
		I18n:         respI18n,
	}
	if egg.PinnedAt != nil {
		detail.PinnedAt = egg.PinnedAt.Unix()
	}
	return detail, nil
}

func AdminEggCreate(req AdminEggCreateReq) (*AdminEggDetailResp, *errcode.ErrCode) {
	eggID := strings.TrimSpace(req.ID)
	if eggID == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "egg id 不能为空"}
	}
	normalizedI18n, ec := normalizeEggI18nInputs(req.I18n)
	if ec != nil {
		return nil, ec
	}
	if ec := validateAdminEggUpsert(req.CategoryID, normalizedI18n); ec != nil {
		return nil, ec
	}

	color := strings.TrimSpace(req.Color)
	if color == "" {
		color = "#D97706"
	}
	emoji := strings.TrimSpace(req.Emoji)
	if emoji == "" {
		emoji = "🌍"
	}

	err := store.DB.Transaction(func(tx *gorm.DB) error {
		egg := model.Egg{
			ID:               eggID,
			CategoryID:       strings.TrimSpace(req.CategoryID),
			PackageType:      model.EggPackageTypePersonaZip,
			TargetClientType: model.EggTargetClientTypeOpenClaw,
			DefaultColor:     color,
			DefaultEmoji:     emoji,
			Status:           model.EggStatusDraft,
			InstallCount:     0,
		}
		if err := tx.Create(&egg).Error; err != nil {
			return err
		}
		return upsertEggI18nTx(tx, eggID, normalizedI18n)
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, &errcode.ErrCode{HTTPStatus: 409, BizCode: 10005, Msg: "egg 已存在"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "创建 egg 失败"}
	}
	return AdminEggGet(eggID)
}

func AdminEggUpdate(eggID string, req AdminEggUpdateReq) (*AdminEggDetailResp, *errcode.ErrCode) {
	normalizedI18n, ec := normalizeEggI18nInputs(req.I18n)
	if ec != nil {
		return nil, ec
	}
	if ec := validateAdminEggUpsert(req.CategoryID, normalizedI18n); ec != nil {
		return nil, ec
	}

	updates := map[string]any{
		"category_id": strings.TrimSpace(req.CategoryID),
		"updated_at":  time.Now(),
	}
	if color := strings.TrimSpace(req.Color); color != "" {
		updates["default_color"] = color
	}
	if emoji := strings.TrimSpace(req.Emoji); emoji != "" {
		updates["default_emoji"] = emoji
	}

	err := store.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Egg{}).Where("id = ?", eggID).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return upsertEggI18nTx(tx, eggID, normalizedI18n)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "egg 不存在"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "更新 egg 失败"}
	}

	return AdminEggGet(eggID)
}

func AdminEggUpdateStatus(eggID string, req AdminEggStatusReq) *errcode.ErrCode {
	status := model.NormalizeEggStatus(req.Status)
	if !model.IsValidEggStatus(status) {
		return &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "egg 状态不支持"}
	}

	if status == model.EggStatusPublished {
		var count int64
		if err := store.DB.Model(&model.EggVersion{}).Where("egg_id = ?", eggID).Count(&count).Error; err != nil {
			return &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "校验版本失败"}
		}
		if count == 0 {
			return &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "发布前至少需要一个版本"}
		}

		var egg model.Egg
		if err := store.DB.First(&egg, "id = ?", eggID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "egg 不存在"}
			}
			return &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询 egg 失败"}
		}
		var category model.EggCategory
		if err := store.DB.First(&category, "id = ?", egg.CategoryID).Error; err != nil {
			return &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询分类失败"}
		}
		if category.Status != model.EggCategoryStatusActive {
			return &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "分类停用时不可发布"}
		}
	}

	result := store.DB.Model(&model.Egg{}).
		Where("id = ?", eggID).
		Updates(map[string]any{
			"status":     status,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "更新 egg 状态失败"}
	}
	if result.RowsAffected == 0 {
		return &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "egg 不存在"}
	}
	return nil
}

func AdminEggSetPinned(eggID string, req AdminEggPinReq) *errcode.ErrCode {
	var pinnedAt any
	if req.Pinned {
		pinnedAt = time.Now()
	} else {
		pinnedAt = nil
	}

	result := store.DB.Model(&model.Egg{}).
		Where("id = ?", eggID).
		Updates(map[string]any{
			"pinned_at":  pinnedAt,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "更新置顶状态失败"}
	}
	if result.RowsAffected == 0 {
		return &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "egg 不存在"}
	}
	return nil
}

func AdminEggVersionList(eggID string) ([]EggVersionResp, *errcode.ErrCode) {
	eggID = strings.TrimSpace(eggID)
	if eggID == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "egg id 不能为空"}
	}

	var versions []model.EggVersion
	if err := store.DB.Where("egg_id = ?", eggID).Order("version DESC").Find(&versions).Error; err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询版本失败"}
	}

	var allVerI18n []model.EggVersionI18n
	if len(versions) > 0 {
		if err := store.DB.Where("egg_id = ?", eggID).Order("locale ASC").Find(&allVerI18n).Error; err != nil {
			return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询版本文案失败"}
		}
	}
	verI18nMap := make(map[int][]EggVersionI18nInput, len(versions))
	for _, row := range allVerI18n {
		verI18nMap[row.Version] = append(verI18nMap[row.Version], EggVersionI18nInput{
			Locale:      row.Locale,
			VersionDesc: row.VersionDesc,
		})
	}

	resp := make([]EggVersionResp, 0, len(versions))
	for _, version := range versions {
		i18n := verI18nMap[version.Version]
		if i18n == nil {
			i18n = []EggVersionI18nInput{}
		}

		publishedAt := int64(0)
		if version.PublishedAt != nil {
			publishedAt = version.PublishedAt.Unix()
		}

		resp = append(resp, EggVersionResp{
			EggID:            version.EggID,
			Version:          version.Version,
			ZipURL:           version.ZipURL,
			ZipSHA256:        version.ZipSHA256,
			ZipSize:          version.ZipSize,
			PersonaZipURL:    version.PersonaZipURL,
			PersonaZipSHA256: version.PersonaZipSHA256,
			PersonaZipSize:   version.PersonaZipSize,
			SkillZipURL:      version.SkillZipURL,
			SkillZipSHA256:   version.SkillZipSHA256,
			SkillZipSize:     version.SkillZipSize,
			ArtifactManifest: json.RawMessage(version.ArtifactManifestJSON),
			PublishedAt:      publishedAt,
			I18n:             i18n,
		})
	}

	return resp, nil
}

func AdminEggVersionPresign(eggID string, req AdminEggVersionPresignReq) (*AdminEggVersionPresignResp, *errcode.ErrCode) {
	eggID = strings.TrimSpace(eggID)
	if eggID == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "egg id 不能为空"}
	}
	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		filename = "package.zip"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".zip") {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "仅支持 zip 文件"}
	}

	version, err := previewAdminEggVersion(eggID)
	if err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "计算版本号失败"}
	}

	objectKey := buildAdminEggVersionUploadObjectKey(eggID, version, filename)
	presign, err := OSSPresignForExactObjectKey(objectKey, "application/zip")
	if err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "生成上传凭证失败"}
	}

	return &AdminEggVersionPresignResp{
		UploadURL: presign.UploadURL,
		ObjectKey: presign.ObjectKey,
		ZipURL:    presign.MediaAccessURL,
	}, nil
}

func previewAdminEggVersion(eggID string) (int, error) {
	var version int
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		nextVersion, err := nextAdminEggVersionTx(tx, eggID)
		if err != nil {
			return err
		}
		version = nextVersion
		return nil
	})
	return version, err
}

func AdminEggVersionCreate(eggID string, req AdminEggVersionCreateReq) (*EggVersionResp, *errcode.ErrCode) {
	eggID = strings.TrimSpace(eggID)
	if eggID == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "egg id 不能为空"}
	}
	if req.Version <= 0 {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "version 必须大于 0"}
	}

	// Resolve persona zip: prefer new fields, fall back to legacy
	personaZipURL := strings.TrimSpace(req.PersonaZipURL)
	personaZipSHA := strings.TrimSpace(req.PersonaZipSHA256)
	personaZipSize := req.PersonaZipSize
	if personaZipURL == "" {
		personaZipURL = strings.TrimSpace(req.ZipURL)
		personaZipSHA = strings.TrimSpace(req.ZipSHA256)
		personaZipSize = req.ZipSize
	}

	// Resolve skill zip: new fields only
	skillZipURL := strings.TrimSpace(req.SkillZipURL)
	skillZipSHA := strings.TrimSpace(req.SkillZipSHA256)
	skillZipSize := req.SkillZipSize

	// At least one package must be provided
	if personaZipURL == "" && skillZipURL == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "至少需要提供 persona_zip 或 skill_zip"}
	}
	if personaZipURL != "" && (personaZipSHA == "" || personaZipSize <= 0) {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "persona_zip 元数据不完整"}
	}
	if skillZipURL != "" && (skillZipSHA == "" || skillZipSize <= 0) {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "skill_zip 元数据不完整"}
	}
	if len(req.I18n) == 0 {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "版本 i18n 不能为空"}
	}
	normalizedI18n, ec := normalizeVersionI18nInputs(req.I18n)
	if ec != nil {
		return nil, ec
	}

	manifest := req.ArtifactManifest
	if len(manifest) == 0 {
		manifest = json.RawMessage(`{}`)
	}
	publishedAt := time.Now()

	// Legacy zip fields: use persona zip if present, otherwise skill zip
	legacyZipURL := personaZipURL
	legacyZipSHA := personaZipSHA
	legacyZipSize := personaZipSize
	if legacyZipURL == "" {
		legacyZipURL = skillZipURL
		legacyZipSHA = skillZipSHA
		legacyZipSize = skillZipSize
	}

	err := store.DB.Transaction(func(tx *gorm.DB) error {
		version := model.EggVersion{
			EggID:                eggID,
			Version:              req.Version,
			ZipURL:               legacyZipURL,
			ZipSHA256:            legacyZipSHA,
			ZipSize:              legacyZipSize,
			PersonaZipURL:        personaZipURL,
			PersonaZipSHA256:     personaZipSHA,
			PersonaZipSize:       personaZipSize,
			SkillZipURL:          skillZipURL,
			SkillZipSHA256:       skillZipSHA,
			SkillZipSize:         skillZipSize,
			ArtifactManifestJSON: datatypes.JSON(manifest),
			PublishedAt:          &publishedAt,
		}
		if err := tx.Create(&version).Error; err != nil {
			return err
		}
		if err := upsertEggVersionI18nTx(tx, eggID, req.Version, normalizedI18n); err != nil {
			return err
		}
		return updateEggPackageCapabilitiesTx(tx, eggID, personaZipURL != "", skillZipURL != "")
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "egg 不存在"}
		}
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, &errcode.ErrCode{HTTPStatus: 409, BizCode: 10005, Msg: "版本已存在"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "创建版本失败"}
	}

	return AdminEggVersionGet(eggID, req.Version)
}

func updateEggPackageCapabilitiesTx(tx *gorm.DB, eggID string, hasPersonaZip bool, hasSkillZip bool) error {
	updates := map[string]any{
		"has_persona_zip": gorm.Expr("has_persona_zip OR ?", hasPersonaZip),
		"has_skill_zip":   gorm.Expr("has_skill_zip OR ?", hasSkillZip),
		"updated_at":      time.Now(),
	}
	if hasPersonaZip {
		updates["package_type"] = model.EggPackageTypePersonaZip
		updates["target_client_type"] = model.EggTargetClientTypeOpenClaw
	} else if hasSkillZip {
		updates["package_type"] = model.EggPackageTypeSkillZip
		updates["target_client_type"] = model.EggTargetClientTypeClaude
	}
	if hasSkillZip {
		updates["skill_target_type"] = model.EggTargetClientTypeClaude
	}

	result := tx.Model(&model.Egg{}).Where("id = ?", eggID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func AdminEggVersionGet(eggID string, version int) (*EggVersionResp, *errcode.ErrCode) {
	var row model.EggVersion
	if err := store.DB.First(&row, "egg_id = ? AND version = ?", eggID, version).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "版本不存在"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询版本失败"}
	}
	var i18nRows []model.EggVersionI18n
	if err := store.DB.Where("egg_id = ? AND version = ?", eggID, version).Order("locale ASC").Find(&i18nRows).Error; err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询版本文案失败"}
	}
	i18n := make([]EggVersionI18nInput, 0, len(i18nRows))
	for _, i := range i18nRows {
		i18n = append(i18n, EggVersionI18nInput{Locale: i.Locale, VersionDesc: i.VersionDesc})
	}

	publishedAt := int64(0)
	if row.PublishedAt != nil {
		publishedAt = row.PublishedAt.Unix()
	}
	return &EggVersionResp{
		EggID:            row.EggID,
		Version:          row.Version,
		ZipURL:           row.ZipURL,
		ZipSHA256:        row.ZipSHA256,
		ZipSize:          row.ZipSize,
		PersonaZipURL:    row.PersonaZipURL,
		PersonaZipSHA256: row.PersonaZipSHA256,
		PersonaZipSize:   row.PersonaZipSize,
		SkillZipURL:      row.SkillZipURL,
		SkillZipSHA256:   row.SkillZipSHA256,
		SkillZipSize:     row.SkillZipSize,
		ArtifactManifest: json.RawMessage(row.ArtifactManifestJSON),
		PublishedAt:      publishedAt,
		I18n:             i18n,
	}, nil
}

func AdminEggVersionUpdate(eggID string, version int, req AdminEggVersionUpdateReq) (*EggVersionResp, *errcode.ErrCode) {
	if version <= 0 {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "version 必须大于 0"}
	}
	if len(req.I18n) == 0 {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "版本 i18n 不能为空"}
	}
	normalizedI18n, ec := normalizeVersionI18nInputs(req.I18n)
	if ec != nil {
		return nil, ec
	}

	err := store.DB.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"updated_at": time.Now()}
		if len(req.ArtifactManifest) > 0 {
			updates["artifact_manifest_json"] = datatypes.JSON(req.ArtifactManifest)
		}
		result := tx.Model(&model.EggVersion{}).Where("egg_id = ? AND version = ?", eggID, version).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return upsertEggVersionI18nTx(tx, eggID, version, normalizedI18n)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "版本不存在"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "更新版本失败"}
	}

	return AdminEggVersionGet(eggID, version)
}

func normalizeCategoryI18nInputs(inputs []EggCategoryI18nInput) ([]EggCategoryI18nInput, *errcode.ErrCode) {
	normalized := make([]EggCategoryI18nInput, 0, len(inputs))
	seenLocales := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		locale, ok := NormalizeAdminEggLocale(input.Locale)
		if !ok {
			return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "分类 i18n 语言仅支持 zh-CN / en-US"}
		}
		if strings.TrimSpace(input.Name) == "" {
			return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "分类 i18n 需要 locale 和 name"}
		}
		if _, exists := seenLocales[locale]; exists {
			return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "分类 i18n 语言重复"}
		}
		seenLocales[locale] = struct{}{}
		normalized = append(normalized, EggCategoryI18nInput{
			Locale:      locale,
			Name:        strings.TrimSpace(input.Name),
			Description: strings.TrimSpace(input.Description),
		})
	}
	return normalized, nil
}

func normalizeEggI18nInputs(inputs []EggI18nInput) ([]EggI18nInput, *errcode.ErrCode) {
	normalized := make([]EggI18nInput, 0, len(inputs))
	seenLocales := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		locale, ok := NormalizeAdminEggLocale(input.Locale)
		if !ok {
			return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "egg i18n 语言仅支持 zh-CN / en-US"}
		}
		if _, exists := seenLocales[locale]; exists {
			return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "egg i18n 语言重复"}
		}
		seenLocales[locale] = struct{}{}
		normalized = append(normalized, EggI18nInput{
			Locale:      locale,
			Name:        strings.TrimSpace(input.Name),
			Description: strings.TrimSpace(input.Description),
			Vibe:        strings.TrimSpace(input.Vibe),
		})
	}
	return normalized, nil
}

func validateAdminEggUpsert(categoryID string, i18n []EggI18nInput) *errcode.ErrCode {
	if strings.TrimSpace(categoryID) == "" {
		return &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "category_id 不能为空"}
	}
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

func normalizeVersionI18nInputs(inputs []EggVersionI18nInput) ([]EggVersionI18nInput, *errcode.ErrCode) {
	normalized := make([]EggVersionI18nInput, 0, len(inputs))
	seenLocales := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		locale, ok := NormalizeAdminEggLocale(input.Locale)
		if !ok {
			return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "版本 i18n 语言仅支持 zh-CN / en-US"}
		}
		if _, exists := seenLocales[locale]; exists {
			return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "版本 i18n 语言重复"}
		}
		seenLocales[locale] = struct{}{}
		normalized = append(normalized, EggVersionI18nInput{
			Locale:      locale,
			VersionDesc: strings.TrimSpace(input.VersionDesc),
		})
	}
	return normalized, nil
}

func upsertEggCategoryI18nTx(tx *gorm.DB, categoryID string, inputs []EggCategoryI18nInput) error {
	for _, input := range inputs {
		row := model.EggCategoryI18n{
			CategoryID:  categoryID,
			Locale:      strings.TrimSpace(input.Locale),
			Name:        strings.TrimSpace(input.Name),
			Description: strings.TrimSpace(input.Description),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "category_id"}, {Name: "locale"}},
			DoUpdates: clause.Assignments(map[string]any{
				"name":        row.Name,
				"description": row.Description,
				"updated_at":  time.Now(),
			}),
		}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func upsertEggI18nTx(tx *gorm.DB, eggID string, inputs []EggI18nInput) error {
	for _, input := range inputs {
		now := time.Now().UTC()
		row := model.EggI18n{
			EggID:       eggID,
			Locale:      strings.TrimSpace(input.Locale),
			Name:        strings.TrimSpace(input.Name),
			Description: strings.TrimSpace(input.Description),
			Vibe:        strings.TrimSpace(input.Vibe),
			SearchTextNormalized: buildEggSearchText(
				input.Name,
				input.Description,
				input.Vibe,
			),
		}
		searchTSVValue := buildEggSearchTSVValue(tx, row.SearchTextNormalized)
		if err := tx.Model(&model.EggI18n{}).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "egg_id"}, {Name: "locale"}},
			DoUpdates: clause.Assignments(map[string]any{
				"name":                   row.Name,
				"description":            row.Description,
				"vibe":                   row.Vibe,
				"search_text_normalized": row.SearchTextNormalized,
				"search_tsv":             searchTSVValue,
				"updated_at":             now,
			}),
		}).Create(map[string]any{
			"egg_id":                 row.EggID,
			"locale":                 row.Locale,
			"name":                   row.Name,
			"description":            row.Description,
			"vibe":                   row.Vibe,
			"search_text_normalized": row.SearchTextNormalized,
			"search_tsv":             searchTSVValue,
			"created_at":             now,
			"updated_at":             now,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func buildEggSearchTSVValue(tx *gorm.DB, normalized string) any {
	if tx != nil && tx.Dialector != nil && tx.Dialector.Name() == "postgres" {
		return gorm.Expr("to_tsvector('simple', ?)", normalized)
	}
	return normalized
}

func upsertEggVersionI18nTx(tx *gorm.DB, eggID string, version int, inputs []EggVersionI18nInput) error {
	for _, input := range inputs {
		row := model.EggVersionI18n{
			EggID:       eggID,
			Version:     version,
			Locale:      strings.TrimSpace(input.Locale),
			VersionDesc: strings.TrimSpace(input.VersionDesc),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "egg_id"}, {Name: "version"}, {Name: "locale"}},
			DoUpdates: clause.Assignments(map[string]any{
				"version_desc": row.VersionDesc,
				"updated_at":   time.Now(),
			}),
		}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

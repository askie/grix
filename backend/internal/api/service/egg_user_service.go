package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

const (
	eggInstallModeCreateNew     = "create_new"
	eggInstallModeExistingAgent = "existing_agent"
)

type eggListDisplay struct {
	item       EggSearchItem
	localeUsed string
	version    int
}

func normalizeEggInstallMode(raw string) string {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "", eggInstallModeCreateNew:
		return eggInstallModeCreateNew
	case eggInstallModeExistingAgent:
		return eggInstallModeExistingAgent
	default:
		return ""
	}
}

func resolveEggRequestLocale(userID int64, reqLocale string) string {
	locale := strings.TrimSpace(reqLocale)
	if locale != "" {
		return locale
	}
	preferred, _ := loadUserPreferredLanguage(userID)
	if strings.EqualFold(preferred, "zh") {
		return "zh-CN"
	}
	return "en-US"
}

func normalizeEggSearchPagination(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}
	return page, pageSize
}

func normalizeEggSearchKeyword(raw string) string {
	parts := splitEggSearchTerms(raw)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func splitEggSearchTerms(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
		return true
	})
	if len(fields) == 0 {
		return nil
	}

	terms := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		term := strings.ToLower(strings.TrimSpace(field))
		if term == "" {
			continue
		}
		if _, exists := seen[term]; exists {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	return terms
}

func buildEggSearchText(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		partTerms := splitEggSearchTerms(part)
		if len(partTerms) == 0 {
			continue
		}
		terms = append(terms, partTerms...)
	}
	if len(terms) == 0 {
		return ""
	}
	return strings.Join(terms, " ")
}

func EggCategoryList(userID int64, req EggCategoryListReq) (*EggCategoryListResp, *errcode.ErrCode) {
	if store.DB == nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "数据库未初始化"}
	}

	locale := resolveEggRequestLocale(userID, req.Locale)
	localeChain := buildEggLocaleChain(locale)

	var categories []model.EggCategory
	if err := store.DB.Model(&model.EggCategory{}).
		Joins("JOIN eggs ON eggs.category_id = egg_categories.id").
		Where("egg_categories.status = ?", model.EggCategoryStatusActive).
		Where("eggs.status = ?", model.EggStatusPublished).
		Select("egg_categories.*").
		Group("egg_categories.id").
		Order("egg_categories.sort_order ASC").
		Order("egg_categories.code ASC").
		Find(&categories).Error; err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询分类失败"}
	}

	list := make([]EggCategoryListItem, 0, len(categories))
	localeUsed := normalizeEggLocale(locale)
	for _, category := range categories {
		text, hitLocale, err := pickEggCategoryI18nByLocale(category.ID, localeChain)
		if err != nil {
			return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询分类文案失败"}
		}
		if hitLocale != "" {
			localeUsed = hitLocale
		}
		list = append(list, EggCategoryListItem{
			ID:          category.ID,
			Code:        category.Code,
			Name:        text.Name,
			Description: text.Description,
		})
	}

	return &EggCategoryListResp{
		LocaleUsed: localeUsed,
		List:       list,
	}, nil
}

func EggSearch(userID int64, req EggSearchReq) (*EggSearchResp, *errcode.ErrCode) {
	if store.DB == nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "数据库未初始化"}
	}

	page, pageSize := normalizeEggSearchPagination(req.Page, req.PageSize)
	locale := resolveEggRequestLocale(userID, req.Locale)
	localeChain := buildEggLocaleChain(locale)

	query := store.DB.Model(&model.Egg{}).
		Joins("JOIN egg_categories c ON c.id = eggs.category_id").
		Where("eggs.status = ?", model.EggStatusPublished).
		Where("c.status = ?", model.EggCategoryStatusActive)
	searchPhraseLike := ""

	if categoryID := strings.TrimSpace(req.CategoryID); categoryID != "" {
		query = query.Where("eggs.category_id = ?", categoryID)
	}

	normalizedKeyword := normalizeEggSearchKeyword(req.Keyword)
	if normalizedKeyword != "" {
		terms := splitEggSearchTerms(normalizedKeyword)
		searchPhraseLike = "%" + normalizedKeyword + "%"
		query = query.Joins("JOIN egg_i18n ei_filter ON ei_filter.egg_id = eggs.id").
			Where("ei_filter.locale IN ?", localeChain)
		for _, term := range terms {
			query = query.Where("ei_filter.search_text_normalized LIKE ?", "%"+term+"%")
		}
	}

	var total int64
	if err := query.Distinct("eggs.id").Count(&total).Error; err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询 egg 列表失败"}
	}

	fetchQuery := query.Order("eggs.pinned_at DESC NULLS LAST")
	if searchPhraseLike != "" {
		fetchQuery = fetchQuery.Select(
			"eggs.*, MAX(CASE WHEN ei_filter.search_text_normalized LIKE ? THEN 1 ELSE 0 END) AS search_phrase_rank",
			searchPhraseLike,
		).Order("search_phrase_rank DESC")
	} else {
		fetchQuery = fetchQuery.Select("eggs.*")
	}

	var eggs []model.Egg
	if err := fetchQuery.
		Group("eggs.id").
		Order("eggs.install_count DESC").
		Order("eggs.updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&eggs).Error; err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询 egg 列表失败"}
	}

	list := make([]EggSearchItem, 0, len(eggs))
	localeUsed := normalizeEggLocale(locale)
	for _, egg := range eggs {
		display, err := buildEggListDisplay(egg, localeChain)
		if err != nil {
			return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "加载 egg 展示信息失败"}
		}
		scheduleEggDisplayTranslations(egg.ID, display.version, locale)
		if display.localeUsed != "" {
			localeUsed = display.localeUsed
		}
		list = append(list, display.item)
	}

	hasMore := int64(page*pageSize) < total
	return &EggSearchResp{
		LocaleUsed: localeUsed,
		Page:       page,
		PageSize:   pageSize,
		HasMore:    hasMore,
		List:       list,
	}, nil
}

func buildEggListDisplay(egg model.Egg, localeChain []string) (*eggListDisplay, error) {
	eggText, eggLocale, err := pickEggI18nByLocale(egg.ID, localeChain)
	if err != nil {
		return nil, err
	}
	categoryText, categoryLocale, err := pickEggCategoryI18nByLocale(egg.CategoryID, localeChain)
	if err != nil {
		return nil, err
	}
	version, err := pickLatestEggVersion(egg.ID)
	if err != nil {
		return nil, err
	}
	versionText, versionLocale, err := pickEggVersionI18nByLocale(egg.ID, version.Version, localeChain)
	if err != nil {
		return nil, err
	}
	capabilities := buildEggInstallCapabilities(version)

	localeUsed := eggLocale
	if localeUsed == "" {
		localeUsed = categoryLocale
	}
	if localeUsed == "" {
		localeUsed = versionLocale
	}

	item := EggSearchItem{
		ID:                       egg.ID,
		CategoryID:               egg.CategoryID,
		CategoryName:             categoryText.Name,
		Name:                     eggText.Name,
		Description:              eggText.Description,
		Color:                    egg.DefaultColor,
		Emoji:                    egg.DefaultEmoji,
		Vibe:                     eggText.Vibe,
		CanCreateAgent:           capabilities.CanCreateAgent,
		ExistingAgentClientTypes: capabilities.ExistingAgentClientTypes,
		Status:                   egg.Status,
		Version:                  version.Version,
		VersionDesc:              versionText.VersionDesc,
		InstallCount:             egg.InstallCount,
	}

	return &eggListDisplay{item: item, localeUsed: localeUsed, version: version.Version}, nil
}

func EggGet(userID int64, req EggGetReq) (*EggGetResp, *errcode.ErrCode) {
	eggID := strings.TrimSpace(req.ID)
	if eggID == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "egg id 不能为空"}
	}

	locale := resolveEggRequestLocale(userID, req.Locale)
	localeChain := buildEggLocaleChain(locale)

	var egg model.Egg
	if err := store.DB.First(&egg, "id = ?", eggID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "egg 不存在"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询 egg 失败"}
	}

	if egg.Status != model.EggStatusPublished {
		return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "egg 不可见"}
	}

	var category model.EggCategory
	if err := store.DB.First(&category, "id = ?", egg.CategoryID).Error; err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询分类失败"}
	}
	if category.Status != model.EggCategoryStatusActive {
		return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "分类不可用"}
	}

	var version model.EggVersion
	if req.Version > 0 {
		if err := store.DB.First(&version, "egg_id = ? AND version = ?", egg.ID, req.Version).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "版本不存在"}
			}
			return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询版本失败"}
		}
	} else {
		latest, err := pickLatestEggVersion(egg.ID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "缺少可用版本"}
			}
			return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询版本失败"}
		}
		version = latest
	}

	eggText, eggLocale, err := pickEggI18nByLocale(egg.ID, localeChain)
	if err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询 egg 文案失败"}
	}
	categoryText, categoryLocale, err := pickEggCategoryI18nByLocale(egg.CategoryID, localeChain)
	if err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询分类文案失败"}
	}
	versionText, versionLocale, err := pickEggVersionI18nByLocale(egg.ID, version.Version, localeChain)
	if err != nil {
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询版本文案失败"}
	}
	scheduleEggDisplayTranslations(egg.ID, version.Version, locale)

	localeUsed := eggLocale
	if localeUsed == "" {
		localeUsed = categoryLocale
	}
	if localeUsed == "" {
		localeUsed = versionLocale
	}
	if localeUsed == "" {
		localeUsed = normalizeEggLocale(locale)
	}
	capabilities := buildEggInstallCapabilities(version)

	return &EggGetResp{
		LocaleUsed:               localeUsed,
		ID:                       egg.ID,
		CategoryID:               egg.CategoryID,
		CategoryName:             categoryText.Name,
		Name:                     eggText.Name,
		Description:              eggText.Description,
		Color:                    egg.DefaultColor,
		Emoji:                    egg.DefaultEmoji,
		Vibe:                     eggText.Vibe,
		CanCreateAgent:           capabilities.CanCreateAgent,
		ExistingAgentClientTypes: capabilities.ExistingAgentClientTypes,
		Status:                   egg.Status,
		Version:                  version.Version,
		VersionDesc:              versionText.VersionDesc,
		InstallCount:             egg.InstallCount,
		ArtifactManifest:         json.RawMessage(version.ArtifactManifestJSON),
	}, nil
}

func EggInstall(userID int64, req EggInstallReq) (*EggInstallAcceptResp, *errcode.ErrCode) {
	eggID := strings.TrimSpace(req.EggID)
	if eggID == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "egg_id 不能为空"}
	}

	egg, version, ec := loadInstallableEgg(eggID, req.Version)
	if ec != nil {
		return nil, ec
	}
	return startEggInstallViaMainAgent(userID, req, egg, version)
}

func EggInstallStatus(userID int64, installID string) (*EggInstallStatusResp, *errcode.ErrCode) {
	installID = strings.TrimSpace(installID)
	if installID == "" {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "install_id 不能为空"}
	}

	var install model.EggInstall
	if err := store.DB.Where("install_id = ? AND user_id = ?", installID, userID).First(&install).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "安装任务不存在"}
		}
		return nil, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询安装任务失败"}
	}

	targetAgentID := ""
	if install.TargetAgentID != nil && *install.TargetAgentID > 0 {
		targetAgentID = fmt.Sprintf("%d", *install.TargetAgentID)
	}
	executorAgentID := ""
	if install.ExecutorAgentID != nil && *install.ExecutorAgentID > 0 {
		executorAgentID = fmt.Sprintf("%d", *install.ExecutorAgentID)
	}

	return &EggInstallStatusResp{
		InstallID:       install.InstallID,
		Status:          install.Status,
		Step:            install.Step,
		SessionID:       strings.TrimSpace(install.SessionID),
		ExecutorAgentID: executorAgentID,
		TargetAgentID:   targetAgentID,
		ErrorCode:       install.ErrorCode,
		ErrorMsg:        install.ErrorMsg,
	}, nil
}

func loadInstallableEgg(eggID string, requestedVersion int) (model.Egg, model.EggVersion, *errcode.ErrCode) {
	var egg model.Egg
	if err := store.DB.First(&egg, "id = ?", eggID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Egg{}, model.EggVersion{}, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "egg 不存在"}
		}
		return model.Egg{}, model.EggVersion{}, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询 egg 失败"}
	}
	if egg.Status != model.EggStatusPublished {
		return model.Egg{}, model.EggVersion{}, &errcode.ErrCode{HTTPStatus: 403, BizCode: 10002, Msg: "egg 不可安装"}
	}

	var category model.EggCategory
	if err := store.DB.First(&category, "id = ?", egg.CategoryID).Error; err != nil {
		return model.Egg{}, model.EggVersion{}, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询分类失败"}
	}
	if category.Status != model.EggCategoryStatusActive {
		return model.Egg{}, model.EggVersion{}, &errcode.ErrCode{HTTPStatus: 403, BizCode: 10002, Msg: "分类不可安装"}
	}

	var version model.EggVersion
	var err error
	if requestedVersion > 0 {
		err = store.DB.First(&version, "egg_id = ? AND version = ?", egg.ID, requestedVersion).Error
	} else {
		version, err = pickLatestEggVersion(egg.ID)
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Egg{}, model.EggVersion{}, &errcode.ErrCode{HTTPStatus: 404, BizCode: 10004, Msg: "可安装版本不存在"}
		}
		return model.Egg{}, model.EggVersion{}, &errcode.ErrCode{HTTPStatus: 500, BizCode: 50001, Msg: "查询版本失败"}
	}
	return egg, version, nil
}

func pickLatestEggVersion(eggID string) (model.EggVersion, error) {
	var version model.EggVersion
	err := store.DB.Where("egg_id = ?", eggID).Order("version DESC").First(&version).Error
	return version, err
}

func pickEggI18nByLocale(eggID string, localeChain []string) (model.EggI18n, string, error) {
	var rows []model.EggI18n
	if err := store.DB.Where("egg_id = ?", eggID).Find(&rows).Error; err != nil {
		return model.EggI18n{}, "", err
	}
	if len(rows) == 0 {
		return model.EggI18n{}, "", gorm.ErrRecordNotFound
	}
	picked, locale := pickBestEggI18n(localeChain, rows)
	return picked, locale, nil
}

func pickEggCategoryI18nByLocale(categoryID string, localeChain []string) (model.EggCategoryI18n, string, error) {
	var rows []model.EggCategoryI18n
	if err := store.DB.Where("category_id = ?", categoryID).Find(&rows).Error; err != nil {
		return model.EggCategoryI18n{}, "", err
	}
	if len(rows) == 0 {
		return model.EggCategoryI18n{}, "", gorm.ErrRecordNotFound
	}
	picked, locale := pickBestEggCategoryI18n(localeChain, rows)
	return picked, locale, nil
}

func pickEggVersionI18nByLocale(eggID string, version int, localeChain []string) (model.EggVersionI18n, string, error) {
	var rows []model.EggVersionI18n
	if err := store.DB.Where("egg_id = ? AND version = ?", eggID, version).Find(&rows).Error; err != nil {
		return model.EggVersionI18n{}, "", err
	}
	if len(rows) == 0 {
		return model.EggVersionI18n{}, "", gorm.ErrRecordNotFound
	}
	picked, locale := pickBestEggVersionI18n(localeChain, rows)
	return picked, locale, nil
}

func pickBestEggI18n(localeChain []string, rows []model.EggI18n) (model.EggI18n, string) {
	for _, locale := range localeChain {
		for _, row := range rows {
			if strings.EqualFold(row.Locale, locale) {
				return row, row.Locale
			}
		}
	}
	return rows[0], rows[0].Locale
}

func pickBestEggCategoryI18n(localeChain []string, rows []model.EggCategoryI18n) (model.EggCategoryI18n, string) {
	for _, locale := range localeChain {
		for _, row := range rows {
			if strings.EqualFold(row.Locale, locale) {
				return row, row.Locale
			}
		}
	}
	return rows[0], rows[0].Locale
}

func pickBestEggVersionI18n(localeChain []string, rows []model.EggVersionI18n) (model.EggVersionI18n, string) {
	for _, locale := range localeChain {
		for _, row := range rows {
			if strings.EqualFold(row.Locale, locale) {
				return row, row.Locale
			}
		}
	}
	return rows[0], rows[0].Locale
}

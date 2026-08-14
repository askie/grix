package agentapi

import (
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
)

// dispatchEggSearch 搜索虾蛋市场，无需授权，对所有 agent 开放。
func dispatchEggSearch(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	keyword, _ := paramString(params, "keyword")
	categoryID, _ := paramString(params, "category_id")
	locale, _ := paramString(params, "locale")
	page, _ := paramInt(params, "page")
	pageSize, _ := paramInt(params, "page_size")

	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	resp, ec := service.EggSearch(ownerID, service.EggSearchReq{
		Keyword:    strings.TrimSpace(keyword),
		CategoryID: strings.TrimSpace(categoryID),
		Locale:     strings.TrimSpace(locale),
		Page:       page,
		PageSize:   pageSize,
	})
	if ec != nil {
		return nil, ec.BizCode, ec.Msg
	}
	return resp, 0, ""
}

// dispatchEggGet 获取单个虾蛋详情，无需授权，对所有 agent 开放。
func dispatchEggGet(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	id, ok := paramString(params, "id")
	if !ok || strings.TrimSpace(id) == "" {
		return nil, 4001, "id required"
	}
	locale, _ := paramString(params, "locale")
	version, _ := paramInt(params, "version")

	resp, ec := service.EggGet(ownerID, service.EggGetReq{
		ID:      strings.TrimSpace(id),
		Locale:  strings.TrimSpace(locale),
		Version: version,
	})
	if ec != nil {
		return nil, ec.BizCode, ec.Msg
	}
	return resp, 0, ""
}

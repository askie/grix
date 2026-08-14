package admin

import (
	"net/http"
	"strconv"
	"strings"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/gin-gonic/gin"
)

// registerLinkBlocklistAPIRoutes 注册链接黑名单相关 JSON 接口。
// 设计文档见 docs/architecture/35_link_safety_protection_design.md。
func registerLinkBlocklistAPIRoutes(g *gin.RouterGroup) {
	rules := g.Group("/link-blocklist/rules")
	{
		rules.GET("", apiListLinkBlocklistRules)
		rules.POST("", apiCreateLinkBlocklistRule)
		rules.GET("/:id", apiGetLinkBlocklistRule)
		rules.PUT("/:id", apiUpdateLinkBlocklistRule)
		rules.DELETE("/:id", apiDeleteLinkBlocklistRule)
		rules.POST("/batch", apiBatchLinkBlocklistRules)
	}
	g.POST("/link-blocklist/test", apiTestLinkBlocklist)
	g.POST("/link-blocklist/import", apiImportLinkBlocklistCSV)
	g.GET("/link-blocklist/stats", apiLinkBlocklistStats)
	g.GET("/link-blocklist/events", apiLinkBlocklistRecentEvents)
	g.GET("/link-blocklist/settings", apiGetLinkSafetySettings)
	g.PUT("/link-blocklist/settings", apiUpdateLinkSafetySettings)
}

// --- 列表 / 单条 / 增删改 ---

func apiListLinkBlocklistRules(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	params := adminservice.LinkBlocklistListParams{
		Query:    c.Query("q"),
		Kind:     c.Query("kind"),
		Severity: c.Query("severity"),
		Source:   c.Query("source"),
		Page:     page,
		PageSize: pageSize,
	}
	if v := strings.TrimSpace(c.Query("enabled")); v != "" {
		b := v == "1" || strings.EqualFold(v, "true")
		params.Enabled = &b
	}
	result, err := adminservice.ListLinkBlocklistRules(params)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{
		"items":     result.Items,
		"total":     result.Total,
		"page":      result.Page,
		"page_size": result.PageSize,
	})
}

func apiGetLinkBlocklistRule(c *gin.Context) {
	id, err := parseInt64Param(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "id 格式错误")
		return
	}
	row, err := adminservice.GetLinkBlocklistRule(id)
	if err != nil {
		status := http.StatusInternalServerError
		if err == adminservice.ErrNotFound {
			status = http.StatusNotFound
		}
		response.Fail(c, status, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"item": row})
}

func apiCreateLinkBlocklistRule(c *gin.Context) {
	var body adminservice.LinkBlocklistRulePayload
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	row, err := adminservice.CreateLinkBlocklistRule(
		c.Request.Context(),
		admin.ID, body, c.ClientIP(), c.Request.UserAgent(),
	)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"item": row})
}

func apiUpdateLinkBlocklistRule(c *gin.Context) {
	id, err := parseInt64Param(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "id 格式错误")
		return
	}
	var body adminservice.LinkBlocklistRulePayload
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	row, err := adminservice.UpdateLinkBlocklistRule(
		c.Request.Context(),
		admin.ID, id, body, c.ClientIP(), c.Request.UserAgent(),
	)
	if err != nil {
		status := http.StatusBadRequest
		if err == adminservice.ErrNotFound {
			status = http.StatusNotFound
		}
		response.Fail(c, status, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"item": row})
}

func apiDeleteLinkBlocklistRule(c *gin.Context) {
	id, err := parseInt64Param(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "id 格式错误")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	if err := adminservice.DeleteLinkBlocklistRule(
		c.Request.Context(),
		admin.ID, id, c.ClientIP(), c.Request.UserAgent(),
	); err != nil {
		status := http.StatusInternalServerError
		if err == adminservice.ErrNotFound {
			status = http.StatusNotFound
		}
		response.Fail(c, status, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

// --- 批量 ---

type batchLinkBlocklistRequest struct {
	IDs    []string `json:"ids"`
	Action string   `json:"action"`
}

func apiBatchLinkBlocklistRules(c *gin.Context) {
	var body batchLinkBlocklistRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	ids := make([]int64, 0, len(body.IDs))
	for _, s := range body.IDs {
		v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err == nil && v > 0 {
			ids = append(ids, v)
		}
	}
	if len(ids) == 0 {
		response.Fail(c, http.StatusBadRequest, 10002, "ids 不能为空")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	affected, err := adminservice.BatchUpdateLinkBlocklistRules(
		c.Request.Context(),
		admin.ID, ids,
		adminservice.BatchLinkBlocklistAction(strings.ToLower(strings.TrimSpace(body.Action))),
		c.ClientIP(), c.Request.UserAgent(),
	)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"affected": affected})
}

// --- 在线测试 ---

type testLinkBlocklistRequest struct {
	URL string `json:"url"`
}

func apiTestLinkBlocklist(c *gin.Context) {
	var body testLinkBlocklistRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	url := strings.TrimSpace(body.URL)
	if url == "" {
		response.Fail(c, http.StatusBadRequest, 10002, "url 不能为空")
		return
	}
	v := adminservice.TestLinkBlocklist(url)
	response.OK(c, gin.H{"result": v})
}

// --- 批量导入 CSV ---

type importLinkBlocklistRequest struct {
	CSV string `json:"csv"`
}

// 导入 body 不限大小（CSV 可能 MB 级），单独不上 16KB 限制；
// 上限 5MB（容纳 8 万行规则左右），足够一次性灌入开源反诈/钓鱼库。
const linkBlocklistImportMaxBytes = 5 * 1024 * 1024

func apiImportLinkBlocklistCSV(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, linkBlocklistImportMaxBytes)
	var body importLinkBlocklistRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	if strings.TrimSpace(body.CSV) == "" {
		response.Fail(c, http.StatusBadRequest, 10002, "csv 不能为空")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	result, err := adminservice.ImportLinkBlocklistRulesCSV(
		c.Request.Context(),
		admin.ID, body.CSV, c.ClientIP(), c.Request.UserAgent(),
	)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"result": result})
}

// --- 拦截统计 ---

func apiLinkBlocklistStats(c *gin.Context) {
	stats, err := adminservice.LoadLinkBlocklistStats(c.Request.Context())
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"stats": stats})
}

func apiLinkBlocklistRecentEvents(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	events, err := adminservice.LoadLinkBlocklistRecentEvents(c.Request.Context(), limit)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"events": events})
}

// --- 设置 ---

func apiGetLinkSafetySettings(c *gin.Context) {
	s, err := systemsetting.GetLinkSafetySettings()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"settings": s})
}

func apiUpdateLinkSafetySettings(c *gin.Context) {
	var s systemsetting.LinkSafetySettings
	if err := c.ShouldBindJSON(&s); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	if err := systemsetting.SaveLinkSafetySettings(s, &admin.ID); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	// 设置变更也清掉 matcher / 缓存，立即生效
	go func() {
		_ = systemsetting.GetLinkSafetySettings // touch
	}()
	response.OK(c, gin.H{"ok": true})
}

// --- 辅助 ---

func parseInt64Param(raw string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
}

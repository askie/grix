package admin

import (
	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"net/http"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func registerFeatureGateAPIRoutes(g *gin.RouterGroup) {
	g.GET("/feature-gates", apiListFeatureGates)
	g.GET("/feature-gates/whitelist", apiListFeatureGateWhitelist)
	g.POST("/feature-gates", apiCreateFeatureGate)
	g.POST("/feature-gates/status", apiUpdateFeatureGateStatus)
	g.POST("/feature-gates/users", apiFeatureGateUsers)
}

func apiListFeatureGates(c *gin.Context) {
	gates, err := featuregate.GetAllGates()
	if err != nil {
		gates = []featuregate.FeatureGateInfo{}
	}
	available, _ := featuregate.AvailableFeatures()
	avail := make([]gin.H, 0, len(available))
	for _, f := range available {
		avail = append(avail, gin.H{"key": f.Key, "display_name": f.DisplayName})
	}
	response.OK(c, gin.H{
		"gates":     gates,
		"available": avail,
	})
}

// apiListFeatureGateWhitelist 列出指定 feature gate 白名单内的用户（带搜索与分页）。
func apiListFeatureGateWhitelist(c *gin.Context) {
	key := strings.TrimSpace(c.Query("key"))
	if key == "" {
		response.Fail(c, http.StatusBadRequest, 10002, "缺少 key")
		return
	}
	if _, err := featuregate.GetGate(key); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, "功能开关不存在")
		return
	}
	entries, err := featuregate.GetWhitelistUsers(key)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.UserID)
	}

	result, err := adminservice.ListUsers(adminservice.UserListParams{
		Query:    c.Query("q"),
		IDs:      ids,
		Page:     page,
		PageSize: pageSize,
	})
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

func apiCreateFeatureGate(c *gin.Context) {
	var body struct {
		Key string `json:"key"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Key == "" {
		response.Fail(c, http.StatusBadRequest, 10002, "请选择功能开关")
		return
	}
	var displayName string
	for _, f := range featuregate.BuiltinFeatures {
		if f.Key == body.Key {
			displayName = f.DisplayName
			break
		}
	}
	if displayName == "" {
		response.Fail(c, http.StatusBadRequest, 10006, "不支持的功能开关: "+body.Key)
		return
	}
	_, err := featuregate.CreateGate(body.Key, displayName, "disabled")
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	featuregate.InvalidateCache()
	response.OK(c, gin.H{"ok": true})
}

func apiUpdateFeatureGateStatus(c *gin.Context) {
	var body struct {
		Key    string `json:"key"`
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Key == "" || body.Status == "" {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	if !featuregate.IsValidStatus(body.Status) {
		response.Fail(c, http.StatusBadRequest, 10006, "无效的状态值")
		return
	}
	if strings.HasPrefix(body.Key, "auth_") && body.Status == "whitelist" {
		response.Fail(c, http.StatusBadRequest, 10006, "认证类功能开关不支持白名单模式，请使用 enabled 或 disabled")
		return
	}
	if err := featuregate.UpdateGateStatus(body.Key, body.Status); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	featuregate.InvalidateCache()
	response.OK(c, gin.H{"ok": true})
}

func apiFeatureGateUsers(c *gin.Context) {
	var body struct {
		Key     string `json:"key"`
		Action  string `json:"action"`
		UserIDs string `json:"user_ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Key == "" || body.UserIDs == "" {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	parts := strings.Split(body.UserIDs, ",")
	var userIDs []int64
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			response.Fail(c, http.StatusBadRequest, 10002, "用户ID格式错误: "+p)
			return
		}
		userIDs = append(userIDs, id)
	}
	if len(userIDs) == 0 {
		response.Fail(c, http.StatusBadRequest, 10002, "请输入用户ID")
		return
	}
	if _, err := featuregate.GetGate(body.Key); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, "功能开关不存在")
		return
	}
	var err error
	switch body.Action {
	case "add":
		err = featuregate.AddUsersToWhitelist(body.Key, userIDs)
	case "remove":
		err = featuregate.RemoveUsersFromWhitelist(body.Key, userIDs)
	default:
		response.Fail(c, http.StatusBadRequest, 10006, "无效的操作")
		return
	}
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	featuregate.InvalidateCache()
	response.OK(c, gin.H{"ok": true})
}

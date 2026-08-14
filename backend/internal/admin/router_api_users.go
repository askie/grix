package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// registerUserLookupAPIRoutes 注册用户目录查询接口。
// 挂在仅登录鉴权的组上（不要求 users 权限）：各模块（举报/审查/计费等）
// 都需要把记录里的用户 ID 渲染成昵称；无 users 权限的管理员降敏返回（不含邮箱/手机号）。
func registerUserLookupAPIRoutes(g *gin.RouterGroup) {
	g.GET("/users/lookup", apiLookupUsers)
}

// registerUserAPIRoutes 注册用户管理相关 JSON 接口。
func registerUserAPIRoutes(g *gin.RouterGroup) {
	g.GET("/users", apiListUsers)
	g.GET("/users/:id/customer-coach-snapshot", apiGetUserCustomerCoachSnapshot)
	g.POST("/users/:id/ban", apiBanUser)
	g.POST("/users/:id/unban", apiUnbanUser)
	g.POST("/users/:id/unmute-moderation", apiUnmuteUserModeration)
	g.POST("/users/:id/unlock-login", apiUnlockUserLogin)
	g.POST("/users/:id/unbind-phone", apiUnbindUserPhone)
}

// lookupMaxIDs 单次批量查询的用户 ID 上限。
const lookupMaxIDs = 100

func apiLookupUsers(c *gin.Context) {
	raw := strings.Split(c.Query("ids"), ",")
	seen := make(map[int64]struct{}, len(raw))
	ids := make([]int64, 0, len(raw))
	for _, s := range raw {
		id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
		if len(ids) >= lookupMaxIDs {
			break
		}
	}
	if len(ids) == 0 {
		response.OK(c, gin.H{"items": []adminservice.UserListItem{}})
		return
	}

	result, err := adminservice.ListUsers(adminservice.UserListParams{
		IDs:      ids,
		PageSize: lookupMaxIDs,
	})
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}

	// 无 users 权限的管理员只用于展示昵称/状态，抹掉联系方式。
	if !currentAdminHasPermission(c, "users") {
		for i := range result.Items {
			result.Items[i].Email = ""
			result.Items[i].PhoneE164 = ""
			result.Items[i].PhoneCountry = ""
		}
	}
	response.OK(c, gin.H{"items": result.Items})
}

// currentAdminHasPermission 判断当前管理员是否拥有指定权限（超管恒真）。
func currentAdminHasPermission(c *gin.Context, key string) bool {
	admin := adminmiddleware.CurrentAdmin(c)
	if admin == nil {
		return false
	}
	if admin.Role == model.AdminRoleSuperAdmin {
		return true
	}
	for _, k := range adminmiddleware.GetPermissions(c) {
		if k == key {
			return true
		}
	}
	return false
}

func apiListUsers(c *gin.Context) {
	var status int16
	switch strings.TrimSpace(c.Query("status")) {
	case "active":
		status = model.UserStatusActive
	case "banned":
		status = model.UserStatusBanned
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	result, err := adminservice.ListUsers(adminservice.UserListParams{
		Query:      c.Query("q"),
		Status:     status,
		OnlineOnly: strings.EqualFold(strings.TrimSpace(c.Query("online")), "true"),
		Page:       page,
		PageSize:   pageSize,
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

func apiGetUserCustomerCoachSnapshot(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		response.Fail(c, http.StatusBadRequest, 10002, "无效用户ID")
		return
	}

	result, err := adminservice.GetUserCustomerCoachSnapshot(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Fail(c, http.StatusNotFound, 10005, "用户不存在")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}

	response.OK(c, result)
}

// apiUserActionHandler 抽象“按用户 ID 执行一个管理动作”的通用形态，
// 收敛 unban/unmute/unlock 的样板代码。
func apiUserActionHandler(c *gin.Context, action func(adminID, userID int64, clientIP, userAgent string) error) {
	admin := adminmiddleware.CurrentAdmin(c)
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "无效用户ID")
		return
	}
	if err := action(admin.ID, userID, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiBanUser(c *gin.Context) {
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	admin := adminmiddleware.CurrentAdmin(c)
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "无效用户ID")
		return
	}
	if err := adminservice.BanUser(admin.ID, userID, body.Reason, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiUnbanUser(c *gin.Context) {
	apiUserActionHandler(c, adminservice.UnbanUser)
}

func apiUnmuteUserModeration(c *gin.Context) {
	apiUserActionHandler(c, adminservice.UnmuteUserContentModerationSessions)
}

func apiUnlockUserLogin(c *gin.Context) {
	apiUserActionHandler(c, adminservice.UnlockUserLogin)
}

func apiUnbindUserPhone(c *gin.Context) {
	apiUserActionHandler(c, adminservice.UnbindUserPhone)
}

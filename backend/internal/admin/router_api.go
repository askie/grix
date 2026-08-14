package admin

import (
	"net/http"
	"strings"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	adminservice "github.com/askie/grix/backend/internal/admin/service"
	apimiddleware "github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// registerAPIRoutes 挂载面向 Flutter Admin App 的 JSON 接口组。
// 认证使用 Bearer Token，各业务模块分拆至独立 router_api_<module>.go。
func registerAPIRoutes(group *gin.RouterGroup) {
	api := group.Group("/api")

	// 公开接口：登录（暴力破解由 internal/security 指数退避限流防护）
	api.POST("/login", apimiddleware.RateLimitByIP("admin-api-login", 10, 5.0/60), apiLogin)

	// 需要鉴权的接口
	authed := api.Group("")
	authed.Use(adminmiddleware.RequireAPIAuth())
	authed.POST("/logout", apiLogout)
	authed.GET("/me", apiCurrentAdmin)
	authed.POST("/settings/password", apiChangeOwnPassword)
	registerDashboardAPIRoutes(authed)
	registerUserLookupAPIRoutes(authed)

	// 角色管理 + 管理员管理（超管专属）
	adminGroup := authed.Group("")
	adminGroup.Use(adminmiddleware.RequirePermission("admins"))
	registerRoleAPIRoutes(adminGroup)
	registerAdminAPIRoutes(adminGroup)

	// 各业务模块（按权限 key 保护）
	registerUserAPIRoutes(authedGroup(authed, "users"))
	// agent WS 连接安全管控复用 users 权限（同属账户/agent 治理域）
	registerAgentSecurityAPIRoutes(authedGroup(authed, "users"))
	registerReportAPIRoutes(authedGroup(authed, "reports"))
	registerModerationAPIRoutes(authedGroup(authed, "moderation"))
	registerVisitorBanAPIRoutes(authedGroup(authed, "visitor_bans"))
	registerSettingsAPIRoutes(authedGroup(authed, "settings"))
	registerSmsSettingsAPIRoutes(authedGroup(authed, "settings"))
	registerPushSettingsAPIRoutes(authedGroup(authed, "settings"))
	registerPayChannelSettingsAPIRoutes(authedGroup(authed, "settings"))
	registerFeatureGateAPIRoutes(authedGroup(authed, "feature_gates"))
	registerAppReleaseAPIRoutes(authedGroup(authed, "app"))
	registerConnectorAPIRoutes(authedGroup(authed, "connector"))
	registerEggAPIRoutes(authedGroup(authed, "eggs"))
	registerLinkBlocklistAPIRoutes(authedGroup(authed, "link_blocklist"))
	registerGatewayAPIRoutes(authedGroup(authed, "gateway_billing"))
	registerReachAPIRoutes(authedGroup(authed, "app"))
}

// authedGroup 创建一个附带 RequirePermission 中间件的子路由组。
func authedGroup(parent *gin.RouterGroup, permKey string) *gin.RouterGroup {
	g := parent.Group("")
	g.Use(adminmiddleware.RequirePermission(permKey))
	return g
}

// --- 认证 ---

type apiLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func apiLogin(c *gin.Context) {
	var req apiLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}

	if err := adminservice.EnsureAdminBootstrapAvailable(); err != nil {
		response.Fail(c, http.StatusForbidden, 10003, err.Error())
		return
	}

	sessionID, admin, err := adminservice.Login(
		strings.TrimSpace(req.Username),
		req.Password,
		c.ClientIP(),
		c.Request.UserAgent(),
	)
	if err != nil {
		response.Fail(c, http.StatusUnauthorized, 10001, err.Error())
		return
	}

	response.OK(c, gin.H{
		"token":       sessionID,
		"admin":       admin,
		"permissions": adminservice.LoadAdminPermissions(admin),
	})
}

func apiLogout(c *gin.Context) {
	_ = adminservice.Logout(adminmiddleware.CurrentSessionToken(c))
	response.OK(c, gin.H{"ok": true})
}

func apiCurrentAdmin(c *gin.Context) {
	admin := adminmiddleware.CurrentAdmin(c)
	response.OK(c, gin.H{
		"admin":       admin,
		"permissions": adminmiddleware.GetPermissions(c),
	})
}

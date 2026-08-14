package admin

import (
	"net/http"
	"strconv"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// registerAdminAPIRoutes 注册管理员管理相关 JSON 接口。
func registerAdminAPIRoutes(g *gin.RouterGroup) {
	g.GET("/admins", apiListAdmins)
	g.POST("/admins", apiCreateAdmin)
	g.POST("/admins/:id/enable", apiEnableAdmin)
	g.POST("/admins/:id/disable", apiDisableAdmin)
	g.DELETE("/admins/:id", apiDeleteAdmin)
}

func apiListAdmins(c *gin.Context) {
	items, err := adminservice.ListAdmins()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	// 附带当前管理员 ID，便于前端禁用“对自己”的危险操作。
	response.OK(c, gin.H{
		"items":            items,
		"current_admin_id": strconv.FormatInt(adminmiddleware.CurrentAdmin(c).ID, 10),
	})
}

func apiCreateAdmin(c *gin.Context) {
	var body struct {
		Username string  `json:"username"`
		Nickname string  `json:"nickname"`
		Password string  `json:"password"`
		Role     int16   `json:"role"`
		RoleID   *string `json:"role_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}

	var roleID *int64
	if body.RoleID != nil && *body.RoleID != "" {
		id, err := strconv.ParseInt(*body.RoleID, 10, 64)
		if err != nil {
			response.Fail(c, http.StatusBadRequest, 10002, "无效角色ID")
			return
		}
		roleID = &id
	}

	admin := adminmiddleware.CurrentAdmin(c)
	created, err := adminservice.CreateAdmin(admin.ID, adminservice.CreateAdminInput{
		Username: body.Username,
		Nickname: body.Nickname,
		Password: body.Password,
		Role:     body.Role,
		RoleID:   roleID,
	}, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"admin": created})
}

func apiEnableAdmin(c *gin.Context) {
	apiAdminActionHandler(c, adminservice.EnableAdmin)
}

func apiDisableAdmin(c *gin.Context) {
	apiAdminActionHandler(c, adminservice.DisableAdmin)
}

func apiDeleteAdmin(c *gin.Context) {
	apiAdminActionHandler(c, adminservice.DeleteAdmin)
}

// apiAdminActionHandler 收敛“对目标管理员执行操作”的样板。
func apiAdminActionHandler(c *gin.Context, action func(operatorID, targetID int64, clientIP, userAgent string) error) {
	targetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "无效管理员ID")
		return
	}
	operator := adminmiddleware.CurrentAdmin(c)
	if err := action(operator.ID, targetID, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

package admin

import (
	"net/http"
	"strconv"

	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// registerRoleAPIRoutes 注册角色管理接口（超管专属，已在调用处加 RequirePermission("admins")）。
func registerRoleAPIRoutes(g *gin.RouterGroup) {
	g.GET("/roles", apiListRoles)
	g.POST("/roles", apiCreateRole)
	g.PUT("/roles/:id", apiUpdateRole)
	g.DELETE("/roles/:id", apiDeleteRole)
}

func apiListRoles(c *gin.Context) {
	roles, err := adminservice.ListRoles()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"items": roles})
}

func apiCreateRole(c *gin.Context) {
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	role, err := adminservice.CreateRole(adminservice.RoleInput{
		Name:        body.Name,
		Description: body.Description,
		Permissions: body.Permissions,
	})
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"role": role})
}

func apiUpdateRole(c *gin.Context) {
	roleID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "无效角色ID")
		return
	}
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	role, err := adminservice.UpdateRole(roleID, adminservice.RoleInput{
		Name:        body.Name,
		Description: body.Description,
		Permissions: body.Permissions,
	})
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"role": role})
}

func apiDeleteRole(c *gin.Context) {
	roleID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "无效角色ID")
		return
	}
	if err := adminservice.DeleteRole(roleID); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

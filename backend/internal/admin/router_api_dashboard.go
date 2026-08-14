package admin

import (
	"net/http"

	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func registerDashboardAPIRoutes(g *gin.RouterGroup) {
	g.GET("/dashboard/stats", apiDashboardStats)
}

func apiDashboardStats(c *gin.Context) {
	data, err := adminservice.DashboardOverview()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, data)
}

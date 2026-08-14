package handler

import (
	"net/http"

	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

const (
	widgetNotImplementedCode = 50101
)

// RegisterWidgetRoutes wires widget-related API routes.
// Phase M0 only exposes route skeletons behind the feature flag.
func RegisterWidgetRoutes(v1 *gin.RouterGroup, authed *gin.RouterGroup) {
	if v1 == nil || authed == nil {
		return
	}

	widgetPublic := v1.Group("/widget")
	{
		widgetPublic.POST("/visitor/init",
			middleware.RateLimitByIP("widget-visitor-init", 20, 1.0/3),
			WidgetVisitorInit,
		)
		widgetPublic.GET("/config",
			middleware.RateLimitByIP("widget-config", 60, 1.0),
			WidgetVisitorConfig,
		)
	}
}

func RegisterWidgetManagementRoutes(authed *gin.RouterGroup) {
	if authed == nil {
		return
	}
	widgetSites := authed.Group("/widget/sites")
	{
		widgetSites.POST("/create", WidgetSiteCreate)
		widgetSites.POST("/update", WidgetSiteUpdate)
		widgetSites.GET("/list", WidgetSiteList)
		widgetSites.GET("/detail", WidgetSiteDetail)
		widgetSites.POST("/rotate_secret", WidgetSiteRotateSecret)
		widgetSites.POST("/delete", WidgetSiteDelete)
	}

	widgetSessions := authed.Group("/widget/sessions")
	{
		widgetSessions.GET("/list", WidgetSessionList)
		widgetSessions.POST("/close", WidgetSessionClose)
		widgetSessions.POST("/ban", WidgetSessionBan)
	}

	widgetIPBans := authed.Group("/widget/ip_bans")
	{
		widgetIPBans.GET("/list", WidgetIPBanList)
		widgetIPBans.POST("/delete", WidgetIPBanDelete)
	}
}

func WidgetNotImplemented(c *gin.Context) {
	response.Fail(
		c,
		http.StatusNotImplemented,
		widgetNotImplementedCode,
		"widget feature is not implemented yet",
	)
}

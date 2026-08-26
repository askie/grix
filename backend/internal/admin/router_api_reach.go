package admin

import (
	"github.com/askie/grix/backend/internal/api/handler"
	"github.com/gin-gonic/gin"
)

func registerReachAPIRoutes(group *gin.RouterGroup) {
	group.GET("/reach/tasks", handler.AdminListReachTasks)
	group.GET("/reach/tasks/:id", handler.AdminGetReachTask)
	group.POST("/reach/tasks/:id/pause", handler.AdminPauseReachTask)
	group.POST("/reach/tasks/:id/cancel", handler.AdminCancelReachTask)
	group.POST("/reach/tasks/:id/resume", handler.AdminResumeReachTask)
	group.PUT("/reach/tasks/:id/content", handler.AdminUpdateReachTaskContent)
	group.POST("/reach/tasks/:id/send", handler.AdminSendReachTask)
	group.GET("/reach/templates", handler.AdminListReachTemplates)
	group.GET("/reach/templates/:id", handler.AdminGetReachTemplate)
	group.POST("/reach/templates", handler.AdminCreateReachTemplate)
	group.PUT("/reach/templates/:id", handler.AdminUpdateReachTemplate)
	group.DELETE("/reach/templates/:id", handler.AdminDeleteReachTemplate)
	group.POST("/reach/tasks/marketing", handler.AdminCreateMarketingTask)
	group.POST("/reach/audience/preview", handler.AdminPreviewReachAudience)
	group.POST("/reach/tasks/ab-test", handler.AdminCreateABTest)
	group.GET("/reach/ab/:group_id/stats", handler.AdminGetABTestStats)
	group.GET("/reach/tasks/:id/stats", handler.AdminGetReachTaskStats)
	group.GET("/reach/subscriptions/overview", handler.AdminGetReachSubscriptionOverview)
	group.POST("/reach/direct", handler.AdminSendDirectUserReach)
	group.POST("/reach/email-preview", handler.AdminPreviewReachEmailTemplate)
}

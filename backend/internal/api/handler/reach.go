package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func AdminListReachTasks(c *gin.Context) {
	var req struct {
		Status   string `form:"status"`
		Kind     string `form:"kind"`
		Page     int    `form:"page"`
		PageSize int    `form:"page_size"`
	}
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	result, err := service.ListReachTasks(service.ListReachTasksReq{
		Status:   req.Status,
		Kind:     req.Kind,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, result)
}

func AdminGetReachTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid id")
		return
	}
	result, err := service.GetReachTask(id)
	if err != nil {
		response.Fail(c, http.StatusNotFound, 10004, "task not found")
		return
	}
	response.OK(c, result)
}

func AdminPauseReachTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid id")
		return
	}
	if err := service.PauseReachTask(id); err != nil {
		response.Fail(c, http.StatusConflict, 10009, err.Error())
		return
	}
	response.OK(c, struct{}{})
}

func AdminCancelReachTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid id")
		return
	}
	if err := service.CancelReachTask(id); err != nil {
		response.Fail(c, http.StatusConflict, 10009, err.Error())
		return
	}
	response.OK(c, struct{}{})
}

func AdminResumeReachTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid id")
		return
	}
	if err := service.ResumeReachTask(context.Background(), id); err != nil {
		response.Fail(c, http.StatusConflict, 10009, err.Error())
		return
	}
	response.OK(c, struct{}{})
}

// AdminUpdateReachTaskContent 编辑待发送公告草稿的中英文案。
func AdminUpdateReachTaskContent(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid id")
		return
	}
	var req service.ReachAnnouncementContent
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid body")
		return
	}
	if err := service.UpdateReachAnnouncementContent(id, req); err != nil {
		response.Fail(c, http.StatusConflict, 10009, err.Error())
		return
	}
	response.OK(c, struct{}{})
}

// AdminSendReachTask 手动触发待发送公告草稿的全量群发。
func AdminSendReachTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid id")
		return
	}
	if err := service.SendReachAnnouncement(context.Background(), id); err != nil {
		response.Fail(c, http.StatusConflict, 10009, err.Error())
		return
	}
	response.OK(c, struct{}{})
}

func AdminListReachTemplates(c *gin.Context) {
	var req struct {
		Page     int `form:"page"`
		PageSize int `form:"page_size"`
	}
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	result, err := service.ListReachTemplates(service.ListReachTemplatesReq{
		Page: req.Page, PageSize: req.PageSize,
	})
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, result)
}

func AdminGetReachTemplate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid id")
		return
	}
	result, err := service.GetReachTemplate(id)
	if err != nil {
		response.Fail(c, http.StatusNotFound, 10004, "template not found")
		return
	}
	response.OK(c, result)
}

func AdminCreateReachTemplate(c *gin.Context) {
	var req service.CreateReachTemplateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid body")
		return
	}
	result, err := service.CreateReachTemplate(req)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	response.OK(c, result)
}

func AdminUpdateReachTemplate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid id")
		return
	}
	var req service.UpdateReachTemplateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid body")
		return
	}
	result, err := service.UpdateReachTemplate(id, req)
	if err != nil {
		response.Fail(c, http.StatusNotFound, 10004, "template not found")
		return
	}
	response.OK(c, result)
}

func AdminGetReachTaskStats(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid id")
		return
	}
	result, err := service.GetReachTaskStats(id)
	if err != nil {
		response.Fail(c, http.StatusNotFound, 10004, "task not found")
		return
	}
	response.OK(c, result)
}

func AdminGetReachSubscriptionOverview(c *gin.Context) {
	result, err := service.GetReachSubscriptionOverview()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, result)
}

func AdminCreateMarketingTask(c *gin.Context) {
	var req service.CreateMarketingTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid body")
		return
	}
	if req.TemplateID == 0 || len(req.Channels) == 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "template_id and channels required")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	var adminID int64
	if admin != nil {
		adminID = admin.ID
	}
	result, err := service.CreateMarketingReachTask(c.Request.Context(), req, adminID)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	response.OK(c, result)
}

func AdminPreviewReachAudience(c *gin.Context) {
	var audience service.ReachAudience
	if err := c.ShouldBindJSON(&audience); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid body")
		return
	}
	n, err := service.CountReachAudience(&audience)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	response.OK(c, gin.H{"count": n})
}

func AdminSendDirectUserReach(c *gin.Context) {
	var req service.SendDirectUserReachReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid body")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	if admin != nil {
		req.CreatedBy = admin.ID
	}
	result, err := service.SendDirectUserReach(c.Request.Context(), req)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	response.OK(c, result)
}

func AdminDeleteReachTemplate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid id")
		return
	}
	if err := service.DeleteReachTemplate(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Fail(c, http.StatusNotFound, 10004, "template not found")
			return
		}
		response.Fail(c, http.StatusConflict, 10009, err.Error())
		return
	}
	response.OK(c, struct{}{})
}

var transparentGIF = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00,
	0x80, 0x00, 0x00, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21,
	0xf9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2c, 0x00, 0x00,
	0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44,
	0x01, 0x00, 0x3b,
}

func ReachTrackOpen(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err == nil && id > 0 {
		_ = service.TrackReachOpen(id)
	}
	c.Data(http.StatusOK, "image/gif", transparentGIF)
}

func ReachTrackClick(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err == nil && id > 0 {
		_ = service.TrackReachClick(id)
	}
	target := c.Query("url")
	if target == "" {
		target = "/"
	}
	c.Redirect(http.StatusFound, target)
}

func AdminCreateABTest(c *gin.Context) {
	var req service.CreateABTestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid body")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	var adminID int64
	if admin != nil {
		adminID = admin.ID
	}
	result, err := service.CreateABTest(c.Request.Context(), req, adminID)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	response.OK(c, result)
}

func AdminGetABTestStats(c *gin.Context) {
	groupID := c.Param("group_id")
	if groupID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "missing group_id")
		return
	}
	result, err := service.GetABTestStats(groupID)
	if err != nil {
		response.Fail(c, http.StatusNotFound, 10004, err.Error())
		return
	}
	response.OK(c, result)
}

func ReachUnsubscribe(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "missing token")
		return
	}
	if err := service.UnsubscribeByToken(token); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	response.OK(c, map[string]string{"message": "unsubscribed"})
}

func ReachGetSubscription(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sub, err := service.EnsureReachSubscription(userID, "")
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, sub)
}

func ReachUpdateSubscription(c *gin.Context) {
	userID := middleware.GetUserID(c)
	var req struct {
		Subscribed bool `json:"subscribed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid body")
		return
	}
	service.EnsureReachSubscription(userID, "")
	if err := service.UpdateReachSubscription(userID, req.Subscribed); err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, struct{}{})
}

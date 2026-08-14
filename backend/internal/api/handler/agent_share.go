package handler

import (
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type agentShareCreateReq struct {
	SharedTo int64 `json:"shared_to,string" binding:"required"`
}

// AgentShareCreate handles POST /v1/agents/:id/shares — 主人把 agent 共享给某账户。
func AgentShareCreate(c *gin.Context) {
	userID := middleware.GetUserID(c)
	userIDRef := userID
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return
	}
	var req agentShareCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	if ec := service.AgentShareCreate(userID, agentID, req.SharedTo); ec != nil {
		service.WriteAuditLog(service.WriteAuditLogReq{
			EventType: "agent_share_create_failed",
			UserID:    &userIDRef,
			ClientIP:  c.ClientIP(),
			UserAgent: c.GetHeader("User-Agent"),
			Detail: map[string]any{
				"agent_id":  strconv.FormatInt(agentID, 10),
				"shared_to": strconv.FormatInt(req.SharedTo, 10),
				"biz_code":  ec.BizCode,
			},
		})
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	service.WriteAuditLog(service.WriteAuditLogReq{
		EventType: "agent_share_create",
		UserID:    &userIDRef,
		ClientIP:  c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
		Detail: map[string]any{
			"agent_id":  strconv.FormatInt(agentID, 10),
			"shared_to": strconv.FormatInt(req.SharedTo, 10),
		},
	})
	response.OK(c, gin.H{"shared_to": strconv.FormatInt(req.SharedTo, 10)})
}

// AgentShareRevoke handles DELETE /v1/agents/:id/shares/:uid — 撤销共享。
func AgentShareRevoke(c *gin.Context) {
	userID := middleware.GetUserID(c)
	userIDRef := userID
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return
	}
	sharedTo, err := strconv.ParseInt(c.Param("uid"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的用户 ID")
		return
	}
	if ec := service.AgentShareRevoke(userID, agentID, sharedTo); ec != nil {
		service.WriteAuditLog(service.WriteAuditLogReq{
			EventType: "agent_share_revoke_failed",
			UserID:    &userIDRef,
			ClientIP:  c.ClientIP(),
			UserAgent: c.GetHeader("User-Agent"),
			Detail: map[string]any{
				"agent_id":  strconv.FormatInt(agentID, 10),
				"shared_to": strconv.FormatInt(sharedTo, 10),
				"biz_code":  ec.BizCode,
			},
		})
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	service.WriteAuditLog(service.WriteAuditLogReq{
		EventType: "agent_share_revoke",
		UserID:    &userIDRef,
		ClientIP:  c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
		Detail: map[string]any{
			"agent_id":  strconv.FormatInt(agentID, 10),
			"shared_to": strconv.FormatInt(sharedTo, 10),
		},
	})
	response.OK(c, nil)
}

// AgentShareList handles GET /v1/agents/:id/shares — 主人查看该 agent 的共享对象。
func AgentShareList(c *gin.Context) {
	userID := middleware.GetUserID(c)
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return
	}
	shares, ec := service.AgentShareList(userID, agentID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	list := make([]gin.H, 0, len(shares))
	for i := range shares {
		list = append(list, gin.H{
			"shared_to":  strconv.FormatInt(shares[i].SharedTo, 10),
			"created_at": shares[i].CreatedAt.Unix(),
		})
	}
	response.OK(c, gin.H{"list": list})
}

// AgentSharedWithMe handles GET /v1/agents/shared-with-me — 被共享者查看「分享给我的 agent」。
func AgentSharedWithMe(c *gin.Context) {
	userID := middleware.GetUserID(c)
	data, err := service.AgentSharedWithMe(userID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, gin.H{"list": data})
}

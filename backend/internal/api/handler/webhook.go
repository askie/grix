package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/askie/grix/backend/internal/webhook"
	"github.com/gin-gonic/gin"
)

var webhookService = webhook.NewService()

func WebhookCreate(c *gin.Context) {
	var req struct {
		SessionID string  `json:"session_id" binding:"required"`
		ExpiresAt *string `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil && strings.TrimSpace(*req.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.ExpiresAt))
		if err != nil {
			response.Fail(c, http.StatusBadRequest, 10003, "expires_at 格式错误")
			return
		}
		u := parsed.UTC()
		expiresAt = &u
	}
	baseURL := webhookBaseURL(c.Request)
	if strings.TrimSpace(baseURL) == "" {
		response.Fail(c, http.StatusInternalServerError, 50001, "webhook base url not configured")
		return
	}
	item, err := webhookService.CreateEndpoint(c.Request.Context(), webhook.CreateRequest{
		UserID:    middleware.GetUserID(c),
		SessionID: req.SessionID,
		ExpiresAt: expiresAt,
		BaseURL:   baseURL,
	})
	if err != nil {
		if errors.Is(err, webhook.ErrForbidden) {
			response.Fail(c, http.StatusForbidden, 4003, "无权限")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, item)
}

func WebhookList(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID := c.Param("session_id")
	items, err := webhookService.ListEndpoints(c.Request.Context(), userID, sessionID, webhookBaseURL(c.Request))
	if err != nil {
		if errors.Is(err, webhook.ErrForbidden) {
			response.Fail(c, http.StatusForbidden, 4003, "无权限")
			return
		}
		if errors.Is(err, webhook.ErrInvalidPayload) {
			response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, gin.H{"items": items})
}

func WebhookDelete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	err = webhookService.DeleteEndpoint(c.Request.Context(), middleware.GetUserID(c), id)
	if err != nil {
		if errors.Is(err, webhook.ErrNotFound) {
			response.Fail(c, http.StatusNotFound, 4004, "记录不存在")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, gin.H{"success": true})
}

func WebhookListAll(c *gin.Context) {
	userID := middleware.GetUserID(c)
	limit, _ := strconv.Atoi(strings.TrimSpace(c.Query("limit")))
	offset, _ := strconv.Atoi(strings.TrimSpace(c.Query("offset")))
	items, err := webhookService.ListEndpointsByUser(
		c.Request.Context(),
		userID,
		webhookBaseURL(c.Request),
		limit,
		offset,
	)
	if err != nil {
		if errors.Is(err, webhook.ErrInvalidPayload) {
			response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, gin.H{"items": items})
}

func webhookBaseURL(r *http.Request) string {
	for _, candidate := range []string{
		strings.TrimSpace(config.C.Server.AgentAPIDomain),
		strings.TrimSpace(config.C.Server.FriendQRBaseURL),
		strings.TrimSpace(config.C.Server.GroupQRBaseURL),
	} {
		if candidate == "" {
			continue
		}
		u, err := url.Parse(candidate)
		if err != nil || u.Host == "" {
			continue
		}
		scheme := u.Scheme
		switch scheme {
		case "ws":
			scheme = "http"
		case "wss":
			scheme = "https"
		}
		if scheme != "http" && scheme != "https" {
			scheme = "https"
		}
		return scheme + "://" + u.Host
	}
	return ""
}

package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type widgetVisitorInitRequest struct {
	SiteKey      string `json:"site_key"`
	VisitorKey   string `json:"visitor_key"`
	VisitorName  string `json:"visitor_name"`
	VisitorEmail string `json:"visitor_email"`
	PageURL      string `json:"page_url"`
	Locale       string `json:"locale"`
}

func WidgetVisitorInit(c *gin.Context) {
	var req widgetVisitorInitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid widget init payload")
		return
	}

	data, err := service.WidgetVisitorInit(service.WidgetVisitorInitInput{
		SiteKey:      req.SiteKey,
		VisitorKey:   req.VisitorKey,
		VisitorName:  req.VisitorName,
		VisitorEmail: req.VisitorEmail,
		PageURL:      req.PageURL,
		Locale:       req.Locale,
		Origin:       c.GetHeader("Origin"),
		WSURL:        resolveWidgetWSURL(c),
		ClientIP:     c.ClientIP(),
		UserAgent:    c.GetHeader("User-Agent"),
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrWidgetInitInvalidInput):
			response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		case errors.Is(err, service.ErrWidgetSiteNotFound):
			response.Fail(c, http.StatusNotFound, 4004, err.Error())
		case errors.Is(err, service.ErrWidgetSiteDisabled),
			errors.Is(err, service.ErrWidgetOriginNotAllowed),
			errors.Is(err, service.ErrWidgetVisitorBanned),
			errors.Is(err, service.ErrWidgetIPBanned):
			response.Fail(c, http.StatusForbidden, 4003, err.Error())
		case errors.Is(err, service.ErrWidgetVisitorRateLimit):
			response.Fail(c, http.StatusTooManyRequests, 10005, err.Error())
		default:
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}
	response.OK(c, data)
}

// WidgetVisitorConfig returns the site's public display config without
// creating a visitor session. Lets the loader apply appearance and decide
// auto-expand on page load cheaply.
func WidgetVisitorConfig(c *gin.Context) {
	siteKey := strings.TrimSpace(c.Query("site_key"))
	cfg, err := service.WidgetVisitorConfig(siteKey, c.GetHeader("Origin"), c.Query("locale"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrWidgetInitInvalidInput):
			response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		case errors.Is(err, service.ErrWidgetSiteNotFound):
			response.Fail(c, http.StatusNotFound, 4004, err.Error())
		case errors.Is(err, service.ErrWidgetSiteDisabled),
			errors.Is(err, service.ErrWidgetOriginNotAllowed):
			response.Fail(c, http.StatusForbidden, 4003, err.Error())
		default:
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}
	response.OK(c, gin.H{"display_config": cfg})
}

func resolveWidgetWSURL(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	host := strings.TrimSpace(c.Request.Host)
	if forwardedHost := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); forwardedHost != "" {
		host = forwardedHost
	}
	if host == "" {
		return ""
	}
	scheme := "ws"
	if c.Request.TLS != nil || strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https") {
		scheme = "wss"
	}
	return fmt.Sprintf("%s://%s/v1/widget/ws", scheme, host)
}

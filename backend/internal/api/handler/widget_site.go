package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func WidgetSiteCreate(c *gin.Context) {
	var req struct {
		SiteName       string                      `json:"site_name" binding:"required"`
		AllowedOrigins []string                    `json:"allowed_origins" binding:"required"`
		DisplayConfig  service.WidgetDisplayConfig `json:"display_config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	resp, err := service.WidgetSiteCreate(service.WidgetSiteCreateInput{
		OwnerUserID:    middleware.GetUserID(c),
		SiteName:       req.SiteName,
		AllowedOrigins: req.AllowedOrigins,
		DisplayConfig:  req.DisplayConfig,
	})
	if err != nil {
		handleWidgetSiteError(c, err)
		return
	}
	response.OK(c, resp)
}

func WidgetSiteUpdate(c *gin.Context) {
	var req struct {
		ID             int64                       `json:"id,string" binding:"required"`
		SiteName       string                      `json:"site_name" binding:"required"`
		AllowedOrigins []string                    `json:"allowed_origins" binding:"required"`
		DisplayConfig  service.WidgetDisplayConfig `json:"display_config"`
		Status         int16                       `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	resp, err := service.WidgetSiteUpdate(service.WidgetSiteUpdateInput{
		OwnerUserID:    middleware.GetUserID(c),
		SiteID:         req.ID,
		SiteName:       req.SiteName,
		AllowedOrigins: req.AllowedOrigins,
		DisplayConfig:  req.DisplayConfig,
		Status:         req.Status,
	})
	if err != nil {
		handleWidgetSiteError(c, err)
		return
	}
	response.OK(c, resp)
}

func WidgetSiteList(c *gin.Context) {
	status := parseInt16Query(c.Query("status"), 0)
	limit := parseIntQuery(c.Query("limit"), 20)
	offset := parseIntQuery(c.Query("offset"), 0)
	resp, err := service.WidgetSiteList(service.WidgetSiteListInput{
		OwnerUserID: middleware.GetUserID(c),
		Status:      status,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		handleWidgetSiteError(c, err)
		return
	}
	response.OK(c, resp)
}

func WidgetSiteDetail(c *gin.Context) {
	siteID, err := strconv.ParseInt(c.Query("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	resp, err := service.WidgetSiteDetail(middleware.GetUserID(c), siteID)
	if err != nil {
		handleWidgetSiteError(c, err)
		return
	}
	loaderURL := resolveWidgetLoaderURL(c)
	response.OK(c, gin.H{
		"site":       resp,
		"loader_url": loaderURL,
		"embed_code": buildWidgetEmbedCode(loaderURL, resp.SiteKey),
	})
}

func WidgetSiteRotateSecret(c *gin.Context) {
	var req struct {
		ID int64 `json:"id,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	resp, err := service.WidgetSiteRotateSecret(middleware.GetUserID(c), req.ID)
	if err != nil {
		handleWidgetSiteError(c, err)
		return
	}
	response.OK(c, resp)
}

func WidgetSiteDelete(c *gin.Context) {
	var req struct {
		ID int64 `json:"id,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	if err := service.WidgetSiteDelete(middleware.GetUserID(c), req.ID); err != nil {
		handleWidgetSiteError(c, err)
		return
	}
	response.OK(c, nil)
}

func handleWidgetSiteError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrWidgetSiteInvalidInput):
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
	case errors.Is(err, service.ErrWidgetSiteNotOwned):
		response.Fail(c, http.StatusNotFound, 4004, "记录不存在")
	default:
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
	}
}

func parseIntQuery(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func parseInt16Query(raw string, fallback int16) int16 {
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 16)
	if err != nil {
		return fallback
	}
	return int16(v)
}

func parseInt64Query(raw string, fallback int64) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

func resolveWidgetLoaderURL(c *gin.Context) string {
	proto := "http"
	if c.Request.TLS != nil {
		proto = "https"
	}
	if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
		proto = forwarded
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(c.Request.Host)
	}
	u := &url.URL{
		Scheme: proto,
		Host:   host,
		Path:   "/public/widget/widget.js",
	}
	return u.String()
}

func buildWidgetEmbedCode(loaderURL, siteKey string) string {
	key := strings.TrimSpace(siteKey)
	if strings.TrimSpace(loaderURL) == "" || key == "" {
		return ""
	}
	return "<script src=\"" + loaderURL + "\" data-site-key=\"" + key + "\" defer></script>"
}

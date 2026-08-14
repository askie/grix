package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func EggCategoryList(c *gin.Context) {
	userID := middleware.GetUserID(c)
	data, ec := service.EggCategoryList(userID, service.EggCategoryListReq{
		Locale: resolveEggRequestLocale(c),
	})
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

func EggSearch(c *gin.Context) {
	userID := middleware.GetUserID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	data, ec := service.EggSearch(userID, service.EggSearchReq{
		Keyword:    c.Query("keyword"),
		CategoryID: c.Query("category_id"),
		Locale:     resolveEggRequestLocale(c),
		Page:       page,
		PageSize:   pageSize,
	})
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

func EggGet(c *gin.Context) {
	userID := middleware.GetUserID(c)
	version, _ := strconv.Atoi(c.DefaultQuery("version", "0"))

	data, ec := service.EggGet(userID, service.EggGetReq{
		ID:      c.Query("id"),
		Locale:  resolveEggRequestLocale(c),
		Version: version,
	})
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

func EggInstall(c *gin.Context) {
	userID := middleware.GetUserID(c)
	var req service.EggInstallReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	req.Locale = resolveEggRequestLocale(c)

	data, ec := service.EggInstall(userID, req)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

func EggInstallStatus(c *gin.Context) {
	userID := middleware.GetUserID(c)
	data, ec := service.EggInstallStatus(userID, c.Param("install_id"))
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

func resolveEggRequestLocale(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if locale := strings.TrimSpace(c.Query("locale")); locale != "" {
		return locale
	}
	if locale := strings.TrimSpace(c.GetHeader("X-App-Locale")); locale != "" {
		return locale
	}
	return strings.TrimSpace(c.GetHeader("Accept-Language"))
}

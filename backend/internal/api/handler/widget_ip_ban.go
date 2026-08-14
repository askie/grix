package handler

import (
	"errors"
	"net/http"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// widget 访客 IP 封禁管理接口（owner 鉴权）。
// 路由注册见 widget_stub.go 的 RegisterWidgetManagementRoutes。

func WidgetIPBanList(c *gin.Context) {
	resp, err := service.WidgetIPBanList(middleware.GetUserID(c))
	if err != nil {
		handleWidgetIPBanError(c, err)
		return
	}
	response.OK(c, resp)
}

func WidgetIPBanDelete(c *gin.Context) {
	var req struct {
		ID int64 `json:"id,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	if err := service.WidgetIPBanDelete(middleware.GetUserID(c), req.ID); err != nil {
		handleWidgetIPBanError(c, err)
		return
	}
	response.OK(c, nil)
}

func handleWidgetIPBanError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrWidgetSiteInvalidInput):
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
	case errors.Is(err, service.ErrWidgetIPBanNotOwned):
		response.Fail(c, http.StatusNotFound, 4004, "记录不存在")
	default:
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
	}
}

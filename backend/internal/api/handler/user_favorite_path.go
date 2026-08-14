package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func ListFavoritePaths(c *gin.Context) {
	userID := middleware.GetUserID(c)
	data, err := service.ListUserFavoritePaths(userID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, data)
}

func AddFavoritePath(c *gin.Context) {
	var req service.AddFavoritePathReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	data, err := service.AddUserFavoritePath(userID, req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFavoriteAlreadyExists):
			response.Fail(c, http.StatusConflict, 10020, err.Error())
		case errors.Is(err, service.ErrFavoritePathEmpty),
			errors.Is(err, service.ErrFavoriteNameEmpty):
			response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		default:
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}
	response.OK(c, data)
}

func DeleteFavoritePath(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的收藏 ID")
		return
	}
	userID := middleware.GetUserID(c)
	if err := service.DeleteUserFavoritePath(userID, id); err != nil {
		switch {
		case errors.Is(err, service.ErrFavoriteNotFound):
			response.Fail(c, http.StatusNotFound, 10004, err.Error())
		case errors.Is(err, service.ErrFavoriteNotOwned):
			response.Fail(c, http.StatusForbidden, 4003, err.Error())
		default:
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}
	response.OK(c, nil)
}

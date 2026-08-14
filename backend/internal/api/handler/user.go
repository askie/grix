package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetProfile(c *gin.Context) {
	userID := middleware.GetUserID(c)
	user, err := service.GetUserProfile(userID)
	if err != nil {
		response.Fail(c, http.StatusNotFound, 10004, "用户不存在")
		return
	}
	response.OK(c, user)
}

type updateProfileReq struct {
	Nickname     *string `json:"nickname"`
	Introduction *string `json:"introduction"`
	AvatarURL    *string `json:"avatar_url"`
}

func UpdateProfile(c *gin.Context) {
	var req updateProfileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	if req.AvatarURL != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "头像必须通过上传接口设置")
		return
	}
	userID := middleware.GetUserID(c)
	err := service.UpdateUserProfile(
		userID,
		req.Nickname,
		req.AvatarURL,
		req.Introduction,
	)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, nil)
}

func UploadAvatar(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "头像文件不能为空")
		return
	}

	userID := middleware.GetUserID(c)
	avatarURL, err := service.UploadUserAvatar(userID, fileHeader)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAvatarFileRequired):
			response.Fail(c, http.StatusBadRequest, 10003, "头像文件不能为空")
		case errors.Is(err, service.ErrAvatarFileTooLarge):
			response.Fail(c, http.StatusBadRequest, 10003, "头像文件过大（最大10MB）")
		case errors.Is(err, service.ErrAvatarImageInvalid):
			response.Fail(c, http.StatusBadRequest, 10003, "头像文件格式无效")
		default:
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}

	response.OK(c, gin.H{
		"avatar_url": avatarURL,
	})
}

type updateUsernameReq struct {
	Username string `json:"username" binding:"required"`
}

func UpdateUsername(c *gin.Context) {
	var req updateUsernameReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	userID := middleware.GetUserID(c)
	err := service.UpdateUsername(userID, req.Username)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10005, err.Error())
		return
	}
	response.OK(c, nil)
}

func GetUserProfile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid user id")
		return
	}
	requesterID := middleware.GetUserID(c)
	user, err := service.GetPublicProfile(requesterID, id)
	if err != nil {
		// 查无此人（已注销用户、挂件访客等）是解析昵称时的预期情况，
		// 返回 200 空数据而非 404，避免前端反复触发浏览器控制台的 404 噪音。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.OK(c, nil)
			return
		}
		response.Fail(c, http.StatusInternalServerError, 50001, "获取用户资料失败")
		return
	}
	response.OK(c, user)
}

func DeleteAccount(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if err := service.DeleteAccount(userID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Fail(c, http.StatusNotFound, 10004, "用户不存在")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, nil)
}

func GetUserSettings(c *gin.Context) {
	userID := middleware.GetUserID(c)
	data, err := service.GetUserSettings(userID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, data)
}

func UpdateUserSettings(c *gin.Context) {
	var req service.UserSettingsUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.UpdateUserSettings(userID, req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUserSettingsInvalidPayload),
			errors.Is(err, service.ErrUserSettingsInvalidAgentID),
			errors.Is(err, service.ErrUserSettingsInvalidLanguage),
			errors.Is(err, service.ErrUserSettingsInvalidFriendAddMode),
			errors.Is(err, service.ErrUserSettingsAutoAgentNotFound),
			errors.Is(err, service.ErrUserSettingsAutoAgentUnavailable):
			response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		case errors.Is(err, service.ErrUserSettingsAutoAgentNotOwned):
			response.Fail(c, http.StatusForbidden, 4003, err.Error())
		default:
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}
	response.OK(c, data)
}

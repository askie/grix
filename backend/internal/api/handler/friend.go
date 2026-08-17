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

// UserSearch 搜索用户
func UserSearch(c *gin.Context) {
	keyword := c.Query("keyword")
	if keyword == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "keyword is required")
		return
	}
	userID := middleware.GetUserID(c)
	results, err := service.SearchUsers(keyword, userID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, gin.H{"list": results})
}

// --- Friend Request ---

type sendFriendReq struct {
	ToUserID   int64  `json:"to_user_id,string"`
	ToUsername string `json:"to_username"`
	Message    string `json:"message"`
}

// FriendRequestSend 发送好友请求
func FriendRequestSend(c *gin.Context) {
	var req sendFriendReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	targetID := req.ToUserID
	if name := strings.TrimSpace(req.ToUsername); name != "" {
		resolvedID, err := service.ResolveUserIDByUsername(name)
		if err != nil {
			response.Fail(c, http.StatusBadRequest, 10003, err.Error())
			return
		}
		targetID = resolvedID
	}
	if targetID <= 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "to_user_id or to_username is required")
		return
	}

	userID := middleware.GetUserID(c)
	if err := service.SendFriendRequest(userID, targetID, req.Message); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	response.OK(c, nil)
}

// FriendRequestList 获取好友请求列表
func FriendRequestList(c *gin.Context) {
	userID := middleware.GetUserID(c)
	list, err := service.GetFriendRequests(userID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, gin.H{"list": list})
}

type handleFriendReq struct {
	RequestID int64 `json:"request_id,string" binding:"required"`
	Accept    bool  `json:"accept"`
}

// FriendRequestHandle 处理好友请求
func FriendRequestHandle(c *gin.Context) {
	var req handleFriendReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	if err := service.HandleFriendRequest(req.RequestID, userID, req.Accept); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	response.OK(c, nil)
}

// --- Friend List ---

// FriendList 获取好友列表
func FriendList(c *gin.Context) {
	userID := middleware.GetUserID(c)
	list, err := service.GetFriendList(userID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, gin.H{"list": list})
}

type updateFriendRemarkReq struct {
	FriendUserID int64  `json:"friend_user_id,string" binding:"required"`
	RemarkName   string `json:"remark_name"`
}

type blockFriendReq struct {
	BlockedUserID int64 `json:"blocked_user_id,string" binding:"required"`
}

func FriendRemarkUpdate(c *gin.Context) {
	var req updateFriendRemarkReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	userID := middleware.GetUserID(c)
	item, err := service.UpdateFriendRemark(userID, req.FriendUserID, req.RemarkName)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	response.OK(c, item)
}

// FriendDelete 删除好友
func FriendDelete(c *gin.Context) {
	friendID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid friend id")
		return
	}
	userID := middleware.GetUserID(c)
	if err := service.DeleteFriend(userID, friendID); err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, nil)
}

// FriendBlock 拉黑用户
func FriendBlock(c *gin.Context) {
	var req blockFriendReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	userID := middleware.GetUserID(c)
	if err := service.BlockUser(userID, req.BlockedUserID); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	response.OK(c, nil)
}

// FriendQRCodeGet returns current user's add-friend QR code and share URL.
func FriendQRCodeGet(c *gin.Context) {
	userID := middleware.GetUserID(c)
	info, err := service.GetOrCreateFriendQRCode(userID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, info)
}

type setFriendPinReq struct {
	FriendUserID int64 `json:"friend_user_id,string" binding:"required"`
	IsPinned     *bool `json:"is_pinned" binding:"required"`
}

func FriendSetPinned(c *gin.Context) {
	var req setFriendPinReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.FriendSetPinned(userID, req.FriendUserID, *req.IsPinned)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	response.OK(c, data)
}

type setFriendMuteReq struct {
	FriendUserID int64 `json:"friend_user_id,string" binding:"required"`
	IsMuted      *bool `json:"is_muted" binding:"required"`
}

func FriendSetMuted(c *gin.Context) {
	var req setFriendMuteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.FriendSetMuted(userID, req.FriendUserID, *req.IsMuted)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	response.OK(c, data)
}

// FriendQRCodeResolve resolves qr code to target profile and relation state.
func FriendQRCodeResolve(c *gin.Context) {
	userID := middleware.GetUserID(c)
	code := strings.TrimSpace(c.Param("code"))
	if code == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid qr code")
		return
	}

	result, err := service.ResolveFriendQRCode(userID, code)
	if err != nil {
		if service.IsFriendQRCodeNotFound(err) {
			response.Fail(c, http.StatusNotFound, 10004, "二维码无效")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, result)
}

package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

func notifyFriendRequestReceived(req model.FriendRequest) {
	var fromUser model.User
	if err := store.DB.Select("id", "username", "nickname", "avatar_url").First(&fromUser, req.FromUserID).Error; err != nil {
		return
	}
	createdAt := req.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	pushFriendEvent(req.ToUserID, map[string]interface{}{
		"event": "friend_request_received",
		"request": map[string]interface{}{
			"id":           fmt.Sprintf("%d", req.ID),
			"from_user_id": fmt.Sprintf("%d", req.FromUserID),
			"to_user_id":   fmt.Sprintf("%d", req.ToUserID),
			"username":     fromUser.Username,
			"nickname":     fromUser.Nickname,
			"avatar_url":   fromUser.AvatarURL,
			"message":      req.Message,
			"status":       friendRequestStatusPending,
			"created_at":   createdAt.Format(time.RFC3339),
		},
	})
}

func notifyFriendRequestHandled(req model.FriendRequest, status int8) {
	payload := map[string]interface{}{
		"event":        "friend_request_handled",
		"request_id":   fmt.Sprintf("%d", req.ID),
		"from_user_id": fmt.Sprintf("%d", req.FromUserID),
		"to_user_id":   fmt.Sprintf("%d", req.ToUserID),
		"status":       status,
	}
	pushFriendEvent(req.FromUserID, payload)
	pushFriendEvent(req.ToUserID, payload)
}

func notifyFriendAdded(userID, friendID int64) {
	item, ok := buildFriendItem(userID, friendID)
	if !ok {
		return
	}
	pushFriendEvent(userID, map[string]interface{}{
		"event":  "friend_added",
		"friend": item,
	})
}

func notifyFriendRemarkUpdated(userID int64, item *FriendItem) {
	if item == nil {
		return
	}
	pushFriendEvent(userID, map[string]interface{}{
		"event": "friend_remark_updated",
		"friend": map[string]interface{}{
			"id":          fmt.Sprintf("%d", item.ID),
			"user_id":     fmt.Sprintf("%d", item.UserID),
			"username":    item.Username,
			"nickname":    item.Nickname,
			"remark_name": item.RemarkName,
			"avatar_url":  item.AvatarURL,
		},
	})
}

func buildFriendItem(userID, friendID int64) (map[string]interface{}, bool) {
	var rel model.Friend
	if err := store.DB.Where("user_id = ? AND friend_id = ?", userID, friendID).First(&rel).Error; err != nil {
		return nil, false
	}
	var u model.User
	if err := store.DB.Select("id", "username", "nickname", "avatar_url").First(&u, friendID).Error; err != nil {
		return nil, false
	}
	return map[string]interface{}{
		"id":          fmt.Sprintf("%d", rel.ID),
		"user_id":     fmt.Sprintf("%d", u.ID),
		"username":    u.Username,
		"nickname":    resolveFriendDisplayNickname(rel.RemarkName, u.Nickname, u.Username),
		"remark_name": strings.TrimSpace(rel.RemarkName),
		"avatar_url":  u.AvatarURL,
	}, true
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const deletedUserNickname = "Deleted User"

func DeleteAccount(userID int64) error {
	if userID <= 0 {
		return errors.New("invalid user id")
	}

	deviceIDs := make([]string, 0, 4)

	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, userID).Error; err != nil {
			return err
		}
		if user.Status == model.UserStatusDeleted {
			return nil
		}

		if err := tx.Model(&model.Device{}).
			Where("user_id = ?", userID).
			Pluck("device_id", &deviceIDs).Error; err != nil {
			return err
		}

		ownedAgentIDs := make([]int64, 0, 4)
		if err := tx.Model(&model.Agent{}).
			Where("owner_id = ?", userID).
			Pluck("id", &ownedAgentIDs).Error; err != nil {
			return err
		}

		groupSessionIDs := make([]string, 0, 4)
		if err := tx.Model(&model.Session{}).
			Where("owner_id = ? AND session_type = ? AND is_deleted = false", userID, 2).
			Pluck("session_id", &groupSessionIDs).Error; err != nil {
			return err
		}

		now := time.Now().UTC()
		if err := tx.Model(&model.User{}).
			Where("id = ?", userID).
			Updates(map[string]any{
				"username":          fmt.Sprintf("deleted_user_%d", userID),
				"email":             fmt.Sprintf("deleted+%d@grix.invalid", userID),
				"password_hash":     "",
				"username_modified": true,
				"auth_provider":     "",
				"nickname":          deletedUserNickname,
				"avatar_url":        "",
				"status":            model.UserStatusDeleted,
				"banned_reason":     "account_deleted",
				"banned_at":         now,
				"banned_by":         nil,
				"updated_at":        now,
			}).Error; err != nil {
			return err
		}

		if err := tx.Where("user_id = ?", userID).Delete(&model.OAuthAccount{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.RefreshToken{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.UserSetting{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.Device{}).Error; err != nil {
			return err
		}
		if err := tx.Where("from_user_id = ? OR to_user_id = ?", userID, userID).Delete(&model.FriendRequest{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? OR friend_id = ?", userID, userID).Delete(&model.Friend{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.UserInbox{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.SessionHistoryReset{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.LLMUsageLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.DelegationLog{}).Error; err != nil {
			return err
		}

		if len(ownedAgentIDs) > 0 {
			if err := tx.Where("agent_id IN ?", ownedAgentIDs).Delete(&model.KnowledgeDoc{}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.Agent{}).
				Where("id IN ?", ownedAgentIDs).
				Updates(map[string]any{
					"status":       3,
					"avatar_url":   "",
					"api_key_hash": "",
					"api_key_hint": "",
					"updated_at":   now,
				}).Error; err != nil {
				return err
			}
		}

		if len(groupSessionIDs) > 0 {
			if err := tx.Model(&model.Session{}).
				Where("session_id IN ?", groupSessionIDs).
				Updates(map[string]any{
					"is_deleted": true,
					"updated_at": now,
					"group_name": "",
					"owner_id":   userID,
				}).Error; err != nil {
				return err
			}
			if err := tx.Where("session_id IN ?", groupSessionIDs).Delete(&model.SessionMember{}).Error; err != nil {
				return err
			}
		}

		groupMembershipSubQuery := tx.Model(&model.Session{}).
			Select("session_id").
			Where("session_type = ? AND is_deleted = false", 2)
		if err := tx.Where(
			"member_id = ? AND member_type = ? AND session_id IN (?)",
			userID,
			1,
			groupMembershipSubQuery,
		).Delete(&model.SessionMember{}).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	clearDeletedUserRealtimeState(context.Background(), userID, deviceIDs)
	return nil
}

func clearDeletedUserRealtimeState(ctx context.Context, userID int64, deviceIDs []string) {
	if store.RDB == nil || userID <= 0 {
		return
	}

	routeKey := fmt.Sprintf("im:ws:route:%d", userID)
	routes, err := store.RDB.HGetAll(ctx, routeKey).Result()
	if err == nil && len(routes) > 0 {
		payload, marshalErr := json.Marshal(map[string]any{
			"reason": "account_deleted",
		})
		if marshalErr == nil {
			type envelope struct {
				UserID  int64          `json:"user_id"`
				Cmd     string         `json:"cmd"`
				Payload datatypes.JSON `json:"payload"`
			}

			seenNodes := make(map[string]struct{}, len(routes))
			for _, nodeID := range routes {
				nodeID = strings.TrimSpace(nodeID)
				if nodeID == "" {
					continue
				}
				if _, ok := seenNodes[nodeID]; ok {
					continue
				}
				seenNodes[nodeID] = struct{}{}

				raw, rawErr := json.Marshal(envelope{
					UserID:  userID,
					Cmd:     "kicked",
					Payload: datatypes.JSON(payload),
				})
				if rawErr != nil {
					continue
				}
				_ = store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", nodeID), raw).Err()
			}
		}
	}

	_ = store.RDB.Del(ctx, fmt.Sprintf("im:user:devices:%d", userID)).Err()
	_ = store.RDB.Del(ctx, routeKey).Err()
	for _, deviceID := range deviceIDs {
		deviceID = strings.TrimSpace(deviceID)
		if deviceID == "" {
			continue
		}
		_ = store.RDB.Del(ctx, fmt.Sprintf("im:ws:alive:%d:%s", userID, deviceID)).Err()
	}
}

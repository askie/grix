package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
)

func countUnreadAfterMsgID(
	tx *gorm.DB,
	sessionID string,
	userID int64,
	lastReadMsgID int64,
) (int64, error) {
	var remaining int64
	err := tx.Model(&model.Message{}).
		Where("session_id = ? AND msg_id > ? AND is_deleted = false AND is_revoked = false", sessionID, lastReadMsgID).
		Where("NOT (sender_type = ? AND sender_id = ?)", 1, userID).
		Count(&remaining).Error
	return remaining, err
}

func resolveExistingSessionReadBoundary(
	tx *gorm.DB,
	sessionID string,
	requestedLastReadMsgID int64,
) (int64, error) {
	if requestedLastReadMsgID <= 0 {
		return 0, nil
	}

	var row struct {
		MaxMsgID int64
	}
	err := tx.Model(&model.Message{}).
		Select("COALESCE(MAX(msg_id), 0) AS max_msg_id").
		Where("session_id = ? AND msg_id <= ?", sessionID, requestedLastReadMsgID).
		Scan(&row).Error
	if err != nil {
		return 0, err
	}
	return row.MaxMsgID, nil
}

// HandleSessionRead advances server-side read cursor up to the client-confirmed boundary.
// Client should call this when user opens a session or marks it as read.
func HandleSessionRead(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.SessionReadPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("session_read payload error: %v", err)
		conn.SendPayload(protocol.CmdSessionReadAck, pkt.Seq, protocol.SessionReadAckPayload{
			SessionID: "",
			Code:      4001,
			Msg:       "invalid session_read payload",
		})
		return
	}
	if payload.SessionID == "" {
		conn.SendPayload(protocol.CmdSessionReadAck, pkt.Seq, protocol.SessionReadAckPayload{
			SessionID: payload.SessionID,
			Code:      4001,
			Msg:       "invalid session_id",
		})
		return
	}
	if payload.LastReadMsgID < 0 {
		conn.SendPayload(protocol.CmdSessionReadAck, pkt.Seq, protocol.SessionReadAckPayload{
			SessionID: payload.SessionID,
			Code:      4001,
			Msg:       "invalid last_read_msg_id",
		})
		return
	}

	userID := conn.GetUserID()
	var member model.SessionMember
	if err := store.DB.
		Where("session_id = ? AND member_id = ? AND member_type = 1", payload.SessionID, userID).
		First(&member).Error; err != nil {
		conn.SendPayload(protocol.CmdSessionReadAck, pkt.Seq, protocol.SessionReadAckPayload{
			SessionID:     payload.SessionID,
			Code:          4003,
			Msg:           "permission denied",
			LastReadMsgID: member.LastReadMsgID,
		})
		return
	}

	sessionType := loadSessionType(payload.SessionID)

	targetLastReadMsgID, err := resolveExistingSessionReadBoundary(
		store.DB,
		payload.SessionID,
		payload.LastReadMsgID,
	)
	if err != nil {
		conn.SendPayload(protocol.CmdSessionReadAck, pkt.Seq, protocol.SessionReadAckPayload{
			SessionID: payload.SessionID,
			Code:      5001,
			Msg:       "resolve read boundary failed",
		})
		return
	}
	if targetLastReadMsgID < member.LastReadMsgID {
		targetLastReadMsgID = member.LastReadMsgID
	}

	if targetLastReadMsgID == member.LastReadMsgID && member.UnreadCount == 0 {
		conn.SendPayload(protocol.CmdSessionReadAck, pkt.Seq, protocol.SessionReadAckPayload{
			SessionID:     payload.SessionID,
			Code:          0,
			LastReadMsgID: member.LastReadMsgID,
		})
		return
	}

	ctx := context.Background()
	recentKey := fmt.Sprintf(
		"im:session_read:recent:%d:%s:%d",
		userID,
		payload.SessionID,
		targetLastReadMsgID,
	)
	if exists, err := store.RDB.Exists(ctx, recentKey).Result(); err == nil && exists > 0 {
		conn.SendPayload(protocol.CmdSessionReadAck, pkt.Seq, protocol.SessionReadAckPayload{
			SessionID:     payload.SessionID,
			Code:          0,
			LastReadMsgID: targetLastReadMsgID,
		})
		return
	}

	now := time.Now().UTC()
	var remainingUnread int64
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		remainingUnread, err = countUnreadAfterMsgID(
			tx,
			payload.SessionID,
			userID,
			targetLastReadMsgID,
		)
		if err != nil {
			return err
		}

		return tx.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 1", payload.SessionID, userID).
			Updates(map[string]interface{}{
				"unread_count":     remainingUnread,
				"last_read_msg_id": targetLastReadMsgID,
				"last_active_at":   now,
			}).Error
	}); err != nil {
		conn.SendPayload(protocol.CmdSessionReadAck, pkt.Seq, protocol.SessionReadAckPayload{
			SessionID: payload.SessionID,
			Code:      5001,
			Msg:       "update read state failed",
		})
		return
	}

	unreadKey := fmt.Sprintf("im:unread:%d", userID)
	if remainingUnread > 0 {
		store.RDB.HSet(ctx, unreadKey, payload.SessionID, remainingUnread)
	} else {
		store.RDB.HDel(ctx, unreadKey, payload.SessionID)
	}
	if err := store.RDB.Set(ctx, recentKey, 1, 2*time.Second).Err(); err != nil {
		logger.L.Warnf("set session_read recent key error: %v", err)
	}

	// Phase 3.2: 通知 reader 自己（含当前设备）多端未读数同步,
	// 走 session_read_sync(unread_count) 而非独立的 unread_sync。
	if hub != nil {
		uc := remainingUnread
		readerSync := protocol.SessionReadSyncPayload{
			SessionID:     payload.SessionID,
			ReaderID:      userID,
			LastReadMsgID: targetLastReadMsgID,
			UnreadCount:   &uc,
			UpdatedAt:     now.UnixMilli(),
		}
		broadcastToUser(hub, ctx, userID, protocol.CmdSessionReadSync, readerSync)
	}

	if hub != nil && sessionType == 2 && targetLastReadMsgID > 0 {
		var members []model.SessionMember
		if err := store.DB.
			Select("member_id", "member_type").
			Where("session_id = ? AND member_type = 1", payload.SessionID).
			Find(&members).Error; err != nil {
			logger.L.Warnf("load session members for session_read_sync failed session=%s: %v", payload.SessionID, err)
		} else {
			groupReadSync := protocol.SessionReadSyncPayload{
				SessionID:     payload.SessionID,
				ReaderID:      userID,
				LastReadMsgID: targetLastReadMsgID,
				UpdatedAt:     now.UnixMilli(),
			}
			for _, groupMember := range members {
				if groupMember.MemberID == userID {
					// reader 已经在上面收到带 UnreadCount 的版本。
					continue
				}
				broadcastToUser(hub, ctx, groupMember.MemberID, protocol.CmdSessionReadSync, groupReadSync)
			}
		}
	}

	conn.SendPayload(protocol.CmdSessionReadAck, pkt.Seq, protocol.SessionReadAckPayload{
		SessionID:     payload.SessionID,
		Code:          0,
		LastReadMsgID: targetLastReadMsgID,
	})
}

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/syncstream"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func countUnreadAfterMsgID(
	tx *gorm.DB,
	sessionID string,
	userID int64,
	lastReadMsgID int64,
) (int64, error) {
	query := tx.Model(&model.Message{}).
		Where("session_id = ? AND msg_id > ? AND is_deleted = false AND is_revoked = false", sessionID, lastReadMsgID).
		Where("NOT (sender_type = ? AND sender_id = ?)", 1, userID).
		// 排除 msg_type=4 流式占位消息：占位消息无 inbox 行、不进 HTTP 历史，
		// 客户端任何界面都看不到它们，不应计入剩余未读（与历史可见性规则一致）。
		Where("msg_type <> ?", model.MsgTypeAIStream)
	// Match history / conversation-list visibility: hidden messages the reader
	// cannot see must not inflate remaining unread after session_read.
	if store.IsPostgres() {
		query = query.Where(
			"visible_to IS NULL OR sender_id = ? OR visible_to @> to_jsonb(?::bigint)",
			userID, userID,
		)
		var remaining int64
		err := query.Count(&remaining).Error
		return remaining, err
	}

	// SQLite tests lack jsonb @>; filter visible_to in process.
	var rows []struct {
		SenderID  int64          `gorm:"column:sender_id"`
		VisibleTo datatypes.JSON `gorm:"column:visible_to"`
	}
	if err := query.Select("sender_id", "visible_to").Find(&rows).Error; err != nil {
		return 0, err
	}
	var remaining int64
	for _, row := range rows {
		if messageVisibleToUser(row.VisibleTo, row.SenderID, userID) {
			remaining++
		}
	}
	return remaining, nil
}

// messageVisibleToUser reports whether a stored visible_to payload includes userID
// (or is unrestricted / authored by userID). Used by the SQLite session_read path.
func messageVisibleToUser(visibleTo datatypes.JSON, senderID, userID int64) bool {
	if len(visibleTo) == 0 {
		return true
	}
	var ids []int64
	if err := json.Unmarshal(visibleTo, &ids); err != nil || len(ids) == 0 {
		return true
	}
	if senderID == userID {
		return true
	}
	for _, id := range ids {
		if id == userID {
			return true
		}
	}
	return false
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

	if payload.CommandID == "" && targetLastReadMsgID == member.LastReadMsgID && member.UnreadCount == 0 {
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
	if payload.CommandID == "" {
		if exists, err := store.RDB.Exists(ctx, recentKey).Result(); err == nil && exists > 0 {
			conn.SendPayload(protocol.CmdSessionReadAck, pkt.Seq, protocol.SessionReadAckPayload{
				SessionID:     payload.SessionID,
				Code:          0,
				LastReadMsgID: targetLastReadMsgID,
			})
			return
		}
	}

	now := time.Now().UTC()
	var remainingUnread int64
	duplicateCommand := false
	noStateChange := false
	repairedStaleUnread := false
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if payload.CommandID != "" {
			claimed, err := syncstream.ClaimCommandTx(tx, userID, "session.read", payload.CommandID, map[string]any{"session_id": payload.SessionID, "last_read_msg_id": targetLastReadMsgID})
			if err != nil {
				return err
			}
			if !claimed {
				// 重复命令不再直接短路：首次执行时的 recount 可能受当时正在
				// finalize 的流式消息影响而失真，这里在校成员行锁后重新核对
				// 存储的未读数，给已腐化的状态一个自愈机会。游标保持单调，
				// 不会回退。
				duplicateCommand = true
			}
		}
		var lockedMember model.SessionMember
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ? AND member_id = ? AND member_type = 1", payload.SessionID, userID).
			First(&lockedMember).Error; err != nil {
			return err
		}
		if targetLastReadMsgID < lockedMember.LastReadMsgID {
			targetLastReadMsgID = lockedMember.LastReadMsgID
		}
		if targetLastReadMsgID == lockedMember.LastReadMsgID && lockedMember.UnreadCount == 0 && !duplicateCommand {
			noStateChange = true
			if payload.CommandID == "" {
				return nil
			}
			readState := map[string]any{
				"session_id": payload.SessionID, "reader_id": userID,
				"last_read_msg_id": lockedMember.LastReadMsgID,
				"unread_count":     lockedMember.UnreadCount,
				"state_version":    lockedMember.StateVersion,
				"updated_at":       now.UnixMilli(),
			}
			_, err := syncstream.AppendTx(tx, []syncstream.Event{{
				UserID: userID, Kind: "session.read_state", EntityType: "session_member",
				EntityID: payload.SessionID, EntityVersion: lockedMember.StateVersion,
				CommandID: payload.CommandID, Payload: readState,
			}})
			return err
		}
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
		if duplicateCommand && remainingUnread == int64(lockedMember.UnreadCount) {
			// 存储的未读数与重算一致，重复命令保持纯 no-op（不写行、不追加事件）。
			return nil
		}
		if duplicateCommand {
			repairedStaleUnread = true
		}

		if err := tx.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 1", payload.SessionID, userID).
			Updates(map[string]interface{}{
				"unread_count":     remainingUnread,
				"last_read_msg_id": targetLastReadMsgID,
				"last_active_at":   now,
				"state_version":    gorm.Expr("state_version + 1"),
			}).Error; err != nil {
			return err
		}
		var current model.SessionMember
		if err := tx.Where("session_id = ? AND member_id = ? AND member_type = 1", payload.SessionID, userID).First(&current).Error; err != nil {
			return err
		}
		readState := map[string]any{"session_id": payload.SessionID, "reader_id": userID, "last_read_msg_id": current.LastReadMsgID, "unread_count": current.UnreadCount, "state_version": current.StateVersion, "updated_at": now.UnixMilli()}
		events := []syncstream.Event{
			{UserID: userID, Kind: "session.read_state", EntityType: "session_member", EntityID: payload.SessionID, EntityVersion: current.StateVersion, CommandID: payload.CommandID, Payload: readState},
			{UserID: userID, Kind: "session.unread_set", EntityType: "session_member", EntityID: payload.SessionID, EntityVersion: current.StateVersion, CommandID: payload.CommandID, Payload: readState},
		}
		if sessionType == model.SessionTypeGroup {
			peerReadState := map[string]any{
				"session_id": payload.SessionID, "reader_id": userID,
				"last_read_msg_id": current.LastReadMsgID,
				"state_version":    current.StateVersion,
				"updated_at":       now.UnixMilli(),
			}
			var peers []model.SessionMember
			if err := tx.Select("member_id").Where("session_id = ? AND member_type = 1 AND member_id <> ?", payload.SessionID, userID).Find(&peers).Error; err != nil {
				return err
			}
			for _, peer := range peers {
				events = append(events, syncstream.Event{UserID: peer.MemberID, Kind: "session.read_state", EntityType: "session_member", EntityID: payload.SessionID + ":" + fmt.Sprintf("%d", userID), EntityVersion: current.StateVersion, CommandID: payload.CommandID, Payload: peerReadState})
			}
		}
		_, err = syncstream.AppendTx(tx, events)
		return err
	}); err != nil {
		conn.SendPayload(protocol.CmdSessionReadAck, pkt.Seq, protocol.SessionReadAckPayload{
			SessionID: payload.SessionID,
			Code:      5001,
			Msg:       "update read state failed",
		})
		return
	}
	if noStateChange || (duplicateCommand && !repairedStaleUnread) {
		conn.SendPayload(protocol.CmdSessionReadAck, pkt.Seq, protocol.SessionReadAckPayload{SessionID: payload.SessionID, Code: 0, LastReadMsgID: targetLastReadMsgID})
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

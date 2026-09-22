package handler

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/syncstream"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func HandleSessionHistoryReset(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.SessionHistoryResetPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("session_history_reset payload error: %v", err)
		conn.SendPayload(protocol.CmdSessionHistoryResetAck, pkt.Seq, protocol.SessionHistoryResetAckPayload{
			SessionID: payload.SessionID,
			Code:      4001,
			Msg:       "invalid payload",
		})
		return
	}

	userID := conn.GetUserID()
	sessionID := payload.SessionID
	if sessionID == "" || userID <= 0 {
		conn.SendPayload(protocol.CmdSessionHistoryResetAck, pkt.Seq, protocol.SessionHistoryResetAckPayload{
			SessionID: sessionID,
			Code:      4001,
			Msg:       "invalid payload",
		})
		return
	}

	var member model.SessionMember
	if err := store.DB.Select("session_id").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		First(&member).Error; err != nil {
		code := 5001
		msg := "permission denied"
		if errors.Is(err, gorm.ErrRecordNotFound) {
			code = 4003
			msg = "permission denied"
		}
		conn.SendPayload(protocol.CmdSessionHistoryResetAck, pkt.Seq, protocol.SessionHistoryResetAckPayload{
			SessionID: sessionID,
			Code:      code,
			Msg:       msg,
		})
		return
	}

	now := time.Now()
	deletedAtMs := payload.DeletedAt
	if deletedAtMs <= 0 {
		deletedAtMs = now.UnixMilli()
	}
	// Never trust future client clocks for cutoff.
	if deletedAtMs > now.UnixMilli() {
		deletedAtMs = now.UnixMilli()
	}
	deletedBefore := time.UnixMilli(deletedAtMs)

	changed := false
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		claimed, err := syncstream.ClaimCommandTx(tx, userID, "session.history_reset", payload.CommandID, map[string]any{"session_id": sessionID, "deleted_at": deletedAtMs})
		if err != nil {
			return err
		}
		if !claimed {
			return nil
		}
		row := model.SessionHistoryReset{
			SessionID:     sessionID,
			UserID:        userID,
			DeletedBefore: deletedBefore,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		result := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "session_id"}, {Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"deleted_before": deletedBefore.UTC(),
				"updated_at":     now.UTC(),
				"state_version":  gorm.Expr("session_history_resets.state_version + 1"),
			}),
			Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "session_history_resets.deleted_before < EXCLUDED.deleted_before"}}},
		}).Create(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		changed = true
		if err := tx.Model(&model.Session{}).Where("session_id = ?", sessionID).
			Updates(map[string]any{"updated_at": now.UTC(), "state_version": gorm.Expr("state_version + 1")}).Error; err != nil {
			return err
		}
		var currentSession model.Session
		if err := tx.First(&currentSession, "session_id = ?", sessionID).Error; err != nil {
			return err
		}
		var currentReset model.SessionHistoryReset
		if err := tx.First(&currentReset, "session_id = ? AND user_id = ?", sessionID, userID).Error; err != nil {
			return err
		}
		_, err = syncstream.AppendTx(tx, []syncstream.Event{{UserID: userID, Kind: "session.remove", EntityType: "session", EntityID: sessionID, EntityVersion: currentSession.StateVersion, Tombstone: true, CommandID: payload.CommandID, Payload: map[string]any{"session": currentSession, "reason": "history_reset", "deleted_at": currentReset.DeletedBefore.UnixMilli(), "state_version": currentReset.StateVersion}}})
		return err
	})
	if err != nil {
		logger.L.Warnf("session_history_reset save error user=%d session=%s: %v", userID, sessionID, err)
		conn.SendPayload(protocol.CmdSessionHistoryResetAck, pkt.Seq, protocol.SessionHistoryResetAckPayload{SessionID: sessionID, Code: 5001, Msg: "save failed"})
		return
	}

	conn.SendPayload(protocol.CmdSessionHistoryResetAck, pkt.Seq, protocol.SessionHistoryResetAckPayload{
		SessionID: sessionID,
		Code:      0,
	})

	if changed {
		syncstream.NotifyDirty(userID)
	}
	if changed && hub != nil {
		broadcastToUserExceptDevice(hub, context.Background(), userID, conn.GetDeviceID(), protocol.CmdSessionHistoryResetSync, protocol.SessionHistoryResetPayload{
			SessionID: sessionID,
			DeletedAt: deletedAtMs,
		})

		// If this session belongs to an active widget visitor, close the widget
		// session so the visitor gets a fresh session on next init instead of
		// reusing the old one (which would clear this history-reset marker).
		// Also stops any in-progress AI agent streaming to prevent push_msg
		// from resurrecting the deleted session on the owner's frontend.
		closeVisitorSessionOnHistoryReset(sessionID, userID, hub)
	}
}

// closeVisitorSessionOnHistoryReset closes an active widget session associated
// with the given IM session, stops any in-progress AI agent streaming, and
// notifies the visitor. When the owner deletes the conversation history with a
// visitor, the widget session must be closed; otherwise WidgetVisitorInit
// reuses the active session and clears the owner's history-reset marker.
// Additionally, any active agent output is stopped to prevent push_msg from
// resurrecting the deleted session on the owner's frontend.
func closeVisitorSessionOnHistoryReset(sessionID string, ownerID int64, hub HubInterface) {
	var ws model.WidgetSession
	if err := store.DB.Where("session_id = ? AND status = ?", sessionID, model.WidgetSessionStatusActive).
		First(&ws).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.L.Warnf("closeVisitorSessionOnHistoryReset: query error session=%s: %v", sessionID, err)
		}
		return
	}
	now := time.Now().UTC()
	if err := store.DB.Model(&model.WidgetSession{}).Where("id = ?", ws.ID).Updates(map[string]interface{}{
		"status":     model.WidgetSessionStatusClosed,
		"updated_at": now,
	}).Error; err != nil {
		logger.L.Warnf("closeVisitorSessionOnHistoryReset: update error session=%s: %v", sessionID, err)
		return
	}
	notifyWidgetSessionClosed(hub, ws.VisitorID, sessionID, "closed")

	// Stop any in-progress AI agent streaming for this session so that
	// pending push_msg events don't resurrect the deleted conversation.
	stopAgentStreamOnVisitorSessionClose(sessionID, ownerID)
}

// stopAgentStreamOnVisitorSessionClose requests an output stop for any active
// agent run in the given session and applies local fence+revoke effects.
func stopAgentStreamOnVisitorSessionClose(sessionID string, ownerID int64) {
	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		return
	}
	ack, run, err := mgr.RequestOutputStop(ownerID, sessionID, "")
	if err != nil {
		return
	}
	if run != nil && run.StreamMsgID > 0 {
		ApplyAgentOutputStopLocalEffects(context.Background(), run)
	}
	if run != nil {
		_ = mgr.DispatchOutputStop(ack, run)
	}
	logger.L.Infof(
		"stopAgentStreamOnVisitorSessionClose: owner=%d session=%s accepted=%t stream_msg_id=%d",
		ownerID, sessionID, ack.Accepted, func() int64 {
			if run == nil {
				return 0
			}
			return run.StreamMsgID
		}(),
	)
}

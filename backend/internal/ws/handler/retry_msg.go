package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/askie/grix/backend/internal/ws/threadmeta"
)

func sendRetryMsgAck(
	conn ConnInterface,
	pkt *protocol.Packet,
	sessionID string,
	msgID int64,
	code int,
	msg string,
) {
	conn.SendPayload(protocol.CmdRetryMsgAck, pkt.Seq, protocol.RetryMsgAckPayload{
		SessionID: sessionID,
		MsgID:     msgID,
		Code:      code,
		Msg:       msg,
	})
}

func HandleRetryMsg(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.RetryMsgPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("retry_msg payload error: %v", err)
		sendRetryMsgAck(conn, pkt, "", 0, 4001, "invalid retry_msg payload")
		return
	}
	if payload.SessionID == "" || payload.MsgID <= 0 {
		logger.L.Warnf(
			"retry_msg invalid payload: user=%d session_id=%q msg_id=%d",
			conn.GetUserID(),
			payload.SessionID,
			payload.MsgID,
		)
		sendRetryMsgAck(
			conn,
			pkt,
			payload.SessionID,
			payload.MsgID,
			4001,
			"invalid retry_msg payload",
		)
		return
	}

	ctx := context.Background()

	var member model.SessionMember
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ?", payload.SessionID, conn.GetUserID()).
		First(&member).Error; err != nil {
		logger.L.Warnf(
			"retry_msg permission denied: user=%d session=%s msg_id=%d",
			conn.GetUserID(),
			payload.SessionID,
			payload.MsgID,
		)
		sendRetryMsgAck(conn, pkt, payload.SessionID, payload.MsgID, 4003, "permission denied")
		return
	}
	if err := validatePrivateHumanSendPermission(
		payload.SessionID,
		conn.GetUserID(),
		member.MemberType,
		0,
	); err != nil {
		if errors.Is(err, errPrivatePeerNotFriend) || errors.Is(err, errPrivatePeerBlocked) {
			logger.L.Warnf(
				"retry_msg rejected: sender=%d session=%s msg_id=%d reason=%v",
				conn.GetUserID(),
				payload.SessionID,
				payload.MsgID,
				err,
			)
			sendRetryMsgAck(conn, pkt, payload.SessionID, payload.MsgID, 4003, err.Error())
			return
		}
		logger.L.Errorf(
			"retry_msg private friend guard failed user=%d session=%s msg_id=%d: %v",
			conn.GetUserID(),
			payload.SessionID,
			payload.MsgID,
			err,
		)
		sendRetryMsgAck(conn, pkt, payload.SessionID, payload.MsgID, 5001, "retry message failed")
		return
	}
	if err := sessionguard.ValidateSpeakPermission(
		ctx,
		nil,
		payload.SessionID,
		member.MemberID,
		member.MemberType,
	); err != nil {
		logger.L.Warnf(
			"retry_msg speaking denied: user=%d session=%s msg_id=%d err=%v",
			conn.GetUserID(),
			payload.SessionID,
			payload.MsgID,
			err,
		)
		if !sessionguard.IsDeniedError(err) {
			sendRetryMsgAck(conn, pkt, payload.SessionID, payload.MsgID, 5001, "retry message failed")
			return
		}
		sendRetryMsgAck(
			conn,
			pkt,
			payload.SessionID,
			payload.MsgID,
			4003,
			sessionguard.ErrorMessage(err),
		)
		return
	}

	var msg model.Message
	if err := store.DB.
		Where(
			"session_id = ? AND msg_id = ? AND is_deleted = false AND is_revoked = false",
			payload.SessionID,
			payload.MsgID,
		).
		First(&msg).Error; err != nil {
		logger.L.Warnf(
			"retry_msg message not found: user=%d session=%s msg_id=%d err=%v",
			conn.GetUserID(),
			payload.SessionID,
			payload.MsgID,
			err,
		)
		sendRetryMsgAck(conn, pkt, payload.SessionID, payload.MsgID, 4004, "message not found")
		return
	}
	if msg.SenderID != conn.GetUserID() || msg.SenderType != member.MemberType {
		logger.L.Warnf(
			"retry_msg sender mismatch: user=%d session=%s msg_id=%d sender=%d sender_type=%d member_type=%d",
			conn.GetUserID(),
			payload.SessionID,
			payload.MsgID,
			msg.SenderID,
			msg.SenderType,
			member.MemberType,
		)
		sendRetryMsgAck(conn, pkt, payload.SessionID, payload.MsgID, 4003, "permission denied")
		return
	}
	if msg.MsgType != 1 || strings.TrimSpace(msg.Content) == "" {
		sendRetryMsgAck(
			conn,
			pkt,
			payload.SessionID,
			payload.MsgID,
			4004,
			"message is not retryable",
		)
		return
	}

	sessionType := loadSessionType(payload.SessionID)

	extraRaw := threadmeta.Merge(json.RawMessage(msg.Extra), msg.ThreadID)

	var retryVisibleTo []int64
	if msg.VisibleTo != nil {
		_ = json.Unmarshal(msg.VisibleTo, &retryVisibleTo)
	}

	directTriggered := false
	if msg.SenderType != 2 {
		route, err := resolveDirectSessionRoute(
			payload.SessionID,
			sessionType,
			msg.SenderID,
			msg.SenderType,
			msg.MsgID,
			msg.QuotedMessageID,
			msg.MsgType,
			msg.Content,
			extraRaw,
			nil,
			retryVisibleTo,
			nil,
			false,
		)
		if err != nil {
			logger.L.Warnf(
				"retry_msg resolve direct session route failed session=%s msg_id=%d: %v",
				payload.SessionID,
				payload.MsgID,
				err,
			)
		} else if route != nil {
			directTriggered = true
			dispatchDirectSessionRoute(
				hub,
				ctx,
				payload.SessionID,
				sessionType,
				msg.SenderID,
				msg.SenderType,
				msg.MsgID,
				msg.QuotedMessageID,
				msg.MsgType,
				msg.Content,
				extraRaw,
				route,
			)
		}
	}

	delegateTriggered := TriggerDelegatesForMessage(
		hub,
		ctx,
		payload.SessionID,
		msg.SenderID,
		msg.SenderType,
		msg.MsgID,
		msg.QuotedMessageID,
		msg.MsgType,
		msg.Content,
		extraRaw,
		retryVisibleTo,
	)
	if !directTriggered && !delegateTriggered {
		sendRetryMsgAck(
			conn,
			pkt,
			payload.SessionID,
			payload.MsgID,
			4004,
			"message is not retryable",
		)
		return
	}

	sendRetryMsgAck(conn, pkt, payload.SessionID, payload.MsgID, 0, "retry accepted")
}

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const localStreamTimeout = 10 * time.Minute

// ---------------------------------------------------------------------------
// Streaming local inference: start -> chunk(N) -> finish
// ---------------------------------------------------------------------------

type localStreamEntry struct {
	ss             *agentmsg.StreamSession
	hub            HubInterface
	originUserID   int64
	originDeviceID string
	sessionID      string
	agentID        int64
	triggerMsgID   int64
	members        []model.SessionMember // cached human members
	timer          *time.Timer
	activity       protocol.SessionActivityPayload
	composingShown bool
}

var localStreamRegistry sync.Map // key: int64(msgID) -> *localStreamEntry

// broadcastToSessionExceptDevice sends cmd+payload to all human members of a session,
// skipping originDeviceID for originUserID.
func broadcastToSessionExceptDevice(
	hub HubInterface,
	ctx context.Context,
	members []model.SessionMember,
	originUserID int64,
	originDeviceID string,
	cmd string,
	payload any,
) {
	for _, m := range members {
		if m.MemberType != 1 {
			continue
		}
		if m.MemberID == originUserID {
			broadcastToUserExceptDevice(hub, ctx, m.MemberID, originDeviceID, cmd, payload)
		} else {
			conns := hub.GetUserConns(m.MemberID)
			for _, c := range conns {
				c.SendPayload(cmd, c.NextSeq(), payload)
			}
			if len(conns) == 0 {
				dispatchCrossNode(ctx, m.MemberID, cmd, payload)
			}
		}
	}
}

// HandleRelayLocalStreamStart begins a streaming local inference session.
func HandleRelayLocalStreamStart(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.RelayLocalStreamStartPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("relay_local_stream_start payload error: %v", err)
		return
	}
	if payload.SessionID == "" || payload.AgentID <= 0 {
		sendRelayLocalStreamStartAck(conn, pkt.Seq, payload, 400, 0, "invalid payload")
		return
	}

	ctx := context.Background()

	if err := validateHumanSpeakTrigger(ctx, payload.SessionID, conn.GetUserID()); err != nil {
		code := 403
		msg := sessionguard.ErrorMessage(err)
		if !sessionguard.IsDeniedError(err) {
			code = 500
			msg = "internal error"
			logger.L.Errorf(
				"relay_local_stream_start: requester speaking validation failed user=%d session=%s err=%v",
				conn.GetUserID(),
				payload.SessionID,
				err,
			)
		}
		sendRelayLocalStreamStartAck(conn, pkt.Seq, payload, code, 0, msg)
		return
	}
	if err := validateLocalInferenceTarget(
		ctx,
		payload.SessionID,
		conn.GetUserID(),
		payload.AgentID,
		payload.TriggerMsgID,
	); err != nil {
		code := 403
		msg := err.Error()
		if !errors.Is(err, errLocalInferenceAgentNotInSession) &&
			!errors.Is(err, errLocalInferenceAgentUnavailable) &&
			!errors.Is(err, errLocalInferenceTriggerInvalid) {
			code = 500
			msg = "internal error"
			logger.L.Errorf(
				"relay_local_stream_start: local inference target validation failed user=%d session=%s agent=%d trigger=%d err=%v",
				conn.GetUserID(),
				payload.SessionID,
				payload.AgentID,
				payload.TriggerMsgID,
				err,
			)
		}
		sendRelayLocalStreamStartAck(conn, pkt.Seq, payload, code, 0, msg)
		return
	}

	existingState, err := findLocalStreamStartState(ctx, payload)
	if err != nil {
		logger.L.Errorf(
			"relay_local_stream_start: load existing start state failed user=%d session=%s agent=%d trigger=%d err=%v",
			conn.GetUserID(),
			payload.SessionID,
			payload.AgentID,
			payload.TriggerMsgID,
			err,
		)
		sendRelayLocalStreamStartAck(conn, pkt.Seq, payload, 500, 0, "internal error")
		return
	}
	if existingState.MsgID > 0 {
		sendRelayLocalStreamStartAck(conn, pkt.Seq, payload, 200, existingState.MsgID, "")
		return
	}

	activeKey := localStreamActiveKey(payload.SessionID)
	if store.RDB != nil {
		ok, err := store.RDB.SetNX(ctx, activeKey, 1, localStreamTimeout).Result()
		if err != nil {
			logger.L.Errorf(
				"relay_local_stream_start: acquire active lease failed user=%d session=%s agent=%d trigger=%d err=%v",
				conn.GetUserID(),
				payload.SessionID,
				payload.AgentID,
				payload.TriggerMsgID,
				err,
			)
			sendRelayLocalStreamStartAck(conn, pkt.Seq, payload, 500, 0, "internal error")
			return
		}
		if !ok {
			sendRelayLocalStreamStartAck(conn, pkt.Seq, payload, 409, 0, "stream already active")
			return
		}
	}

	ss, err := agentmsg.NewStreamSession(agentmsg.StreamSessionConfig{
		Ctx:       ctx,
		SessionID: payload.SessionID,
		Identity: &agentmsg.SenderIdentity{
			SenderID:   payload.AgentID,
			SenderType: 2,
		},
		QuotedMessageID: payload.TriggerMsgID,
		BuilderTTL:      localStreamTimeout,
	})
	if err != nil {
		if store.RDB != nil {
			store.RDB.Del(ctx, activeKey)
		}
		if errors.Is(err, sessionguard.ErrSpeakForbidden) ||
			errors.Is(err, sessionguard.ErrGroupAllMembersMuted) ||
			errors.Is(err, sessionguard.ErrMemberSpeakMuted) {
			sendRelayLocalStreamStartAck(conn, pkt.Seq, payload, 403, 0, sessionguard.ErrorMessage(err))
			return
		}
		logger.L.Errorf("relay_local_stream_start: NewStreamSession failed user=%d session=%s: %v",
			conn.GetUserID(), payload.SessionID, err)
		sendRelayLocalStreamStartAck(conn, pkt.Seq, payload, 500, 0, "internal error")
		return
	}

	var members []model.SessionMember
	store.DB.Where("session_id = ? AND member_type = 1", payload.SessionID).Find(&members)

	msgID := ss.MsgID()
	entry := &localStreamEntry{
		ss:             ss,
		hub:            hub,
		originUserID:   conn.GetUserID(),
		originDeviceID: conn.GetDeviceID(),
		sessionID:      payload.SessionID,
		agentID:        payload.AgentID,
		triggerMsgID:   payload.TriggerMsgID,
		members:        members,
		activity:       buildLocalStreamActivity(payload.SessionID, payload.AgentID, payload.TriggerMsgID, msgID),
	}
	entry.timer = time.AfterFunc(localStreamTimeout, func() {
		handleLocalStreamTimeout(entry)
	})
	localStreamRegistry.Store(msgID, entry)
	if err := entry.showComposing(ctx); err != nil {
		logger.L.Warnf("relay_local_stream_start: set composing failed msg_id=%d session=%s: %v", msgID, payload.SessionID, err)
	}
	if err := storeLocalStreamStartState(ctx, payload, msgID); err != nil {
		localStreamRegistry.Delete(msgID)
		entry.timer.Stop()
		entry.clearComposing(ctx, "start_state_store_failed")
		ss.Abort()
		if store.RDB != nil {
			store.RDB.Del(ctx, activeKey)
		}
		logger.L.Errorf(
			"relay_local_stream_start: persist request state failed session=%s agent=%d trigger=%d msg_id=%d err=%v",
			payload.SessionID,
			payload.AgentID,
			payload.TriggerMsgID,
			msgID,
			err,
		)
		sendRelayLocalStreamStartAck(conn, pkt.Seq, payload, 500, 0, "internal error")
		return
	}
	if err := agentreceive.ClearBuffer(ctx, payload.SessionID, 2, payload.AgentID); err != nil {
		logger.L.Warnf(
			"relay_local_stream_start: clear receive buffer failed session=%s agent=%d trigger=%d: %v",
			payload.SessionID,
			payload.AgentID,
			payload.TriggerMsgID,
			err,
		)
	}

	sendRelayLocalStreamStartAck(conn, pkt.Seq, payload, 200, msgID, "")
}

// HandleRelayLocalStreamChunk appends a chunk from local LLM inference and broadcasts to other devices.
func HandleRelayLocalStreamChunk(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.RelayLocalStreamChunkPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("relay_local_stream_chunk payload error: %v", err)
		return
	}
	if payload.MsgID == 0 || payload.DeltaContent == "" {
		return
	}

	if handleRelayLocalStreamChunkLocal(conn.GetUserID(), payload) {
		return
	}
	logger.L.Debugf("relay_local_stream_chunk: unknown msg_id=%d", payload.MsgID)
}

// HandleRelayLocalStreamFinish finalizes a streaming local inference session.
func HandleRelayLocalStreamFinish(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.RelayLocalStreamFinishPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("relay_local_stream_finish payload error: %v", err)
		return
	}
	if payload.MsgID == 0 {
		return
	}

	if handleRelayLocalStreamFinishLocal(conn.GetUserID(), payload) {
		return
	}
	logger.L.Warnf("relay_local_stream_finish: unknown msg_id=%d", payload.MsgID)
}

func (e *localStreamEntry) showComposing(ctx context.Context) error {
	if e == nil || e.composingShown {
		return nil
	}
	if err := UpsertSessionActivity(ctx, e.hub, e.activity); err != nil {
		return err
	}
	e.composingShown = true
	return nil
}

func (e *localStreamEntry) clearComposing(ctx context.Context, reason string) {
	if e == nil || !e.composingShown {
		return
	}
	if err := ClearSessionActivity(ctx, e.hub, e.activity); err != nil {
		logger.L.Warnf("relay_local_stream: clear composing failed msg_id=%d session=%s reason=%s: %v", e.ss.MsgID(), e.sessionID, reason, err)
		return
	}
	e.composingShown = false
}

func sendRelayLocalStreamStartAck(
	conn ConnInterface,
	seq int64,
	payload protocol.RelayLocalStreamStartPayload,
	code int,
	msgID int64,
	msg string,
) {
	conn.SendPayload(protocol.CmdRelayLocalStreamStartAck, seq, protocol.RelayLocalStreamStartAckPayload{
		Code:         code,
		SessionID:    payload.SessionID,
		AgentID:      payload.AgentID,
		TriggerMsgID: payload.TriggerMsgID,
		MsgID:        msgID,
		Msg:          msg,
	})
}

func handleRelayLocalStreamChunkLocal(senderUserID int64, payload protocol.RelayLocalStreamChunkPayload) bool {
	val, ok := localStreamRegistry.Load(payload.MsgID)
	if !ok {
		return false
	}
	entry := val.(*localStreamEntry)

	if senderUserID != entry.originUserID {
		logger.L.Warnf("relay_local_stream_chunk: sender mismatch msg_id=%d user=%d expected=%d",
			payload.MsgID, senderUserID, entry.originUserID)
		return true
	}
	if payload.ChunkSeq > 0 && payload.ChunkSeq <= entry.ss.ChunkSeq() {
		logger.L.Warnf(
			"relay_local_stream_chunk: duplicate chunk ignored msg_id=%d payload_seq=%d current_seq=%d",
			payload.MsgID,
			payload.ChunkSeq,
			entry.ss.ChunkSeq(),
		)
		return true
	}

	chunkSeq := entry.ss.AppendChunkNoBC(payload.DeltaContent)
	ctx := context.Background()
	if err := clearPendingLocalInferenceHint(ctx, entry.triggerMsgID); err != nil {
		logger.L.Warnf(
			"relay_local_stream_chunk: clear pending hint failed session=%s trigger=%d: %v",
			entry.sessionID,
			entry.triggerMsgID,
			err,
		)
	}
	entry.clearComposing(ctx, "first_chunk")

	chunkPayload := protocol.StreamChunkPayload{
		MsgID:        payload.MsgID,
		SessionID:    entry.sessionID,
		SenderID:     entry.agentID,
		SenderType:   2,
		DeltaContent: payload.DeltaContent,
		ChunkSeq:     chunkSeq,
		IsFinish:     false,
		CreatedAt:    time.Now().UnixMilli(),
	}
	broadcastToSessionExceptDevice(entry.hub, ctx, entry.members,
		entry.originUserID, entry.originDeviceID, protocol.CmdStreamChunk, chunkPayload)

	entry.timer.Reset(localStreamTimeout)
	return true
}

func handleRelayLocalStreamFinishLocal(senderUserID int64, payload protocol.RelayLocalStreamFinishPayload) bool {
	val, ok := localStreamRegistry.LoadAndDelete(payload.MsgID)
	if !ok {
		return false
	}
	entry := val.(*localStreamEntry)

	if senderUserID != entry.originUserID {
		logger.L.Warnf("relay_local_stream_finish: sender mismatch msg_id=%d", payload.MsgID)
		localStreamRegistry.Store(payload.MsgID, entry)
		return true
	}

	ctx := context.Background()
	entry.timer.Stop()
	entry.clearComposing(ctx, "finish")
	if err := clearLocalStreamStartState(ctx, entry.sessionID, entry.agentID, entry.triggerMsgID, payload.MsgID); err != nil {
		logger.L.Warnf(
			"relay_local_stream_finish: clear request state failed session=%s agent=%d trigger=%d msg_id=%d err=%v",
			entry.sessionID,
			entry.agentID,
			entry.triggerMsgID,
			payload.MsgID,
			err,
		)
	}
	if store.RDB != nil {
		store.RDB.Del(ctx, localStreamActiveKey(entry.sessionID))
	}
	if err := clearPendingLocalInferenceHint(ctx, entry.triggerMsgID); err != nil {
		logger.L.Warnf(
			"relay_local_stream_finish: clear pending hint failed session=%s trigger=%d: %v",
			entry.sessionID,
			entry.triggerMsgID,
			err,
		)
	}

	if payload.FinalContent != "" {
		if err := entry.ss.SetBuilderContent(payload.FinalContent); err != nil {
			logger.L.Warnf("relay_local_stream_finish: SetBuilderContent failed msg_id=%d: %v", payload.MsgID, err)
		}
	}

	fullContent, err := entry.ss.FinishNoBC()
	if err != nil {
		logger.L.Errorf("relay_local_stream_finish: FinishNoBC failed msg_id=%d: %v", payload.MsgID, err)
	}

	finishPayload := protocol.StreamFinishPayload{
		MsgID:           payload.MsgID,
		SessionID:       entry.sessionID,
		SenderID:        entry.agentID,
		SenderType:      2,
		FinalContent:    fullContent,
		QuotedMessageID: entry.triggerMsgID,
		LastChunkSeq:    entry.ss.ChunkSeq(),
		IsFinish:        true,
		CreatedAt:       time.Now().UnixMilli(),
	}
	broadcastToSessionExceptDevice(entry.hub, ctx, entry.members,
		entry.originUserID, entry.originDeviceID, protocol.CmdStreamFinish, finishPayload)

	service.ScheduleContentModeration(service.ContentModerationTask{
		SessionID: entry.sessionID,
		MsgID:     payload.MsgID,
	})
	return true
}

func handleLocalStreamTimeout(entry *localStreamEntry) {
	if entry == nil || entry.ss == nil {
		return
	}
	if !localStreamRegistry.CompareAndDelete(entry.ss.MsgID(), entry) {
		return
	}

	ctx := context.Background()
	logger.L.Warnf("relay_local_stream timeout: aborting msg_id=%d session=%s", entry.ss.MsgID(), entry.sessionID)
	entry.clearComposing(ctx, "timeout")
	entry.ss.Abort()
	if err := clearLocalStreamStartState(ctx, entry.sessionID, entry.agentID, entry.triggerMsgID, entry.ss.MsgID()); err != nil {
		logger.L.Warnf(
			"relay_local_stream timeout: clear request state failed session=%s agent=%d trigger=%d msg_id=%d err=%v",
			entry.sessionID,
			entry.agentID,
			entry.triggerMsgID,
			entry.ss.MsgID(),
			err,
		)
	}
	if store.RDB != nil {
		store.RDB.Del(ctx, localStreamActiveKey(entry.sessionID))
	}
	if err := clearPendingLocalInferenceHint(ctx, entry.triggerMsgID); err != nil {
		logger.L.Warnf(
			"relay_local_stream timeout: clear pending hint failed session=%s trigger=%d err=%v",
			entry.sessionID,
			entry.triggerMsgID,
			err,
		)
	}
}

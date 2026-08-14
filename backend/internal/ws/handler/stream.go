package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleStreamStop(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.StreamStopPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("stream_stop payload error: %v", err)
		return
	}

	// Publish stop signal via NATS
	data, _ := json.Marshal(map[string]interface{}{
		"cmd":        "stream_stop",
		"msg_id":     payload.MsgID,
		"session_id": payload.SessionID,
		"user_id":    conn.GetUserID(),
	})
	if store.JS != nil {
		store.JS.Publish(fmt.Sprintf("ai.request.%s", payload.SessionID), data)
	}
}

func HandleClientStreamChunk(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.ClientStreamChunkPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("client_stream_chunk payload error: %v", err)
		return
	}
	ctx := context.Background()
	if err := validateHumanSpeakTrigger(ctx, payload.SessionID, conn.GetUserID()); err != nil {
		logger.L.Warnf(
			"client_stream_chunk speaking denied user=%d session=%s err=%v",
			conn.GetUserID(),
			payload.SessionID,
			err,
		)
		code := 5001
		msg := "stream request failed"
		if sessionguard.IsDeniedError(err) {
			code = 4003
			msg = sessionguard.ErrorMessage(err)
		}
		sendStreamErrorToConn(
			conn,
			pkt.Seq,
			payload.SessionID,
			conn.GetUserID(),
			code,
			msg,
		)
		return
	}

	// Forward to AI orchestrator via NATS
	data, _ := json.Marshal(map[string]interface{}{
		"cmd":           "client_stream_chunk",
		"session_id":    payload.SessionID,
		"sender_id":     conn.GetUserID(),
		"delta_content": payload.DeltaContent,
		"is_finish":     payload.IsFinish,
	})
	if store.JS != nil {
		store.JS.Publish(fmt.Sprintf("ai.request.%s", payload.SessionID), data)
	}

	// If is_finish, also increment context version
	if payload.IsFinish {
		verKey := fmt.Sprintf("ai:ctx_ver:%s", payload.SessionID)
		store.RDB.Incr(ctx, verKey)
		store.RDB.Expire(ctx, verKey, 3600*1e9)
	}
}

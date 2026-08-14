package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleOverride(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.OverrideStreamPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("override_stream payload error: %v", err)
		return
	}

	// Signal to AI orchestrator to stop and hand over
	data, _ := json.Marshal(map[string]interface{}{
		"cmd":           "override_stream",
		"session_id":    payload.SessionID,
		"target_msg_id": payload.TargetMsgID,
		"user_id":       conn.GetUserID(),
	})
	if store.JS != nil {
		store.JS.Publish(fmt.Sprintf("ai.request.%s", payload.SessionID), data)
	}

	// Increment context version to invalidate current generation
	ctx := context.Background()
	verKey := fmt.Sprintf("ai:ctx_ver:%s", payload.SessionID)
	store.RDB.Incr(ctx, verKey)
	store.RDB.Expire(ctx, verKey, 3600*1e9)
}

package agentapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

const bindingCardMsgIDTTL = 48 * time.Hour

func bindingCardMsgIDKey(agentID int64, sessionID string) string {
	return fmt.Sprintf("im:agent_api:binding_card:%d:%s", agentID, strings.TrimSpace(sessionID))
}

func saveBindingCardMsgID(ctx context.Context, agentID int64, sessionID string, msgID int64) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" || msgID <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := store.RDB.Set(ctx, bindingCardMsgIDKey(agentID, sessionID), msgID, bindingCardMsgIDTTL).Err(); err != nil {
		logger.L.Warnf("save binding card msg_id failed agent=%d session=%s msg_id=%d err=%v", agentID, sessionID, msgID, err)
	}
}

// SaveBindingCardMsgID records the latest Open Workspace card message for an agent session.
func SaveBindingCardMsgID(ctx context.Context, agentID int64, sessionID string, msgID int64) {
	saveBindingCardMsgID(ctx, agentID, sessionID, msgID)
}

func loadBindingCardMsgID(ctx context.Context, agentID int64, sessionID string) int64 {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" {
		return 0
	}
	if ctx == nil {
		ctx = context.Background()
	}
	msgID, err := store.RDB.Get(ctx, bindingCardMsgIDKey(agentID, sessionID)).Int64()
	if err != nil {
		return 0
	}
	return msgID
}

// LoadBindingCardMsgID returns the latest Open Workspace card message recorded for an agent session.
func LoadBindingCardMsgID(ctx context.Context, agentID int64, sessionID string) int64 {
	return loadBindingCardMsgID(ctx, agentID, sessionID)
}

func deleteBindingCardMsgID(ctx context.Context, agentID int64, sessionID string) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := store.RDB.Del(ctx, bindingCardMsgIDKey(agentID, sessionID)).Err(); err != nil {
		logger.L.Warnf("delete binding card msg_id failed agent=%d session=%s err=%v", agentID, sessionID, err)
	}
}

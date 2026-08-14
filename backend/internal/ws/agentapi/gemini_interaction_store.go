package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
)

const (
	geminiPendingInteractionTTL = 48 * time.Hour

	geminiInteractionKindWorkspace = "workspace"
	geminiInteractionKindAuth      = "auth"
	geminiInteractionKindQuestion  = "question"
)

type geminiPendingInteraction struct {
	Kind      string               `json:"kind"`
	RequestID string               `json:"request_id,omitempty"`
	Event     DelegateEventPayload `json:"event"`
	CreatedAt int64                `json:"created_at"`
	UpdatedAt int64                `json:"updated_at"`
}

func geminiPendingWorkspaceKey(agentID int64, sessionID string) string {
	return fmt.Sprintf("im:agent_api:gemini:workspace:%d:%s", agentID, strings.TrimSpace(sessionID))
}

func geminiPendingRequestKey(requestID string) string {
	return fmt.Sprintf("im:agent_api:gemini:request:%s", strings.TrimSpace(requestID))
}

func saveGeminiPendingWorkspace(ctx context.Context, record geminiPendingInteraction) bool {
	if store.RDB == nil || record.Event.AgentID <= 0 || strings.TrimSpace(record.Event.SessionID) == "" {
		return false
	}
	record.Kind = geminiInteractionKindWorkspace
	return saveGeminiPendingInteraction(ctx, geminiPendingWorkspaceKey(record.Event.AgentID, record.Event.SessionID), record)
}

func loadGeminiPendingWorkspace(ctx context.Context, agentID int64, sessionID string) (*geminiPendingInteraction, bool) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" {
		return nil, false
	}
	return loadGeminiPendingInteraction(ctx, geminiPendingWorkspaceKey(agentID, sessionID))
}

func deleteGeminiPendingWorkspace(ctx context.Context, agentID int64, sessionID string) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" {
		return
	}
	deleteGeminiPendingInteraction(ctx, geminiPendingWorkspaceKey(agentID, sessionID))
}

func saveGeminiPendingRequest(ctx context.Context, record geminiPendingInteraction) bool {
	requestID := strings.TrimSpace(record.RequestID)
	if store.RDB == nil || requestID == "" {
		return false
	}
	if strings.TrimSpace(record.Kind) == "" {
		record.Kind = geminiInteractionKindAuth
	}
	record.RequestID = requestID
	return saveGeminiPendingInteraction(ctx, geminiPendingRequestKey(requestID), record)
}

func loadGeminiPendingRequest(ctx context.Context, requestID string) (*geminiPendingInteraction, bool) {
	if store.RDB == nil || strings.TrimSpace(requestID) == "" {
		return nil, false
	}
	return loadGeminiPendingInteraction(ctx, geminiPendingRequestKey(requestID))
}

func deleteGeminiPendingRequest(ctx context.Context, requestID string) {
	if store.RDB == nil || strings.TrimSpace(requestID) == "" {
		return
	}
	deleteGeminiPendingInteraction(ctx, geminiPendingRequestKey(requestID))
}

func saveGeminiPendingInteraction(ctx context.Context, key string, record geminiPendingInteraction) bool {
	if store.RDB == nil || strings.TrimSpace(key) == "" {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UnixMilli()
	if record.CreatedAt <= 0 {
		record.CreatedAt = now
	}
	record.UpdatedAt = now

	raw, err := json.Marshal(record)
	if err != nil {
		logger.L.Warnf("marshal gemini pending interaction failed key=%s err=%v", key, err)
		return false
	}
	if err := store.RDB.Set(ctx, key, raw, geminiPendingInteractionTTL).Err(); err != nil {
		logger.L.Warnf("save gemini pending interaction failed key=%s err=%v", key, err)
		return false
	}
	return true
}

func loadGeminiPendingInteraction(ctx context.Context, key string) (*geminiPendingInteraction, bool) {
	if store.RDB == nil || strings.TrimSpace(key) == "" {
		return nil, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := store.RDB.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, false
	}
	if err != nil {
		logger.L.Warnf("load gemini pending interaction failed key=%s err=%v", key, err)
		return nil, false
	}
	var record geminiPendingInteraction
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		logger.L.Warnf("decode gemini pending interaction failed key=%s err=%v", key, err)
		return nil, false
	}
	if record.Event.AgentID <= 0 || strings.TrimSpace(record.Event.SessionID) == "" {
		return nil, false
	}
	return &record, true
}

func deleteGeminiPendingInteraction(ctx context.Context, key string) {
	if store.RDB == nil || strings.TrimSpace(key) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := store.RDB.Del(ctx, key).Err(); err != nil {
		logger.L.Warnf("delete gemini pending interaction failed key=%s err=%v", key, err)
	}
}

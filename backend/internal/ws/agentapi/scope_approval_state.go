package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
)

const (
	scopeApprovalRequestPrefix      = "scope:"
	scopeApprovalDeniedTTL          = 24 * time.Hour
	scopeApprovalPendingTTL         = 24 * time.Hour
	maxPendingScopeApprovalsPerAgent = 5
)

type scopeApprovalPending struct {
	SessionID   string `json:"session_id,omitempty"`
	OwnerID     int64  `json:"owner_id,omitempty"`
	SenderID    int64  `json:"sender_id,omitempty"`
	SessionType int16  `json:"session_type,omitempty"`
	CanWake     bool   `json:"can_wake,omitempty"`
	CreatedAt   int64  `json:"created_at,omitempty"`
	ExpiresAt   int64  `json:"expires_at"`
}

func scopeApprovalPendingKey(agentID int64, scope string) string {
	return fmt.Sprintf("im:agent:scope_approval:pending:%d:%s", agentID, strings.TrimSpace(scope))
}

func scopeApprovalDeniedKey(agentID int64, scope string) string {
	return fmt.Sprintf("im:agent:scope_approval:denied:%d:%s", agentID, strings.TrimSpace(scope))
}

func scopeApprovalCountKey(agentID int64) string {
	return fmt.Sprintf("im:agent:scope_approval:count:%d", agentID)
}

func scopeApprovalDenied(ctx context.Context, agentID int64, scope string) bool {
	if store.RDB == nil {
		return false
	}
	key := scopeApprovalDeniedKey(agentID, scope)
	n, err := store.RDB.Exists(ctx, key).Result()
	return err == nil && n > 0
}

func markScopeApprovalDenied(ctx context.Context, agentID int64, scope string) {
	if store.RDB == nil {
		return
	}
	_ = store.RDB.Set(ctx, scopeApprovalDeniedKey(agentID, scope), "1", scopeApprovalDeniedTTL).Err()
	clearScopeApprovalPending(ctx, agentID, scope)
}

func loadScopeApprovalPending(ctx context.Context, agentID int64, scope string) (*scopeApprovalPending, bool) {
	if store.RDB == nil {
		return nil, false
	}
	raw, err := store.RDB.Get(ctx, scopeApprovalPendingKey(agentID, scope)).Result()
	if errors.Is(err, redis.Nil) || strings.TrimSpace(raw) == "" {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	var pending scopeApprovalPending
	if json.Unmarshal([]byte(raw), &pending) != nil {
		return nil, false
	}
	if pending.ExpiresAt > 0 && pending.ExpiresAt <= time.Now().UnixMilli() {
		clearScopeApprovalPending(ctx, agentID, scope)
		return nil, false
	}
	return &pending, true
}

func saveScopeApprovalPending(ctx context.Context, agentID int64, scope string, resume scopeResumeContext) (bool, int64, error) {
	if store.RDB == nil {
		return false, 0, errors.New("redis unavailable")
	}
	if scopeApprovalDenied(ctx, agentID, scope) {
		return false, 0, nil
	}
	if _, ok := loadScopeApprovalPending(ctx, agentID, scope); ok {
		return false, 0, nil
	}
	count, err := store.RDB.Get(ctx, scopeApprovalCountKey(agentID)).Int()
	if err != nil && !errors.Is(err, redis.Nil) {
		return false, 0, err
	}
	if count >= maxPendingScopeApprovalsPerAgent {
		return false, 0, nil
	}
	now := time.Now().UnixMilli()
	pending := scopeApprovalPending{
		SessionID:   resume.SessionID,
		OwnerID:     resume.OwnerID,
		SenderID:    resume.SenderID,
		SessionType: resume.SessionType,
		CanWake:     resume.CanWake,
		CreatedAt:   now,
		ExpiresAt:   now + scopeApprovalPendingTTL.Milliseconds(),
	}
	data, err := json.Marshal(pending)
	if err != nil {
		return false, 0, err
	}
	pipe := store.RDB.TxPipeline()
	pipe.Set(ctx, scopeApprovalPendingKey(agentID, scope), string(data), scopeApprovalPendingTTL)
	pipe.Incr(ctx, scopeApprovalCountKey(agentID))
	pipe.Expire(ctx, scopeApprovalCountKey(agentID), scopeApprovalPendingTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, 0, err
	}
	return true, now, nil
}

func clearScopeApprovalPending(ctx context.Context, agentID int64, scope string) {
	if store.RDB == nil {
		return
	}
	if n, err := store.RDB.Exists(ctx, scopeApprovalPendingKey(agentID, scope)).Result(); err == nil && n > 0 {
		store.RDB.Del(ctx, scopeApprovalPendingKey(agentID, scope))
		if count, decErr := store.RDB.Decr(ctx, scopeApprovalCountKey(agentID)).Result(); decErr == nil && count <= 0 {
			store.RDB.Del(ctx, scopeApprovalCountKey(agentID))
		}
	}
}

func consumeScopeApprovalPending(ctx context.Context, agentID int64, scope string) (*scopeApprovalPending, bool) {
	pending, ok := loadScopeApprovalPending(ctx, agentID, scope)
	if !ok || pending == nil {
		return nil, false
	}
	clearScopeApprovalPending(ctx, agentID, scope)
	return pending, true
}

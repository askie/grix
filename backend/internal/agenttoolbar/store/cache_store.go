package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	appstore "github.com/askie/grix/backend/internal/store"
)

const (
	snapshotTTL = 24 * time.Hour
	actionTTL   = 60 * time.Second
)

type CacheStore struct{}

type actionRecord struct {
	Processing bool           `json:"processing"`
	Ack        core.ActionAck `json:"ack"`
}

func NewCacheStore() *CacheStore {
	return &CacheStore{}
}

func (s *CacheStore) LoadSnapshot(ctx context.Context, ownerID int64, sessionID string, agentID int64) (toolprotocol.Snapshot, bool, error) {
	if appstore.RDB == nil || ownerID <= 0 || strings.TrimSpace(sessionID) == "" {
		return toolprotocol.Snapshot{}, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	data, err := appstore.RDB.Get(ctx, snapshotKey(ownerID, sessionID, agentID)).Bytes()
	if err != nil {
		return toolprotocol.Snapshot{}, false, nil
	}
	var snapshot toolprotocol.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return toolprotocol.Snapshot{}, false, err
	}
	return snapshot, true, nil
}

func (s *CacheStore) SaveSnapshot(ctx context.Context, ownerID int64, snapshot toolprotocol.Snapshot) (toolprotocol.Snapshot, bool, error) {
	if appstore.RDB == nil || ownerID <= 0 || strings.TrimSpace(snapshot.SessionID) == "" {
		return snapshot, true, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	existing, ok, err := s.LoadSnapshot(ctx, ownerID, snapshot.SessionID, snapshot.AgentID)
	if err != nil {
		return toolprotocol.Snapshot{}, false, err
	}
	if ok && snapshotsEqual(existing, snapshot) {
		if existing.Revision > 0 {
			_ = appstore.RDB.Set(ctx, revisionKey(ownerID, snapshot.SessionID, snapshot.AgentID), existing.Revision, snapshotTTL).Err()
		}
		s.syncIndex(ctx, ownerID, existing.AgentID, snapshot.SessionID, existing.Visible)
		_ = appstore.RDB.Expire(ctx, snapshotKey(ownerID, snapshot.SessionID, snapshot.AgentID), snapshotTTL).Err()
		return existing, false, nil
	}

	nextRevision, err := appstore.RDB.Incr(ctx, revisionKey(ownerID, snapshot.SessionID, snapshot.AgentID)).Result()
	if err != nil {
		return toolprotocol.Snapshot{}, false, err
	}
	snapshot.Revision = nextRevision
	snapshot.UpdatedAt = time.Now().UnixMilli()
	data, err := json.Marshal(snapshot)
	if err != nil {
		return toolprotocol.Snapshot{}, false, err
	}
	if err := appstore.RDB.Set(ctx, snapshotKey(ownerID, snapshot.SessionID, snapshot.AgentID), data, snapshotTTL).Err(); err != nil {
		return toolprotocol.Snapshot{}, false, err
	}
	_ = appstore.RDB.Expire(ctx, revisionKey(ownerID, snapshot.SessionID, snapshot.AgentID), snapshotTTL).Err()
	if ok && existing.AgentID > 0 && existing.AgentID != snapshot.AgentID {
		_ = appstore.RDB.SRem(ctx, indexKey(ownerID, existing.AgentID), strings.TrimSpace(snapshot.SessionID)).Err()
	}
	s.syncIndex(ctx, ownerID, snapshot.AgentID, snapshot.SessionID, snapshot.Visible)
	return snapshot, true, nil
}

func (s *CacheStore) DeleteSnapshot(ctx context.Context, ownerID int64, sessionID string, agentID int64) error {
	if appstore.RDB == nil || ownerID <= 0 || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	existing, ok, _ := s.LoadSnapshot(ctx, ownerID, sessionID, agentID)
	if ok && existing.AgentID > 0 {
		_ = appstore.RDB.SRem(ctx, indexKey(ownerID, existing.AgentID), strings.TrimSpace(sessionID)).Err()
	}
	targetAgentID := agentID
	if targetAgentID <= 0 && ok {
		targetAgentID = existing.AgentID
	}
	return appstore.RDB.Del(
		ctx,
		snapshotKey(ownerID, sessionID, targetAgentID),
		revisionKey(ownerID, sessionID, targetAgentID),
	).Err()
}

func (s *CacheStore) ListIndexedSessions(ctx context.Context, ownerID, agentID int64) ([]string, error) {
	if appstore.RDB == nil || ownerID <= 0 || agentID <= 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	values, err := appstore.RDB.SMembers(ctx, indexKey(ownerID, agentID)).Result()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(values))
	sessions := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		sessions = append(sessions, trimmed)
	}
	return sessions, nil
}

func (s *CacheStore) ReserveContextWarm(ctx context.Context, ownerID, agentID int64, sessionID string, ttl time.Duration) (bool, error) {
	if appstore.RDB == nil || ownerID <= 0 || agentID <= 0 || strings.TrimSpace(sessionID) == "" {
		return true, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return appstore.RDB.SetNX(ctx, contextWarmKey(ownerID, agentID, sessionID), "1", ttl).Result()
}

func (s *CacheStore) ReserveRateLimitFetch(ctx context.Context, ownerID int64, accountKey string, ttl time.Duration) (bool, error) {
	if appstore.RDB == nil || ownerID <= 0 || strings.TrimSpace(accountKey) == "" {
		return true, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	return appstore.RDB.SetNX(ctx, rateLimitFetchKey(ownerID, accountKey), "1", ttl).Result()
}

func (s *CacheStore) ReserveAction(ctx context.Context, ownerID int64, sessionID string, agentID int64, clientActionID string) (bool, core.ActionAck, error) {
	if appstore.RDB == nil || ownerID <= 0 || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(clientActionID) == "" {
		return true, core.ActionAck{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	record := actionRecord{Processing: true}
	data, err := json.Marshal(record)
	if err != nil {
		return false, core.ActionAck{}, err
	}
	ok, err := appstore.RDB.SetNX(ctx, actionKey(ownerID, sessionID, agentID, clientActionID), data, actionTTL).Result()
	if err != nil {
		return false, core.ActionAck{}, err
	}
	if ok {
		return true, core.ActionAck{}, nil
	}
	existing, _, err := s.loadAction(ctx, ownerID, sessionID, agentID, clientActionID)
	return false, existing, err
}

func (s *CacheStore) CompleteAction(ctx context.Context, ownerID int64, sessionID string, agentID int64, clientActionID string, ack core.ActionAck) error {
	if appstore.RDB == nil || ownerID <= 0 || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(clientActionID) == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	record := actionRecord{Ack: ack}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return appstore.RDB.Set(ctx, actionKey(ownerID, sessionID, agentID, clientActionID), data, actionTTL).Err()
}

func (s *CacheStore) loadAction(ctx context.Context, ownerID int64, sessionID string, agentID int64, clientActionID string) (core.ActionAck, bool, error) {
	if appstore.RDB == nil {
		return core.ActionAck{}, false, nil
	}
	data, err := appstore.RDB.Get(ctx, actionKey(ownerID, sessionID, agentID, clientActionID)).Bytes()
	if err != nil {
		return core.ActionAck{}, false, nil
	}
	var record actionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return core.ActionAck{}, false, err
	}
	return record.Ack, record.Processing, nil
}

func (s *CacheStore) syncIndex(ctx context.Context, ownerID, agentID int64, sessionID string, visible bool) {
	if appstore.RDB == nil || ownerID <= 0 || agentID <= 0 || strings.TrimSpace(sessionID) == "" {
		return
	}
	if visible {
		_ = appstore.RDB.SAdd(ctx, indexKey(ownerID, agentID), strings.TrimSpace(sessionID)).Err()
		_ = appstore.RDB.Expire(ctx, indexKey(ownerID, agentID), snapshotTTL).Err()
		return
	}
	_ = appstore.RDB.SRem(ctx, indexKey(ownerID, agentID), strings.TrimSpace(sessionID)).Err()
}

func snapshotKey(ownerID int64, sessionID string, agentID int64) string {
	return fmt.Sprintf("im:agent_toolbar:snapshot:%d:%s:%d", ownerID, strings.TrimSpace(sessionID), agentID)
}

func revisionKey(ownerID int64, sessionID string, agentID int64) string {
	return fmt.Sprintf("im:agent_toolbar:rev:%d:%s:%d", ownerID, strings.TrimSpace(sessionID), agentID)
}

func indexKey(ownerID, agentID int64) string {
	return fmt.Sprintf("im:agent_toolbar:index:%d:%d", ownerID, agentID)
}

func actionKey(ownerID int64, sessionID string, agentID int64, clientActionID string) string {
	return fmt.Sprintf(
		"im:agent_toolbar:action:%d:%s:%d:%s",
		ownerID,
		strings.TrimSpace(sessionID),
		agentID,
		strings.TrimSpace(clientActionID),
	)
}

func contextWarmKey(ownerID, agentID int64, sessionID string) string {
	return fmt.Sprintf(
		"im:agent_toolbar:warm_context:%d:%d:%s",
		ownerID,
		agentID,
		strings.TrimSpace(sessionID),
	)
}

func rateLimitFetchKey(ownerID int64, accountKey string) string {
	return fmt.Sprintf(
		"im:agent_toolbar:rate_limit_fetch:%d:%s",
		ownerID,
		strings.TrimSpace(accountKey),
	)
}

func snapshotsEqual(a, b toolprotocol.Snapshot) bool {
	a.Revision = 0
	a.UpdatedAt = 0
	b.Revision = 0
	b.UpdatedAt = 0
	aBytes, _ := json.Marshal(a)
	bBytes, _ := json.Marshal(b)
	return string(aBytes) == string(bBytes)
}

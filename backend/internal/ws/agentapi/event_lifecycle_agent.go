package agentapi

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

func ResolveEventLifecycleAgentID(ownerID int64, sessionID string, preferredAgentID int64) int64 {
	sid := strings.TrimSpace(sessionID)
	if ownerID <= 0 || sid == "" {
		return 0
	}

	if preferredAgentID > 0 && isSessionAgentMember(sid, preferredAgentID) {
		return preferredAgentID
	}

	if aid := resolveDelegateAgentID(ownerID, sid); aid > 0 {
		return aid
	}
	if mgr := GetGlobal(); mgr != nil {
		if run := mgr.LookupActiveRunBySessionOwner(ownerID, sid); run != nil && run.AgentID > 0 {
			return run.AgentID
		}
	}
	if store.DB == nil {
		return 0
	}

	var member model.SessionMember
	if err := store.DB.
		Where("session_id = ? AND member_type = 2", sid).
		Order("joined_at ASC").
		Take(&member).Error; err == nil && member.MemberID > 0 {
		return member.MemberID
	}
	return 0
}

func isSessionAgentMember(sessionID string, agentID int64) bool {
	if store.DB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" {
		return false
	}
	var member model.SessionMember
	if err := store.DB.
		Where("session_id = ? AND member_type = 2 AND member_id = ?", sessionID, agentID).
		Take(&member).Error; err != nil {
		return false
	}
	return member.MemberID > 0
}

func resolveDelegateAgentID(ownerID int64, sessionID string) int64 {
	if store.RDB == nil {
		return 0
	}
	key := fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID)
	raw, err := store.RDB.HGet(context.Background(), key, "agent_id").Result()
	if err != nil {
		return 0
	}
	agentID, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return agentID
}

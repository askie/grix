package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

const autoDelegateDefaultMaxConsecutiveReplies = 10

// EnsureAutoDelegateForPrivateSession activates the peer user's configured
// auto-delegate on a 1:1 session when missing. Same rules as private-session
// create / register-welcome (owner-owned or actively shared agent).
func EnsureAutoDelegateForPrivateSession(sessionID string, initiatorID, peerID int64, peerType int16) {
	ensureAutoDelegateForPrivateSession(sessionID, initiatorID, peerID, peerType)
}

func ensureAutoDelegateForPrivateSession(sessionID string, initiatorID, peerID int64, peerType int16) {
	if peerType != 1 {
		return
	}
	ensureAutoDelegateForSessionMember(sessionID, initiatorID, peerID)
}

// ResolveAutoDelegateAgentID returns the user's configured chat auto-delegate
// agent when it passes the same ownership/share validation used by settings and
// session auto-start, and is an active Agent API provider.
func ResolveAutoDelegateAgentID(userID int64) (int64, bool, error) {
	if userID <= 0 {
		return 0, false, nil
	}
	agentID, hasAutoAgent, err := loadUserAutoDelegateAgentID(userID)
	if err != nil {
		return 0, false, err
	}
	if !hasAutoAgent {
		return 0, false, nil
	}
	if err := validateAutoDelegateAgent(userID, agentID); err != nil {
		return 0, false, nil
	}
	var agent model.Agent
	if err := store.DB.Select("id", "status", "provider_type").
		First(&agent, agentID).Error; err != nil {
		return 0, false, err
	}
	if agent.Status != model.AgentStatusActive || agent.ProviderType != model.AgentProviderAPI {
		return 0, false, nil
	}
	return agent.ID, true, nil
}

func ensureAutoDelegateForGroupSessionMembers(
	sessionID string,
	initiatorID int64,
	memberIDs []int64,
	memberTypes []int16,
) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	if len(memberIDs) == 0 || len(memberIDs) != len(memberTypes) {
		return
	}
	for i, memberID := range memberIDs {
		if memberTypes[i] != 1 {
			continue
		}
		ensureAutoDelegateForSessionMember(sessionID, initiatorID, memberID)
	}
}

func ensureAutoDelegateForSessionMember(sessionID string, initiatorID, targetUserID int64) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	if initiatorID <= 0 || targetUserID <= 0 || targetUserID == initiatorID {
		return
	}
	if store.RDB == nil {
		return
	}

	agentID, hasAutoAgent, err := loadUserAutoDelegateAgentID(targetUserID)
	if err != nil {
		logger.L.Warnf(
			"load user auto delegate agent failed session=%s user=%d: %v",
			sessionID,
			targetUserID,
			err,
		)
		return
	}
	if !hasAutoAgent {
		return
	}

	if err := validateAutoDelegateAgent(targetUserID, agentID); err != nil {
		logger.L.Warnf(
			"skip auto delegate due to invalid agent session=%s user=%d agent=%d: %v",
			sessionID,
			targetUserID,
			agentID,
			err,
		)
		return
	}

	ctx := context.Background()
	delegateKey := fmt.Sprintf("im:delegate:%s:%d", sessionID, targetUserID)
	exists, err := store.RDB.Exists(ctx, delegateKey).Result()
	if err != nil {
		logger.L.Warnf(
			"check auto delegate key failed session=%s user=%d err=%v",
			sessionID,
			targetUserID,
			err,
		)
		return
	}
	if exists > 0 {
		return
	}

	nowUnix := time.Now().Unix()
	if err := store.RDB.HSet(ctx, delegateKey, map[string]any{
		"agent_id":                agentID,
		"started_at":              nowUnix,
		"updated_at":              nowUnix,
		"max_consecutive_replies": autoDelegateDefaultMaxConsecutiveReplies,
	}).Err(); err != nil {
		logger.L.Warnf(
			"auto delegate key set failed session=%s user=%d agent=%d: %v",
			sessionID,
			targetUserID,
			agentID,
			err,
		)
		return
	}

	streakKey := fmt.Sprintf("im:delegate:streak:%s:%d", sessionID, targetUserID)
	if err := store.RDB.Del(ctx, streakKey).Err(); err != nil {
		logger.L.Warnf(
			"clear auto delegate streak failed session=%s user=%d: %v",
			sessionID,
			targetUserID,
			err,
		)
	}

	if err := store.DB.Create(&model.DelegationLog{
		SessionID: sessionID,
		UserID:    targetUserID,
		AgentID:   agentID,
		Action:    "auto_start",
	}).Error; err != nil {
		logger.L.Warnf(
			"create auto delegate log failed session=%s user=%d agent=%d: %v",
			sessionID,
			targetUserID,
			agentID,
			err,
		)
	}
}

package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/model"
)

func TestSessionUpdateMemberAgentReceiveSettingPreservesHiddenBacklogCount(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		ownerID   = int64(9811)
		agentID   = int64(9812)
		sessionID = "session-agent-receive-setting-1"
	)

	now := time.Now().UTC()
	seedUser(t, testDB, ownerID)
	seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)

	if err := testDB.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: model.SessionTypeGroup,
		GroupName:   "receive-setting",
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{
			SessionID:                sessionID,
			MemberID:                 ownerID,
			MemberType:               1,
			Role:                     3,
			LastActiveAt:             now,
			JoinedAt:                 now,
			AgentReceiveMode:         agentreceive.ModeNormal,
			AgentReceiveBacklogCount: 6,
		},
		{
			SessionID:                sessionID,
			MemberID:                 agentID,
			MemberType:               2,
			Role:                     1,
			LastActiveAt:             now,
			JoinedAt:                 now,
			AgentReceiveMode:         agentreceive.ModeNormal,
			AgentReceiveBacklogCount: 12,
		},
	}).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}

	resp, err := SessionUpdateMemberAgentReceiveSetting(
		ownerID,
		sessionID,
		agentID,
		2,
		agentreceive.ModeMentionOnly,
		0,
	)
	if err != nil {
		t.Fatalf("SessionUpdateMemberAgentReceiveSetting() error = %v", err)
	}
	if resp.AgentReceiveBacklogCount != 12 {
		t.Fatalf("backlog_count=%d want=12", resp.AgentReceiveBacklogCount)
	}
	if resp.AgentReceiveMode != agentreceive.ModeMentionOnly {
		t.Fatalf("mode=%d want=%d", resp.AgentReceiveMode, agentreceive.ModeMentionOnly)
	}

	var member model.SessionMember
	if err := testDB.DB.
		Where("session_id = ? AND member_id = ? AND member_type = 2", sessionID, agentID).
		First(&member).Error; err != nil {
		t.Fatalf("load session member error: %v", err)
	}
	if member.AgentReceiveBacklogCount != 12 {
		t.Fatalf("stored backlog_count=%d want=12", member.AgentReceiveBacklogCount)
	}
	if member.AgentReceiveMode != agentreceive.ModeMentionOnly {
		t.Fatalf("stored mode=%d want=%d", member.AgentReceiveMode, agentreceive.ModeMentionOnly)
	}
}

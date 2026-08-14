package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/model"
)

func TestSessionConvertToGroupFlipsTypeAndClearsDirectKey(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		ownerID   = int64(7711)
		agentID   = int64(7712)
		sessionID = "session-convert-1"
	)

	now := time.Now().UTC()
	seedUser(t, testDB, ownerID)
	seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)

	directKey := "direct-7711-7712"
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		DirectKey:      &directKey,
		OwnerID:        ownerID,
		SessionType:    model.SessionTypeDirect,
		LastMsgSummary: "Claude",
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, CustomTitle: "Claude", LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1, LastActiveAt: now, JoinedAt: now},
	}).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	resp, err := SessionConvertToGroup(ownerID, sessionID, "我的群聊")
	if err != nil {
		t.Fatalf("SessionConvertToGroup() error = %v", err)
	}
	if resp.SessionType != model.SessionTypeGroup {
		t.Fatalf("resp type=%d want=%d", resp.SessionType, model.SessionTypeGroup)
	}

	var session model.Session
	if err := testDB.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		t.Fatalf("reload session error: %v", err)
	}
	if session.SessionType != model.SessionTypeGroup {
		t.Fatalf("stored type=%d want=%d", session.SessionType, model.SessionTypeGroup)
	}
	if session.DirectKey != nil {
		t.Fatalf("direct_key=%v want nil", *session.DirectKey)
	}
	if session.GroupName != "我的群聊" {
		t.Fatalf("group_name=%q want=%q", session.GroupName, "我的群聊")
	}

	// 成员保持不变。
	var count int64
	if err := testDB.DB.Model(&model.SessionMember{}).
		Where("session_id = ?", sessionID).Count(&count).Error; err != nil {
		t.Fatalf("count members error: %v", err)
	}
	if count != 2 {
		t.Fatalf("member count=%d want=2", count)
	}

	// agent 成员被置为 ModeAll（群内有问必答）。
	var agentMember model.SessionMember
	if err := testDB.DB.
		Where("session_id = ? AND member_id = ? AND member_type = 2", sessionID, agentID).
		First(&agentMember).Error; err != nil {
		t.Fatalf("load agent member error: %v", err)
	}
	if agentMember.AgentReceiveMode != agentreceive.ModeAll {
		t.Fatalf("agent mode=%d want=%d", agentMember.AgentReceiveMode, agentreceive.ModeAll)
	}
}

func TestSessionConvertToGroupRejectsNonMember(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		ownerID    = int64(7721)
		agentID    = int64(7722)
		strangerID = int64(7799)
		sessionID  = "session-convert-2"
	)

	now := time.Now().UTC()
	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, strangerID)
	seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)

	directKey := "direct-7721-7722"
	if err := testDB.DB.Create(&model.Session{
		SessionID:   sessionID,
		DirectKey:   &directKey,
		OwnerID:     ownerID,
		SessionType: model.SessionTypeDirect,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1, LastActiveAt: now, JoinedAt: now},
	}).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	if _, err := SessionConvertToGroup(strangerID, sessionID, "x"); err != ErrSessionPermissionDenied {
		t.Fatalf("err=%v want=ErrSessionPermissionDenied", err)
	}
}

func TestSessionConvertToGroupRejectsAlreadyGroup(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		ownerID   = int64(7731)
		sessionID = "session-convert-3"
	)

	now := time.Now().UTC()
	seedUser(t, testDB, ownerID)

	if err := testDB.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: model.SessionTypeGroup,
		GroupName:   "already",
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now,
	}).Error; err != nil {
		t.Fatalf("create member error: %v", err)
	}

	if _, err := SessionConvertToGroup(ownerID, sessionID, "x"); err != ErrSessionInvalidType {
		t.Fatalf("err=%v want=ErrSessionInvalidType", err)
	}
}

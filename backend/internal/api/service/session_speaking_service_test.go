package service

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
)

func TestSessionSpeakingGovernance(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		ownerID   = int64(9801)
		adminID   = int64(9802)
		memberID  = int64(9803)
		agentID   = int64(9804)
		sessionID = "session-speaking-governance-1"
	)

	now := time.Now()
	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, adminID)
	seedUser(t, testDB, memberID)
	seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    model.SessionTypeGroup,
		GroupName:      "speaking-governance",
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: adminID, MemberType: 1, Role: 2, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1, LastActiveAt: now, JoinedAt: now},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}

	t.Run("admin can enable all members muted", func(t *testing.T) {
		resp, err := SessionUpdateAllMembersMuted(adminID, sessionID, true)
		if err != nil {
			t.Fatalf("SessionUpdateAllMembersMuted() error = %v", err)
		}
		if !resp.AllMembersMuted {
			t.Fatal("expected all_members_muted=true")
		}

		var updated model.Session
		if err := testDB.DB.Select("all_members_muted").First(&updated, "session_id = ?", sessionID).Error; err != nil {
			t.Fatalf("load updated session error: %v", err)
		}
		if !updated.AllMembersMuted {
			t.Fatal("expected session.all_members_muted=true")
		}
	})

	t.Run("owner can whitelist muted group member", func(t *testing.T) {
		allow := true
		resp, err := SessionUpdateMemberSpeaking(ownerID, sessionID, memberID, 1, nil, &allow)
		if err != nil {
			t.Fatalf("SessionUpdateMemberSpeaking() whitelist error = %v", err)
		}
		if !resp.CanSpeakWhenAllMuted {
			t.Fatal("expected can_speak_when_all_muted=true")
		}
	})

	t.Run("owner can mute agent member directly", func(t *testing.T) {
		muted := true
		resp, err := SessionUpdateMemberSpeaking(ownerID, sessionID, agentID, 2, &muted, nil)
		if err != nil {
			t.Fatalf("SessionUpdateMemberSpeaking() mute agent error = %v", err)
		}
		if !resp.IsSpeakMuted {
			t.Fatal("expected is_speak_muted=true")
		}
	})

	t.Run("admin cannot mute owner", func(t *testing.T) {
		muted := true
		_, err := SessionUpdateMemberSpeaking(adminID, sessionID, ownerID, 1, &muted, nil)
		if !errors.Is(err, ErrSessionOwnerSpeakingImmutable) {
			t.Fatalf("expected ErrSessionOwnerSpeakingImmutable, got %v", err)
		}
	})

	t.Run("detail exposes speaking fields", func(t *testing.T) {
		resp, err := SessionDetail(ownerID, sessionID)
		if err != nil {
			t.Fatalf("SessionDetail() error = %v", err)
		}
		if !resp.AllMembersMuted {
			t.Fatal("expected detail all_members_muted=true")
		}

		memberMap := make(map[string]SessionDetailMember, len(resp.Members))
		for _, item := range resp.Members {
			memberMap[strconv.FormatInt(item.MemberID, 10)] = item
		}
		if !memberMap["9803"].CanSpeakWhenAllMuted {
			t.Fatal("expected whitelisted member in detail")
		}
		if !memberMap["9804"].IsSpeakMuted {
			t.Fatal("expected muted agent in detail")
		}
	})
}

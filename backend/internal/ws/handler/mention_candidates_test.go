package handler

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

func TestResolveMentionCandidatesForSessionIncludesViewerRemark(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-mention-candidates-remark"
		ownerID   = int64(8101)
		peerID    = int64(8102)
	)

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "remark-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	users := []model.User{
		{ID: ownerID, Username: "owner_user", Email: "owner_user@example.com", Nickname: "OwnerNick"},
		{ID: peerID, Username: "target_user", Email: "target_user@example.com", Nickname: "TargetNick"},
	}
	for _, user := range users {
		if err := store.DB.Create(&user).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{
			SessionID:     sessionID,
			MemberID:      peerID,
			MemberType:    1,
			GroupNickname: "群内称呼",
			JoinedAt:      now,
			LastActiveAt:  now,
		},
	}
	for _, member := range members {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Friend{
		ID:         99001,
		UserID:     ownerID,
		FriendID:   peerID,
		RemarkName: "备注名",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create friend relation error: %v", err)
	}

	candidates := ResolveMentionCandidatesForSession(sessionID, ownerID)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	var peerCandidate *struct {
		UserID  int64
		Aliases []string
	}
	for _, item := range candidates {
		if item.UserID != peerID {
			continue
		}
		peerCandidate = &struct {
			UserID  int64
			Aliases []string
		}{UserID: item.UserID, Aliases: item.Aliases}
		break
	}
	if peerCandidate == nil {
		t.Fatalf("missing peer candidate")
	}
	if !containsAlias(peerCandidate.Aliases, "备注名") {
		t.Fatalf("expected aliases to include remark name, got %v", peerCandidate.Aliases)
	}
	if !containsAlias(peerCandidate.Aliases, "群内称呼") {
		t.Fatalf("expected aliases to include group nickname, got %v", peerCandidate.Aliases)
	}
	if !containsAlias(peerCandidate.Aliases, "target_user") {
		t.Fatalf("expected aliases to include username, got %v", peerCandidate.Aliases)
	}
	if !containsAlias(peerCandidate.Aliases, "TargetNick") {
		t.Fatalf("expected aliases to include nickname, got %v", peerCandidate.Aliases)
	}

	withoutRemark := ResolveMentionCandidatesForSession(sessionID, 0)
	for _, item := range withoutRemark {
		if item.UserID != peerID {
			continue
		}
		if containsAlias(item.Aliases, "备注名") {
			t.Fatalf("unexpected remark alias without viewer id, aliases=%v", item.Aliases)
		}
	}
}

func containsAlias(aliases []string, target string) bool {
	for _, alias := range aliases {
		if alias == target {
			return true
		}
	}
	return false
}

package sessionguard

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestValidateSpeakPermissionRejectsMutedDirectMember(t *testing.T) {
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	defer testDB.Close()

	const (
		sessionID = "direct-muted"
		userID    = int64(8101)
	)
	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: model.SessionTypeDirect,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     userID,
		MemberType:   1,
		IsSpeakMuted: true,
		LastActiveAt: now,
		JoinedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}

	err := ValidateSpeakPermission(context.Background(), nil, sessionID, userID, 1)
	if !errors.Is(err, ErrMemberSpeakMuted) {
		t.Fatalf("ValidateSpeakPermission() error = %v, want %v", err, ErrMemberSpeakMuted)
	}
}

func TestValidateSpeakPermissionRejectsMutedGroupMember(t *testing.T) {
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	defer testDB.Close()

	const (
		sessionID = "group-muted"
		userID    = int64(8102)
	)
	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: model.SessionTypeGroup,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     userID,
		MemberType:   1,
		IsSpeakMuted: true,
		LastActiveAt: now,
		JoinedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}

	err := ValidateSpeakPermission(context.Background(), nil, sessionID, userID, 1)
	if !errors.Is(err, ErrMemberSpeakMuted) {
		t.Fatalf("ValidateSpeakPermission() in group session should reject muted member, got: %v", err)
	}
}

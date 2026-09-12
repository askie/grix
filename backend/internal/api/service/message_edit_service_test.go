package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// setupMessageEditTest seeds a session with an owner member and an agent
// member, plus one text message sent by the agent, ready to be edited.
func setupMessageEditTest(t *testing.T) (sessionID string, ownerID, agentID, msgID int64, cleanup func()) {
	t.Helper()
	testDB, teardown := setupMessageTest(t)

	sessionID = "edit-service-session"
	ownerID = int64(8701)
	agentID = int64(9701)
	msgID = int64(7002001)
	now := time.Now().UTC()

	if err := testDB.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: model.SessionTypeDirect,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		m := member
		if err := testDB.DB.Create(&m).Error; err != nil {
			t.Fatalf("create member(%d,%d) error: %v", m.MemberID, m.MemberType, err)
		}
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   agentID,
		SenderType: 2,
		MsgType:    model.MsgTypeText,
		Content:    "original content",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}
	return sessionID, ownerID, agentID, msgID, teardown
}

func TestEditMessage_AgentEditsOwnTextMessageSucceeds(t *testing.T) {
	sessionID, ownerID, agentID, msgID, cleanup := setupMessageEditTest(t)
	defer cleanup()

	_, err := EditMessage(context.Background(), sessionID, msgID, MessageEditActor{
		UserID:  ownerID,
		AgentID: agentID,
	}, "updated content")
	if err != nil {
		t.Fatalf("EditMessage() error = %v", err)
	}

	var msg model.Message
	if err := store.DB.Where("msg_id = ? AND session_id = ?", msgID, sessionID).First(&msg).Error; err != nil {
		t.Fatalf("reload message error: %v", err)
	}
	if msg.Content != "updated content" {
		t.Fatalf("content=%q want=%q", msg.Content, "updated content")
	}
}

// TestEditMessage_ReturnsMentionDispatchContextOnSuccess locks the contract
// that callers (HTTP handlers, the Agent API WS bridge) rely on to hand off
// to ws/handler.DispatchMessageEditMentionAdditions after a successful edit:
// the editor's identity and the pre-edit content/extra must come back so the
// caller can diff old vs. new mentions without a second DB round trip.
func TestEditMessage_ReturnsMentionDispatchContextOnSuccess(t *testing.T) {
	sessionID, ownerID, agentID, msgID, cleanup := setupMessageEditTest(t)
	defer cleanup()

	outcome, err := EditMessage(context.Background(), sessionID, msgID, MessageEditActor{
		UserID:  ownerID,
		AgentID: agentID,
	}, "updated content")
	if err != nil {
		t.Fatalf("EditMessage() error = %v", err)
	}
	if outcome == nil {
		t.Fatalf("outcome=nil want non-nil dispatch context")
	}
	if outcome.EditorMemberID != agentID {
		t.Fatalf("EditorMemberID=%d want=%d", outcome.EditorMemberID, agentID)
	}
	if outcome.EditorMemberType != 2 {
		t.Fatalf("EditorMemberType=%d want=2", outcome.EditorMemberType)
	}
	if outcome.OldContent != "original content" {
		t.Fatalf("OldContent=%q want=%q", outcome.OldContent, "original content")
	}
	if outcome.MsgType != model.MsgTypeText {
		t.Fatalf("MsgType=%d want=%d", outcome.MsgType, model.MsgTypeText)
	}
	if outcome.QuotedMessageID != 0 {
		t.Fatalf("QuotedMessageID=%d want=0", outcome.QuotedMessageID)
	}
}

// TestEditMessage_NoOpEditReturnsNilContext covers the "pure text edit that
// changes nothing" boundary: EditMessage's existing no-op short-circuit
// (content and extra both unchanged) must also report no dispatch context,
// so identical-content edits never queue a mention diff.
func TestEditMessage_NoOpEditReturnsNilContext(t *testing.T) {
	sessionID, ownerID, agentID, msgID, cleanup := setupMessageEditTest(t)
	defer cleanup()

	outcome, err := EditMessage(context.Background(), sessionID, msgID, MessageEditActor{
		UserID:  ownerID,
		AgentID: agentID,
	}, "original content")
	if err != nil {
		t.Fatalf("EditMessage() error = %v", err)
	}
	if outcome != nil {
		t.Fatalf("outcome=%#v want=nil for a no-op edit", outcome)
	}
}

// TestEditMessage_ReturnsNilContextOnFailure locks the other half of the same
// contract: a failed edit (transaction never committed) must never hand back
// a dispatch context, so callers that only branch on err structurally cannot
// dispatch a mention diff for an edit that did not happen.
func TestEditMessage_ReturnsNilContextOnFailure(t *testing.T) {
	sessionID, ownerID, agentID, msgID, cleanup := setupMessageEditTest(t)
	defer cleanup()

	outcome, err := EditMessage(context.Background(), sessionID, msgID, MessageEditActor{
		UserID:  ownerID,
		AgentID: agentID + 1,
	}, "hijacked content")
	if !errors.Is(err, ErrMessageEditDenied) {
		t.Fatalf("err=%v want ErrMessageEditDenied", err)
	}
	if outcome != nil {
		t.Fatalf("outcome=%#v want=nil on failure", outcome)
	}
}

func TestEditMessage_RejectsEditingAnotherSendersMessage(t *testing.T) {
	sessionID, ownerID, agentID, msgID, cleanup := setupMessageEditTest(t)
	defer cleanup()
	_ = agentID

	_, err := EditMessage(context.Background(), sessionID, msgID, MessageEditActor{
		UserID:  ownerID,
		AgentID: agentID + 1,
	}, "hijacked content")
	if !errors.Is(err, ErrMessageEditDenied) {
		t.Fatalf("err=%v want ErrMessageEditDenied", err)
	}
}

func TestEditMessage_RejectsEditingCardMessage(t *testing.T) {
	sessionID, ownerID, agentID, msgID, cleanup := setupMessageEditTest(t)
	defer cleanup()

	if err := store.DB.Model(&model.Message{}).
		Where("msg_id = ? AND session_id = ?", msgID, sessionID).
		Update("content", "[Approve](grix://card/approval?d=abc)").Error; err != nil {
		t.Fatalf("seed card content error: %v", err)
	}

	_, err := EditMessage(context.Background(), sessionID, msgID, MessageEditActor{
		UserID:  ownerID,
		AgentID: agentID,
	}, "trying to rewrite the card")
	if !errors.Is(err, ErrMessageEditNotAllowed) {
		t.Fatalf("err=%v want ErrMessageEditNotAllowed", err)
	}
}

func TestEditMessage_AllowCardMessageBypassesCardRestriction(t *testing.T) {
	sessionID, ownerID, agentID, msgID, cleanup := setupMessageEditTest(t)
	defer cleanup()

	if err := store.DB.Model(&model.Message{}).
		Where("msg_id = ? AND session_id = ?", msgID, sessionID).
		Update("content", "[Approve](grix://card/approval?d=abc)").Error; err != nil {
		t.Fatalf("seed card content error: %v", err)
	}

	_, err := EditMessage(context.Background(), sessionID, msgID, MessageEditActor{
		UserID:           ownerID,
		AgentID:          agentID,
		AllowCardMessage: true,
	}, "[Approve](grix://card/approval?d=updated)")
	if err != nil {
		t.Fatalf("EditMessage() error = %v", err)
	}
}

func TestEditMessage_RejectsEditingRevokedMessage(t *testing.T) {
	sessionID, ownerID, agentID, msgID, cleanup := setupMessageEditTest(t)
	defer cleanup()

	if err := store.DB.Model(&model.Message{}).
		Where("msg_id = ? AND session_id = ?", msgID, sessionID).
		Update("is_revoked", true).Error; err != nil {
		t.Fatalf("seed revoked flag error: %v", err)
	}

	_, err := EditMessage(context.Background(), sessionID, msgID, MessageEditActor{
		UserID:  ownerID,
		AgentID: agentID,
	}, "trying to edit revoked message")
	if !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("err=%v want ErrMessageNotFound", err)
	}
}

func TestBuildMessageEditPayloadIncludesThreadID(t *testing.T) {
	msg := model.Message{
		MsgID:           9001,
		SessionID:       "edit-session",
		ThreadID:        "topic-edit-a",
		SenderID:        1001,
		SenderType:      2,
		MsgType:         1,
		Content:         "edited",
		QuotedMessageID: 18889990001,
		CreatedAt:       time.Unix(1700000000, 0).UTC(),
	}

	payload := buildMessageEditPayload(msg, 2, 88)
	if payload.ThreadID != "topic-edit-a" {
		t.Fatalf("thread_id=%q want=topic-edit-a", payload.ThreadID)
	}
}

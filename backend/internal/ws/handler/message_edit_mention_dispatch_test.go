package handler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

func TestDispatchMessageEditMentionAdditions_NewAgentMentionDispatchesOnce(t *testing.T) {
	sessionID := "edit-mention-new-agent"
	senderID := int64(61001)
	agentID := int64(62001)
	fx := setupMultiAgentGroupFixture(t, sessionID, senderID, agentID)
	defer fx.cleanup()

	msgID := int64(70010001)
	oldContent := "let's plan the rollout"
	newContent := fmt.Sprintf("let's plan the rollout, cc @%d", agentID)

	DispatchMessageEditMentionAdditions(
		fx.hub, context.Background(), sessionID,
		senderID, 1,
		msgID, 0, 1,
		oldContent, nil,
		newContent, nil,
	)

	events := collectForwardedAgentEvents(t, fx.channel, 1)
	event := requireForwardedAgentEventByOwner(t, events, senderID+1000)
	if event.AgentID != agentID {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, agentID)
	}
	if !event.Edited {
		t.Fatalf("edited=false want=true")
	}
	if event.EditedMsgID != msgID {
		t.Fatalf("edited_msg_id=%d want=%d", event.EditedMsgID, msgID)
	}
	if event.EventType != "group_mention" {
		t.Fatalf("event_type=%q want=group_mention", event.EventType)
	}
	if len(event.MentionUserIDs) != 1 || event.MentionUserIDs[0] != agentID {
		t.Fatalf("mention_user_ids=%v want=[%d]", event.MentionUserIDs, agentID)
	}
	assertNoMoreForwardedAgentEvents(t, fx.channel)

	var receiptCount int64
	if err := store.DB.Model(&model.MessageMentionDispatchReceipt{}).
		Where("msg_id = ? AND member_id = ?", msgID, agentID).
		Count(&receiptCount).Error; err != nil {
		t.Fatalf("count receipt error: %v", err)
	}
	if receiptCount != 1 {
		t.Fatalf("receipt count=%d want=1", receiptCount)
	}
}

func TestDispatchMessageEditMentionAdditions_RemovedMentionDoesNotTrigger(t *testing.T) {
	sessionID := "edit-mention-removed"
	senderID := int64(61002)
	agentID := int64(62002)
	fx := setupMultiAgentGroupFixture(t, sessionID, senderID, agentID)
	defer fx.cleanup()

	oldContent := fmt.Sprintf("cc @%d please review", agentID)
	newContent := "please review"

	DispatchMessageEditMentionAdditions(
		fx.hub, context.Background(), sessionID,
		senderID, 1,
		int64(70020001), 0, 1,
		oldContent, nil,
		newContent, nil,
	)

	assertNoMoreForwardedAgentEvents(t, fx.channel)
}

func TestDispatchMessageEditMentionAdditions_PlainTextEditDoesNotTrigger(t *testing.T) {
	sessionID := "edit-mention-plain-text"
	senderID := int64(61003)
	agentID := int64(62003)
	fx := setupMultiAgentGroupFixture(t, sessionID, senderID, agentID)
	defer fx.cleanup()

	oldContent := fmt.Sprintf("draft v1 cc @%d", agentID)
	newContent := fmt.Sprintf("draft v2 (typo fix) cc @%d", agentID)

	DispatchMessageEditMentionAdditions(
		fx.hub, context.Background(), sessionID,
		senderID, 1,
		int64(70030001), 0, 1,
		oldContent, nil,
		newContent, nil,
	)

	assertNoMoreForwardedAgentEvents(t, fx.channel)
}

// TestDispatchMessageEditMentionAdditions_RepeatedAddRemoveAddDoesNotDoubleDispatch
// covers the oscillation case explicitly called out in the decision note: a
// lead repeatedly toggling the same @mention across separate edits of the
// same message must wake the mentioned agent at most once, ever.
func TestDispatchMessageEditMentionAdditions_RepeatedAddRemoveAddDoesNotDoubleDispatch(t *testing.T) {
	sessionID := "edit-mention-oscillation"
	senderID := int64(61004)
	agentID := int64(62004)
	fx := setupMultiAgentGroupFixture(t, sessionID, senderID, agentID)
	defer fx.cleanup()

	msgID := int64(70040001)
	withoutMention := "flowchart draft"
	withMention := fmt.Sprintf("flowchart draft cc @%d", agentID)

	// Edit 1: add the mention -> dispatches once.
	DispatchMessageEditMentionAdditions(
		fx.hub, context.Background(), sessionID,
		senderID, 1, msgID, 0, 1,
		withoutMention, nil, withMention, nil,
	)
	events := collectForwardedAgentEvents(t, fx.channel, 1)
	if events[0].AgentID != agentID {
		t.Fatalf("agent_id=%d want=%d", events[0].AgentID, agentID)
	}

	// Edit 2: remove the mention -> no trigger (removal never notifies).
	DispatchMessageEditMentionAdditions(
		fx.hub, context.Background(), sessionID,
		senderID, 1, msgID, 0, 1,
		withMention, nil, withoutMention, nil,
	)
	assertNoMoreForwardedAgentEvents(t, fx.channel)

	// Edit 3: re-add the same mention -> this edit's own diff sees it as
	// "newly added" again, but the durable receipt from edit 1 must still
	// suppress a second dispatch.
	DispatchMessageEditMentionAdditions(
		fx.hub, context.Background(), sessionID,
		senderID, 1, msgID, 0, 1,
		withoutMention, nil, withMention, nil,
	)
	assertNoMoreForwardedAgentEvents(t, fx.channel)

	var receiptCount int64
	if err := store.DB.Model(&model.MessageMentionDispatchReceipt{}).
		Where("msg_id = ? AND member_id = ?", msgID, agentID).
		Count(&receiptCount).Error; err != nil {
		t.Fatalf("count receipt error: %v", err)
	}
	if receiptCount != 1 {
		t.Fatalf("receipt count=%d want=1 (claimed once, never re-claimed)", receiptCount)
	}
}

func TestDispatchMessageEditMentionAdditions_NewHumanMentionSkipsAgentDispatch(t *testing.T) {
	sessionID := "edit-mention-new-human"
	senderID := int64(61005)
	agentID := int64(62005)
	fx := setupMultiAgentGroupFixture(t, sessionID, senderID, agentID)
	defer fx.cleanup()

	// The human being newly @mentioned is already a session member (senderID
	// itself, for a minimal fixture) — humans need no explicit dispatch here,
	// see the function's doc comment.
	oldContent := "morning update"
	newContent := fmt.Sprintf("morning update cc @%d", senderID)

	DispatchMessageEditMentionAdditions(
		fx.hub, context.Background(), sessionID,
		senderID, 1,
		int64(70050001), 0, 1,
		oldContent, nil,
		newContent, nil,
	)

	assertNoMoreForwardedAgentEvents(t, fx.channel)
}

func TestDispatchMessageEditMentionAdditions_PrivateSessionIsNoOp(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "edit-mention-private-session"
	ownerID := int64(61006)
	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	// Must not panic or query anything requiring group-only state (no agents,
	// no session members seeded at all).
	DispatchMessageEditMentionAdditions(
		nil, context.Background(), sessionID,
		ownerID, 1,
		int64(70060001), 0, 1,
		"hi", nil,
		fmt.Sprintf("hi @%d", ownerID+1), nil,
	)
}

func TestDispatchMessageEditMentionAdditions_SelfMentionSkipped(t *testing.T) {
	sessionID := "edit-mention-self"
	senderID := int64(61007)
	agentID := int64(62007)
	fx := setupMultiAgentGroupFixture(t, sessionID, senderID, agentID)
	defer fx.cleanup()

	oldContent := "status update"
	newContent := fmt.Sprintf("status update cc @%d", agentID)

	// The agent itself is the editor, and the newly added mention targets
	// itself. The existing self-sender skip in resolveDirectSessionRoute
	// (isDirectRouteSelfSender) must suppress delivery without this feature
	// reimplementing that rule.
	DispatchMessageEditMentionAdditions(
		fx.hub, context.Background(), sessionID,
		agentID, 2,
		int64(70070001), 0, 1,
		oldContent, nil,
		newContent, nil,
	)

	assertNoMoreForwardedAgentEvents(t, fx.channel)
}

func TestDispatchMessageEditMentionAdditions_AgentEditorMentionsAnotherAgentDispatches(t *testing.T) {
	sessionID := "edit-mention-agent-to-agent"
	senderID := int64(61008)
	editorAgentID := int64(62008)
	targetAgentID := int64(62108)
	fx := setupMultiAgentGroupFixture(t, sessionID, senderID, editorAgentID, targetAgentID)
	defer fx.cleanup()

	oldContent := "handing this off"
	newContent := fmt.Sprintf("handing this off to @%d", targetAgentID)

	DispatchMessageEditMentionAdditions(
		fx.hub, context.Background(), sessionID,
		editorAgentID, 2,
		int64(70080001), 0, 1,
		oldContent, nil,
		newContent, nil,
	)

	events := collectForwardedAgentEvents(t, fx.channel, 1)
	if events[0].AgentID != targetAgentID {
		t.Fatalf("agent_id=%d want=%d", events[0].AgentID, targetAgentID)
	}
	if events[0].EventType != "group_mention" {
		t.Fatalf("event_type=%q want=group_mention", events[0].EventType)
	}
	assertNoMoreForwardedAgentEvents(t, fx.channel)
}

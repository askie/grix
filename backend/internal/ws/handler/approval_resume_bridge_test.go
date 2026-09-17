package handler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/claudeaccess"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
)

func TestAccessApprovalRedispatchSingleAgent(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-access-redispatch", 88001, 98001, 98002)
	defer fixture.cleanup()

	const strangerID int64 = 88002
	now := time.Now().UTC()
	if err := store.DB.Create(&model.User{
		ID: strangerID, Username: "stranger", Email: "stranger@t.local", Nickname: "Stranger",
		Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create stranger: %v", err)
	}

	triggerMsgID := int64(18880001)
	if err := store.DB.Create(&model.Message{
		MsgID: triggerMsgID, SessionID: fixture.sessionID,
		SenderID: strangerID, SenderType: 1, MsgType: 1,
		Content: fmt.Sprintf("@%d hello gate", fixture.agentIDs[0]),
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}

	if _, err := claudeaccess.EvaluateInbound(context.Background(), fixture.agentIDs[0], fmt.Sprintf("%d", strangerID), fixture.sessionID, 2, triggerMsgID); err != nil {
		t.Fatalf("EvaluateInbound: %v", err)
	}
	status, _ := claudeaccess.GetStatus(context.Background(), fixture.agentIDs[0])
	if len(status.PendingPairs) != 1 {
		t.Fatalf("pending=%+v", status.PendingPairs)
	}
	code := status.PendingPairs[0].Code

	result, err := claudeaccess.ApprovePairing(context.Background(), fixture.agentIDs[0], code)
	if err != nil {
		t.Fatalf("ApprovePairing: %v", err)
	}
	if result.TriggerMsgID != triggerMsgID {
		t.Fatalf("TriggerMsgID=%d want %d", result.TriggerMsgID, triggerMsgID)
	}

	RedispatchAccessBlockedMessage(fixture.hub, wsagentapi.AccessRedispatchParams{
		AgentID: fixture.agentIDs[0], SessionID: fixture.sessionID, TriggerMsgID: triggerMsgID,
	})

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	if len(events) != 1 {
		t.Fatalf("events=%d want 1", len(events))
	}
	if events[0].AgentID != fixture.agentIDs[0] {
		t.Fatalf("agent_id=%d want %d", events[0].AgentID, fixture.agentIDs[0])
	}
	if events[0].MsgID != triggerMsgID {
		t.Fatalf("msg_id=%d want %d", events[0].MsgID, triggerMsgID)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestAccessApprovalRedispatchSkipsRevokedMessage(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-access-revoked", 88011, 98011, 98012)
	defer fixture.cleanup()
	triggerMsgID := int64(18880011)
	if err := store.DB.Create(&model.Message{
		MsgID: triggerMsgID, SessionID: fixture.sessionID,
		SenderID: 88011, SenderType: 1, MsgType: 1, Content: "hi", IsRevoked: true,
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}
	RedispatchAccessBlockedMessage(fixture.hub, wsagentapi.AccessRedispatchParams{
		AgentID: fixture.agentIDs[0], SessionID: fixture.sessionID, TriggerMsgID: triggerMsgID,
	})
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestAccessApprovalOldPendingPairWithoutTriggerMsgID(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() { _ = store.RDB.Close(); store.RDB = previous }()
	_ = snowflake.Init(1)
	const agentID int64 = 70099
	raw := `{"schema_version":1,"policy":"allowlist","allowlist":{},"pending_pairs":{"OLD123":{"sender_id":"9","session_id":"g1","expires_at":9999999999999}}}`
	if err := store.RDB.Set(context.Background(), fmt.Sprintf("im:claude:access:%d", agentID), raw, time.Hour).Err(); err != nil {
		t.Fatalf("seed redis: %v", err)
	}
	result, err := claudeaccess.ApprovePairing(context.Background(), agentID, "OLD123")
	if err != nil {
		t.Fatalf("ApprovePairing: %v", err)
	}
	if result.TriggerMsgID != 0 {
		t.Fatalf("TriggerMsgID=%d want 0 for legacy pending", result.TriggerMsgID)
	}
}

func TestAccessRedispatchLoopChainNotDoubled(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-access-loop", 88021, 98021, 98022)
	defer fixture.cleanup()
	ctx := context.Background()

	msgID := int64(18880021)
	content := fmt.Sprintf("ping @%d", fixture.agentIDs[1])
	if err := store.DB.Create(&model.Message{
		MsgID: msgID, SessionID: fixture.sessionID,
		SenderID: fixture.agentIDs[0], SenderType: 2, MsgType: 1, Content: content,
		QuotedMessageID: 18880020,
	}).Error; err != nil {
		t.Fatalf("create msg: %v", err)
	}
	semantics, err := resolvePersistedGroupDispatchSemantics(ctx, fixture.sessionID, fixture.agentIDs[0], 2, msgID, 18880020, content, nil)
	if err != nil {
		t.Fatalf("semantics: %v", err)
	}
	TriggerDirectRouteForMessage(fixture.hub, ctx, fixture.sessionID, fixture.agentIDs[0], 2, msgID, 18880020, 1, content, nil, nil, &semantics)
	countAfterFirst, _ := store.RDB.Get(ctx, agentAutoLoopChainKey(fixture.sessionID)).Int64()

	RedispatchAccessBlockedMessage(fixture.hub, wsagentapi.AccessRedispatchParams{
		AgentID: fixture.agentIDs[1], SessionID: fixture.sessionID, TriggerMsgID: msgID,
	})
	countAfterRedispatch, _ := store.RDB.Get(ctx, agentAutoLoopChainKey(fixture.sessionID)).Int64()
	if countAfterRedispatch != countAfterFirst {
		t.Fatalf("loop chain %d -> %d, want unchanged redispatch", countAfterFirst, countAfterRedispatch)
	}
}

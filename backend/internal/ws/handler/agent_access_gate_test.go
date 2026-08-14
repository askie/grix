package handler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/claudeaccess"
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
)

func TestMaybeHandleAgentAccessGateBlockedPrivateSender(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const agentID = int64(9801)
	if _, err := claudeaccess.AllowSender(context.Background(), agentID, "seed-allowed"); err != nil {
		t.Fatalf("AllowSender error: %v", err)
	}

	var sent []wsagentapi.SendMessageReq
	handled, err := maybeHandleAgentAccessGate(context.Background(), agentAccessGateRequest{
		Agent: model.Agent{
			ID:              agentID,
			OwnerID:         8801,
			ProviderType:    model.AgentProviderAPI,
			AgentClientType: model.AgentClientTypeClaude,
			Status:          model.AgentStatusActive,
		},
		OwnerID:      8801,
		SessionID:    "chat-direct-claude-access",
		SessionType:  1,
		SenderID:     8802,
		TriggerMsgID: 9901,
	}, func(_ context.Context, req wsagentapi.SendMessageReq) error {
		sent = append(sent, req)
		return nil
	})
	if err != nil {
		t.Fatalf("maybeHandleAgentAccessGate error: %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want true")
	}
	if len(sent) != 1 {
		t.Fatalf("send count = %d, want 1", len(sent))
	}
	// 私聊硬规则：陌生人收到"改走群聊"提示卡（agent_status），不是配对码卡。
	if !strings.Contains(sent[0].Content, "grix://card/agent_status?") {
		t.Fatalf("owner-only notice content = %q", sent[0].Content)
	}
	if strings.Contains(sent[0].Content, "grix://card/agent_pairing?") {
		t.Fatalf("private stranger must NOT get a pairing card: %q", sent[0].Content)
	}
	if !strings.Contains(sent[0].ClientMsgID, "agent_access_private_only") {
		t.Fatalf("client_msg_id = %q, want agent_access_private_only prefix", sent[0].ClientMsgID)
	}
}

// 注：原 TestCheckDelegatesBlockedClaudeSenderSkipsRateLimitAndSendsPairing 已删除。
// 它锁的是"托管路径下访客被门禁拦截并收到提示卡"的旧行为——那正是误伤主人托管的 bug。
// 托管路径现已整体豁免门禁，正向断言见 TestCheckDelegatesVisitorNotBlockedByAccessGate。

func TestMaybeHandleAgentAccessGateOwnerExempt(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const agentID = int64(9821)
	const ownerID = int64(8821)
	if _, err := claudeaccess.AllowSender(context.Background(), agentID, "seed-allowed"); err != nil {
		t.Fatalf("AllowSender error: %v", err)
	}

	var sent []wsagentapi.SendMessageReq
	handled, err := maybeHandleAgentAccessGate(context.Background(), agentAccessGateRequest{
		Agent: model.Agent{
			ID:              agentID,
			OwnerID:         ownerID,
			ProviderType:    model.AgentProviderAPI,
			AgentClientType: model.AgentClientTypeClaude,
			Status:          model.AgentStatusActive,
		},
		OwnerID:      ownerID,
		SessionID:    "chat-owner-exempt",
		SessionType:  1,
		SenderID:     ownerID, // 主人本人发消息
		TriggerMsgID: 9921,
	}, func(_ context.Context, req wsagentapi.SendMessageReq) error {
		sent = append(sent, req)
		return nil
	})
	if err != nil {
		t.Fatalf("maybeHandleAgentAccessGate error: %v", err)
	}
	if handled {
		t.Fatal("handled = true, want false (owner is always exempt)")
	}
	if len(sent) != 0 {
		t.Fatalf("send count = %d, want 0", len(sent))
	}
}

func TestMaybeHandleAgentAccessGateNonClaudeGroupBypassWhenFlagOff(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()
	featuregate.InvalidateCache()

	const agentID = int64(9831)
	if _, err := claudeaccess.AllowSender(context.Background(), agentID, "seed-allowed"); err != nil {
		t.Fatalf("AllowSender error: %v", err)
	}

	var sent []wsagentapi.SendMessageReq
	handled, err := maybeHandleAgentAccessGate(context.Background(), agentAccessGateRequest{
		Agent: model.Agent{
			ID:              agentID,
			OwnerID:         8831,
			ProviderType:    model.AgentProviderAPI,
			AgentClientType: "codex",
			Status:          model.AgentStatusActive,
		},
		OwnerID:      8831,
		SessionID:    "chat-nonclaude-flag-off",
		SessionType:  2, // 群聊维度受开关灰度；私聊硬规则另测
		SenderID:     8832,
		TriggerMsgID: 9931,
	}, func(_ context.Context, req wsagentapi.SendMessageReq) error {
		sent = append(sent, req)
		return nil
	})
	if err != nil {
		t.Fatalf("maybeHandleAgentAccessGate error: %v", err)
	}
	if handled {
		t.Fatal("handled = true, want false (flag off keeps non-claude group ungated)")
	}
	if len(sent) != 0 {
		t.Fatalf("send count = %d, want 0", len(sent))
	}
}

func TestMaybeHandleAgentAccessGatePrivateHardRuleIgnoresFlag(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()
	featuregate.InvalidateCache()

	// 非 Claude、开关未开、名单为空：私聊硬规则仍生效，陌生人被挡下收到"改走群聊"提示、不直通、不发配对码。
	var sent []wsagentapi.SendMessageReq
	handled, err := maybeHandleAgentAccessGate(context.Background(), agentAccessGateRequest{
		Agent: model.Agent{
			ID:              9881,
			OwnerID:         8881,
			ProviderType:    model.AgentProviderAPI,
			AgentClientType: "codex",
			Status:          model.AgentStatusActive,
		},
		OwnerID:      8881,
		SessionID:    "chat-private-hard-rule",
		SessionType:  1,
		SenderID:     8882,
		TriggerMsgID: 9981,
	}, func(_ context.Context, req wsagentapi.SendMessageReq) error {
		sent = append(sent, req)
		return nil
	})
	if err != nil {
		t.Fatalf("maybeHandleAgentAccessGate error: %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want true (private chat is owner-only regardless of flag)")
	}
	if len(sent) != 1 {
		t.Fatalf("send count = %d, want 1", len(sent))
	}
	if !strings.Contains(sent[0].Content, "grix://card/agent_status?") ||
		strings.Contains(sent[0].Content, "grix://card/agent_pairing?") {
		t.Fatalf("expected an owner-only notice (not a pairing card), got %q", sent[0].Content)
	}
}

func TestMaybeHandleAgentAccessGateNonClaudeGatedWhenFlagEnabled(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	if err := featuregate.SaveGate(featuregate.FeatureAgentAccessGate, "Agent 访问门禁", model.FeatureStatusEnabled); err != nil {
		t.Fatalf("SaveGate error: %v", err)
	}
	featuregate.InvalidateCache()
	defer featuregate.InvalidateCache()

	const agentID = int64(9841)
	if _, err := claudeaccess.AllowSender(context.Background(), agentID, "seed-allowed"); err != nil {
		t.Fatalf("AllowSender error: %v", err)
	}

	var sent []wsagentapi.SendMessageReq
	handled, err := maybeHandleAgentAccessGate(context.Background(), agentAccessGateRequest{
		Agent: model.Agent{
			ID:              agentID,
			OwnerID:         8841,
			ProviderType:    model.AgentProviderAPI,
			AgentClientType: "codex",
			Status:          model.AgentStatusActive,
		},
		OwnerID:      8841,
		SessionID:    "chat-nonclaude-flag-on",
		SessionType:  2, // 群聊维度受开关灰度；门禁激活后陌生人走配对码
		SenderID:     8842,
		TriggerMsgID: 9941,
	}, func(_ context.Context, req wsagentapi.SendMessageReq) error {
		sent = append(sent, req)
		return nil
	})
	if err != nil {
		t.Fatalf("maybeHandleAgentAccessGate error: %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want true (flag on gates non-claude)")
	}
	// 群访问申请改走主人私聊审批卡（经全局 agentapi Manager），群内不再发任何卡片。
	if len(sent) != 0 {
		t.Fatalf("group must stay silent, got %+v", sent)
	}
	// 本测试未挂全局 Manager，通知必然失败：待批记录必须被回滚，
	// 否则 pending 挡住重试而主人没收到卡（双盲黑洞）。
	status, statusErr := claudeaccess.GetStatus(context.Background(), agentID)
	if statusErr != nil {
		t.Fatalf("GetStatus error: %v", statusErr)
	}
	if status.PendingPairCount != 0 {
		t.Fatalf("PendingPairCount = %d, want 0 (rollback on notify failure)", status.PendingPairCount)
	}
}

func TestMaybeHandleAgentAccessGateNonClaudeWhitelistScopesByOwner(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const gatedOwner = int64(8851)
	const ungatedOwner = int64(8852)
	if err := featuregate.SaveGate(featuregate.FeatureAgentAccessGate, "Agent 访问门禁", model.FeatureStatusWhitelist); err != nil {
		t.Fatalf("SaveGate error: %v", err)
	}
	if err := featuregate.AddUsersToWhitelist(featuregate.FeatureAgentAccessGate, []int64{gatedOwner}); err != nil {
		t.Fatalf("AddUsersToWhitelist error: %v", err)
	}
	featuregate.InvalidateCache()
	defer featuregate.InvalidateCache()

	run := func(agentID, ownerID, senderID int64, session string) (bool, int) {
		var sent []wsagentapi.SendMessageReq
		handled, err := maybeHandleAgentAccessGate(context.Background(), agentAccessGateRequest{
			Agent: model.Agent{
				ID:              agentID,
				OwnerID:         ownerID,
				ProviderType:    model.AgentProviderAPI,
				AgentClientType: "codex",
				Status:          model.AgentStatusActive,
			},
			OwnerID:      ownerID,
			SessionID:    session,
			SessionType:  2, // 开关灰度只影响群聊维度
			SenderID:     senderID,
			TriggerMsgID: 9951,
		}, func(_ context.Context, req wsagentapi.SendMessageReq) error {
			sent = append(sent, req)
			return nil
		})
		if err != nil {
			t.Fatalf("maybeHandleAgentAccessGate error: %v", err)
		}
		return handled, len(sent)
	}

	if _, err := claudeaccess.AllowSender(context.Background(), 9851, "seed-allowed"); err != nil {
		t.Fatalf("AllowSender error: %v", err)
	}
	if _, err := claudeaccess.AllowSender(context.Background(), 9852, "seed-allowed"); err != nil {
		t.Fatalf("AllowSender error: %v", err)
	}

	handled, sends := run(9851, gatedOwner, 7777, "chat-whitelist-gated")
	if !handled || sends != 0 {
		t.Fatalf("whitelisted owner should be gated silently (approval card goes to owner thread): handled=%v sends=%d", handled, sends)
	}
	handled, sends = run(9852, ungatedOwner, 7777, "chat-whitelist-ungated")
	if handled || sends != 0 {
		t.Fatalf("non-whitelisted owner should bypass: handled=%v sends=%d", handled, sends)
	}
}

func TestMaybeHandleAgentAccessGateSharedRecipientExempt(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const agentID = int64(9861)
	const ownerID = int64(8861)
	const sharedUser = int64(8862)
	if _, err := claudeaccess.AllowSender(context.Background(), agentID, "seed-allowed"); err != nil {
		t.Fatalf("AllowSender error: %v", err)
	}
	if err := store.DB.Create(&model.User{ID: sharedUser, Status: model.UserStatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatalf("create user error: %v", err)
	}
	if err := store.DB.Create(&model.AgentShare{AgentID: agentID, OwnerID: ownerID, SharedTo: sharedUser, Status: model.AgentShareStatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatalf("create share error: %v", err)
	}

	var sent []wsagentapi.SendMessageReq
	handled, err := maybeHandleAgentAccessGate(context.Background(), agentAccessGateRequest{
		Agent: model.Agent{
			ID:              agentID,
			OwnerID:         ownerID,
			ProviderType:    model.AgentProviderAPI,
			AgentClientType: model.AgentClientTypeClaude,
			Status:          model.AgentStatusActive,
		},
		OwnerID:      ownerID,
		SessionID:    "chat-shared-exempt",
		SessionType:  2, // 群聊——共享场景最常见的被拦位置
		SenderID:     sharedUser,
		TriggerMsgID: 9961,
	}, func(_ context.Context, req wsagentapi.SendMessageReq) error {
		sent = append(sent, req)
		return nil
	})
	if err != nil {
		t.Fatalf("maybeHandleAgentAccessGate error: %v", err)
	}
	if handled {
		t.Fatal("handled = true, want false (active share recipient is exempt)")
	}
	if len(sent) != 0 {
		t.Fatalf("send count = %d, want 0", len(sent))
	}
	// 评估过程中为被共享者临时落下的待批申请必须被清掉（否则主人会看到幽灵申请）。
	status, statusErr := claudeaccess.GetStatus(context.Background(), agentID)
	if statusErr != nil {
		t.Fatalf("GetStatus error: %v", statusErr)
	}
	if status.PendingPairCount != 0 {
		t.Fatalf("PendingPairCount = %d, want 0 (ghost pending cleaned)", status.PendingPairCount)
	}
}

func TestMaybeHandleAgentAccessGateFailOpenOnEvaluateError(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	// 打断 Redis：群聊路径的 EvaluateInbound 必然报错，门禁应放行而不是吞消息
	_ = store.RDB.Close()

	var sent []wsagentapi.SendMessageReq
	handled, err := maybeHandleAgentAccessGate(context.Background(), agentAccessGateRequest{
		Agent: model.Agent{
			ID:              9871,
			OwnerID:         8871,
			ProviderType:    model.AgentProviderAPI,
			AgentClientType: model.AgentClientTypeClaude,
			Status:          model.AgentStatusActive,
		},
		OwnerID:      8871,
		SessionID:    "chat-fail-open",
		SessionType:  2,
		SenderID:     8872,
		TriggerMsgID: 9971,
	}, func(_ context.Context, req wsagentapi.SendMessageReq) error {
		sent = append(sent, req)
		return nil
	})
	if err != nil {
		t.Fatalf("maybeHandleAgentAccessGate error: %v (want fail-open, no error)", err)
	}
	if handled {
		t.Fatal("handled = true, want false (evaluate failure must fail open)")
	}
	if len(sent) != 0 {
		t.Fatalf("send count = %d, want 0", len(sent))
	}
}

func TestMaybeHandleAgentAccessGateAgentSenderExempt(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const agentID = int64(9891)
	if _, err := claudeaccess.AllowSender(context.Background(), agentID, "seed-allowed"); err != nil {
		t.Fatalf("AllowSender error: %v", err)
	}

	// 群里另一个 agent @ 本 agent：协作消息不受访客门禁约束（agent ID 与用户 ID 不同命名空间）。
	var sent []wsagentapi.SendMessageReq
	handled, err := maybeHandleAgentAccessGate(context.Background(), agentAccessGateRequest{
		Agent: model.Agent{
			ID:              agentID,
			OwnerID:         8891,
			ProviderType:    model.AgentProviderAPI,
			AgentClientType: model.AgentClientTypeClaude,
			Status:          model.AgentStatusActive,
		},
		OwnerID:      8891,
		SessionID:    "chat-agent-sender",
		SessionType:  2,
		SenderID:     7654321, // 发送方 agent 的 ID
		SenderType:   2,
		TriggerMsgID: 9991,
	}, func(_ context.Context, req wsagentapi.SendMessageReq) error {
		sent = append(sent, req)
		return nil
	})
	if err != nil {
		t.Fatalf("maybeHandleAgentAccessGate error: %v", err)
	}
	if handled {
		t.Fatal("handled = true, want false (agent senders are exempt)")
	}
	if len(sent) != 0 {
		t.Fatalf("send count = %d, want 0", len(sent))
	}
	status, _ := claudeaccess.GetStatus(context.Background(), agentID)
	if status.PendingPairCount != 0 {
		t.Fatalf("PendingPairCount = %d, want 0 (no request for agent senders)", status.PendingPairCount)
	}
}

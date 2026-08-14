package claudeaccess

import (
	"context"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupClaudeAccessTest(t *testing.T) func() {
	t.Helper()
	previousRedis := store.RDB
	store.RDB = testutil.NewMockRedis()
	return func() {
		_ = store.RDB.Close()
		store.RDB = previousRedis
	}
}

func TestEvaluateInboundPrivateStrangerBlockedOwnerOnly(t *testing.T) {
	cleanup := setupClaudeAccessTest(t)
	defer cleanup()

	// 私聊硬规则：陌生人一律挡，提示改走群聊，且不发配对码、不生成 pending pair。
	// 名单为空也不例外（无首发自动认领），名单非空同理。
	for _, seed := range []bool{false, true} {
		agentID := int64(7001)
		senderID := "sender-a"
		if seed {
			agentID = 7011
			senderID = "sender-x" // 名单内的人：私聊同样不放行（名单只授予群聊使用权）
			if _, err := AllowSender(context.Background(), agentID, "sender-x"); err != nil {
				t.Fatalf("AllowSender error = %v", err)
			}
		}
		result, err := EvaluateInbound(context.Background(), agentID, senderID, "chat-a", 1)
		if err != nil {
			t.Fatalf("EvaluateInbound(seed=%v) error = %v", seed, err)
		}
		if result.Dispatch {
			t.Fatalf("Dispatch(seed=%v) = true, want false (private is owner-only)", seed)
		}
		if result.Reason != "private_owner_only" {
			t.Fatalf("Reason(seed=%v) = %q, want private_owner_only", seed, result.Reason)
		}
		if strings.TrimSpace(result.PairingCode) != "" {
			t.Fatalf("PairingCode(seed=%v) = %q, want empty (no pairing in private)", seed, result.PairingCode)
		}
		status, err := GetStatus(context.Background(), agentID)
		if err != nil {
			t.Fatalf("GetStatus error = %v", err)
		}
		if status.PendingPairCount != 0 {
			t.Fatalf("PendingPairCount(seed=%v) = %d, want 0 (no pairing in private)", seed, status.PendingPairCount)
		}
	}
}

func TestEvaluateInboundGroupVisitorRequiresApprovalEvenWithEmptyAllowlist(t *testing.T) {
	cleanup := setupClaudeAccessTest(t)
	defer cleanup()

	// 群聊访客不在名单（名单空也一样）：一律走主人审批。
	result, err := EvaluateInbound(context.Background(), 7002, "sender-a", "chat-a", 2)
	if err != nil {
		t.Fatalf("EvaluateInbound error = %v", err)
	}
	if result.Dispatch {
		t.Fatal("Dispatch = true, want false (group visitor needs owner approval)")
	}
	if result.Reason != "pairing_required" || result.PairingCode == "" {
		t.Fatalf("Reason = %q code=%q, want pairing_required with code", result.Reason, result.PairingCode)
	}
}

func TestEvaluateInboundDisabledPolicyBlocksEvenWithEmptyAllowlist(t *testing.T) {
	cleanup := setupClaudeAccessTest(t)
	defer cleanup()

	if _, err := SetPolicy(context.Background(), 7005, PolicyDisabled); err != nil {
		t.Fatalf("SetPolicy error = %v", err)
	}
	result, err := EvaluateInbound(context.Background(), 7005, "sender-a", "chat-a", 1)
	if err != nil {
		t.Fatalf("EvaluateInbound error = %v", err)
	}
	if result.Dispatch {
		t.Fatal("Dispatch = true, want false for disabled policy")
	}
	if result.Reason != "policy_disabled" {
		t.Fatalf("Reason = %q, want policy_disabled", result.Reason)
	}
}

func TestEvaluateInboundIssuesPairingForBlockedGroupSender(t *testing.T) {
	cleanup := setupClaudeAccessTest(t)
	defer cleanup()

	// 群聊 + 门禁激活（名单非空）+ 陌生人：首次触发发配对码。
	if _, err := AllowSender(context.Background(), 7002, "sender-a"); err != nil {
		t.Fatalf("AllowSender error = %v", err)
	}

	result, err := EvaluateInbound(context.Background(), 7002, "sender-b", "chat-b", 2)
	if err != nil {
		t.Fatalf("EvaluateInbound error = %v", err)
	}
	if result.Dispatch {
		t.Fatalf("Dispatch = true, want false")
	}
	if result.Reason != "pairing_required" {
		t.Fatalf("Reason = %q, want pairing_required", result.Reason)
	}
	if strings.TrimSpace(result.PairingCode) == "" {
		t.Fatal("PairingCode should not be empty")
	}

	status, err := GetStatus(context.Background(), 7002)
	if err != nil {
		t.Fatalf("GetStatus error = %v", err)
	}
	if status.PendingPairCount != 1 {
		t.Fatalf("PendingPairCount = %d, want 1", status.PendingPairCount)
	}
	if status.PendingPairs[0].Code != result.PairingCode {
		t.Fatalf("Pending pairing code = %q, want %q", status.PendingPairs[0].Code, result.PairingCode)
	}

	// 同一 sender+session 重复触发：不再刷卡片，返回 pairing_pending 且不新增 pending。
	repeat, err := EvaluateInbound(context.Background(), 7002, "sender-b", "chat-b", 2)
	if err != nil {
		t.Fatalf("EvaluateInbound(repeat) error = %v", err)
	}
	if repeat.Reason != "pairing_pending" {
		t.Fatalf("repeat Reason = %q, want pairing_pending", repeat.Reason)
	}
	if strings.TrimSpace(repeat.PairingCode) != "" {
		t.Fatalf("repeat PairingCode = %q, want empty", repeat.PairingCode)
	}
	status2, _ := GetStatus(context.Background(), 7002)
	if status2.PendingPairCount != 1 {
		t.Fatalf("PendingPairCount after repeat = %d, want 1 (no new code)", status2.PendingPairCount)
	}
}

func TestApproveAndDenyPairing(t *testing.T) {
	cleanup := setupClaudeAccessTest(t)
	defer cleanup()

	if _, err := AllowSender(context.Background(), 7003, "sender-a"); err != nil {
		t.Fatalf("AllowSender error = %v", err)
	}
	pair, err := EvaluateInbound(context.Background(), 7003, "sender-b", "chat-b", 2)
	if err != nil {
		t.Fatalf("EvaluateInbound error = %v", err)
	}
	if pair.PairingCode == "" {
		t.Fatal("PairingCode should not be empty")
	}

	approved, err := ApprovePairing(context.Background(), 7003, pair.PairingCode)
	if err != nil {
		t.Fatalf("ApprovePairing error = %v", err)
	}
	if approved.SenderID != "sender-b" {
		t.Fatalf("approved sender = %q, want sender-b", approved.SenderID)
	}

	status, err := GetStatus(context.Background(), 7003)
	if err != nil {
		t.Fatalf("GetStatus error = %v", err)
	}
	if status.AllowlistCount != 2 {
		t.Fatalf("AllowlistCount = %d, want 2", status.AllowlistCount)
	}
	if status.PendingPairCount != 0 {
		t.Fatalf("PendingPairCount = %d, want 0", status.PendingPairCount)
	}

	pairDenied, err := EvaluateInbound(context.Background(), 7003, "sender-c", "chat-c", 2)
	if err != nil {
		t.Fatalf("EvaluateInbound error = %v", err)
	}
	denied, err := DenyPairing(context.Background(), 7003, pairDenied.PairingCode)
	if err != nil {
		t.Fatalf("DenyPairing error = %v", err)
	}
	if denied.Code != pairDenied.PairingCode {
		t.Fatalf("denied code = %q, want %q", denied.Code, pairDenied.PairingCode)
	}
}

func TestBuildAccessCards(t *testing.T) {
	statusCard := BuildStatusCardMarkdown("Claude Grix access is currently disabled for this channel.", StatusWarning, "evt-1")
	if !strings.Contains(statusCard, "grix://card/agent_status?") {
		t.Fatalf("status card = %q", statusCard)
	}
	if !strings.Contains(statusCard, "category=access") {
		t.Fatalf("status card missing category: %q", statusCard)
	}

	pairingCard := BuildPairingCardMarkdown("PAIR123")
	if !strings.Contains(pairingCard, "grix://card/agent_pairing?") {
		t.Fatalf("pairing card = %q", pairingCard)
	}
	if !strings.Contains(pairingCard, "pairing_code=PAIR123") {
		t.Fatalf("pairing card missing code: %q", pairingCard)
	}
	if !strings.Contains(pairingCard, "command_hint=%2Fgrix+access+pair+%3Ccode%3E") {
		t.Fatalf("pairing card missing command hint: %q", pairingCard)
	}
}

func TestDenyPairingIsSticky(t *testing.T) {
	cleanup := setupClaudeAccessTest(t)
	defer cleanup()

	if _, err := AllowSender(context.Background(), 7006, "sender-a"); err != nil {
		t.Fatalf("AllowSender error = %v", err)
	}
	pair, err := EvaluateInbound(context.Background(), 7006, "sender-b", "chat-b", 2)
	if err != nil || pair.PairingCode == "" {
		t.Fatalf("EvaluateInbound error = %v code=%q", err, pair.PairingCode)
	}
	if _, err := DenyPairing(context.Background(), 7006, pair.PairingCode); err != nil {
		t.Fatalf("DenyPairing error = %v", err)
	}

	// 拒绝后再 @：粘性窗口内静默拦截，不生成新申请、不再打扰主人。
	repeat, err := EvaluateInbound(context.Background(), 7006, "sender-b", "chat-b", 2)
	if err != nil {
		t.Fatalf("EvaluateInbound(repeat) error = %v", err)
	}
	if repeat.Dispatch || repeat.Reason != "pairing_denied" || repeat.PairingCode != "" {
		t.Fatalf("repeat = %+v, want silent pairing_denied", repeat)
	}
	status, _ := GetStatus(context.Background(), 7006)
	if status.PendingPairCount != 0 {
		t.Fatalf("PendingPairCount = %d, want 0 (deny is sticky, no new request)", status.PendingPairCount)
	}

	// 主人手动 allow 解除粘性。
	if _, err := AllowSender(context.Background(), 7006, "sender-b"); err != nil {
		t.Fatalf("AllowSender error = %v", err)
	}
	after, err := EvaluateInbound(context.Background(), 7006, "sender-b", "chat-b", 2)
	if err != nil || !after.Dispatch {
		t.Fatalf("after allow = %+v err=%v, want dispatch", after, err)
	}
}

func TestCancelPendingLeavesNoStickiness(t *testing.T) {
	cleanup := setupClaudeAccessTest(t)
	defer cleanup()

	if _, err := AllowSender(context.Background(), 7007, "sender-a"); err != nil {
		t.Fatalf("AllowSender error = %v", err)
	}
	pair, err := EvaluateInbound(context.Background(), 7007, "sender-b", "chat-b", 2)
	if err != nil || pair.PairingCode == "" {
		t.Fatalf("EvaluateInbound error = %v", err)
	}
	if _, err := CancelPending(context.Background(), 7007, pair.PairingCode); err != nil {
		t.Fatalf("CancelPending error = %v", err)
	}
	// 回滚不是拒绝：再 @ 应重新生成申请（走通知重试路径）。
	retry, err := EvaluateInbound(context.Background(), 7007, "sender-b", "chat-b", 2)
	if err != nil {
		t.Fatalf("EvaluateInbound(retry) error = %v", err)
	}
	if retry.Reason != "pairing_required" || retry.PairingCode == "" {
		t.Fatalf("retry = %+v, want fresh pairing_required", retry)
	}
}


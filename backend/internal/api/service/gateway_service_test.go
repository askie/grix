package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupGatewayServiceTest(t *testing.T) {
	t.Helper()
	testDB := testutil.NewTestDB()
	t.Cleanup(testDB.Close)

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() { store.DB = originalDB })

	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake: %v", err)
	}
}

func TestGatewayCreateKey_AutoProvisionsWallet(t *testing.T) {
	setupGatewayServiceTest(t)

	resp, ec := GatewayCreateKey(2001, GatewayCreateKeyReq{Label: "desktop-test"})
	if ec != nil {
		t.Fatalf("GatewayCreateKey failed: %+v", ec)
	}
	if resp.VirtualKey == "" {
		t.Fatal("expected plaintext virtual key to be returned")
	}
	if resp.Key.Label != "desktop-test" {
		t.Fatalf("expected label 'desktop-test', got %q", resp.Key.Label)
	}

	list, ec := GatewayListKeys(2001)
	if ec != nil {
		t.Fatalf("GatewayListKeys failed: %+v", ec)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 key, got %d", len(list.Items))
	}

	w, ec := GatewayGetWallet(2001)
	if ec != nil {
		t.Fatalf("GatewayGetWallet failed: %+v", ec)
	}
	if w.OwnerID != 2001 {
		t.Fatalf("expected wallet owner_id 2001, got %d", w.OwnerID)
	}
}

func TestGatewayRevokeKey_ForbidsOtherUsersKey(t *testing.T) {
	setupGatewayServiceTest(t)

	created, ec := GatewayCreateKey(3001, GatewayCreateKeyReq{Label: "owner-key"})
	if ec != nil {
		t.Fatalf("GatewayCreateKey failed: %+v", ec)
	}

	// 另一个用户不能吊销别人的Key。
	ec = GatewayRevokeKey(9999, created.Key.ID)
	if ec == nil {
		t.Fatal("expected forbidden error when revoking another user's key")
	}
	if ec.BizCode != errcode.ErrGatewayKeyForbidden.BizCode {
		t.Fatalf("expected ErrGatewayKeyForbidden, got %+v", ec)
	}

	// 本人可以正常吊销。
	if ec := GatewayRevokeKey(3001, created.Key.ID); ec != nil {
		t.Fatalf("expected owner to revoke successfully, got %+v", ec)
	}
}

func TestGatewayRevokeKey_NotFound(t *testing.T) {
	setupGatewayServiceTest(t)

	ec := GatewayRevokeKey(4001, 999999999)
	if ec == nil {
		t.Fatal("expected not-found error for nonexistent key")
	}
	if ec.BizCode != errcode.ErrGatewayKeyNotFound.BizCode {
		t.Fatalf("expected ErrGatewayKeyNotFound, got %+v", ec)
	}
}

func TestGatewayListLedgerAndTopups_EmptyByDefault(t *testing.T) {
	setupGatewayServiceTest(t)

	ledger, ec := GatewayListLedger(5001, 1, 20)
	if ec != nil {
		t.Fatalf("GatewayListLedger failed: %+v", ec)
	}
	if ledger.Total != 0 || len(ledger.Items) != 0 {
		t.Fatalf("expected empty ledger, got total=%d items=%d", ledger.Total, len(ledger.Items))
	}

	topups, ec := GatewayListTopups(5001, 1, 20)
	if ec != nil {
		t.Fatalf("GatewayListTopups failed: %+v", ec)
	}
	if topups.Total != 0 || len(topups.Items) != 0 {
		t.Fatalf("expected empty topups, got total=%d items=%d", topups.Total, len(topups.Items))
	}
}

func setupGatewayConfigureAgentTest(t *testing.T) {
	t.Helper()
	setupGatewayServiceTest(t)

	originalRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = originalRDB
	})
}

func createTestAgent(t *testing.T, id, ownerID int64, clientType string) {
	t.Helper()
	agent := model.Agent{
		ID:              id,
		AgentName:       "test-agent",
		OwnerID:         ownerID,
		AgentClientType: clientType,
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create test agent: %v", err)
	}
}

func TestGatewayConfigureAgentProvider_IssuesKeyAndPublishes(t *testing.T) {
	setupGatewayConfigureAgentTest(t)
	createTestAgent(t, 6001, 6000, model.AgentClientTypeClaude)

	resp, ec := GatewayConfigureAgentProvider(6000, 6001, "https://grix.dhf.pub/anthropic", "https://grix.dhf.pub/openai", false)
	if ec != nil {
		t.Fatalf("GatewayConfigureAgentProvider failed: %+v", ec)
	}
	if resp.AlreadyConfigured {
		t.Fatal("expected first call to not be already-configured")
	}

	// 幂等：第二次调用不应该再发一把新Key。
	resp2, ec := GatewayConfigureAgentProvider(6000, 6001, "https://grix.dhf.pub/anthropic", "https://grix.dhf.pub/openai", false)
	if ec != nil {
		t.Fatalf("second GatewayConfigureAgentProvider failed: %+v", ec)
	}
	if !resp2.AlreadyConfigured {
		t.Fatal("expected second call to be already-configured (idempotent)")
	}

	keys, ec := GatewayListKeys(6000)
	if ec != nil {
		t.Fatalf("GatewayListKeys failed: %+v", ec)
	}
	if len(keys.Items) != 1 {
		t.Fatalf("expected exactly 1 key issued across both calls, got %d", len(keys.Items))
	}
}

// 已经由旧广播链路签发的 Key 没有 relay_model，因此也从未收到 direct_relay。
// direct 能力上线后再次“启用”必须重签一次，不能被旧的幂等短路永久卡在 MITM。
func TestGatewayConfigureAgentProvider_UpgradesLegacyCodexKeyToDirectRelay(t *testing.T) {
	setupGatewayConfigureAgentTest(t)
	setDirectRelayFlag(t, true)
	createTestAgent(t, 6051, 6050, model.AgentClientTypeCodex)
	seedGatewayServableModel(t, 605001, "deepseek-v4-pro")
	if _, ec := GatewayPutRelaySettings(6050, GatewayPutRelaySettingsReq{DefaultModel: "deepseek-v4-pro"}); ec != nil {
		t.Fatalf("save relay default: %+v", ec)
	}
	w, ec := ensureGatewayWallet(6050)
	if ec != nil {
		t.Fatalf("ensure wallet: %+v", ec)
	}
	if err := store.DB.Create(&model.GatewayVirtualKey{
		ID: 6051001, WalletID: w.ID, KeyHash: "legacy", KeyHint: "legacy",
		Status: model.GatewayVirtualKeyStatusActive, AgentID: 6051,
	}).Error; err != nil {
		t.Fatalf("seed legacy key: %v", err)
	}

	resp, ec := GatewayConfigureAgentProvider(6050, 6051, "https://gw/anthropic/v1", "https://gw/openai/v1", false)
	if ec != nil || resp.AlreadyConfigured {
		t.Fatalf("legacy direct upgrade must issue a replacement, resp=%+v ec=%+v", resp, ec)
	}
	keys, ec := GatewayListKeys(6050)
	if ec != nil {
		t.Fatalf("list keys: %+v", ec)
	}
	var active *model.GatewayVirtualKey
	for i := range keys.Items {
		if keys.Items[i].Status == model.GatewayVirtualKeyStatusActive {
			active = &keys.Items[i]
		}
	}
	if active == nil || active.RelayModel != "deepseek-v4-pro" {
		t.Fatalf("expected replacement key with direct-compatible model, got %+v", active)
	}
}

// resend=true 模拟"Key下发那次连接器没收到、库里却已经标成active"的场景：
// 客户端问connector确认它本地确实没有这把Key后，必须能拿到一把新Key重新走一次下发，
// 而不是被 resend=false 时的幂等短路卡死。
func TestGatewayConfigureAgentProvider_ResendReissuesKeyWhenAlreadyActive(t *testing.T) {
	setupGatewayConfigureAgentTest(t)
	createTestAgent(t, 6501, 6500, model.AgentClientTypeClaude)

	first, ec := GatewayConfigureAgentProvider(6500, 6501, "https://grix.dhf.pub/anthropic", "https://grix.dhf.pub/openai", false)
	if ec != nil {
		t.Fatalf("GatewayConfigureAgentProvider failed: %+v", ec)
	}
	if first.AlreadyConfigured {
		t.Fatal("expected first call to not be already-configured")
	}

	firstKeys, ec := GatewayListKeys(6500)
	if ec != nil {
		t.Fatalf("GatewayListKeys failed: %+v", ec)
	}
	if len(firstKeys.Items) != 1 {
		t.Fatalf("expected exactly 1 key after first call, got %d", len(firstKeys.Items))
	}
	firstKeyID := firstKeys.Items[0].ID

	// 不带 resend 的重试必须还是老的幂等短路行为，不受这次改动影响。
	resp, ec := GatewayConfigureAgentProvider(6500, 6501, "https://grix.dhf.pub/anthropic", "https://grix.dhf.pub/openai", false)
	if ec != nil {
		t.Fatalf("second GatewayConfigureAgentProvider failed: %+v", ec)
	}
	if !resp.AlreadyConfigured {
		t.Fatal("expected resend=false retry to be already-configured (idempotent)")
	}

	// resend=true：即使库里已经有 active Key，也要作废旧的、发一把新的、重新广播。
	resendResp, ec := GatewayConfigureAgentProvider(6500, 6501, "https://grix.dhf.pub/anthropic", "https://grix.dhf.pub/openai", true)
	if ec != nil {
		t.Fatalf("resend GatewayConfigureAgentProvider failed: %+v", ec)
	}
	if resendResp.AlreadyConfigured {
		t.Fatal("expected resend=true to issue a fresh key, not report already-configured")
	}

	allKeys, ec := GatewayListKeys(6500)
	if ec != nil {
		t.Fatalf("GatewayListKeys failed: %+v", ec)
	}
	if len(allKeys.Items) != 2 {
		t.Fatalf("expected old key revoked + 1 new key = 2 total keys, got %d", len(allKeys.Items))
	}
	activeCount := 0
	for _, k := range allKeys.Items {
		if k.ID == firstKeyID {
			if k.Status != model.GatewayVirtualKeyStatusRevoked {
				t.Fatalf("expected original key to be revoked after resend, got status %v", k.Status)
			}
			continue
		}
		if k.Status == model.GatewayVirtualKeyStatusActive {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active key after resend, got %d", activeCount)
	}
}

// resend=true 时旧Key必须撑到广播确认成功才吊销：如果广播失败，旧Key（此时还在正常
// 工作）不能陪着一起报废，否则一次网络抖动就把"resend"变成了"把agent的Key全弄没了"。
func TestGatewayConfigureAgentProvider_ResendKeepsOldKeyActiveWhenPublishFails(t *testing.T) {
	setupGatewayConfigureAgentTest(t)
	createTestAgent(t, 6601, 6600, model.AgentClientTypeClaude)

	first, ec := GatewayConfigureAgentProvider(6600, 6601, "https://grix.dhf.pub/anthropic", "https://grix.dhf.pub/openai", false)
	if ec != nil {
		t.Fatalf("GatewayConfigureAgentProvider failed: %+v", ec)
	}
	if first.AlreadyConfigured {
		t.Fatal("expected first call to not be already-configured")
	}
	firstKeys, ec := GatewayListKeys(6600)
	if ec != nil {
		t.Fatalf("GatewayListKeys failed: %+v", ec)
	}
	if len(firstKeys.Items) != 1 {
		t.Fatalf("expected exactly 1 key after first call, got %d", len(firstKeys.Items))
	}
	originalKeyID := firstKeys.Items[0].ID

	workingRDB := store.RDB
	store.RDB = nil // 模拟 resend 时广播失败（Redis不可用）
	ec = errCodeOf(GatewayConfigureAgentProvider(6600, 6601, "https://grix.dhf.pub/anthropic", "https://grix.dhf.pub/openai", true))
	if ec == nil {
		t.Fatal("expected publish failure to bubble up as an error")
	}
	store.RDB = workingRDB

	keys, ec := GatewayListKeys(6600)
	if ec != nil {
		t.Fatalf("GatewayListKeys failed: %+v", ec)
	}
	activeCount := 0
	originalStillActive := false
	for _, k := range keys.Items {
		if k.Status == model.GatewayVirtualKeyStatusActive {
			activeCount++
			if k.ID == originalKeyID {
				originalStillActive = true
			}
		}
	}
	if activeCount != 1 || !originalStillActive {
		t.Fatalf("expected the original key to remain the sole active key after a failed resend, got %d active (original still active=%v)", activeCount, originalStillActive)
	}
}

// resend=true 但库里此时没有任何 active Key（比如上一次广播失败已经把新旧Key都回滚掉了，
// 用户再点一次）：必须能正常走首次签发分支，不能因为找不到"旧Key"就出错。
func TestGatewayConfigureAgentProvider_ResendFallsBackToFirstIssueWhenNoActiveKey(t *testing.T) {
	setupGatewayConfigureAgentTest(t)
	createTestAgent(t, 6701, 6700, model.AgentClientTypeClaude)

	resp, ec := GatewayConfigureAgentProvider(6700, 6701, "https://grix.dhf.pub/anthropic", "https://grix.dhf.pub/openai", true)
	if ec != nil {
		t.Fatalf("resend on an agent with no existing key should succeed, got %+v", ec)
	}
	if resp.AlreadyConfigured {
		t.Fatal("expected a fresh key to be issued, not already-configured")
	}

	keys, ec := GatewayListKeys(6700)
	if ec != nil {
		t.Fatalf("GatewayListKeys failed: %+v", ec)
	}
	activeCount := 0
	for _, k := range keys.Items {
		if k.Status == model.GatewayVirtualKeyStatusActive {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active key, got %d", activeCount)
	}
}

func TestGatewayConfigureAgentProvider_RollsBackKeyWhenPublishFails(t *testing.T) {
	setupGatewayConfigureAgentTest(t)
	createTestAgent(t, 6301, 6300, model.AgentClientTypeClaude)

	workingRDB := store.RDB
	store.RDB = nil // 模拟广播失败（Redis不可用）
	ec := errCodeOf(GatewayConfigureAgentProvider(6300, 6301, "https://grix.dhf.pub/anthropic", "https://grix.dhf.pub/openai", false))
	if ec == nil {
		t.Fatal("expected publish failure to bubble up as an error")
	}
	store.RDB = workingRDB

	// 广播失败后必须把刚发的Key回滚，否则重试会因为查到"已有active Key"被幂等跳过，
	// connector 永远拿不到明文Key，Agent就卡死在"库里已配、实际没配上"。
	resp, ec := GatewayConfigureAgentProvider(6300, 6301, "https://grix.dhf.pub/anthropic", "https://grix.dhf.pub/openai", false)
	if ec != nil {
		t.Fatalf("retry after rollback should succeed, got %+v", ec)
	}
	if resp.AlreadyConfigured {
		t.Fatal("expected retry to issue a fresh key, not be treated as already-configured")
	}

	keys, ec := GatewayListKeys(6300)
	if ec != nil {
		t.Fatalf("GatewayListKeys failed: %+v", ec)
	}
	activeCount := 0
	for _, k := range keys.Items {
		if k.Status == model.GatewayVirtualKeyStatusActive {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active key after rollback+retry, got %d (total keys=%d)", activeCount, len(keys.Items))
	}
}

func TestGatewayConfigureAgentProvider_ForbidsOtherUsersAgent(t *testing.T) {
	setupGatewayConfigureAgentTest(t)
	createTestAgent(t, 6101, 6100, model.AgentClientTypeCodex)

	ec := errCodeOf(GatewayConfigureAgentProvider(9999, 6101, "", "https://grix.dhf.pub/openai", false))
	if ec == nil || ec.BizCode != errcode.ErrAgentForbidden.BizCode {
		t.Fatalf("expected ErrAgentForbidden, got %+v", ec)
	}
}

func TestGatewayConfigureAgentProvider_NotFound(t *testing.T) {
	setupGatewayConfigureAgentTest(t)

	ec := errCodeOf(GatewayConfigureAgentProvider(1, 999999999, "", "https://grix.dhf.pub/openai", false))
	if ec == nil || ec.BizCode != errcode.ErrAgentNotFound.BizCode {
		t.Fatalf("expected ErrAgentNotFound, got %+v", ec)
	}
}

func TestGatewayConfigureAgentProvider_UnsupportedClientType(t *testing.T) {
	setupGatewayConfigureAgentTest(t)
	createTestAgent(t, 6201, 6200, model.AgentClientTypeGemini)

	ec := errCodeOf(GatewayConfigureAgentProvider(6200, 6201, "", "https://grix.dhf.pub/openai", false))
	if ec == nil || ec.BizCode != errcode.ErrGatewayUnsupportedClientType.BizCode {
		t.Fatalf("expected ErrGatewayUnsupportedClientType, got %+v", ec)
	}
}

// 已删除的 Agent（软删除，status=3）不能再签出中转虚拟Key——否则拿一个已删 agent 的 ID
// 照样能签一把 active Key 并广播给 connector，即便列表接口已经把它过滤掉了。
func TestGatewayConfigureAgentProvider_DeletedAgentNotFound(t *testing.T) {
	setupGatewayConfigureAgentTest(t)
	createTestAgent(t, 6401, 6400, model.AgentClientTypeClaude)
	if err := store.DB.Model(&model.Agent{}).Where("id = ?", 6401).Update("status", 3).Error; err != nil {
		t.Fatalf("mark agent deleted: %v", err)
	}

	ec := errCodeOf(GatewayConfigureAgentProvider(6400, 6401, "", "https://grix.dhf.pub/openai", false))
	if ec == nil || ec.BizCode != errcode.ErrAgentNotFound.BizCode {
		t.Fatalf("expected ErrAgentNotFound for deleted agent, got %+v", ec)
	}
}

func errCodeOf(_ *GatewayConfigureAgentProviderResp, ec *errcode.ErrCode) *errcode.ErrCode {
	return ec
}

func errCodeOfCredential(_ *GatewayIssueAgentRelayCredentialResp, ec *errcode.ErrCode) *errcode.ErrCode {
	return ec
}

// GatewayIssueAgentRelayCredential 是桌面端直连本地Connector改造的新签发入口：不经Redis/WS
// 广播，直接在HTTP响应里把明文Key和两个协议地址一起返回，所以这里特意不给 store.RDB 置空、
// 也不接 mock Redis ——用 setupGatewayServiceTest 而不是 setupGatewayConfigureAgentTest，
// 证明这条路径完全不依赖 Redis 广播就能正常签发成功。
func TestGatewayIssueAgentRelayCredential_IssuesPlaintextKeyWithoutBroadcast(t *testing.T) {
	setupGatewayServiceTest(t)
	createTestAgent(t, 7001, 7000, model.AgentClientTypeClaude)

	resp, ec := GatewayIssueAgentRelayCredential(7000, 7001, "https://grix.dhf.pub/anthropic/v1", "https://grix.dhf.pub/openai/v1", "")
	if ec != nil {
		t.Fatalf("GatewayIssueAgentRelayCredential failed: %+v", ec)
	}
	if resp.VirtualKey == "" {
		t.Fatal("expected plaintext virtual key to be returned")
	}
	if resp.AnthropicBaseURL != "https://grix.dhf.pub/anthropic/v1" || resp.OpenAIBaseURL != "https://grix.dhf.pub/openai/v1" {
		t.Fatalf("expected base URLs to be passed through unchanged, got %+v", resp)
	}

	keys, ec2 := GatewayListKeys(7000)
	if ec2 != nil {
		t.Fatalf("GatewayListKeys failed: %+v", ec2)
	}
	if len(keys.Items) != 1 || keys.Items[0].Status != model.GatewayVirtualKeyStatusActive {
		t.Fatalf("expected exactly 1 active key issued, got %+v", keys.Items)
	}
}

// 跟旧接口的幂等短路不同：新接口每次调用都直接签一把全新Key并吊销旧的——因为HTTP响应本身
// 就是可靠交付，不存在"广播出去但可能没送达"的问题，调用方决定要不要重新申请。
func TestGatewayIssueAgentRelayCredential_ReissuesFreshKeyEveryCall(t *testing.T) {
	setupGatewayServiceTest(t)
	createTestAgent(t, 7101, 7100, model.AgentClientTypeClaude)

	first, ec := GatewayIssueAgentRelayCredential(7100, 7101, "", "", "")
	if ec != nil {
		t.Fatalf("first GatewayIssueAgentRelayCredential failed: %+v", ec)
	}
	second, ec := GatewayIssueAgentRelayCredential(7100, 7101, "", "", "")
	if ec != nil {
		t.Fatalf("second GatewayIssueAgentRelayCredential failed: %+v", ec)
	}
	if first.VirtualKey == second.VirtualKey {
		t.Fatal("expected each call to issue a distinct plaintext key")
	}

	keys, ec2 := GatewayListKeys(7100)
	if ec2 != nil {
		t.Fatalf("GatewayListKeys failed: %+v", ec2)
	}
	activeCount := 0
	for _, k := range keys.Items {
		if k.Status == model.GatewayVirtualKeyStatusActive {
			activeCount++
		}
	}
	if len(keys.Items) != 2 {
		t.Fatalf("expected 2 keys total (old revoked + new active), got %d", len(keys.Items))
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active key after reissue, got %d active among %+v", activeCount, keys.Items)
	}
}

func TestGatewayIssueAgentRelayCredential_ForbidsOtherUsersAgent(t *testing.T) {
	setupGatewayServiceTest(t)
	createTestAgent(t, 7201, 7200, model.AgentClientTypeCodex)

	ec := errCodeOfCredential(GatewayIssueAgentRelayCredential(9999, 7201, "", "", ""))
	if ec == nil || ec.BizCode != errcode.ErrAgentForbidden.BizCode {
		t.Fatalf("expected ErrAgentForbidden, got %+v", ec)
	}
}

func TestGatewayIssueAgentRelayCredential_NotFound(t *testing.T) {
	setupGatewayServiceTest(t)

	ec := errCodeOfCredential(GatewayIssueAgentRelayCredential(1, 999999999, "", "", ""))
	if ec == nil || ec.BizCode != errcode.ErrAgentNotFound.BizCode {
		t.Fatalf("expected ErrAgentNotFound, got %+v", ec)
	}
}

func TestGatewayIssueAgentRelayCredential_UnsupportedClientType(t *testing.T) {
	setupGatewayServiceTest(t)
	createTestAgent(t, 7301, 7300, model.AgentClientTypeGemini)

	ec := errCodeOfCredential(GatewayIssueAgentRelayCredential(7300, 7301, "", "", ""))
	if ec == nil || ec.BizCode != errcode.ErrGatewayUnsupportedClientType.BizCode {
		t.Fatalf("expected ErrGatewayUnsupportedClientType, got %+v", ec)
	}
}

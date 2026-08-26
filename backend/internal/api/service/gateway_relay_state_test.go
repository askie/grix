package service

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
)

// setGatewayRelayStateFlag 在测试内翻转 gateway.relay_state_enabled（config.C 是全局零值，
// 测试进程里默认 false，用例必须显式置位并复原）。
func setGatewayRelayStateFlag(t *testing.T, enabled bool) {
	t.Helper()
	original := config.C.Gateway.RelayStateEnabled
	config.C.Gateway.RelayStateEnabled = enabled
	t.Cleanup(func() { config.C.Gateway.RelayStateEnabled = original })
}

// 归属校验：他人的 agent 与根本不存在的 agent 一样返回 404，不泄露存在性（设计 §2.3/§2.6）。
func TestGatewaySetAgentRelay_ForbidsOtherUsersAgent(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8601, 8600, model.AgentClientTypeClaude)

	// 他人 agent → 404。
	_, ec := GatewaySetAgentRelay(context.Background(), 9999, 8601, true, "", nil)
	if ec == nil || ec.BizCode != errcode.ErrAgentNotFound.BizCode || ec.HTTPStatus != 404 {
		t.Fatalf("expected 404 ErrAgentNotFound for other's agent, got %+v", ec)
	}

	// 不存在的 agent → 同样 404。
	_, ec = GatewaySetAgentRelay(context.Background(), 8600, 987654321, true, "", nil)
	if ec == nil || ec.BizCode != errcode.ErrAgentNotFound.BizCode {
		t.Fatalf("expected 404 ErrAgentNotFound for missing agent, got %+v", ec)
	}
}

// model 非空时必须在"后端支持的模型"清单内（复用 ErrGatewayModelNotServable 口径）。
func TestGatewaySetAgentRelay_RejectsUnservableModel(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8701, 8700, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 870001, "deepseek-v4-flash")

	_, ec := GatewaySetAgentRelay(context.Background(), 8700, 8701, true, "no-such-model", nil)
	if ec == nil || ec.BizCode != errcode.ErrGatewayModelNotServable.BizCode {
		t.Fatalf("expected ErrGatewayModelNotServable, got %+v", ec)
	}
}

// 原生配置类型开启时 model 必填（400 need_model 语义）；MITM 类型不带 model 可开。
func TestGatewaySetAgentRelay_NativeTypeRequiresModel(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8801, 8800, model.AgentClientTypeQwen)
	createTestAgent(t, 8802, 8800, model.AgentClientTypeClaude)

	_, ec := GatewaySetAgentRelay(context.Background(), 8800, 8801, true, "", nil)
	if ec == nil || ec.BizCode != errcode.ErrGatewayRelayModelRequired.BizCode || ec.HTTPStatus != 400 {
		t.Fatalf("expected 400 ErrGatewayRelayModelRequired, got %+v", ec)
	}

	// 原生类型关闭时不需要 model。
	if _, ec = GatewaySetAgentRelay(context.Background(), 8800, 8801, false, "", nil); ec != nil {
		t.Fatalf("disable native type without model should succeed, got %+v", ec)
	}
	// MITM 类型开启不带 model 走映射/兜底，合法。
	if _, ec = GatewaySetAgentRelay(context.Background(), 8800, 8802, true, "", nil); ec != nil {
		t.Fatalf("enable MITM type without model should succeed, got %+v", ec)
	}
}

// 写前隐式建钱包（state 表 wallet_id 非空，设计 §2.2 评审#6），返回最新 state。
func TestGatewaySetAgentRelay_AutoProvisionsWallet(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8901, 8900, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 890001, "deepseek-v4-flash")

	resp, ec := GatewaySetAgentRelay(context.Background(), 8900, 8901, true, "deepseek-v4-flash", nil)
	if ec != nil {
		t.Fatalf("GatewaySetAgentRelay failed: %+v", ec)
	}
	if !resp.Enabled || resp.RelayModel != "deepseek-v4-flash" || resp.Revision != 1 || resp.Applied {
		t.Fatalf("unexpected state resp: %+v", resp)
	}

	w, ec := GatewayGetWallet(8900)
	if ec != nil {
		t.Fatalf("expected wallet auto-provisioned, got %+v", ec)
	}
	row, err := store.GetGatewayAgentRelayState(8901)
	if err != nil {
		t.Fatalf("expected state row persisted: %v", err)
	}
	if row.WalletID != w.ID {
		t.Fatalf("expected state wallet_id %d, got %d", w.ID, row.WalletID)
	}
}

// 乐观锁：expected_revision 不一致返回 409 并带回最新 state；不传则 last-write-wins。
func TestGatewaySetAgentRelay_RevisionConflict(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9001, 9000, model.AgentClientTypeClaude)

	resp, ec := GatewaySetAgentRelay(context.Background(), 9000, 9001, true, "", nil)
	if ec != nil || resp.Revision != 1 {
		t.Fatalf("first set failed: resp=%+v ec=%+v", resp, ec)
	}

	expected := int64(1)
	resp, ec = GatewaySetAgentRelay(context.Background(), 9000, 9001, false, "", &expected)
	if ec != nil || resp.Revision != 2 || resp.Enabled {
		t.Fatalf("expected-revision set failed: resp=%+v ec=%+v", resp, ec)
	}

	// 同一 revision 已被消费 → 409，并带回最新 state（revision 2）供前端刷新重试。
	conflict, ec := GatewaySetAgentRelay(context.Background(), 9000, 9001, true, "", &expected)
	if ec == nil || ec.BizCode != errcode.ErrGatewayRelayStateConflict.BizCode || ec.HTTPStatus != 409 {
		t.Fatalf("expected 409 ErrGatewayRelayStateConflict, got %+v", ec)
	}
	if conflict == nil || conflict.Revision != 2 || conflict.Enabled {
		t.Fatalf("expected latest state carried with 409, got %+v", conflict)
	}

	// 不传 expected_revision → last-write-wins 覆盖。
	resp, ec = GatewaySetAgentRelay(context.Background(), 9000, 9001, true, "", nil)
	if ec != nil || resp.Revision != 3 || !resp.Enabled {
		t.Fatalf("last-write-wins set failed: resp=%+v ec=%+v", resp, ec)
	}
}

// ListAgents 扩展字段：desired 以 state 表为唯一权威（盖过 Key 快照），actual 随回执变化；
// state 表无行时 enabled=false、relay_model 回退最新活跃 Key 的 relay_model。
func TestGatewayListAgents_RelayStateFields(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9101, 9100, model.AgentClientTypeQwen)
	createTestAgent(t, 9102, 9100, model.AgentClientTypeClaude)
	seedGatewayServableModel(t, 910001, "deepseek-v4-flash")

	w, ec := ensureGatewayWallet(9100)
	if ec != nil {
		t.Fatalf("ensure wallet: %+v", ec)
	}
	// 9101 有一把旧 Key（快照模型 key-snapshot-model），desired 必须盖过它；
	// 9102 没有 state 行，relay_model 回退 Key 快照、enabled=false。
	for _, k := range []struct {
		id      int64
		agentID int64
		m       string
	}{{9101001, 9101, "key-snapshot-model"}, {9101002, 9102, "fallback-key-model"}} {
		if err := store.DB.Create(&model.GatewayVirtualKey{
			ID: k.id, WalletID: w.ID, KeyHash: k.m, KeyHint: "hint",
			Status: model.GatewayVirtualKeyStatusActive, AgentID: k.agentID, RelayModel: k.m,
		}).Error; err != nil {
			t.Fatalf("seed key: %v", err)
		}
	}

	if _, ec = GatewaySetAgentRelay(context.Background(), 9100, 9101, true, "deepseek-v4-flash", nil); ec != nil {
		t.Fatalf("GatewaySetAgentRelay failed: %+v", ec)
	}

	resp, ec := GatewayListAgents(9100)
	if ec != nil {
		t.Fatalf("GatewayListAgents failed: %+v", ec)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %+v", resp.Items)
	}
	byID := map[int64]GatewayAgentRelayState{}
	for _, it := range resp.Items {
		byID[it.AgentID] = it
	}

	withState := byID[9101]
	if withState.Enabled == nil || !*withState.Enabled {
		t.Fatalf("expected enabled=true for 9101, got %+v", withState)
	}
	if withState.Applied == nil || *withState.Applied {
		t.Fatalf("expected applied=false for 9101, got %+v", withState)
	}
	if withState.StateKnown == nil || *withState.StateKnown {
		t.Fatalf("expected state_known=false (no WS route in test), got %+v", withState)
	}
	if withState.AppliedAt != nil {
		t.Fatalf("expected nil applied_at before any receipt, got %+v", withState.AppliedAt)
	}
	if withState.RelayModel != "deepseek-v4-flash" {
		t.Fatalf("relay_model must be desired, not key snapshot: got %q", withState.RelayModel)
	}
	if !withState.Configured {
		t.Fatalf("configured 保留（有活跃 Key），got %+v", withState)
	}

	noState := byID[9102]
	if noState.Enabled == nil || *noState.Enabled {
		t.Fatalf("expected enabled=false for stateless agent, got %+v", noState)
	}
	if noState.RelayModel != "fallback-key-model" {
		t.Fatalf("stateless agent relay_model must fall back to key snapshot, got %q", noState.RelayModel)
	}

	// actual 回执写回后，列表随之变化。
	appliedAt := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	if err := store.SetGatewayAgentRelayStateApplied(9101, true, appliedAt); err != nil {
		t.Fatalf("SetApplied: %v", err)
	}
	resp, ec = GatewayListAgents(9100)
	if ec != nil {
		t.Fatalf("GatewayListAgents after applied failed: %+v", ec)
	}
	for _, it := range resp.Items {
		if it.AgentID != 9101 {
			continue
		}
		if it.Applied == nil || !*it.Applied || it.AppliedAt == nil || !appliedAt.Equal(*it.AppliedAt) {
			t.Fatalf("expected applied=true with applied_at, got %+v", it)
		}
	}
}

// feature flag 关闭：POST 返回 503；GET /agents 不读 state 表，响应回落旧语义
// （即使 state 行存在也不返回扩展字段，relay_model 回到 Key 快照口径）。
func TestGatewayRelayState_FlagDisabled(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9201, 9200, model.AgentClientTypeClaude)

	// 先在 flag 开启时落一条 desired（enabled=true），并留一把 Key 快照。
	if _, ec := GatewaySetAgentRelay(context.Background(), 9200, 9201, true, "", nil); ec != nil {
		t.Fatalf("seed desired failed: %+v", ec)
	}
	w, ec := ensureGatewayWallet(9200)
	if ec != nil {
		t.Fatalf("ensure wallet: %+v", ec)
	}
	if err := store.DB.Create(&model.GatewayVirtualKey{
		ID: 9201001, WalletID: w.ID, KeyHash: "h", KeyHint: "hint",
		Status: model.GatewayVirtualKeyStatusActive, AgentID: 9201, RelayModel: "key-snapshot-model",
	}).Error; err != nil {
		t.Fatalf("seed key: %v", err)
	}

	setGatewayRelayStateFlag(t, false)

	_, ec = GatewaySetAgentRelay(context.Background(), 9200, 9201, false, "", nil)
	if ec == nil || ec.BizCode != errcode.ErrGatewayRelayStateDisabled.BizCode || ec.HTTPStatus != 503 {
		t.Fatalf("expected 503 ErrGatewayRelayStateDisabled, got %+v", ec)
	}

	resp, ec := GatewayListAgents(9200)
	if ec != nil {
		t.Fatalf("GatewayListAgents failed: %+v", ec)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %+v", resp.Items)
	}
	it := resp.Items[0]
	if it.Enabled != nil || it.Applied != nil || it.StateKnown != nil || it.AppliedAt != nil {
		t.Fatalf("flag off must not return relay state fields, got %+v", it)
	}
	if it.RelayModel != "key-snapshot-model" {
		t.Fatalf("flag off relay_model must fall back to key snapshot, got %q", it.RelayModel)
	}
	if !it.Configured {
		t.Fatalf("configured semantics preserved under flag off, got %+v", it)
	}
}

// host_name 取自 connector 上报并落在 agents.config 里的 host_meta.hostname：
// 有值时如实带出（供前端按机器归类同名 Agent），没上报过或 config 不可解析时给空串。
func TestGatewayListAgents_HostNameFromHostMeta(t *testing.T) {
	setupGatewayServiceTest(t)
	createTestAgent(t, 9301, 9300, model.AgentClientTypeClaude)
	createTestAgent(t, 9302, 9300, model.AgentClientTypeClaude)
	createTestAgent(t, 9303, 9300, model.AgentClientTypeClaude)

	// 9301：正常上报过 host_meta；9302：config 里没有 host_meta；9303：config 是坏 JSON。
	setAgentConfigJSON(t, 9301, `{"host_meta":{"hostname":" gcf-Mac-mini.local ","platform":"darwin"}}`)
	setAgentConfigJSON(t, 9302, `{"other":1}`)
	setAgentConfigJSON(t, 9303, `not-json`)

	resp, ec := GatewayListAgents(9300)
	if ec != nil {
		t.Fatalf("GatewayListAgents failed: %+v", ec)
	}
	hosts := make(map[int64]string, len(resp.Items))
	for _, it := range resp.Items {
		hosts[it.AgentID] = it.HostName
	}
	if hosts[9301] != "gcf-Mac-mini.local" {
		t.Fatalf("expected trimmed hostname for 9301, got %q", hosts[9301])
	}
	if hosts[9302] != "" {
		t.Fatalf("expected empty host_name without host_meta, got %q", hosts[9302])
	}
	if hosts[9303] != "" {
		t.Fatalf("expected empty host_name for unparsable config, got %q", hosts[9303])
	}
}

func setAgentConfigJSON(t *testing.T, agentID int64, raw string) {
	t.Helper()
	if err := store.DB.Model(&model.Agent{}).
		Where("id = ?", agentID).
		Update("config", raw).Error; err != nil {
		t.Fatalf("set agent config: %v", err)
	}
}

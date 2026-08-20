package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/askie/grix/backend/internal/gateway/provisioning"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
)

// relay_state_sync / relay_state_report / apply_relay_state 回执的业务规则测试
// （设计 §2.4，v5：事件驱动，无心跳）。WS 协议适配层（seq 关联、限流、广播契约）
// 的测试在 ws/agentapi/relay_state_handler_test.go。

func boolPtr(v bool) *bool { return &v }

// 首报落库：state 行不存在且上报了 local_enabled 时，以首个上报设备的本机名单落
// initial desired；enabled=true 且无有效 Key 时顺带签发凭证随应答下发。
func TestGatewayRelayStateSync_FirstReportSeedsInitialDesired(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9301, 9300, model.AgentClientTypeClaude)

	resp, ec := GatewayRelayStateSync(9300, 9301, boolPtr(true), "", "", "")
	if ec != nil {
		t.Fatalf("sync failed: %+v", ec)
	}
	if !resp.Enabled || resp.Revision != 1 || resp.ModelAutoFilled {
		t.Fatalf("unexpected sync resp: %+v", resp)
	}
	if resp.Credential == nil || resp.Credential.VirtualKey == "" {
		t.Fatalf("enabled without active key must reissue credential, got %+v", resp.Credential)
	}

	row, err := store.GetGatewayAgentRelayState(9301)
	if err != nil {
		t.Fatalf("expected state row persisted: %v", err)
	}
	if !row.Enabled || row.Revision != 1 || row.Applied {
		t.Fatalf("unexpected state row: %+v", row)
	}
}

// 同步顺带签发时，connector 自报的入口地址必须一路带到 direct_relay；否则 Codex
// capability 只能 unsupported，连接器会回退到 MITM 并继续命中本机 cc-switch 配置。
func TestGatewayRelayStateSync_InlineCredentialCarriesCodexDirectRelay(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	setDirectRelayFlag(t, true)
	createTestAgent(t, 9351, 9350, model.AgentClientTypeCodex)
	seedGatewayServableModel(t, 935001, "deepseek-v4-pro")

	if _, ec := GatewaySetAgentRelay(context.Background(), 9350, 9351, true, "deepseek-v4-pro", nil); ec != nil {
		t.Fatalf("set desired: %+v", ec)
	}
	resp, ec := GatewayRelayStateSync(
		9350, 9351, nil, "", "https://gw.example/anthropic/v1", "https://gw.example/openai/v1",
	)
	if ec != nil {
		t.Fatalf("sync failed: %+v", ec)
	}
	if resp.Credential == nil || resp.Credential.DirectRelay == nil || resp.Credential.DirectRelay.Codex == nil {
		t.Fatalf("expected inline Codex direct_relay capability, got %+v", resp.Credential)
	}
	if !resp.Credential.DirectRelay.Codex.Supported || resp.Credential.DirectRelay.Codex.BaseURL != "https://gw.example/openai" {
		t.Fatalf("unexpected Codex direct capability: %+v", resp.Credential.DirectRelay.Codex)
	}
}

// 二报忽略：state 行已存在时忽略 local_enabled/local_model，以服务端 desired 为准，
// 本机名单不再回写（多设备互不翻转）。desired model 与最新活跃 Key 一致时不重签。
func TestGatewayRelayStateSync_SecondReportIgnored(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9401, 9400, model.AgentClientTypeClaude)

	first, ec := GatewayRelayStateSync(9400, 9401, boolPtr(true), "", "", "")
	if ec != nil || !first.Enabled {
		t.Fatalf("first sync failed: resp=%+v ec=%+v", first, ec)
	}

	// 另一台设备（或本机名单相反的旧端）上报 local_enabled=false：必须被忽略。
	resp, ec := GatewayRelayStateSync(9400, 9401, boolPtr(false), "whatever-model", "", "")
	if ec != nil {
		t.Fatalf("second sync failed: %+v", ec)
	}
	if !resp.Enabled || resp.Revision != 1 {
		t.Fatalf("existing desired must win over local report, got %+v", resp)
	}
	// 首报已签发 Key 且 model 口径一致（均为空）：不再重签。
	if resp.Credential != nil {
		t.Fatalf("no reissue expected when desired matches active key, got %+v", resp.Credential)
	}
	row, err := store.GetGatewayAgentRelayState(9401)
	if err != nil || !row.Enabled {
		t.Fatalf("local report must not flip desired, row=%+v err=%v", row, err)
	}
}

// 未上报本机名单（local_enabled 缺失）且 state 行不存在：不建行，desired 默认关。
func TestGatewayRelayStateSync_NoLocalReportNoRow(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9501, 9500, model.AgentClientTypeClaude)

	resp, ec := GatewayRelayStateSync(9500, 9501, nil, "", "", "")
	if ec != nil {
		t.Fatalf("sync failed: %+v", ec)
	}
	if resp.Enabled || resp.Revision != 0 || resp.Credential != nil {
		t.Fatalf("expected disabled empty desired, got %+v", resp)
	}
	if _, err := store.GetGatewayAgentRelayState(9501); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("no state row should be created, err=%v", err)
	}
}

// 原生类型回填分支一：local_enabled=true 且缺 model 时，先取最新活跃 Key 的
// relay_model 回填；desired 与 Key 一致，不再重签。
func TestGatewayRelayStateSync_NativeBackfillFromActiveKey(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9601, 9600, model.AgentClientTypeQwen)

	w, ec := ensureGatewayWallet(9600)
	if ec != nil {
		t.Fatalf("ensure wallet: %+v", ec)
	}
	if err := store.DB.Create(&model.GatewayVirtualKey{
		ID: 9601001, WalletID: w.ID, KeyHash: "h", KeyHint: "hint",
		Status: model.GatewayVirtualKeyStatusActive, AgentID: 9601, RelayModel: "key-snapshot-model",
	}).Error; err != nil {
		t.Fatalf("seed key: %v", err)
	}

	resp, ec := GatewayRelayStateSync(9600, 9601, boolPtr(true), "", "", "")
	if ec != nil {
		t.Fatalf("sync failed: %+v", ec)
	}
	if resp.Model != "key-snapshot-model" || !resp.ModelAutoFilled {
		t.Fatalf("expected backfill from active key, got %+v", resp)
	}
	if resp.Credential != nil {
		t.Fatalf("no reissue expected when desired matches active key, got %+v", resp.Credential)
	}
	row, err := store.GetGatewayAgentRelayState(9601)
	if err != nil || row.RelayModel != "key-snapshot-model" {
		t.Fatalf("desired model must be backfilled, row=%+v err=%v", row, err)
	}
}

// 原生类型回填分支二：无活跃 Key 时回退钱包级 default_model（测试库未配系统默认，
// 落内置兜底 deepseek-v4-flash），并因"无有效 Key"顺带重签。
func TestGatewayRelayStateSync_NativeBackfillFallsBackToDefaultModel(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9701, 9700, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 970001, "deepseek-v4-flash")

	resp, ec := GatewayRelayStateSync(9700, 9701, boolPtr(true), "", "", "")
	if ec != nil {
		t.Fatalf("sync failed: %+v", ec)
	}
	if resp.Model != "deepseek-v4-flash" || !resp.ModelAutoFilled {
		t.Fatalf("expected default_model backfill, got %+v", resp)
	}
	if resp.Credential == nil || resp.Credential.RelayModel != "deepseek-v4-flash" {
		t.Fatalf("expected reissue with backfilled model, got %+v", resp.Credential)
	}
}

// 原生类型回填分支三：connector 自报了 local_model，直接采用，不触发回填。
func TestGatewayRelayStateSync_NativeLocalModelProvided(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9801, 9800, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 980001, "deepseek-v4-flash")

	resp, ec := GatewayRelayStateSync(9800, 9801, boolPtr(true), "deepseek-v4-flash", "", "")
	if ec != nil {
		t.Fatalf("sync failed: %+v", ec)
	}
	if resp.Model != "deepseek-v4-flash" || resp.ModelAutoFilled {
		t.Fatalf("local_model must be used as-is, got %+v", resp)
	}
}

// 重签触发：desired model 与最新活跃 Key 的 relay_model 不一致时，sync 顺带重签
// （吊销旧 Key），应答携带新凭证。
func TestGatewayRelayStateSync_ReissuesOnModelDrift(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9901, 9900, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 990001, "deepseek-v4-flash")

	w, ec := ensureGatewayWallet(9900)
	if ec != nil {
		t.Fatalf("ensure wallet: %+v", ec)
	}
	if err := store.DB.Create(&model.GatewayVirtualKey{
		ID: 9901001, WalletID: w.ID, KeyHash: "old", KeyHint: "hint",
		Status: model.GatewayVirtualKeyStatusActive, AgentID: 9901, RelayModel: "old-model",
	}).Error; err != nil {
		t.Fatalf("seed key: %v", err)
	}
	if _, ec = GatewaySetAgentRelay(context.Background(), 9900, 9901, true, "deepseek-v4-flash", nil); ec != nil {
		t.Fatalf("set desired failed: %+v", ec)
	}

	resp, ec := GatewayRelayStateSync(9900, 9901, nil, "", "", "")
	if ec != nil {
		t.Fatalf("sync failed: %+v", ec)
	}
	if resp.Credential == nil || resp.Credential.RelayModel != "deepseek-v4-flash" {
		t.Fatalf("model drift must trigger reissue, got %+v", resp.Credential)
	}
	// 旧 Key 被吊销，最新活跃 Key 的 model 与 desired 对齐；再 sync 不再重签。
	keyModel, hasKey := gatewayLatestActiveKeyRelayModel(w.ID, 9901)
	if !hasKey || keyModel != "deepseek-v4-flash" {
		t.Fatalf("latest active key model=%q hasKey=%v, want deepseek-v4-flash", keyModel, hasKey)
	}
	again, ec := GatewayRelayStateSync(9900, 9901, nil, "", "", "")
	if ec != nil {
		t.Fatalf("second sync failed: %+v", ec)
	}
	if again.Credential != nil {
		t.Fatalf("no reissue expected after drift resolved, got %+v", again.Credential)
	}
}

// report 回执幂等：只接受 revision >= 当前 desired revision 的报告写回 applied，
// 过期一律丢弃（防多设备重复下发写回旧值 + desired 变更窗口内延迟报告的竞态）。
func TestGatewayRelayStateReport_StaleRevisionDropped(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9302, 9300, model.AgentClientTypeClaude)

	// desired revision 升到 2。
	if _, ec := GatewaySetAgentRelay(context.Background(), 9300, 9302, true, "", nil); ec != nil {
		t.Fatalf("set desired failed: %+v", ec)
	}
	if _, ec := GatewaySetAgentRelay(context.Background(), 9300, 9302, true, "", nil); ec != nil {
		t.Fatalf("bump desired failed: %+v", ec)
	}

	// 过期 revision=1：丢弃。
	written, ec := GatewayRelayStateReport(9300, 9302, true, 1)
	if ec != nil || written {
		t.Fatalf("stale report must be dropped, written=%v ec=%+v", written, ec)
	}
	row, err := store.GetGatewayAgentRelayState(9302)
	if err != nil || row.Applied {
		t.Fatalf("stale report must not write applied, row=%+v err=%v", row, err)
	}

	// 当前 revision=2：写回。
	written, ec = GatewayRelayStateReport(9300, 9302, true, 2)
	if ec != nil || !written {
		t.Fatalf("fresh report must be written, written=%v ec=%+v", written, ec)
	}
	row, err = store.GetGatewayAgentRelayState(9302)
	if err != nil || !row.Applied || row.AppliedAt == nil {
		t.Fatalf("expected applied=true with applied_at, row=%+v err=%v", row, err)
	}

	// applied=false 的报告同样写回（actual 跟随最近一次有效事件）。
	written, ec = GatewayRelayStateReport(9300, 9302, false, 2)
	if ec != nil || !written {
		t.Fatalf("disable report must be written, written=%v ec=%+v", written, ec)
	}
	row, err = store.GetGatewayAgentRelayState(9302)
	if err != nil || row.Applied {
		t.Fatalf("expected applied=false, row=%+v err=%v", row, err)
	}
}

// apply_relay_state 的 local_action_result 写回：与 report 同一套 revision 门槛；
// ok=false（期望未达成）把 applied 置 false。
func TestGatewayRelayStateApplyResult_RevisionGate(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9402, 9400, model.AgentClientTypeClaude)

	if _, ec := GatewaySetAgentRelay(context.Background(), 9400, 9402, true, "", nil); ec != nil {
		t.Fatalf("set desired failed: %+v", ec)
	}
	if _, ec := GatewaySetAgentRelay(context.Background(), 9400, 9402, true, "", nil); ec != nil {
		t.Fatalf("bump desired failed: %+v", ec)
	}

	// 过期回执丢弃。
	written, ec := GatewayRelayStateApplyResult(9400, 9402, 1, true)
	if ec != nil || written {
		t.Fatalf("stale apply result must be dropped, written=%v ec=%+v", written, ec)
	}
	// 新鲜回执 ok=true → applied=true。
	written, ec = GatewayRelayStateApplyResult(9400, 9402, 2, true)
	if ec != nil || !written {
		t.Fatalf("fresh apply result must be written, written=%v ec=%+v", written, ec)
	}
	row, err := store.GetGatewayAgentRelayState(9402)
	if err != nil || !row.Applied {
		t.Fatalf("expected applied=true, row=%+v err=%v", row, err)
	}
	// ok=false → applied=false。
	written, ec = GatewayRelayStateApplyResult(9400, 9402, 2, false)
	if ec != nil || !written {
		t.Fatalf("failed apply result must be written, written=%v ec=%+v", written, ec)
	}
	row, err = store.GetGatewayAgentRelayState(9402)
	if err != nil || row.Applied {
		t.Fatalf("expected applied=false after ok=false, row=%+v err=%v", row, err)
	}
}

// 无 state 行的 agent 回执直接丢弃（没有 desired 就不存在可展示的"已生效"）。
func TestGatewayRelayStateReport_NoStateRowDropped(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9502, 9500, model.AgentClientTypeClaude)

	written, ec := GatewayRelayStateReport(9500, 9502, true, 1)
	if ec != nil || written {
		t.Fatalf("report without desired must be dropped, written=%v ec=%+v", written, ec)
	}
}

// feature flag 关闭：sync 返回 503 业务码；report/apply 回执静默丢弃不报错；
// 断连清理停用。
func TestGatewayRelayStateSync_FlagDisabled(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9602, 9600, model.AgentClientTypeClaude)

	if _, ec := GatewaySetAgentRelay(context.Background(), 9600, 9602, true, "", nil); ec != nil {
		t.Fatalf("set desired failed: %+v", ec)
	}
	if err := store.SetGatewayAgentRelayStateApplied(9602, true, time.Now()); err != nil {
		t.Fatalf("seed applied: %v", err)
	}

	setGatewayRelayStateFlag(t, false)

	if _, ec := GatewayRelayStateSync(9600, 9602, boolPtr(true), "", "", ""); ec == nil ||
		ec.BizCode != errcode.ErrGatewayRelayStateDisabled.BizCode || ec.HTTPStatus != 503 {
		t.Fatalf("expected 503 ErrGatewayRelayStateDisabled, got %+v", ec)
	}
	written, ec := GatewayRelayStateReport(9600, 9602, false, 1)
	if ec != nil || written {
		t.Fatalf("report must be silently dropped under flag off, written=%v ec=%+v", written, ec)
	}
	GatewayRelayStateDisconnected(9602)
	row, err := store.GetGatewayAgentRelayState(9602)
	if err != nil || !row.Applied {
		t.Fatalf("disconnect hook must be disabled under flag off, row=%+v err=%v", row, err)
	}
}

// 断连钩子：最后一条权威连接断开时 applied 置 false（实际态随离线不可知，保守标未生效）。
func TestGatewayRelayStateDisconnected_ClearsApplied(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 9702, 9700, model.AgentClientTypeClaude)

	if _, ec := GatewaySetAgentRelay(context.Background(), 9700, 9702, true, "", nil); ec != nil {
		t.Fatalf("set desired failed: %+v", ec)
	}
	if err := store.SetGatewayAgentRelayStateApplied(9702, true, time.Now()); err != nil {
		t.Fatalf("seed applied: %v", err)
	}

	GatewayRelayStateDisconnected(9702)
	row, err := store.GetGatewayAgentRelayState(9702)
	if err != nil || row.Applied {
		t.Fatalf("expected applied=false after disconnect, row=%+v err=%v", row, err)
	}

	// 无 state 行的 agent 调用安全（不建行、不报错）。
	GatewayRelayStateDisconnected(999999)
}

// state_known 能力位（设计 §2.3 规则写死）：在线权威连接（路由 key）且能力清单
// 声明 apply_relay_state 才算 true；老 connector（无能力 key 或清单不含该值）、
// 离线（无路由）均 false。
func TestGatewayAgentStatesKnown_RequiresCapabilityBit(t *testing.T) {
	setupGatewayConfigureAgentTest(t) // 自带 miniredis
	ctx := context.Background()

	const (
		agentCapable      = 9802 // owner 路由 + 能力含 apply_relay_state
		agentOldConnector = 9803 // owner 路由 + 能力不含
		agentNoCaps       = 9804 // owner 路由，无能力 key
		agentMainRoute    = 9805 // 主路由 + 主能力含 apply_relay_state
		agentOffline      = 9806 // 什么都没有
	)
	capsWith, _ := json.Marshal([]string{provisioning.ApplyRelayStateActionType, "get_context"})
	capsWithout, _ := json.Marshal([]string{"get_context"})

	set := func(key string, val any) {
		if err := store.RDB.Set(ctx, key, val, time.Minute).Err(); err != nil {
			t.Fatalf("seed redis %s: %v", key, err)
		}
	}
	set(agentWSRouteKeyForOwner(agentCapable, 9800), "node-1")
	set(agentWSCapabilitiesKeyForOwner(agentCapable, 9800), capsWith)
	set(agentWSRouteKeyForOwner(agentOldConnector, 9800), "node-1")
	set(agentWSCapabilitiesKeyForOwner(agentOldConnector, 9800), capsWithout)
	set(agentWSRouteKeyForOwner(agentNoCaps, 9800), "node-1")
	set(agentWSRouteKey(agentMainRoute), "node-1")
	set(agentWSCapabilitiesKey(agentMainRoute), capsWith)

	known := gatewayAgentStatesKnown(9800, []int64{
		agentCapable, agentOldConnector, agentNoCaps, agentMainRoute, agentOffline,
	})
	want := map[int64]bool{
		agentCapable:      true,
		agentOldConnector: false,
		agentNoCaps:       false,
		agentMainRoute:    true,
		agentOffline:      false,
	}
	for id, w := range want {
		if known[id] != w {
			t.Fatalf("state_known agent=%d got %v want %v (all: %+v)", id, known[id], w, known)
		}
	}
}

// 能力清单 JSON 解析：畸形/空按未声明处理。
func TestGatewayRelayStateCapable(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{`["apply_relay_state","get_context"]`, true},
		{`["get_context"]`, false},
		{`[]`, false},
		{"", false},
		{"not-json", false},
		{`{"apply_relay_state":true}`, false},
	}
	for _, c := range cases {
		if got := gatewayRelayStateCapable(c.in); got != c.want {
			t.Fatalf("gatewayRelayStateCapable(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

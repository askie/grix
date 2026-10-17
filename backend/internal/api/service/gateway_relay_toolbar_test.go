package service

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// 投影只读：无钱包时 BalancePositive=false，且绝不能隐式建钱包/建行（验收：无钱包快照不新增数据库行）。
func TestGatewayAgentRelayToolbarProjection_NoWalletNoSideEffects(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8901, 8900, model.AgentClientTypeClaude)

	p, err := GatewayAgentRelayToolbarProjectionForAgent(8900, 8901)
	if err != nil {
		t.Fatalf("projection failed: %v", err)
	}
	if p == nil {
		t.Fatal("expected projection for owner's supported agent")
	}
	if p.BalancePositive || p.Enabled || p.Revision != 0 {
		t.Fatalf("empty projection mismatch: %+v", p)
	}

	var wallets, states int64
	if err := store.DB.Model(&model.GatewayWallet{}).Count(&wallets).Error; err != nil {
		t.Fatalf("count wallets: %v", err)
	}
	if err := store.DB.Model(&model.GatewayAgentRelayState{}).Count(&states).Error; err != nil {
		t.Fatalf("count states: %v", err)
	}
	if wallets != 0 || states != 0 {
		t.Fatalf("read-only projection must not create rows, wallets=%d states=%d", wallets, states)
	}
}

// 不注入的三种情形：请求人不是主人（共享者看不到）、client type 不支持、flag 关闭。
func TestGatewayAgentRelayToolbarProjection_SkipRules(t *testing.T) {
	setupGatewayServiceTest(t)
	createTestAgent(t, 8902, 8900, model.AgentClientTypeClaude)
	createTestAgent(t, 8903, 8900, model.AgentClientTypeGemini) // gemini 不在中转支持名单

	setGatewayRelayStateFlag(t, true)
	if p, err := GatewayAgentRelayToolbarProjectionForAgent(9999, 8902); err != nil || p != nil {
		t.Fatalf("non-owner must get nil projection, got %+v err=%v", p, err)
	}
	if p, err := GatewayAgentRelayToolbarProjectionForAgent(8900, 8903); err != nil || p != nil {
		t.Fatalf("unsupported client type must get nil projection, got %+v err=%v", p, err)
	}

	setGatewayRelayStateFlag(t, false)
	if p, err := GatewayAgentRelayToolbarProjectionForAgent(8900, 8902); err != nil || p != nil {
		t.Fatalf("flag off must get nil projection, got %+v err=%v", p, err)
	}
}

// 投影字段：余额判定 + state 行字段 + 无行时的模型兜底（钱包 default_model）。
func TestGatewayAgentRelayToolbarProjection_Fields(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8904, 8900, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 890001, "deepseek-flash")
	if err := store.DB.Create(&model.GatewayWallet{ID: 890011, OwnerID: 8900, Balance: decimal.NewFromInt(3)}).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	// 无 state 行：模型回退到钱包兜底。
	if _, ec := GatewayPutRelaySettings(8900, GatewayPutRelaySettingsReq{DefaultModel: "deepseek-flash"}); ec != nil {
		t.Fatalf("put relay settings: %+v", ec)
	}
	p, err := GatewayAgentRelayToolbarProjectionForAgent(8900, 8904)
	if err != nil || p == nil {
		t.Fatalf("projection: %+v err=%v", p, err)
	}
	if !p.BalancePositive || p.RelayModel != "deepseek-flash" || p.Revision != 0 {
		t.Fatalf("projection mismatch: %+v", p)
	}

	// 有 state 行：以行的 desired/model/revision 为准。
	if _, ec := GatewaySetAgentRelay(context.Background(), 8900, 8904, true, "deepseek-flash", nil); ec != nil {
		t.Fatalf("enable relay: %+v", ec)
	}
	p, err = GatewayAgentRelayToolbarProjectionForAgent(8900, 8904)
	if err != nil || p == nil {
		t.Fatalf("projection: %+v err=%v", p, err)
	}
	if !p.Enabled || p.RelayModel != "deepseek-flash" || p.Revision != 1 {
		t.Fatalf("projection mismatch after enable: %+v", p)
	}
}

// 关闭保留已选模型且不校验可用性：模型下架（价目删除）后仍能关，重开沿用原模型。
func TestGatewaySetAgentRelay_DisablePreservesModelAfterRetired(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8905, 8900, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 890002, "deepseek-flash")

	if _, ec := GatewaySetAgentRelay(context.Background(), 8900, 8905, true, "deepseek-flash", nil); ec != nil {
		t.Fatalf("enable relay: %+v", ec)
	}
	// 模型下架：可用清单清空。
	if err := store.DB.Where("1 = 1").Delete(&model.GatewayPricingRule{}).Error; err != nil {
		t.Fatalf("retire model: %v", err)
	}

	resp, ec := GatewaySetAgentRelay(context.Background(), 8900, 8905, false, "", nil)
	if ec != nil {
		t.Fatalf("disable must not be blocked by retired model, got %+v", ec)
	}
	if resp.Enabled || resp.RelayModel != "deepseek-flash" {
		t.Fatalf("disable should preserve the chosen model, got %+v", resp)
	}

	// 重新上架后无参重开：沿用 state 已选模型。
	seedGatewayServableModel(t, 890003, "deepseek-flash")
	resp, ec = GatewaySetAgentRelay(context.Background(), 8900, 8905, true, "", nil)
	if ec != nil {
		t.Fatalf("re-enable should reuse the preserved model, got %+v", ec)
	}
	if !resp.Enabled || resp.RelayModel != "deepseek-flash" {
		t.Fatalf("re-enable mismatch: %+v", resp)
	}
}

// 原生类型首次开启无参：从钱包兜底模型回填（工具栏一键开启的语义）。
func TestGatewaySetAgentRelay_NativeBackfillsWalletDefaultModel(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8906, 8900, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 890004, "deepseek-flash")
	if _, ec := GatewayPutRelaySettings(8900, GatewayPutRelaySettingsReq{DefaultModel: "deepseek-flash"}); ec != nil {
		t.Fatalf("put relay settings: %+v", ec)
	}

	resp, ec := GatewaySetAgentRelay(context.Background(), 8900, 8906, true, "", nil)
	if ec != nil {
		t.Fatalf("enable with backfill failed: %+v", ec)
	}
	if !resp.Enabled || resp.RelayModel != "deepseek-flash" {
		t.Fatalf("backfilled model mismatch: %+v", resp)
	}
}

// 回填值已不在可用清单时报 need_model 引导明确选择，而不是甩"模型不可用"。
func TestGatewaySetAgentRelay_NativeBackfillUnservableAsksForModel(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8907, 8900, model.AgentClientTypeQwen)
	// 价目表为空：任何回填值都不可用。直接落库设置行，绕过 PUT 的可用性校验
	// （模拟"设置时可用、后来下架"的存量状态）。
	w, ec := ensureGatewayWallet(8900)
	if ec != nil {
		t.Fatalf("ensure wallet: %+v", ec)
	}
	if err := store.DB.Create(&model.GatewayRelaySetting{WalletID: w.ID, DefaultModel: "deepseek-flash"}).Error; err != nil {
		t.Fatalf("seed relay setting: %v", err)
	}

	_, ec = GatewaySetAgentRelay(context.Background(), 8900, 8907, true, "", nil)
	if ec == nil || ec.Msg != "该Agent类型启用中转必须选择模型" {
		t.Fatalf("expected need_model, got %+v", ec)
	}
}

// 关闭保留模型必须原子：并发写入的新模型不能被"读旧模型再写回"覆盖（F3 回归）。
// 用顺序调用模拟交错：关闭前另一请求已把模型改成新值。
func TestGatewaySetAgentRelay_DisableKeepsConcurrentlyUpdatedModel(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8908, 8900, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 890005, "deepseek-flash")
	seedGatewayServableModel(t, 890006, "deepseek-v4-pro")

	if _, ec := GatewaySetAgentRelay(context.Background(), 8900, 8908, true, "deepseek-flash", nil); ec != nil {
		t.Fatalf("enable: %+v", ec)
	}
	// 交错：另一请求把模型换成 deepseek-v4-pro（仍开启）。
	if _, ec := GatewaySetAgentRelay(context.Background(), 8900, 8908, true, "deepseek-v4-pro", nil); ec != nil {
		t.Fatalf("switch model: %+v", ec)
	}
	// 关闭（不带 revision 的 REST 口径）：必须保留 pro，不能写回 flash。
	resp, ec := GatewaySetAgentRelay(context.Background(), 8900, 8908, false, "", nil)
	if ec != nil {
		t.Fatalf("disable: %+v", ec)
	}
	if resp.Enabled || resp.RelayModel != "deepseek-v4-pro" {
		t.Fatalf("disable must keep the concurrently updated model, got %+v", resp)
	}
}

// MITM 类型空模型开启后 relay_model 保持为空（走映射/兜底），不被回填固化（F1 回归）。
func TestGatewaySetAgentRelay_MITMEnableKeepsEmptyModel(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8909, 8900, model.AgentClientTypeClaude)
	seedGatewayServableModel(t, 890007, "deepseek-flash")
	if _, ec := GatewayPutRelaySettings(8900, GatewayPutRelaySettingsReq{DefaultModel: "deepseek-flash"}); ec != nil {
		t.Fatalf("put relay settings: %+v", ec)
	}

	resp, ec := GatewaySetAgentRelay(context.Background(), 8900, 8909, true, "", nil)
	if ec != nil {
		t.Fatalf("enable MITM: %+v", ec)
	}
	if !resp.Enabled || resp.RelayModel != "" {
		t.Fatalf("MITM enable must keep empty model (mapping/fallback), got %+v", resp)
	}
}

// 投影必须把 Key/设置的查询故障传播为 error（不注入按钮），不能折叠成兜底值（F4 回归）。
func TestGatewayAgentRelayToolbarProjection_PropagatesQueryFailure(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8910, 8900, model.AgentClientTypeQwen)
	if err := store.DB.Create(&model.GatewayWallet{ID: 891011, OwnerID: 8900, Balance: decimal.NewFromInt(3)}).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	// 无 state 行 + 有钱包 → 进入模型兜底，必然查 virtual_keys；把表删掉制造故障。
	if err := store.DB.Exec("DROP TABLE gateway_virtual_keys").Error; err != nil {
		t.Fatalf("drop table: %v", err)
	}
	p, err := GatewayAgentRelayToolbarProjectionForAgent(8900, 8910)
	if err == nil || p != nil {
		t.Fatalf("query failure must propagate, got p=%+v err=%v", p, err)
	}
}

// 系统默认模型链路的查询故障也必须传播（F4 残留）：无钱包设置行时会落到
// system_settings，宽松实现会把故障折叠成内置常量。
func TestGatewayAgentRelayToolbarProjection_PropagatesSystemDefaultFailure(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8911, 8900, model.AgentClientTypeQwen)
	if err := store.DB.Create(&model.GatewayWallet{ID: 891111, OwnerID: 8900, Balance: decimal.NewFromInt(3)}).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	// 无 state 行、无 Key、无钱包设置行 → 落到系统默认；把 system_settings 删掉制造故障。
	if err := store.DB.Exec("DROP TABLE system_settings").Error; err != nil {
		t.Fatalf("drop table: %v", err)
	}
	p, err := GatewayAgentRelayToolbarProjectionForAgent(8900, 8911)
	if err == nil || p != nil {
		t.Fatalf("system default query failure must propagate, got p=%+v err=%v", p, err)
	}
}

// F3 竞态回归（确定性交错）：用 GORM 钩子在"关闭流程读现有 state"之后、原子写之前
// 注入一次并发改模型。旧实现（读旧模型再随写回）会把并发新模型覆盖掉；原子关闭不动
// relay_model，必须保留并发写入的新值。
func TestGatewaySetAgentRelay_DisablePreservesModelUnderInterleaving(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8912, 8900, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 890008, "deepseek-flash")
	seedGatewayServableModel(t, 890009, "deepseek-v4-pro")

	if _, ec := GatewaySetAgentRelay(context.Background(), 8900, 8912, true, "deepseek-flash", nil); ec != nil {
		t.Fatalf("enable: %+v", ec)
	}

	injected := false
	w, wErr := ensureGatewayWallet(8900)
	if wErr != nil {
		t.Fatalf("ensure wallet: %+v", wErr)
	}
	hook := func(tx *gorm.DB) {
		if injected || tx.Statement.Table != "gateway_agent_relay_state" {
			return
		}
		injected = true
		// 并发请求：改用新模型（仍开启）。
		if _, err := store.UpsertGatewayAgentRelayStateDesired(8912, w.ID, true, "deepseek-v4-pro", nil); err != nil {
			t.Errorf("inject concurrent model switch: %v", err)
		}
	}
	if err := store.DB.Callback().Query().After("gorm:after_query").Register("test:inject_model_switch", hook); err != nil {
		t.Fatalf("register hook: %v", err)
	}
	t.Cleanup(func() { store.DB.Callback().Query().Remove("test:inject_model_switch") })

	resp, ec := GatewaySetAgentRelay(context.Background(), 8900, 8912, false, "", nil)
	if ec != nil {
		t.Fatalf("disable: %+v", ec)
	}
	if !injected {
		t.Fatal("interleaving hook did not fire; test is not exercising the race window")
	}
	if resp.Enabled || resp.RelayModel != "deepseek-v4-pro" {
		t.Fatalf("disable must preserve the concurrently chosen model, got %+v", resp)
	}
}

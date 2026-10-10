package service

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

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
	seedGatewayServableModel(t, 890001, "deepseek-v4-flash")
	if err := store.DB.Create(&model.GatewayWallet{ID: 890011, OwnerID: 8900, Balance: decimal.NewFromInt(3)}).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	// 无 state 行：模型回退到钱包兜底。
	if _, ec := GatewayPutRelaySettings(8900, GatewayPutRelaySettingsReq{DefaultModel: "deepseek-v4-flash"}); ec != nil {
		t.Fatalf("put relay settings: %+v", ec)
	}
	p, err := GatewayAgentRelayToolbarProjectionForAgent(8900, 8904)
	if err != nil || p == nil {
		t.Fatalf("projection: %+v err=%v", p, err)
	}
	if !p.BalancePositive || p.RelayModel != "deepseek-v4-flash" || p.Revision != 0 {
		t.Fatalf("projection mismatch: %+v", p)
	}

	// 有 state 行：以行的 desired/model/revision 为准。
	if _, ec := GatewaySetAgentRelay(context.Background(), 8900, 8904, true, "deepseek-v4-flash", nil); ec != nil {
		t.Fatalf("enable relay: %+v", ec)
	}
	p, err = GatewayAgentRelayToolbarProjectionForAgent(8900, 8904)
	if err != nil || p == nil {
		t.Fatalf("projection: %+v err=%v", p, err)
	}
	if !p.Enabled || p.RelayModel != "deepseek-v4-flash" || p.Revision != 1 {
		t.Fatalf("projection mismatch after enable: %+v", p)
	}
}

// 关闭保留已选模型且不校验可用性：模型下架（价目删除）后仍能关，重开沿用原模型。
func TestGatewaySetAgentRelay_DisablePreservesModelAfterRetired(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8905, 8900, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 890002, "deepseek-v4-flash")

	if _, ec := GatewaySetAgentRelay(context.Background(), 8900, 8905, true, "deepseek-v4-flash", nil); ec != nil {
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
	if resp.Enabled || resp.RelayModel != "deepseek-v4-flash" {
		t.Fatalf("disable should preserve the chosen model, got %+v", resp)
	}

	// 重新上架后无参重开：沿用 state 已选模型。
	seedGatewayServableModel(t, 890003, "deepseek-v4-flash")
	resp, ec = GatewaySetAgentRelay(context.Background(), 8900, 8905, true, "", nil)
	if ec != nil {
		t.Fatalf("re-enable should reuse the preserved model, got %+v", ec)
	}
	if !resp.Enabled || resp.RelayModel != "deepseek-v4-flash" {
		t.Fatalf("re-enable mismatch: %+v", resp)
	}
}

// 原生类型首次开启无参：从钱包兜底模型回填（工具栏一键开启的语义）。
func TestGatewaySetAgentRelay_NativeBackfillsWalletDefaultModel(t *testing.T) {
	setupGatewayServiceTest(t)
	setGatewayRelayStateFlag(t, true)
	createTestAgent(t, 8906, 8900, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 890004, "deepseek-v4-flash")
	if _, ec := GatewayPutRelaySettings(8900, GatewayPutRelaySettingsReq{DefaultModel: "deepseek-v4-flash"}); ec != nil {
		t.Fatalf("put relay settings: %+v", ec)
	}

	resp, ec := GatewaySetAgentRelay(context.Background(), 8900, 8906, true, "", nil)
	if ec != nil {
		t.Fatalf("enable with backfill failed: %+v", ec)
	}
	if !resp.Enabled || resp.RelayModel != "deepseek-v4-flash" {
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
	if err := store.DB.Create(&model.GatewayRelaySetting{WalletID: w.ID, DefaultModel: "deepseek-v4-flash"}).Error; err != nil {
		t.Fatalf("seed relay setting: %v", err)
	}

	_, ec = GatewaySetAgentRelay(context.Background(), 8900, 8907, true, "", nil)
	if ec == nil || ec.Msg != "该Agent类型启用中转必须选择模型" {
		t.Fatalf("expected need_model, got %+v", ec)
	}
}

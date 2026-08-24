package service

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
)

// seedGatewayServableModel 给价目表灌一条基准价，使该模型进入"后端支持的模型"清单
// （servableModels 的口径 = 有基准价 + modelroute 可路由且 provider 对得上）。
func seedGatewayServableModel(t *testing.T, id int64, m string) {
	t.Helper()
	if err := store.DB.Create(&model.GatewayPricingRule{
		ID:                     id,
		Provider:               "deepseek",
		Model:                  m,
		CachedInputPricePerM:   decimal.NewFromFloat(0.01),
		UncachedInputPricePerM: decimal.NewFromFloat(0.07),
		OutputPricePerM:        decimal.NewFromFloat(0.11),
		SourceCurrency:         "USD",
		CreatedBy:              model.GatewayPricingRuleCreatedByManual,
	}).Error; err != nil {
		t.Fatalf("seed pricing rule: %v", err)
	}
}

// 非 MITM 类型（qwen/kimi/reasonix/deepseek/opencode/codewhale/pi/hermes/kiro/openclaw）
// 把网关端点写进 CLI 自己的原生配置，配置结构里模型名必填，签发凭证不带 model 必须在源头
// 被拦下（跟 connector 的 MISSING_MODEL / missing_model 同一约定）。该用例同时钉住这十个
// 类型都已进入 supported 名单——若被误移出，报错会变成 ErrGatewayUnsupportedClientType
// 而非 ErrGatewayRelayModelRequired。
func TestGatewayIssueAgentRelayCredential_NativeTypesRequireModel(t *testing.T) {
	setupGatewayServiceTest(t)

	types := []string{
		model.AgentClientTypeQwen,
		model.AgentClientTypeKimi,
		model.AgentClientTypeReasonix,
		model.AgentClientTypeDeepSeek,
		model.AgentClientTypeOpenCode,
		model.AgentClientTypeCodeWhale,
		model.AgentClientTypePi,
		model.AgentClientTypeHermes,
		model.AgentClientTypeKiro,
		model.AgentClientTypeOpenClaw,
	}
	for i, ct := range types {
		agentID := int64(8100 + i)
		createTestAgent(t, agentID, 8000, ct)
		ec := errCodeOfCredential(GatewayIssueAgentRelayCredential(8000, agentID, "", "", ""))
		if ec == nil || ec.BizCode != errcode.ErrGatewayRelayModelRequired.BizCode {
			t.Fatalf("client type %q: expected ErrGatewayRelayModelRequired, got %+v", ct, ec)
		}
	}
}

// MITM 类型（claude/codex）不要求 model：网关的模型映射/兜底在服务端生效，历史行为不变。
func TestGatewayIssueAgentRelayCredential_MitmTypesModelOptional(t *testing.T) {
	setupGatewayServiceTest(t)
	createTestAgent(t, 8201, 8200, model.AgentClientTypeClaude)

	resp, ec := GatewayIssueAgentRelayCredential(8200, 8201, "", "", "")
	if ec != nil {
		t.Fatalf("claude without model should succeed, got %+v", ec)
	}
	if resp.RelayModel != "" {
		t.Fatalf("expected empty relay model, got %q", resp.RelayModel)
	}
}

// 选定的模型随虚拟Key落库，签发响应原样带回，Agent 列表用它回显；重签发覆盖为新选择。
func TestGatewayIssueAgentRelayCredential_StoresAndEchoesRelayModel(t *testing.T) {
	setupGatewayServiceTest(t)
	createTestAgent(t, 8301, 8300, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 830001, "deepseek-v4-flash")
	seedGatewayServableModel(t, 830002, "deepseek-v4-pro")

	resp, ec := GatewayIssueAgentRelayCredential(8300, 8301, "", "https://gw/openai/v1", "deepseek-v4-flash")
	if ec != nil {
		t.Fatalf("issue with model failed: %+v", ec)
	}
	if resp.RelayModel != "deepseek-v4-flash" {
		t.Fatalf("expected relay model echoed, got %q", resp.RelayModel)
	}

	agents, ec2 := GatewayListAgents(8300)
	if ec2 != nil {
		t.Fatalf("GatewayListAgents failed: %+v", ec2)
	}
	if len(agents.Items) != 1 || agents.Items[0].RelayModel != "deepseek-v4-flash" {
		t.Fatalf("expected list to echo relay model, got %+v", agents.Items)
	}

	// 重签发换模型 → 列表回显跟着换（取自当前活跃Key，不残留旧值）
	if _, ec := GatewayIssueAgentRelayCredential(8300, 8301, "", "https://gw/openai/v1", "deepseek-v4-pro"); ec != nil {
		t.Fatalf("reissue with new model failed: %+v", ec)
	}
	agents, ec2 = GatewayListAgents(8300)
	if ec2 != nil {
		t.Fatalf("GatewayListAgents after reissue failed: %+v", ec2)
	}
	if agents.Items[0].RelayModel != "deepseek-v4-pro" {
		t.Fatalf("expected relay model updated to deepseek-v4-pro, got %q", agents.Items[0].RelayModel)
	}
}

// 吊销旧Key失败会留下同 agent 的多把活跃Key（签发方有意不中断）；
// 回显必须确定地取最新签发那把的模型，不能看遍历顺序的脸色。
func TestGatewayListAgents_MultipleActiveKeysEchoLatestRelayModel(t *testing.T) {
	setupGatewayServiceTest(t)
	createTestAgent(t, 8501, 8500, model.AgentClientTypeQwen)

	w, ec := ensureGatewayWallet(8500)
	if ec != nil {
		t.Fatalf("ensure wallet: %+v", ec)
	}
	// 直接构造两把活跃Key（模拟吊销失败的残留），ID 小=旧、ID 大=新。
	for _, k := range []struct {
		id    int64
		model string
	}{{100, "old-model"}, {200, "new-model"}} {
		if err := store.DB.Create(&model.GatewayVirtualKey{
			ID: k.id, WalletID: w.ID, KeyHash: k.model, KeyHint: "hint",
			Status: model.GatewayVirtualKeyStatusActive, AgentID: 8501, RelayModel: k.model,
		}).Error; err != nil {
			t.Fatalf("seed key: %v", err)
		}
	}

	agents, ec := GatewayListAgents(8500)
	if ec != nil {
		t.Fatalf("GatewayListAgents failed: %+v", ec)
	}
	if len(agents.Items) != 1 || agents.Items[0].RelayModel != "new-model" {
		t.Fatalf("expected latest key's relay model, got %+v", agents.Items)
	}
}

// 模型必须在"后端支持的模型"清单里：放一个不可服务的模型落库等于埋雷。
func TestGatewayIssueAgentRelayCredential_RejectsUnservableModel(t *testing.T) {
	setupGatewayServiceTest(t)
	createTestAgent(t, 8401, 8400, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 840001, "deepseek-v4-flash")

	ec := errCodeOfCredential(GatewayIssueAgentRelayCredential(8400, 8401, "", "", "no-such-model"))
	if ec == nil || ec.BizCode != errcode.ErrGatewayModelNotServable.BizCode {
		t.Fatalf("expected ErrGatewayModelNotServable, got %+v", ec)
	}
}

package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/gateway/modelcatalog"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// setDirectRelayFlag 在测试内翻转 gateway.direct_relay_enabled（同 relay_state 测试的口径）。
func setDirectRelayFlag(t *testing.T, enabled bool) {
	t.Helper()
	original := config.C.Gateway.DirectRelayEnabled
	config.C.Gateway.DirectRelayEnabled = enabled
	t.Cleanup(func() { config.C.Gateway.DirectRelayEnabled = original })
}

// seedServableModelWithProvider 与 seedGatewayServableModel 同口径，但允许指定 provider，
// 用于造"非 DeepSeek 模型"验证 Codex capability 不会对桥接厂商声明 supported。
func seedServableModelWithProvider(t *testing.T, id int64, provider, m string) {
	t.Helper()
	if err := store.DB.Create(&model.GatewayPricingRule{
		ID:                     id,
		Provider:               provider,
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

// flag 关闭（默认/回滚态）：响应完全不含 direct_relay，旧字段原样——旧连接器看到的一切不变。
func TestDirectRelay_DisabledByFlag(t *testing.T) {
	setupGatewayServiceTest(t)
	setDirectRelayFlag(t, false)
	createTestAgent(t, 9101, 9100, model.AgentClientTypeClaude)
	seedGatewayServableModel(t, 910001, "deepseek-v4-flash")

	resp, ec := GatewayIssueAgentRelayCredential(9100, 9101, "https://gw/anthropic/v1", "https://gw/openai/v1", "deepseek-v4-flash")
	if ec != nil {
		t.Fatalf("issue failed: %+v", ec)
	}
	if resp.DirectRelay != nil {
		t.Fatalf("expected no direct_relay when flag off, got %+v", resp.DirectRelay)
	}
	if resp.VirtualKey == "" || resp.AnthropicBaseURL != "https://gw/anthropic/v1" || resp.OpenAIBaseURL != "https://gw/openai/v1" || resp.RelayModel != "deepseek-v4-flash" {
		t.Fatalf("legacy fields changed: %+v", resp)
	}
}

// Claude + DeepSeek 模型：声明 claude capability；base_url 是不带 /v1 的 SDK base（直连合同 D1）。
func TestDirectRelay_ClaudeSupported(t *testing.T) {
	setupGatewayServiceTest(t)
	setDirectRelayFlag(t, true)
	createTestAgent(t, 9201, 9200, model.AgentClientTypeClaude)
	seedGatewayServableModel(t, 920001, "deepseek-v4-flash")

	resp, ec := GatewayIssueAgentRelayCredential(9200, 9201, "https://gw/anthropic/v1", "https://gw/openai/v1", "deepseek-v4-flash")
	if ec != nil {
		t.Fatalf("issue failed: %+v", ec)
	}
	dr := resp.DirectRelay
	if dr == nil || dr.Version != 1 {
		t.Fatalf("expected direct_relay version=1, got %+v", dr)
	}
	if dr.Codex != nil {
		t.Fatalf("claude agent should not get codex capability, got %+v", dr.Codex)
	}
	cl := dr.Claude
	if cl == nil || !cl.Supported {
		t.Fatalf("expected claude supported, got %+v", cl)
	}
	if cl.BaseURL != "https://gw/anthropic" {
		t.Fatalf("direct base_url must strip /v1, got %q", cl.BaseURL)
	}
	if cl.PrimaryModel != "deepseek-v4-flash" {
		t.Fatalf("expected primary_model, got %q", cl.PrimaryModel)
	}
	// 旧字段语义不变（D6）。
	if resp.AnthropicBaseURL != "https://gw/anthropic/v1" {
		t.Fatalf("legacy anthropic_base_url changed: %q", resp.AnthropicBaseURL)
	}
}

// 未选定模型时 capability 字段不完整：只能 supported=false，后端绝不替连接器猜（D1）。
func TestDirectRelay_ClaudeWithoutModelNotSupported(t *testing.T) {
	setupGatewayServiceTest(t)
	setDirectRelayFlag(t, true)
	createTestAgent(t, 9301, 9300, model.AgentClientTypeClaude)

	resp, ec := GatewayIssueAgentRelayCredential(9300, 9301, "https://gw/anthropic/v1", "https://gw/openai/v1", "")
	if ec != nil {
		t.Fatalf("issue failed: %+v", ec)
	}
	if resp.DirectRelay == nil || resp.DirectRelay.Claude == nil {
		t.Fatalf("expected claude capability object, got %+v", resp.DirectRelay)
	}
	if resp.DirectRelay.Claude.Supported {
		t.Fatal("claude capability must not declare supported without a model")
	}
	if resp.DirectRelay.Claude.BaseURL != "" || resp.DirectRelay.Claude.PrimaryModel != "" {
		t.Fatalf("incomplete capability must not carry config fields: %+v", resp.DirectRelay.Claude)
	}
}

// relay_state_sync 的重签路径历史上会传空 base URL；这种响应只能保持兼容模式，
// 绝不能宣告 supported=true 再把空地址交给官方客户端。
func TestDirectRelay_EmptyOrInvalidBaseURLNotSupported(t *testing.T) {
	for _, base := range []string{"", "   ", "/anthropic/v1", "ftp://gw/anthropic/v1", "https://u:p@gw/anthropic/v1"} {
		if got := buildClaudeDirectCapability("deepseek-v4-flash", base); got.Supported {
			t.Fatalf("claude must not be supported with base URL %q: %+v", base, got)
		}
	}
	for _, base := range []string{"", "   ", "/openai/v1", "ftp://gw/openai/v1", "https://gw/openai/v1?q=1"} {
		if got := buildCodexDirectCapability("deepseek-v4-pro", base); got.Supported {
			t.Fatalf("codex must not be supported with base URL %q: %+v", base, got)
		}
	}
}

// Codex + DeepSeek（原生 Responses）：声明 codex capability + 内嵌版本化 catalog（D4 + §6.2 方式A）。
func TestDirectRelay_CodexSupportedWithCatalog(t *testing.T) {
	setupGatewayServiceTest(t)
	setDirectRelayFlag(t, true)
	createTestAgent(t, 9401, 9400, model.AgentClientTypeCodex)
	seedGatewayServableModel(t, 940001, "deepseek-v4-pro")

	resp, ec := GatewayIssueAgentRelayCredential(9400, 9401, "https://gw/anthropic/v1", "https://gw/openai/v1", "deepseek-v4-pro")
	if ec != nil {
		t.Fatalf("issue failed: %+v", ec)
	}
	cx := resp.DirectRelay.Codex
	if cx == nil || !cx.Supported {
		t.Fatalf("expected codex supported, got %+v", cx)
	}
	if cx.BaseURL != "https://gw/openai" {
		t.Fatalf("direct base_url must strip /v1, got %q", cx.BaseURL)
	}
	if cx.WireAPI != "responses" {
		t.Fatalf("codex direct must use wire_api=responses, got %q", cx.WireAPI)
	}
	if cx.Model != "deepseek-v4-pro" {
		t.Fatalf("expected model, got %q", cx.Model)
	}
	if cx.CatalogVersion != modelcatalog.Version {
		t.Fatalf("catalog version mismatch: %q vs %q", cx.CatalogVersion, modelcatalog.Version)
	}
	if cx.CatalogSHA256 != modelcatalog.SHA256() {
		t.Fatal("catalog sha256 mismatch")
	}
	if cx.CatalogJSON != string(modelcatalog.CanonicalJSON()) {
		t.Fatal("catalog_json must preserve the exact bytes used for sha256")
	}
	var catalog modelcatalog.Catalog
	if err := json.Unmarshal(cx.Catalog, &catalog); err != nil {
		t.Fatalf("embedded catalog not valid JSON: %v", err)
	}
	found := false
	for _, m := range catalog.Models {
		if m.Slug == "deepseek-v4-pro" {
			found = true
		}
	}
	if !found {
		t.Fatal("embedded catalog missing deepseek-v4-pro")
	}
}

func TestDirectRelay_CodexAliasUsesCatalogModelID(t *testing.T) {
	cx := buildCodexDirectCapability("deepseek-reasoner", "https://gw/openai/v1")
	if !cx.Supported || cx.Model != "deepseek-v4-pro" {
		t.Fatalf("alias must resolve to catalog model id: %+v", cx)
	}
}

// Codex + 非原生 Responses 厂商（volcano_ark 走 Responses↔Chat 桥接）：不得声明 supported（D4）。
func TestDirectRelay_CodexBridgedProviderNotSupported(t *testing.T) {
	setupGatewayServiceTest(t)
	setDirectRelayFlag(t, true)
	createTestAgent(t, 9501, 9500, model.AgentClientTypeCodex)
	seedServableModelWithProvider(t, 950001, "volcano_ark", "doubao-seed-1-6")

	resp, ec := GatewayIssueAgentRelayCredential(9500, 9500+1, "https://gw/anthropic/v1", "https://gw/openai/v1", "doubao-seed-1-6")
	if ec != nil {
		t.Fatalf("issue failed: %+v", ec)
	}
	cx := resp.DirectRelay.Codex
	if cx == nil {
		t.Fatal("expected codex capability object")
	}
	if cx.Supported {
		t.Fatal("codex direct must not declare supported for bridged provider")
	}
	if cx.Catalog != nil || cx.CatalogSHA256 != "" {
		t.Fatalf("unsupported capability must not carry catalog: %+v", cx)
	}
}

// 非 claude/codex 类型（原生配置类）：不下发 direct_relay。
func TestDirectRelay_NativeTypesOmitted(t *testing.T) {
	setupGatewayServiceTest(t)
	setDirectRelayFlag(t, true)
	createTestAgent(t, 9601, 9600, model.AgentClientTypeQwen)
	seedGatewayServableModel(t, 960001, "deepseek-v4-flash")

	resp, ec := GatewayIssueAgentRelayCredential(9600, 9601, "https://gw/anthropic/v1", "https://gw/openai/v1", "deepseek-v4-flash")
	if ec != nil {
		t.Fatalf("issue failed: %+v", ec)
	}
	if resp.DirectRelay != nil {
		t.Fatalf("qwen agent should not get direct_relay, got %+v", resp.DirectRelay)
	}
}

// capability 绝不含虚拟 Key（明文或哈希）——direct_relay 会进连接器配置落盘，泄密面必须为零。
func TestDirectRelay_NeverContainsVirtualKey(t *testing.T) {
	setupGatewayServiceTest(t)
	setDirectRelayFlag(t, true)
	createTestAgent(t, 9701, 9700, model.AgentClientTypeCodex)
	seedGatewayServableModel(t, 970001, "deepseek-v4-flash")

	resp, ec := GatewayIssueAgentRelayCredential(9700, 9701, "https://gw/anthropic/v1", "https://gw/openai/v1", "deepseek-v4-flash")
	if ec != nil {
		t.Fatalf("issue failed: %+v", ec)
	}
	raw, err := json.Marshal(resp.DirectRelay)
	if err != nil {
		t.Fatalf("marshal direct_relay: %v", err)
	}
	if strings.Contains(string(raw), resp.VirtualKey) {
		t.Fatal("direct_relay contains virtual key")
	}
}

package e2e

import (
	"net/http"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/askie/grix/backend/internal/gateway/pricing"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// seedRelayPricing 灌两档后端模型的全天兜底价。
// "后端支持的模型" = 价目表里有基准价的模型，模型清单接口就是从这里派生的。
func seedRelayPricing(t *testing.T) {
	t.Helper()
	for i, m := range []struct {
		name              string
		unc, outp, cached string
	}{
		{"deepseek-v4-flash", "0.07", "0.11", "0.014"},
		{"deepseek-v4-pro", "0.28", "0.42", "0.056"},
	} {
		require.NoError(t, store.DB.Create(&model.GatewayPricingRule{
			ID:                     int64(9000 + i),
			Provider:               "deepseek",
			Model:                  m.name,
			CachedInputPricePerM:   decimal.RequireFromString(m.cached),
			UncachedInputPricePerM: decimal.RequireFromString(m.unc),
			OutputPricePerM:        decimal.RequireFromString(m.outp),
			SourceCurrency:         "USD",
			CreatedBy:              model.GatewayPricingRuleCreatedByManual,
		}).Error)
	}
}

// 模型清单从价目表派生，并带出单价——前端要把成本直接摆给用户看。
func TestGatewayListModels(t *testing.T) {
	ctx := setupE2E(t)
	seedRelayPricing(t)
	token, _ := ctx.loginHelper(t, "relay-models", "Passw0rd!A")

	w := ctx.doReq(t, http.MethodGet, "/v1/gateway/models", token, nil)
	require.Equal(t, http.StatusOK, w.Code)

	data := parseResp(t, w)["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	require.Len(t, items, 2)

	first := items[0].(map[string]interface{})
	assert.Equal(t, "deepseek", first["provider"])
	assert.Equal(t, "deepseek-v4-flash", first["model"])
	// 单价必须给出去，否则用户看不见成本
	assert.NotEmpty(t, first["input_price_per_m"])
	assert.NotEmpty(t, first["output_price_per_m"])
}

// 网关路由不了的模型不得进入用户清单——哪怕塘主给它录了价目。
// 塘主后台创建价目规则不校验 provider；若清单只看"有基准价"，一条 openai/gpt-5 的价目
// 就会把网关服务不了的模型塞进用户下拉，用户选中当兜底 → 所有请求 400。
// 清单和网关必须用同一份 modelroute 判定。
func TestGatewayListModels_ExcludesUnroutableProviders(t *testing.T) {
	ctx := setupE2E(t)
	seedRelayPricing(t)
	token, _ := ctx.loginHelper(t, "relay-unroutable", "Passw0rd!A")

	// 塘主录了一条 openai 的价目——网关没有 openai 上游，路由不了
	require.NoError(t, store.DB.Create(&model.GatewayPricingRule{
		ID:                     9200,
		Provider:               "openai",
		Model:                  "gpt-5",
		CachedInputPricePerM:   decimal.RequireFromString("0.1"),
		UncachedInputPricePerM: decimal.RequireFromString("1"),
		OutputPricePerM:        decimal.RequireFromString("3"),
		SourceCurrency:         "USD",
		CreatedBy:              model.GatewayPricingRuleCreatedByManual,
	}).Error)

	w := ctx.doReq(t, http.MethodGet, "/v1/gateway/models", token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "gpt-5", "网关路由不了的模型不得进用户清单")
	assert.Contains(t, w.Body.String(), "deepseek-v4-flash")
}

// 退休掉的价目规则必须从用户的可选模型清单里消失。
//
// 这是真实存在的坑：线上价目表里混着历史探测留下的废规则（上游根本不认的模型别名，
// 如 doubao-seed-2.0-pro）。它们对结算无害（结算按上游回显名查价，永远命中不到），
// 但模型清单是从价目表派生的——不退休掉，用户就会在下拉里看到它们，选中即报错。
func TestGatewayListModels_ExcludesRetiredRules(t *testing.T) {
	ctx := setupE2E(t)
	seedRelayPricing(t)
	token, _ := ctx.loginHelper(t, "relay-retired", "Passw0rd!A")

	// 灌一条"废规则"：上游不认这个模型名
	var junk model.GatewayPricingRule
	require.NoError(t, store.DB.Create(&model.GatewayPricingRule{
		ID:                     9100,
		Provider:               "volcano_ark",
		Model:                  "doubao-seed-2.0-pro", // 带点号的别名，Ark 不认
		CachedInputPricePerM:   decimal.RequireFromString("0.1"),
		UncachedInputPricePerM: decimal.RequireFromString("0.7"),
		OutputPricePerM:        decimal.RequireFromString("3.5"),
		SourceCurrency:         "USD",
		CreatedBy:              model.GatewayPricingRuleCreatedByManual,
	}).Error)
	require.NoError(t, store.DB.Where("id = ?", 9100).First(&junk).Error)

	// 退休前：它在清单里（这正是我们要消灭的现状）
	w := ctx.doReq(t, http.MethodGet, "/v1/gateway/models", token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "doubao-seed-2.0-pro")

	// 退休它
	require.NoError(t, pricing.New(store.DB).RetireRule(junk.ID))

	// 退休后：必须从用户的清单里消失
	w = ctx.doReq(t, http.MethodGet, "/v1/gateway/models", token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "doubao-seed-2.0-pro", "退休的废规则不该再出现在用户的模型清单里")
	// 正常模型不受影响
	assert.Contains(t, w.Body.String(), "deepseek-v4-flash")
}

// 从没设置过的用户也必须有兜底模型——链路不能因为"用户没配置"而中断。
func TestGatewayRelaySettings_DefaultsWhenNeverConfigured(t *testing.T) {
	ctx := setupE2E(t)
	seedRelayPricing(t)
	token, _ := ctx.loginHelper(t, "relay-default", "Passw0rd!A")

	w := ctx.doReq(t, http.MethodGet, "/v1/gateway/relay-settings", token, nil)
	require.Equal(t, http.StatusOK, w.Code)

	data := parseResp(t, w)["data"].(map[string]interface{})
	assert.Equal(t, "deepseek-v4-flash", data["default_model"], "没配置过也必须有兜底模型")
	assert.Empty(t, data["model_map"])
}

// 保存后立即可读回：网关每次请求实时读这份设置，所以保存即生效。
func TestGatewayRelaySettings_SaveAndReadBack(t *testing.T) {
	ctx := setupE2E(t)
	seedRelayPricing(t)
	token, _ := ctx.loginHelper(t, "relay-save", "Passw0rd!A")

	w := ctx.doReq(t, http.MethodPut, "/v1/gateway/relay-settings", token, map[string]interface{}{
		"default_model": "deepseek-v4-pro",
		"model_map": map[string]string{
			"claude-opus-4-8": "deepseek-v4-pro",
			"gpt-5-codex":     "deepseek-v4-flash",
		},
	})
	require.Equal(t, http.StatusOK, w.Code)

	w = ctx.doReq(t, http.MethodGet, "/v1/gateway/relay-settings", token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	data := parseResp(t, w)["data"].(map[string]interface{})
	assert.Equal(t, "deepseek-v4-pro", data["default_model"])

	mm := data["model_map"].(map[string]interface{})
	assert.Equal(t, "deepseek-v4-pro", mm["claude-opus-4-8"])
	assert.Equal(t, "deepseek-v4-flash", mm["gpt-5-codex"])
}

// 映射目标必须是后端真支持的模型：让一条指向不存在模型的映射落库，
// 等于给用户埋一个只在真正发请求时才炸的雷，必须当场拒绝。
func TestGatewayRelaySettings_RejectsUnservableTarget(t *testing.T) {
	ctx := setupE2E(t)
	seedRelayPricing(t)
	token, _ := ctx.loginHelper(t, "relay-reject", "Passw0rd!A")

	// 兜底模型不合法
	w := ctx.doReq(t, http.MethodPut, "/v1/gateway/relay-settings", token, map[string]interface{}{
		"default_model": "deepseek-v9-nonexistent",
		"model_map":     map[string]string{},
	})
	assert.Equal(t, http.StatusBadRequest, w.Code, "兜底模型不可用必须拒绝")

	// 映射目标不合法
	w = ctx.doReq(t, http.MethodPut, "/v1/gateway/relay-settings", token, map[string]interface{}{
		"default_model": "deepseek-v4-flash",
		"model_map":     map[string]string{"claude-opus-4-8": "deepseek-v9-nonexistent"},
	})
	assert.Equal(t, http.StatusBadRequest, w.Code, "映射目标不可用必须拒绝")

	// 客户端侧模型名（映射的 key）不校验：用户爱写什么写什么，
	// 写错也只会落到兜底模型，不会挂。
	w = ctx.doReq(t, http.MethodPut, "/v1/gateway/relay-settings", token, map[string]interface{}{
		"default_model": "deepseek-v4-flash",
		"model_map":     map[string]string{"随便写的客户端模型名": "deepseek-v4-pro"},
	})
	assert.Equal(t, http.StatusOK, w.Code, "客户端侧模型名不该被校验")
}

// Agent 列表要标出每个 Agent 能不能接中转、接没接——
// 不支持的类型必须明确标注，否则用户会疑惑"为什么 Gemini 不扣我的钱"。
func TestGatewayListAgents_MarksSupportAndConfigured(t *testing.T) {
	ctx := setupE2E(t)
	seedRelayPricing(t)
	token, ownerID := ctx.loginHelper(t, "relay-agents", "Passw0rd!A")

	require.NoError(t, store.DB.Create(&model.Agent{
		ID:              9001,
		OwnerID:         ownerID,
		AgentName:       "我的Claude",
		AgentClientType: model.AgentClientTypeClaude,
	}).Error)
	require.NoError(t, store.DB.Create(&model.Agent{
		ID:              9002,
		OwnerID:         ownerID,
		AgentName:       "小Gemini",
		AgentClientType: model.AgentClientTypeGemini,
	}).Error)

	w := ctx.doReq(t, http.MethodGet, "/v1/gateway/agents", token, nil)
	require.Equal(t, http.StatusOK, w.Code)

	items := parseResp(t, w)["data"].(map[string]interface{})["items"].([]interface{})
	require.Len(t, items, 2)

	byName := map[string]map[string]interface{}{}
	for _, it := range items {
		m := it.(map[string]interface{})
		byName[m["agent_name"].(string)] = m
	}

	assert.Equal(t, true, byName["我的Claude"]["supported"], "Claude 能接中转")
	assert.Equal(t, false, byName["我的Claude"]["configured"], "还没接入")

	assert.Equal(t, false, byName["小Gemini"]["supported"], "Gemini 接不了中转，必须标出来")
}

// 已删除的 Agent（软删除，status=3）不能出现在中转设置的 Agent 列表里——
// 删除只是把 status 置 3，行还在表里，列表查询必须显式排掉。
func TestGatewayListAgents_ExcludesDeletedAgent(t *testing.T) {
	ctx := setupE2E(t)
	seedRelayPricing(t)
	token, ownerID := ctx.loginHelper(t, "relay-agents-deleted", "Passw0rd!A")

	require.NoError(t, store.DB.Create(&model.Agent{
		ID:              9101,
		OwnerID:         ownerID,
		AgentName:       "还活着的Claude",
		AgentClientType: model.AgentClientTypeClaude,
	}).Error)
	require.NoError(t, store.DB.Create(&model.Agent{
		ID:              9102,
		OwnerID:         ownerID,
		AgentName:       "已删除的Claude",
		AgentClientType: model.AgentClientTypeClaude,
		Status:          3,
	}).Error)

	w := ctx.doReq(t, http.MethodGet, "/v1/gateway/agents", token, nil)
	require.Equal(t, http.StatusOK, w.Code)

	items := parseResp(t, w)["data"].(map[string]interface{})["items"].([]interface{})
	require.Len(t, items, 1, "已删除的 agent 不该出现在列表里")
	assert.Equal(t, "还活着的Claude", items[0].(map[string]interface{})["agent_name"])
}

// 桌面端直连本地Connector改造的新入口：走真实路由+JWT鉴权，签发结果必须直接在HTTP
// 响应里带出明文virtual_key和两个协议地址，不再依赖Redis广播这条旧路径。
func TestGatewayIssueAgentRelayCredential_ReturnsPlaintextKeyOverHTTP(t *testing.T) {
	ctx := setupE2E(t)
	token, ownerID := ctx.loginHelper(t, "relay-credential", "Passw0rd!A")

	require.NoError(t, store.DB.Create(&model.Agent{
		ID:              9201,
		OwnerID:         ownerID,
		AgentName:       "桌面Claude",
		AgentClientType: model.AgentClientTypeClaude,
	}).Error)

	w := ctx.doReq(t, http.MethodPost, "/v1/gateway/agents/9201/relay-credential", token, nil)
	require.Equal(t, http.StatusOK, w.Code)

	data := parseResp(t, w)["data"].(map[string]interface{})
	assert.NotEmpty(t, data["virtual_key"])
	assert.Contains(t, data["anthropic_base_url"], "/anthropic/v1")
	assert.Contains(t, data["openai_base_url"], "/openai/v1")
}

// 不是自己名下的 Agent 不能签到凭证，避免越权拿到别人Agent的可用中转Key。
func TestGatewayIssueAgentRelayCredential_ForbidsOtherUsersAgent(t *testing.T) {
	ctx := setupE2E(t)
	_, ownerID := ctx.loginHelper(t, "relay-credential-owner", "Passw0rd!A")
	attackerToken, _ := ctx.loginHelper(t, "relay-credential-attacker", "Passw0rd!A")

	require.NoError(t, store.DB.Create(&model.Agent{
		ID:              9301,
		OwnerID:         ownerID,
		AgentName:       "别人的Claude",
		AgentClientType: model.AgentClientTypeClaude,
	}).Error)

	w := ctx.doReq(t, http.MethodPost, "/v1/gateway/agents/9301/relay-credential", attackerToken, nil)
	require.Equal(t, http.StatusForbidden, w.Code)
}

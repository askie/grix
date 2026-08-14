package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/askie/grix/backend/internal/gateway/pricing"
	"github.com/askie/grix/backend/internal/gateway/relay"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
)

// newRelayTestHandler 起一个只带 Pricing + Relay 的 Handler（模型解析不碰钱包和上游），
// 并把两个后端模型的基准价灌进价目表——"后端支持的模型"就是"价目表里有基准价的模型"。
func newRelayTestHandler(t *testing.T) (*Handler, *gorm.DB) {
	t.Helper()
	tdb := testutil.NewTestDB()
	db := tdb.DB

	for _, m := range []string{"deepseek-v4-flash", "deepseek-v4-pro"} {
		require.NoError(t, db.Create(&model.GatewayPricingRule{
			ID:                     int64(len(m)) + int64(m[0]), // 任意唯一ID
			Provider:               "deepseek",
			Model:                  m,
			CachedInputPricePerM:   decimal.NewFromFloat(0.01),
			UncachedInputPricePerM: decimal.NewFromFloat(0.07),
			OutputPricePerM:        decimal.NewFromFloat(0.11),
			SourceCurrency:         "USD",
			CreatedBy:              model.GatewayPricingRuleCreatedByManual,
		}).Error)
	}

	return &Handler{
		Pricing: pricing.New(db),
		Relay:   relay.New(db, nil), // nil Redis：缓存缺席时必须降级为直接查库
	}, db
}

func putRelaySettings(t *testing.T, db *gorm.DB, walletID int64, defaultModel string, modelMap map[string]string) {
	t.Helper()
	jm := datatypes.JSONMap{}
	for k, v := range modelMap {
		jm[k] = v
	}
	require.NoError(t, db.Create(&model.GatewayRelaySetting{
		WalletID:     walletID,
		DefaultModel: defaultModel,
		ModelMap:     jm,
	}).Error)
}

// 映射命中：客户端报什么名字不重要，按用户配的映射走。
func TestResolveEffectiveModel_MappingHit(t *testing.T) {
	h, db := newRelayTestHandler(t)
	putRelaySettings(t, db, 1, "deepseek-v4-flash", map[string]string{
		"claude-opus-4-8": "deepseek-v4-pro",
	})

	assert.Equal(t, "deepseek-v4-pro", h.resolveEffectiveModel(1, "claude-opus-4-8"))
}

// 未命中映射，但请求的模型本身后端就支持 → 直接用它，不该被兜底模型顶掉。
func TestResolveEffectiveModel_PassThroughServable(t *testing.T) {
	h, db := newRelayTestHandler(t)
	putRelaySettings(t, db, 1, "deepseek-v4-flash", nil)

	assert.Equal(t, "deepseek-v4-pro", h.resolveEffectiveModel(1, "deepseek-v4-pro"))
}

// 核心兜底：Claude/Codex 发布的任何新模型名网关都不认识，必须落到兜底模型，
// 绝不能 400——上游一发版这边就挂是不可接受的。
func TestResolveEffectiveModel_UnknownModelFallsBack(t *testing.T) {
	h, db := newRelayTestHandler(t)
	putRelaySettings(t, db, 1, "deepseek-v4-flash", map[string]string{
		"claude-opus-4-8": "deepseek-v4-pro",
	})

	// 没配映射的新模型、以及压根没见过的名字，一律落兜底
	assert.Equal(t, "deepseek-v4-flash", h.resolveEffectiveModel(1, "claude-sonnet-9-9"))
	assert.Equal(t, "deepseek-v4-flash", h.resolveEffectiveModel(1, "gpt-6-turbo"))
	assert.Equal(t, "deepseek-v4-flash", h.resolveEffectiveModel(1, "随便写的名字"))
}

// 用户从没进过设置页（没有设置行）：也必须有兜底，回落到系统默认模型。
func TestResolveEffectiveModel_NoSettingsRowUsesSystemDefault(t *testing.T) {
	h, _ := newRelayTestHandler(t)

	assert.Equal(t, "deepseek-v4-flash", h.resolveEffectiveModel(999, "claude-opus-4-8"))
}

// 塘主在后台改了系统默认模型 → 没有自己设置的用户立即跟着走新的默认值。
func TestResolveEffectiveModel_SystemDefaultOverride(t *testing.T) {
	h, db := newRelayTestHandler(t)
	raw, err := json.Marshal("deepseek-v4-pro")
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.SystemSetting{
		Key:   relay.SystemDefaultModelKey,
		Value: datatypes.JSON(raw),
	}).Error)

	assert.Equal(t, "deepseek-v4-pro", h.resolveEffectiveModel(999, "claude-opus-4-8"))
}

// 映射指向一个已经被下架（价目表里没有）的模型时，仍按用户的映射走——
// 这样后续会明确报 unpriced/unsupported，而不是被兜底悄悄换成另一个模型去花钱。
// 用户配错了要能看见，不能静默替换。
func TestResolveEffectiveModel_MappingWinsEvenIfTargetUnpriced(t *testing.T) {
	h, db := newRelayTestHandler(t)
	putRelaySettings(t, db, 1, "deepseek-v4-flash", map[string]string{
		"claude-opus-4-8": "deepseek-v9-nonexistent",
	})

	assert.Equal(t, "deepseek-v9-nonexistent", h.resolveEffectiveModel(1, "claude-opus-4-8"))
}

// 改写请求体只动 model，其余字段（messages/stream/tools...）必须原样保留。
func TestRewriteBodyModel_PreservesOtherFields(t *testing.T) {
	raw := []byte(`{"model":"claude-opus-4-8","stream":true,"messages":[{"role":"user","content":"hi"}],"max_tokens":1024}`)

	out, err := rewriteBodyModel(raw, "deepseek-v4-pro")
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(out, &body))
	assert.Equal(t, "deepseek-v4-pro", body["model"])
	assert.Equal(t, true, body["stream"])
	assert.Equal(t, float64(1024), body["max_tokens"])
	assert.Len(t, body["messages"], 1)
}

func TestRewriteBodyModel_InvalidJSON(t *testing.T) {
	_, err := rewriteBodyModel([]byte("not json"), "deepseek-v4-pro")
	assert.Error(t, err)
}

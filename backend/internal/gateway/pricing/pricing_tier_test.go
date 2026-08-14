package pricing

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
)

func TestTierContains(t *testing.T) {
	assert.True(t, tierContains(0, 32000, 0))       // 左闭
	assert.True(t, tierContains(0, 32000, 31999))   // 区间内
	assert.False(t, tierContains(0, 32000, 32000))  // 右开
	assert.True(t, tierContains(32000, 128000, 32000))
	assert.False(t, tierContains(32000, 128000, 128000))
}

func TestTiersOverlap(t *testing.T) {
	assert.True(t, tiersOverlap(0, 32000, 31999, 64000))   // 相交
	assert.False(t, tiersOverlap(0, 32000, 32000, 128000)) // 边界相接不算相交
	assert.True(t, tiersOverlap(0, 128000, 32000, 64000))  // 包含
	assert.False(t, tiersOverlap(0, 32000, 64000, 128000)) // 分离
}

func newPricingTestService(t *testing.T) *Service {
	t.Helper()
	logger.Init()
	_ = snowflake.Init(1)
	tdb := testutil.NewTestDB()
	t.Cleanup(tdb.Close)
	return New(tdb.DB)
}

func intp(v int) *int { return &v }

// 火山豆包 2.0 场景：兜底价=最高档，低两档用输入分档覆盖。
func seedVolcanoTiers(t *testing.T, s *Service) {
	t.Helper()
	one := decimal.NewFromInt(1)
	// 兜底 = 最高档 [128k,256k] 的价：9.6/1.92/48
	_, err := s.CreateRule(CreateRuleInput{
		Provider: "volcano_ark", Model: "doubao-x",
		Cached: "1.92", Uncached: "9.6", Output: "48",
		SourceCurrency: "USD", FxRate: one,
	})
	require.NoError(t, err)
	// [0,32k)：3.2/0.64/16
	_, err = s.CreateRule(CreateRuleInput{
		Provider: "volcano_ark", Model: "doubao-x",
		Cached: "0.64", Uncached: "3.2", Output: "16",
		SourceCurrency: "USD", FxRate: one,
		TierStartTokens: intp(0), TierEndTokens: intp(32000),
	})
	require.NoError(t, err)
	// [32k,128k)：4.8/0.96/24
	_, err = s.CreateRule(CreateRuleInput{
		Provider: "volcano_ark", Model: "doubao-x",
		Cached: "0.96", Uncached: "4.8", Output: "24",
		SourceCurrency: "USD", FxRate: one,
		TierStartTokens: intp(32000), TierEndTokens: intp(128000),
	})
	require.NoError(t, err)
}

func TestRuleFor_InputTierSelection(t *testing.T) {
	s := newPricingTestService(t)
	seedVolcanoTiers(t, s)
	at := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)

	// 短输入命中低档
	rule, err := s.RuleFor("volcano_ark", "doubao-x", at, 10_000)
	require.NoError(t, err)
	assert.Equal(t, "3.2", rule.UncachedInputPricePerM.String())

	// 中输入命中中档（32k 左闭）
	rule, err = s.RuleFor("volcano_ark", "doubao-x", at, 32_000)
	require.NoError(t, err)
	assert.Equal(t, "4.8", rule.UncachedInputPricePerM.String())

	// 超出所有分档 → 落兜底（最高档价，保证不亏）
	rule, err = s.RuleFor("volcano_ark", "doubao-x", at, 200_000)
	require.NoError(t, err)
	assert.Equal(t, "9.6", rule.UncachedInputPricePerM.String())
}

func TestCalculate_UsesInputTierByCachedPlusUncached(t *testing.T) {
	s := newPricingTestService(t)
	seedVolcanoTiers(t, s)
	at := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)

	// 缓存 30k + 未命中 5k = 输入 35k → 中档(4.8/0.96/24)
	cost, rule, err := s.Calculate("volcano_ark", "doubao-x", Usage{
		CachedTokens: 30_000, UncachedTokens: 5_000, CompletionTokens: 1_000,
	}, at)
	require.NoError(t, err)
	assert.Equal(t, "4.8", rule.UncachedInputPricePerM.String())
	// 30k/1M*0.96 + 5k/1M*4.8 + 1k/1M*24 = 0.0288 + 0.024 + 0.024 = 0.0768
	assert.Equal(t, "0.0768", cost.String())
}

func TestCreateRule_TierValidationAndOverlap(t *testing.T) {
	s := newPricingTestService(t)
	one := decimal.NewFromInt(1)

	// 只给一端 → 拒绝
	_, err := s.CreateRule(CreateRuleInput{
		Provider: "p", Model: "m", Cached: "1", Uncached: "1", Output: "1",
		SourceCurrency: "USD", FxRate: one, TierStartTokens: intp(0),
	})
	assert.ErrorContains(t, err, "both be set")

	// start>=end → 拒绝
	_, err = s.CreateRule(CreateRuleInput{
		Provider: "p", Model: "m", Cached: "1", Uncached: "1", Output: "1",
		SourceCurrency: "USD", FxRate: one, TierStartTokens: intp(100), TierEndTokens: intp(100),
	})
	assert.ErrorContains(t, err, "start < end")

	// 建 [0,32k) 后，再建起点不同但区间相交的 [31k,64k) → 拒绝
	_, err = s.CreateRule(CreateRuleInput{
		Provider: "p", Model: "m", Cached: "1", Uncached: "1", Output: "1",
		SourceCurrency: "USD", FxRate: one, TierStartTokens: intp(0), TierEndTokens: intp(32000),
	})
	require.NoError(t, err)
	_, err = s.CreateRule(CreateRuleInput{
		Provider: "p", Model: "m", Cached: "2", Uncached: "2", Output: "2",
		SourceCurrency: "USD", FxRate: one, TierStartTokens: intp(31000), TierEndTokens: intp(64000),
	})
	assert.ErrorContains(t, err, "overlaps")

	// 同起点同档重录 → 收口替换，不报错
	_, err = s.CreateRule(CreateRuleInput{
		Provider: "p", Model: "m", Cached: "3", Uncached: "3", Output: "3",
		SourceCurrency: "USD", FxRate: one, TierStartTokens: intp(0), TierEndTokens: intp(32000),
	})
	require.NoError(t, err)
	rule, err := s.RuleFor("p", "m", time.Now().UTC(), 1_000)
	require.NoError(t, err)
	assert.Equal(t, "3", rule.UncachedInputPricePerM.String())

	// 边界相接 [32k,128k) 不算重叠 → 允许
	_, err = s.CreateRule(CreateRuleInput{
		Provider: "p", Model: "m", Cached: "4", Uncached: "4", Output: "4",
		SourceCurrency: "USD", FxRate: one, TierStartTokens: intp(32000), TierEndTokens: intp(128000),
	})
	assert.NoError(t, err)
}

func TestRuleFor_TierBeatsWindow(t *testing.T) {
	s := newPricingTestService(t)
	one := decimal.NewFromInt(1)
	// 兜底
	_, err := s.CreateRule(CreateRuleInput{
		Provider: "p", Model: "m", Cached: "1", Uncached: "10", Output: "1",
		SourceCurrency: "USD", FxRate: one,
	})
	require.NoError(t, err)
	// 分时档：北京 00:30-08:30
	_, err = s.CreateRule(CreateRuleInput{
		Provider: "p", Model: "m", Cached: "1", Uncached: "5", Output: "1",
		SourceCurrency: "USD", FxRate: one, WindowStartMin: intp(30), WindowEndMin: intp(510),
	})
	require.NoError(t, err)
	// 输入档：[0,32k)
	_, err = s.CreateRule(CreateRuleInput{
		Provider: "p", Model: "m", Cached: "1", Uncached: "3", Output: "1",
		SourceCurrency: "USD", FxRate: one, TierStartTokens: intp(0), TierEndTokens: intp(32000),
	})
	require.NoError(t, err)

	// 北京 01:00（分时命中）+ 输入 10k（分档命中）→ 分档优先
	at := time.Date(2026, 7, 2, 17, 0, 0, 0, time.UTC) // 北京 01:00
	rule, err := s.RuleFor("p", "m", at, 10_000)
	require.NoError(t, err)
	assert.Equal(t, "3", rule.UncachedInputPricePerM.String())

	// 分档不命中（200k）但分时命中 → 用分时档
	rule, err = s.RuleFor("p", "m", at, 200_000)
	require.NoError(t, err)
	assert.Equal(t, "5", rule.UncachedInputPricePerM.String())

	// 都不命中 → 兜底
	atNoon := time.Date(2026, 7, 3, 4, 0, 0, 0, time.UTC) // 北京 12:00
	rule, err = s.RuleFor("p", "m", atNoon, 200_000)
	require.NoError(t, err)
	assert.Equal(t, "10", rule.UncachedInputPricePerM.String())
}

func TestDefaultRule_IgnoresTierRules(t *testing.T) {
	s := newPricingTestService(t)
	one := decimal.NewFromInt(1)
	// 只有分档规则、没有兜底 → DefaultRule 报错（预检会挡住）
	_, err := s.CreateRule(CreateRuleInput{
		Provider: "p", Model: "m", Cached: "1", Uncached: "1", Output: "1",
		SourceCurrency: "USD", FxRate: one, TierStartTokens: intp(0), TierEndTokens: intp(32000),
	})
	require.NoError(t, err)
	_, err = s.DefaultRule("p", "m")
	assert.ErrorIs(t, err, ErrNoPricingRule)

	// 补兜底后能取到，且取到的是兜底那条
	_, err = s.CreateRule(CreateRuleInput{
		Provider: "p", Model: "m", Cached: "9", Uncached: "9", Output: "9",
		SourceCurrency: "USD", FxRate: one,
	})
	require.NoError(t, err)
	rule, err := s.DefaultRule("p", "m")
	require.NoError(t, err)
	assert.Nil(t, rule.InputTierStartTokens)
	assert.Equal(t, "9", rule.UncachedInputPricePerM.String())
}

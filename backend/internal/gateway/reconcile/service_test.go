package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
)

func init() { _ = snowflake.Init(1) }

func decPtr(v float64) *decimal.Decimal {
	d := decimal.NewFromFloat(v)
	return &d
}

// fakeChecker 返回一个固定的当前余额（CNY），用于驱动 RunWindow。
type fakeChecker struct{ balanceCNY decimal.Decimal }

func (f fakeChecker) CurrentBalanceCNY(context.Context) (decimal.Decimal, error) {
	return f.balanceCNY, nil
}

// --- resolveFxRate ---

func TestResolveFxRate_UsesRuleRateWhenPresent(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	svc := New(tdb.DB)

	rule := &model.GatewayPricingRule{FxRateUsed: decPtr(0.14)}
	rate, err := svc.resolveFxRate(rule, time.Now().UTC())
	require.NoError(t, err)
	assert.True(t, rate.Equal(decimal.NewFromFloat(0.14)), "should return the rule's own fx_rate_used")
}

func TestResolveFxRate_FallsBackToLatestFxTable(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	svc := New(tdb.DB)

	now := time.Now().UTC()
	// 三条 CNY→USD：已生效的旧/新两条 + 一条尚未生效(超前)的。asOf=now 时应取"已生效里最新"那条(0.14753)，
	// 既不取更旧的 0.13，也不取尚未生效的 0.99。
	older := model.GatewayFxRate{ID: 1, FromCurrency: "CNY", ToCurrency: "USD", Rate: decimal.NewFromFloat(0.13), EffectiveFrom: now.Add(-48 * time.Hour), Source: "test"}
	newer := model.GatewayFxRate{ID: 2, FromCurrency: "CNY", ToCurrency: "USD", Rate: decimal.NewFromFloat(0.14753), EffectiveFrom: now.Add(-1 * time.Hour), Source: "test"}
	ahead := model.GatewayFxRate{ID: 3, FromCurrency: "CNY", ToCurrency: "USD", Rate: decimal.NewFromFloat(0.99), EffectiveFrom: now.Add(1 * time.Hour), Source: "test"}
	require.NoError(t, tdb.DB.Create(&older).Error)
	require.NoError(t, tdb.DB.Create(&newer).Error)
	require.NoError(t, tdb.DB.Create(&ahead).Error)

	rule := &model.GatewayPricingRule{Provider: "deepseek", Model: "deepseek-v4-flash", FxRateUsed: nil}
	rate, err := svc.resolveFxRate(rule, now)
	require.NoError(t, err)
	// SQLite NUMERIC 列以 REAL 存，Windows 下回读带浮点误差，DB 回读值用容差断言。
	// 候选值 0.13/0.14753/0.99 彼此差百分比量级，1e-9 容差不影响"选对哪条"的测试意图。
	assertDecimalNear(t, decimal.NewFromFloat(0.14753), rate, "should take the latest already-effective CNY→USD rate, not older nor not-yet-effective")
}

// assertDecimalNear 金额/汇率断言：允许 1e-9 以内的存储层浮点误差（跨平台 SQLite 行为差异）。
func assertDecimalNear(t *testing.T, want, got decimal.Decimal, msg string) {
	t.Helper()
	diff := got.Sub(want).Abs()
	assert.True(t, diff.LessThanOrEqual(decimal.New(1, -9)),
		"%s：期望 %s，实际 %s（差 %s）", msg, want.String(), got.String(), diff.String())
}

func TestResolveFxRate_ErrorsWhenNoRuleRateAndNoFxRow(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	svc := New(tdb.DB)

	rule := &model.GatewayPricingRule{Provider: "deepseek", Model: "deepseek-v4-flash", FxRateUsed: nil}
	_, err := svc.resolveFxRate(rule, time.Now().UTC())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no CNY→USD rate")
}

func TestResolveFxRate_ErrorsWhenRateIsZero(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	svc := New(tdb.DB)

	now := time.Now().UTC()
	// 汇率表里有行但值为 0（脏数据）：不能拿 0 去折算，必须报错而不是把厂商花费算成 0。
	require.NoError(t, tdb.DB.Create(&model.GatewayFxRate{ID: 1, FromCurrency: "CNY", ToCurrency: "USD", Rate: decimal.Zero, EffectiveFrom: now.Add(-1 * time.Hour), Source: "test"}).Error)

	rule := &model.GatewayPricingRule{Provider: "deepseek", Model: "deepseek-v4-flash", FxRateUsed: nil}
	_, err := svc.resolveFxRate(rule, now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is zero")
}

// --- RunWindow: USD 原生价目靠回退汇率也能对账（打通 DeepSeek） ---

func TestRunWindow_UsdNativeRuleReconcilesViaFallbackFx(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	db := tdb.DB
	svc := New(db)

	now := time.Now().UTC()
	// DeepSeek 风格价目：source_currency=USD，fx_rate_used 为空。
	require.NoError(t, db.Create(&model.GatewayPricingRule{
		ID: 10, Provider: "deepseek", Model: "deepseek-v4-flash",
		CachedInputPricePerM: decimal.NewFromFloat(1), UncachedInputPricePerM: decimal.NewFromFloat(1), OutputPricePerM: decimal.NewFromFloat(1),
		SourceCurrency: "USD", FxRateUsed: nil, CreatedBy: model.GatewayPricingRuleCreatedByManual, EffectiveFrom: now.Add(-72 * time.Hour),
	}).Error)
	// 回退汇率表里有 fxsync 同步的 CNY→USD。
	require.NoError(t, db.Create(&model.GatewayFxRate{ID: 20, FromCurrency: "CNY", ToCurrency: "USD", Rate: decimal.NewFromFloat(0.14), EffectiveFrom: now.Add(-1 * time.Hour), Source: "test"}).Error)

	// 上一轮基线：余额 100 CNY，窗口结束于 now-2h。
	base := decimal.NewFromFloat(100)
	prevEnd := now.Add(-2 * time.Hour)
	require.NoError(t, db.Create(&model.GatewayReconciliationReport{
		ID: 30, Provider: "deepseek", WindowStart: prevEnd.Add(-time.Hour), WindowEnd: prevEnd,
		VendorActualCost: decimal.Zero, LedgerExpectedCost: decimal.Zero, Diff: decimal.Zero,
		Status: model.GatewayReconciliationStatusOK, VendorBalanceSnapshot: &base, CreatedAt: prevEnd,
	}).Error)
	// 窗口内我方流水：0.7 USD（now-1h 落在 (prevEnd, now] 内）。
	require.NoError(t, db.Create(&model.GatewayLedgerEntry{
		ID: 40, WalletID: 1, VirtualKeyID: 1, RequestID: "r1", Provider: "deepseek", Model: "deepseek-v4-flash",
		Cost: decimal.NewFromFloat(0.7), Status: model.GatewayLedgerStatusSettled, CreatedAt: now.Add(-1 * time.Hour),
	}).Error)

	// 厂商现余额 95 → 花了 5 CNY → 5 * 0.14 = 0.7 USD，正好与流水吻合。
	rep, err := svc.RunWindow(context.Background(), "deepseek", "deepseek-v4-flash", fakeChecker{balanceCNY: decimal.NewFromFloat(95)})
	require.NoError(t, err, "USD 原生价目应能靠回退汇率跑通对账，不再因缺 fx_rate_used 报错")
	require.NotNil(t, rep)
	assertDecimalNear(t, decimal.NewFromFloat(0.7), rep.VendorActualCost, "厂商花费应=5CNY*0.14=0.7USD")
	assert.Equal(t, model.GatewayReconciliationStatusOK, rep.Status)
}

// --- RunWindow: 即便命中调价条件也永不自动调价（锁死） ---

func TestRunWindow_NeverAutoAdjustsEvenWhenTriggered(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	db := tdb.DB
	svc := New(db)

	now := time.Now().UTC()
	// 价目人民币计价：fx_rate_used=0.14。
	require.NoError(t, db.Create(&model.GatewayPricingRule{
		ID: 100, Provider: "deepseek", Model: "deepseek-v4-flash",
		CachedInputPricePerM: decimal.NewFromFloat(1), UncachedInputPricePerM: decimal.NewFromFloat(1), OutputPricePerM: decimal.NewFromFloat(1),
		SourceCurrency: "CNY", FxRateUsed: decPtr(0.14), CreatedBy: model.GatewayPricingRuleCreatedByManual, EffectiveFrom: now.Add(-72 * time.Hour),
	}).Error)

	// 上一轮已越过触发阈值、方向为正（vendor 偏高），构成"连续同向"的前一窗口。
	base := decimal.NewFromFloat(100)
	prevEnd := now.Add(-2 * time.Hour)
	prevRatio := decimal.NewFromFloat(0.10)
	require.NoError(t, db.Create(&model.GatewayReconciliationReport{
		ID: 110, Provider: "deepseek", WindowStart: prevEnd.Add(-time.Hour), WindowEnd: prevEnd,
		VendorActualCost: decimal.NewFromFloat(1.1), LedgerExpectedCost: decimal.NewFromFloat(1), Diff: decimal.NewFromFloat(0.1),
		DiffRatio: &prevRatio, Status: model.GatewayReconciliationStatusWarning, VendorBalanceSnapshot: &base, CreatedAt: prevEnd,
	}).Error)
	// 窗口内流水 1.0 USD（now-1h 落在 (prevEnd, now] 内）。
	require.NoError(t, db.Create(&model.GatewayLedgerEntry{
		ID: 120, WalletID: 1, VirtualKeyID: 1, RequestID: "r2", Provider: "deepseek", Model: "deepseek-v4-flash",
		Cost: decimal.NewFromFloat(1), Status: model.GatewayLedgerStatusSettled, CreatedAt: now.Add(-1 * time.Hour),
	}).Error)

	// 厂商花了约 8.57 CNY → 8.57*0.14≈1.2 USD，比流水高 20% > 5%，与上一轮同向 → 旧逻辑会自动调价。
	rep, err := svc.RunWindow(context.Background(), "deepseek", "deepseek-v4-flash", fakeChecker{balanceCNY: decimal.NewFromFloat(91.4286)})
	require.NoError(t, err)
	require.NotNil(t, rep)

	// 断言：报告落了，但绝不自动调价。
	assert.False(t, rep.AutoAdjusted, "自动调价已锁死，AutoAdjusted 必须为 false")
	require.NotNil(t, rep.DiffRatio)
	assert.True(t, rep.DiffRatio.GreaterThan(decimal.NewFromFloat(0.05)), "diff_ratio 应确实越过触发阈值，证明命中了本会调价的分支，got %s", rep.DiffRatio)

	// 价目表不得被改动：仍只有那条人工规则、仍生效、没有 auto_reconciliation 新规则。
	var rules []model.GatewayPricingRule
	require.NoError(t, db.Where("provider = ?", "deepseek").Find(&rules).Error)
	assert.Len(t, rules, 1, "不应产生任何自动调价规则")
	assert.Equal(t, model.GatewayPricingRuleCreatedByManual, rules[0].CreatedBy)
	assert.Nil(t, rules[0].EffectiveTo, "原人工价目应仍然生效，未被收口")
}

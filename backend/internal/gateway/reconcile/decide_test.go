package reconcile

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/askie/grix/backend/internal/model"
)

func d(s string) decimal.Decimal {
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return v
}

func TestDecide_WithinTolerance_NoAction(t *testing.T) {
	// 差1%，在±2%容忍范围内
	got := decide(d("101"), d("100"), nil, d("1"))
	assert.Equal(t, model.GatewayReconciliationStatusOK, got.Status)
	assert.False(t, got.AutoAdjust)
}

func TestDecide_SingleWindowOver5Percent_WaitsForConfirmation(t *testing.T) {
	// 单次超5%但没有上一轮数据佐证，先观察不调价
	got := decide(d("110"), d("100"), nil, d("1"))
	assert.Equal(t, model.GatewayReconciliationStatusWarning, got.Status)
	assert.False(t, got.AutoAdjust)
}

func TestDecide_ConsecutiveSameDirection_TriggersAutoAdjust(t *testing.T) {
	prevRatio := d("0.10")
	prev := &model.GatewayReconciliationReport{DiffRatio: &prevRatio}
	// 连续两轮都是"厂商多收10%"，方向一致 -> 触发自动调价
	got := decide(d("110"), d("100"), prev, d("1"))
	assert.True(t, got.AutoAdjust)
	assert.Equal(t, model.GatewayReconciliationStatusWarning, got.Status)
	assert.True(t, got.NewPriceRatio.Equal(d("1.10")), "expected ~1.10 got %s", got.NewPriceRatio)
	assert.False(t, got.CumulativeCapped)
}

func TestDecide_ConsecutiveDifferentDirection_DoesNotTrigger(t *testing.T) {
	prevRatio := d("-0.10") // 上一轮是"厂商少收"，这一轮是"厂商多收"，方向不一致
	prev := &model.GatewayReconciliationReport{DiffRatio: &prevRatio}
	got := decide(d("110"), d("100"), prev, d("1"))
	assert.False(t, got.AutoAdjust)
	assert.Equal(t, model.GatewayReconciliationStatusWarning, got.Status)
}

func TestDecide_SingleStepCappedAt20Percent(t *testing.T) {
	prevRatio := d("1.0") // 上一轮差了100%，本轮也一样，方向一致
	prev := &model.GatewayReconciliationReport{DiffRatio: &prevRatio}
	// 厂商实际多收了一倍(200 vs 100)，单次调整应该封顶在20%，不能一次性翻倍
	got := decide(d("200"), d("100"), prev, d("1"))
	assert.True(t, got.AutoAdjust)
	assert.True(t, got.NewPriceRatio.Equal(d("1.20")), "expected capped at 1.20 got %s", got.NewPriceRatio)
}

func TestDecide_CumulativeCapAt50Percent_MarksCritical(t *testing.T) {
	prevRatio := d("0.10")
	prev := &model.GatewayReconciliationReport{DiffRatio: &prevRatio}
	// 距离最近一次人工定价已经累计调整到1.45倍，这次再涨20%就会冲破1.5倍上限，应该被夹住并标记critical
	got := decide(d("110"), d("100"), prev, d("1.45"))
	assert.True(t, got.AutoAdjust)
	assert.Equal(t, model.GatewayReconciliationStatusCritical, got.Status)
	assert.True(t, got.CumulativeCapped)
	// 1.5 / 1.45 ≈ 1.0345，比原本请求的1.10要小
	assert.True(t, got.NewPriceRatio.LessThan(d("1.10")))
}

func TestDecide_PriceDecrease_SymmetricHandling(t *testing.T) {
	prevRatio := d("-0.10")
	prev := &model.GatewayReconciliationReport{DiffRatio: &prevRatio}
	// 厂商实际少收了(90 vs 100)，方向一致地连续两轮 -> 应该对称地往下调
	got := decide(d("90"), d("100"), prev, d("1"))
	assert.True(t, got.AutoAdjust)
	assert.True(t, got.NewPriceRatio.Equal(d("0.90")), "expected ~0.90 got %s", got.NewPriceRatio)
}

func TestDecide_ZeroLedgerCost_NoDivideByZeroPanic(t *testing.T) {
	got := decide(d("5"), d("0"), nil, d("1"))
	assert.Equal(t, model.GatewayReconciliationStatusWarning, got.Status)
	assert.False(t, got.AutoAdjust)
}

func TestDecide_ZeroLedgerCostAndZeroVendorCost_IsOK(t *testing.T) {
	got := decide(d("0"), d("0"), nil, d("1"))
	assert.Equal(t, model.GatewayReconciliationStatusOK, got.Status)
}

func TestDecide_BelowMinVolume_SkipsEvenWithHugeRatio(t *testing.T) {
	// 理论花费只有 0.000006 美元，跟DeepSeek余额显示的0.01元精度是同一量级，
	// 即使账面上"差了100%"也不该拿去触发调价——这是真实测试中发现的场景。
	got := decide(d("0"), d("0.0000062955"), nil, d("1"))
	assert.Equal(t, model.GatewayReconciliationStatusOK, got.Status)
	assert.False(t, got.AutoAdjust)
	assert.Nil(t, got.DiffRatio)
}

func TestDecide_ZeroCumulativeRatio_NoDivideByZeroPanic(t *testing.T) {
	prevRatio := d("0.10")
	prev := &model.GatewayReconciliationReport{DiffRatio: &prevRatio}
	// 累计倍数为0（某档价被录成0导致），过去会在按上限反算stepRatio时Div(0)panic拖垮进程；
	// 现在应判为critical、不自动调价，绝不panic。
	assert.NotPanics(t, func() {
		got := decide(d("110"), d("100"), prev, d("0"))
		assert.Equal(t, model.GatewayReconciliationStatusCritical, got.Status)
		assert.False(t, got.AutoAdjust)
	})
}

func TestDecide_AtCumulativeCap_NoChurn(t *testing.T) {
	prevRatio := d("0.10")
	prev := &model.GatewayReconciliationReport{DiffRatio: &prevRatio}
	// 已经顶到1.5倍上限，这次还想涨——夹出来的stepRatio≈1(=1.5/1.5)，不该再插入一条与现价相同的新规则，
	// 只标critical让人工来看，避免每小时churn。
	got := decide(d("110"), d("100"), prev, d("1.5"))
	assert.Equal(t, model.GatewayReconciliationStatusCritical, got.Status)
	assert.False(t, got.AutoAdjust)
	assert.True(t, got.CumulativeCapped)
}

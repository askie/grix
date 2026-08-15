// Package reconcile 对账引擎：定期比较"厂商真实花费"与"我方流水理论花费"，
// 差异过大时自动等比例调价（不熔断服务）。
package reconcile

import (
	"github.com/shopspring/decimal"

	"github.com/askie/grix/backend/internal/model"
)

var (
	observeTolerance    = decimal.NewFromFloat(0.02) // ±2%：正常抖动，忽略
	triggerThreshold    = decimal.NewFromFloat(0.05) // ±5%：连续2窗口才触发自动调价
	maxSingleStepAdjust = decimal.NewFromFloat(0.20) // 单次最多调20%
	maxCumulativeAdjust = decimal.NewFromFloat(0.50) // 相对最近一次人工定价累计最多调50%

	// minReconcilableCostUSD 是这一轮判断值不值得信的最低理论花费门槛。
	// 实测 DeepSeek /user/balance 只返回2位小数的人民币(如"368.14")，分辨率大约0.01元≈0.0014美元；
	// 窗口内理论花费如果小到跟这个分辨率同一个量级，diff_ratio 纯粹是余额显示精度造成的噪声，
	// 不代表真的有价格问题——这个门槛必须留够安全边际（这里取约10倍分辨率），否则低流量时段会被
	// 误判触发自动调价。
	minReconcilableCostUSD = decimal.NewFromFloat(0.014)

	// adjustNoopEpsilon：调价比例与1的差小于这个值就视为"不动"，避免顶到累计上限后每轮空转churn。
	adjustNoopEpsilon = decimal.NewFromFloat(0.0001)
)

// Decision 是一轮对账算出来的结论：是否需要调价、调到什么程度、这轮的状态是什么。
type Decision struct {
	DiffRatio    *decimal.Decimal
	Status       string // ok / warning / critical
	AutoAdjust   bool
	// NewPriceRatio 是"新价=旧价*NewPriceRatio"里的这个比例系数；AutoAdjust=false 时无意义。
	NewPriceRatio decimal.Decimal
	// CumulativeCapped 表示这次调整已经顶到了相对最近人工定价的±50%上限，需要人工介入。
	CumulativeCapped bool
}

// decide 是纯计算：给定这轮的差值占比、上一轮的报告（可能为nil）、当前价相对最近人工定价的累计倍数，
// 判断这轮该怎么处理。不碰数据库、不碰网络，方便单测覆盖所有分支。
func decide(vendorActualCost, ledgerExpectedCost decimal.Decimal, prev *model.GatewayReconciliationReport, cumulativeRatioSinceManual decimal.Decimal) Decision {
	diff := vendorActualCost.Sub(ledgerExpectedCost)

	if ledgerExpectedCost.IsZero() {
		if diff.Abs().IsZero() {
			return Decision{Status: model.GatewayReconciliationStatusOK}
		}
		// 这段窗口我们完全没收费但厂商却扣了钱（比如免费额度用尽/我们统计有漏），
		// 没法算出比例，只能先报警，不擅自调价。
		return Decision{Status: model.GatewayReconciliationStatusWarning}
	}

	if ledgerExpectedCost.Abs().LessThan(minReconcilableCostUSD) {
		// 这轮窗口理论花费太小，跟厂商余额显示的精度是同一量级，diff_ratio 没有统计意义，
		// 不判定、不计入"连续几轮"的证据链，避免低流量时段被误判触发调价。
		return Decision{Status: model.GatewayReconciliationStatusOK}
	}

	diffRatio := diff.Div(ledgerExpectedCost)

	if diffRatio.Abs().LessThanOrEqual(observeTolerance) {
		return Decision{DiffRatio: &diffRatio, Status: model.GatewayReconciliationStatusOK}
	}

	exceedsTrigger := diffRatio.Abs().GreaterThan(triggerThreshold)
	prevExceededSameDirection := false
	if prev != nil && prev.DiffRatio != nil {
		prevExceededSameDirection = prev.DiffRatio.Abs().GreaterThan(triggerThreshold) && sameSign(*prev.DiffRatio, diffRatio)
	}

	if !exceedsTrigger || !prevExceededSameDirection {
		// 单次超过5%还不动手：可能只是这一小时的偶发波动，等下一轮确认方向是否一致。
		return Decision{DiffRatio: &diffRatio, Status: model.GatewayReconciliationStatusWarning}
	}

	// cumulativeRatioSinceManual 是"当前价相对最近人工定价"的倍数，正常恒>0。
	// 万一它为0（有人把某档价录成0导致代表档为0），后面按上限反算 stepRatio 会 Div(0) panic，
	// 而对账跑在裸 goroutine 里会拖垮整个进程——这里直接判为需人工介入，不自动调价。
	if cumulativeRatioSinceManual.IsZero() {
		return Decision{DiffRatio: &diffRatio, Status: model.GatewayReconciliationStatusCritical}
	}

	rawRatio := vendorActualCost.Div(ledgerExpectedCost)
	stepRatio := clamp(rawRatio, decimal.NewFromInt(1).Sub(maxSingleStepAdjust), decimal.NewFromInt(1).Add(maxSingleStepAdjust))

	projectedCumulative := cumulativeRatioSinceManual.Mul(stepRatio)
	lowBound := decimal.NewFromInt(1).Sub(maxCumulativeAdjust)
	highBound := decimal.NewFromInt(1).Add(maxCumulativeAdjust)

	capped := false
	if projectedCumulative.GreaterThan(highBound) {
		stepRatio = highBound.Div(cumulativeRatioSinceManual)
		capped = true
	} else if projectedCumulative.LessThan(lowBound) {
		stepRatio = lowBound.Div(cumulativeRatioSinceManual)
		capped = true
	}

	// 已经顶到累计上限、这次算出来的 stepRatio 实际等于不动（≈1）：不再插入一条与现价相同的新规则
	// （否则每小时都 churn 一条规则+发一条 critical 告警），只标记 critical 让人工来看。
	if capped && stepRatio.Sub(decimal.NewFromInt(1)).Abs().LessThan(adjustNoopEpsilon) {
		return Decision{DiffRatio: &diffRatio, Status: model.GatewayReconciliationStatusCritical, CumulativeCapped: true}
	}

	status := model.GatewayReconciliationStatusWarning
	if capped {
		status = model.GatewayReconciliationStatusCritical
	}

	return Decision{
		DiffRatio:        &diffRatio,
		Status:           status,
		AutoAdjust:       true,
		NewPriceRatio:    stepRatio,
		CumulativeCapped: capped,
	}
}

func sameSign(a, b decimal.Decimal) bool {
	return a.Sign() == b.Sign()
}

func clamp(v, min, max decimal.Decimal) decimal.Decimal {
	if v.LessThan(min) {
		return min
	}
	if v.GreaterThan(max) {
		return max
	}
	return v
}

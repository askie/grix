package reconcile

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
)

// BalanceChecker 查厂商官方账户当前余额（原始币种，比如DeepSeek是CNY）。
type BalanceChecker interface {
	CurrentBalanceCNY(ctx context.Context) (decimal.Decimal, error)
}

// autoAdjustEnabled 控制对账是否允许自动调价。
// 老郭 2026-07-16 拍板：对账只出报告+告警，永不自动改动价目。
// 保留 applyAutoAdjust/decide 里的调价计算作为将来人工开启的能力，但默认关闭。
const autoAdjustEnabled = false

type Service struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// RunAllActive 对每个配了 BalanceChecker 的厂商跑一轮对账。
// 对账数据源是账户级的（厂商余额、流水都按 provider 汇总，不分 model），所以**每个 provider 每轮只跑一次**，
// 不能按 provider+model 逐条跑——否则同一 provider 下多个模型会各产生一条报告、互相污染"连续窗口"证据链，
// 且只有一个模型的价目会被调价、其余模型永远得不到纠偏（会持续亏钱）。
// 每个 provider 取一个确定性的代表模型（按 model 名排序取第一个）来读汇率/算累计倍数；
// 触发调价时由 applyAutoAdjust 同步等比例调整该 provider 下**所有**生效模型。
func (s *Service) RunAllActive(ctx context.Context, checkers map[string]BalanceChecker) []error {
	var rules []model.GatewayPricingRule
	if err := s.db.Where("effective_to IS NULL").Order("provider ASC, model ASC").Find(&rules).Error; err != nil {
		return []error{fmt.Errorf("load active pricing rules: %w", err)}
	}

	// 每个 provider 取排序后的第一个 model 作代表（Order 已保证 model ASC，故首次遇到即最小）。
	representative := map[string]string{}
	for _, rule := range rules {
		if _, seen := representative[rule.Provider]; !seen {
			representative[rule.Provider] = rule.Model
		}
	}

	var errs []error
	for provider, model_ := range representative {
		checker, ok := checkers[provider]
		if !ok {
			continue
		}
		if _, err := s.RunWindow(ctx, provider, model_, checker); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", provider, err))
		}
	}
	return errs
}

// RunWindow 对一个厂商跑一轮对账：查厂商真实余额变化 vs 我方流水理论花费，
// 按第9.3节的阈值判断状态并落一条对账记录（自动调价已锁死，见 autoAdjustEnabled）。
// 厂商余额是人民币，换算成记账货币USD用的汇率由 resolveFxRate 决定：
// 价目人民币计价时沿用价目锁定汇率（波动自相抵消），价目美元原生时退回实时汇率。
func (s *Service) RunWindow(ctx context.Context, provider, model_ string, checker BalanceChecker) (*model.GatewayReconciliationReport, error) {
	rule, err := s.currentRule(provider, model_)
	if err != nil {
		return nil, fmt.Errorf("no pricing rule for %s/%s: %w", provider, model_, err)
	}

	prev, err := s.latestReport(provider)
	if err != nil {
		return nil, err
	}

	currentBalanceCNY, err := checker.CurrentBalanceCNY(ctx)
	if err != nil {
		return nil, fmt.Errorf("query vendor balance: %w", err)
	}

	windowEnd := time.Now().UTC()
	var windowStart time.Time
	var vendorActualCostUSD decimal.Decimal

	if prev == nil || prev.VendorBalanceSnapshot == nil {
		// 冷启动：没有上一轮基线，没法算出这段时间花了多少，只记一条基线，不判定、不调价。
		windowStart = windowEnd.Add(-1 * time.Hour)
		report := &model.GatewayReconciliationReport{
			ID:                    snowflake.GenID(),
			Provider:              provider,
			WindowStart:           windowStart,
			WindowEnd:             windowEnd,
			VendorActualCost:      decimal.Zero,
			LedgerExpectedCost:    decimal.Zero,
			Diff:                  decimal.Zero,
			Status:                model.GatewayReconciliationStatusOK,
			VendorBalanceSnapshot: &currentBalanceCNY,
		}
		if err := s.db.Create(report).Error; err != nil {
			return nil, err
		}
		return report, nil
	}

	// 非冷启动才需要汇率：冷启动只记基线快照、不折算花费，不该因汇率表暂时没数据而连基线都落不下。
	fxRate, err := s.resolveFxRate(rule, windowEnd)
	if err != nil {
		return nil, err
	}

	windowStart = prev.WindowEnd
	// 余额是随时间减少的（花钱），delta = 上次余额 - 这次余额。
	// 注意：如果运营在这段窗口手动给官方账户充值了，会让这个delta偏小甚至为负，
	// 一期暂不排除这种干扰，人工充值时最好避开对账窗口，或者充值后手动核对一次。
	prevBalanceCNY := *prev.VendorBalanceSnapshot
	deltaCNY := prevBalanceCNY.Sub(currentBalanceCNY)
	vendorActualCostUSD = deltaCNY.Mul(fxRate)

	ledgerExpectedCostUSD, err := s.sumLedgerCost(provider, windowStart, windowEnd)
	if err != nil {
		return nil, err
	}

	cumulativeRatio, err := s.cumulativeRatioSinceManual(provider, model_, rule)
	if err != nil {
		return nil, err
	}

	decision := decide(vendorActualCostUSD, ledgerExpectedCostUSD, prev, cumulativeRatio)

	report := &model.GatewayReconciliationReport{
		ID:                    snowflake.GenID(),
		Provider:              provider,
		WindowStart:           windowStart,
		WindowEnd:             windowEnd,
		VendorActualCost:      vendorActualCostUSD,
		LedgerExpectedCost:    ledgerExpectedCostUSD,
		Diff:                  vendorActualCostUSD.Sub(ledgerExpectedCostUSD),
		DiffRatio:             decision.DiffRatio,
		Status:                decision.Status,
		AutoAdjusted:          autoAdjustEnabled && decision.AutoAdjust,
		VendorBalanceSnapshot: &currentBalanceCNY,
	}

	if err := s.db.Create(report).Error; err != nil {
		return nil, err
	}

	// 自动调价默认锁死（见 autoAdjustEnabled）：即使 decide 判定该调价，也只落报告+告警，不改动价目。
	if autoAdjustEnabled && decision.AutoAdjust {
		if err := s.applyAutoAdjust(provider, decision.NewPriceRatio, report.ID); err != nil {
			return report, fmt.Errorf("record saved but auto-adjust failed: %w", err)
		}
	}

	return report, nil
}

// currentRule 取该 provider+model 的"全天兜底价"当前生效规则作对账锚点。
// 分时/分档定价下同一模型有多档价，对账只需一个稳定锚点取汇率/算累计倍数——用全天兜底档最稳定。
func (s *Service) currentRule(provider, model_ string) (*model.GatewayPricingRule, error) {
	var rule model.GatewayPricingRule
	err := s.db.Where("provider = ? AND model = ? AND effective_to IS NULL AND daily_window_start_min IS NULL AND input_tier_start_tokens IS NULL", provider, model_).
		Order("effective_from DESC").First(&rule).Error
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// resolveFxRate 决定这一轮把"厂商人民币花费"换算成美元用哪个汇率。
// 价目本身是人民币计价（fx_rate_used 非空）：沿用价目锁定的同一汇率，让汇率正常波动在比对里自相抵消。
// 价目是美元原生计价（如 DeepSeek，fx_rate_used 为空）：退回查 gateway_fx_rates 里 fxsync 每天同步的
// CNY→USD 汇率——这样美元原生厂商也能对账（否则永远因缺 fx_rate_used 被跳过），
// 代价是日间汇率漂移会进入 diff，但落在 observeTolerance(±2%) 内、不影响判定。
// asOf 是本轮对账时点：只取截至此刻已生效(effective_from <= asOf)的最新一条，与钱包/fxsync 的取价口径一致，
// 避免取到 fxsync 允许最多超前 1 小时入库、尚未生效的那条。
func (s *Service) resolveFxRate(rule *model.GatewayPricingRule, asOf time.Time) (decimal.Decimal, error) {
	if rule.FxRateUsed != nil {
		return *rule.FxRateUsed, nil
	}
	var fx model.GatewayFxRate
	err := s.db.Where("from_currency = ? AND to_currency = ? AND effective_from <= ?", "CNY", "USD", asOf).
		Order("effective_from DESC").First(&fx).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return decimal.Zero, fmt.Errorf("pricing rule for %s/%s has no fx_rate_used and no CNY→USD rate found in gateway_fx_rates", rule.Provider, rule.Model)
	}
	if err != nil {
		return decimal.Zero, fmt.Errorf("load fallback CNY→USD fx rate for %s/%s: %w", rule.Provider, rule.Model, err)
	}
	if fx.Rate.IsZero() {
		return decimal.Zero, fmt.Errorf("fallback CNY→USD fx rate is zero for %s/%s", rule.Provider, rule.Model)
	}
	return fx.Rate, nil
}

func (s *Service) latestReport(provider string) (*model.GatewayReconciliationReport, error) {
	var report model.GatewayReconciliationReport
	err := s.db.Where("provider = ?", provider).Order("window_end DESC").First(&report).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &report, nil
}

func (s *Service) sumLedgerCost(provider string, start, end time.Time) (decimal.Decimal, error) {
	var entries []model.GatewayLedgerEntry
	err := s.db.Where("provider = ? AND status = ? AND created_at > ? AND created_at <= ?",
		provider, model.GatewayLedgerStatusSettled, start, end).Find(&entries).Error
	if err != nil {
		return decimal.Zero, err
	}
	total := decimal.Zero
	for _, e := range entries {
		total = total.Add(e.Cost)
	}
	return total, nil
}

// cumulativeRatioSinceManual 算出"当前价目相对最近一次人工定价"累计变动了多少倍，
// 用于第9.3节的24小时(实质是"相对最近一次人工定价")累计调整上限判断。
// 用 uncached_input_price_per_m 作为代表性价格档位算比例（三档同步等比例缩放，任取一档即可）。
func (s *Service) cumulativeRatioSinceManual(provider, model_ string, current *model.GatewayPricingRule) (decimal.Decimal, error) {
	var lastManual model.GatewayPricingRule
	err := s.db.Where("provider = ? AND model = ? AND created_by = ? AND daily_window_start_min IS NULL AND input_tier_start_tokens IS NULL", provider, model_, model.GatewayPricingRuleCreatedByManual).
		Order("effective_from DESC").First(&lastManual).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 从没人工定过价（理论上不会发生，价目表至少有一条种子数据），保守起见认为当前价就是基线。
		return decimal.NewFromInt(1), nil
	}
	if err != nil {
		return decimal.Zero, err
	}
	if lastManual.UncachedInputPricePerM.IsZero() {
		return decimal.NewFromInt(1), nil
	}
	return current.UncachedInputPricePerM.Div(lastManual.UncachedInputPricePerM), nil
}

// applyAutoAdjust 把该 provider 下**所有**当前生效的价目规则同步按 ratio 等比例调整。
// 对账是账户级的，一个 provider 下多个模型必须整体同步调价，不能只动一个模型（否则其余模型永不纠偏）。
// 用行锁锁住这些生效规则再收口+建新，避免与人工建价并发时留下多条 effective_to IS NULL 的僵尸行。
func (s *Service) applyAutoAdjust(provider string, ratio decimal.Decimal, reportID int64) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var activeRules []model.GatewayPricingRule
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("provider = ? AND effective_to IS NULL", provider).
			Find(&activeRules).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, old := range activeRules {
			if err := tx.Model(&model.GatewayPricingRule{}).Where("id = ?", old.ID).Update("effective_to", now).Error; err != nil {
				return err
			}
			newRule := model.GatewayPricingRule{
				ID:                     snowflake.GenID(),
				Provider:               old.Provider,
				Model:                  old.Model,
				CachedInputPricePerM:   old.CachedInputPricePerM.Mul(ratio),
				UncachedInputPricePerM: old.UncachedInputPricePerM.Mul(ratio),
				OutputPricePerM:        old.OutputPricePerM.Mul(ratio),
				SourceCurrency:         old.SourceCurrency,
				FxRateUsed:             old.FxRateUsed,
				CreatedBy:              model.GatewayPricingRuleCreatedByAutoReconciliation,
				TriggeredByReportID:    &reportID,
				// 档位维度必须原样带到新规则上：丢了分时/分档字段会让各档全变成兜底价，
				// 同一模型出现多条 NULL 档生效规则，直接撞部分唯一索引导致整个调价事务失败。
				DailyWindowStartMin:  old.DailyWindowStartMin,
				DailyWindowEndMin:    old.DailyWindowEndMin,
				InputTierStartTokens: old.InputTierStartTokens,
				InputTierEndTokens:   old.InputTierEndTokens,
				EffectiveFrom:        now,
			}
			if err := tx.Create(&newRule).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ListReports 分页列出对账报告（管理后台用），可按 provider 过滤。
func (s *Service) ListReports(provider string, page, pageSize int) ([]model.GatewayReconciliationReport, int64, error) {
	q := s.db.Model(&model.GatewayReconciliationReport{})
	if provider != "" {
		q = q.Where("provider = ?", provider)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var reports []model.GatewayReconciliationReport
	if err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&reports).Error; err != nil {
		return nil, 0, err
	}
	return reports, total, nil
}

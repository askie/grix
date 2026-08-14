// Package pricing 按当前生效的价目表把归一化后的usage算成记账货币(USD)成本。
// 扣费路径本身不做任何汇率换算——价目表里存的价格已经是USD，直接乘就是。
package pricing

import (
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
)

var ErrNoPricingRule = errors.New("gateway: no effective pricing rule for provider/model")

// ErrRuleNotFound 表示按 ID 操作的价目规则不存在（调用方可据此回 404 而非 500）。
var ErrRuleNotFound = errors.New("gateway: pricing rule not found")

var million = decimal.NewFromInt(1_000_000)

// beijingOffsetSeconds 是北京时间相对UTC的固定偏移(+8h，无夏令时)。分时价的时段按北京时间划分。
const beijingOffsetSeconds = 8 * 3600

type Service struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// Usage 是各厂商 usage 字段归一化之后的统一结构。
type Usage struct {
	CachedTokens     int
	UncachedTokens   int
	CompletionTokens int
	ReasoningTokens  int
}

// beijingMinuteOfDay 返回 t 对应的"北京时间当日分钟数"[0,1440)。
func beijingMinuteOfDay(t time.Time) int {
	secs := (t.UTC().Unix() + beijingOffsetSeconds) % 86400
	if secs < 0 {
		secs += 86400
	}
	return int(secs / 60)
}

// windowContains 判断分钟数 minute 是否落在[start,end)时段内（左闭右开），支持跨零点(start>end)。
func windowContains(startMin, endMin, minute int) bool {
	if startMin <= endMin {
		return minute >= startMin && minute < endMin
	}
	// 跨零点：如 22:00-02:00 → [1320,1440) ∪ [0,120)
	return minute >= startMin || minute < endMin
}

// windowSegments 把一个[start,end)时段（可能跨零点）拆成不跨零点的[起,止)区间列表。
func windowSegments(startMin, endMin int) [][2]int {
	if startMin <= endMin {
		return [][2]int{{startMin, endMin}}
	}
	return [][2]int{{startMin, 1440}, {0, endMin}}
}

// windowsOverlap 判断两个每日时段（各自可能跨零点）是否有相交分钟。
func windowsOverlap(aStart, aEnd, bStart, bEnd int) bool {
	for _, a := range windowSegments(aStart, aEnd) {
		for _, b := range windowSegments(bStart, bEnd) {
			if a[0] < b[1] && b[0] < a[1] { // 两个左闭右开区间相交
				return true
			}
		}
	}
	return false
}

// tierContains 判断输入token数 tokens 是否落在[start,end)档内（左闭右开，无跨越语义）。
func tierContains(start, end, tokens int) bool {
	return tokens >= start && tokens < end
}

// tiersOverlap 判断两个输入档[start,end)区间是否相交。
func tiersOverlap(aStart, aEnd, bStart, bEnd int) bool {
	return aStart < bEnd && bStart < aEnd
}

// activeRules 取 provider+model 当前所有生效(effective_to IS NULL)的价目规则（含各分时档 + 全天兜底档）。
func (s *Service) activeRules(provider, model_ string) ([]model.GatewayPricingRule, error) {
	var rules []model.GatewayPricingRule
	if err := s.db.
		Where("provider = ? AND model = ? AND effective_to IS NULL", provider, model_).
		Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

// DefaultRule 取 provider+model 的"全天兜底价"(分时、分档维度都为空)当前生效规则。
// 每个模型必须有一条兜底价；分时档/输入档只是在特定时段/特定输入长度覆盖它。
// reconcile 用它作汇率/累计倍数的锚点。
func (s *Service) DefaultRule(provider, model_ string) (*model.GatewayPricingRule, error) {
	rules, err := s.activeRules(provider, model_)
	if err != nil {
		return nil, err
	}
	for i := range rules {
		if rules[i].DailyWindowStartMin == nil && rules[i].DailyWindowEndMin == nil &&
			rules[i].InputTierStartTokens == nil && rules[i].InputTierEndTokens == nil {
			return &rules[i], nil
		}
	}
	return nil, fmt.Errorf("%w: provider=%s model=%s (no all-day base rule)", ErrNoPricingRule, provider, model_)
}

// RuleFor 按"请求完成时刻 at"和"这次请求的输入token数 inputTokens"选出该生效的价目。
// 规则的分时/分档维度为空表示该维度不限；命中维度越多越优先：分档+分时 > 分档 > 分时 > 全天兜底。
// （同一特定性档位之间 CreateRule 已保证区间不重叠，不会出现歧义命中。）
func (s *Service) RuleFor(provider, model_ string, at time.Time, inputTokens int) (*model.GatewayPricingRule, error) {
	rules, err := s.activeRules(provider, model_)
	if err != nil {
		return nil, err
	}
	minute := beijingMinuteOfDay(at)
	var best *model.GatewayPricingRule
	bestScore := -1
	for i := range rules {
		r := &rules[i]
		hasWindow := r.DailyWindowStartMin != nil && r.DailyWindowEndMin != nil
		hasTier := r.InputTierStartTokens != nil && r.InputTierEndTokens != nil
		if hasWindow && !windowContains(*r.DailyWindowStartMin, *r.DailyWindowEndMin, minute) {
			continue
		}
		if hasTier && !tierContains(*r.InputTierStartTokens, *r.InputTierEndTokens, inputTokens) {
			continue
		}
		score := 0
		if hasTier {
			score += 2
		}
		if hasWindow {
			score++
		}
		if score > bestScore {
			best, bestScore = r, score
		}
	}
	if best == nil {
		return nil, fmt.Errorf("%w: provider=%s model=%s", ErrNoPricingRule, provider, model_)
	}
	return best, nil
}

// Calculate 用"请求完成时刻 at"与这次请求的输入规模该生效的价目，算出真实成本(USD)。
func (s *Service) Calculate(provider, model_ string, usage Usage, at time.Time) (decimal.Decimal, *model.GatewayPricingRule, error) {
	inputTokens := usage.CachedTokens + usage.UncachedTokens
	rule, err := s.RuleFor(provider, model_, at, inputTokens)
	if err != nil {
		return decimal.Zero, nil, err
	}

	cachedCost := decimal.NewFromInt(int64(usage.CachedTokens)).
		Div(million).Mul(rule.CachedInputPricePerM)
	uncachedCost := decimal.NewFromInt(int64(usage.UncachedTokens)).
		Div(million).Mul(rule.UncachedInputPricePerM)
	outputCost := decimal.NewFromInt(int64(usage.CompletionTokens)).
		Div(million).Mul(rule.OutputPricePerM)

	total := cachedCost.Add(uncachedCost).Add(outputCost)
	return total, rule, nil
}

// ListRules 分页列出价目规则（管理后台用），可按 provider 过滤，按生效时间倒序（当前生效的排最前）。
func (s *Service) ListRules(provider string, page, pageSize int) ([]model.GatewayPricingRule, int64, error) {
	q := s.db.Model(&model.GatewayPricingRule{})
	if provider != "" {
		q = q.Where("provider = ?", provider)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rules []model.GatewayPricingRule
	if err := q.Order("effective_from DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rules).Error; err != nil {
		return nil, 0, err
	}
	return rules, total, nil
}

// RetireRule 把一条价目规则退休：置 effective_to = now，此后它不再参与计价，也不再被
// "可用模型清单"列出。
//
// 为什么需要它：价目表里混着历史探测留下的废规则（试模型名试出来的别名，如 doubao-seed-2.0-pro，
// 上游根本不认这个名字）。它们对结算无害（结算按上游回显名查价，永远命中不到），
// 但一旦价目表被当作"后端支持哪些模型"的数据源暴露给用户，这些废规则就会出现在用户的模型下拉里，
// 用户选中即报错。垃圾要清掉，而不是在展示层绕过。
//
// 已经退休的规则重复调用不报错（幂等）。
func (s *Service) RetireRule(id int64) error {
	res := s.db.Model(&model.GatewayPricingRule{}).
		Where("id = ? AND effective_to IS NULL", id).
		Update("effective_to", time.Now())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		// 要么不存在，要么已经退休了。区分开：不存在要报错，已退休是幂等成功。
		var count int64
		if err := s.db.Model(&model.GatewayPricingRule{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("%w: rule id=%d", ErrRuleNotFound, id)
		}
	}
	return nil
}

// CreateRuleInput 是手工录入一条价目规则的入参。
// windowStartMin/windowEndMin 为分时定价的时段(北京时间当日分钟数[0,1440))；
// tierStartTokens/tierEndTokens 为按输入token数分档的档位区间[start,end)。
// 各维度两个端点都为 nil 表示该维度不限；全部为 nil 即全天兜底价。
type CreateRuleInput struct {
	Provider        string
	Model           string
	Cached          string
	Uncached        string
	Output          string
	SourceCurrency  string
	FxRate          decimal.Decimal
	WindowStartMin  *int
	WindowEndMin    *int
	TierStartTokens *int
	TierEndTokens   *int
}

// CreateRule 手工录入一条价目规则：传入的价格是官方原始币种下的数值，按 fxRate 换算成USD存库。
// 只收口"同一 provider+model+同一时段"上一条仍生效的规则——录入分时档不会误伤全天兜底价，反之亦然。
func (s *Service) CreateRule(in CreateRuleInput) (*model.GatewayPricingRule, error) {
	if (in.WindowStartMin == nil) != (in.WindowEndMin == nil) {
		return nil, fmt.Errorf("daily window start/end must both be set or both empty")
	}
	if in.WindowStartMin != nil {
		if *in.WindowStartMin < 0 || *in.WindowStartMin >= 1440 || *in.WindowEndMin < 0 || *in.WindowEndMin >= 1440 {
			return nil, fmt.Errorf("daily window minutes must be in [0,1440)")
		}
		if *in.WindowStartMin == *in.WindowEndMin {
			return nil, fmt.Errorf("daily window start and end must differ")
		}
	}
	if (in.TierStartTokens == nil) != (in.TierEndTokens == nil) {
		return nil, fmt.Errorf("input tier start/end must both be set or both empty")
	}
	if in.TierStartTokens != nil {
		if *in.TierStartTokens < 0 || *in.TierEndTokens <= *in.TierStartTokens {
			return nil, fmt.Errorf("input tier must satisfy 0 <= start < end")
		}
	}
	cachedD, err := decimal.NewFromString(in.Cached)
	if err != nil {
		return nil, fmt.Errorf("invalid cached price: %w", err)
	}
	uncachedD, err := decimal.NewFromString(in.Uncached)
	if err != nil {
		return nil, fmt.Errorf("invalid uncached price: %w", err)
	}
	outputD, err := decimal.NewFromString(in.Output)
	if err != nil {
		return nil, fmt.Errorf("invalid output price: %w", err)
	}

	rule := model.GatewayPricingRule{
		CachedInputPricePerM:   cachedD.Mul(in.FxRate),
		UncachedInputPricePerM: uncachedD.Mul(in.FxRate),
		OutputPricePerM:        outputD.Mul(in.FxRate),
		SourceCurrency:         in.SourceCurrency,
		CreatedBy:              model.GatewayPricingRuleCreatedByManual,
		Provider:               in.Provider,
		Model:                  in.Model,
		DailyWindowStartMin:    in.WindowStartMin,
		DailyWindowEndMin:      in.WindowEndMin,
		InputTierStartTokens:   in.TierStartTokens,
		InputTierEndTokens:     in.TierEndTokens,
	}
	if in.SourceCurrency != "USD" {
		rule.FxRateUsed = &in.FxRate
	}

	// 收口范围限定在"同一档位"（同时段起点+同输入档起点，NULL 各算一档）：
	// 兜底价、各分时档、各输入档互不影响，重录同一档只替换该档。
	scopeMatch := func(q *gorm.DB) *gorm.DB {
		if in.WindowStartMin == nil {
			q = q.Where("daily_window_start_min IS NULL")
		} else {
			q = q.Where("daily_window_start_min = ?", *in.WindowStartMin)
		}
		if in.TierStartTokens == nil {
			q = q.Where("input_tier_start_tokens IS NULL")
		} else {
			q = q.Where("input_tier_start_tokens = ?", *in.TierStartTokens)
		}
		return q
	}

	return &rule, s.db.Transaction(func(tx *gorm.DB) error {
		// 先行锁住同 provider+model+档位 当前生效的规则，与对账自动调价串行化，避免并发留下多条生效规则。
		// （migration 089 的部分唯一索引是最终兜底：真撞上了后提交方 INSERT 会唯一冲突失败而非产生僵尸行。）
		var existing []model.GatewayPricingRule
		if err := scopeMatch(tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("provider = ? AND model = ? AND effective_to IS NULL", in.Provider, in.Model)).
			Find(&existing).Error; err != nil {
			return err
		}

		// 同一特定性档位(分时/分档维度"有无"完全一致)之间要防区间重叠：唯一索引只挡"相同起点"，
		// 起点不同但区间相交时，重叠区间按哪条价算不确定，直接拒绝。
		// 不同特定性档位允许共存（命中时按 分档+分时 > 分档 > 分时 > 兜底 择优）。
		if in.WindowStartMin != nil || in.TierStartTokens != nil {
			q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("provider = ? AND model = ? AND effective_to IS NULL", in.Provider, in.Model)
			if in.WindowStartMin != nil {
				q = q.Where("daily_window_start_min IS NOT NULL")
			} else {
				q = q.Where("daily_window_start_min IS NULL")
			}
			if in.TierStartTokens != nil {
				q = q.Where("input_tier_start_tokens IS NOT NULL")
			} else {
				q = q.Where("input_tier_start_tokens IS NULL")
			}
			var others []model.GatewayPricingRule
			if err := q.Find(&others).Error; err != nil {
				return err
			}
			for _, o := range others {
				sameScope := (in.WindowStartMin == nil || *o.DailyWindowStartMin == *in.WindowStartMin) &&
					(in.TierStartTokens == nil || *o.InputTierStartTokens == *in.TierStartTokens)
				if sameScope { // 同档重录，走收口替换，不算冲突
					continue
				}
				windowClash := in.WindowStartMin == nil ||
					windowsOverlap(*in.WindowStartMin, *in.WindowEndMin, *o.DailyWindowStartMin, *o.DailyWindowEndMin)
				tierClash := in.TierStartTokens == nil ||
					tiersOverlap(*in.TierStartTokens, *in.TierEndTokens, *o.InputTierStartTokens, *o.InputTierEndTokens)
				if windowClash && tierClash {
					return fmt.Errorf("new rule overlaps existing rule (id=%d) on window/tier for %s/%s",
						o.ID, in.Provider, in.Model)
				}
			}
		}
		now := time.Now().UTC()
		if err := scopeMatch(tx.Model(&model.GatewayPricingRule{}).
			Where("provider = ? AND model = ? AND effective_to IS NULL", in.Provider, in.Model)).
			Update("effective_to", now).Error; err != nil {
			return err
		}
		rule.ID = snowflake.GenID()
		rule.EffectiveFrom = now
		return tx.Create(&rule).Error
	})
}

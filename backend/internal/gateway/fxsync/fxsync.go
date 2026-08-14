// Package fxsync 定时从外部汇率源同步「源币种→USD」汇率，落 gateway_fx_rates。
// 消费方是充值入账（sourceAmount × rate = 到账USD）和价目表换算，不在计费热路径上。
//
// 汇率写库即等于影响真金白银，故一切异常宁可不写：写库前有合理区间、相邻偏差两道闸，
// 任一不过就拒写并留错误日志，让旧汇率继续兜底（汇率月波动小，可用性优先于新鲜度）。
package fxsync

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
)

// SourceName 标记这批汇率的来源，落在 gateway_fx_rates.source，用于区分人工录入。
const SourceName = "exchangerate-api"

// 默认同步间隔取 24h：免 Key 数据源每 24 小时才刷新一次报价，更密的轮询只会拿到同一份数据。
const DefaultInterval = 24 * time.Hour

// maxDeviation 是新汇率相对当前生效汇率的最大允许偏差。真实汇率日波动远小于 1%，
// 超过 10% 只可能是数据源异常或被投毒，直接拒写。
const maxDeviation = 0.10

// staleAfter 是汇率新鲜度告警线。超过它说明数据源已连续多轮失败，
// 只打错误日志暴露给日志巡检，不拒单——旧汇率照常兜底。
const staleAfter = 72 * time.Hour

// deviationBaseMaxAge 是「当前汇率还能不能用作偏差闸比对基准」的年龄上限，
// 与告警线 staleAfter 刻意解耦，且远大于它。
//
// 不能用 staleAfter(72h) 兼任此职：同步间隔 24h，数据源本身也 24h 才换一次报价，
// 连续三轮拉取失败就会越过 72h，而那只是最寻常的抖动。更危险的是，这会让偏差闸
// 变成可被攻击者亲手关掉的东西——只要先让数据源连续失败三天，第四天再投毒即可。
// 偏差闸唯一要防的就是这个人。
//
// 取 30 天：CNY→USD 走完 10% 的漂移在现代汇率史上以月计（2022 年全年 -11.5% 花了 10 个月），
// 30 天窗口内漂移超阈值没有先例，故「汇率被永久冻死」的风险在此窗口内不成立。
// 真到 30 天仍未恢复，说明错误日志已连喊 27 天无人处理，此时就该人工介入，
// 而不是让系统自动放行未经比对的汇率。
const deviationBaseMaxAge = 30 * 24 * time.Hour

// maxQuoteSkew 是允许数据源报价时间超前本地时钟的上限。超前更多说明源有 bug 或时钟错乱，
// 这种行会先落库、`EffectiveRate` 暂时选不中它，等时间走到却突然生效——必须拒收。
const maxQuoteSkew = time.Hour

// httpTimeout 单次拉取超时；外部源再慢也不能拖住同步协程。
const httpTimeout = 30 * time.Second

// sanityBounds 是各源币种兑 USD 的硬编码绝对区间，每次写库都要过这道闸。
// 新增币种必须在这里补一行，否则同步会被拒——这是刻意的：区间是人工设定的最后一道防线，
// 在没有历史汇率可比对时（首次写入、或旧汇率已过期）它是唯一能挡住脏数据的东西。
// 取值按该币种历史波动区间从宽设定，但不能宽到让投毒数据造成显著资损。
// 它是偏差闸关闭时唯一的防线，因此它的宽度直接等于最坏资损面，宁窄勿宽。
// 触边是 fail-loud 拒写 + 错误日志，改常量重发即可；汇率不会一夜之间跳出区间。
var sanityBounds = map[string]struct{ min, max float64 }{
	// 实际约 0.147。近二十年区间约 [0.1208, 0.1647]（下界为 2005 年汇改前固定 8.28 的倒数，
	// 上界为 2014 年高点）。取 [0.12, 0.18] 把最坏多入账压到 0.18/0.147 ≈ 1.22 倍。
	"CNY": {0.12, 0.18},
}

// conflictClause 是写入汇率时的冲突处理：撞上业务唯一键
// (from_currency, to_currency, effective_from) 即静默忽略。
//
// 必须显式指定冲突目标列，不能用裸的 DoNothing：后者会把**任何**唯一约束冲突都吞掉，
// 包括 snowflake 主键碰撞——那是真事故，不该被误当成「这份报价已存在」而静默放过。
//
// 生产写入与测试共用这个变量，避免测试手抄一份 clause 而测不到生产真正用的写法。
var conflictClause = clause.OnConflict{
	Columns: []clause.Column{
		{Name: "from_currency"}, {Name: "to_currency"}, {Name: "effective_from"},
	},
	DoNothing: true,
}

// Syncer 持有数据源与库连接，一轮同步覆盖配置里的全部币种。
type Syncer struct {
	db         *gorm.DB
	provider   RateProvider
	currencies []string
}

// New 用真实外部数据源构造 Syncer。apiURL 留空走免 Key 公开端点，apiKey 留空即免 Key 模式。
func New(db *gorm.DB, apiURL, apiKey string, currencies []string) *Syncer {
	provider := NewERAPIProvider(apiURL, apiKey, &http.Client{Timeout: httpTimeout})
	return NewWithProvider(db, provider, currencies)
}

// NewWithProvider 便于测试注入假数据源。
func NewWithProvider(db *gorm.DB, provider RateProvider, currencies []string) *Syncer {
	return &Syncer{db: db, provider: provider, currencies: currencies}
}

// SyncOnce 跑一轮：逐个币种拉取、校验、落库，并检查落库后的新鲜度。
// 单个币种失败不影响其它币种，错误只记日志（汇率同步是旁路，永远不能拖垮网关）。
func (s *Syncer) SyncOnce(ctx context.Context) {
	for _, currency := range s.currencies {
		if err := s.syncCurrency(ctx, currency); err != nil {
			logger.L.Errorf("fxsync: sync %s failed: %v", currency, err)
		}
		s.warnIfStale(currency)
	}
}

func (s *Syncer) syncCurrency(ctx context.Context, currency string) error {
	if currency == "USD" {
		return fmt.Errorf("fxsync: USD needs no rate row (EffectiveRate returns 1)")
	}
	rate, quotedAt, err := s.provider.FetchToUSD(ctx, currency)
	if err != nil {
		return err
	}
	if quotedAt.After(time.Now().Add(maxQuoteSkew)) {
		return fmt.Errorf("fxsync: %s quote time %s is in the future, refuse to store",
			currency, quotedAt.UTC().Format(time.RFC3339))
	}
	base, hasBase, err := s.deviationBase(currency)
	if err != nil {
		return err
	}
	if err := validate(currency, rate, base, hasBase); err != nil {
		return err
	}

	// 数据源每 24h 才换一次报价时间，轮询更密会重复拿到同一份；以报价时间做幂等键，
	// 避免同一份汇率在表里堆成上百行。
	//
	// 用「插入即忽略冲突」而非先查后插：跨副本 advisory lock 在取锁失败或非 Postgres 时
	// 会降级放行，check-then-act 在那种情形下挡不住并发重复插入。唯一键
	// (from_currency, to_currency, effective_from) 由 migration 101 保证。
	row := &model.GatewayFxRate{
		ID:            snowflake.GenID(),
		FromCurrency:  currency,
		ToCurrency:    "USD",
		Rate:          rate,
		EffectiveFrom: quotedAt,
		Source:        SourceName,
	}
	res := s.db.Clauses(conflictClause).Create(row)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return nil // 这份报价已在库里（本轮重复拉取，或别的副本刚写完）
	}
	logger.L.Infof("fxsync: %s->USD rate=%s quoted_at=%s stored", currency, rate, quotedAt.UTC().Format(time.RFC3339))
	return nil
}

// validate 是写库前的两道闸：
//   - 绝对区间闸：每次写库都查，是偏差闸关闭时唯一的防线。
//   - 相邻偏差闸：只在拿得到可用比对基准（hasBase）时才查。
//
// 拿不到可用基准（库里没有 / 基准非正数 / 基准超过 deviationBaseMaxAge）时一律**跳过**偏差闸，
// 而不是拒写：一个不可用的旧基准不该把一条已经过了绝对区间闸的健康汇率挡在门外，
// 否则汇率会被冻死且无自动恢复路径。此时绝对区间闸独自把关，这正是它存在的意义。
func validate(currency string, rate, base decimal.Decimal, hasBase bool) error {
	if !rate.IsPositive() {
		return fmt.Errorf("fxsync: %s rate %s is not positive", currency, rate)
	}
	bounds, ok := sanityBounds[currency]
	if !ok {
		return fmt.Errorf("fxsync: %s has no sanity bounds configured, refuse to store", currency)
	}
	if rate.LessThan(decimal.NewFromFloat(bounds.min)) || rate.GreaterThan(decimal.NewFromFloat(bounds.max)) {
		return fmt.Errorf("fxsync: %s rate %s outside sanity bounds [%v, %v], refuse to store",
			currency, rate, bounds.min, bounds.max)
	}
	if !hasBase {
		return nil
	}
	deviation := rate.Sub(base).Abs().Div(base)
	if deviation.GreaterThan(decimal.NewFromFloat(maxDeviation)) {
		return fmt.Errorf("fxsync: %s rate %s deviates %s from base %s, exceeds %.0f%%, refuse to store",
			currency, rate, deviation.Shift(2).StringFixed(2)+"%", base, maxDeviation*100)
	}
	return nil
}

// deviationBase 取偏差闸的比对基准：该币种当前生效汇率
// （与 wallet.EffectiveRate 同口径：effective_from<=now 的最新一条）。
// 基准不存在、非正数、或已超过 deviationBaseMaxAge 时返回 ok=false，调用方据此跳过偏差闸。
// ok=false 时一律返回 decimal.Zero，杜绝调用方误用一个不该用的值。
func (s *Syncer) deviationBase(currency string) (base decimal.Decimal, ok bool, err error) {
	var fx model.GatewayFxRate
	err = s.db.Where("from_currency = ? AND to_currency = ? AND effective_from <= ?", currency, "USD", time.Now()).
		Order("effective_from DESC").First(&fx).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return decimal.Zero, false, nil
	}
	if err != nil {
		return decimal.Zero, false, err
	}
	if !fx.Rate.IsPositive() {
		logger.L.Errorf("fxsync: %s current rate %s is not positive (bad manual entry?), skip deviation check", currency, fx.Rate)
		return decimal.Zero, false, nil
	}
	if age := time.Since(fx.EffectiveFrom); age > deviationBaseMaxAge {
		logger.L.Errorf("fxsync: %s current rate quoted at %s is %.0f days old, exceeds deviation-base max age, skip deviation check (sanity bounds still enforced)",
			currency, fx.EffectiveFrom.UTC().Format(time.RFC3339), age.Hours()/24)
		return decimal.Zero, false, nil
	}
	return fx.Rate, true, nil
}

// warnIfStale 在数据源连续多轮拉不到新报价时把问题暴露到日志，而不是让充值悄悄按老汇率跑下去。
// 查询口径必须与 EffectiveRate/deviationBase 一致（只看 effective_from<=now 的行）：
// 否则一条未来生效的行会让 time.Since 得负数，把「真正在生效的汇率已过期」这件事静默掉。
func (s *Syncer) warnIfStale(currency string) {
	var fx model.GatewayFxRate
	err := s.db.Where("from_currency = ? AND to_currency = ? AND effective_from <= ?", currency, "USD", time.Now()).
		Order("effective_from DESC").First(&fx).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		logger.L.Errorf("fxsync: %s->USD has no rate at all, topup in %s will be rejected", currency, currency)
		return
	}
	if err != nil {
		logger.L.Errorf("fxsync: %s check staleness failed: %v", currency, err)
		return
	}
	if age := time.Since(fx.EffectiveFrom); age > staleAfter {
		logger.L.Errorf("fxsync: %s->USD rate is %.0fh old (quoted at %s), upstream may be down",
			currency, age.Hours(), fx.EffectiveFrom.UTC().Format(time.RFC3339))
	}
}

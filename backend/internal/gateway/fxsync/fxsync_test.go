package fxsync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
)

func TestMain(m *testing.M) {
	logger.Init()
	_ = snowflake.Init(1)
	os.Exit(m.Run())
}

// newDB 建一个只在本用例内存活的库，并把 gorm 句柄直接交给被测代码。
func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB()
	t.Cleanup(func() { db.Close() })
	return db.DB
}

// stubProvider 让测试完全掌控外部数据源返回什么。
type stubProvider struct {
	rate     decimal.Decimal
	quotedAt time.Time
	err      error
}

func (s stubProvider) FetchToUSD(context.Context, string) (decimal.Decimal, time.Time, error) {
	return s.rate, s.quotedAt, s.err
}

func seedRate(t *testing.T, db *gorm.DB, currency string, rate float64, effectiveFrom time.Time) {
	t.Helper()
	require.NoError(t, db.Create(&model.GatewayFxRate{
		ID:            snowflake.GenID(),
		FromCurrency:  currency,
		ToCurrency:    "USD",
		Rate:          decimal.NewFromFloat(rate),
		EffectiveFrom: effectiveFrom,
		Source:        "manual",
	}).Error)
}

func storedRates(t *testing.T, db *gorm.DB, currency string) []model.GatewayFxRate {
	t.Helper()
	var rows []model.GatewayFxRate
	require.NoError(t, db.Where("from_currency = ?", currency).Order("effective_from ASC").Find(&rows).Error)
	return rows
}

// 首次同步（库里无历史）落在合理区间内 → 正常写入，且方向是 CNY→USD（约 0.14 而非 6.8）。
func TestSyncOnce_FirstRateWithinBounds_Stored(t *testing.T) {
	db := newDB(t)
	quotedAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromFloat(0.1468), quotedAt: quotedAt}, []string{"CNY"})

	s.SyncOnce(context.Background())

	rows := storedRates(t, db, "CNY")
	require.Len(t, rows, 1)
	assert.Equal(t, "USD", rows[0].ToCurrency)
	assertDecimalNear(t, decimal.NewFromFloat(0.1468), rows[0].Rate)
	assert.Equal(t, SourceName, rows[0].Source)
	assert.WithinDuration(t, quotedAt, rows[0].EffectiveFrom, time.Second)
}

// 汇率方向写反（拿了 USD→CNY 的 6.81）会导致用户充1元到账6.81美元。
// 绝对区间闸必须把它挡住——无论库里有没有历史汇率。
func TestSyncOnce_FirstRateInvertedDirection_Rejected(t *testing.T) {
	db := newDB(t)
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromFloat(6.81), quotedAt: time.Now()}, []string{"CNY"})

	s.SyncOnce(context.Background())

	assert.Empty(t, storedRates(t, db, "CNY"), "反向汇率必须被合理区间拒绝")
}

// 库里已有历史汇率时，偏差超过 10% 的新汇率一律拒写（数据源异常/投毒）。
func TestSyncOnce_DeviationExceedsLimit_Rejected(t *testing.T) {
	db := newDB(t)
	seedRate(t, db, "CNY", 0.1400, time.Now().Add(-24*time.Hour))
	// 0.1400 → 0.1600 是 +14.3%，超过 10%
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromFloat(0.1600), quotedAt: time.Now()}, []string{"CNY"})

	s.SyncOnce(context.Background())

	rows := storedRates(t, db, "CNY")
	require.Len(t, rows, 1, "只应剩下原有那条")
	// SQLite NUMERIC 列以 REAL 存储，Windows 下 0.14 回读为 0.13999999999999999，
	// 精确相等断言会挂；容差远小于业务偏差闸（10%），不影响测试意图。
	assertDecimalNear(t, decimal.NewFromFloat(0.14), rows[0].Rate)
}

// assertDecimalNear 金额/汇率断言：允许 1e-9 以内的存储层浮点误差（跨平台 SQLite 行为差异）。
func assertDecimalNear(t *testing.T, want, got decimal.Decimal) {
	t.Helper()
	diff := got.Sub(want).Abs()
	assert.True(t, diff.LessThanOrEqual(decimal.New(1, -9)),
		"期望 %s，实际 %s（差 %s）", want.String(), got.String(), diff.String())
}

// 正常日波动（远小于10%）应当照常写入，形成新的一条生效汇率。
func TestSyncOnce_SmallDeviation_Stored(t *testing.T) {
	db := newDB(t)
	seedRate(t, db, "CNY", 0.1400, time.Now().Add(-24*time.Hour))
	quotedAt := time.Now().Add(-time.Minute).Truncate(time.Second)
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromFloat(0.1405), quotedAt: quotedAt}, []string{"CNY"})

	s.SyncOnce(context.Background())

	rows := storedRates(t, db, "CNY")
	require.Len(t, rows, 2)
	assertDecimalNear(t, decimal.NewFromFloat(0.1405), rows[1].Rate)
}

// 唯一约束是幂等的最终兜底：跨副本 advisory lock 在取锁失败/非PG时降级放行，
// 那时只有 (from,to,effective_from) 唯一键挡得住并发重复插入。
// 直接构造"绕过 SyncOnce 的同键插入"，确认它被 ON CONFLICT 静默吸收而非报错。
func TestUniqueKey_DuplicateInsertIsSilentNoOp(t *testing.T) {
	db := newDB(t)
	quotedAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	row := func() *model.GatewayFxRate {
		return &model.GatewayFxRate{
			ID: snowflake.GenID(), FromCurrency: "CNY", ToCurrency: "USD",
			Rate: decimal.NewFromFloat(0.147), EffectiveFrom: quotedAt, Source: SourceName,
		}
	}
	// 用生产同一个 conflictClause，而不是手抄一份——否则改了生产写法这里照样绿。
	first := db.Clauses(conflictClause).Create(row())
	require.NoError(t, first.Error)
	require.EqualValues(t, 1, first.RowsAffected)

	second := db.Clauses(conflictClause).Create(row())
	require.NoError(t, second.Error, "同键重复插入不该报错")
	assert.EqualValues(t, 0, second.RowsAffected, "同键重复插入应为 no-op")
	assert.Len(t, storedRates(t, db, "CNY"), 1)
}

// 冲突目标必须限定在业务唯一键上：snowflake 主键碰撞是真事故，
// 绝不能被当成"这份报价已存在"而静默吞掉。裸的 DoNothing 会吞掉它。
func TestUniqueKey_PrimaryKeyCollisionIsNotSwallowed(t *testing.T) {
	db := newDB(t)
	const sameID = int64(4242)
	mk := func(quotedAt time.Time) *model.GatewayFxRate {
		return &model.GatewayFxRate{
			ID: sameID, FromCurrency: "CNY", ToCurrency: "USD",
			Rate: decimal.NewFromFloat(0.147), EffectiveFrom: quotedAt, Source: SourceName,
		}
	}
	base := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	require.NoError(t, db.Clauses(conflictClause).Create(mk(base)).Error)

	// 业务键不同（报价时间不同），但主键撞了 → 必须报错，而不是 rows=0 静默通过
	res := db.Clauses(conflictClause).Create(mk(base.Add(time.Hour)))
	require.Error(t, res.Error, "主键碰撞必须暴露为错误，不能被 ON CONFLICT 吞掉")
	assert.Len(t, storedRates(t, db, "CNY"), 1)
}

// 数据源每24h才换一次报价时间；轮询更密时不能把同一份汇率重复堆进表里。
func TestSyncOnce_SameQuoteTime_Idempotent(t *testing.T) {
	db := newDB(t)
	quotedAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromFloat(0.1468), quotedAt: quotedAt}, []string{"CNY"})

	s.SyncOnce(context.Background())
	s.SyncOnce(context.Background())
	s.SyncOnce(context.Background())

	assert.Len(t, storedRates(t, db, "CNY"), 1, "同一报价时间只应落一行")
}

// 没有合理区间配置的币种一律拒写——区间是每次写库都要过的最后一道防线。
func TestSyncOnce_UnknownCurrencyNoBounds_Rejected(t *testing.T) {
	db := newDB(t)
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromFloat(0.011), quotedAt: time.Now()}, []string{"JPY"})

	s.SyncOnce(context.Background())

	assert.Empty(t, storedRates(t, db, "JPY"))
}

// 绝对区间闸每次写库都生效，不只在首次。即便库里有历史汇率，
// 越界值也必须被拒（这里 0.6 相对 0.58 只偏差 3.4%，能过偏差闸，但越出 [0.12,0.18]）。
func TestSyncOnce_OutOfBoundsRejectedEvenWithHistory(t *testing.T) {
	db := newDB(t)
	seedRate(t, db, "CNY", 0.58, time.Now().Add(-time.Hour)) // 人工误录的越界值
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromFloat(0.60), quotedAt: time.Now()}, []string{"CNY"})

	s.SyncOnce(context.Background())

	assert.Len(t, storedRates(t, db, "CNY"), 1, "越界值必须被绝对区间闸拒绝")
}

// ⚠ 审查阻断项的回归护栏：偏差闸绝不能被"让数据源连挂几天"这种手段关掉。
// 同步间隔 24h，连挂三轮即越过告警线 72h——那只是最寻常的抖动。若用告警线兼任
// 偏差闸的基准过期阈值，攻击者只要先让源失败三天，第四天投毒即可畅通无阻。
func TestSyncOnce_BaseStalerThanAlertLine_DeviationGateStillActive(t *testing.T) {
	db := newDB(t)
	// 基准 100h 前（已越过 staleAfter=72h 告警线，但远未到 deviationBaseMaxAge=30d）
	seedRate(t, db, "CNY", 0.14, time.Now().Add(-100*time.Hour))
	// 投毒值 0.166：+18.6% 偏差，但落在绝对区间 [0.12,0.18] 内，只有偏差闸拦得住它
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromFloat(0.166), quotedAt: time.Now()}, []string{"CNY"})

	s.SyncOnce(context.Background())

	rows := storedRates(t, db, "CNY")
	require.Len(t, rows, 1, "越过告警线不等于偏差闸失效，投毒值必须被拦下")
	assertDecimalNear(t, decimal.NewFromFloat(0.14), rows[0].Rate)
}

// 告警线内正常波动照常入库（确认上面那条没有把闸焊死）。
func TestSyncOnce_BaseStalerThanAlertLine_NormalDriftStillStored(t *testing.T) {
	db := newDB(t)
	seedRate(t, db, "CNY", 0.14, time.Now().Add(-100*time.Hour))
	quotedAt := time.Now().Add(-time.Minute).Truncate(time.Second)
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromFloat(0.1405), quotedAt: quotedAt}, []string{"CNY"})

	s.SyncOnce(context.Background())

	require.Len(t, storedRates(t, db, "CNY"), 2, "偏差在阈值内应正常入库")
}

// 基准超过 deviationBaseMaxAge(30d) 才跳过偏差闸——防"汇率永久冻死"，
// 且此时错误日志已连喊 27 天，人工介入是合理预期。
func TestSyncOnce_BaseBeyondMaxAge_DeviationGateSkipped(t *testing.T) {
	db := newDB(t)
	// 90天前的旧基准 0.12，与新汇率 0.147 偏差 22.5%
	seedRate(t, db, "CNY", 0.12, time.Now().Add(-90*24*time.Hour))
	quotedAt := time.Now().Add(-time.Minute).Truncate(time.Second)
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromFloat(0.147), quotedAt: quotedAt}, []string{"CNY"})

	s.SyncOnce(context.Background())

	rows := storedRates(t, db, "CNY")
	require.Len(t, rows, 2, "基准超龄应跳过偏差闸写入新汇率，而非永久冻死")
	assertDecimalNear(t, decimal.NewFromFloat(0.147), rows[1].Rate)
}

// 基准超龄也不等于放行脏数据：绝对区间闸依然拦得住反向汇率。
func TestSyncOnce_BaseBeyondMaxAge_StillBoundedBySanity(t *testing.T) {
	db := newDB(t)
	seedRate(t, db, "CNY", 0.12, time.Now().Add(-90*24*time.Hour))
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromFloat(6.81), quotedAt: time.Now()}, []string{"CNY"})

	s.SyncOnce(context.Background())

	assert.Len(t, storedRates(t, db, "CNY"), 1, "基准超龄时仍须由绝对区间闸挡住反向汇率")
}

// 非正数的旧基准（人工灌错/灌了0）不该把健康的新汇率挡在门外，应跳过偏差闸而非拒写。
// 这与"基准超龄"同源：不可用的基准 → 跳闸，绝对区间闸独自把关。
func TestSyncOnce_NonPositiveBase_SkipsGateInsteadOfRejecting(t *testing.T) {
	db := newDB(t)
	seedRate(t, db, "CNY", 0, time.Now().Add(-time.Hour)) // 人工误录的 0
	quotedAt := time.Now().Add(-time.Minute).Truncate(time.Second)
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromFloat(0.147), quotedAt: quotedAt}, []string{"CNY"})

	s.SyncOnce(context.Background())

	rows := storedRates(t, db, "CNY")
	require.Len(t, rows, 2, "非正基准应跳过偏差闸，健康汇率必须能落库")
	assertDecimalNear(t, decimal.NewFromFloat(0.147), rows[1].Rate)
}

// 数据源返回未来的报价时间（源bug/时钟错）会先落库、之后突然生效，必须拒收。
func TestSyncOnce_FutureQuoteTime_Rejected(t *testing.T) {
	db := newDB(t)
	s := NewWithProvider(db, stubProvider{
		rate:     decimal.NewFromFloat(0.147),
		quotedAt: time.Now().Add(25 * time.Hour), // 远超 maxQuoteSkew=1h
	}, []string{"CNY"})

	s.SyncOnce(context.Background())

	assert.Empty(t, storedRates(t, db, "CNY"), "未来报价时间必须拒写")
}

// 轻微超前（时钟漂移）在容差内应放行，不能把正常同步误伤。
func TestSyncOnce_SlightlyFutureQuoteTime_Allowed(t *testing.T) {
	db := newDB(t)
	s := NewWithProvider(db, stubProvider{
		rate:     decimal.NewFromFloat(0.147),
		quotedAt: time.Now().Add(10 * time.Minute), // 在 maxQuoteSkew=1h 容差内
	}, []string{"CNY"})

	s.SyncOnce(context.Background())

	assert.Len(t, storedRates(t, db, "CNY"), 1, "容差内的时钟漂移不应被误伤")
}

// 非正汇率（0 或负数）任何情况下都不能落库。
func TestSyncOnce_NonPositiveRate_Rejected(t *testing.T) {
	db := newDB(t)
	seedRate(t, db, "CNY", 0.1400, time.Now().Add(-24*time.Hour))
	s := NewWithProvider(db, stubProvider{rate: decimal.Zero, quotedAt: time.Now()}, []string{"CNY"})

	s.SyncOnce(context.Background())

	assert.Len(t, storedRates(t, db, "CNY"), 1)
}

// 外部数据源报错时不写任何东西，也不能 panic——同步是旁路，绝不能拖垮网关。
func TestSyncOnce_ProviderError_NoWriteNoPanic(t *testing.T) {
	db := newDB(t)
	s := NewWithProvider(db, stubProvider{err: errors.New("upstream down")}, []string{"CNY"})

	assert.NotPanics(t, func() { s.SyncOnce(context.Background()) })
	assert.Empty(t, storedRates(t, db, "CNY"))
}

// USD 不需要汇率行（EffectiveRate 恒返回1），误配进 currencies 时拒绝落库。
func TestSyncOnce_USDRejected(t *testing.T) {
	db := newDB(t)
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromInt(1), quotedAt: time.Now()}, []string{"USD"})

	s.SyncOnce(context.Background())

	assert.Empty(t, storedRates(t, db, "USD"))
}

// 未来生效的汇率不参与偏差比对（wallet.EffectiveRate 也只认已生效的），
// 否则一条误录的未来汇率会把之后所有正常汇率都挡在门外。
func TestSyncOnce_FutureRateNotUsedAsComparisonBase(t *testing.T) {
	db := newDB(t)
	seedRate(t, db, "CNY", 0.1400, time.Now().Add(-24*time.Hour)) // 当前生效
	seedRate(t, db, "CNY", 9.9999, time.Now().Add(48*time.Hour))  // 未来生效的离谱值
	quotedAt := time.Now().Add(-time.Minute).Truncate(time.Second)
	s := NewWithProvider(db, stubProvider{rate: decimal.NewFromFloat(0.1405), quotedAt: quotedAt}, []string{"CNY"})

	s.SyncOnce(context.Background())

	rows := storedRates(t, db, "CNY")
	require.Len(t, rows, 3, "应与当前生效的 0.14 比对而通过，写入第三条")
}

// ---- provider 层：解析真实响应体结构 ----

func newTestProvider(t *testing.T, handler http.HandlerFunc) RateProvider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewERAPIProvider(srv.URL, "", srv.Client())
}

// 用 base=CNY 拉取，直接取 rates.USD，不做任何倒数换算。
func TestERAPIProvider_ParsesCNYBaseResponse(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/CNY", r.URL.Path, "必须以源币种为 base 请求")
		_, _ = w.Write([]byte(`{"result":"success","base_code":"CNY","time_last_update_unix":1783641751,"rates":{"USD":0.146837,"CNY":1}}`))
	})

	rate, quotedAt, err := p.FetchToUSD(context.Background(), "CNY")

	require.NoError(t, err)
	assert.True(t, rate.Equal(decimal.NewFromFloat(0.146837)), "rate=%s", rate)
	assert.Equal(t, int64(1783641751), quotedAt.Unix())
}

// base_code 与请求币种不符时拒收：拿到的会是别的货币兑美元的汇率。
func TestERAPIProvider_BaseCodeMismatch_Error(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":"success","base_code":"USD","time_last_update_unix":1783641751,"rates":{"USD":1}}`))
	})

	_, _, err := p.FetchToUSD(context.Background(), "CNY")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "base_code")
}

func TestERAPIProvider_UpstreamErrorResult(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":"error","error-type":"unsupported-code"}`))
	})

	_, _, err := p.FetchToUSD(context.Background(), "CNY")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported-code")
}

func TestERAPIProvider_MissingQuoteTime_Error(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":"success","base_code":"CNY","rates":{"USD":0.1468}}`))
	})

	_, _, err := p.FetchToUSD(context.Background(), "CNY")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timestamp")
}

func TestERAPIProvider_HTTPError(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, _, err := p.FetchToUSD(context.Background(), "CNY")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

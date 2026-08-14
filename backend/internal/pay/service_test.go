package pay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pay/channel"
	"github.com/askie/grix/backend/internal/pkg/testutil"
)

// fakeChannel 是可配置的测试通道，用于驱动支付核心的各条路径。
type fakeChannel struct {
	code            string
	curr            []string
	payURL          string
	createTradeNo   string
	createErr       error
	notifyRes       *channel.NotifyResult
	notifyErr       error
	refundState     channel.RefundState
	refundNo        string
	refundErr       error
	queryRes        *channel.TradeStatus
	queryErr        error
	refundNotifyRes *channel.RefundNotifyResult
	redirectCalled  bool
	redirectRes     *channel.NotifyResult
	redirectErr     error
	// queryByTradeNo 让不同支付单返回不同查单结果（非空时优先于 queryRes），
	// 用于扫描类测试里同时摆放废单与真单。
	queryByTradeNo map[int64]*channel.TradeStatus
}

func (f *fakeChannel) Code() string                  { return f.code }
func (f *fakeChannel) SupportedCurrencies() []string { return f.curr }

func (f *fakeChannel) CreatePay(context.Context, *channel.PayRequest) (*channel.PayResult, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &channel.PayResult{PayURL: f.payURL, ChannelTradeNo: f.createTradeNo}, nil
}

func (f *fakeChannel) ParseNotify(context.Context, http.Header, []byte) (*channel.NotifyResult, error) {
	if f.notifyErr != nil {
		return nil, f.notifyErr
	}
	return f.notifyRes, nil
}

func (f *fakeChannel) QueryTrade(_ context.Context, req channel.TradeQueryRequest) (*channel.TradeStatus, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if st, ok := f.queryByTradeNo[req.PayOrderID]; ok {
		return st, nil
	}
	return f.queryRes, nil
}

func (f *fakeChannel) Refund(context.Context, *channel.RefundRequest) (*channel.RefundResult, error) {
	if f.refundErr != nil {
		return nil, f.refundErr
	}
	return &channel.RefundResult{State: f.refundState, ChannelRefundNo: f.refundNo}, nil
}

func (f *fakeChannel) ParseRefundNotify(context.Context, http.Header, []byte) (*channel.RefundNotifyResult, error) {
	return f.refundNotifyRes, nil
}

// CompleteRedirect 让 fakeChannel 实现 channel.RedirectCapturer，驱动
// Service.CompleteRedirect 的二次确认路径。
func (f *fakeChannel) CompleteRedirect(context.Context, channel.TradeQueryRequest) (*channel.NotifyResult, error) {
	f.redirectCalled = true
	if f.redirectErr != nil {
		return nil, f.redirectErr
	}
	return f.redirectRes, nil
}

// fakeNotifier 记录发布过的事件主题与事件体，用于断言；publishErr 非空时模拟发布失败
// （NATS 挂掉 / stream 未就绪 / ack 超时）。
type fakeNotifier struct {
	subjects []string
	events   []any
	// publishErr 非空则所有发布都失败，用来验证"发布失败不得静默吞掉"。
	publishErr error
}

func (f *fakeNotifier) Publish(subject string, event any) error {
	f.subjects = append(f.subjects, subject)
	f.events = append(f.events, event)
	return f.publishErr
}

// lastEvent 返回最后一次发布的事件体。
func (f *fakeNotifier) lastEvent() any {
	if len(f.events) == 0 {
		return nil
	}
	return f.events[len(f.events)-1]
}
func (f *fakeNotifier) count(subject string) int {
	n := 0
	for _, s := range f.subjects {
		if s == subject {
			n++
		}
	}
	return n
}

func newSvc(t *testing.T, ch channel.PaymentChannel) (*Service, *fakeNotifier) {
	t.Helper()
	tdb := testutil.NewTestDB()
	require.NoError(t, tdb.DB.AutoMigrate(&model.PayOrder{}, &model.PayRefund{}, &model.PayNotifyLog{}))
	reg := channel.NewRegistry()
	reg.Register(ch)
	notif := &fakeNotifier{}
	svc := NewService(NewStore(tdb.DB), reg, notif, "https://pay.test")
	var seq int64
	svc.newID = func() int64 { seq++; return seq + 1000 } // 确定性 ID，避免依赖 snowflake 初始化
	return svc, notif
}

func d(i int64) decimal.Decimal { return decimal.NewFromInt(i) }

func TestCreateOrder_SuccessAndIdempotent(t *testing.T) {
	ch := &fakeChannel{code: "cn", curr: []string{"CNY"}, payURL: "https://cashier"}
	svc, _ := newSvc(t, ch)
	req := CreateOrderRequest{BizType: "recharge", BizOrderID: "R1", Channel: "cn", Amount: d(100), Currency: "CNY", Subject: "充值"}

	r1, err := svc.CreateOrder(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "https://cashier", r1.PayURL)
	assert.Equal(t, model.PayOrderStatusCreated, r1.Status)

	r2, err := svc.CreateOrder(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, r1.PayOrderID, r2.PayOrderID) // 同一业务单只对应一张支付单
}

// TestCreateOrder_PersistsChannelTradeNoImmediately 覆盖测试员发现的回归点：
// 部分通道（如 PayPal）下单即返回第三方交易号，必须立即落库，而不是等到
// MarkPaid 才写入——否则用户跳转确认 / 对账查单 / 退款全都拿不到交易号反查第三方。
func TestCreateOrder_PersistsChannelTradeNoImmediately(t *testing.T) {
	ch := &fakeChannel{code: "pp", curr: []string{"USD"}, payURL: "https://paypal/approve", createTradeNo: "PAYPAL-ORDER-1"}
	svc, _ := newSvc(t, ch)
	r, err := svc.CreateOrder(context.Background(), CreateOrderRequest{
		BizType: "recharge", BizOrderID: "R1", Channel: "pp", Amount: d(100), Currency: "USD"})
	require.NoError(t, err)
	assert.Equal(t, "PAYPAL-ORDER-1", r.ChannelTradeNo)

	// 关键断言：查库里的单据本身（不是下单返回值）也已经带上交易号，
	// 这样 CompleteRedirect / QueryTrade / Refund 才能反查到第三方。
	o, err := svc.QueryOrder(r.PayOrderID)
	require.NoError(t, err)
	assert.Equal(t, "PAYPAL-ORDER-1", o.ChannelTradeNo)
}

// TestCompleteRedirect_CapturesAndFinalizes 覆盖 PayPal 式二次确认主链路：
// 下单落库交易号 → 用户跳转确认 → 通道二次确认成功 → 幂等推进 PAID → 发一次事件；
// 重复触发（用户刷新回跳页）不重复调用通道、不重复发事件。
func TestCompleteRedirect_CapturesAndFinalizes(t *testing.T) {
	ch := &fakeChannel{code: "pp", curr: []string{"USD"}, payURL: "u", createTradeNo: "ORDER1"}
	svc, notif := newSvc(t, ch)
	r, err := svc.CreateOrder(context.Background(), CreateOrderRequest{
		BizType: "b", BizOrderID: "o", Channel: "pp", Amount: d(100), Currency: "USD"})
	require.NoError(t, err)

	ch.redirectRes = &channel.NotifyResult{PayOrderID: r.PayOrderID, ChannelTradeNo: "ORDER1",
		State: channel.TradeSuccess, Amount: d(100), Currency: "USD"}
	got, err := svc.CompleteRedirect(context.Background(), r.PayOrderID)
	require.NoError(t, err)
	assert.True(t, ch.redirectCalled)
	assert.Equal(t, model.PayOrderStatusPaid, got.Status)
	assert.Equal(t, 1, notif.count(EventOrderPaid))

	// 重复触发：订单已终态，Service 层直接短路返回，不再调用通道
	ch.redirectCalled = false
	got2, err := svc.CompleteRedirect(context.Background(), r.PayOrderID)
	require.NoError(t, err)
	assert.False(t, ch.redirectCalled, "已终态订单不应再调用通道二次确认")
	assert.Equal(t, model.PayOrderStatusPaid, got2.Status)
	assert.Equal(t, 1, notif.count(EventOrderPaid))
}

// TestCompleteRedirect_SkipsAdapterWhenAlreadyPaidByWebhook 覆盖审查发现的竞态：
// webhook 比用户跳转先一步把单据打成 PAID 后，用户浏览器才落地 /return，
// Service 层必须直接短路，不能再拿（可能已被覆盖成别的值的）ChannelTradeNo
// 发起一次注定失败的二次确认，把已经成功的支付误报成 "failed"。
func TestCompleteRedirect_SkipsAdapterWhenAlreadyPaidByWebhook(t *testing.T) {
	ch := &fakeChannel{code: "pp", curr: []string{"USD"}, payURL: "u", createTradeNo: "ORDER1"}
	svc, notif := newSvc(t, ch)
	r, err := svc.CreateOrder(context.Background(), CreateOrderRequest{
		BizType: "b", BizOrderID: "o", Channel: "pp", Amount: d(100), Currency: "USD"})
	require.NoError(t, err)

	// webhook 先一步入账
	ch.notifyRes = &channel.NotifyResult{PayOrderID: r.PayOrderID, ChannelTradeNo: "ORDER1",
		State: channel.TradeSuccess, Amount: d(100), Currency: "USD"}
	require.NoError(t, svc.HandleNotify(context.Background(), "pp", nil, []byte("{}")))

	// 用户浏览器随后才跳回 /return
	ch.redirectErr = errors.New("boom: 不该调用到这里")
	got, err := svc.CompleteRedirect(context.Background(), r.PayOrderID)
	require.NoError(t, err)
	assert.False(t, ch.redirectCalled, "订单已因 webhook 入账终态，不该再调用通道")
	assert.Equal(t, model.PayOrderStatusPaid, got.Status)
	assert.Equal(t, 1, notif.count(EventOrderPaid))
}

// TestReconcileOne_AuthorizedTriggersCapture 覆盖"防丢钱"补偿：用户在 PayPal approve
// 后没跳回来，capture 从未发生，订单一直 CREATED；对账查到 TradeAuthorized 时必须
// 主动补一次 capture，把订单推进到 PAID 并入账，而不是当作待支付放着不管。
func TestReconcileOne_AuthorizedTriggersCapture(t *testing.T) {
	ch := &fakeChannel{code: "pp", curr: []string{"USD"}, payURL: "u", createTradeNo: "ORDER1"}
	svc, notif := newSvc(t, ch)
	r, err := svc.CreateOrder(context.Background(), CreateOrderRequest{
		BizType: "b", BizOrderID: "o", Channel: "pp", Amount: d(100), Currency: "USD"})
	require.NoError(t, err)

	// 对账查到"已授权未扣款"，二次确认 capture 成功。
	ch.queryRes = &channel.TradeStatus{ChannelTradeNo: "ORDER1", State: channel.TradeAuthorized, Amount: d(100), Currency: "USD"}
	ch.redirectRes = &channel.NotifyResult{PayOrderID: r.PayOrderID, ChannelTradeNo: "ORDER1",
		State: channel.TradeSuccess, Amount: d(100), Currency: "USD"}

	order, err := svc.QueryOrder(r.PayOrderID)
	require.NoError(t, err)
	moved, err := svc.ReconcileOne(context.Background(), order)
	require.NoError(t, err)
	assert.True(t, moved, "补 capture 入账是真的推进了状态")

	assert.True(t, ch.redirectCalled, "已授权未扣款应触发通道二次确认 capture")
	got, err := svc.QueryOrder(r.PayOrderID)
	require.NoError(t, err)
	assert.Equal(t, model.PayOrderStatusPaid, got.Status)
	assert.Equal(t, 1, notif.count(EventOrderPaid))
}

// staleOrder 建一单并把 CreatedAt 拨老 age（内存与库里同时改），用来驱动 ReconcileOne
// 的超龄判定与扫描排序。bizOrderID 必须各不相同——CreateOrder 按它做幂等，重复传同一个
// 只会拿回同一张单。
func staleOrder(t *testing.T, svc *Service, bizOrderID string, age time.Duration) *model.PayOrder {
	t.Helper()
	r, err := svc.CreateOrder(context.Background(), CreateOrderRequest{
		BizType: "b", BizOrderID: bizOrderID, Channel: "pp", Amount: d(100), Currency: "USD"})
	require.NoError(t, err)
	order, err := svc.QueryOrder(r.PayOrderID)
	require.NoError(t, err)
	order.CreatedAt = time.Now().Add(-age)
	require.NoError(t, svc.store.db.Model(&model.PayOrder{}).Where("id = ?", order.ID).
		Update("created_at", order.CreatedAt).Error)
	return order
}

// TestReconcileOne_StalePendingIsClosed 覆盖"对账不被废单饿死"：用户开了收银台又跑掉的
// 单子查单永远是 TradePending，若不给它终态就会永远卡在 CREATED 无限累积。对账扫描有
// LIMIT，废单堆过阈值后每轮只捞得到这批最老的死单，真正需要补偿的「已授权没跳回来」的
// 单子再也扫不到——防丢钱的补偿链路会静默失效。超龄废单必须关掉。
func TestReconcileOne_StalePendingIsClosed(t *testing.T) {
	ch := &fakeChannel{code: "pp", curr: []string{"USD"}, payURL: "u", createTradeNo: "ORDER1"}
	svc, notif := newSvc(t, ch)
	order := staleOrder(t, svc, "junk", staleOrderTTL+time.Hour)

	ch.queryRes = &channel.TradeStatus{ChannelTradeNo: "ORDER1", State: channel.TradePending}
	moved, err := svc.ReconcileOne(context.Background(), order)
	require.NoError(t, err)
	assert.True(t, moved, "关单是真的推进了状态，须如实上报")

	got, err := svc.QueryOrder(order.ID)
	require.NoError(t, err)
	assert.Equal(t, model.PayOrderStatusClosed, got.Status, "超龄废单须关闭，否则会把对账扫描的名额占满")
	// 关单必须发事件：业务方（网关充值单）靠它推进自己的状态机，不发的话充值单会永远
	// 停在 PENDING——支付侧 CLOSED、业务侧还挂着。废单现在会被批量关掉，这个缺口会批量出现。
	assert.Equal(t, 1, notif.count(EventOrderClosed), "关单必须发 EventOrderClosed，否则业务侧的单永远挂着")
}

// TestReconcileOne_FreshPendingIsKept：还在有效期内的未支付单不能关——用户可能正在
// 收银台上付款，关了他就付不成了。
func TestReconcileOne_FreshPendingIsKept(t *testing.T) {
	ch := &fakeChannel{code: "pp", curr: []string{"USD"}, payURL: "u", createTradeNo: "ORDER1"}
	svc, _ := newSvc(t, ch)
	order := staleOrder(t, svc, "fresh", 10*time.Minute) // 远未超龄

	ch.queryRes = &channel.TradeStatus{ChannelTradeNo: "ORDER1", State: channel.TradePending}
	moved, err := svc.ReconcileOne(context.Background(), order)
	require.NoError(t, err)
	assert.False(t, moved, "什么都没做就不算推进——否则日志会把一屋子 no-op 报成"+`"推进了 N 单"`)

	got, err := svc.QueryOrder(order.ID)
	require.NoError(t, err)
	assert.Equal(t, model.PayOrderStatusCreated, got.Status, "有效期内的未支付单不得关闭，用户可能正在付")
}

// TestReconcileOne_SettlingIsNeverClosed 是上面那条关单逻辑的安全绳，防止把丢钱洞从一头
// 堵到另一头：资金待定（PayPal 延审 / e-check）意味着用户确实付了、第三方也受理了，只是
// 钱还在路上。这种单子哪怕挂再久也绝不能当废单关掉——一旦关了，延审通过后 webhook 送来
// 的 COMPLETED 会被 MarkPaid 的 CAS 挡住（状态已不是 CREATED），钱到账了却入不了账。
func TestReconcileOne_SettlingIsNeverClosed(t *testing.T) {
	ch := &fakeChannel{code: "pp", curr: []string{"USD"}, payURL: "u", createTradeNo: "ORDER1"}
	svc, notif := newSvc(t, ch)
	order := staleOrder(t, svc, "settling", 10*staleOrderTTL) // 挂了很久，远超废单阈值

	ch.queryRes = &channel.TradeStatus{ChannelTradeNo: "ORDER1", State: channel.TradeSettling, Amount: d(100), Currency: "USD"}
	moved, err := svc.ReconcileOne(context.Background(), order)
	require.NoError(t, err)
	assert.False(t, moved, "资金还在路上，没推进任何状态")

	got, err := svc.QueryOrder(order.ID)
	require.NoError(t, err)
	assert.Equal(t, model.PayOrderStatusCreated, got.Status, "资金待定的单子绝不能关，否则钱到了入不了账")
	assert.Equal(t, 0, notif.count(EventOrderPaid), "钱还没到，不得入账")

	// 延审通过：后续对账查到 Success，必须能正常入账——证明上面「不关单」保住了这条路。
	ch.queryRes = &channel.TradeStatus{ChannelTradeNo: "ORDER1", State: channel.TradeSuccess, Amount: d(100), Currency: "USD"}
	moved, err = svc.ReconcileOne(context.Background(), order)
	require.NoError(t, err)
	assert.True(t, moved, "延审通过后入账，是真的推进")

	got, err = svc.QueryOrder(order.ID)
	require.NoError(t, err)
	assert.Equal(t, model.PayOrderStatusPaid, got.Status, "延审通过后必须能入账")
	assert.Equal(t, 1, notif.count(EventOrderPaid))
}

// TestReconcileStale_StaleJunkDoesNotStarveScan 是审查员点出的那条必修的端到端断言：
// 对账扫描有 LIMIT，一旦超龄废单堆过阈值，每轮就只捞得到这批最老的死单，真正需要补偿的
// 「已授权没跳回来」的单子永远排不进来——防丢钱的补偿会静默失效（PayPal 不会为从未发生的
// capture 发 webhook，对账是唯一补救路径）。
//
// 这里造 limit 满额的超龄废单把扫描名额占死，再放一张「已授权」的真单在后面，验证：
// 第一轮先把废单清空（它们拿到终态、退出 CREATED 集合），第二轮名额腾出来，真单被扫到并入账。
func TestReconcileStale_StaleJunkDoesNotStarveScan(t *testing.T) {
	ch := &fakeChannel{code: "pp", curr: []string{"USD"}, payURL: "u", createTradeNo: "ORDER1"}
	svc, notif := newSvc(t, ch)

	const limit = 3 // 用小 limit 复现"名额被占满"，等价于线上的 100

	// 先堆 limit 张超龄废单（用户开了收银台就跑，查单永远 Pending）。
	junk := make([]*model.PayOrder, 0, limit)
	for i := 0; i < limit; i++ {
		// 废单比真单更老，扫描按 created_at 升序会先捞到它们，把名额占满。
		junk = append(junk, staleOrder(t, svc, fmt.Sprintf("junk-%d", i), staleOrderTTL+time.Duration(limit-i)*time.Hour))
	}

	// 再来一张真单（更新）：用户已 approve 但没跳回来，capture 从未发生——只能靠对账补。
	real := staleOrder(t, svc, "real", time.Hour)

	// 查单结果按单号分流：废单 Pending，真单 Authorized。
	ch.queryByTradeNo = map[int64]*channel.TradeStatus{}
	for _, o := range junk {
		ch.queryByTradeNo[o.ID] = &channel.TradeStatus{ChannelTradeNo: "ORDER1", State: channel.TradePending}
	}
	ch.queryByTradeNo[real.ID] = &channel.TradeStatus{ChannelTradeNo: "ORDER1",
		State: channel.TradeAuthorized, Amount: d(100), Currency: "USD"}
	ch.redirectRes = &channel.NotifyResult{ChannelTradeNo: "ORDER1",
		State: channel.TradeSuccess, Amount: d(100), Currency: "USD"}

	before := time.Now().Add(-3 * time.Minute)

	// 第一轮：名额被 limit 张废单占满，真单排不进来。废单拿到终态，退出 CREATED 集合。
	_, failed, err := svc.ReconcileStale(context.Background(), before, limit)
	require.NoError(t, err)
	require.Zero(t, failed, "废单查单不该失败，只是不推进")
	got, err := svc.QueryOrder(real.ID)
	require.NoError(t, err)
	require.Equal(t, model.PayOrderStatusCreated, got.Status, "本轮真单还轮不到，仍应是 CREATED")

	// 第二轮：废单已关闭腾出名额，真单必须被扫到并补 capture 入账。
	// 这一步在修复前永远不会发生——废单不关，就把名额永久占死。
	_, failed, err = svc.ReconcileStale(context.Background(), before, limit)
	require.NoError(t, err)
	require.Zero(t, failed)

	got, err = svc.QueryOrder(real.ID)
	require.NoError(t, err)
	assert.Equal(t, model.PayOrderStatusPaid, got.Status, "废单清空后，真单必须被扫到并补偿入账")
	assert.Equal(t, 1, notif.count(EventOrderPaid))
}

// TestReconcileStale_FailuresAreNeverSilent 锁死"对账失败不得静默"。对账是防丢钱的最后
// 一道防线，凭证配错、第三方长时间不可用会让每一张单都查失败——若只是闷头 continue，整条
// 补偿链路瘫痪而日志和监控里一片安静，等发现时钱已经漏了几周。失败必须计数并逐条发告警事件。
func TestReconcileStale_FailuresAreNeverSilent(t *testing.T) {
	ch := &fakeChannel{code: "pp", curr: []string{"USD"}, payURL: "u", createTradeNo: "ORDER1"}
	svc, notif := newSvc(t, ch)
	o1 := staleOrder(t, svc, "o1", time.Hour)
	o2 := staleOrder(t, svc, "o2", time.Hour)

	// 模拟凭证配错 / PayPal 不可用：每张单查单都失败。
	ch.queryErr = errors.New("paypal: invalid client credentials")

	advanced, failed, err := svc.ReconcileStale(context.Background(), time.Now().Add(-3*time.Minute), 10)
	require.NoError(t, err, "单张失败不该让整轮扫描报错")
	assert.Zero(t, advanced)
	assert.Equal(t, 2, failed, "两张单都失败，必须如实计数上报，不能吞")

	// 告警必须是「每轮一条汇总」而不是「每单一条」：凭证配错 / 通道不可用时每张单都会失败，
	// 逐单发会在 ReconcileScanLimit(1000) 的规模上产生告警风暴淹没信号；更糟的是 Publish
	// 同步等 ack，NATS 不可用时每条告警都卡一次超时，对账循环会被自己的告警拖死。
	require.Equal(t, 1, notif.count(EventReconcileFailed), "每轮只发一条汇总告警，不得逐单发")
	sum, ok := notif.lastEvent().(ReconcileSummaryEvent)
	require.True(t, ok, "对账告警应是汇总事件体")
	assert.Equal(t, 2, sum.Failed)
	assert.True(t, sum.AllFailed, "全军覆没要标出来——那通常是凭证配错或通道整体不可用")
	assert.NotEmpty(t, sum.Samples, "要带上失败原因样本，否则没法定位")

	// 两张单都还在，等下轮重试——失败不推进状态，但也不能悄无声息。
	for _, o := range []*model.PayOrder{o1, o2} {
		got, err := svc.QueryOrder(o.ID)
		require.NoError(t, err)
		assert.Equal(t, model.PayOrderStatusCreated, got.Status)
	}
}

// TestFinalizePaid_PublishFailureIsNotSwallowed 锁死支付链路里后果最严重的一处静默失败。
//
// EventOrderPaid 发布失败 = 订单已经 MarkPaid（钱确实收了），事件却没送出去，网关的充值单
// 永远收不到结果、用户余额永不增加；而对账只扫 CREATED 状态的单，这张已 PAID 的单再也不会
// 被扫到——永久丢账，且零告警。触发它只需要 NATS 抖一下（重启 / stream 未就绪 / ack 超时）。
//
// 注意「把 error 上抛」这条路是死的：webhook 里上抛会让第三方重推，但重推时 MarkPaid 的 CAS
// 返回 advanced=false，就不会再走到 publish，事件照样丢。所以这里能做的是绝不吞错（记日志，
// 日志落本地文件，恰是 NATS 挂掉时唯一还能出声的通道），根治靠业务侧的反查兜底腿。
//
// 本测试保证的是：发布失败时 publish 会走到错误分支（而不是把 err 丢掉），且订单状态与入账
// 语义不受影响——钱该收还是收了，不能因为发不出事件就把订单退回去。
func TestFinalizePaid_PublishFailureIsNotSwallowed(t *testing.T) {
	ch := &fakeChannel{code: "pp", curr: []string{"USD"}, payURL: "u", createTradeNo: "ORDER1"}
	svc, notif := newSvc(t, ch)
	r, err := svc.CreateOrder(context.Background(), CreateOrderRequest{
		BizType: "b", BizOrderID: "o", Channel: "pp", Amount: d(100), Currency: "USD"})
	require.NoError(t, err)

	// NATS 挂了：所有事件发布都失败。
	notif.publishErr = errors.New("nats: no responders available for request")

	ch.notifyRes = &channel.NotifyResult{PayOrderID: r.PayOrderID, ChannelTradeNo: "ORDER1",
		State: channel.TradeSuccess, Amount: d(100), Currency: "USD"}

	// 入账本身必须照常完成——钱确实收了，不能因为事件发不出去就不认账。
	require.NoError(t, svc.HandleNotify(context.Background(), "pp", nil, []byte("{}")))
	got, err := svc.QueryOrder(r.PayOrderID)
	require.NoError(t, err)
	assert.Equal(t, model.PayOrderStatusPaid, got.Status, "钱收了就该入账，事件发不出去是另一回事")

	// 而且确实尝试发过 EventOrderPaid（失败了，但没被吞——publish 会记错误日志）。
	assert.Equal(t, 1, notif.count(EventOrderPaid), "必须尝试发布入账事件")
}

// TestReconcileStale_AdvancedCountsOnlyRealProgress 锁死"日志不得谎报成绩"。
//
// advanced 曾经统计的是「ReconcileOne 没报错的单数」，把还没付、资金在路上这些什么都没做的
// no-op 全算了进去。于是 cmd/pay 会打出"advanced N stale orders"——运维看到这行以为补偿在
// 正常干活，实际上一单都没推进。这跟静默失败是同一类事：系统坏了，日志却在报喜。
func TestReconcileStale_AdvancedCountsOnlyRealProgress(t *testing.T) {
	ch := &fakeChannel{code: "pp", curr: []string{"USD"}, payURL: "u", createTradeNo: "ORDER1"}
	svc, _ := newSvc(t, ch)

	junk := staleOrder(t, svc, "junk", time.Hour)           // 未超龄的废单：不关，no-op
	settling := staleOrder(t, svc, "settling", 2*time.Hour) // 资金待定：原地等，no-op

	ch.queryByTradeNo = map[int64]*channel.TradeStatus{
		junk.ID:     {ChannelTradeNo: "ORDER1", State: channel.TradePending},
		settling.ID: {ChannelTradeNo: "ORDER1", State: channel.TradeSettling, Amount: d(100), Currency: "USD"},
	}

	advanced, failed, err := svc.ReconcileStale(context.Background(), time.Now().Add(-3*time.Minute), 10)
	require.NoError(t, err)
	assert.Zero(t, failed, "no-op 不是失败")
	assert.Zero(t, advanced, "一单都没推进，就不能报成推进了 2 单——否则对账停摆时日志还在报喜")
}

// TestReconcileOne_FailedIsFinalized 锁死"被拒的单必须落终态"。
//
// 第三方明确拒付（PayPal 延审被拒 / 扣款失败）时，对账必须当场把订单打成 FAILED，不能指望
// PAYMENT.CAPTURE.DENIED 那条 webhook——它一旦丢了，这单就永远卡在 CREATED。而且它躲得开
// 废单收尸：TTL 关单只在 TradePending 分支里，被拒单归的是 TradeFailed，永远走不到那儿。
// 结果就是一个永不退出扫描集合的永久占位符，每轮白查一次第三方、占一个 LIMIT 名额——正是
// 第 8 项那个"永久驻留 → 占满名额 → 饿死"的洞，只是入口换成了被拒单。
func TestReconcileOne_FailedIsFinalized(t *testing.T) {
	ch := &fakeChannel{code: "pp", curr: []string{"USD"}, payURL: "u", createTradeNo: "ORDER1"}
	svc, _ := newSvc(t, ch)
	order := staleOrder(t, svc, "denied", time.Hour) // 未超龄——收尸机制救不了它

	ch.queryRes = &channel.TradeStatus{ChannelTradeNo: "ORDER1", State: channel.TradeFailed}
	moved, err := svc.ReconcileOne(context.Background(), order)
	require.NoError(t, err)
	assert.True(t, moved, "落终态是真的推进了状态")

	got, err := svc.QueryOrder(order.ID)
	require.NoError(t, err)
	assert.Equal(t, model.PayOrderStatusFailed, got.Status,
		"第三方明确拒付就必须落终态，否则 DENIED webhook 一丢，这单永远占着扫描名额")
}

// TestReturnURLAllowed 覆盖"防开放重定向"：只有同域（notifyURLBase 主机）或相对路径
// 的 return_url 才允许 302 跳回，外域一律拒绝，防本端点被拿去钓鱼。
func TestReturnURLAllowed(t *testing.T) {
	svc, _ := newSvc(t, &fakeChannel{code: "pp", curr: []string{"USD"}}) // notifyURLBase=https://pay.test
	cases := []struct {
		url  string
		want bool
	}{
		{"", false},
		{"https://pay.test/topup", true},
		{"https://pay.test", true},
		{"http://pay.test/x", true}, // 同主机不同 scheme：主机命中即放行
		{"/relative/path", true},    // 相对路径同源
		{"https://evil.com/phish", false},
		{"https://pay.test.evil.com/x", false}, // 子域伪装：主机不匹配
		{"//evil.com/x", false},                // 协议相对：主机=evil.com
		// 反斜杠绕过：url.Parse 判为相对路径，浏览器却归一成 //evil.com 跳外域，必须拒。
		{"/\\evil.com", false},    // 归一→//evil.com，主机=evil.com
		{"\\/evil.com", false},    // 归一→//evil.com
		{"\\\\evil.com", false},   // 归一→//evil.com
		{"/\\/\\evil.com", false}, // 归一→////evil.com，空主机路径引用，被 //-前缀检查挡下
		{"////evil.com", false},   // 空主机的网络路径引用，浏览器折叠斜杠后跳外域
		{" //evil.com", false},    // 前导空格：浏览器 trim 后成协议相对地址
		// userinfo 伪装：@ 前是白名单域也没用，真正的主机是 @ 后面的 evil.com。
		{"https://pay.test@evil.com/x", false},
		// 控制字符：url.Parse 直接判非法，拒。
		{"/\t/evil.com", false},
		{"/\n/evil.com", false},
		// 伪协议：主机为空但 scheme 非空，走白名单分支，拒。
		{"javascript:alert(1)", false},
		{"data:text/html,x", false},
		// 主机大小写不敏感（RFC 3986）：合法跳回地址不得误拒，否则用户付完钱跳不回完成页。
		{"https://PAY.TEST/topup", true},
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, svc.ReturnURLAllowed(c.url), "url=%q", c.url)
	}
}

func TestCreateOrder_CurrencyUnsupported(t *testing.T) {
	svc, _ := newSvc(t, &fakeChannel{code: "cn", curr: []string{"CNY"}})
	_, err := svc.CreateOrder(context.Background(), CreateOrderRequest{
		BizType: "b", BizOrderID: "o", Channel: "cn", Amount: d(1), Currency: "USD"})
	assert.ErrorIs(t, err, ErrCurrencyUnsupported)
}

func TestCreateOrder_ChannelNotFound(t *testing.T) {
	svc, _ := newSvc(t, &fakeChannel{code: "cn", curr: []string{"CNY"}})
	_, err := svc.CreateOrder(context.Background(), CreateOrderRequest{
		BizType: "b", BizOrderID: "o", Channel: "x", Amount: d(1), Currency: "CNY"})
	assert.ErrorIs(t, err, ErrChannelNotFound)
}

func TestHandleNotify_SuccessAndIdempotent(t *testing.T) {
	ch := &fakeChannel{code: "cn", curr: []string{"CNY"}, payURL: "u"}
	svc, notif := newSvc(t, ch)
	r, _ := svc.CreateOrder(context.Background(), CreateOrderRequest{
		BizType: "recharge", BizOrderID: "R1", Channel: "cn", Amount: d(100), Currency: "CNY"})

	ch.notifyRes = &channel.NotifyResult{
		PayOrderID: r.PayOrderID, ChannelTradeNo: "T1",
		State: channel.TradeSuccess, Amount: d(100), Currency: "CNY"}

	require.NoError(t, svc.HandleNotify(context.Background(), "cn", nil, []byte("{}")))
	o, _ := svc.QueryOrder(r.PayOrderID)
	assert.Equal(t, model.PayOrderStatusPaid, o.Status)
	assert.Equal(t, 1, notif.count(EventOrderPaid))

	// 重复通知：不重复推进、不重复发事件
	require.NoError(t, svc.HandleNotify(context.Background(), "cn", nil, []byte("{}")))
	assert.Equal(t, 1, notif.count(EventOrderPaid))
}

func TestHandleNotify_AmountMismatchRejected(t *testing.T) {
	ch := &fakeChannel{code: "cn", curr: []string{"CNY"}, payURL: "u"}
	svc, _ := newSvc(t, ch)
	r, _ := svc.CreateOrder(context.Background(), CreateOrderRequest{
		BizType: "b", BizOrderID: "o", Channel: "cn", Amount: d(100), Currency: "CNY"})

	ch.notifyRes = &channel.NotifyResult{
		PayOrderID: r.PayOrderID, ChannelTradeNo: "T",
		State: channel.TradeSuccess, Amount: d(99), Currency: "CNY"} // 金额不符

	assert.ErrorIs(t, svc.HandleNotify(context.Background(), "cn", nil, []byte("{}")), ErrNotifyMismatch)
	o, _ := svc.QueryOrder(r.PayOrderID)
	assert.Equal(t, model.PayOrderStatusCreated, o.Status) // 未入账
}

func TestRefund_PartialFullExceedIdempotent(t *testing.T) {
	ch := &fakeChannel{code: "cn", curr: []string{"CNY"}, payURL: "u",
		refundState: channel.RefundSuccess, refundNo: "RF"}
	svc, notif := newSvc(t, ch)
	r, _ := svc.CreateOrder(context.Background(), CreateOrderRequest{
		BizType: "b", BizOrderID: "o", Channel: "cn", Amount: d(100), Currency: "CNY"})
	ch.notifyRes = &channel.NotifyResult{PayOrderID: r.PayOrderID, ChannelTradeNo: "T",
		State: channel.TradeSuccess, Amount: d(100), Currency: "CNY"}
	require.NoError(t, svc.HandleNotify(context.Background(), "cn", nil, []byte("{}")))

	// 部分退 60
	rf1, err := svc.CreateRefund(context.Background(), RefundRequest{PayOrderID: r.PayOrderID, BizRefundID: "RF1", Amount: d(60)})
	require.NoError(t, err)
	assert.Equal(t, model.PayRefundStatusRefunded, rf1.Status)
	o, _ := svc.QueryOrder(r.PayOrderID)
	assert.Equal(t, model.PayOrderStatusPartialRefunded, o.Status)

	// 再退 60 超额（已退 60 + 60 > 100）
	_, err = svc.CreateRefund(context.Background(), RefundRequest{PayOrderID: r.PayOrderID, BizRefundID: "RF2", Amount: d(60)})
	assert.ErrorIs(t, err, ErrRefundExceed)

	// 退剩余 40 → 全退
	rf3, err := svc.CreateRefund(context.Background(), RefundRequest{PayOrderID: r.PayOrderID, BizRefundID: "RF3", Amount: d(40)})
	require.NoError(t, err)
	assert.Equal(t, model.PayRefundStatusRefunded, rf3.Status)
	o, _ = svc.QueryOrder(r.PayOrderID)
	assert.Equal(t, model.PayOrderStatusRefunded, o.Status)

	// 幂等：RF1 再次提交返回原单，即便订单已全退
	dup, err := svc.CreateRefund(context.Background(), RefundRequest{PayOrderID: r.PayOrderID, BizRefundID: "RF1", Amount: d(60)})
	require.NoError(t, err)
	assert.Equal(t, rf1.ID, dup.ID)

	assert.GreaterOrEqual(t, notif.count(EventRefundSucceeded), 2)
}

// TestHandleRefundNotify_IdempotentAndStateGuarded 覆盖异步退款通知路径（审查问题 1）：
// 重复的成功通知不重复发事件；已终态的退款单不被乱序的失败通知打回，支付单不被翻转。
func TestHandleRefundNotify_IdempotentAndStateGuarded(t *testing.T) {
	// refundState=Processing：CreateRefund 只把退款单挂 REFUNDING，等异步通知定终态。
	ch := &fakeChannel{code: "cn", curr: []string{"CNY"}, payURL: "u",
		refundState: channel.RefundProcessing, refundNo: "RF"}
	svc, notif := newSvc(t, ch)
	r, _ := svc.CreateOrder(context.Background(), CreateOrderRequest{
		BizType: "b", BizOrderID: "o", Channel: "cn", Amount: d(100), Currency: "CNY"})
	ch.notifyRes = &channel.NotifyResult{PayOrderID: r.PayOrderID, ChannelTradeNo: "T",
		State: channel.TradeSuccess, Amount: d(100), Currency: "CNY"}
	require.NoError(t, svc.HandleNotify(context.Background(), "cn", nil, []byte("{}")))

	// 全额异步退款：先受理（REFUNDING），再等通知
	rf, err := svc.CreateRefund(context.Background(), RefundRequest{PayOrderID: r.PayOrderID, BizRefundID: "RF1", Amount: d(100)})
	require.NoError(t, err)
	assert.Equal(t, model.PayRefundStatusRefunding, rf.Status)

	// ① 退款成功通知 → 退款单 REFUNDED，支付单 REFUNDED，发 1 次事件
	ch.refundNotifyRes = &channel.RefundNotifyResult{PayOrderID: r.PayOrderID, RefundID: rf.ID,
		State: channel.RefundSuccess, ChannelRefundNo: "RF", Amount: d(100), Currency: "CNY"}
	require.NoError(t, svc.HandleRefundNotify(context.Background(), "cn", nil, []byte("{}")))
	rfDone, _ := svc.QueryRefund(rf.ID)
	assert.Equal(t, model.PayRefundStatusRefunded, rfDone.Status)
	o, _ := svc.QueryOrder(r.PayOrderID)
	assert.Equal(t, model.PayOrderStatusRefunded, o.Status)
	assert.Equal(t, 1, notif.count(EventRefundSucceeded))

	// ② 重复成功通知 → 不重复发事件
	require.NoError(t, svc.HandleRefundNotify(context.Background(), "cn", nil, []byte("{}")))
	assert.Equal(t, 1, notif.count(EventRefundSucceeded))

	// ③ 乱序失败通知到达已 REFUNDED 的单 → 被终态守卫挡下：退款单仍 REFUNDED，
	//    支付单不被翻回 PAID，也不发失败事件
	ch.refundNotifyRes = &channel.RefundNotifyResult{PayOrderID: r.PayOrderID, RefundID: rf.ID,
		State: channel.RefundFailed, Amount: d(100), Currency: "CNY"}
	require.NoError(t, svc.HandleRefundNotify(context.Background(), "cn", nil, []byte("{}")))
	rfAfter, _ := svc.QueryRefund(rf.ID)
	assert.Equal(t, model.PayRefundStatusRefunded, rfAfter.Status, "已退款的单不该被乱序失败通知打回")
	oAfter, _ := svc.QueryOrder(r.PayOrderID)
	assert.Equal(t, model.PayOrderStatusRefunded, oAfter.Status, "支付单不该从 REFUNDED 翻回 PAID")
	assert.Equal(t, 0, notif.count(EventRefundFailed))
}

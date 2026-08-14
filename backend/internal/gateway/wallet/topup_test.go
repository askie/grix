package wallet_test

import (
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/askie/grix/backend/internal/gateway/wallet"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
)

func TestMain(m *testing.M) {
	// 充值单/钱包 ID 由 snowflake 生成，测试前初始化一次。
	_ = snowflake.Init(1)
	os.Exit(m.Run())
}

func dec(s string) decimal.Decimal {
	d, _ := decimal.NewFromString(s)
	return d
}

// TestSettleTopup_CreditsUSDAndIdempotent 验证：用户付人民币，按汇率换算成 USD 入账；
// 重复入账幂等，不重复加钱。
func TestSettleTopup_CreditsUSDAndIdempotent(t *testing.T) {
	db := testutil.NewTestDB()
	defer db.Close()
	svc := wallet.New(db.DB)

	// 汇率快照：1 CNY = 0.14 USD
	require.NoError(t, db.DB.Create(&model.GatewayFxRate{
		ID: snowflake.GenID(), FromCurrency: "CNY", ToCurrency: "USD",
		Rate: dec("0.14"), EffectiveFrom: time.Now(), Source: "test",
	}).Error)

	// 用户发起充值：付 100 元 CNY，走支付宝
	order, err := svc.CreateTopupOrder(90001, "CNY", dec("100"), "alipay")
	require.NoError(t, err)
	require.Equal(t, model.GatewayTopupOrderStatusPending, order.Status)
	require.NoError(t, svc.BindPayOrder(order.ID, 555001)) // 支付系统回填支付单号

	// 支付成功 → 入账（payOrderID 为权威支付单号）
	done, err := svc.SettleTopup(order.ID, 555001)
	require.NoError(t, err)
	assert.Equal(t, model.GatewayTopupOrderStatusPaid, done.Status)

	w, err := svc.GetWallet(order.WalletID)
	require.NoError(t, err)
	assert.True(t, w.Balance.Equal(dec("14")), "100 CNY * 0.14 = 14 USD, got %s", w.Balance)

	// 重投：幂等，不重复加钱
	_, err = svc.SettleTopup(order.ID, 555001)
	require.NoError(t, err)
	w2, _ := svc.GetWallet(order.WalletID)
	assert.True(t, w2.Balance.Equal(dec("14")), "重复入账后余额应不变，got %s", w2.Balance)

	// 只应有一条充值流水，引用 = 支付单号
	_, total, err := svc.ListTopupRecords(order.WalletID, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total, "重复事件应只产生一条充值流水")
}

// TestCreateTopupOrder_UnboundPayOrderStaysNull 复现并锁死线上事故：
// 未绑定支付单的充值单必须落 NULL。曾把 pay_order_id 定义成非指针 int64，
// 未绑定时写的是 0；0 会挤进 uk_gateway_topup_orders_pay 这个「只约束非空值」的
// 部分唯一索引，于是第一张未绑定单占死索引槽位后，之后所有人的充值下单全部撞唯一键 500。
// 这里按生产建表语句造出同样的部分唯一索引，字段一旦退回非指针即会失败。
func TestCreateTopupOrder_UnboundPayOrderStaysNull(t *testing.T) {
	db := testutil.NewTestDB()
	defer db.Close()
	svc := wallet.New(db.DB)

	require.NoError(t, db.DB.Exec(
		`CREATE UNIQUE INDEX uk_gateway_topup_orders_pay ON gateway_topup_orders(pay_order_id)
		 WHERE pay_order_id IS NOT NULL`).Error, "建部分唯一索引（与生产 096 一致）")

	// 连开三张未绑定支付单的充值单：三张都该成功，且支付单号都是 NULL。
	for i := 0; i < 3; i++ {
		order, err := svc.CreateTopupOrder(int64(90010+i), "CNY", dec("1"), "alipay")
		require.NoErrorf(t, err, "第 %d 张未绑定充值单不该撞唯一键", i+1)
		assert.Nil(t, order.PayOrderID, "未绑定支付单时必须是 NULL，不能是 0")
		assert.Nil(t, order.TopupRecordID, "未入账时必须是 NULL，不能是 0")
	}

	var unbound int64
	require.NoError(t, db.DB.Model(&model.GatewayTopupOrder{}).
		Where("pay_order_id IS NULL").Count(&unbound).Error)
	assert.Equal(t, int64(3), unbound, "三张单都应以 NULL 落库")

	// 自证上面的索引真的在挡：改用旧写法（0 当空值）连插两条，第二条必撞唯一键。
	// 若这里没报错，说明约束是摆设，上面的断言也就失去意义。
	ins := `INSERT INTO gateway_topup_orders
		(id, wallet_id, owner_id, source_currency, source_amount, channel, status, pay_order_id, created_at, updated_at)
		VALUES (?, ?, ?, 'CNY', 1, 'alipay', 'PENDING', 0, ?, ?)`
	now := time.Now()
	w, err := svc.CreateWallet(90019)
	require.NoError(t, err)
	require.NoError(t, db.DB.Exec(ins, snowflake.GenID(), w.ID, 90019, now, now).Error,
		"第一条 pay_order_id=0 能插入（它占住了索引槽位）")
	assert.Error(t, db.DB.Exec(ins, snowflake.GenID(), w.ID, 90019, now, now).Error,
		"第二条 pay_order_id=0 必须撞唯一键——这正是线上事故的机理")
}

// TestBindPayOrder_RejectsZero 支付单号为 0 是脏数据，绝不能落库：
// 它会污染部分唯一索引，卡死后续所有人的充值下单。
func TestBindPayOrder_RejectsZero(t *testing.T) {
	db := testutil.NewTestDB()
	defer db.Close()
	svc := wallet.New(db.DB)

	order, err := svc.CreateTopupOrder(90020, "CNY", dec("1"), "alipay")
	require.NoError(t, err)

	assert.Error(t, svc.BindPayOrder(order.ID, 0), "支付单号 0 应被拒绝")

	got, err := svc.GetTopupOrder(order.ID)
	require.NoError(t, err)
	assert.Nil(t, got.PayOrderID, "被拒后支付单号仍应是 NULL")
}

// TestSettleTopup_RejectsCrossOrder 验证串单防护：用一个非绑定的支付单号结算，应被拒。
func TestSettleTopup_RejectsCrossOrder(t *testing.T) {
	db := testutil.NewTestDB()
	defer db.Close()
	svc := wallet.New(db.DB)

	require.NoError(t, db.DB.Create(&model.GatewayFxRate{
		ID: snowflake.GenID(), FromCurrency: "USD", ToCurrency: "USD",
		Rate: dec("1"), EffectiveFrom: time.Now(), Source: "test",
	}).Error)
	order, err := svc.CreateTopupOrder(90002, "USD", dec("20"), "paypal")
	require.NoError(t, err)
	require.NoError(t, svc.BindPayOrder(order.ID, 555001))

	// 用别的支付单号结算 → 拒绝，余额不动
	_, err = svc.SettleTopup(order.ID, 999999)
	require.Error(t, err, "非绑定支付单结算应被拒")
	w, _ := svc.GetWallet(order.WalletID)
	assert.True(t, w.Balance.IsZero(), "串单被拒后余额应为 0，got %s", w.Balance)
}

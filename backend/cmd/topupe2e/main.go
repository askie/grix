// cmd/topupe2e 是用户充值闭环 + 安全/可靠性加固的本地端到端验证程序（非生产）。
// 驱动与 api 服务完全相同的代码路径。前提：cmd/pay 已用 mock 通道跑起来。
//
// 覆盖三个场景：
//
//	① 正常充值：下单 → 支付成功 → 消费者回查支付系统权威状态后入账（美元）
//	② 伪造事件（审查 C2）：未真正支付就往 NATS 塞成功事件 → 回查权威=未付 → 拒绝入账
//	③ 对账补偿（审查 C1）：支付成功但事件丢失 → 对账任务扫回 → 补入账
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/gateway/wallet"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
)

func main() {
	logger.Init()
	config.Load("config.yaml")
	if err := snowflake.Init(config.C.Snowflake.MachineID); err != nil {
		log.Fatalf("snowflake: %v", err)
	}
	store.InitPostgres(config.C.Postgres)
	store.InitNATS(config.C.NATS)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.StartGatewayTopupConsumer(ctx)
	time.Sleep(1 * time.Second)

	w := wallet.New(store.DB)
	userID := int64(88990011)
	wal, _ := w.CreateWallet(userID)
	fmt.Printf("起始余额: %s USD\n\n", wal.Balance)

	// ① 正常充值 100 CNY → 期望 +14 USD
	fmt.Println("【场景① · 正常充值】")
	o1 := createTopup(userID, "100")
	notifyPaid(*o1.PayOrderID)
	waitBalance(w, wal.ID, "14")
	fmt.Printf("  ✓ 余额=%s USD (100×0.14)\n\n", balance(w, wal.ID))

	// ② 伪造事件：未支付就塞成功事件，期望被拒、余额不变
	fmt.Println("【场景② · 伪造事件（不回查会凭空加钱）】")
	o2 := createTopup(userID, "200") // 只下单，不真正支付
	publishForgedPaidEvent(o2)       // 往 NATS 塞一条伪造的支付成功事件
	time.Sleep(2 * time.Second)
	b2 := balance(w, wal.ID)
	if b2 == "14" {
		fmt.Printf("  ✓ 余额仍=%s USD，伪造事件被拒（回查支付系统=未付）\n\n", b2)
	} else {
		fmt.Printf("  ✗ 余额=%s USD，伪造事件竟入账了！\n\n", b2)
	}

	// ③ 对账补偿：支付真成功但事件丢失，靠对账扫回
	fmt.Println("【场景③ · 对账补偿（事件丢失）】")
	o3 := createTopup(userID, "300")
	// 模拟"支付成功但事件丢了"：直接把支付单在库里置 PAID，不走 notify（不发事件）
	markPayOrderPaidInDB(*o3.PayOrderID)
	fmt.Println("  已模拟支付成功但事件丢失，触发对账…")
	recovered := service.ReconcileTopupsOnce(ctx, time.Now()) // before=now：对账所有 PENDING
	fmt.Printf("  对账入账 %d 单\n", recovered)
	waitBalance(w, wal.ID, "56") // 14 + 300×0.14=42 → 56
	fmt.Printf("  ✓ 余额=%s USD (14 + 300×0.14=42)\n", balance(w, wal.ID))
}

func createTopup(userID int64, amount string) *model.GatewayTopupOrder {
	resp, ec := service.GatewayCreateTopup(userID, service.GatewayCreateTopupReq{
		Amount: amount, Currency: "CNY", Channel: "mock",
	})
	if ec != nil {
		log.Fatalf("create topup %s: %+v", amount, ec)
	}
	var id int64
	fmt.Sscan(resp.TopupOrderID, &id)
	o, err := wallet.New(store.DB).GetTopupOrder(id)
	if err != nil {
		log.Fatalf("get topup order: %v", err)
	}
	if o.PayOrderID == nil {
		log.Fatalf("topup order %d: 下单成功却没绑定支付单", o.ID)
	}
	fmt.Printf("  充值单=%d 支付单=%d 金额=%s CNY\n", o.ID, *o.PayOrderID, amount)
	return o
}

func notifyPaid(payOrderID int64) {
	body := fmt.Sprintf(`{"pay_order_id":"%d","channel_trade_no":"mocktrade-%d","state":"SUCCESS","amount":"%s","currency":"CNY"}`,
		payOrderID, payOrderID, payAmount(payOrderID))
	resp, err := http.Post("http://127.0.0.1:27185/v1/pay/notify/mock", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		log.Fatalf("notify: %v", err)
	}
	_ = resp.Body.Close()
}

// payAmount 从库里取支付单金额（notify 金额需与原单一致才被支付系统接受）。
func payAmount(payOrderID int64) string {
	var amt string
	store.DB.Raw("SELECT amount::text FROM pay_order WHERE id = ?", payOrderID).Scan(&amt)
	return amt
}

// publishForgedPaidEvent 伪造一条支付成功事件塞进 NATS（模拟攻击者/错误重放）。
func publishForgedPaidEvent(o *model.GatewayTopupOrder) {
	ev := map[string]any{
		"pay_order_id": fmt.Sprintf("%d", *o.PayOrderID),
		"biz_type":     wallet.TopupBizType,
		"biz_order_id": fmt.Sprintf("%d", o.ID),
		"amount":       o.SourceAmount.String(),
		"currency":     o.SourceCurrency,
		"status":       "PAID",
	}
	data, _ := json.Marshal(ev)
	if _, err := store.JS.Publish("pay.order.paid", data); err != nil {
		log.Fatalf("publish forged: %v", err)
	}
}

// markPayOrderPaidInDB 直接把支付单置 PAID，不发事件（模拟"付成功了但事件丢失"）。
func markPayOrderPaidInDB(payOrderID int64) {
	store.DB.Exec("UPDATE pay_order SET status='PAID', channel_trade_no=?, paid_at=now() WHERE id = ?",
		fmt.Sprintf("mocktrade-%d", payOrderID), payOrderID)
}

func balance(w *wallet.Service, walletID int64) string {
	wal, _ := w.GetWallet(walletID)
	return wal.Balance.String()
}

func waitBalance(w *wallet.Service, walletID int64, want string) {
	for i := 0; i < 20; i++ {
		if balance(w, walletID) == want {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

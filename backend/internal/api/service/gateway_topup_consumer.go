package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/gateway/payclient"
	"github.com/askie/grix/backend/internal/gateway/wallet"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// 支付成功事件订阅参数。用持久队列消费：多个 api 副本共享一个 durable + queue，
// 每条事件只由一个副本处理，避免重复入账。
const (
	payOrderPaidSubject = "pay.order.paid"
	gatewayTopupDurable = "gateway-topup"
	gatewayTopupQueue   = "gateway-topup"
	gatewayTopupAckWait = 30 * time.Second

	// 对账补偿：周期扫描「支付已成功但入账没跟上」的充值单（审查 C1/R2）。
	topupReconcileInterval = 60 * time.Second
	topupReconcileMinAge   = 2 * time.Minute // 只对账创建满 2 分钟仍 PENDING 的单，避开与实时消费竞争
	topupReconcileBatch    = 100
)

// payOrderPaidEvent 是支付成功事件里入账需要的字段（与支付系统 pay.OrderEvent 的 json 契约对齐）。
// 注意：金额不取自事件，一律以回查支付系统的权威值为准（审查 C2）。
type payOrderPaidEvent struct {
	PayOrderID string `json:"pay_order_id"`
	BizType    string `json:"biz_type"`
	BizOrderID string `json:"biz_order_id"`
}

// settleTopupFromPay 是充值入账的唯一权威路径：回查支付系统确认该支付单确为 PAID
// 且金额/币种与充值单一致，才入账。实时消费与对账补偿共用它，杜绝盲信事件凭空加钱
// （审查 C2）与金额不符入账（审查 R4）。
func settleTopupFromPay(ctx context.Context, order *model.GatewayTopupOrder, payOrderID int64) error {
	pc := payclient.New(config.C.Pay.InternalBaseURL)
	st, err := pc.QueryOrder(ctx, payOrderID)
	if err != nil {
		return fmt.Errorf("query pay order %d: %w", payOrderID, err)
	}
	if st.Status != model.PayOrderStatusPaid {
		return fmt.Errorf("pay order %d not paid (status=%s)", payOrderID, st.Status)
	}
	if !st.Amount.Equal(order.SourceAmount) || st.Currency != order.SourceCurrency {
		return fmt.Errorf("pay order %d amount/currency mismatch: pay=%s %s topup=%s %s",
			payOrderID, st.Amount, st.Currency, order.SourceAmount, order.SourceCurrency)
	}
	_, err = gatewayWalletService().SettleTopup(order.ID, payOrderID)
	return err
}

// StartGatewayTopupConsumer 订阅支付成功事件，把 biz_type=wallet_topup 的支付入账到用户钱包。
// 入账前一律回查支付系统权威状态。订阅失败只告警不阻断 api 启动。
func StartGatewayTopupConsumer(ctx context.Context) {
	if store.JS == nil {
		logger.L.Warn("gateway topup consumer: jetstream unavailable, not starting")
		return
	}
	sub, err := store.JS.QueueSubscribe(
		payOrderPaidSubject,
		gatewayTopupQueue,
		func(msg *nats.Msg) { handleTopupPaidMsg(ctx, msg) },
		nats.ManualAck(),
		nats.Durable(gatewayTopupDurable),
		nats.DeliverAll(),
		nats.AckWait(gatewayTopupAckWait),
		nats.MaxDeliver(5),
	)
	if err != nil {
		logger.L.Errorf("gateway topup consumer: subscribe failed: %v", err)
		return
	}
	logger.L.Infof("gateway topup consumer subscribed subject=%s group=%s", payOrderPaidSubject, gatewayTopupQueue)
	go func() {
		<-ctx.Done()
		_ = sub.Drain()
	}()
}

func handleTopupPaidMsg(ctx context.Context, msg *nats.Msg) {
	var ev payOrderPaidEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		logger.L.Errorf("gateway topup consumer: bad event payload: %v", err)
		_ = msg.Ack() // 坏消息，别重投
		return
	}
	if ev.BizType != wallet.TopupBizType {
		_ = msg.Ack() // 非充值业务，忽略
		return
	}
	topupID, err := strconv.ParseInt(ev.BizOrderID, 10, 64)
	if err != nil {
		logger.L.Errorf("gateway topup consumer: bad biz_order_id %q: %v", ev.BizOrderID, err)
		_ = msg.Ack()
		return
	}
	payOrderID, err := strconv.ParseInt(ev.PayOrderID, 10, 64)
	if err != nil {
		logger.L.Errorf("gateway topup consumer: bad pay_order_id %q: %v", ev.PayOrderID, err)
		_ = msg.Ack()
		return
	}
	order, err := gatewayWalletService().GetTopupOrder(topupID)
	if err != nil {
		logger.L.Errorf("gateway topup consumer: topup order %d not found: %v", topupID, err)
		_ = msg.Ack() // 找不到对应充值单，坏事件，丢弃
		return
	}
	if order.Status != model.GatewayTopupOrderStatusPending {
		_ = msg.Ack() // 已入账，幂等
		return
	}
	if err := settleTopupFromPay(ctx, order, payOrderID); err != nil {
		// 入账失败：不 Ack，等 JetStream 重投；最终仍失败由对账补偿兜底。
		logger.L.Warnf("gateway topup consumer: settle order=%d pay=%d err=%v", topupID, payOrderID, err)
		return
	}
	_ = msg.Ack()
}

// StartGatewayTopupReconciler 周期对账：把「支付已成功但入账没跟上」的充值单捞回入账，
// 兜住消费者宕机 / 事件丢失 / 重投耗尽等情况（审查 C1/R2）。走与实时消费相同的权威校验路径。
func StartGatewayTopupReconciler(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(topupReconcileInterval)
		defer ticker.Stop()
		logger.L.Infof("gateway topup reconciler started interval=%s", topupReconcileInterval)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ReconcileTopupsOnce(ctx, time.Now().Add(-topupReconcileMinAge))
			}
		}
	}()
}

// ReconcileTopupsOnce 跑一轮充值对账：把创建早于 before、支付已成功但仍 PENDING 的
// 充值单捞回入账。返回本轮实际入账的单数。定时器与联调可复用。
func ReconcileTopupsOnce(ctx context.Context, before time.Time) int {
	orders, err := gatewayWalletService().ListPendingTopupOrders(before, topupReconcileBatch)
	if err != nil {
		logger.L.Warnf("gateway topup reconciler: list pending failed: %v", err)
		return 0
	}
	recovered := 0
	for i := range orders {
		o := orders[i]
		if o.PayOrderID == nil {
			continue // 未绑定支付单的单据不参与对账（查询已过滤，这里兜底防解引用）
		}
		payOrderID := *o.PayOrderID
		if err := settleTopupFromPay(ctx, &o, payOrderID); err != nil {
			// 尚未付成功的单会在此返回 not-paid，属正常，不告警。
			logger.L.Debugf("gateway topup reconciler: order=%d skip: %v", o.ID, err)
			continue
		}
		recovered++
		logger.L.Infof("gateway topup reconciler: recovered order=%d pay=%d", o.ID, payOrderID)
	}
	return recovered
}

package wallet

import (
	"fmt"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
)

// TopupBizType 是充值单在支付系统里的业务类型标识，支付成功事件按它路由回充值入账。
const TopupBizType = "wallet_topup"

// EffectiveRate 返回源币种兑记账货币(USD)的有效汇率。USD 恒为 1；
// 其它币种取 gateway_fx_rates 里已生效(effective_from<=now)的最新一条，
// 未来生效的汇率不会被提前选中。汇率自动更新（外部 API）后续接入，这里只读当前快照。
func (s *Service) EffectiveRate(sourceCurrency string) (decimal.Decimal, error) {
	if sourceCurrency == "USD" {
		return decimal.NewFromInt(1), nil
	}
	var fx model.GatewayFxRate
	if err := s.db.Where("from_currency = ? AND to_currency = ? AND effective_from <= ?",
		sourceCurrency, "USD", time.Now()).
		Order("effective_from DESC").First(&fx).Error; err != nil {
		return decimal.Zero, fmt.Errorf("gateway: no fx rate for %s: %w", sourceCurrency, err)
	}
	return fx.Rate, nil
}

// CreateTopupOrder 为用户建一张待支付充值单(PENDING)，并确保其钱包存在。
// 返回后由调用方去支付系统下单，再用 BindPayOrder 回填支付单号。
func (s *Service) CreateTopupOrder(ownerID int64, sourceCurrency string, sourceAmount decimal.Decimal, channel string) (*model.GatewayTopupOrder, error) {
	w, err := s.CreateWallet(ownerID)
	if err != nil {
		return nil, err
	}
	order := &model.GatewayTopupOrder{
		ID:             snowflake.GenID(),
		WalletID:       w.ID,
		OwnerID:        ownerID,
		SourceCurrency: sourceCurrency,
		SourceAmount:   sourceAmount,
		Channel:        channel,
		Status:         model.GatewayTopupOrderStatusPending,
	}
	if err := s.db.Create(order).Error; err != nil {
		return nil, err
	}
	return order, nil
}

// BindPayOrder 把支付系统返回的支付单号回填到充值单上。
// 拒绝非正的支付单号：0 会落进部分唯一索引并挤占所有未绑定单的槽位。
func (s *Service) BindPayOrder(topupOrderID, payOrderID int64) error {
	if payOrderID <= 0 {
		return fmt.Errorf("gateway topup: order %d invalid pay order id %d", topupOrderID, payOrderID)
	}
	return s.db.Model(&model.GatewayTopupOrder{}).
		Where("id = ?", topupOrderID).
		Update("pay_order_id", payOrderID).Error
}

// MarkTopupFailed 把仍 PENDING 的充值单置 FAILED，用于下单/绑定支付单失败时收尾，
// 避免留下未绑定支付单、对账永远捞不到的孤儿 PENDING 单（审查问题 2）。
func (s *Service) MarkTopupFailed(topupOrderID int64) error {
	return s.db.Model(&model.GatewayTopupOrder{}).
		Where("id = ? AND status = ?", topupOrderID, model.GatewayTopupOrderStatusPending).
		Update("status", model.GatewayTopupOrderStatusFailed).Error
}

// GetTopupOrder 按 id 查充值单。
func (s *Service) GetTopupOrder(id int64) (*model.GatewayTopupOrder, error) {
	var o model.GatewayTopupOrder
	if err := s.db.First(&o, id).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// SettleTopup 在支付成功后给充值单入账：幂等推进 PENDING → PAID，同一事务内
// 按汇率把充值金额换算成 USD 记入钱包并写充值流水。已非 PENDING 直接跳过（幂等），
// 事件重投不会重复加钱。汇率在事务外先取，避免占用事务连接。
//
// payOrderID 是权威支付单号（由调用方回查支付系统确认已付后传入），用作充值流水的
// 幂等引用；若充值单已绑定到不同的支付单，拒绝入账（防串单）。绑定失败留下的空
// pay_order_id 会在此回填，保证引用不丢（审查 R3）。调用方负责入账前的权威校验（审查 C2/R4）。
func (s *Service) SettleTopup(topupOrderID, payOrderID int64) (*model.GatewayTopupOrder, error) {
	pre, err := s.GetTopupOrder(topupOrderID)
	if err != nil {
		return nil, err
	}
	if pre.Status != model.GatewayTopupOrderStatusPending {
		return pre, nil // 幂等快路径：已处理过
	}
	if pre.PayOrderID != nil && *pre.PayOrderID != payOrderID {
		return nil, fmt.Errorf("gateway topup: order %d bound pay %d != %d (串单)", topupOrderID, *pre.PayOrderID, payOrderID)
	}
	rate, err := s.EffectiveRate(pre.SourceCurrency)
	if err != nil {
		return nil, err
	}

	var order model.GatewayTopupOrder
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, topupOrderID).Error; err != nil {
			return err
		}
		if order.Status != model.GatewayTopupOrderStatusPending {
			return nil // 并发下二次确认，已被别的事务入账
		}
		if order.PayOrderID != nil && *order.PayOrderID != payOrderID {
			return fmt.Errorf("gateway topup: order %d bound pay %d != %d (串单)", topupOrderID, *order.PayOrderID, payOrderID)
		}
		reference := strconv.FormatInt(payOrderID, 10)
		_, record, err := creditTx(tx, order.WalletID, order.SourceCurrency, order.SourceAmount, rate, order.Channel, reference)
		if err != nil {
			return err
		}
		return tx.Model(&order).Updates(map[string]any{
			"status":          model.GatewayTopupOrderStatusPaid,
			"topup_record_id": record.ID,
			"pay_order_id":    payOrderID,
			"paid_at":         time.Now(),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// ListPendingTopupOrders 返回创建时间早于 before、仍 PENDING 且已绑定支付单的充值单，
// 供对账补偿扫描（支付已成功但入账没跟上的单，审查 C1/R2）。
func (s *Service) ListPendingTopupOrders(before time.Time, limit int) ([]model.GatewayTopupOrder, error) {
	var out []model.GatewayTopupOrder
	err := s.db.Where("status = ? AND pay_order_id IS NOT NULL AND created_at < ?",
		model.GatewayTopupOrderStatusPending, before).
		Limit(limit).Find(&out).Error
	return out, err
}

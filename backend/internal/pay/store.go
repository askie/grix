package pay

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/askie/grix/backend/internal/model"
)

// Store 封装支付单 / 退款单 / 通知流水的持久化。
type Store struct {
	db *gorm.DB
}

// NewStore 创建 Store。
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// CreateOrder 落库一张新支付单。若 biz_type + biz_order_id 已存在，
// 返回已有单且 existed=true（下单幂等，第一层幂等）。
func (s *Store) CreateOrder(o *model.PayOrder) (order *model.PayOrder, existed bool, err error) {
	var cur model.PayOrder
	e := s.db.Where("biz_type = ? AND biz_order_id = ?", o.BizType, o.BizOrderID).First(&cur).Error
	if e == nil {
		return &cur, true, nil
	}
	if !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, false, e
	}
	if err := s.db.Create(o).Error; err != nil {
		return nil, false, err
	}
	return o, false, nil
}

// GetOrder 按内部单号取支付单。
func (s *Store) GetOrder(id int64) (*model.PayOrder, error) {
	var o model.PayOrder
	if err := s.db.First(&o, id).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// GetOrderByBiz 按业务单号取支付单。
func (s *Store) GetOrderByBiz(bizType, bizOrderID string) (*model.PayOrder, error) {
	var o model.PayOrder
	if err := s.db.Where("biz_type = ? AND biz_order_id = ?", bizType, bizOrderID).First(&o).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// MarkPaid 幂等推进 CREATED → PAID。仅当当前状态为 CREATED 时生效。
// advanced=true 表示本次实际推进了状态（用于决定是否发成功事件，防重复）。
func (s *Store) MarkPaid(id int64, channelTradeNo string, paidAt time.Time) (advanced bool, err error) {
	res := s.db.Model(&model.PayOrder{}).
		Where("id = ? AND status = ?", id, model.PayOrderStatusCreated).
		Updates(map[string]any{
			"status":           model.PayOrderStatusPaid,
			"channel_trade_no": channelTradeNo,
			"paid_at":          paidAt,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// MarkClosed 幂等推进 CREATED → CLOSED（超时 / 主动关单）。
func (s *Store) MarkClosed(id int64) (advanced bool, err error) {
	res := s.db.Model(&model.PayOrder{}).
		Where("id = ? AND status = ?", id, model.PayOrderStatusCreated).
		Update("status", model.PayOrderStatusClosed)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// BindChannelTradeNo 把下单成功时第三方立即返回的交易号（如 PayPal 的 order id）
// 落库。与状态机推进无关——部分通道（PayPal）下单即拿到交易号，且用户跳转确认 /
// 主动查单 / 退款都要靠它反查第三方，不能等到 MarkPaid 才写入；支付宝等下单不返回
// 交易号的通道，channelTradeNo 传空则跳过。
func (s *Store) BindChannelTradeNo(id int64, channelTradeNo string) error {
	if channelTradeNo == "" {
		return nil
	}
	return s.db.Model(&model.PayOrder{}).Where("id = ?", id).Update("channel_trade_no", channelTradeNo).Error
}

// MarkFailed 幂等推进 CREATED → FAILED（下单失败）。
func (s *Store) MarkFailed(id int64) (advanced bool, err error) {
	res := s.db.Model(&model.PayOrder{}).
		Where("id = ? AND status = ?", id, model.PayOrderStatusCreated).
		Update("status", model.PayOrderStatusFailed)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// UpdateOrderRefundStatus 在退款推进后更新支付单的退款状态
// （REFUNDING / PARTIAL_REFUNDED / REFUNDED）。
func (s *Store) UpdateOrderRefundStatus(id int64, status string) error {
	return s.db.Model(&model.PayOrder{}).Where("id = ?", id).Update("status", status).Error
}

// NotifySeen 记录一条入站通知；channel + channel_trade_no 唯一。
// id 由调用方（Service）用其 newID 生成，与订单/退款单一致，便于测试注入。
// firstTime=true 表示首次见到（应处理），false 表示重复通知（第二层幂等，直接 ACK）。
func (s *Store) NotifySeen(id int64, channel, channelTradeNo, raw string) (firstTime bool, err error) {
	log := &model.PayNotifyLog{
		ID:             id,
		Channel:        channel,
		ChannelTradeNo: channelTradeNo,
		Raw:            raw,
	}
	res := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(log)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// CreateRefund 落库一张退款单。若 biz_refund_id 已存在，返回已有单且
// existed=true（退款幂等，第三层幂等）。
func (s *Store) CreateRefund(r *model.PayRefund) (refund *model.PayRefund, existed bool, err error) {
	var cur model.PayRefund
	e := s.db.Where("biz_refund_id = ?", r.BizRefundID).First(&cur).Error
	if e == nil {
		return &cur, true, nil
	}
	if !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, false, e
	}
	if err := s.db.Create(r).Error; err != nil {
		return nil, false, err
	}
	return r, false, nil
}

// GetRefund 按内部退款单号取退款单。
func (s *Store) GetRefund(id int64) (*model.PayRefund, error) {
	var r model.PayRefund
	if err := s.db.First(&r, id).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

// GetRefundByBiz 按业务退款单号取退款单；不存在返回 (nil, nil)（不视为错误，供幂等判断）。
func (s *Store) GetRefundByBiz(bizRefundID string) (*model.PayRefund, error) {
	var r model.PayRefund
	err := s.db.Where("biz_refund_id = ?", bizRefundID).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdateRefundStatus 幂等推进退款单 REFUNDING → 终态（REFUNDED/FAILED），可回填第三方退款单号。
// 仅当当前为 REFUNDING 时生效（终态守卫）：重复 / 乱序的退款通知不会把已终态的退款单再流转。
// advanced=true 表示本次实际推进了状态（供调用方决定是否更新支付单 + 发事件，防重复）。
func (s *Store) UpdateRefundStatus(id int64, status, channelRefundNo string) (advanced bool, err error) {
	updates := map[string]any{"status": status}
	if channelRefundNo != "" {
		updates["channel_refund_no"] = channelRefundNo
	}
	res := s.db.Model(&model.PayRefund{}).
		Where("id = ? AND status = ?", id, model.PayRefundStatusRefunding).
		Updates(updates)
	return res.RowsAffected > 0, res.Error
}

// SumRefunded 返回某支付单已退（含退款中）的金额合计，用于部分退款的可退余额校验。
func (s *Store) SumRefunded(payOrderID int64) (decimal.Decimal, error) {
	var sum decimal.Decimal
	err := s.db.Model(&model.PayRefund{}).
		Where("pay_order_id = ? AND status IN ?", payOrderID,
			[]string{model.PayRefundStatusRefunding, model.PayRefundStatusRefunded}).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&sum).Error
	return sum, err
}

// PrepareRefund 在单事务内完成「锁支付单行 → 状态校验 → 可退余额校验 → 落退款单 →
// 更新支付单为退款中」。行锁（SELECT ... FOR UPDATE）把同一支付单的并发退款串行化：
// 锁内重算的已退总额是准确的，堵住「检查可退余额」与「落退款单」之间的 TOCTOU 超退窗口
// （不同 biz_refund_id 的并发请求不再能同时通过余额检查）。
// 幂等语义不变：锁内复查 biz_refund_id，已存在则返回已有单且 existed=true。
// 行锁仅对 Postgres 生效；SQLite（单测）本身以单连接串行执行，不支持该子句。
func (s *Store) PrepareRefund(payOrderID int64, amount decimal.Decimal, refund *model.PayRefund) (order *model.PayOrder, sumBefore decimal.Decimal, saved *model.PayRefund, existed bool, err error) {
	err = s.db.Transaction(func(tx *gorm.DB) error {
		q := tx
		if tx.Dialector.Name() == "postgres" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var o model.PayOrder
		e := q.First(&o, payOrderID).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return ErrOrderNotFound
		}
		if e != nil {
			return e
		}
		var dup model.PayRefund
		de := tx.Where("biz_refund_id = ?", refund.BizRefundID).First(&dup).Error
		if de == nil {
			order, saved, existed = &o, &dup, true
			return nil
		}
		if !errors.Is(de, gorm.ErrRecordNotFound) {
			return de
		}
		if o.Status != model.PayOrderStatusPaid && o.Status != model.PayOrderStatusPartialRefunded {
			return ErrOrderNotRefundable
		}
		var sum decimal.Decimal
		if err := tx.Model(&model.PayRefund{}).
			Where("pay_order_id = ? AND status IN ?", payOrderID,
				[]string{model.PayRefundStatusRefunding, model.PayRefundStatusRefunded}).
			Select("COALESCE(SUM(amount), 0)").
			Scan(&sum).Error; err != nil {
			return err
		}
		if sum.Add(amount).GreaterThan(o.Amount) {
			return ErrRefundExceed
		}
		if err := tx.Create(refund).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.PayOrder{}).Where("id = ?", o.ID).
			Update("status", model.PayOrderStatusRefunding).Error; err != nil {
			return err
		}
		order, sumBefore, saved = &o, sum, refund
		return nil
	})
	return order, sumBefore, saved, existed, err
}

// ListStaleCreated 返回创建时间早于 before、仍为 CREATED 的支付单，供对账补偿扫描。
//
// 按 created_at 升序取：这一批里最老的单子既最接近第三方的订单作废期限（补偿最紧迫），
// 也最可能已经超龄、该被 ReconcileOne 当废单关掉——让它们优先出队，CREATED 集合才能
// 持续轮转、腾出 limit 名额给后面的单子。缺了这个排序，返回顺序由数据库物理布局决定，
// 扫描可能反复捞到同一批单子，把名额长期占死。
func (s *Store) ListStaleCreated(before time.Time, limit int) ([]model.PayOrder, error) {
	var out []model.PayOrder
	err := s.db.Where("status = ? AND created_at < ?", model.PayOrderStatusCreated, before).
		Order("created_at ASC").Limit(limit).Find(&out).Error
	return out, err
}

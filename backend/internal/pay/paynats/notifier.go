// Package paynats 用 NATS JetStream 实现支付核心的 outbound 事件通道。
//
// 支付/退款结果事件（pay.order.* / pay.refund.*）经此发布，业务侧
// （如 gateway 充值消费者）订阅并据此加余额。它把 pay 核心与具体消息
// 中间件解耦：pay 只认 pay.Notifier 接口，生产用本实现，测试用 fake。
package paynats

import (
	"encoding/json"
	"errors"

	"github.com/askie/grix/backend/internal/store"
)

// Notifier 把事件 JSON 序列化后发布到 JetStream 对应主题。
type Notifier struct{}

// New 构造 JetStream 通知器。前提：调用方已执行 store.InitNATS。
func New() Notifier { return Notifier{} }

// Publish 将事件发布到 subject（即事件主题，如 "pay.order.paid"）。
// 同步等待 JetStream ack，失败上抛由调用方决定是否重试 / 记录。
func (Notifier) Publish(subject string, event any) error {
	if store.JS == nil {
		return errors.New("paynats: jetstream not initialized")
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = store.JS.Publish(subject, data)
	return err
}

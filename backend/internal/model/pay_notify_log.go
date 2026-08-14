package model

import "time"

// PayNotifyLog 是第三方入站通知的流水与去重记录。
// channel + channel_trade_no 唯一，用于通知幂等（重复通知直接命中已处理）。
type PayNotifyLog struct {
	ID             int64     `gorm:"primaryKey" json:"id,string"`
	Channel        string    `gorm:"size:32;not null;uniqueIndex:uk_pay_notify" json:"channel"`
	ChannelTradeNo string    `gorm:"size:128;not null;uniqueIndex:uk_pay_notify" json:"channel_trade_no"`
	Raw            string    `gorm:"type:text" json:"raw"`
	CreatedAt      time.Time `json:"created_at"`
}

func (PayNotifyLog) TableName() string { return "pay_notify_log" }

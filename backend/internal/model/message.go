package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Message struct {
	MsgID           int64          `gorm:"primaryKey" json:"msg_id,string"`
	SessionID       string         `gorm:"primaryKey;size:50" json:"session_id"`
	ThreadID        string         `gorm:"size:255" json:"thread_id,omitempty"`
	SenderID        int64          `gorm:"not null" json:"sender_id,string"`
	SenderType      int16          `gorm:"default:1" json:"sender_type"` // 1:人类 2:智能体 3:系统
	MsgType         int16          `gorm:"default:1" json:"msg_type"`    // 1:文本 2:图片 3:系统通知 4:AI流式 5:干预
	Content         string         `gorm:"type:text" json:"content"`
	Extra           datatypes.JSON `gorm:"type:jsonb" json:"extra"`
	QuotedMessageID int64          `gorm:"default:0" json:"quoted_message_id,string,omitempty"` // 引用回复目标消息 ID
	VisibleTo       datatypes.JSON `gorm:"type:jsonb;default:null" json:"visible_to,omitempty"` // 群聊消息仅指定人可见，NULL=全员可见
	IsDeleted       bool           `gorm:"default:false" json:"is_deleted"`
	IsRevoked       bool           `gorm:"default:false" json:"is_revoked"`
	CreatedAt       time.Time      `json:"created_at"`
}

func (Message) TableName() string { return "messages" }

// SendMsgIdempotencyReceipt is the permanent atomic sink receipt for Agent API
// send_msg. It lives in a separate, initially empty table so enabling durable
// Gemini terminal cards never builds a blocking unique index over the
// partitioned messages hot table or changes human-client retention semantics.
type SendMsgIdempotencyReceipt struct {
	SessionID    string    `gorm:"primaryKey;size:50" json:"session_id"`
	SenderID     int64     `gorm:"primaryKey" json:"sender_id,string"`
	ClientMsgKey string    `gorm:"primaryKey;size:64" json:"client_msg_key"`
	MsgID        int64     `gorm:"not null" json:"msg_id,string"`
	InboxSeq     int64     `gorm:"not null;default:0" json:"inbox_seq,string"`
	CreatedAt    time.Time `gorm:"not null" json:"created_at"`
}

func (SendMsgIdempotencyReceipt) TableName() string {
	return "send_msg_idempotency_receipts"
}

// BeforeCreate normalizes message timestamps to UTC so ordering and
// per-user history cutoffs are compared against one canonical timeline.
func (m *Message) BeforeCreate(tx *gorm.DB) error {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	} else {
		m.CreatedAt = m.CreatedAt.UTC()
	}
	return nil
}

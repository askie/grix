package model

import "time"

// FriendRequest 好友请求
type FriendRequest struct {
	ID         int64     `gorm:"primaryKey" json:"id,string"`
	FromUserID int64     `gorm:"index;not null" json:"from_user_id,string"`
	ToUserID   int64     `gorm:"index;not null" json:"to_user_id,string"`
	Status     int8      `gorm:"default:0" json:"status"` // 0=pending, 1=accepted, 2=rejected
	Message    string    `gorm:"size:200" json:"message"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (FriendRequest) TableName() string { return "friend_requests" }

// Friend 好友关系（双向存储：A加B会写入两条记录）
type Friend struct {
	ID         int64      `gorm:"primaryKey" json:"id,string"`
	UserID     int64      `gorm:"uniqueIndex:idx_user_friend;not null" json:"user_id,string"`
	FriendID   int64      `gorm:"uniqueIndex:idx_user_friend;not null" json:"friend_id,string"`
	RemarkName string     `gorm:"size:50;not null;default:''" json:"remark_name"`
	IsPinned   bool       `gorm:"default:false" json:"is_pinned"`
	PinnedAt   *time.Time `json:"pinned_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (Friend) TableName() string { return "friends" }

// UserBlock 用户拉黑关系（单向存储：A 拉黑 B 只写一条 A->B）
type UserBlock struct {
	ID            int64     `gorm:"primaryKey" json:"id,string"`
	UserID        int64     `gorm:"uniqueIndex:idx_user_blocks_user_blocked;not null" json:"user_id,string"`
	BlockedUserID int64     `gorm:"uniqueIndex:idx_user_blocks_user_blocked;not null" json:"blocked_user_id,string"`
	CreatedAt     time.Time `json:"created_at"`
}

func (UserBlock) TableName() string { return "user_blocks" }

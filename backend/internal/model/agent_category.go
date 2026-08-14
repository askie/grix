package model

import "time"

// AgentCategory 用户自行创建管理的智能体多级分类目录
type AgentCategory struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id,string"`
	OwnerID   int64     `gorm:"index;not null" json:"owner_id,string"`
	ParentID  int64     `gorm:"index;not null;default:0" json:"parent_id,string"`
	Name      string    `gorm:"size:100;not null" json:"name"`
	SortOrder int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (AgentCategory) TableName() string { return "agent_categories" }

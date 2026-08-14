package model

import "time"

const (
	AgentShareStatusActive  int16 = 1 // 有效
	AgentShareStatusRevoked int16 = 2 // 已撤销
)

// AgentShare 记录一个 agent 被共享给某个账户。
// connector 用「主人 api_key + shared_owner_id」为 SharedTo 建立独立 WS 连接，
// 后端校验主人 key + 本记录存在，把该连接身份认定为 SharedTo，Server 数据/手机端按其身份隔离。
type AgentShare struct {
	ID        int64      `gorm:"primaryKey" json:"id,string"`
	AgentID   int64      `gorm:"index;not null" json:"agent_id,string"`
	OwnerID   int64      `gorm:"not null" json:"owner_id,string"` // agent 主人，冗余便于查询/校验
	SharedTo  int64      `gorm:"index;not null" json:"shared_to,string"`
	Status    int16      `gorm:"not null;default:1" json:"status"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (AgentShare) TableName() string { return "agent_shares" }

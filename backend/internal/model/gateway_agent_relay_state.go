package model

import "time"

// GatewayAgentRelayState 是"Grix中转"按 Agent 的开关状态（migration 111）。
//
// desired（enabled/relay_model/revision）是服务端持有的唯一权威期望态：用户对某个 Agent
// "要开还是要关、用什么模型"的记录，任何端读到的都是同一份；revision 是乐观锁。
// applied/applied_at 是 connector 最近一次有效回执的实际态（M2 WS 协议写回），
// 只用于展示，不反向覆盖 desired。
type GatewayAgentRelayState struct {
	AgentID int64 `gorm:"primaryKey" json:"agent_id,string"`
	// WalletID 冗余落库（wallet 通过 owner 间接关联 agent），供按钱包批量取回。
	WalletID int64 `gorm:"not null;index" json:"wallet_id,string"`
	// Enabled 是期望开关。
	Enabled bool `gorm:"not null;default:false" json:"enabled"`
	// RelayModel 是唯一权威的期望模型，空 = 走网关模型映射/兜底。
	RelayModel string `gorm:"size:128;not null;default:''" json:"relay_model"`
	// Revision 是乐观锁版本号，每次写 desired +1。
	Revision int64 `gorm:"not null;default:1" json:"revision"`
	// Applied 是 connector 最近一次有效回执的实际态。
	Applied bool `gorm:"not null;default:false" json:"applied"`
	// AppliedAt 是最近一次有效回执时间；nil 表示从未收到回执。
	AppliedAt *time.Time `json:"applied_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (GatewayAgentRelayState) TableName() string { return "gateway_agent_relay_state" }

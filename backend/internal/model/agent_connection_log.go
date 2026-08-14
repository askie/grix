package model

import "time"

// AgentConnectionLog 记录 agent WS（/v1/agent-api）每次上线的来源信息。
// 每条成功握手一行；断开时回填 DisconnectedAt / DisconnectReason。
// GeoChanged 表示本次归属地与该 (agent, owner) 上一次成功连接不同（异地标记）。
type AgentConnectionLog struct {
	ID               int64      `gorm:"primaryKey" json:"id,string"`
	AgentID          int64      `gorm:"index:idx_agent_conn_logs_agent_time,priority:1;not null" json:"agent_id,string"`
	OwnerID          int64      `gorm:"not null;default:0" json:"owner_id,string"`
	IsPrimary        bool       `gorm:"not null;default:true" json:"is_primary"`
	ClientType       string     `gorm:"size:32;not null;default:''" json:"client_type"`
	ClientIP         string     `gorm:"size:45;index;not null;default:''" json:"client_ip"`
	IPLocation       string     `gorm:"size:128;not null;default:''" json:"ip_location"`
	GeoChanged       bool       `gorm:"not null;default:false" json:"geo_changed"`
	AllowlistMiss    bool       `gorm:"not null;default:false" json:"allowlist_miss"`
	NodeID           string     `gorm:"size:64;not null;default:''" json:"node_id"`
	ConnectedAt      time.Time  `gorm:"index:idx_agent_conn_logs_agent_time,priority:2,sort:desc;not null" json:"connected_at"`
	DisconnectedAt   *time.Time `json:"disconnected_at,omitempty"`
	DisconnectReason string     `gorm:"size:64;not null;default:''" json:"disconnect_reason"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (AgentConnectionLog) TableName() string { return "agent_connection_logs" }

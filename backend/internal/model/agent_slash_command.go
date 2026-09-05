package model

import "time"

// AgentSlashCommand 是主人为自己的 agent 自定义的一条斜杠命令。
// 内置命令由 internal/agentslashcmd 按 client_type 静态注册，不落库；
// 这张表只存自定义部分，工具栏快照下发时追加到内置命令之后。
// Name 含前导斜杠并统一小写，(AgentID, Name) 唯一。
type AgentSlashCommand struct {
	ID          int64     `gorm:"primaryKey" json:"id,string"`
	AgentID     int64     `gorm:"uniqueIndex:uq_agent_slash_commands_name,priority:1;not null" json:"agent_id,string"`
	OwnerID     int64     `gorm:"not null" json:"owner_id,string"`
	Name        string    `gorm:"size:64;uniqueIndex:uq_agent_slash_commands_name,priority:2;not null" json:"name"`
	Description string    `gorm:"size:200;not null;default:''" json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

func (AgentSlashCommand) TableName() string { return "agent_slash_commands" }

package model

import "time"

// UserSkill 是用户的自定义技能包（docs/architecture/38：自定义技能多机器同步）。
// 平台按 owner 维度存储；connector 拉取后落到机器级 grix/skills 目录，多机同步。
// 平台只存与分发，不解析 Content、不校验、不参与技能的调用与产出。
type UserSkill struct {
	ID      int64  `gorm:"primaryKey" json:"id,string"`
	OwnerID int64  `gorm:"not null;uniqueIndex:idx_user_skills_owner_name,priority:1" json:"owner_id,string"`
	Name    string `gorm:"size:100;not null;uniqueIndex:idx_user_skills_owner_name,priority:2" json:"name"`
	// Content 是技能包全文（SKILL.md），平台原样存取，不解析。
	Content string `gorm:"type:text;not null" json:"content,omitempty"`
	// Version 单调递增（每次更新 +1），connector 据此判断本机是否落后。
	Version int64 `gorm:"not null;default:1" json:"version,string"`
	// Digest 是 Content 的 sha256，connector 据此判断内容是否变化、做增量拉取。
	Digest    string    `gorm:"size:64;not null;default:''" json:"digest"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (UserSkill) TableName() string { return "user_skills" }

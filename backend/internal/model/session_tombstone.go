package model

import "time"

// SessionTombstone 记录某人类用户失去某会话可见性的时刻（退群 / 被踢 / 群解散）。
// 供 /sessions/sync 增量返回 deleted_session_ids：客户端离线期间错过的会话移除，
// 重新上线时可凭墓碑增量对账清除，无需每次拉全量 /sessions/list 做整表比对。
//
// 仅人类成员（member_type=1）写墓碑，因为会话列表是人类视角；agent 成员变动不入墓碑。
// 主键 (user_id, session_id) 保证同一用户对同一会话只保留最新一条：重新加入后再退出
// 会覆盖 deleted_at。重新加入但未再退出的场景由 sync 查询侧的 LEFT JOIN 现成员关系排除。
type SessionTombstone struct {
	UserID    int64     `gorm:"primaryKey;index:idx_session_tombstones_user_deleted,priority:1" json:"user_id,string"`
	SessionID string    `gorm:"primaryKey;size:64" json:"session_id"`
	DeletedAt time.Time `gorm:"index:idx_session_tombstones_user_deleted,priority:2,sort:desc" json:"deleted_at"`
}

func (SessionTombstone) TableName() string { return "session_tombstones" }

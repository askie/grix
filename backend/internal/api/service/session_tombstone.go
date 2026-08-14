package service

import (
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// recordSessionTombstones 为失去会话可见性的人类用户写墓碑，使其 /sessions/sync 能增量
// 发现该会话已被移除。仅接受人类 user_id，必须与删除 session_members 的操作共用同一 tx，
// 保证墓碑与成员移除原子一致。同一 (user_id, session_id) 重复写入会刷新 deleted_at。
func recordSessionTombstones(tx *gorm.DB, sessionID string, humanUserIDs []int64, deletedAt time.Time) error {
	if sessionID == "" || len(humanUserIDs) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(humanUserIDs))
	rows := make([]model.SessionTombstone, 0, len(humanUserIDs))
	for _, uid := range humanUserIDs {
		if uid <= 0 {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		rows = append(rows, model.SessionTombstone{
			UserID:    uid,
			SessionID: sessionID,
			DeletedAt: deletedAt,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "session_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"deleted_at"}),
	}).Create(&rows).Error
}

// loadDeletedSessionIDs 返回 since 之后该用户被移除的会话 id。通过 LEFT JOIN 现有成员关系
// 排除"墓碑存在但用户当前仍是成员"的会话（即退出后又重新加入），避免把仍可见的会话误清。
func loadDeletedSessionIDs(userID int64, since time.Time) ([]string, error) {
	if userID <= 0 {
		return []string{}, nil
	}
	var rows []struct {
		SessionID string `gorm:"column:session_id"`
	}
	if err := store.DB.Table("session_tombstones AS t").
		Select("t.session_id").
		Joins(
			"LEFT JOIN session_members m ON m.session_id = t.session_id AND m.member_id = ? AND m.member_type = 1",
			userID,
		).
		Where("t.user_id = ? AND t.deleted_at > ? AND m.session_id IS NULL", userID, since).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.SessionID != "" {
			ids = append(ids, row.SessionID)
		}
	}
	return ids, nil
}

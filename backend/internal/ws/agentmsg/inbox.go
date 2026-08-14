package agentmsg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/inboxseq"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func resolveHumanSessionViewingUsers(
	ctx context.Context,
	sessionID string,
	memberIDs []int64,
) map[int64]bool {
	result := map[int64]bool{}
	if store.RDB == nil || sessionID == "" || len(memberIDs) == 0 {
		return result
	}
	if ctx == nil {
		ctx = context.Background()
	}

	uniqMemberIDs := make([]int64, 0, len(memberIDs))
	seen := make(map[int64]struct{}, len(memberIDs))
	for _, memberID := range memberIDs {
		if memberID <= 0 {
			continue
		}
		if _, exists := seen[memberID]; exists {
			continue
		}
		seen[memberID] = struct{}{}
		uniqMemberIDs = append(uniqMemberIDs, memberID)
	}
	if len(uniqMemberIDs) == 0 {
		return result
	}

	pipe := store.RDB.Pipeline()
	existsCmds := make(map[int64]*redis.IntCmd, len(uniqMemberIDs))
	for _, memberID := range uniqMemberIDs {
		key := fmt.Sprintf(
			"im:activity:%s:%s:%d:%s",
			sessionID,
			protocol.SessionActivityActorTypeHuman,
			memberID,
			protocol.SessionActivityKindViewing,
		)
		existsCmds[memberID] = pipe.Exists(ctx, key)
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		logger.L.Warnf("agentmsg resolve viewing users pipeline failed session=%s: %v", sessionID, err)
	}

	for memberID, cmd := range existsCmds {
		exists, err := cmd.Result()
		if err != nil {
			continue
		}
		if exists > 0 {
			result[memberID] = true
		}
	}
	return result
}

// EnqueueStreamInbox writes a stream-finished message to each human member's inbox.
// When senderID belongs to a human member, that member gets an inbox row without unread increment.
// All other human members get unread +1.
// When visibleTo is non-empty, only sender + listed user IDs receive inbox rows.
func EnqueueStreamInbox(ctx context.Context, sessionID string, msgID, senderID int64, visibleTo []int64) {
	if sessionID == "" || msgID <= 0 || store.DB == nil || store.RDB == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var members []model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_type = 1", sessionID).Find(&members).Error; err != nil {
		logger.L.Warnf("agentmsg inbox query members error session=%s msg=%d: %v", sessionID, msgID, err)
		return
	}

	// Filter members by visibleTo when set.
	if len(visibleTo) > 0 {
		allowed := make(map[int64]struct{}, len(visibleTo)+1)
		allowed[senderID] = struct{}{}
		for _, id := range visibleTo {
			allowed[id] = struct{}{}
		}
		var filtered []model.SessionMember
		for _, m := range members {
			if _, ok := allowed[m.MemberID]; ok {
				filtered = append(filtered, m)
			}
		}
		members = filtered
	}

	memberIDs := make([]int64, 0, len(members))
	for _, m := range members {
		if m.MemberID <= 0 || m.MemberID == senderID {
			continue
		}
		memberIDs = append(memberIDs, m.MemberID)
	}
	viewingUsers := resolveHumanSessionViewingUsers(ctx, sessionID, memberIDs)
	now := time.Now().UTC()

	for _, m := range members {
		// Redis-level dedup first (fast path)
		dedupeKey := fmt.Sprintf("im:stream_inbox:dedup:%d:%d:%s", m.MemberID, msgID, sessionID)
		ok, err := store.RDB.SetNX(ctx, dedupeKey, 1, 24*time.Hour).Result()
		if err != nil || !ok {
			continue
		}

		// DB-level dedup: skip if inbox row already exists (covers Redis key expiry edge case).
		var exists int64
		if err := store.DB.Model(&model.UserInbox{}).
			Where("user_id = ? AND msg_id = ? AND session_id = ?", m.MemberID, msgID, sessionID).
			Count(&exists).Error; err != nil {
			store.RDB.Del(ctx, dedupeKey)
			logger.L.Warnf("agentmsg inbox exists query error user=%d msg=%d: %v", m.MemberID, msgID, err)
			continue
		}
		if exists > 0 {
			continue
		}

		isSender := m.MemberID == senderID
		isViewing := viewingUsers[m.MemberID]

		if err := store.DB.Transaction(func(tx *gorm.DB) error {
			inboxSeq, err := inboxseq.NextTx(ctx, tx, m.MemberID)
			if err != nil {
				return err
			}
			if err := tx.Create(&model.UserInbox{
				UserID:    m.MemberID,
				InboxSeq:  inboxSeq,
				MsgID:     msgID,
				SessionID: sessionID,
				EventKind: model.UserInboxEventKindMessage,
			}).Error; err != nil {
				return err
			}
			if isSender {
				return nil
			}
			if isViewing {
				if err := tx.Model(&model.SessionMember{}).
					Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, m.MemberID).
					Updates(map[string]interface{}{
						"last_active_at":   now,
						"unread_count":     0,
						"last_read_msg_id": gorm.Expr("CASE WHEN last_read_msg_id < ? THEN ? ELSE last_read_msg_id END", msgID, msgID),
					}).Error; err != nil {
					return err
				}
				return nil
			}
			if err := tx.Model(&model.SessionMember{}).
				Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, m.MemberID).
				Updates(map[string]interface{}{
					"last_active_at": now,
					"unread_count":   gorm.Expr("unread_count + 1"),
				}).Error; err != nil {
				return err
			}
			return nil
		}); err != nil {
			store.RDB.Del(ctx, dedupeKey)
			logger.L.Warnf("agentmsg inbox transaction error user=%d msg=%d: %v", m.MemberID, msgID, err)
			continue
		}
		if isSender {
			continue
		}
		if isViewing {
			if err := store.RDB.HDel(ctx, fmt.Sprintf("im:unread:%d", m.MemberID), sessionID).Err(); err != nil {
				logger.L.Warnf("agentmsg recipient redis unread clear error user=%d msg=%d: %v", m.MemberID, msgID, err)
			}
		} else if err := store.RDB.HIncrBy(ctx, fmt.Sprintf("im:unread:%d", m.MemberID), sessionID, 1).Err(); err != nil {
			logger.L.Warnf("agentmsg recipient redis unread error user=%d msg=%d: %v", m.MemberID, msgID, err)
		}
	}
}

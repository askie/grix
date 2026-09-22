package agentmsg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/inboxseq"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/syncstream"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FinalizeStreamMessage atomically turns a streaming placeholder into a
// durable message, updates session/unread projections, writes v1 inbox rows,
// and appends the v2 events. Redis is updated only after the transaction.
func FinalizeStreamMessage(ctx context.Context, sessionID string, msgID, senderID int64, visibleTo []int64, content string, messageUpdates map[string]any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if store.DB == nil || sessionID == "" || msgID <= 0 {
		return errors.New("invalid stream finalization")
	}
	var members []model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_type = 1", sessionID).Find(&members).Error; err != nil {
		return err
	}
	if len(visibleTo) > 0 {
		allowed := make(map[int64]struct{}, len(visibleTo)+1)
		allowed[senderID] = struct{}{}
		for _, id := range visibleTo {
			allowed[id] = struct{}{}
		}
		filtered := make([]model.SessionMember, 0, len(members))
		for _, member := range members {
			if _, ok := allowed[member.MemberID]; ok {
				filtered = append(filtered, member)
			}
		}
		members = filtered
	}
	memberIDs := make([]int64, 0, len(members))
	for _, member := range members {
		memberIDs = append(memberIDs, member.MemberID)
	}
	viewingUsers := resolveHumanSessionViewingUsers(ctx, sessionID, memberIDs)
	type unreadUpdate struct {
		userID  int64
		viewing bool
	}
	unreadUpdates := make([]unreadUpdate, 0, len(members))
	now := time.Now().UTC()

	err := store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lockedMessage model.Message
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedMessage, "msg_id = ? AND session_id = ?", msgID, sessionID).Error; err != nil {
			return err
		}
		var existing []model.UserInbox
		if len(memberIDs) > 0 {
			if err := tx.Select("user_id").Where("session_id = ? AND msg_id = ? AND user_id IN ?", sessionID, msgID, memberIDs).Find(&existing).Error; err != nil {
				return err
			}
		}
		existingUsers := make(map[int64]struct{}, len(existing))
		for _, row := range existing {
			existingUsers[row.UserID] = struct{}{}
		}
		pending := make([]model.SessionMember, 0, len(members))
		pendingIDs := make([]int64, 0, len(members))
		for _, member := range members {
			if _, exists := existingUsers[member.MemberID]; exists {
				continue
			}
			pending = append(pending, member)
			pendingIDs = append(pendingIDs, member.MemberID)
		}
		// A completed duplicate has no pending recipients and must remain
		// idempotent. An agent-only session can also have no recipients on its
		// first finalization; its streaming placeholder (msg_type=4) still needs
		// to be converted to the final message.
		if len(pending) == 0 && lockedMessage.MsgType != 4 {
			return nil
		}

		updates := make(map[string]any, len(messageUpdates)+1)
		for key, value := range messageUpdates {
			updates[key] = value
		}
		updates["state_version"] = gorm.Expr("state_version + 1")
		result := tx.Model(&model.Message{}).Where("msg_id = ? AND session_id = ?", msgID, sessionID).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		if len(pending) == 0 {
			return nil
		}

		sessionUpdates := map[string]any{"updated_at": now, "state_version": gorm.Expr("state_version + 1")}
		if len(visibleTo) == 0 {
			sessionUpdates["last_msg_id"] = msgID
			if !textutil.IsStandaloneCardMessage(content) {
				sessionUpdates["last_msg_summary"] = textutil.TruncateRunes(content, 60)
			}
		}
		if err := tx.Model(&model.Session{}).Where("session_id = ?", sessionID).Updates(sessionUpdates).Error; err != nil {
			return err
		}

		nextSeqByUser, err := inboxseq.AllocateNextBatchTx(ctx, tx, pendingIDs)
		if err != nil {
			return err
		}
		for _, member := range pending {
			if err := tx.Create(&model.UserInbox{UserID: member.MemberID, InboxSeq: nextSeqByUser[member.MemberID], MsgID: msgID, SessionID: sessionID, EventKind: model.UserInboxEventKindMessage, CreatedAt: now}).Error; err != nil {
				return err
			}
			if member.MemberID != senderID {
				viewing := viewingUsers[member.MemberID]
				memberUpdates := map[string]any{"last_active_at": now, "state_version": gorm.Expr("state_version + 1")}
				if viewing {
					memberUpdates["unread_count"] = 0
					memberUpdates["last_read_msg_id"] = gorm.Expr("CASE WHEN last_read_msg_id < ? THEN ? ELSE last_read_msg_id END", msgID, msgID)
				} else {
					memberUpdates["unread_count"] = gorm.Expr("unread_count + 1")
				}
				if err := tx.Model(&model.SessionMember{}).Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, member.MemberID).Updates(memberUpdates).Error; err != nil {
					return err
				}
				unreadUpdates = append(unreadUpdates, unreadUpdate{userID: member.MemberID, viewing: viewing})
			}
		}

		var msg model.Message
		if err := tx.First(&msg, "msg_id = ? AND session_id = ?", msgID, sessionID).Error; err != nil {
			return err
		}
		var session model.Session
		if err := tx.First(&session, "session_id = ?", sessionID).Error; err != nil {
			return err
		}
		events := make([]syncstream.Event, 0, len(pending)*3)
		var currentMembers []model.SessionMember
		if len(pendingIDs) > 0 {
			if err := tx.Where("session_id = ? AND member_id IN ? AND member_type = 1", sessionID, pendingIDs).Find(&currentMembers).Error; err != nil {
				return err
			}
			if len(currentMembers) != len(pendingIDs) {
				return fmt.Errorf("load updated session members: got %d want %d", len(currentMembers), len(pendingIDs))
			}
		}
		for _, currentMember := range currentMembers {
			events = append(events,
				syncstream.Event{UserID: currentMember.MemberID, Kind: "message.upsert", EntityType: "message", EntityID: fmt.Sprintf("%d", msgID), EntityVersion: msg.StateVersion, Payload: msg},
				syncstream.Event{UserID: currentMember.MemberID, Kind: "session.upsert", EntityType: "session", EntityID: sessionID, EntityVersion: session.StateVersion, Payload: session},
				syncstream.Event{UserID: currentMember.MemberID, Kind: "session.unread_set", EntityType: "session_member", EntityID: sessionID, EntityVersion: currentMember.StateVersion, Payload: map[string]any{"session_id": sessionID, "unread_count": currentMember.UnreadCount, "last_read_msg_id": currentMember.LastReadMsgID, "state_version": currentMember.StateVersion}},
			)
		}
		_, err = syncstream.AppendTx(tx, events)
		return err
	})
	if err != nil {
		return err
	}
	if store.RDB != nil {
		for _, update := range unreadUpdates {
			if update.viewing {
				_ = store.RDB.HDel(ctx, fmt.Sprintf("im:unread:%d", update.userID), sessionID).Err()
			} else {
				_ = store.RDB.HIncrBy(ctx, fmt.Sprintf("im:unread:%d", update.userID), sessionID, 1).Err()
			}
		}
	}
	return nil
}

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

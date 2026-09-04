package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/askie/grix/backend/internal/notification"
	"regexp"
	"strings"
	"time"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/inboxseq"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/pkg/userpref"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type agentNoticeDelivery struct {
	memberID              int64
	inboxSeq              int64
	shouldIncrementUnread bool
}

// EmitAgentDeliveryFailureMessage writes an agent delivery failure as a normal
// chat message so the conversation, summary, and unread badge share one path.
//
// 托管场景（scope=delegate）里 agent 是替主人回复对端的，"agent 掉线/超时"是主人
// 该处理的运维信息，对端既不认识这个 agent 也无从处理；此时提示只对主人可见
// （visible_to=[owner]，仅写主人 inbox/未读/推送，不改会话摘要），对端看到的会话
// 里不会出现一个陌生 agent 头像的气泡。直投场景看提示的就是主人，维持全员可见。
func EmitAgentDeliveryFailureMessage(
	hub HubInterface,
	ctx context.Context,
	sessionID string,
	ownerID int64,
	agentID int64,
	triggerMsgID int64,
	scope string,
	code string,
	reason string,
) {
	if agentID <= 0 {
		return
	}
	if hub == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	if store.DB == nil || store.RDB == nil {
		logger.L.Warnf("skip agent delivery notice: db/redis unavailable session=%s", sessionID)
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	scope = strings.TrimSpace(scope)
	code = strings.TrimSpace(code)
	ownerOnly := scope == protocol.AgentDeliveryScopeDelegate && ownerID > 0
	content, reasonNotice := buildAgentDeliveryFailureMessageContent(code, reason, userpref.Language(ctx, ownerID))
	if content == "" {
		// 没有可展示的原因：不写会话消息，失败状态仍通过 agent_delivery_status 推送。
		return
	}
	if reasonNotice && ownerID > 0 {
		// 带连接器原始失败原因（没 key、余额不足、进程崩溃…）的提示是主人的运维信息，
		// 任何 scope 下都只对主人可见，不出现在对端会话里。
		ownerOnly = true
	}

	extraRaw, _ := json.Marshal(map[string]any{
		"type":           "agent_delivery_notice",
		"scope":          scope,
		"code":           code,
		"owner_id":       ownerID,
		"agent_id":       agentID,
		"trigger_msg_id": triggerMsgID,
	})

	msgID := snowflake.GenID()
	now := time.Now().UTC()
	summary := ""
	if !ownerOnly && !textutil.IsStandaloneCardMessage(content) {
		summary = textutil.TruncateRunes(content, sessionSummaryMaxRunes)
	}
	var visibleTo datatypes.JSON
	if ownerOnly {
		visibleTo, _ = json.Marshal([]int64{ownerID})
	}

	sessionType := int16(1)
	deliveries := make([]agentNoticeDelivery, 0, 4)

	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		var session model.Session
		if err := tx.Select("session_id", "session_type").
			Where("session_id = ? AND is_deleted = false", sessionID).
			First(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		sessionType = session.SessionType

		if err := tx.Create(&model.Message{
			MsgID:      msgID,
			SessionID:  sessionID,
			SenderID:   agentID,
			SenderType: 2,
			MsgType:    1,
			Content:    content,
			Extra:      datatypes.JSON(extraRaw),
			VisibleTo:  visibleTo,
			CreatedAt:  now,
		}).Error; err != nil {
			return err
		}
		sessionUpdates := map[string]any{
			"last_msg_id": msgID,
			"updated_at":  now,
		}
		if summary != "" {
			sessionUpdates["last_msg_summary"] = summary
		}
		if err := tx.Model(&model.Session{}).
			Where("session_id = ?", sessionID).
			Updates(sessionUpdates).Error; err != nil {
			return err
		}

		var members []model.SessionMember
		memberQuery := tx.Select("member_id").Where("session_id = ? AND member_type = 1", sessionID)
		if ownerOnly {
			memberQuery = memberQuery.Where("member_id = ?", ownerID)
		}
		if err := memberQuery.Find(&members).Error; err != nil {
			return err
		}
		memberIDs := make([]int64, 0, len(members))
		for _, m := range members {
			memberIDs = append(memberIDs, m.MemberID)
		}
		viewingUsers := ResolveSessionViewingUsers(ctx, sessionID, memberIDs)
		nextSeqByUser, err := inboxseq.AllocateNextBatchTx(ctx, tx, memberIDs)
		if err != nil {
			return fmt.Errorf("allocate notice inbox_seq batch: %w", err)
		}
		for _, m := range members {
			if m.MemberID <= 0 {
				continue
			}

			inboxSeq := nextSeqByUser[m.MemberID]

			if err := tx.Create(&model.UserInbox{
				UserID:    m.MemberID,
				InboxSeq:  inboxSeq,
				MsgID:     msgID,
				SessionID: sessionID,
				EventKind: model.UserInboxEventKindMessage,
			}).Error; err != nil {
				return err
			}
			isViewing := viewingUsers[m.MemberID]
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
			} else {
				if err := tx.Model(&model.SessionMember{}).
					Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, m.MemberID).
					Updates(map[string]interface{}{
						"last_active_at": now,
						"unread_count":   gorm.Expr("unread_count + 1"),
					}).Error; err != nil {
					return err
				}
			}

			deliveries = append(deliveries, agentNoticeDelivery{
				memberID:              m.MemberID,
				inboxSeq:              inboxSeq,
				shouldIncrementUnread: !isViewing,
			})
		}
		return nil
	}); err != nil {
		logger.L.Warnf("emit agent delivery notice failed session=%s owner=%d agent=%d err=%v", sessionID, ownerID, agentID, err)
		return
	}

	if len(deliveries) == 0 {
		return
	}

	pushExtra := json.RawMessage(extraRaw)
	var pushVisibleTo protocol.StringInt64s
	if ownerOnly {
		pushVisibleTo = protocol.StringInt64s{ownerID}
	}
	// 私聊投递通知同样带上会话成员身份：通知的发送者是 agent 自己时客户端能
	// 推导，但主人自己名义的场景推导不出，统一带上口径才一致。
	sessionMembers := apiservice.PrivateSessionMemberIdentities(sessionID, sessionType)
	for _, d := range deliveries {
		if d.shouldIncrementUnread {
			if err := store.RDB.HIncrBy(ctx, fmt.Sprintf("im:unread:%d", d.memberID), sessionID, 1).Err(); err != nil {
				logger.L.Warnf("redis unread increment for notice failed user=%d session=%s: %v", d.memberID, sessionID, err)
			}
		} else {
			if err := store.RDB.HDel(ctx, fmt.Sprintf("im:unread:%d", d.memberID), sessionID).Err(); err != nil {
				logger.L.Warnf("redis unread clear for notice failed user=%d session=%s: %v", d.memberID, sessionID, err)
			}
		}
		broadcastPushMsgToUser(hub, ctx, d.memberID, protocol.PushMsgPayload{
			InboxSeq:    d.inboxSeq,
			MsgID:       msgID,
			SessionID:   sessionID,
			SessionType: sessionType,
			SenderID:    agentID,
			SenderType:  2,
			MsgType:     1,
			Content:     content,
			Extra:       pushExtra,
			VisibleTo:   pushVisibleTo,
			CreatedAt:   now.UnixMilli(),

			SessionMembers: sessionMembers,
		})
	}
}

// buildAgentDeliveryFailureMessageContent 返回会话提示文案。第二个返回值为 true 表示
// 文案含连接器回报的原始失败原因，调用方须把它限制为仅主人可见。
func buildAgentDeliveryFailureMessageContent(code string, reason string, language string) (string, bool) {
	copy := agentDeliveryFailureCopyFor(language)
	switch strings.TrimSpace(code) {
	case protocol.AgentDeliveryCodeAckTimeout:
		return copy.ackTimeout, false
	case protocol.AgentDeliveryCodeQueuedOffline:
		return copy.offlineQueued, false
	}
	if strings.TrimSpace(reason) == "queue full" {
		return copy.queueFull, false
	}
	summary := summarizeAgentFailureReason(reason)
	if summary == "" {
		// 连接器没带原因（如 result_timeout、event_stale）时退回按失败码的本地化文案。
		summary = notification.AgentDeliveryFailReason(strings.TrimSpace(code), language)
	}
	if summary == "" {
		return "", false
	}
	return copy.failedPrefix + summary, true
}

const agentFailureReasonMaxRunes = 200

var agentFailureReasonWhitespace = regexp.MustCompile(`\s+`)

// summarizeAgentFailureReason 把连接器回报的原始错误压成一行给主人看：
// 取首个非空行、合并空白、截断。多行堆栈/内部字段名对主人没有意义。
func summarizeAgentFailureReason(reason string) string {
	for _, line := range strings.Split(reason, "\n") {
		line = strings.TrimSpace(agentFailureReasonWhitespace.ReplaceAllString(line, " "))
		if line == "" {
			continue
		}
		return textutil.TruncateRunes(line, agentFailureReasonMaxRunes)
	}
	return ""
}

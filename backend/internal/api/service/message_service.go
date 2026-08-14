package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/inboxseq"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type MessageHistoryResp struct {
	HasMore  bool            `json:"has_more"`
	Messages []model.Message `json:"messages"`
}

type ChatTaskFinalResult struct {
	MsgID     int64
	Content   string
	CreatedAt time.Time
}

const (
	defaultMessageQueryLimit = 20
	defaultAgentMessageLimit = 1
	maxMessageQueryLimit     = 100
)

type MessageDeleteActor struct {
	UserID  int64
	AgentID int64
}

func resolveMessageDeleteActor(msg model.Message) (MessageDeleteActor, error) {
	switch msg.SenderType {
	case 1:
		if msg.SenderID <= 0 {
			return MessageDeleteActor{}, errors.New("invalid human sender")
		}
		return MessageDeleteActor{UserID: msg.SenderID}, nil
	case 2:
		if msg.SenderID <= 0 {
			return MessageDeleteActor{}, errors.New("invalid agent sender")
		}
		return MessageDeleteActor{AgentID: msg.SenderID}, nil
	default:
		return MessageDeleteActor{}, errors.New("unsupported message sender type")
	}
}

type revokedInboxRecipient struct {
	UserID   int64 `gorm:"column:user_id"`
	InboxSeq int64 `gorm:"column:inbox_seq"`
}

func MessageHistory(userID int64, sessionID string, beforeID int64, limit int) (*MessageHistoryResp, error) {
	query, normalizedLimit, err := buildVisibleSessionMessageQuery(userID, sessionID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	return loadVisibleMessagePage(query, normalizedLimit)
}

func SessionOwnedBy(userID int64, sessionID string) (bool, error) {
	var count int64
	err := store.DB.Model(&model.Session{}).
		Where("session_id = ? AND owner_id = ? AND is_deleted = false", strings.TrimSpace(sessionID), userID).
		Count(&count).Error
	return count > 0, err
}

// AgentMessageHistory returns the compact conversation view exposed to agents.
// It keeps plain text and approval-family cards while removing tool/status/
// binding cards that are useful to the UI but noisy in model context.
func AgentMessageHistory(userID int64, sessionID string, beforeID int64, limit int) (*MessageHistoryResp, error) {
	normalizedLimit := normalizeAgentMessageQueryLimit(limit)
	query, _, err := buildVisibleSessionMessageQuery(userID, sessionID, beforeID, normalizedLimit)
	if err != nil {
		return nil, err
	}
	return loadVisibleMessagePageFiltered(query, normalizedLimit, isAgentHistoryMessage)
}

// ChatTaskResult returns exactly one final, plain-text agent reply for the
// completed task represented by state. Non-completed tasks and incomplete
// state records never touch the messages table.
func ChatTaskResult(ownerID int64, state model.SessionAgentState) (*ChatTaskFinalResult, error) {
	if state.OwnerID != ownerID || state.State != model.SessionAgentStateCompleted || state.StartedAt == nil {
		return nil, nil
	}

	query := store.DB.Where(
		"session_id = ? AND sender_id = ? AND sender_type = ? AND is_deleted = false AND msg_type = ? AND created_at >= ?",
		state.SessionID,
		state.AgentID,
		2,
		model.MsgTypeText,
		state.StartedAt.UTC(),
	)
	if state.CompletedAt != nil {
		query = query.Where("created_at <= ?", state.CompletedAt.UTC())
	}

	page, err := loadVisibleMessagePageFiltered(query, 1, isPlainConversationText)
	if err != nil {
		return nil, err
	}
	if len(page.Messages) == 0 {
		return nil, nil
	}
	msg := page.Messages[0]
	return &ChatTaskFinalResult{
		MsgID:     msg.MsgID,
		Content:   msg.Content,
		CreatedAt: msg.CreatedAt,
	}, nil
}

func MessageSearch(
	userID int64,
	sessionID string,
	keyword string,
	beforeID int64,
	limit int,
) (*MessageHistoryResp, error) {
	normalizedKeyword := strings.TrimSpace(keyword)
	if normalizedKeyword == "" {
		return nil, errors.New("keyword required")
	}

	query, normalizedLimit, err := buildVisibleSessionMessageQuery(userID, sessionID, beforeID, limit)
	if err != nil {
		return nil, err
	}

	pattern := "%" + strings.ToLower(normalizedKeyword) + "%"
	query = query.Where("LOWER(content) LIKE ?", pattern)
	return loadVisibleMessagePage(query, normalizedLimit)
}

func buildVisibleSessionMessageQuery(userID int64, sessionID string, beforeID int64, limit int) (*gorm.DB, int, error) {
	if err := ensureSessionAccessible(context.Background(), sessionID); err != nil {
		return nil, 0, err
	}

	memberInfo, err := loadHumanSessionMemberInfo(userID, sessionID)
	if err != nil {
		return nil, 0, err
	}

	// 排除 msg_type=4 流式占位消息：占位消息是流式输出过程中的临时态，
	// 内容尚未封板（content 为空、无 inbox_seq），不属于已封板历史。
	// 这与 pull_sync 经 user_inbox 关联的语义保持一致——占位消息在 finalize
	// 前没有 inbox 行，pull_sync 永远不会返回它们。若 HTTP 历史返回这些占位，
	// 客户端会把空内容写入本地库并渲染成空白气泡。
	query := store.DB.Where(
		"session_id = ? AND is_deleted = false AND msg_type <> ?",
		sessionID, model.MsgTypeAIStream,
	)
	if beforeID > 0 {
		query = query.Where("msg_id < ?", beforeID)
	}
	if cutoff, ok := loadMessageHistoryCutoff(sessionID, userID); ok {
		query = query.Where("created_at > ?", cutoff)
	}
	// 群聊成员只能拉取入群时间之后的历史消息，入群前的记录对其不可见。
	if memberInfo.SessionType == model.SessionTypeGroup {
		query = query.Where("created_at >= ?", memberInfo.JoinedAt)
	}

	// Visibility filter: show message if visible_to is NULL (all members),
	// or the requesting user is the sender, or the user is in the visible_to list.
	// Only applies to PostgreSQL (production); SQLite tests skip this filter.
	if store.IsPostgres() {
		query = query.Where(
			"visible_to IS NULL OR sender_id = ? OR visible_to @> to_jsonb(?::bigint)",
			userID, userID,
		)
	}

	return query, normalizeMessageQueryLimit(limit), nil
}

type humanSessionMemberInfo struct {
	JoinedAt    time.Time
	SessionType int16
}

func loadHumanSessionMemberInfo(userID int64, sessionID string) (*humanSessionMemberInfo, error) {
	var info struct {
		JoinedAt    time.Time `gorm:"column:joined_at"`
		SessionType int16     `gorm:"column:session_type"`
	}
	err := store.DB.Table("session_members AS me").
		Select("me.joined_at, s.session_type").
		Joins("JOIN sessions AS s ON s.session_id = me.session_id").
		Where(
			"me.session_id = ? AND me.member_id = ? AND me.member_type = 1",
			sessionID,
			userID,
		).
		First(&info).Error
	if err != nil {
		return nil, err
	}
	// messages.created_at 入库时统一转 UTC（model.Message.BeforeCreate），
	// joined_at 与 created_at 比较前必须对齐到同一时间轴。
	return &humanSessionMemberInfo{
		JoinedAt:    info.JoinedAt.UTC(),
		SessionType: info.SessionType,
	}, nil
}

func ensureHumanSessionMembership(userID int64, sessionID string) error {
	var member model.SessionMember
	return store.DB.Where(
		"session_id = ? AND member_id = ? AND member_type = 1",
		sessionID,
		userID,
	).First(&member).Error
}

func loadMessageHistoryCutoff(sessionID string, userID int64) (time.Time, bool) {
	if sessionID == "" || userID <= 0 {
		return time.Time{}, false
	}

	var reset model.SessionHistoryReset
	result := store.DB.Where("session_id = ? AND user_id = ?", sessionID, userID).
		Limit(1).
		Find(&reset)
	if result.Error != nil || result.RowsAffected == 0 {
		return time.Time{}, false
	}
	return reset.DeletedBefore.UTC(), !reset.DeletedBefore.IsZero()
}

func loadVisibleMessagePage(query *gorm.DB, limit int) (*MessageHistoryResp, error) {
	if query == nil {
		return nil, errors.New("message query unavailable")
	}

	normalizedLimit := normalizeMessageQueryLimit(limit)
	var messages []model.Message
	err := query.Order("msg_id DESC").Limit(normalizedLimit + 1).Find(&messages).Error
	if err != nil {
		return nil, err
	}

	hasMore := len(messages) > normalizedLimit
	if hasMore {
		messages = messages[:normalizedLimit]
	}
	signMessagePage(messages)
	return &MessageHistoryResp{HasMore: hasMore, Messages: messages}, nil
}

func loadVisibleMessagePageFiltered(
	query *gorm.DB,
	limit int,
	keep func(model.Message) bool,
) (*MessageHistoryResp, error) {
	if query == nil {
		return nil, errors.New("message query unavailable")
	}
	if keep == nil {
		return loadVisibleMessagePage(query, limit)
	}

	normalizedLimit := normalizeMessageQueryLimit(limit)
	targetCount := normalizedLimit + 1
	batchSize := max(20, targetCount*2)
	batchSize = min(batchSize, maxMessageQueryLimit)
	filtered := make([]model.Message, 0, targetCount)
	var cursor int64

	for len(filtered) < targetCount {
		pageQuery := query
		if cursor > 0 {
			pageQuery = pageQuery.Where("msg_id < ?", cursor)
		}
		var batch []model.Message
		if err := pageQuery.Order("msg_id DESC").Limit(batchSize).Find(&batch).Error; err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		cursor = batch[len(batch)-1].MsgID
		for _, msg := range batch {
			if keep(msg) {
				filtered = append(filtered, msg)
				if len(filtered) == targetCount {
					break
				}
			}
		}
		if len(batch) < batchSize {
			break
		}
	}

	hasMore := len(filtered) > normalizedLimit
	if hasMore {
		filtered = filtered[:normalizedLimit]
	}
	signMessagePage(filtered)
	return &MessageHistoryResp{HasMore: hasMore, Messages: filtered}, nil
}

func signMessagePage(messages []model.Message) {
	// Re-sign at egress to handle messages stored before creation-time signing was
	// deployed (bare URLs in old rows). New rows already carry signed URLs; signing
	// them again just refreshes the TTL, which is harmless.
	for i := range messages {
		signedContent, signedExtra := SignMessageMedia(
			messages[i].Content,
			json.RawMessage(messages[i].Extra),
		)
		messages[i].Content = signedContent
		messages[i].Extra = datatypes.JSON(signedExtra)
	}
}

func isAgentHistoryMessage(msg model.Message) bool {
	if msg.MsgType != model.MsgTypeText || strings.TrimSpace(msg.Content) == "" {
		return false
	}
	cardTypes := messageCardTypes(msg)
	if len(cardTypes) == 0 {
		return true
	}
	for _, cardType := range cardTypes {
		if cardType != "exec_approval" && cardType != "exec_status" {
			return false
		}
	}
	return true
}

func isPlainConversationText(msg model.Message) bool {
	return msg.MsgType == model.MsgTypeText &&
		strings.TrimSpace(msg.Content) != "" &&
		len(messageCardTypes(msg)) == 0
}

func messageCardTypes(msg model.Message) []string {
	seen := make(map[string]struct{})
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			seen[value] = struct{}{}
		}
	}

	content := strings.ToLower(msg.Content)
	const marker = "grix://card/"
	for {
		index := strings.Index(content, marker)
		if index < 0 {
			break
		}
		content = content[index+len(marker):]
		end := len(content)
		for i, r := range content {
			if r == '?' || r == '#' || r == ')' || r == ']' || r == '&' ||
				r == ' ' || r == '\t' || r == '\r' || r == '\n' {
				end = i
				break
			}
		}
		add(content[:end])
		if end >= len(content) {
			break
		}
		content = content[end:]
	}

	var envelope map[string]any
	if len(msg.Extra) > 0 && json.Unmarshal(msg.Extra, &envelope) == nil {
		if bizCard, ok := envelope["biz_card"].(map[string]any); ok {
			if cardType, ok := bizCard["type"].(string); ok {
				add(cardType)
			}
		}
	}

	cardTypes := make([]string, 0, len(seen))
	for cardType := range seen {
		cardTypes = append(cardTypes, cardType)
	}
	return cardTypes
}

func normalizeMessageQueryLimit(limit int) int {
	if limit <= 0 {
		return defaultMessageQueryLimit
	}
	if limit > maxMessageQueryLimit {
		return maxMessageQueryLimit
	}
	return limit
}

func normalizeAgentMessageQueryLimit(limit int) int {
	if limit <= 0 {
		return defaultAgentMessageLimit
	}
	return normalizeMessageQueryLimit(limit)
}

func (a MessageDeleteActor) canDelete(msg model.Message) bool {
	switch msg.SenderType {
	case 1:
		return a.UserID > 0 && msg.SenderID == a.UserID
	case 2:
		return a.AgentID > 0 && msg.SenderID == a.AgentID
	default:
		return false
	}
}

func (a MessageDeleteActor) canRevokeGroupMessage(
	session model.Session,
) (bool, error) {
	if session.SessionType != model.SessionTypeGroup || a.UserID <= 0 {
		return false, nil
	}

	var operator model.SessionMember
	if err := store.DB.Select("role").
		Where(
			"session_id = ? AND member_id = ? AND member_type = 1",
			session.SessionID,
			a.UserID,
		).
		First(&operator).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}

	return operator.Role == 2 || operator.Role == 3, nil
}

func (a MessageDeleteActor) canRevokeOwnedAgentDirectMessage(
	session model.Session,
	msg model.Message,
) (bool, error) {
	if session.SessionType != model.SessionTypeDirect {
		return false, nil
	}
	if a.UserID <= 0 || session.OwnerID != a.UserID {
		return false, nil
	}
	if msg.SenderType != 2 || msg.SenderID <= 0 {
		return false, nil
	}

	var agent model.Agent
	if err := store.DB.Select("id", "owner_id").
		Where("id = ?", msg.SenderID).
		First(&agent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if agent.OwnerID != a.UserID {
		return false, nil
	}

	var totalMembers int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ?", session.SessionID).
		Count(&totalMembers).Error; err != nil {
		return false, err
	}
	if totalMembers != 2 {
		return false, nil
	}

	var ownerMembershipCount int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where(
			"session_id = ? AND member_id = ? AND member_type = 1",
			session.SessionID,
			a.UserID,
		).
		Count(&ownerMembershipCount).Error; err != nil {
		return false, err
	}
	if ownerMembershipCount != 1 {
		return false, nil
	}

	var agentMembershipCount int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where(
			"session_id = ? AND member_id = ? AND member_type = 2",
			session.SessionID,
			msg.SenderID,
		).
		Count(&agentMembershipCount).Error; err != nil {
		return false, err
	}

	return agentMembershipCount == 1, nil
}

func collectInboxRecipientsTx(tx *gorm.DB, sessionID string, msgID int64) ([]revokedInboxRecipient, error) {
	if tx == nil {
		return nil, errors.New("delete message transaction unavailable")
	}

	var recipients []revokedInboxRecipient
	if err := tx.Model(&model.UserInbox{}).
		Select("user_id, MAX(inbox_seq) AS inbox_seq").
		Where("session_id = ? AND msg_id = ?", sessionID, msgID).
		Order("user_id ASC").
		Group("user_id").
		Scan(&recipients).Error; err != nil {
		return nil, err
	}
	return recipients, nil
}

func recreateRevokedInboxTombstonesTx(
	ctx context.Context,
	tx *gorm.DB,
	sessionID string,
	msgID int64,
	recipients []revokedInboxRecipient,
	createdAt time.Time,
) ([]model.UserInbox, error) {
	if len(recipients) == 0 {
		return nil, nil
	}

	// 墓碑序号必须严格大于该用户对本消息的原投递序号（recipient.InboxSeq），
	// 作为 per-user floor 传入。统一走 Redis 单源批量发号，避免与正常消息
	// 发号（同样走 Redis）产生跨路径撞号。
	userIDs := make([]int64, 0, len(recipients))
	extraFloorByUser := make(map[int64]int64, len(recipients))
	for _, recipient := range recipients {
		userIDs = append(userIDs, recipient.UserID)
		if recipient.InboxSeq > extraFloorByUser[recipient.UserID] {
			extraFloorByUser[recipient.UserID] = recipient.InboxSeq
		}
	}

	nextSeqByUser, err := inboxseq.AllocateNextBatchWithFloorTx(ctx, tx, userIDs, extraFloorByUser)
	if err != nil {
		return nil, err
	}

	rows := make([]model.UserInbox, 0, len(recipients))
	for _, recipient := range recipients {
		rows = append(rows, model.UserInbox{
			UserID:    recipient.UserID,
			InboxSeq:  nextSeqByUser[recipient.UserID],
			MsgID:     msgID,
			SessionID: sessionID,
			EventKind: model.UserInboxEventKindRevoke,
			CreatedAt: createdAt,
		})
	}
	if err := tx.Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func RevokeMessageForStop(ctx context.Context, sessionID string, msgID int64) error {
	if ctx == nil {
		ctx = context.Background()
	}

	var msg model.Message
	if err := store.DB.
		Select("msg_id", "session_id", "sender_id", "sender_type", "is_deleted").
		Where("msg_id = ? AND session_id = ?", msgID, sessionID).
		First(&msg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if msg.IsDeleted {
		return nil
	}

	actor, err := resolveMessageDeleteActor(msg)
	if err != nil {
		return err
	}
	return DeleteMessage(ctx, sessionID, msgID, actor)
}

func DeleteMessage(ctx context.Context, sessionID string, msgID int64, actor MessageDeleteActor) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ensureSessionAccessible(ctx, sessionID); err != nil {
		return err
	}

	var msg model.Message
	if err := store.DB.Where("msg_id = ? AND session_id = ?", msgID, sessionID).First(&msg).Error; err != nil {
		return errors.New("20008") // either not found or cross-session, unify with 20008
	}

	var session model.Session
	if err := store.DB.Select("session_id", "owner_id", "session_type").Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		return err
	}

	authorized := actor.canDelete(msg)
	if !authorized {
		canRevoke, err := actor.canRevokeGroupMessage(session)
		if err != nil {
			return err
		}
		authorized = canRevoke
	}
	if !authorized {
		canRevoke, err := actor.canRevokeOwnedAgentDirectMessage(session, msg)
		if err != nil {
			return err
		}
		authorized = canRevoke
	}
	if !authorized {
		return errors.New("20008")
	}

	if msg.IsDeleted {
		return nil
	}

	var members []model.SessionMember
	var revokeInboxRows []model.UserInbox
	var unreadMemberIDs []int64
	var hadOriginalInboxRecipients bool
	revokeUnreadCountByUserID := make(map[int64]int, 4)
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ?", sessionID).Find(&members).Error; err != nil {
			return err
		}
		inboxRecipients, err := collectInboxRecipientsTx(tx, sessionID, msgID)
		if err != nil {
			return err
		}
		hadOriginalInboxRecipients = len(inboxRecipients) > 0
		revokedAt := time.Now().UTC()

		if err := tx.Model(&msg).Updates(map[string]interface{}{
			"is_deleted": true,
			"is_revoked": true,
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("msg_id = ? AND session_id = ?", msgID, sessionID).Delete(&model.UserInbox{}).Error; err != nil {
			return err
		}
		if len(inboxRecipients) > 0 {
			rows, err := recreateRevokedInboxTombstonesTx(
				ctx,
				tx,
				sessionID,
				msgID,
				inboxRecipients,
				revokedAt,
			)
			if err != nil {
				return err
			}
			revokeInboxRows = rows
		} else {
			rows, err := buildMessageRevokeInboxRowsTx(
				ctx,
				tx,
				members,
				sessionID,
				msgID,
			)
			if err != nil {
				return err
			}
			if len(rows) > 0 {
				for i := range rows {
					rows[i].CreatedAt = revokedAt
				}
				if err := tx.Create(&rows).Error; err != nil {
					return err
				}
			}
			revokeInboxRows = rows
		}

		// Recompute last_msg for the session
		var lastMsg model.Message
		lastMsgErr := tx.
			Select("msg_id", "content").
			Where("session_id = ? AND is_deleted = false AND msg_type <> ? AND content NOT LIKE ?",
				sessionID, model.MsgTypeAIStream, "%](grix://card/%").
			Order("msg_id DESC").
			First(&lastMsg).Error
		switch {
		case lastMsgErr == nil:
			summary := textutil.TruncateRunes(lastMsg.Content, 60)
			if err := tx.Model(&model.Session{}).Where("session_id = ?", sessionID).Updates(map[string]interface{}{
				"last_msg_id":      lastMsg.MsgID,
				"last_msg_summary": summary,
				"updated_at":       revokedAt,
			}).Error; err != nil {
				return err
			}
		case errors.Is(lastMsgErr, gorm.ErrRecordNotFound):
			if err := tx.Model(&model.Session{}).Where("session_id = ?", sessionID).Updates(map[string]interface{}{
				"last_msg_id":      0,
				"last_msg_summary": "",
				"updated_at":       revokedAt,
			}).Error; err != nil {
				return err
			}
		default:
			return lastMsgErr
		}

		// Only real inbox rows can contribute to unread counters.
		unreadMemberIDs = unreadMemberIDs[:0]
		for _, member := range members {
			if member.MemberType != 1 || member.MemberID <= 0 {
				continue
			}
			nextUnreadCount := member.UnreadCount
			if hadOriginalInboxRecipients &&
				member.LastReadMsgID < msgID &&
				member.UnreadCount > 0 {
				unreadMemberIDs = append(unreadMemberIDs, member.MemberID)
				nextUnreadCount--
			}
			revokeUnreadCountByUserID[member.MemberID] = nextUnreadCount
		}
		if hadOriginalInboxRecipients && len(unreadMemberIDs) > 0 {
			if err := tx.Model(&model.SessionMember{}).
				Where("session_id = ? AND member_id IN ? AND member_type = 1", sessionID, unreadMemberIDs).
				UpdateColumn(
					"unread_count",
					gorm.Expr("CASE WHEN unread_count > 0 THEN unread_count - 1 ELSE 0 END"),
				).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return err
	}

	if hadOriginalInboxRecipients && len(unreadMemberIDs) > 0 && store.RDB != nil {
		pipe := store.RDB.Pipeline()
		for _, userID := range unreadMemberIDs {
			pipe.HIncrBy(ctx, fmt.Sprintf("im:unread:%d", userID), sessionID, -1)
		}
		if _, execErr := pipe.Exec(ctx); execErr != nil {
			logger.L.Warnf(
				"delete message unread redis sync error session=%s msg=%d: %v",
				sessionID,
				msgID,
				execErr,
			)
		}
	}

	// Broadcast
	revokePayload := protocol.AgentRevokeEventPayload{
		SessionID:   sessionID,
		ThreadID:    msg.ThreadID,
		SessionType: session.SessionType,
		MsgID:       msgID,
		SenderID:    msg.SenderID,
		IsRevoked:   true,
	}
	revokeInboxSeqByUserID := make(map[int64]int64, len(revokeInboxRows))
	for _, row := range revokeInboxRows {
		if row.UserID <= 0 || row.InboxSeq <= 0 {
			continue
		}
		revokeInboxSeqByUserID[row.UserID] = row.InboxSeq
	}

	for _, m := range members {
		if m.MemberType == 1 {
			userPayload := map[string]interface{}{
				"msg_id":       fmt.Sprintf("%d", revokePayload.MsgID),
				"session_id":   revokePayload.SessionID,
				"session_type": revokePayload.SessionType,
				"sender_id":    fmt.Sprintf("%d", revokePayload.SenderID),
				"is_revoked":   revokePayload.IsRevoked,
			}
			if revokePayload.ThreadID != "" {
				userPayload["thread_id"] = revokePayload.ThreadID
			}
			if inboxSeq := revokeInboxSeqByUserID[m.MemberID]; inboxSeq > 0 {
				userPayload["inbox_seq"] = fmt.Sprintf("%d", inboxSeq)
			}
			if unreadCount, ok := revokeUnreadCountByUserID[m.MemberID]; ok {
				userPayload["session_unread_count"] = unreadCount
			}
			pushRealtimeEvent(m.MemberID, "push_revoke", userPayload)
		} else if m.MemberType == 2 {
			// agent 共享多连接物理隔离:私聊场景下按 session.OwnerID(=与该 agent 对话的那个人,可能是
			// 主人 A 也可能是被共享者 B)精确路由,确保 B↔X 的 revoke 落到 B 的 connector,而不是
			// 主人 A 的。群聊场景 agent 始终走主实例(共享只在私聊生效):owner=0 已被路由层视为非法,
			// 必须显式解析 agent.OwnerID 后按精确路由推送。
			pushOwnerID := int64(0)
			if revokePayload.SessionType == 1 {
				pushOwnerID = session.OwnerID
			} else {
				pushOwnerID = resolveAgentPrimaryOwnerID(m.MemberID)
			}
			pushAgentChannelEvent(m.MemberID, pushOwnerID, "event_revoke", revokePayload)
		}
	}

	scheduleRevokedMessageAttachmentCleanup(msg)

	return nil
}

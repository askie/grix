package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/conversationaudit"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/inboxseq"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/mention"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/askie/grix/backend/internal/ws/threadmeta"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const sendMsgDedupTTL = 6 * time.Hour
const sessionSummaryMaxRunes = 60
const delegateHumanRateTTL = 3 * time.Second
const delegateAutoRateTTL = 350 * time.Millisecond

// delegateVoiceRateTTL 是直拨语音转写触发托管的限流窗口（架构文档 33）。
// ASR debounce 已按 1.5s 沉默聚合句子，相邻两段独立表达的间隔可短于常规
// delegateHumanRateTTL，沿用 3s 会丢弃第二段，故放宽到 1s。
const delegateVoiceRateTTL = 1 * time.Second

var scheduleContentModeration = apiservice.ScheduleContentModeration

func sendMsgNack(
	conn ConnInterface,
	pkt *protocol.Packet,
	clientMsgID string,
	code int,
	msg string,
) {
	conn.SendPayload(protocol.CmdSendNack, pkt.Seq, protocol.SendNackPayload{
		ClientMsgID: clientMsgID,
		Code:        code,
		Msg:         msg,
	})
}

type persistentSendMsgReceipt struct {
	MsgID     int64
	InboxSeq  int64
	CreatedAt time.Time
}

func sendMsgClientKey(clientMsgID string) string {
	sum := sha256.Sum256([]byte(clientMsgID))
	return fmt.Sprintf("%x", sum[:])
}

func loadPersistentSendMsgReceiptByKey(
	sessionID string,
	senderID int64,
	clientMsgKey string,
) (*persistentSendMsgReceipt, error) {
	if store.DB == nil || strings.TrimSpace(sessionID) == "" ||
		senderID <= 0 || strings.TrimSpace(clientMsgKey) == "" {
		return nil, nil
	}
	var row model.SendMsgIdempotencyReceipt
	err := store.DB.
		Where(
			"session_id = ? AND sender_id = ? AND client_msg_key = ?",
			sessionID,
			senderID,
			clientMsgKey,
		).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &persistentSendMsgReceipt{
		MsgID:     row.MsgID,
		InboxSeq:  row.InboxSeq,
		CreatedAt: row.CreatedAt,
	}, nil
}

func sendExistingSendMsgAck(
	ctx context.Context,
	hub HubInterface,
	conn ConnInterface,
	pkt *protocol.Packet,
	payload protocol.SendMsgPayload,
	senderType int16,
	skipAutoClearSenderComposing bool,
	receipt persistentSendMsgReceipt,
) {
	var localInferenceHint *protocol.LocalInferenceHint
	hint, hintErr := loadPendingLocalInferenceHint(ctx, receipt.MsgID)
	if hintErr != nil {
		logger.L.Warnf(
			"load pending local inference hint failed user=%d session=%s msg_id=%d client_msg_id=%s: %v",
			conn.GetUserID(),
			payload.SessionID,
			receipt.MsgID,
			payload.ClientMsgID,
			hintErr,
		)
	} else {
		localInferenceHint = hint
	}
	createdAt := receipt.CreatedAt.UnixMilli()
	if createdAt <= 0 {
		createdAt = time.Now().UnixMilli()
	}
	conn.SendPayload(protocol.CmdSendAck, pkt.Seq, protocol.SendAckPayload{
		MsgID:          receipt.MsgID,
		ClientMsgID:    payload.ClientMsgID,
		InboxSeq:       receipt.InboxSeq,
		CreatedAt:      createdAt,
		LocalInference: localInferenceHint,
	})
	if !skipAutoClearSenderComposing {
		clearSenderComposing(ctx, hub, payload.SessionID, conn.GetUserID(), senderType)
	}
}

func HandleSendMsg(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.SendMsgPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("send_msg payload error: %v", err)
		sendMsgNack(conn, pkt, extractClientMsgIDFromSendMsgPayload(pkt.Payload), 4001, "invalid send_msg payload")
		return
	}
	if payload.SessionID == "" || payload.ClientMsgID == "" {
		// agent 连接（agent_api_*）经工具调用等路径发消息时可能漏填 client_msg_id，
		// 直接拒绝会让 agent 的回复整条丢失、事件被判失败（用户看到 agent 无响应）。
		// 此处为 agent 连接自动补一个唯一 id；人类客户端仍按协议拒绝，暴露客户端 bug。
		if payload.SessionID != "" && payload.ClientMsgID == "" &&
			strings.HasPrefix(conn.GetDeviceID(), "agent_api_") {
			payload.ClientMsgID = fmt.Sprintf("agent_auto_%d", snowflake.GenID())
			logger.L.Warnf("send_msg auto-fill empty client_msg_id: user=%d session_id=%q client_msg_id=%q",
				conn.GetUserID(), payload.SessionID, payload.ClientMsgID)
		} else {
			logger.L.Warnf("send_msg invalid payload: user=%d session_id=%q client_msg_id=%q",
				conn.GetUserID(), payload.SessionID, payload.ClientMsgID)
			sendMsgNack(conn, pkt, payload.ClientMsgID, 4001, "invalid send_msg payload")
			return
		}
	}
	if payload.MsgType <= 0 {
		payload.MsgType = 1
	}
	payload.ThreadID = strings.TrimSpace(payload.ThreadID)

	ctx := context.Background()
	sessionType := int16(1)
	var groupNormalization *groupMentionNormalization
	sessionType = loadSessionType(payload.SessionID)

	// Resolve membership before group mention normalization. ModeCaller /
	// ModeDelegate bridge conns use device_id agent_api_* while sender_id is
	// the human owner — classifying them as agent here would take the
	// agent-to-agent quote loop path and skip human quote→wake semantics.
	var member model.SessionMember
	memberErr := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ?", payload.SessionID, conn.GetUserID()).
		First(&member).Error

	if sessionType == 2 {
		senderTypeForNormalization := int16(1)
		if memberErr == nil && member.MemberType > 0 {
			senderTypeForNormalization = member.MemberType
		} else if strings.HasPrefix(conn.GetDeviceID(), "agent_api_") {
			senderTypeForNormalization = 2
		}
		normalization := resolveGroupMentionDispatchNormalization(
			ctx,
			payload.SessionID,
			conn.GetUserID(),
			senderTypeForNormalization,
			payload.QuotedMessageID,
			payload.Content,
			payload.Extra,
		)
		groupNormalization = &normalization
		payload.Extra = normalization.ExtraRaw
		logger.L.Debugf(
			"[DelegateTrace] mention normalized session=%s sender=%d sender_type=%d mentions=%v content_len=%d",
			payload.SessionID,
			conn.GetUserID(),
			senderTypeForNormalization,
			normalization.MentionUserIDs,
			len(payload.Content),
		)
	} else {
		payload.Extra = mention.RemoveMentionUserIDs(payload.Extra)
	}
	payload.Extra = threadmeta.Merge(payload.Extra, payload.ThreadID)

	// Quote visibility inheritance: when quoting a visible_to-restricted message,
	// force the reply's visible_to to [quoted message sender] regardless of client setting.
	if sessionType == 2 && payload.QuotedMessageID > 0 {
		quotedSenderID, quotedVisibleTo := ResolveQuotedMessageOwnerAndVisibility(
			payload.SessionID, payload.QuotedMessageID,
		)
		if quotedSenderID > 0 && len(quotedVisibleTo) > 0 {
			payload.VisibleTo = []int64{quotedSenderID}
		}
	}
	if sessionType == 2 && isOpenSessionSubmit(payload.Content) {
		if targetAgentID := resolveGroupOpenSessionSubmitTarget(ctx, payload.SessionID, payload.QuotedMessageID); targetAgentID > 0 {
			payload.VisibleTo = []int64{targetAgentID}
		}
	}

	// Validate visible_to: only for group sessions, filter to valid group members.
	var validVisibleTo []int64
	if sessionType == 2 && len(payload.VisibleTo) > 0 {
		validatedVisibleTo, err := validateVisibleToMembers(payload.SessionID, payload.VisibleTo)
		if err != nil {
			logger.L.Warnf(
				"validate visible_to members failed session=%s sender=%d client_msg_id=%s err=%v",
				payload.SessionID,
				conn.GetUserID(),
				payload.ClientMsgID,
				err,
			)
			sendMsgNack(conn, pkt, payload.ClientMsgID, 5001, "save message failed")
			return
		}
		if len(validatedVisibleTo) == 0 {
			logger.L.Warnf(
				"invalid visible_to members session=%s sender=%d client_msg_id=%s visible_to=%v",
				payload.SessionID,
				conn.GetUserID(),
				payload.ClientMsgID,
				payload.VisibleTo,
			)
			sendMsgNack(conn, pkt, payload.ClientMsgID, 4001, "invalid visible_to members")
			return
		}
		validVisibleTo = validatedVisibleTo
	}

	// When visible_to is set, treat only those users as @mentioned targets.
	// @mentions of users outside visible_to are discarded: those users cannot
	// see the message, so including them in the mention list would leak intent
	// and cause agent dispatch to trigger for inaccessible users.
	if sessionType == 2 && len(validVisibleTo) > 0 && groupNormalization != nil {
		restricted := append([]int64(nil), validVisibleTo...)
		groupNormalization.MentionUserIDs = restricted
		groupNormalization.ExplicitMentionUserIDs = restricted
		groupNormalization.HasExplicitMentions = len(restricted) > 0
		payload.Extra = writeCanonicalMentionUserIDs(payload.Extra, restricted)
		payload.Extra = writeExplicitMentionUserIDs(payload.Extra, restricted)
	}

	if code, msg := validateSendContent(ctx, conn.GetUserID(), payload.SessionID, payload.Content); code != 0 {
		metrics := inspectSendContentMetrics(payload.Content)
		logger.L.Warnf("send_msg guard rejected: user=%d session=%s client_msg_id=%s code=%d msg=%s",
			conn.GetUserID(), payload.SessionID, payload.ClientMsgID, code, msg)
		logger.L.Warnf(
			"send_msg guard details: user=%d session=%s client_msg_id=%s code=%d msg=%s bytes=%d runes=%d",
			conn.GetUserID(),
			payload.SessionID,
			payload.ClientMsgID,
			code,
			msg,
			metrics.TrimmedLengthBytes,
			metrics.TrimmedLengthRunes,
		)
		sendMsgNack(conn, pkt, payload.ClientMsgID, code, msg)
		return
	}

	delegateMeta := parseDelegateTriggerMeta(payload.Extra)
	skipAutoClearSenderComposing := strings.HasPrefix(conn.GetDeviceID(), "agent_api_")

	// Permission check: sender must be a member in session (human or API agent).
	if memberErr != nil {
		logger.L.Warnf("send_msg permission denied: user=%d session=%s", conn.GetUserID(), payload.SessionID)
		sendMsgNack(conn, pkt, payload.ClientMsgID, 4003, "permission denied")
		return
	}
	senderType := member.MemberType

	// 预加载会话内 AI agent(member_type=2),供归属 guard 与直聊路由复用,去重查询。
	var directAgents []directSessionAgentRow
	if senderType != 2 {
		directAgents, _ = loadDirectSessionAgents(payload.SessionID)
	}

	// 对话审计标记服务端权威化：忽略客户端 extra 里的 audit 键，
	// 仅按服务端持久化的 (user, agent) 偏好决定是否给消息打审计标记。
	if senderType == 1 {
		agentIDs := make([]int64, 0, len(directAgents))
		for _, a := range directAgents {
			if a.ID > 0 {
				agentIDs = append(agentIDs, a.ID)
			}
		}
		payload.Extra = conversationaudit.ApplyTurnPreference(payload.Extra, conn.GetUserID(), agentIDs)
	}

	// Agent usage guard: in private sessions, a human sender may only send to
	// agents they're allowed to use —— the owner, or a still-valid shared
	// grantee (agent 共享). Runs on every message, so revoking a share takes
	// effect immediately, without waiting for the connection to drop.
	if sessionType == 1 && senderType == 1 {
		for _, a := range directAgents {
			if a.Status != 1 || a.OwnerID == conn.GetUserID() {
				continue
			}
			ok, err := apiservice.CanUseAgent(conn.GetUserID(), a.ID)
			if err != nil || !ok {
				logger.L.Warnf("send_msg agent usage denied: user=%d session=%s agent=%d owner=%d err=%v",
					conn.GetUserID(), payload.SessionID, a.ID, a.OwnerID, err)
				sendMsgNack(conn, pkt, payload.ClientMsgID, 4003, "permission denied")
				return
			}
		}
	}

	if err := validatePrivateHumanSendPermission(payload.SessionID, conn.GetUserID(), senderType, sessionType); err != nil {
		if errors.Is(err, errPrivatePeerNotFriend) || errors.Is(err, errPrivatePeerBlocked) {
			logger.L.Warnf(
				"send_msg rejected: sender=%d session=%s client_msg_id=%s reason=%v",
				conn.GetUserID(),
				payload.SessionID,
				payload.ClientMsgID,
				err,
			)
			sendMsgNack(conn, pkt, payload.ClientMsgID, 4003, err.Error())
			return
		}
		logger.L.Errorf(
			"send_msg private friend guard failed user=%d session=%s client_msg_id=%s: %v",
			conn.GetUserID(),
			payload.SessionID,
			payload.ClientMsgID,
			err,
		)
		sendMsgNack(conn, pkt, payload.ClientMsgID, 5001, "save message failed")
		return
	}
	if err := sessionguard.ValidateSpeakPermission(
		ctx,
		nil,
		payload.SessionID,
		member.MemberID,
		member.MemberType,
	); err != nil {
		logger.L.Warnf(
			"send_msg speaking denied: user=%d session=%s client_msg_id=%s err=%v",
			conn.GetUserID(),
			payload.SessionID,
			payload.ClientMsgID,
			err,
		)
		if !sessionguard.IsDeniedError(err) {
			sendMsgNack(conn, pkt, payload.ClientMsgID, 5001, "save message failed")
			return
		}
		sendMsgNack(conn, pkt, payload.ClientMsgID, 4003, sessionguard.ErrorMessage(err))
		return
	}

	// Idempotency check via client_msg_id
	dedupKey := fmt.Sprintf(
		"msg:dedup:%d:%s:%s",
		conn.GetUserID(),
		payload.SessionID,
		payload.ClientMsgID,
	)
	existingMsgID, err := store.RDB.Get(ctx, dedupKey).Int64()
	if err == nil && existingMsgID > 0 {
		receipt := persistentSendMsgReceipt{MsgID: existingMsgID}
		var persisted model.Message
		if err := store.DB.
			Select("created_at").
			Where("msg_id = ? AND session_id = ?", existingMsgID, payload.SessionID).
			First(&persisted).Error; err == nil {
			receipt.CreatedAt = persisted.CreatedAt
		}
		var inbox model.UserInbox
		if err := store.DB.
			Where("user_id = ? AND msg_id = ? AND session_id = ?", conn.GetUserID(), existingMsgID, payload.SessionID).
			Order("inbox_seq DESC").
			First(&inbox).Error; err == nil {
			receipt.InboxSeq = inbox.InboxSeq
		}
		sendExistingSendMsgAck(
			ctx, hub, conn, pkt, payload, senderType,
			skipAutoClearSenderComposing, receipt,
		)
		return
	}
	usePermanentReceipt := strings.HasPrefix(conn.GetDeviceID(), "agent_api_")
	clientMsgKey := ""
	if usePermanentReceipt {
		clientMsgKey = sendMsgClientKey(payload.ClientMsgID)
		persistedReceipt, receiptErr := loadPersistentSendMsgReceiptByKey(
			payload.SessionID,
			conn.GetUserID(),
			clientMsgKey,
		)
		if receiptErr != nil {
			logger.L.Errorf(
				"load permanent send_msg receipt failed user=%d session=%s client_msg_id=%s: %v",
				conn.GetUserID(),
				payload.SessionID,
				payload.ClientMsgID,
				receiptErr,
			)
			sendMsgNack(conn, pkt, payload.ClientMsgID, 5001, "save message failed")
			return
		}
		if persistedReceipt != nil {
			if err := store.RDB.Set(
				ctx, dedupKey, persistedReceipt.MsgID, sendMsgDedupTTL,
			).Err(); err != nil {
				logger.L.Warnf(
					"repair send dedup key error user=%d session=%s client_msg_id=%s: %v",
					conn.GetUserID(),
					payload.SessionID,
					payload.ClientMsgID,
					err,
				)
			}
			sendExistingSendMsgAck(
				ctx, hub, conn, pkt, payload, senderType,
				skipAutoClearSenderComposing, *persistedReceipt,
			)
			return
		}
	}

	var liveGroupSemantics *groupDispatchSemantics
	if sessionType == 2 {
		var (
			semantics  groupDispatchSemantics
			resolveErr error
		)
		if groupNormalization != nil {
			semantics, resolveErr = resolveLiveGroupDispatchSemanticsWithNormalization(
				ctx,
				payload.SessionID,
				conn.GetUserID(),
				senderType,
				payload.QuotedMessageID,
				payload.Content,
				payload.Extra,
				*groupNormalization,
				!delegateMeta.IsDelegateOrigin,
			)
		} else {
			semantics, resolveErr = resolveLiveGroupDispatchSemantics(
				ctx,
				payload.SessionID,
				conn.GetUserID(),
				senderType,
				payload.QuotedMessageID,
				payload.Content,
				payload.Extra,
				!delegateMeta.IsDelegateOrigin,
			)
		}
		liveGroupSemantics = &semantics
		if liveGroupSemantics != nil && liveGroupSemantics.ContinuedMentionAll {
			payload.Extra = stripGroupMentionExtra(payload.Extra)
		}
		if resolveErr != nil {
			logger.L.Warnf(
				"resolve live group dispatch semantics failed session=%s sender=%d client_msg_id=%s: %v",
				payload.SessionID,
				conn.GetUserID(),
				payload.ClientMsgID,
				resolveErr,
			)
		}
	}

	msgID := snowflake.GenID()
	now := time.Now().UTC()

	// Sign media URLs in one place: overwrite payload so every downstream path
	// (DB write, real-time push, delegate routing, agent delivery) uses the
	// same signed URL automatically. URLs carry a 7-day TTL.
	payload.Content, payload.Extra = apiservice.SignMessageMedia(payload.Content, payload.Extra)

	// Update session last_msg (skip card messages that are not human-readable previews)
	summary := ""
	if !textutil.IsStandaloneCardMessage(payload.Content) {
		summary = textutil.TruncateRunes(payload.Content, sessionSummaryMaxRunes)
	}
	senderInboxSeq := int64(0)
	isFirstHumanTextMessage := false
	type recipientDelivery struct {
		memberID              int64
		inboxSeq              int64
		shouldIncrementUnread bool
	}
	recipientDeliveries := make([]recipientDelivery, 0, 4)
	var concurrentReceipt *persistentSendMsgReceipt

	// Keep DB writes for this message atomic:
	// message row + session summary + sender/recipient inbox + recipient unread_count.
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if usePermanentReceipt {
			row := model.SendMsgIdempotencyReceipt{
				SessionID:    payload.SessionID,
				SenderID:     conn.GetUserID(),
				ClientMsgKey: clientMsgKey,
				MsgID:        msgID,
				CreatedAt:    now,
			}
			insert := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "session_id"},
					{Name: "sender_id"},
					{Name: "client_msg_key"},
				},
				DoNothing: true,
			}).Create(&row)
			if insert.Error != nil {
				return insert.Error
			}
			if insert.RowsAffected == 0 {
				var existing model.SendMsgIdempotencyReceipt
				if err := tx.Where(
					"session_id = ? AND sender_id = ? AND client_msg_key = ?",
					payload.SessionID,
					conn.GetUserID(),
					clientMsgKey,
				).First(&existing).Error; err != nil {
					return err
				}
				concurrentReceipt = &persistentSendMsgReceipt{
					MsgID:     existing.MsgID,
					InboxSeq:  existing.InboxSeq,
					CreatedAt: existing.CreatedAt,
				}
				return nil
			}
		}
		// 用于「首条消息自动起标题」：只统计此前的人类文字消息，
		// 跳过卡片/状态/系统消息（如 dispatch_agent 建会话时插入的绑定目录状态卡片），
		// 以及目录绑定指令消息（grix://open/session），
		// 确保标题从真正的第一条文字消息提取，而不是会话里物理意义上的第一条。
		var priorHumanTextCount int64
		if err := tx.Model(&model.Message{}).
			Where("session_id = ? AND is_deleted = false AND sender_type = 1 AND msg_type = 1", payload.SessionID).
			Where("content NOT LIKE ?", "grix://open/session%").
			Limit(1).Count(&priorHumanTextCount).Error; err != nil {
			return err
		}
		isFirstHumanTextMessage = priorHumanTextCount == 0
		msg := model.Message{
			MsgID:           msgID,
			SessionID:       payload.SessionID,
			ThreadID:        payload.ThreadID,
			SenderID:        conn.GetUserID(),
			SenderType:      senderType,
			MsgType:         payload.MsgType,
			Content:         payload.Content,
			Extra:           datatypes.JSON(payload.Extra),
			QuotedMessageID: payload.QuotedMessageID,
			CreatedAt:       now,
		}
		if len(validVisibleTo) > 0 {
			visibleToJSON, _ := json.Marshal(validVisibleTo)
			msg.VisibleTo = datatypes.JSON(visibleToJSON)
		}
		if err := tx.Create(&msg).Error; err != nil {
			return err
		}

		sessionUpdates := map[string]interface{}{
			"last_msg_id": msgID,
			"updated_at":  now,
		}
		if len(validVisibleTo) == 0 && summary != "" {
			sessionUpdates["last_msg_summary"] = summary
		}
		if err := tx.Model(&model.Session{}).Where("session_id = ?", payload.SessionID).
			Updates(sessionUpdates).Error; err != nil {
			return err
		}

		var members []model.SessionMember
		memberQuery := tx.Where("session_id = ? AND member_type = 1 AND member_id != ?", payload.SessionID, conn.GetUserID())
		if len(validVisibleTo) > 0 {
			memberQuery = memberQuery.Where("member_id IN ?", validVisibleTo)
		}
		if err := memberQuery.Find(&members).Error; err != nil {
			return err
		}
		memberIDs := make([]int64, 0, len(members))
		for _, m := range members {
			memberIDs = append(memberIDs, m.MemberID)
		}
		viewingUsers := ResolveSessionViewingUsers(ctx, payload.SessionID, memberIDs)
		inboxUserIDs := make([]int64, 0, len(members)+1)
		inboxUserIDs = append(inboxUserIDs, conn.GetUserID())
		for _, m := range members {
			inboxUserIDs = append(inboxUserIDs, m.MemberID)
		}
		nextSeqByUser, err := inboxseq.AllocateNextBatchTx(ctx, tx, inboxUserIDs)
		if err != nil {
			return fmt.Errorf("allocate inbox_seq batch: %w", err)
		}
		senderInboxSeq = nextSeqByUser[conn.GetUserID()]
		if err := tx.Create(&model.UserInbox{
			UserID:    conn.GetUserID(),
			InboxSeq:  senderInboxSeq,
			MsgID:     msgID,
			SessionID: payload.SessionID,
			EventKind: model.UserInboxEventKindMessage,
		}).Error; err != nil {
			return err
		}
		if usePermanentReceipt {
			result := tx.Model(&model.SendMsgIdempotencyReceipt{}).
				Where(
					"session_id = ? AND sender_id = ? AND client_msg_key = ? AND msg_id = ?",
					payload.SessionID,
					conn.GetUserID(),
					clientMsgKey,
					msgID,
				).
				Update("inbox_seq", senderInboxSeq)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("finalize send_msg idempotency receipt")
			}
		}

		for _, m := range members {
			inboxSeq := nextSeqByUser[m.MemberID]

			inbox := model.UserInbox{
				UserID:    m.MemberID,
				InboxSeq:  inboxSeq,
				MsgID:     msgID,
				SessionID: payload.SessionID,
				EventKind: model.UserInboxEventKindMessage,
			}
			if err := tx.Create(&inbox).Error; err != nil {
				return err
			}
			isViewing := viewingUsers[m.MemberID]
			if isViewing {
				if err := tx.Model(&model.SessionMember{}).
					Where("session_id = ? AND member_id = ? AND member_type = 1", payload.SessionID, m.MemberID).
					Updates(map[string]interface{}{
						"last_active_at":   now,
						"unread_count":     0,
						"last_read_msg_id": gorm.Expr("CASE WHEN last_read_msg_id < ? THEN ? ELSE last_read_msg_id END", msgID, msgID),
					}).Error; err != nil {
					return err
				}
			} else {
				if err := tx.Model(&model.SessionMember{}).
					Where("session_id = ? AND member_id = ? AND member_type = 1", payload.SessionID, m.MemberID).
					Updates(map[string]interface{}{
						"last_active_at": now,
						"unread_count":   gorm.Expr("unread_count + 1"),
					}).Error; err != nil {
					return err
				}
			}

			recipientDeliveries = append(recipientDeliveries, recipientDelivery{
				memberID:              m.MemberID,
				inboxSeq:              inboxSeq,
				shouldIncrementUnread: !isViewing,
			})
		}
		return nil
	}); err != nil {
		logger.L.Errorf("send_msg transactional write failed user=%d session=%s client_msg_id=%s: %v",
			conn.GetUserID(), payload.SessionID, payload.ClientMsgID, err)
		sendMsgNack(conn, pkt, payload.ClientMsgID, 5001, "save message failed")
		return
	}
	if concurrentReceipt != nil {
		if err := store.RDB.Set(
			ctx, dedupKey, concurrentReceipt.MsgID, sendMsgDedupTTL,
		).Err(); err != nil {
			logger.L.Warnf(
				"repair concurrent send dedup key error user=%d session=%s client_msg_id=%s: %v",
				conn.GetUserID(),
				payload.SessionID,
				payload.ClientMsgID,
				err,
			)
		}
		sendExistingSendMsgAck(
			ctx, hub, conn, pkt, payload, senderType,
			skipAutoClearSenderComposing, *concurrentReceipt,
		)
		return
	}

	// Set dedup key after successful commit
	if err := store.RDB.Set(ctx, dedupKey, msgID, sendMsgDedupTTL).Err(); err != nil {
		logger.L.Warnf("set send dedup key error user=%d session=%s client_msg_id=%s: %v",
			conn.GetUserID(), payload.SessionID, payload.ClientMsgID, err)
	}
	if sessionType == 2 && !delegateMeta.IsDelegateOrigin && liveGroupSemantics != nil {
		if err := recordGroupDispatchSemantics(
			ctx,
			payload.SessionID,
			conn.GetUserID(),
			senderType,
			msgID,
			*liveGroupSemantics,
		); err != nil {
			logger.L.Warnf(
				"record group dispatch semantics failed session=%s sender=%d msg_id=%d: %v",
				payload.SessionID,
				conn.GetUserID(),
				msgID,
				err,
			)
		}
	}
	if sessionType == 2 {
		BufferVisibleGroupMessage(
			ctx,
			payload.SessionID,
			conn.GetUserID(),
			senderType,
			msgID,
			validVisibleTo,
		)
	}

	var localInferenceHint *protocol.LocalInferenceHint
	var directRoute *directSessionRoute
	if !delegateMeta.IsDelegateOrigin && senderType != 2 {
		route, err := resolveDirectSessionRoute(
			payload.SessionID,
			sessionType,
			conn.GetUserID(),
			senderType,
			msgID,
			payload.QuotedMessageID,
			payload.MsgType,
			payload.Content,
			payload.Extra,
			liveGroupSemantics,
			validVisibleTo,
			directAgents,
			true,
		)
		if err != nil {
			logger.L.Warnf("resolve direct session route failed session=%s msg_id=%d: %v", payload.SessionID, msgID, err)
		} else {
			directRoute = route
			if route != nil {
				localInferenceHint = route.LocalInference
			}
		}
	}
	if localInferenceHint != nil {
		if err := storePendingLocalInferenceHint(ctx, msgID, localInferenceHint); err != nil {
			logger.L.Warnf(
				"store pending local inference hint failed session=%s msg_id=%d agent=%d: %v",
				payload.SessionID,
				msgID,
				localInferenceHint.AgentID,
				err,
			)
		}
	}

	// Send ACK to sender
	conn.SendPayload(protocol.CmdSendAck, pkt.Seq, protocol.SendAckPayload{
		MsgID:          msgID,
		ClientMsgID:    payload.ClientMsgID,
		InboxSeq:       senderInboxSeq,
		CreatedAt:      now.UnixMilli(),
		LocalInference: localInferenceHint,
	})
	if localInferenceHint != nil {
		clearDirectRouteLocalBuffersOnAccept(ctx, sessionType, payload.SessionID, directRoute)
	}
	if !skipAutoClearSenderComposing {
		clearSenderComposing(ctx, hub, payload.SessionID, conn.GetUserID(), senderType)
	}

	for _, d := range recipientDeliveries {
		// Keep Redis unread in sync with DB-side read/unread decision.
		if d.shouldIncrementUnread {
			if err := store.RDB.HIncrBy(ctx, fmt.Sprintf("im:unread:%d", d.memberID), payload.SessionID, 1).Err(); err != nil {
				logger.L.Warnf("redis unread increment error user=%d session=%s: %v", d.memberID, payload.SessionID, err)
			}
		} else {
			if err := store.RDB.HDel(ctx, fmt.Sprintf("im:unread:%d", d.memberID), payload.SessionID).Err(); err != nil {
				logger.L.Warnf("redis unread clear error user=%d session=%s: %v", d.memberID, payload.SessionID, err)
			}
		}

		pushPayload := protocol.PushMsgPayload{
			InboxSeq:        d.inboxSeq,
			MsgID:           msgID,
			SessionID:       payload.SessionID,
			ThreadID:        payload.ThreadID,
			SessionType:     sessionType,
			SenderID:        conn.GetUserID(),
			SenderType:      senderType,
			MsgType:         payload.MsgType,
			Content:         payload.Content,
			Extra:           payload.Extra,
			QuotedMessageID: payload.QuotedMessageID,
			CreatedAt:       now.UnixMilli(),
			VisibleTo:       payload.VisibleTo,
		}
		broadcastPushMsgToUser(hub, ctx, d.memberID, pushPayload)
	}

	// Push to sender's all online devices (local + cross-node), including current device.
	// This keeps sender-side rendering consistent even when clients choose not to
	// optimistically insert a local temp message (e.g. some forward flows).
	senderPushPayload := protocol.PushMsgPayload{
		InboxSeq:        senderInboxSeq,
		MsgID:           msgID,
		SessionID:       payload.SessionID,
		ThreadID:        payload.ThreadID,
		SessionType:     sessionType,
		SenderID:        conn.GetUserID(),
		SenderType:      senderType,
		MsgType:         payload.MsgType,
		Content:         payload.Content,
		Extra:           payload.Extra,
		QuotedMessageID: payload.QuotedMessageID,
		CreatedAt:       now.UnixMilli(),
		VisibleTo:       payload.VisibleTo,
	}
	broadcastToUser(hub, ctx, conn.GetUserID(), protocol.CmdPushMsg, senderPushPayload)
	// 当人类发送第一条文本消息时，将消息内容截取后写入 custom_title，
	// 并推送给所有成员以实时更新会话列表标题。
	// 群聊建群时已设置标题，不参与首条消息自动起标题。
	// 目录绑定指令消息（grix://open/session）不参与起标题，留给下一条真实文字消息。
	if isFirstHumanTextMessage && sessionType != 2 && senderType == 1 && payload.MsgType == 1 &&
		!isOpenSessionSubmit(payload.Content) {
		if title := apiservice.BuildFallbackTitleFromMessage(payload.Content); title != "" {
			if err := apiservice.SetSessionCustomTitleIfEmpty(payload.SessionID, title); err != nil {
				logger.L.Warnf("first-message set custom_title failed session=%s: %v", payload.SessionID, err)
			}
			if err := apiservice.PushSessionTitleUpdate(payload.SessionID, conn.GetUserID(), title); err != nil {
				logger.L.Warnf("first-message title push failed session=%s: %v", payload.SessionID, err)
			}
		}
	}

	scheduleContentModeration(apiservice.ContentModerationTask{
		SessionID: payload.SessionID,
		MsgID:     msgID,
	})
	if err := apiservice.ReconcileEggInstallChatStatus(payload.SessionID, conn.GetUserID(), senderType, payload.Content); err != nil {
		logger.L.Warnf(
			"reconcile egg install chat status failed session=%s sender=%d msg_id=%d: %v",
			payload.SessionID,
			conn.GetUserID(),
			msgID,
			err,
		)
	}

	if delegateMeta.IsDelegateOrigin {
		// Auto-origin message (delegate/API) counts toward this owner's streak cap.
		store.RDB.Incr(ctx, delegateStreakKey(payload.SessionID, conn.GetUserID()))
	} else {
		// Manual message from owner resets this owner's delegated consecutive streak.
		store.RDB.Del(ctx, delegateStreakKey(payload.SessionID, conn.GetUserID()))
	}
	// Delegate detection: always evaluate other owners' delegates, regardless of sender side.
	checkDelegates(
		hub,
		ctx,
		payload.SessionID,
		conn.GetUserID(),
		senderType,
		sessionType,
		msgID,
		payload.QuotedMessageID,
		payload.MsgType,
		payload.Content,
		payload.Extra,
		delegateMeta,
		liveGroupSemantics,
		validVisibleTo,
		false,
	)

	if !delegateMeta.IsDelegateOrigin && senderType != 2 {
		dispatchDirectSessionRoute(
			hub,
			ctx,
			payload.SessionID,
			sessionType,
			conn.GetUserID(),
			senderType,
			msgID,
			payload.QuotedMessageID,
			payload.MsgType,
			payload.Content,
			payload.Extra,
			directRoute,
		)
	}
	if !delegateMeta.IsDelegateOrigin && senderType == 2 {
		TriggerDirectRouteForMessage(
			hub,
			ctx,
			payload.SessionID,
			conn.GetUserID(),
			senderType,
			msgID,
			payload.QuotedMessageID,
			payload.MsgType,
			payload.Content,
			payload.Extra,
			validVisibleTo,
			liveGroupSemantics,
		)
	}
}

func extractClientMsgIDFromSendMsgPayload(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var partial struct {
		ClientMsgID string `json:"client_msg_id"`
	}
	if err := json.Unmarshal(raw, &partial); err != nil {
		return ""
	}
	return partial.ClientMsgID
}

func clearSenderComposing(
	ctx context.Context,
	hub HubInterface,
	sessionID string,
	senderID int64,
	senderType int16,
) {
	if sessionID == "" || senderID <= 0 {
		return
	}
	if err := ClearSessionActivity(ctx, hub, protocol.SessionActivityPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		ActorID:   senderID,
		ActorType: actorTypeFromSenderType(senderType),
	}); err != nil {
		logger.L.Warnf(
			"clear sender composing failed session=%s sender=%d sender_type=%d: %v",
			sessionID,
			senderID,
			senderType,
			err,
		)
	}
}

// validateVisibleToMembers filters visible_to user IDs to only include valid group members.
func validateVisibleToMembers(sessionID string, visibleTo []int64) ([]int64, error) {
	if len(visibleTo) == 0 {
		return nil, nil
	}
	var memberIDs []int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id IN ?", sessionID, visibleTo).
		Pluck("member_id", &memberIDs).Error; err != nil {
		return nil, err
	}
	return memberIDs, nil
}

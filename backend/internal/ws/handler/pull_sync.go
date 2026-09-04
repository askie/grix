package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type pullSyncRow struct {
	InboxSeq    int64  `gorm:"column:inbox_seq"`
	MsgID       int64  `gorm:"column:msg_id"`
	SessionID   string `gorm:"column:session_id"`
	ThreadID    string `gorm:"column:thread_id"`
	SessionType int16  `gorm:"column:session_type"`
	EventKind   string `gorm:"column:event_kind"`
	SenderID    int64  `gorm:"column:sender_id"`
	SenderType  int16  `gorm:"column:sender_type"`
	MsgType     int16  `gorm:"column:msg_type"`
	Content     string `gorm:"column:content"`
	Extra       []byte `gorm:"column:extra"`
	IsRevoked   bool   `gorm:"column:is_revoked"`
	CreatedAtMs int64  `gorm:"column:created_at_ms"`
}

type unreadSnapshotRow struct {
	SessionID   string `gorm:"column:session_id"`
	UnreadCount int    `gorm:"column:unread_count"`
}

func HandlePullSync(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.PullSyncPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("pull_sync payload error: %v", err)
		return
	}

	userID := conn.GetUserID()
	// 网站访客（web_widget）只拉取给人看的正常消息，agent 的内部过程卡片
	// （工具执行/思考等）不返回，避免轻量访客端漏出 grix://card 的 fallback 文字。
	isWidget := conn.GetPlatform() == WidgetPlatform
	limit := 100
	createdAtExpr := "CAST(EXTRACT(EPOCH FROM m.created_at) * 1000 AS BIGINT)"
	if store.DB != nil && store.DB.Dialector != nil && store.DB.Dialector.Name() == "sqlite" {
		createdAtExpr = "CAST(strftime('%s', m.created_at) AS INTEGER) * 1000"
	}
	selectSQL := fmt.Sprintf(`
				ui.inbox_seq AS inbox_seq,
				m.msg_id AS msg_id,
				m.session_id AS session_id,
				m.thread_id AS thread_id,
				s.session_type AS session_type,
				ui.event_kind AS event_kind,
				m.sender_id AS sender_id,
				m.sender_type AS sender_type,
				m.msg_type AS msg_type,
				m.content AS content,
				m.extra AS extra,
				m.is_revoked AS is_revoked,
				%s AS created_at_ms
			`, createdAtExpr)

	// Query user_inbox + messages in one round-trip (avoid N+1 scans).
	var rows []pullSyncRow
	if err := store.Read().Table("user_inbox ui").
		Select(selectSQL).
		Joins("JOIN messages m ON m.msg_id = ui.msg_id AND m.session_id = ui.session_id").
		Joins("JOIN sessions s ON s.session_id = ui.session_id").
		Joins("LEFT JOIN session_history_resets shr ON shr.session_id = ui.session_id AND shr.user_id = ui.user_id").
		Where("ui.user_id = ? AND ui.inbox_seq > ?", userID, payload.LastInboxSeq).
		Where("shr.deleted_before IS NULL OR m.created_at > shr.deleted_before").
		Order("ui.inbox_seq ASC").
		Limit(limit + 1).
		Scan(&rows).Error; err != nil {
		logger.L.Warnf("pull_sync query error: %v", err)
		return
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	// 私聊消息随身带上会话成员身份，让离线补拉回来的消息在落库那一刻就能定下
	// 会话对端。一次批量查询覆盖整页消息，避免每条消息一次成员查询。
	privateSessionIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.SessionType == model.SessionTypeDirect {
			privateSessionIDs = append(privateSessionIDs, row.SessionID)
		}
	}
	membersBySession := apiservice.PrivateSessionMemberIdentitiesBatch(privateSessionIDs)

	messages := make([]protocol.PushMsgPayload, 0, len(rows))
	for _, row := range rows {
		if isWidget && !row.IsRevoked && shouldHideFromWidget(row.Content, row.Extra) {
			continue // 访客端隐藏 agent 内部过程消息
		}
		// Re-sign at egress to handle messages stored before the creation-time signing
		// was deployed (bare URLs in old rows). New rows already carry signed URLs;
		// signing them again just refreshes the TTL, which is harmless.
		outContent, outExtra := apiservice.SignMessageMedia(row.Content, json.RawMessage(row.Extra))
		p := protocol.PushMsgPayload{
			InboxSeq:    row.InboxSeq,
			MsgID:       row.MsgID,
			SessionID:   row.SessionID,
			ThreadID:    row.ThreadID,
			SessionType: row.SessionType,
			SenderID:    row.SenderID,
			SenderType:  row.SenderType,
			MsgType:     row.MsgType,
			Content:     outContent,
			Extra:       outExtra,
			IsRevoked:   row.IsRevoked,
			CreatedAt:   row.CreatedAtMs,
		}
		if row.SessionType == model.SessionTypeDirect {
			p.SessionMembers = membersBySession[row.SessionID]
		}
		if row.EventKind == model.UserInboxEventKindEdit {
			p.SyncEvent = model.UserInboxEventKindEdit
		}

		if row.IsRevoked {
			p.Content = ""
			p.Extra = nil
			messages = append(messages, p)
			continue
		}

		// Check if this is a streaming AI message
		if row.MsgType == 4 && row.Content == "" {
			builderKey := fmt.Sprintf("ai:builder:%d", row.MsgID)
			cached, err := store.RDB.Get(context.Background(), builderKey).Result()
			if err == nil && cached != "" {
				p.Content = cached
				p.IsStreaming = true
			}
		}

		messages = append(messages, p)
	}

	unreadSnapshot := map[string]int{}

	// Phase 2.3: 在查询 unread_snapshot 之前先抓住当前最大 inbox_seq,
	// 客户端可据此比对 local_max_inbox_seq, 决定是否信任本次快照。
	var snapshotSeq int64
	if err := store.Read().Table("user_inbox").
		Where("user_id = ?", userID).
		Select("COALESCE(MAX(inbox_seq), 0)").
		Scan(&snapshotSeq).Error; err != nil {
		logger.L.Warnf("pull_sync snapshot_seq query error user=%d: %v", userID, err)
	}

	// 未读快照口径必须与会话列表(loadConversationCandidates)一致：
	// 未删除会话 + 群组活跃 + (从未历史重置 OR 重置点之后仍有可见消息)。
	// 共用 store.VisibleAfterCutoffExistsSQL，确保底部角标计入的会话一定能在
	// 会话列表中找到对应行，杜绝「角标有数但列表无行」。删除会话不清未读计数器，
	// 故必须用 cutoff EXISTS 判定而非单看 unread_count。
	existsSQL, existsArgs := store.VisibleAfterCutoffExistsSQL("me.session_id", "shr.deleted_before", "me.joined_at", "s.session_type", userID)
	unreadQuery := store.Read().Table("session_members AS me").
		Select("me.session_id AS session_id, me.unread_count AS unread_count").
		Joins("JOIN sessions AS s ON s.session_id = me.session_id").
		Joins("LEFT JOIN session_history_resets AS shr ON shr.session_id = me.session_id AND shr.user_id = ?", userID).
		Where("me.member_id = ? AND me.member_type = 1 AND me.unread_count > 0", userID).
		Where("s.is_deleted = false AND (s.session_type <> ? OR s.moderation_status = ?)", model.SessionTypeGroup, model.SessionModerationStatusActive).
		Where("shr.session_id IS NULL OR "+existsSQL, existsArgs...)
	var dbUnread []unreadSnapshotRow
	if err := unreadQuery.Scan(&dbUnread).Error; err != nil {
		logger.L.Warnf("pull_sync unread db query error user=%d: %v", userID, err)
	} else {
		for _, row := range dbUnread {
			if row.SessionID == "" || row.UnreadCount <= 0 {
				continue
			}
			unreadSnapshot[row.SessionID] = row.UnreadCount
		}
	}

	// "语音中"全量快照与 unread 同语义：仅末批（hasMore=false）下发，客户端整份覆盖。
	// 中间批返回 nil(JSON null)，客户端据此跳过，避免分页期间重复查询与抖动。
	var activeVoiceCalls []protocol.CallAiDelegatedPayload
	if !hasMore {
		activeVoiceCalls = buildActiveVoiceCallsSnapshot(userID)
	}

	conn.SendPayload(protocol.CmdPullSyncResp, pkt.Seq, protocol.PullSyncRespPayload{
		HasMore:          hasMore,
		Messages:         messages,
		UnreadSnapshot:   unreadSnapshot,
		SnapshotSeq:      snapshotSeq,
		ActiveVoiceCalls: activeVoiceCalls,
	})
}

// buildActiveVoiceCallsSnapshot 取该 owner 当前仍在进行中的 AI 代接通话,
// 作为"语音中"全量快照随 pull_sync 下发。客户端整份覆盖本地徽标集合,
// 漏收的结束/开始通知由此对账自愈。空集合表示该 owner 当前无进行中通话(应清空徽标)。
func buildActiveVoiceCallsSnapshot(userID int64) []protocol.CallAiDelegatedPayload {
	if store.DB == nil {
		return nil
	}
	records, err := store.NewCallRecordStore(store.Read()).
		ListActiveDelegatedByOwner(context.Background(), userID)
	if err != nil {
		logger.L.Warnf("pull_sync active_voice_calls query error user=%d: %v", userID, err)
		return nil
	}
	snapshot := make([]protocol.CallAiDelegatedPayload, 0, len(records))
	for _, rec := range records {
		snapshot = append(snapshot, protocol.CallAiDelegatedPayload{
			CallID:    strconv.FormatInt(rec.ID, 10),
			SessionID: rec.SessionID,
			PeerName:  lookupCallerName(rec.CallerID),
		})
	}
	return snapshot
}

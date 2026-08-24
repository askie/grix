package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type delegateTriggerMeta struct {
	IsDelegateOrigin bool
}

// Phase 3.1: 投递失败通知统一走 agent_delivery_status(status="failed"),
// 不再发送独立的 agent_delivery_error。CmdAgentDeliveryError 与 AgentDeliveryErrorPayload
// 仅作为已废弃的占位保留 1 个版本周期,服务端不再产生该 cmd。
//
// Every final delivery error also becomes a normal chat message. This keeps
// retries visible after reconnects instead of leaving an unread/status-only
// error outside the conversation history.
func notifyAgentDeliveryError(
	hub HubInterface,
	ctx context.Context,
	ownerID int64,
	sessionID string,
	agentID int64,
	triggerMsgID int64,
	scope string,
	code string,
	msg string,
) {
	if hub == nil || ownerID <= 0 || sessionID == "" {
		return
	}

	now := time.Now().UnixMilli()
	broadcastToUser(hub, ctx, ownerID, protocol.CmdAgentDeliveryStatus, protocol.AgentDeliveryStatusPayload{
		SessionID:    sessionID,
		OwnerID:      ownerID,
		AgentID:      agentID,
		TriggerMsgID: triggerMsgID,
		Scope:        scope,
		Status:       protocol.AgentDeliveryStatusFailed,
		Code:         code,
		Msg:          msg,
		UpdatedAt:    now,
	})
	EmitAgentDeliveryFailureMessage(hub, ctx, sessionID, ownerID, agentID, triggerMsgID, scope, code, msg)
}

// TriggerDelegatesForMessage runs delegated-agent detection for an already
// persisted message (used by non-send_msg paths such as stream finish).
func TriggerDelegatesForMessage(
	hub HubInterface,
	ctx context.Context,
	sessionID string,
	senderID int64,
	senderType int16,
	triggerMsgID int64,
	quotedMessageID int64,
	msgType int16,
	content string,
	extraRaw json.RawMessage,
	visibleTo []int64,
) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	return checkDelegates(
		hub,
		ctx,
		sessionID,
		senderID,
		senderType,
		0,
		triggerMsgID,
		quotedMessageID,
		msgType,
		content,
		extraRaw,
		parseDelegateTriggerMeta(extraRaw),
		nil,
		visibleTo,
		false,
	)
}

// TriggerSelfDelegateForMessage 与 TriggerDelegatesForMessage 相同，但放行
// "跳过 sender 本人托管"的常规规则，并使用语音节奏的宽松限流。
// 仅供直拨语音通话的转写触发使用（说话人即托管 owner 本人，架构文档 33）；
// 普通文字消息路径严禁调用，否则用户给自己挂的托管会被自己的消息触发成自答循环。
func TriggerSelfDelegateForMessage(
	hub HubInterface,
	ctx context.Context,
	sessionID string,
	senderID int64,
	senderType int16,
	triggerMsgID int64,
	quotedMessageID int64,
	msgType int16,
	content string,
	extraRaw json.RawMessage,
	visibleTo []int64,
) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	return checkDelegates(
		hub,
		ctx,
		sessionID,
		senderID,
		senderType,
		0,
		triggerMsgID,
		quotedMessageID,
		msgType,
		content,
		extraRaw,
		parseDelegateTriggerMeta(extraRaw),
		nil,
		visibleTo,
		true,
	)
}

// checkDelegates scans session human members for active delegation,
// and publishes delegate_request to NATS for each delegated agent.
// allowSelf 放行 sender 本人的托管并使用语音节奏限流（仅直拨语音转写路径传 true）。
func checkDelegates(
	hub HubInterface,
	ctx context.Context,
	sessionID string,
	senderID int64,
	senderType int16,
	sessionType int16,
	triggerMsgID int64,
	quotedMessageID int64,
	msgType int16,
	content string,
	extraRaw json.RawMessage,
	meta delegateTriggerMeta,
	semantics *groupDispatchSemantics,
	visibleTo []int64,
	allowSelf bool,
) bool {
	if sessionType <= 0 { // 未由调用方预加载时回退(走进程内缓存)
		sessionType = loadSessionType(sessionID)
	}
	var mentionUserIDs []int64
	var explicitMentionUserIDs []int64
	var targetUserIDs []int64
	if sessionType == 2 {
		if semantics == nil {
			resolved, resolveErr := resolvePersistedGroupDispatchSemantics(
				ctx,
				sessionID,
				senderID,
				senderType,
				triggerMsgID,
				quotedMessageID,
				content,
				extraRaw,
			)
			if resolveErr != nil {
				logger.L.Warnf(
					"resolve persisted group dispatch semantics failed session=%s sender=%d trigger_msg_id=%d: %v",
					sessionID,
					senderID,
					triggerMsgID,
					resolveErr,
				)
			} else {
				semantics = &resolved
			}
		}
		if semantics != nil {
			mentionUserIDs = append([]int64(nil), semantics.MentionUserIDs...)
			explicitMentionUserIDs = append([]int64(nil), semantics.ExplicitMentionUserIDs...)
			targetUserIDs = append([]int64(nil), semantics.TargetUserIDs...)
		}
	}
	logger.L.Debugf(
		"[DelegateTrace] check_delegates start session=%s session_type=%d sender=%d sender_type=%d trigger_msg_id=%d delegate_origin=%t mention_ids=%v target_ids=%v content_len=%d",
		sessionID,
		sessionType,
		senderID,
		senderType,
		triggerMsgID,
		meta.IsDelegateOrigin,
		mentionUserIDs,
		targetUserIDs,
		len(content),
	)
	eventCreatedAt := time.Now().UnixMilli()
	dispatched := false
	coldStartDispatch := sessionType == 2 && semantics != nil && semantics.ColdStart && senderType == 1 && len(targetUserIDs) == 0
	trigger := agentreceive.MessageTrigger{
		SessionID:       sessionID,
		SessionType:     sessionType,
		MsgID:           triggerMsgID,
		SenderID:        senderID,
		SenderType:      senderType,
		MsgType:         msgType,
		Content:         content,
		ExtraRaw:        extraRaw,
		QuotedMessageID: quotedMessageID,
		MentionUserIDs:  mentionUserIDs,
		CreatedAt:       time.Now().UTC(),
	}

	agentCache := make(map[string]model.Agent)
	coldStartDirectAgentIDs := make(map[int64]struct{})
	if coldStartDispatch {
		directAgents, loadErr := loadDirectSessionAgents(sessionID)
		if loadErr != nil {
			logger.L.Warnf("load direct session agents for cold start failed session=%s: %v", sessionID, loadErr)
		} else {
			for _, row := range directAgents {
				if row.ID <= 0 {
					continue
				}
				coldStartDirectAgentIDs[row.ID] = struct{}{}
			}
		}
	}

	var members []model.SessionMember
	store.DB.Where("session_id = ? AND member_type = 1", sessionID).Find(&members)

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

	// 私聊投递降级：会话内无人正在查看时，让 agent 走一次性整段回复而非逐字流式，
	// 省掉离线/未打开会话场景下大量分片的传输与中转处理。完整消息仍照常落库 + 离线推送。
	// 判定口径为"会话内任一真人成员正在查看"，覆盖 AI 替主人应答对端的托管私聊。
	deliverSingleMessage := false
	if sessionType == 1 && len(members) > 0 {
		viewerIDs := make([]int64, 0, len(members))
		for _, m := range members {
			viewerIDs = append(viewerIDs, m.MemberID)
		}
		deliverSingleMessage = len(ResolveSessionViewingUsers(ctx, sessionID, viewerIDs)) == 0
	}

	// For group approval resolutions, pre-compute which agent issued the card so
	// we can bypass receive-mode filtering exactly for that agent. Without this,
	// agents in mention-only mode never see the resolution and the card stays loading.
	var approvalResolutionIssuerAgentID int64
	if sessionType == 2 {
		if requestID := extractApprovalResolutionCommandID(content); requestID != "" {
			approvalResolutionIssuerAgentID = wsagentapi.LoadApprovalIssuer(context.Background(), sessionID, requestID)
		}
	}

	for _, m := range members {
		if m.MemberID == senderID && !allowSelf {
			logger.L.Debugf("[DelegateTrace] check_delegates skip self session=%s owner=%d sender=%d", sessionID, m.MemberID, senderID)
			continue
		}
		ownerMentioned := sessionType == 2 && containsInt64(explicitMentionUserIDs, m.MemberID)
		ownerTargeted := sessionType == 2 && containsInt64(targetUserIDs, m.MemberID)
		continuedMentionAll := sessionType == 2 && semantics != nil && semantics.ContinuedMentionAll && ownerTargeted
		publicTriggered := ownerTargeted || coldStartDispatch
		delegateKey := fmt.Sprintf("im:delegate:%s:%d", sessionID, m.MemberID)
		delegateFields, err := store.RDB.HMGet(ctx, delegateKey, "agent_id", "max_consecutive_replies").Result()
		if err != nil {
			logger.L.Debugf("[DebugDelegate] no delegation active session=%s owner=%d err=%v", sessionID, m.MemberID, err)
			continue
		}
		if len(delegateFields) < 1 || delegateFields[0] == nil {
			logger.L.Debugf("[DebugDelegate] delegation fields empty session=%s owner=%d fields=%v", sessionID, m.MemberID, delegateFields)
			continue
		}
		agentIDStr := fmt.Sprint(delegateFields[0])
		if agentIDStr == "" || agentIDStr == "<nil>" {
			logger.L.Debugf("[DebugDelegate] agent_id empty session=%s owner=%d agent_id=%v", sessionID, m.MemberID, agentIDStr)
			continue
		}
		logger.L.Debugf(
			"[DelegateTrace] check_delegates candidate session=%s owner=%d agent=%s",
			sessionID,
			m.MemberID,
			agentIDStr,
		)

		maxConsecutive := defaultDelegateMaxConsecutiveReplies
		if len(delegateFields) >= 2 {
			maxConsecutive = delegateMaxRepliesFromRedis(delegateFields[1])
		}

		streakKey := delegateStreakKey(sessionID, m.MemberID)
		if !meta.IsDelegateOrigin {
			store.RDB.Del(ctx, streakKey)
		}
		if streak, err := store.RDB.Get(ctx, streakKey).Int(); err == nil && streak >= maxConsecutive {
			logger.L.Debugf("[DebugDelegate] max consecutive streak reached session=%s owner=%d streak=%d", sessionID, m.MemberID, streak)
			continue
		}
		parsedAgentID, parsedAgentErr := strconv.ParseInt(agentIDStr, 10, 64)
		if parsedAgentErr == nil && parsedAgentID > 0 && senderType == 2 && parsedAgentID == senderID {
			logger.L.Debugf("[DebugDelegate] skip self-bounce session=%s owner=%d agent=%d", sessionID, m.MemberID, parsedAgentID)
			continue
		}
		decision, decisionErr := agentreceive.Evaluate(
			ctx,
			agentreceive.Policy{
				SessionID:    sessionID,
				MemberID:     m.MemberID,
				MemberType:   1,
				Mode:         m.AgentReceiveMode,
				BacklogCount: m.AgentReceiveBacklogCount,
			},
			trigger,
			publicTriggered,
			ownerMentioned || continuedMentionAll,
		)
		if decisionErr != nil {
			logger.L.Warnf(
				"delegate receive policy failed session=%s owner=%d trigger_msg_id=%d: %v",
				sessionID,
				m.MemberID,
				triggerMsgID,
				decisionErr,
			)
		}
		dispatchToOwner := decision.Dispatch
		if sessionType == 2 && len(targetUserIDs) > 0 && !ownerTargeted {
			logger.L.Debugf(
				"[DelegateTrace] check_delegates skip non-targeted delegate session=%s owner=%d target_ids=%v",
				sessionID,
				m.MemberID,
				targetUserIDs,
			)
			continue
		}

		var cached model.Agent
		if parsedAgentErr == nil && parsedAgentID > 0 {
			var ok bool
			cached, ok = agentCache[agentIDStr]
			if !ok {
				var agent model.Agent
				if err := store.DB.Select("id,owner_id,provider_type,agent_client_type,status").First(&agent, parsedAgentID).Error; err == nil {
					cached = agent
				}
				agentCache[agentIDStr] = cached
			}
		}
		if coldStartDispatch && cached.ID > 0 {
			if _, exists := coldStartDirectAgentIDs[cached.ID]; exists {
				dispatchToOwner = false
			}
		}

		// 托管不走访问门禁：托管由主人在本会话内主动开启（delegate_start 已校验 agent 归属主人、
		// 主人是会话成员），托管态下被驱动的必然是主人自己的 agent，不存在门禁要防的越权驱动；
		// 访客是在跟主人说话，不是在私聊 agent。门禁只管人直接找 agent 的路径（direct_session_route）。
		if parsedAgentErr == nil && cached.ID > 0 && cached.Status != 3 && cached.ProviderType == model.AgentProviderAPI {
			if dispatchToOwner {
				enforceRateLimit := !(sessionType == 2 && ownerTargeted && !meta.IsDelegateOrigin)
				if enforceRateLimit {
					rateLimitKey := fmt.Sprintf("im:delegate:rate:human:%s:%d", sessionID, m.MemberID)
					rateTTL := delegateHumanRateTTL
					if allowSelf {
						// 语音转写节奏：ASR debounce 已聚合句子，相邻两段表达可短于常规阈值
						rateTTL = delegateVoiceRateTTL
					}
					if meta.IsDelegateOrigin {
						rateLimitKey = fmt.Sprintf("im:delegate:rate:auto:%s:%d", sessionID, m.MemberID)
						rateTTL = delegateAutoRateTTL
					}
					if ok, _ := store.RDB.SetNX(ctx, rateLimitKey, 1, rateTTL).Result(); !ok {
						logger.L.Debugf("[DebugDelegate] rate limited session=%s owner=%d ratelimit_key=%s", sessionID, m.MemberID, rateLimitKey)
						dispatchToOwner = false
					}
				}
			}

			var contextMessages []protocol.ContextMessagePayload
			if dispatchToOwner {
				contextMessages = signDispatchContextMessages(buildDispatchContextMessages(
					ctx,
					sessionType,
					sessionID,
					1,
					m.MemberID,
					m.MemberID,
					m.AgentReceiveBacklogCount,
					quotedMessageID,
					m.AgentReceiveMode,
					decision.ContextMessages,
				))
			}

			mirrorMode := wsagentapi.MirrorModeRecordOnly
			if dispatchToOwner {
				mirrorMode = wsagentapi.MirrorModeRecordAndProcess
			}
			// Approval resolution directives must bypass normal receive-mode filtering
			// and reach the agent that issued the card, regardless of mention mode.
			isApprovalForThisAgent := approvalResolutionIssuerAgentID > 0 && approvalResolutionIssuerAgentID == cached.ID
			if isApprovalForThisAgent && mirrorMode == wsagentapi.MirrorModeRecordOnly {
				mirrorMode = wsagentapi.MirrorModeRecordAndProcess
			}

			eventType := delegateEventType(sessionType, ownerMentioned)
			receiveMode, _ := agentreceive.Normalize(m.AgentReceiveMode, m.AgentReceiveBacklogCount)
			if sessionType == 2 &&
				mirrorMode == wsagentapi.MirrorModeRecordOnly &&
				receiveMode == agentreceive.ModeMentionOnly {
				continue
			}

			// Proprietary agent filtering for group sessions.
			// 注：这是「人类成员托管(delegate)某 agent 代答」的路径，与会话成员 agent 的
			// 直投路径(direct_session_route)不同；它的 @-only 拦截基于人类成员而非 agent
			// 成员，私聊转群的 ModeAll 豁免不作用于此。常规 1:1 owner↔agent 私聊转群不经过
			// 这条路径；「托管了 proprietary agent 的私聊转群后仍有问必答」不在本次改动范围。
			agentClientType := wsagentapi.GetAgentClientType(cached.ID)
			if agentClientType == "" {
				agentClientType = cached.AgentClientType
			}
			if model.IsProprietaryAgentClientType(agentClientType) && sessionType == 2 {
				if mirrorMode == wsagentapi.MirrorModeRecordOnly {
					continue
				}
				if eventType != "group_mention" && !isApprovalForThisAgent {
					continue
				}
			}

			event := buildDelegateAPIEvent(
				mirrorMode,
				sessionID,
				sessionType,
				m.MemberID,
				cached.ID,
				senderID,
				triggerMsgID,
				quotedMessageID,
				msgType,
				content,
				extraRaw,
				mentionUserIDs,
				contextMessages,
				eventCreatedAt,
			)
			event.EventType = eventType
			if deliverSingleMessage {
				event.Extra = applyConnectorResponseDelivery(event.Extra, connectorResponseDeliverySingle)
			}
			// 托管代答私聊（含 widget 客服）：agent 代 owner 回复对端，过程文本/工具卡/
			// 思考及 grix_reply 之后的续写都不投递给对端，只保留正式应答。群聊
			// (session_type=2) 已在 event_router 出站层统一抑制，此处补齐私聊托管一侧。
			if sessionType == 1 {
				event.Extra = applyConnectorManagedDelegateHints(event.Extra)
			}
			// 注意:即便 IsAgentChannelAvailable=false, PushDelegateEvent 仍会把事件
			// 持久化到 retry 队列, 等 agent 上线后重投。这种情况下不应当对用户报"投递失败"。
			// 只有 PushDelegateEvent 真返回 false（连队列都失败）才算真失败。
			logger.L.Debugf("[DebugDelegate] pushing to Agent API WS... session=%s owner=%d agent=%d mirror_mode=%s", sessionID, m.MemberID, cached.ID, mirrorMode)
			if ok := wsagentapi.PushDelegateEvent(event); !ok {
				if dispatchToOwner {
					logger.L.Warnf("agent api event dropped session=%s owner=%d agent=%d", sessionID, m.MemberID, cached.ID)
					notifyAgentDeliveryError(
						hub,
						ctx,
						m.MemberID,
						sessionID,
						cached.ID,
						triggerMsgID,
						protocol.AgentDeliveryScopeDelegate,
						protocol.AgentDeliveryCodeChannelUnavailable,
						"delegated agent channel unavailable",
					)
				}
			} else if dispatchToOwner {
				clearDelegateReceiveBufferOnAccept(ctx, sessionID, m.MemberID, decision)
				dispatched = true
				logger.L.Infof(
					"[DelegateTrace] delegate dispatched via agent_api session=%s owner=%d agent=%d trigger_msg_id=%d event_type=%s",
					sessionID,
					m.MemberID,
					cached.ID,
					triggerMsgID,
					eventType,
				)
			}
			continue
		}
		logger.L.Debugf("[DebugDelegate] cached agent invalid or not api provider session=%s owner=%d agentIDStr=%s cached=%+v", sessionID, m.MemberID, agentIDStr, cached)

		if !dispatchToOwner {
			continue
		}

		enforceRateLimit := !(sessionType == 2 && ownerTargeted && !meta.IsDelegateOrigin)
		if enforceRateLimit {
			rateLimitKey := fmt.Sprintf("im:delegate:rate:human:%s:%d", sessionID, m.MemberID)
			rateTTL := delegateHumanRateTTL
			if allowSelf {
				rateTTL = delegateVoiceRateTTL
			}
			if meta.IsDelegateOrigin {
				rateLimitKey = fmt.Sprintf("im:delegate:rate:auto:%s:%d", sessionID, m.MemberID)
				rateTTL = delegateAutoRateTTL
			}
			if ok, _ := store.RDB.SetNX(ctx, rateLimitKey, 1, rateTTL).Result(); !ok {
				logger.L.Debugf("[DebugDelegate] rate limited session=%s owner=%d ratelimit_key=%s", sessionID, m.MemberID, rateLimitKey)
				continue
			}
		}

		delegateReq := map[string]interface{}{
			"cmd":            "delegate_request",
			"session_id":     sessionID,
			"sender_id":      senderID,
			"owner_id":       m.MemberID,
			"agent_id_str":   agentIDStr,
			"content":        content,
			"trigger_msg_id": triggerMsgID,
			"context_messages": signDispatchContextMessages(buildDispatchContextMessages(
				ctx,
				sessionType,
				sessionID,
				1,
				m.MemberID,
				m.MemberID,
				m.AgentReceiveBacklogCount,
				quotedMessageID,
				m.AgentReceiveMode,
				decision.ContextMessages,
			)),
		}
		data, _ := json.Marshal(delegateReq)
		if store.JS != nil {
			if _, err := store.JS.Publish(fmt.Sprintf("ai.request.%s", sessionID), data); err != nil {
				logger.L.Warnf("delegate request publish failed session=%s owner=%d: %v", sessionID, m.MemberID, err)
				continue
			}
			clearDelegateReceiveBufferOnAccept(ctx, sessionID, m.MemberID, decision)
			logger.L.Infof(
				"[DelegateTrace] delegate dispatched via nats session=%s owner=%d agent=%s trigger_msg_id=%d",
				sessionID,
				m.MemberID,
				agentIDStr,
				triggerMsgID,
			)
			dispatched = true
			continue
		}
		logger.L.Warnf(
			"[DelegateTrace] delegate dispatch skipped session=%s owner=%d reason=nats_not_configured",
			sessionID,
			m.MemberID,
		)
	}
	return dispatched
}

func clearDelegateReceiveBufferOnAccept(
	ctx context.Context,
	sessionID string,
	ownerID int64,
	decision agentreceive.Decision,
) {
	if !decision.ClearBufferOnAccept {
		return
	}
	clearBufferedGroupDispatchContext(ctx, 2, sessionID, 1, ownerID)
}

func delegateEventType(sessionType int16, ownerMentioned bool) string {
	if sessionType != 2 {
		return "user_chat"
	}
	if ownerMentioned {
		return "group_mention"
	}
	return "group_message"
}

// connectorResponseDeliverySingle 指示 connector 用一次性整段消息回复，而非逐字流式。
// 与群聊运行时配置（event_router.applyGroupConnectorRuntimeConfig）共用同一协议字段
// extra.connector.response_delivery，connector 侧无需区分私聊/群聊来源。
const connectorResponseDeliverySingle = "single_message"

// applyConnectorResponseDelivery 在 extra.connector 下写入 response_delivery，保留 extra
// 其余字段原始字节（顶层与 connector 同级字段不做反序列化，避免大整数精度损失）。
func applyConnectorResponseDelivery(extra json.RawMessage, mode string) json.RawMessage {
	fields := map[string]json.RawMessage{}
	if len(extra) > 0 {
		_ = json.Unmarshal(extra, &fields)
	}

	connector := map[string]json.RawMessage{}
	if raw, ok := fields["connector"]; ok && len(raw) > 0 {
		_ = json.Unmarshal(raw, &connector)
	}
	modeRaw, err := json.Marshal(mode)
	if err != nil {
		return extra
	}
	connector["response_delivery"] = modeRaw

	connRaw, err := json.Marshal(connector)
	if err != nil {
		return extra
	}
	fields["connector"] = connRaw

	merged, err := json.Marshal(fields)
	if err != nil {
		return extra
	}
	return merged
}

// applyConnectorManagedDelegateHints 为「托管代答」私聊下发运行时抑制指令：agent 是
// 代 owner 回复对端（含 widget 客服场景），它的工具卡、思考、以及非最终应答的纯文本
// 过程/续写都不应投递给对端，只保留 grix_reply 的正式应答。与群聊
// event_router.applyGroupConnectorRuntimeConfig 共用同一 extra.connector 协议字段，
// 语义上多一个 text_events=drop 用于挡 hermes send() 里非 final-reply 的白话文本。
// 保留 extra 其余字段原始字节，避免大整数精度损失。
func applyConnectorManagedDelegateHints(extra json.RawMessage) json.RawMessage {
	fields := map[string]json.RawMessage{}
	if len(extra) > 0 {
		_ = json.Unmarshal(extra, &fields)
	}

	connector := map[string]json.RawMessage{}
	if raw, ok := fields["connector"]; ok && len(raw) > 0 {
		_ = json.Unmarshal(raw, &connector)
	}
	for _, key := range []string{"tool_events", "thinking_events", "text_events"} {
		connector[key] = json.RawMessage(`"drop"`)
	}

	connRaw, err := json.Marshal(connector)
	if err != nil {
		return extra
	}
	fields["connector"] = connRaw

	merged, err := json.Marshal(fields)
	if err != nil {
		return extra
	}
	return merged
}

func buildDelegateAPIEvent(
	mirrorMode string,
	sessionID string,
	sessionType int16,
	ownerID int64,
	agentID int64,
	senderID int64,
	triggerMsgID int64,
	quotedMessageID int64,
	msgType int16,
	content string,
	extraRaw json.RawMessage,
	mentionUserIDs []int64,
	contextMessages []protocol.ContextMessagePayload,
	eventCreatedAt int64,
) wsagentapi.DelegateEventPayload {
	eventID := fmt.Sprintf("%s:%d:%d", sessionID, ownerID, triggerMsgID)
	recordOnly := strings.EqualFold(strings.TrimSpace(mirrorMode), wsagentapi.MirrorModeRecordOnly)
	if recordOnly {
		eventID += ":mirror"
		contextMessages = nil
	}
	event := wsagentapi.DelegateEventPayload{
		EventID:         eventID,
		MirrorMode:      mirrorMode,
		AgentID:         agentID,
		OwnerID:         ownerID,
		SessionID:       sessionID,
		SessionType:     sessionType,
		MsgID:           triggerMsgID,
		QuotedMessageID: quotedMessageID,
		SenderID:        senderID,
		Content:         content,
		MentionUserIDs:  protocol.StringInt64s(mentionUserIDs),
		ContextMessages: contextMessages,
		CreatedAt:       eventCreatedAt,
	}
	wsagentapi.ApplyStructuredMessagePayload(&event, msgType, extraRaw)
	return event
}

// extractApprovalResolutionCommandID extracts the approval_command_id (or
// approval_id as fallback) from an [[exec-approval-resolution|...]] directive.
// Returns empty string if the content is not such a directive.
func extractApprovalResolutionCommandID(content string) string {
	const prefix = "[[exec-approval-resolution|"
	s := strings.TrimSpace(content)
	if !strings.HasPrefix(s, prefix) || !strings.HasSuffix(s, "]]") {
		return ""
	}
	inner := s[len(prefix) : len(s)-2]
	var approvalCommandID, approvalID string
	for _, seg := range strings.Split(inner, "|") {
		kv := strings.SplitN(seg, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "approval_command_id":
			approvalCommandID = strings.TrimSpace(kv[1])
		case "approval_id":
			approvalID = strings.TrimSpace(kv[1])
		}
	}
	if approvalCommandID != "" {
		return approvalCommandID
	}
	return approvalID
}

// signDispatchContextMessages signs media URLs in context message content at
// egress. Rows written before creation-time signing may carry bare URLs; rows
// written after just get their TTL refreshed, which is harmless.
func signDispatchContextMessages(msgs []protocol.ContextMessagePayload) []protocol.ContextMessagePayload {
	for i := range msgs {
		msgs[i].Content, _ = apiservice.SignMessageMedia(msgs[i].Content, nil)
	}
	return msgs
}

func parseDelegateTriggerMeta(extraRaw json.RawMessage) delegateTriggerMeta {
	meta := delegateTriggerMeta{}
	if len(extraRaw) == 0 {
		return meta
	}

	var extra map[string]interface{}
	if err := json.Unmarshal(extraRaw, &extra); err != nil {
		return meta
	}

	if v, ok := extra["delegate_origin"]; ok {
		if b, ok := v.(bool); ok && b {
			meta.IsDelegateOrigin = true
		}
	}
	return meta
}

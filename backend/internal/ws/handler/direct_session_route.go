package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type directSessionRoute struct {
	Targets        []directDispatchTarget
	MirrorTargets  []directDispatchTarget
	MentionUserIDs []int64
	LocalInference *protocol.LocalInferenceHint
}

type directDispatchTarget struct {
	Agent               model.Agent
	BufferMemberType    int16
	BufferMemberID      int64
	ViewerUserID        int64
	BacklogCount        int
	Mentioned           bool
	ClearBufferOnAccept bool
	ContextMessages     []protocol.ContextMessagePayload
}

type directSessionAgentRow struct {
	ID                       int64  `gorm:"column:id"`
	OwnerID                  int64  `gorm:"column:owner_id"`
	ProviderType             int16  `gorm:"column:provider_type"`
	AgentClientType          string `gorm:"column:agent_client_type"`
	LocalEndpoint            string `gorm:"column:local_endpoint"`
	LocalModelName           string `gorm:"column:local_model_name"`
	SystemPrompt             string `gorm:"column:system_prompt"`
	Status                   int16  `gorm:"column:status"`
	AgentReceiveMode         int16  `gorm:"column:agent_receive_mode"`
	AgentReceiveBacklogCount int    `gorm:"column:agent_receive_backlog_count"`
}

func resolveDirectSessionRoute(
	sessionID string,
	sessionType int16,
	senderID int64,
	senderType int16,
	triggerMsgID int64,
	quotedMessageID int64,
	msgType int16,
	content string,
	extraRaw json.RawMessage,
	semantics *groupDispatchSemantics,
	visibleTo []int64,
	preloadedAgents []directSessionAgentRow,
	usePreloaded bool,
) (*directSessionRoute, error) {
	var agents []directSessionAgentRow
	var err error
	if usePreloaded {
		agents = preloadedAgents
	} else {
		agents, err = loadDirectSessionAgents(sessionID)
	}
	if err != nil || len(agents) == 0 {
		return nil, err
	}

	// When visibleTo is set, restrict agents to those whose owner or own ID is visible.
	if len(visibleTo) > 0 {
		allowed := make(map[int64]struct{}, len(visibleTo))
		for _, id := range visibleTo {
			allowed[id] = struct{}{}
		}
		filtered := make([]directSessionAgentRow, 0, len(agents))
		for _, row := range agents {
			if _, ok := allowed[row.OwnerID]; ok {
				filtered = append(filtered, row)
			} else if _, ok := allowed[row.ID]; ok {
				filtered = append(filtered, row)
			}
		}
		agents = filtered
		if len(agents) == 0 {
			return nil, nil
		}
	}

	var mentionUserIDs []int64
	var explicitMentionUserIDs []int64
	var targetUserIDs []int64
	if sessionType == 2 {
		if semantics == nil {
			resolved, resolveErr := resolvePersistedGroupDispatchSemantics(
				context.Background(),
				sessionID,
				senderID,
				senderType,
				triggerMsgID,
				quotedMessageID,
				content,
				extraRaw,
			)
			if resolveErr != nil {
				return nil, resolveErr
			}
			semantics = &resolved
		}
		mentionUserIDs = append([]int64(nil), semantics.MentionUserIDs...)
		explicitMentionUserIDs = append([]int64(nil), semantics.ExplicitMentionUserIDs...)
		targetUserIDs = append([]int64(nil), semantics.TargetUserIDs...)
	}
	coldStartDispatch := sessionType == 2 && senderType == 1 && semantics != nil && semantics.ColdStart && len(targetUserIDs) == 0
	// A directed single-agent continuation (a human replying right after a
	// specific agent spoke, or carrying a 1:1 thread with it) counts as
	// addressing that agent — even proprietary agents that otherwise require an
	// explicit @mention in groups. Broadcast carry-overs (@all continuation) are
	// excluded so proprietary agents are never pulled in by group-wide cues.
	directedContinuation := sessionType == 2 && senderType == 1 && semantics != nil &&
		semantics.Continued && !semantics.ContinuedMentionAll && !semantics.ExplicitMentionAll &&
		len(targetUserIDs) == 1
	// 审批回传必须抵达发出卡片的那个 agent：卡片按钮生成的是一条普通群消息，
	// 若被 @-only / 接收模式过滤挡下，agent 永远等不到结果，卡片停在「提交中」。
	// 托管路径（send_msg_delegate）已有同样的豁免，这里是会话成员 agent 的直投路径。
	var approvalIssuerAgentID int64
	if sessionType == 2 && senderType == 1 {
		if requestID := extractApprovalResolutionCommandID(content); requestID != "" {
			approvalIssuerAgentID = wsagentapi.LoadApprovalIssuer(context.Background(), sessionID, requestID)
		}
	}

	targets := ensureApprovalIssuerTarget(
		selectDirectSessionTargets(sessionType, targetUserIDs, agents),
		agents,
		approvalIssuerAgentID,
	)
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
	route := &directSessionRoute{MentionUserIDs: mentionUserIDs}
	// Agent-origin group replies stay mirror-only unless they explicitly point
	// at another direct target.
	allowProcessingTargets := senderType != 2 || (sessionType == 2 && (len(explicitMentionUserIDs) > 0 || len(targetUserIDs) > 0))
	if allowProcessingTargets {
		for _, row := range targets {
			if senderType == 2 && row.ID == senderID {
				continue
			}
			targeted := sessionType == 2 && containsInt64(targetUserIDs, row.ID)
			mentioned := sessionType == 2 && containsInt64(explicitMentionUserIDs, row.ID)
			isApprovalIssuer := approvalIssuerAgentID > 0 && row.ID == approvalIssuerAgentID

			// Proprietary agents only receive explicit @mention events in groups,
			// plus a directed single-agent continuation aimed at this agent.
			agentClientType := wsagentapi.GetAgentClientType(row.ID)
			if agentClientType == "" {
				agentClientType = row.AgentClientType
			}
			// 私聊转群后，原 agent 被置为 ModeAll（群内有问必答），放行其对自有 agent 的 @-only 限制。
			// 新群第一条消息也是公共冷启动触发，正常模式的 proprietary agent 必须能收到初始任务。
			if model.IsProprietaryAgentClientType(agentClientType) && sessionType == 2 &&
				row.AgentReceiveMode != agentreceive.ModeAll {
				if !mentioned && !isApprovalIssuer && !coldStartDispatch && !(directedContinuation && targeted) {
					continue
				}
			}
			continuedMentionAll := sessionType == 2 && semantics != nil && semantics.ContinuedMentionAll && targeted
			publicTriggered := targeted || coldStartDispatch || isApprovalIssuer
			// A directed single-agent continuation — a human replying right after
			// this specific agent spoke — counts as addressing the agent, for every
			// client type. ModeMentionOnly exists to stop an agent from answering
			// unrelated group chatter, not to ignore someone picking up its own
			// last utterance, so the continuation signal passes the receive-mode
			// policy below the same way an explicit @mention does.
			decision, decisionErr := agentreceive.Evaluate(
				context.Background(),
				agentreceive.Policy{
					SessionID:    sessionID,
					MemberID:     row.ID,
					MemberType:   2,
					Mode:         row.AgentReceiveMode,
					BacklogCount: row.AgentReceiveBacklogCount,
				},
				trigger,
				publicTriggered,
				mentioned || continuedMentionAll || (directedContinuation && targeted) || isApprovalIssuer,
			)
			if decisionErr != nil {
				logger.L.Warnf(
					"direct route receive policy failed session=%s agent=%d msg_id=%d: %v",
					sessionID,
					row.ID,
					triggerMsgID,
					decisionErr,
				)
			}
			if !decision.Dispatch {
				logger.L.Infof(
					"direct route receive policy declined session=%s agent=%d msg_id=%d mode=%d targeted=%v mentioned=%v continued=%v",
					sessionID,
					row.ID,
					triggerMsgID,
					row.AgentReceiveMode,
					targeted,
					mentioned,
					directedContinuation,
				)
				continue
			}
			// 纯靠审批豁免进来的事件不是「agent 被点名」：它在 PushDelegateEvent 里
			// 会被审批拦截器消费成 local_action，agent 根本看不到 context_messages。
			// 若照常挂上下文并清缓冲，agent 攒的群聊未读会被静默丢掉一批。
			// 只让它穿过门禁抵达拦截器，不消费 backlog。
			approvalOnly := isApprovalIssuer && !mentioned && !targeted && !coldStartDispatch
			var contextMessages []protocol.ContextMessagePayload
			clearBufferOnAccept := decision.ClearBufferOnAccept
			if approvalOnly {
				clearBufferOnAccept = false
			} else {
				contextMessages = buildDispatchContextMessages(
					context.Background(),
					sessionType,
					sessionID,
					2,
					row.ID,
					row.OwnerID,
					row.AgentReceiveBacklogCount,
					quotedMessageID,
					row.AgentReceiveMode,
					decision.ContextMessages,
				)
			}
			route.Targets = append(route.Targets, directDispatchTarget{
				Agent:               buildDirectRouteAgent(row),
				BufferMemberType:    2,
				BufferMemberID:      row.ID,
				ViewerUserID:        row.OwnerID,
				BacklogCount:        row.AgentReceiveBacklogCount,
				Mentioned:           mentioned,
				ClearBufferOnAccept: clearBufferOnAccept,
				ContextMessages:     contextMessages,
			})
		}
	}
	if sessionType == 2 {
		route.MirrorTargets = buildDirectSessionMirrorTargets(
			agents,
			route.Targets,
			targetUserIDs,
			mentionUserIDs,
			senderID,
			senderType,
			visibleTo,
		)
	}
	if len(route.Targets) == 0 && len(route.MirrorTargets) == 0 {
		return nil, nil
	}

	route.LocalInference = buildDirectLocalInferenceHint(sessionID, triggerMsgID, route.Targets)
	return route, nil
}

func loadDirectSessionAgents(sessionID string) ([]directSessionAgentRow, error) {
	if sessionID == "" {
		return nil, nil
	}

	var rows []directSessionAgentRow
	if err := store.DB.Table("session_members AS sm").
		Select("a.id, a.owner_id, a.provider_type, a.agent_client_type, a.local_endpoint, a.local_model_name, a.system_prompt, a.status, sm.agent_receive_mode, sm.agent_receive_backlog_count").
		Joins("JOIN agents a ON a.id = sm.member_id").
		Where("sm.session_id = ? AND sm.member_type = 2", sessionID).
		Order("sm.joined_at ASC, sm.member_id ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	filtered := make([]directSessionAgentRow, 0, len(rows))
	for _, row := range rows {
		if row.ID <= 0 || row.Status == 3 {
			continue
		}
		filtered = append(filtered, row)
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	return filtered, nil
}

func selectDirectSessionTargets(
	sessionType int16,
	targetUserIDs []int64,
	agents []directSessionAgentRow,
) []directSessionAgentRow {
	if len(agents) == 0 {
		return nil
	}
	if sessionType != 2 {
		return agents[:1]
	}
	return filterDirectTargetsForGroup(targetUserIDs, agents)
}

func filterDirectTargetsForGroup(
	targetUserIDs []int64,
	agents []directSessionAgentRow,
) []directSessionAgentRow {
	if len(agents) == 0 {
		return nil
	}
	if len(targetUserIDs) == 0 {
		return agents
	}

	filtered := make([]directSessionAgentRow, 0, len(agents))
	for _, agent := range agents {
		if agent.ID > 0 && containsInt64(targetUserIDs, agent.ID) {
			filtered = append(filtered, agent)
		}
	}
	return filtered
}

// ensureApprovalIssuerTarget 保证发出审批卡的 agent 一定在直投目标里。群聊的接续语义
// 可能把目标收窄到别的 agent（多 agent 群），而审批回传只对发卡方有意义，必须必达。
func ensureApprovalIssuerTarget(
	targets []directSessionAgentRow,
	agents []directSessionAgentRow,
	issuerAgentID int64,
) []directSessionAgentRow {
	if issuerAgentID <= 0 {
		return targets
	}
	for _, row := range targets {
		if row.ID == issuerAgentID {
			return targets
		}
	}
	for _, row := range agents {
		if row.ID == issuerAgentID {
			extended := make([]directSessionAgentRow, 0, len(targets)+1)
			extended = append(extended, targets...)
			return append(extended, row)
		}
	}
	return targets
}

func buildDirectRouteAgent(row directSessionAgentRow) model.Agent {
	return model.Agent{
		ID:              row.ID,
		OwnerID:         row.OwnerID,
		ProviderType:    row.ProviderType,
		AgentClientType: row.AgentClientType,
		LocalEndpoint:   row.LocalEndpoint,
		LocalModelName:  row.LocalModelName,
		SystemPrompt:    row.SystemPrompt,
		Status:          row.Status,
	}
}

func buildDirectSessionMirrorTargets(
	agents []directSessionAgentRow,
	targets []directDispatchTarget,
	targetUserIDs []int64,
	mentionUserIDs []int64,
	senderID int64,
	senderType int16,
	visibleTo []int64,
) []directDispatchTarget {
	if len(agents) == 0 {
		return nil
	}

	dispatchedAgentIDs := make(map[int64]struct{}, len(targets))
	for _, target := range targets {
		if target.Agent.ID > 0 {
			dispatchedAgentIDs[target.Agent.ID] = struct{}{}
		}
	}

	mirrorTargets := make([]directDispatchTarget, 0, len(agents))
	for _, row := range agents {
		if row.ID <= 0 || row.ProviderType != model.AgentProviderAPI {
			continue
		}
		if senderType == 2 && row.ID == senderID {
			continue
		}
		agentClientType := wsagentapi.GetAgentClientType(row.ID)
		if agentClientType == "" {
			agentClientType = row.AgentClientType
		}
		if model.IsProprietaryAgentClientType(agentClientType) {
			continue
		}
		if _, exists := dispatchedAgentIDs[row.ID]; exists {
			continue
		}
		receiveMode, _ := agentreceive.Normalize(row.AgentReceiveMode, row.AgentReceiveBacklogCount)
		if receiveMode == agentreceive.ModeMentionOnly {
			continue
		}
		if containsInt64(targetUserIDs, row.OwnerID) {
			continue
		}
		// Defense-in-depth: skip agents not in visibleTo when set.
		if len(visibleTo) > 0 {
			visible := containsInt64(visibleTo, row.OwnerID) || containsInt64(visibleTo, row.ID)
			if !visible {
				continue
			}
		}
		mirrorTargets = append(mirrorTargets, directDispatchTarget{
			Agent:     buildDirectRouteAgent(row),
			Mentioned: containsInt64(mentionUserIDs, row.ID),
		})
	}
	if len(mirrorTargets) == 0 {
		return nil
	}
	return mirrorTargets
}

func buildDirectLocalInferenceHint(
	sessionID string,
	triggerMsgID int64,
	targets []directDispatchTarget,
) *protocol.LocalInferenceHint {
	for _, target := range targets {
		agent := target.Agent
		if agent.ProviderType != model.AgentProviderLocal || agent.LocalEndpoint == "" {
			continue
		}
		return &protocol.LocalInferenceHint{
			AgentID:         agent.ID,
			SessionID:       sessionID,
			TriggerMsgID:    triggerMsgID,
			Endpoint:        agent.LocalEndpoint,
			ModelName:       agent.LocalModelName,
			SystemPrompt:    agent.SystemPrompt,
			ContextMessages: target.ContextMessages,
		}
	}
	return nil
}

func dispatchDirectSessionRoute(
	hub HubInterface,
	ctx context.Context,
	sessionID string,
	sessionType int16,
	senderID int64,
	senderType int16,
	triggerMsgID int64,
	quotedMessageID int64,
	msgType int16,
	content string,
	extraRaw json.RawMessage,
	route *directSessionRoute,
) {
	if route == nil || (len(route.Targets) == 0 && len(route.MirrorTargets) == 0) {
		return
	}

	eventCreatedAt := int64(0)
	if sessionType == 2 || hasDirectAPITarget(route.Targets) {
		eventCreatedAt = nowUnixMilli()
	}

	remoteTriggered := false
	for _, target := range route.Targets {
		agent := target.Agent
		if senderType == 2 && senderID > 0 && agent.ID == senderID {
			continue
		}
		switch agent.ProviderType {
		case model.AgentProviderLocal:
			continue
		case model.AgentProviderVoice:
			// 语音大模型只做语音通话，不接文字消息（避免"未配置可用模型"错误气泡）
			continue
		case model.AgentProviderAPI:
			handled, gateErr := maybeHandleAgentAccessGate(ctx, agentAccessGateRequest{
				Agent:        agent,
				OwnerID:      agent.OwnerID,
				SessionID:    sessionID,
				SessionType:  sessionType,
				SenderID:     senderID,
				SenderType:   senderType,
				TriggerMsgID: triggerMsgID,
			}, sendAgentAccessMessage)
			if gateErr != nil {
				logger.L.Warnf(
					"agent access gate failed session=%s owner=%d agent=%d msg_id=%d: %v",
					sessionID,
					agent.OwnerID,
					agent.ID,
					triggerMsgID,
					gateErr,
				)
				continue
			}
			if handled {
				clearDirectRouteBufferOnAccept(ctx, sessionID, target)
				continue
			}

			eventType := "user_chat"
			if sessionType == 2 {
				if target.Mentioned {
					eventType = "group_mention"
				} else {
					eventType = "group_message"
				}
			}

			// OwnerID 决定后端按哪条 agent 连接路由事件(agent 共享多连接物理隔离):
			// - 私聊(sessionType=1): senderID 必为会话方之一(主人或被共享者),命中其对应连接;
			// - 群里(sessionType=2): agent 在群中代主人响应,senderID 可能是群里任意成员
			//   (非主人/非被共享者),必须用 agent.OwnerID 路由到主连接;否则 lookupConnByOwner
			//   找不到→事件入死队列→agent 没反应。
			eventOwnerID := senderID
			if sessionType == 2 {
				eventOwnerID = agent.OwnerID
			}
			event := wsagentapi.DelegateEventPayload{
				EventID:         fmt.Sprintf("%s:%d:%d:%d", sessionID, senderID, agent.ID, triggerMsgID),
				EventType:       eventType,
				MirrorMode:      wsagentapi.MirrorModeRecordAndProcess,
				AgentID:         agent.ID,
				OwnerID:         eventOwnerID,
				SessionID:       sessionID,
				SessionType:     sessionType,
				MsgID:           triggerMsgID,
				QuotedMessageID: quotedMessageID,
				SenderID:        senderID,
				Content:         content,
				MentionUserIDs:  protocol.StringInt64s(route.MentionUserIDs),
				ContextMessages: target.ContextMessages,
				CreatedAt:       eventCreatedAt,
			}
			wsagentapi.ApplyStructuredMessagePayload(&event, msgType, extraRaw)
			// 与托管代答一致：即便 IsAgentChannelAvailable=false，PushDelegateEvent
			// 仍可能直投/跨节点转发/入离线队列。入队成功不算投递失败，不应对用户报
			//「Agent 离线」——否则会出现先 toast 离线、几秒后队列 drain 又有回复的误报。
			// 只有 Push 真返回 false（连队列都失败）才通知 channel_unavailable。
			// 但入队成功也不能完全沉默：如果发送前 agent 就没有可达连接，这条消息大概率
			// 会在队列里躺到 agent 重新上线，用户至少要知道"消息已保存、agent 未连接"。
			// 用 eventOwnerID 维度判断可用性，与 pushDelegateEvent 实际按
			// (agentID, OwnerID) 精确路由的口径保持一致——agent 共享场景下，主连接是否
			// 在线和某个被共享者自己的连接是否在线是两回事，不能用 agent 级判断代替。
			wasAvailable := wsagentapi.IsAgentChannelAvailableForOwner(agent.ID, eventOwnerID)
			if ok := wsagentapi.PushDelegateEvent(event); !ok {
				logger.L.Warnf(
					"direct agent api event dropped session=%s owner=%d agent=%d",
					sessionID,
					senderID,
					agent.ID,
				)
				notifyAgentDeliveryError(
					hub,
					ctx,
					senderID,
					sessionID,
					agent.ID,
					triggerMsgID,
					protocol.AgentDeliveryScopeDirect,
					protocol.AgentDeliveryCodeChannelUnavailable,
					"agent api channel unavailable",
				)
			} else {
				clearDirectRouteBufferOnAccept(ctx, sessionID, target)
				if !wasAvailable {
					notifyAgentQueuedOffline(hub, ctx, senderID, sessionID, agent.ID, triggerMsgID, protocol.AgentDeliveryScopeDirect)
				}
			}
		default:
			if remoteTriggered {
				continue
			}
			remoteTriggered = true
			aiReq := map[string]interface{}{
				"cmd":           "client_stream_chunk",
				"session_id":    sessionID,
				"sender_id":     senderID,
				"user_id":       senderID,
				"delta_content": content,
				"is_finish":     true,
				"msg_id":        triggerMsgID,
				"agent_id":      agent.ID,
			}
			if len(target.ContextMessages) > 0 {
				aiReq["context_messages"] = target.ContextMessages
			}
			data, _ := json.Marshal(aiReq)
			if store.JS != nil {
				if _, err := store.JS.Publish("ai.request.chat", data); err != nil {
					logger.L.Warnf(
						"direct remote request publish failed session=%s sender=%d agent=%d msg_id=%d: %v",
						sessionID,
						senderID,
						agent.ID,
						triggerMsgID,
						err,
					)
					continue
				}
				clearDirectRouteBufferOnAccept(ctx, sessionID, target)
			}
		}
	}

	for _, target := range route.MirrorTargets {
		agent := target.Agent
		if agent.ProviderType != model.AgentProviderAPI {
			continue
		}
		eventType := "user_chat"
		if sessionType == 2 {
			if target.Mentioned {
				eventType = "group_mention"
			} else {
				eventType = "group_message"
			}
		}
		// OwnerID 路由口径与上面主投递保持一致(mirror 也要进同一条连接)。
		eventOwnerID := senderID
		if sessionType == 2 {
			eventOwnerID = agent.OwnerID
		}
		event := wsagentapi.DelegateEventPayload{
			EventID:         fmt.Sprintf("%s:%d:%d:%d:mirror", sessionID, senderID, agent.ID, triggerMsgID),
			EventType:       eventType,
			MirrorMode:      wsagentapi.MirrorModeRecordOnly,
			AgentID:         agent.ID,
			OwnerID:         eventOwnerID,
			SessionID:       sessionID,
			SessionType:     sessionType,
			MsgID:           triggerMsgID,
			QuotedMessageID: quotedMessageID,
			SenderID:        senderID,
			Content:         content,
			MentionUserIDs:  protocol.StringInt64s(route.MentionUserIDs),
			CreatedAt:       eventCreatedAt,
		}
		wsagentapi.ApplyStructuredMessagePayload(&event, msgType, extraRaw)
		if ok := wsagentapi.PushDelegateEvent(event); !ok {
			logger.L.Warnf(
				"direct agent api mirror dropped session=%s sender=%d agent=%d msg_id=%d",
				sessionID,
				senderID,
				agent.ID,
				triggerMsgID,
			)
		}
	}
}

func clearDirectRouteBufferOnAccept(ctx context.Context, sessionID string, target directDispatchTarget) {
	if !target.ClearBufferOnAccept {
		return
	}
	clearBufferedGroupDispatchContext(
		ctx,
		2,
		sessionID,
		target.BufferMemberType,
		target.BufferMemberID,
	)
}

func clearDirectRouteLocalBuffersOnAccept(ctx context.Context, sessionType int16, sessionID string, route *directSessionRoute) {
	if route == nil || len(route.Targets) == 0 {
		return
	}
	for _, target := range route.Targets {
		if target.Agent.ProviderType != model.AgentProviderLocal || !target.ClearBufferOnAccept {
			continue
		}
		clearBufferedGroupDispatchContext(
			ctx,
			sessionType,
			sessionID,
			target.BufferMemberType,
			target.BufferMemberID,
		)
	}
}

// TriggerDirectRouteForMessage resolves and dispatches direct agent routing for
// an already persisted message (used by stream-finish and agent-origin paths).
// precomputedSemantics lets a caller that already resolved group mention
// dispatch semantics for this same message (e.g. HandleSendMsg's
// liveGroupSemantics) pass them through, instead of resolving them again here —
// resolution has side effects (it increments the agent-to-agent loop-chain
// counter), so resolving twice for one message would double-count it. Pass nil
// when no such semantics exist yet (e.g. the stream-finish path, which is the
// first and only resolution point for that message).
func TriggerDirectRouteForMessage(
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
	precomputedSemantics *groupDispatchSemantics,
) {
	if sessionID == "" || senderID <= 0 || triggerMsgID <= 0 {
		return
	}
	if parseDelegateTriggerMeta(extraRaw).IsDelegateOrigin {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	sessionType := int16(1)
	if err := store.DB.Model(&model.Session{}).
		Select("session_type").
		Where("session_id = ?", sessionID).
		Scan(&sessionType).Error; err != nil {
		sessionType = 1
	}

	route, err := resolveDirectSessionRoute(
		sessionID,
		sessionType,
		senderID,
		senderType,
		triggerMsgID,
		quotedMessageID,
		msgType,
		content,
		extraRaw,
		precomputedSemantics,
		visibleTo,
		nil,
		false,
	)
	if err != nil {
		logger.L.Warnf("resolve direct session route failed session=%s msg_id=%d: %v", sessionID, triggerMsgID, err)
		return
	}
	if route == nil {
		return
	}

	dispatchDirectSessionRoute(
		hub,
		ctx,
		sessionID,
		sessionType,
		senderID,
		senderType,
		triggerMsgID,
		quotedMessageID,
		msgType,
		content,
		extraRaw,
		route,
	)
}

func hasDirectAPITarget(targets []directDispatchTarget) bool {
	for _, target := range targets {
		if target.Agent.ProviderType == model.AgentProviderAPI {
			return true
		}
	}
	return false
}

func nowUnixMilli() int64 {
	return time.Now().UnixMilli()
}

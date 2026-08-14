package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	llmctx "github.com/askie/grix/backend/internal/llm/context"
	"github.com/askie/grix/backend/internal/llm/provider"
	"github.com/askie/grix/backend/internal/llm/rag"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// processDelegateRequest handles a delegate_request: load agent, call LLM, send reply.
func (o *Orchestrator) processDelegateRequest(ctx context.Context, req *AIRequest) {
	if req.SessionID == "" || req.OwnerID <= 0 {
		return
	}

	var agentID int64
	if req.AgentID > 0 {
		agentID = req.AgentID
	} else if req.AgentIDStr != "" {
		if parsed, err := strconv.ParseInt(req.AgentIDStr, 10, 64); err == nil {
			agentID = parsed
		}
	}
	if agentID == 0 {
		return
	}
	activeAgentID, active := o.resolveActiveDelegateAgentID(ctx, req.SessionID, req.OwnerID)
	if !active {
		if logger.L != nil {
			logger.L.Infof("skip delegate request: delegation inactive session=%s owner=%d", req.SessionID, req.OwnerID)
		}
		return
	}
	if activeAgentID > 0 && activeAgentID != agentID {
		if logger.L != nil {
			logger.L.Infof(
				"skip stale delegate request: delegation switched session=%s owner=%d active_agent=%d req_agent=%d",
				req.SessionID,
				req.OwnerID,
				activeAgentID,
				agentID,
			)
		}
		return
	}
	delegateTimeout := o.delegateRequestTimeout()
	delegateCtx, cancel := context.WithTimeout(ctx, delegateTimeout)
	runID := o.registerDelegateRun(req.SessionID, req.OwnerID, cancel)
	defer func() {
		o.unregisterDelegateRun(req.SessionID, req.OwnerID, runID)
		cancel()
	}()
	startAt := time.Now()

	var agent model.Agent
	if err := store.DB.First(&agent, agentID).Error; err != nil {
		o.sendDelegateStreamError(ctx, req.SessionID, 0, req.OwnerID, 5004, "Agent not found")
		return
	}

	p, providerName := o.selectProvider(&agent)
	if p == nil {
		o.sendDelegateStreamError(ctx, req.SessionID, 0, req.OwnerID, 5005, "No provider available")
		return
	}

	var ragContext []string
	results, err := rag.Retrieve(ctx, req.SessionID, req.OwnerID, req.Content, 5)
	if err == nil {
		for _, r := range results {
			ragContext = append(ragContext, r.ContentText)
		}
	}

	var (
		messages    []provider.Message
		promptStats llmctx.PromptStats
	)
	if len(req.ContextMessages) > 0 {
		messages, promptStats = llmctx.BuildPromptForContextMessagesWithStats(
			&agent,
			req.Content,
			ragContext,
			req.ContextMessages,
		)
	} else {
		messages, promptStats = llmctx.BuildPromptForUserWithStats(
			req.SessionID,
			req.OwnerID,
			&agent,
			req.Content,
			ragContext,
		)
	}
	o.logPromptStats("delegate", req.SessionID, req.OwnerID, agentID, promptStats)

	identity, err := agentmsg.ResolveIdentity(ctx, agentmsg.IdentityParams{
		Mode:      agentmsg.ModeDelegate,
		SessionID: req.SessionID,
		OwnerID:   req.OwnerID,
		AgentID:   agentID,
	})
	if err != nil {
		logger.L.Warnf("resolve delegate identity failed session=%s owner=%d agent=%d err=%v", req.SessionID, req.OwnerID, agentID, err)
		o.sendDelegateStreamError(ctx, req.SessionID, 0, req.OwnerID, 5001, "Identity resolution failed")
		return
	}
	ss, err := agentmsg.NewStreamSession(agentmsg.StreamSessionConfig{
		Ctx:       ctx,
		SessionID: req.SessionID,
		Identity:  identity,
	})
	if err != nil {
		if errors.Is(err, sessionguard.ErrSpeakForbidden) ||
			errors.Is(err, sessionguard.ErrGroupAllMembersMuted) ||
			errors.Is(err, sessionguard.ErrMemberSpeakMuted) {
			o.sendDelegateStreamError(ctx, req.SessionID, 0, req.OwnerID, 4003, sessionguard.ErrorMessage(err))
			return
		}
		o.sendDelegateStreamError(ctx, req.SessionID, 0, req.OwnerID, 5001, "Save placeholder failed")
		return
	}
	msgID := ss.MsgID()
	activity := protocol.SessionActivityPayload{
		SessionID:    req.SessionID,
		Kind:         protocol.SessionActivityKindComposing,
		ActorID:      req.OwnerID,
		ActorType:    protocol.SessionActivityActorTypeHuman,
		ExecutorID:   agentID,
		ExecutorType: protocol.SessionActivityActorTypeAgent,
		Source:       protocol.SessionActivitySourceLLMDelegate,
		RefMsgID:     strconv.FormatInt(msgID, 10),
		RefEventID:   strconv.FormatInt(req.TriggerMsgID, 10),
	}
	if err := handler.UpsertSessionActivity(ctx, nil, activity); err != nil {
		logger.L.Warnf("set delegate llm composing failed session=%s owner=%d agent=%d: %v", req.SessionID, req.OwnerID, agentID, err)
	}
	defer func() {
		if err := handler.ClearSessionActivity(ctx, nil, activity); err != nil {
			logger.L.Warnf("clear delegate llm composing failed session=%s owner=%d agent=%d: %v", req.SessionID, req.OwnerID, agentID, err)
		}
	}()

	var totalPromptTokens, totalCompletionTokens int
	finished := false
	composingCleared := false
	var streamErr error

	llmReq := &provider.Request{Messages: messages, Stream: true}

	for attempt := 1; attempt <= llmStreamMaxAttempts; attempt++ {
		attemptHasChunk := false
		streamErr = nil
		err = p.StreamChat(delegateCtx, llmReq, func(chunk provider.StreamChunk) {
			if delegateCtx.Err() != nil || finished || streamErr != nil {
				return
			}
			if chunk.Error != nil {
				streamErr = chunk.Error
				return
			}

			if chunk.DeltaContent != "" {
				attemptHasChunk = true
				ss.AppendChunkBuffered(o.luaSHA, chunk.DeltaContent)
				if !composingCleared {
					composingCleared = true
					if err := handler.ClearSessionActivity(ctx, nil, activity); err != nil {
						logger.L.Warnf("clear delegate llm composing on first chunk failed session=%s owner=%d agent=%d: %v", req.SessionID, req.OwnerID, agentID, err)
					}
				}
			}

			if chunk.PromptTokens > 0 {
				totalPromptTokens = chunk.PromptTokens
			}
			if chunk.CompletionTokens > 0 {
				totalCompletionTokens = chunk.CompletionTokens
			}
			if chunk.IsFinish {
				finished = true
			}
		})

		if finished {
			break
		}
		if streamErr == nil {
			streamErr = err
		}
		if streamErr == nil {
			streamErr = errors.New("llm stream ended without finish")
		}
		if errors.Is(streamErr, context.Canceled) || errors.Is(delegateCtx.Err(), context.Canceled) {
			ss.Abort()
			return
		}
		if o.shouldRetryLLMStream(delegateCtx, streamErr, attemptHasChunk, attempt) {
			delay := llmStreamRetryDelay(attempt)
			if logger.L != nil {
				logger.L.Warnf(
					"delegate llm transient error, retrying session=%s owner=%d agent=%d provider=%s attempt=%d/%d delay=%s err=%v",
					req.SessionID,
					req.OwnerID,
					agentID,
					providerName,
					attempt+1,
					llmStreamMaxAttempts,
					delay,
					streamErr,
				)
			}
			if !sleepWithContext(delegateCtx, delay) {
				streamErr = delegateCtx.Err()
				break
			}
			continue
		}
		break
	}

	if !finished {
		if streamErr == nil {
			streamErr = err
		}
		if streamErr == nil {
			streamErr = errors.New("llm stream ended without finish")
		}
		if errors.Is(streamErr, context.Canceled) || errors.Is(delegateCtx.Err(), context.Canceled) {
			ss.Abort()
			return
		}
		ss.DeletePlaceholder()
		code, msg := o.classifyLLMError(delegateCtx, streamErr)
		if logger.L != nil {
			logger.L.Warnf(
				"delegate llm failed session=%s owner=%d agent=%d provider=%s timeout=%s elapsed=%s err=%v",
				req.SessionID,
				req.OwnerID,
				agentID,
				providerName,
				delegateTimeout,
				time.Since(startAt),
				streamErr,
			)
		}
		o.publishStreamError(ctx, req.SessionID, msgID, code, msg)
		return
	}

	fullContent, _ := store.RDB.Get(ctx, ss.BuilderKey()).Result()
	if fullContent == "" {
		ss.DeletePlaceholder()
		o.publishStreamError(ctx, req.SessionID, msgID, 5003, "Empty response from LLM")
		return
	}

	fullContent, err = ss.Finish()
	if err != nil {
		ss.DeletePlaceholder()
		o.publishStreamError(ctx, req.SessionID, msgID, 5003, "Save delegate final failed")
		return
	}

	if !finished {
		logger.L.Warnf("delegate stream finished without explicit finish flag session=%s msg=%d", req.SessionID, msgID)
	}
	if logger.L != nil {
		logger.L.Infof(
			"delegate llm success session=%s owner=%d agent=%d provider=%s elapsed=%s",
			req.SessionID,
			req.OwnerID,
			agentID,
			providerName,
			time.Since(startAt),
		)
	}

	store.RDB.Incr(ctx, fmt.Sprintf("im:delegate:streak:%s:%d", req.SessionID, req.OwnerID))
	o.triggerDelegatesForAutoMessage(ctx, req.SessionID, req.OwnerID, msgID, fullContent)

	store.DB.Create(&model.LLMUsageLog{
		UserID:           req.OwnerID,
		SessionID:        req.SessionID,
		AgentID:          agentID,
		ModelProvider:    providerName,
		PromptTokens:     totalPromptTokens,
		CompletionTokens: totalCompletionTokens,
	})
	service.ScheduleContentModeration(service.ContentModerationTask{
		SessionID: req.SessionID,
		MsgID:     msgID,
	})
}

func (o *Orchestrator) sendDelegateStreamError(
	ctx context.Context,
	sessionID string,
	msgID, ownerID int64,
	code int,
	errMsg string,
) {
	if sessionID == "" || ownerID <= 0 {
		return
	}
	if msgID <= 0 {
		msgID = snowflake.GenID()
	}

	payload := protocol.StreamErrorPayload{
		MsgID:     msgID,
		SessionID: sessionID,
		SenderID:  ownerID,
		ErrorCode: code,
		ErrorMsg:  errMsg,
		CreatedAt: time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(map[string]interface{}{
		"user_id": ownerID,
		"cmd":     "stream_error",
		"payload": payload,
	})

	routeKey := fmt.Sprintf("im:ws:route:%d", ownerID)
	devices, err := store.RDB.HGetAll(ctx, routeKey).Result()
	if err != nil {
		return
	}
	publishedNodes := make(map[string]bool)
	for _, nodeID := range devices {
		if publishedNodes[nodeID] {
			continue
		}
		publishedNodes[nodeID] = true
		if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", nodeID), string(data)).Err(); err != nil {
			logger.L.Warnf("publish delegate stream error to node %s failed: %v", nodeID, err)
		}
	}
}

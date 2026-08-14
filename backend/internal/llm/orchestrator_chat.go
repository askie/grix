package llm

import (
	"context"
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
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func (o *Orchestrator) processAIGeneration(ctx context.Context, req *AIRequest) {
	sessionID := req.SessionID
	if err := validateChatRequestSpeakPermission(ctx, req); err != nil {
		if errors.Is(err, sessionguard.ErrSpeakForbidden) ||
			errors.Is(err, sessionguard.ErrGroupAllMembersMuted) ||
			errors.Is(err, sessionguard.ErrMemberSpeakMuted) {
			o.publishStreamError(ctx, sessionID, 0, 4003, sessionguard.ErrorMessage(err))
			return
		}
		logger.L.Warnf("chat request speaking validation failed session=%s sender=%d err=%v", sessionID, req.SenderID, err)
		o.publishStreamError(ctx, sessionID, 0, 5001, "权限校验失败")
		return
	}

	quotaKey := fmt.Sprintf("ai:quota:%d:%s", req.SenderID, time.Now().Format("2006-01-02"))
	currentUsage, _ := store.RDB.Get(ctx, quotaKey).Int()
	if currentUsage > 1000000 {
		o.publishStreamError(ctx, sessionID, 0, 5010, "每日 Token 配额已耗尽")
		return
	}

	var member model.SessionMember
	if req.AgentID > 0 {
		store.DB.Where("session_id = ? AND member_id = ? AND member_type = 2", sessionID, req.AgentID).Take(&member)
	} else {
		store.DB.Where("session_id = ? AND member_type = 2", sessionID).Take(&member)
	}
	var agent model.Agent
	store.DB.Take(&agent, member.MemberID)

	var ragContext []string
	results, err := rag.Retrieve(ctx, sessionID, req.SenderID, req.DeltaContent, 5)
	if err == nil {
		for _, r := range results {
			ragContext = append(ragContext, r.ContentText)
		}
	}

	if agent.ID > 0 {
		kResults, err := rag.RetrieveKnowledge(ctx, agent.ID, req.DeltaContent, 3)
		if err == nil {
			for _, r := range kResults {
				ragContext = append(ragContext, r.ContentText)
			}
		}
	}

	var (
		messages    []provider.Message
		promptStats llmctx.PromptStats
	)
	if len(req.ContextMessages) > 0 {
		messages, promptStats = llmctx.BuildPromptForContextMessagesWithStats(
			&agent,
			req.DeltaContent,
			ragContext,
			req.ContextMessages,
		)
	} else {
		messages, promptStats = llmctx.BuildPromptForUserWithStats(
			sessionID,
			req.SenderID,
			&agent,
			req.DeltaContent,
			ragContext,
		)
	}
	o.logPromptStats("chat", sessionID, req.SenderID, agent.ID, promptStats)

	verKey := fmt.Sprintf("ai:ctx_ver:%s", sessionID)
	ctxVersion, _ := store.RDB.Get(ctx, verKey).Int64()

	identity, err := agentmsg.ResolveIdentity(ctx, agentmsg.IdentityParams{
		Mode:    agentmsg.ModeAIDirect,
		AgentID: agent.ID,
	})
	if err != nil {
		logger.L.Errorf("resolve ai_direct identity failed session=%s agent=%d err=%v", sessionID, agent.ID, err)
		o.publishStreamError(ctx, sessionID, 0, 5001, "身份解析失败")
		return
	}
	ss, err := agentmsg.NewStreamSession(agentmsg.StreamSessionConfig{
		Ctx:       ctx,
		SessionID: sessionID,
		Identity:  identity,
	})
	if err != nil {
		if errors.Is(err, sessionguard.ErrSpeakForbidden) ||
			errors.Is(err, sessionguard.ErrGroupAllMembersMuted) ||
			errors.Is(err, sessionguard.ErrMemberSpeakMuted) {
			o.publishStreamError(ctx, sessionID, 0, 4003, sessionguard.ErrorMessage(err))
			return
		}
		logger.L.Errorf("create stream session failed session=%s err=%v", sessionID, err)
		o.publishStreamError(ctx, sessionID, 0, 5001, "创建消息失败")
		return
	}
	msgID := ss.MsgID()
	activity := protocol.SessionActivityPayload{
		SessionID:    sessionID,
		Kind:         protocol.SessionActivityKindComposing,
		ActorID:      agent.ID,
		ActorType:    protocol.SessionActivityActorTypeAgent,
		ExecutorID:   agent.ID,
		ExecutorType: protocol.SessionActivityActorTypeAgent,
		Source:       protocol.SessionActivitySourceLLMDirect,
		RefMsgID:     strconv.FormatInt(msgID, 10),
	}
	if err := handler.UpsertSessionActivity(ctx, nil, activity); err != nil {
		logger.L.Warnf("set direct llm composing failed session=%s agent=%d: %v", sessionID, agent.ID, err)
	}
	defer func() {
		if err := handler.ClearSessionActivity(ctx, nil, activity); err != nil {
			logger.L.Warnf("clear direct llm composing failed session=%s agent=%d: %v", sessionID, agent.ID, err)
		}
	}()

	p, providerName := o.selectProvider(&agent)
	if p == nil {
		logger.L.Warnf("no llm provider available session=%s agent=%d provider=%s", sessionID, agent.ID, providerName)
		o.publishStreamError(ctx, sessionID, msgID, 5005, "未配置可用模型")
		return
	}

	llmReq := &provider.Request{Messages: messages, Stream: true}

	var totalPromptTokens, totalCompletionTokens int
	composingCleared := false
	finished := false
	contextChanged := false
	var streamErr error
	for attempt := 1; attempt <= llmStreamMaxAttempts; attempt++ {
		attemptHasChunk := false
		streamErr = nil
		err = p.StreamChat(ctx, llmReq, func(chunk provider.StreamChunk) {
			if finished || contextChanged || streamErr != nil {
				return
			}
			if chunk.Error != nil {
				streamErr = chunk.Error
				return
			}

			currentVer, _ := store.RDB.Get(ctx, verKey).Int64()
			if currentVer != ctxVersion {
				contextChanged = true
				return
			}

			if chunk.DeltaContent != "" {
				attemptHasChunk = true
				ss.AppendChunkBuffered(o.luaSHA, chunk.DeltaContent)
				if !composingCleared {
					composingCleared = true
					if err := handler.ClearSessionActivity(ctx, nil, activity); err != nil {
						logger.L.Warnf("clear direct llm composing on first chunk failed session=%s agent=%d: %v", sessionID, agent.ID, err)
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

		if contextChanged || finished {
			break
		}

		if streamErr == nil {
			streamErr = err
		}
		if streamErr == nil {
			streamErr = errors.New("llm stream ended without finish")
		}

		if o.shouldRetryLLMStream(ctx, streamErr, attemptHasChunk, attempt) {
			delay := llmStreamRetryDelay(attempt)
			logger.L.Warnf(
				"llm stream transient error, retrying session=%s msg_id=%d agent=%d provider=%s attempt=%d/%d delay=%s err=%v",
				sessionID,
				msgID,
				agent.ID,
				providerName,
				attempt+1,
				llmStreamMaxAttempts,
				delay,
				streamErr,
			)
			if !sleepWithContext(ctx, delay) {
				streamErr = ctx.Err()
				break
			}
			continue
		}
		break
	}

	if contextChanged {
		return
	}

	if !finished {
		if streamErr == nil {
			streamErr = err
		}
		if streamErr == nil {
			streamErr = errors.New("llm stream ended without finish")
		}
		code, msg := o.classifyLLMError(ctx, streamErr)
		logger.L.Errorf("LLM stream error session=%s err=%v", sessionID, streamErr)
		ss.Abort()
		o.publishStreamError(ctx, sessionID, msgID, code, msg)
		return
	}

	fullContent, finishErr := ss.Finish()
	if finishErr != nil {
		logger.L.Errorf("processAIGeneration finish failed session=%s msg_id=%d err=%v", sessionID, msgID, finishErr)
		ss.Abort()
		o.publishStreamError(ctx, sessionID, msgID, 5001, "保存消息失败")
		return
	}

	store.DB.Create(&model.LLMUsageLog{
		UserID:           req.SenderID,
		SessionID:        sessionID,
		AgentID:          agent.ID,
		ModelProvider:    providerName,
		PromptTokens:     totalPromptTokens,
		CompletionTokens: totalCompletionTokens,
	})

	totalTokens := totalPromptTokens + totalCompletionTokens
	store.RDB.IncrBy(ctx, quotaKey, int64(totalTokens))
	store.RDB.Expire(ctx, quotaKey, 24*time.Hour)

	o.publishEmbeddingTask(msgID, sessionID, fullContent)
	service.ScheduleContentModeration(service.ContentModerationTask{
		SessionID: sessionID,
		MsgID:     msgID,
	})
}

func validateChatRequestSpeakPermission(ctx context.Context, req *AIRequest) error {
	if req == nil {
		return sessionguard.ErrSpeakForbidden
	}
	return sessionguard.ValidateSpeakPermission(ctx, nil, req.SessionID, req.SenderID, 1)
}

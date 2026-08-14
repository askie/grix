package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/api/service"
	llmctx "github.com/askie/grix/backend/internal/llm/context"
	"github.com/askie/grix/backend/internal/llm/provider"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// Orchestrator manages AI request processing.
type Orchestrator struct {
	providers      map[string]provider.Provider
	luaSHA         string // ai_builder_append.lua SHA
	delegateMu     sync.Mutex
	delegateRuns   map[string]map[uint64]context.CancelFunc
	delegateRunSeq uint64
}

const (
	embeddingQueueGroup         = "llm-embedding-v1"
	embeddingDurable            = "llm-embedding-v1"
	embeddingAckWait            = 10 * time.Minute
	embeddingInProgressInterval = 20 * time.Second
	aiRequestDedupeTTL          = 24 * time.Hour
	delegateAutoRateTTL         = 350 * time.Millisecond
	delegateMaxRepliesDefault   = 10
	delegateMaxRepliesCeiling   = 50
	defaultDelegateTimeout      = 5 * time.Minute
	minDelegateTimeout          = 10 * time.Second
	maxDelegateTimeout          = 30 * time.Minute
	llmStreamMaxAttempts        = 3
	llmStreamRetryBaseDelay     = 300 * time.Millisecond
	llmStreamRetryMaxDelay      = 2 * time.Second
	promptEstimateWarnTokens    = 3500
)

func NewOrchestrator(providers map[string]provider.Provider) *Orchestrator {
	// Load Lua script
	script := `
redis.call('APPEND', KEYS[1], ARGV[1])
if redis.call('TTL', KEYS[1]) == -1 then
    redis.call('EXPIRE', KEYS[1], ARGV[2])
end
return redis.call('STRLEN', KEYS[1])
`
	sha, err := store.RDB.ScriptLoad(context.Background(), script).Result()
	if err != nil {
		logger.L.Errorf("failed to load ai_builder lua: %v", err)
	}

	return &Orchestrator{
		providers:    providers,
		luaSHA:       sha,
		delegateRuns: make(map[string]map[uint64]context.CancelFunc),
	}
}

// AIRequest represents an incoming AI request from NATS.
type AIRequest struct {
	Cmd             string                           `json:"cmd"`
	SessionID       string                           `json:"session_id"`
	SenderID        int64                            `json:"sender_id"`
	UserID          int64                            `json:"user_id"`
	DeltaContent    string                           `json:"delta_content"`
	IsFinish        bool                             `json:"is_finish"`
	MsgID           int64                            `json:"msg_id"`
	TargetMsgID     int64                            `json:"target_msg_id"`
	ContextMessages []protocol.ContextMessagePayload `json:"context_messages,omitempty"`
	// Delegate-specific fields
	OwnerID      int64  `json:"owner_id"`
	AgentID      int64  `json:"agent_id"`
	AgentIDStr   string `json:"agent_id_str"`
	Content      string `json:"content"`
	TriggerMsgID int64  `json:"trigger_msg_id"`
}

func (o *Orchestrator) HandleRequest(ctx context.Context, req *AIRequest) {
	switch req.Cmd {
	case "client_stream_chunk":
		if req.IsFinish {
			if !o.acquireChatRequestDedupe(ctx, req) {
				return
			}
			o.processAIGeneration(ctx, req)
		}
	case "delegate_request":
		if !o.acquireDelegateRequestDedupe(ctx, req) {
			return
		}
		o.processDelegateRequest(ctx, req)
	case "stream_stop":
		o.handleStop(ctx, req)
	case "override_stream":
		o.handleOverride(ctx, req)
	case "delegate_stop":
		o.handleDelegateStop(ctx, req)
	}
}

func (o *Orchestrator) acquireChatRequestDedupe(ctx context.Context, req *AIRequest) bool {
	if req == nil || req.SessionID == "" || req.SenderID <= 0 || req.MsgID <= 0 {
		return true
	}
	key := fmt.Sprintf("im:ai:req:dedupe:chat:%s:%d:%d", req.SessionID, req.SenderID, req.MsgID)
	if req.AgentID > 0 {
		key = fmt.Sprintf("%s:%d", key, req.AgentID)
	}
	ok, err := store.RDB.SetNX(ctx, key, 1, aiRequestDedupeTTL).Result()
	if err != nil {
		if logger.L != nil {
			logger.L.Warnf("chat dedupe setnx error key=%s: %v", key, err)
		}
		return true // fail-open to avoid dropping valid requests when redis jitters
	}
	if !ok {
		if logger.L != nil {
			logger.L.Infof("skip duplicate chat request session=%s sender=%d msg_id=%d", req.SessionID, req.SenderID, req.MsgID)
		}
		return false
	}
	return true
}

func (o *Orchestrator) acquireDelegateRequestDedupe(ctx context.Context, req *AIRequest) bool {
	if req == nil || req.SessionID == "" || req.OwnerID <= 0 || req.TriggerMsgID <= 0 {
		return true
	}
	key := fmt.Sprintf(
		"im:ai:req:dedupe:delegate:%s:%d:%d",
		req.SessionID,
		req.OwnerID,
		req.TriggerMsgID,
	)
	ok, err := store.RDB.SetNX(ctx, key, 1, aiRequestDedupeTTL).Result()
	if err != nil {
		if logger.L != nil {
			logger.L.Warnf("delegate dedupe setnx error key=%s: %v", key, err)
		}
		return true // fail-open to avoid dropping valid requests when redis jitters
	}
	if !ok {
		if logger.L != nil {
			logger.L.Infof(
				"skip duplicate delegate request session=%s owner=%d trigger_msg_id=%d",
				req.SessionID,
				req.OwnerID,
				req.TriggerMsgID,
			)
		}
		return false
	}
	return true
}

// selectProvider chooses a provider based on the agent's configuration.
func (o *Orchestrator) selectProvider(agent *model.Agent) (provider.Provider, string) {
	if agent.ProviderType == model.AgentProviderAPI {
		// Agent API provider is handled by ws/agent-api bridge instead of LLM providers.
		return nil, "agent_api"
	}
	if agent.ProviderType == model.AgentProviderVoice {
		// 语音大模型（type=4）只做语音通话，不参与文字应答。
		return nil, "voice_only"
	}

	// Local LLM provider
	if agent.ProviderType == model.AgentProviderLocal && agent.LocalEndpoint != "" {
		return provider.NewLocalProvider(agent.LocalEndpoint, agent.LocalModelName), "local"
	}

	// Remote provider
	providerName := "openai"
	if agent.ModelProvider != "" {
		if _, ok := o.providers[agent.ModelProvider]; ok {
			providerName = agent.ModelProvider
		}
	}
	return o.providers[providerName], providerName
}

func delegateRunKey(sessionID string, ownerID int64) string {
	return fmt.Sprintf("%s:%d", sessionID, ownerID)
}

func (o *Orchestrator) registerDelegateRun(sessionID string, ownerID int64, cancel context.CancelFunc) uint64 {
	if sessionID == "" || ownerID <= 0 || cancel == nil {
		return 0
	}

	key := delegateRunKey(sessionID, ownerID)
	runID := atomic.AddUint64(&o.delegateRunSeq, 1)

	o.delegateMu.Lock()
	if _, ok := o.delegateRuns[key]; !ok {
		o.delegateRuns[key] = make(map[uint64]context.CancelFunc)
	}
	o.delegateRuns[key][runID] = cancel
	o.delegateMu.Unlock()

	return runID
}

func (o *Orchestrator) unregisterDelegateRun(sessionID string, ownerID int64, runID uint64) {
	if runID == 0 || sessionID == "" || ownerID <= 0 {
		return
	}

	key := delegateRunKey(sessionID, ownerID)
	o.delegateMu.Lock()
	if runs, ok := o.delegateRuns[key]; ok {
		delete(runs, runID)
		if len(runs) == 0 {
			delete(o.delegateRuns, key)
		}
	}
	o.delegateMu.Unlock()
}

func (o *Orchestrator) cancelDelegateRun(sessionID string, ownerID int64) bool {
	if sessionID == "" || ownerID <= 0 {
		return false
	}

	key := delegateRunKey(sessionID, ownerID)
	var cancels []context.CancelFunc

	o.delegateMu.Lock()
	if runs, ok := o.delegateRuns[key]; ok {
		for _, cancel := range runs {
			if cancel != nil {
				cancels = append(cancels, cancel)
			}
		}
		delete(o.delegateRuns, key)
	}
	o.delegateMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	return len(cancels) > 0
}

func (o *Orchestrator) handleDelegateStop(ctx context.Context, req *AIRequest) {
	_ = ctx
	if req == nil || req.SessionID == "" {
		return
	}

	ownerID := req.OwnerID
	if ownerID <= 0 {
		ownerID = req.UserID
	}
	if ownerID <= 0 {
		return
	}

	if o.cancelDelegateRun(req.SessionID, ownerID) && logger.L != nil {
		logger.L.Infof("delegate stream canceled session=%s owner=%d", req.SessionID, ownerID)
	}
}

func (o *Orchestrator) resolveActiveDelegateAgentID(
	ctx context.Context,
	sessionID string,
	ownerID int64,
) (int64, bool) {
	if sessionID == "" || ownerID <= 0 {
		return 0, false
	}

	delegateKey := fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID)
	agentIDStr, err := store.RDB.HGet(ctx, delegateKey, "agent_id").Result()
	if err != nil {
		return 0, false
	}
	agentIDStr = strings.TrimSpace(agentIDStr)
	if agentIDStr == "" {
		return 0, false
	}

	agentID, err := strconv.ParseInt(agentIDStr, 10, 64)
	if err != nil || agentID <= 0 {
		return 0, false
	}
	return agentID, true
}

func (o *Orchestrator) delegateRequestTimeout() time.Duration {
	sec := config.C.LLM.DelegateTimeoutSec
	if sec <= 0 {
		return defaultDelegateTimeout
	}
	timeout := time.Duration(sec) * time.Second
	if timeout < minDelegateTimeout {
		return minDelegateTimeout
	}
	if timeout > maxDelegateTimeout {
		return maxDelegateTimeout
	}
	return timeout
}

func (o *Orchestrator) logPromptStats(
	mode string,
	sessionID string,
	userID int64,
	agentID int64,
	stats llmctx.PromptStats,
) {
	if logger.L == nil || stats.EstimatedTokens < promptEstimateWarnTokens {
		return
	}
	logger.L.Warnf(
		"prompt size high mode=%s session=%s user=%d agent=%d messages=%d est_tokens=%d system=%d rag=%d history_msgs=%d history_tokens=%d user_tokens=%d user_dedup=%t",
		mode,
		sessionID,
		userID,
		agentID,
		stats.TotalMessages,
		stats.EstimatedTokens,
		stats.SystemTokens,
		stats.RAGTokens,
		stats.HistoryMessages,
		stats.HistoryTokens,
		stats.UserInputTokens,
		stats.UserInputDedup,
	)
}

func (o *Orchestrator) shouldRetryLLMStream(
	ctx context.Context,
	err error,
	attemptHasChunk bool,
	attempt int,
) bool {
	if err == nil || attemptHasChunk || attempt >= llmStreamMaxAttempts {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	return isRetryableLLMStreamError(err)
}

func llmStreamRetryDelay(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	delay := llmStreamRetryBaseDelay << uint(attempt-1)
	if delay > llmStreamRetryMaxDelay {
		return llmStreamRetryMaxDelay
	}
	return delay
}

func sleepWithContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	if ctx == nil {
		time.Sleep(delay)
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func isRetryableLLMStreamError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}

	if strings.Contains(msg, "api error 429") ||
		strings.Contains(msg, "api error 500") ||
		strings.Contains(msg, "api error 502") ||
		strings.Contains(msg, "api error 503") ||
		strings.Contains(msg, "api error 504") {
		return true
	}

	retryHints := []string{
		"timeout",
		"temporarily unavailable",
		"connection reset",
		"connection refused",
		"connection aborted",
		"broken pipe",
		"unexpected eof",
		"eof",
		"tls handshake timeout",
		"no such host",
		"server closed idle connection",
		"http2: client connection lost",
		"upstream",
	}
	for _, hint := range retryHints {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}

func (o *Orchestrator) classifyLLMError(ctx context.Context, err error) (int, string) {
	if err == nil {
		return 5003, "上游服务响应异常"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return 5003, "上游服务响应超时"
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return 5003, "请求已取消"
	}
	return 5003, "上游服务响应异常"
}

// triggerDelegatesForAutoMessage continues delegation chain for other owners
// when current delegated output finishes (delegate_origin=true path).
func (o *Orchestrator) triggerDelegatesForAutoMessage(
	ctx context.Context,
	sessionID string,
	senderID, triggerMsgID int64,
	content string,
) {
	if store.JS == nil || sessionID == "" || triggerMsgID <= 0 || content == "" {
		return
	}

	var members []model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_type = 1", sessionID).Find(&members).Error; err != nil {
		logger.L.Warnf("delegate chain query members error session=%s: %v", sessionID, err)
		return
	}

	for _, m := range members {
		if m.MemberID == senderID {
			continue
		}

		delegateKey := fmt.Sprintf("im:delegate:%s:%d", sessionID, m.MemberID)
		fields, err := store.RDB.HMGet(ctx, delegateKey, "agent_id", "max_consecutive_replies").Result()
		if err != nil || len(fields) < 1 || fields[0] == nil {
			continue
		}

		agentIDStr := fmt.Sprint(fields[0])
		if agentIDStr == "" || agentIDStr == "<nil>" {
			continue
		}

		maxConsecutive := delegateMaxRepliesDefault
		if len(fields) >= 2 {
			maxConsecutive = delegateMaxRepliesFromRedis(fields[1])
		}

		streakKey := fmt.Sprintf("im:delegate:streak:%s:%d", sessionID, m.MemberID)
		if streak, err := store.RDB.Get(ctx, streakKey).Int(); err == nil && streak >= maxConsecutive {
			continue
		}

		rateLimitKey := fmt.Sprintf("im:delegate:rate:auto:%s:%d", sessionID, m.MemberID)
		if ok, _ := store.RDB.SetNX(ctx, rateLimitKey, 1, delegateAutoRateTTL).Result(); !ok {
			continue
		}

		delegateReq := map[string]interface{}{
			"cmd":            "delegate_request",
			"session_id":     sessionID,
			"sender_id":      senderID,
			"owner_id":       m.MemberID,
			"agent_id_str":   agentIDStr,
			"content":        content,
			"trigger_msg_id": triggerMsgID,
		}
		data, _ := json.Marshal(delegateReq)
		if _, err := store.JS.Publish(fmt.Sprintf("ai.request.%s", sessionID), data); err != nil {
			logger.L.Warnf("delegate chain publish failed session=%s owner=%d: %v", sessionID, m.MemberID, err)
		}
	}
}

func delegateMaxRepliesFromRedis(raw any) int {
	n := delegateMaxRepliesDefault
	switch v := raw.(type) {
	case int:
		n = v
	case int64:
		n = int(v)
	case int32:
		n = int(v)
	case float64:
		n = int(v)
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			n = parsed
		}
	case []byte:
		if parsed, err := strconv.Atoi(string(v)); err == nil {
			n = parsed
		}
	}
	return normalizeDelegateMaxReplies(n)
}

func normalizeDelegateMaxReplies(v int) int {
	if v <= 0 {
		return delegateMaxRepliesDefault
	}
	if v > delegateMaxRepliesCeiling {
		return delegateMaxRepliesCeiling
	}
	return v
}

func (o *Orchestrator) handleStop(ctx context.Context, req *AIRequest) {
	// Increment context version to invalidate current stream
	verKey := fmt.Sprintf("ai:ctx_ver:%s", req.SessionID)
	store.RDB.Incr(ctx, verKey)

	if req.MsgID > 0 {
		// Look up the original sender from the placeholder message.
		var msg model.Message
		if err := store.DB.Select("sender_id, sender_type").
			Where("msg_id = ? AND session_id = ?", req.MsgID, req.SessionID).
			First(&msg).Error; err != nil {
			logger.L.Warnf("handle_stop: query message sender failed msg_id=%d err=%v", req.MsgID, err)
			return
		}

		identity := &agentmsg.SenderIdentity{
			SenderID:   msg.SenderID,
			SenderType: msg.SenderType,
		}
		ss := agentmsg.ResumeStreamSession(agentmsg.StreamSessionConfig{
			Ctx:       ctx,
			SessionID: req.SessionID,
			Identity:  identity,
		}, req.MsgID)

		// ForceFinish handles: get builder content, update DB+session summary, inbox, broadcast, cleanup.
		if _, err := ss.ForceFinish(nil); err != nil {
			logger.L.Warnf("handle_stop: force finish failed session=%s msg_id=%d err=%v", req.SessionID, req.MsgID, err)
			ss.Abort()
		} else {
			service.ScheduleContentModeration(service.ContentModerationTask{
				SessionID: req.SessionID,
				MsgID:     req.MsgID,
			})
		}
	}
}

func (o *Orchestrator) handleOverride(ctx context.Context, req *AIRequest) {
	// Same as stop but mark as overridden
	verKey := fmt.Sprintf("ai:ctx_ver:%s", req.SessionID)
	store.RDB.Incr(ctx, verKey)

	if req.TargetMsgID > 0 {
		var msg model.Message
		if err := store.DB.Select("sender_id, sender_type, extra").
			Where("msg_id = ? AND session_id = ?", req.TargetMsgID, req.SessionID).
			First(&msg).Error; err != nil {
			logger.L.Warnf("handle_override: query message sender failed msg_id=%d err=%v", req.TargetMsgID, err)
			return
		}

		identity := &agentmsg.SenderIdentity{
			SenderID:   msg.SenderID,
			SenderType: msg.SenderType,
		}
		ss := agentmsg.ResumeStreamSession(agentmsg.StreamSessionConfig{
			Ctx:       ctx,
			SessionID: req.SessionID,
			Identity:  identity,
		}, req.TargetMsgID)

		var extra map[string]any
		if len(msg.Extra) > 0 {
			_ = json.Unmarshal(msg.Extra, &extra)
		}
		if extra == nil {
			extra = make(map[string]any)
		}
		extra["is_overridden"] = true
		newExtraJSON, _ := json.Marshal(extra)

		if _, err := ss.ForceFinish(map[string]any{
			"extra": newExtraJSON,
		}); err != nil {
			logger.L.Warnf("handle_override: force finish failed session=%s msg_id=%d err=%v", req.SessionID, req.TargetMsgID, err)
			ss.Abort()
		} else {
			service.ScheduleContentModeration(service.ContentModerationTask{
				SessionID: req.SessionID,
				MsgID:     req.TargetMsgID,
			})
		}
	}
}

func (o *Orchestrator) publishStreamError(ctx context.Context, sessionID string, msgID int64, code int, msg string) {
	payload := protocol.StreamErrorPayload{
		MsgID:     msgID,
		SessionID: sessionID,
		ErrorCode: code,
		ErrorMsg:  msg,
		CreatedAt: time.Now().UnixMilli(),
	}
	agentmsg.BroadcastToSession(ctx, sessionID, "stream_error", payload)
}

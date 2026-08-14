package call

import (
	"context"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
)

// VoiceBridgeSpec 承载启动一次 AI 语音桥接所需的完整配置（BYOK）。
// 由调用方（handler）从 agent 表解析并解密 API key 后传入，Controller 不依赖 DB。
type VoiceBridgeSpec struct {
	AgentID        int64
	CallerID       int64  // 发起方用户 ID，voicebridge 据此确定监听哪个 participant 的音频
	OwnerID        int64  // 被叫（主人）用户 ID；>0 时 voicebridge 额外接入主人音频（三方插话），其语音不落 IM
	SessionID      string // IM 会话 ID，作为豆包 dialog_id 保持跨通话上下文连续性
	Provider       string // voice_provider，如 openai_realtime / doubao_realtime
	Model          string // 模型名
	Endpoint       string // 可选自定义 base URL
	Voice          string // 音色 ID（可空，用 provider 默认）
	SystemPrompt   string // 人设
	APIKey         string // 用户自带的明文 API key（仅内网链路流转，不落日志）
	Language       string // 语言代码，见 pkg/locale.Supported（恒非空，默认 en_US）
	Opening        string // 语音开场白文案（按 Language 从 agent 配置选取，空则不主动播报）
	MaxCallSeconds int    // 单通话时长上限(秒)，0=不限
	DailyLimit     int    // 每日通话次数上限，0=不限
	AllowVisitor   bool   // 是否开放访客/客服通话
	MaxConcurrent  int    // 同一 agent 同时接待的并发通话上限，0=不限（仅访客客服路径校验）
	RelayMode      bool   // 传声筒模式（语音大脑）：语音模型只念固定过场语、绝不自答，
	// 文字 agent 回复经豆包 SayHello(事件300) 逐字念回；与客服混合快答(事件502 参考资料)互斥。
	TextAgentID int64 // 语音大脑的文字 agent ID，仅记入 call_records.text_agent_id，voicebridge 不使用。
}

// BridgeManager 抽象 Voice Bridge Worker 的控制接口，便于测试 mock。
// 真实实现由 cmd/voicebridge 进程通过 NATS 或 gRPC 提供。
type BridgeManager interface {
	// StartBridge 启动指定通话的 AI 语音桥接（连接 OpenAI Realtime 等 Provider）。
	StartBridge(ctx context.Context, callID int64, spec VoiceBridgeSpec) error
	// StopBridge 停止桥接（通话结束或 AI 异常时调用）。
	StopBridge(ctx context.Context, callID int64) error
	// InterruptBridge 暂停并销毁 AI session（旧接管路径，已被 MuteBridge 取代，保留备用）。
	InterruptBridge(ctx context.Context, callID int64) error
	// MuteBridge 静音 AI：停止喂访客音频 + 停止 AI 发声，但保留 session。
	// 接管时调用——连接与上下文不断，API 不断，交回可秒恢复。
	MuteBridge(ctx context.Context, callID int64) error
	// UnmuteBridge 恢复 AI 听 + 发声（从接管交回 AI 时调用）。
	UnmuteBridge(ctx context.Context, callID int64) error
}

// NewWithBridge 创建带 BridgeManager 的 Controller（Phase 2 用）。
func NewWithBridge(rooms RoomProvider, persist Persister, notify NotifyFn, bridge BridgeManager) *Controller {
	c := New(rooms, persist, notify)
	c.bridge = bridge
	return c
}

// AnswerWithAI 处理 B 选择 AI 代接。
// 状态机：RINGING → AI_DELEGATED。
// 返回 B 的 room token（B 可选择监听通话）和 room URL。
// spec 由调用方（handler）从 agent 表解析（含解密 API key）后传入，避免 Controller 依赖 DB。
func (c *Controller) AnswerWithAI(ctx context.Context, callID, calleeID int64, spec VoiceBridgeSpec) (tokenCallee, roomURL string, err error) {
	agentID := spec.AgentID
	callLogInfof("call trace: controller answer_with_ai begin call=%d callee=%d agent=%d provider=%s", callID, calleeID, agentID, spec.Provider)
	c.mu.Lock()
	entry, ok := c.calls[callID]
	if !ok {
		c.mu.Unlock()
		return "", "", fmt.Errorf("call not found: %d", callID)
	}
	if entry.record.CalleeID != calleeID {
		c.mu.Unlock()
		return "", "", fmt.Errorf("callee mismatch")
	}
	if entry.record.State != model.CallStateRinging {
		c.mu.Unlock()
		return "", "", fmt.Errorf("call not ringing, state=%d", entry.record.State)
	}
	c.stopCallTimer(entry.timer)
	now := time.Now()
	entry.record.State = model.CallStateAIDelegated
	entry.record.AnsweredAt = &now
	entry.record.DelegationMode = model.CallDelegationAIDelegated
	entry.record.DelegatedAgentID = &agentID
	entry.record.AIProvider = spec.Provider
	entry.aiSpec = spec
	token := entry.tokenCallee
	url := entry.roomURL
	callerID := entry.record.CallerID
	sessionID := entry.record.SessionID
	c.mu.Unlock()

	// 启动 Voice Bridge
	if c.bridge != nil {
		spec.CallerID = callerID
		spec.OwnerID = calleeID    // 主人（被叫）插话音频接入，三方通话用
		spec.SessionID = sessionID // 跨通话 dialog 连续性
		if err := c.bridge.StartBridge(ctx, callID, spec); err != nil {
			callLogInfof("call trace: controller answer_with_ai bridge_fail call=%d agent=%d err=%v", callID, agentID, err)
			c.mu.Lock()
			if e, exists := c.calls[callID]; exists {
				e.record.State = model.CallStateRinging
				e.record.AnsweredAt = nil
				e.record.DelegationMode = model.CallDelegationHuman
				e.record.DelegatedAgentID = nil
				e.record.AIProvider = ""
				c.resetCallTimer(e.timer, c.ringTimeout)
			}
			c.mu.Unlock()
			return "", "", fmt.Errorf("start bridge: %w", err)
		}
	}
	// Bridge 可用后再持久化接听记录，避免启动失败留下错误的 AI_DELEGATED 记录。
	if err := c.persist.UpdateAnsweredWithAI(ctx, callID, agentID, now); err != nil {
		callLogInfof("call trace: controller answer_with_ai persist_fail call=%d err=%v", callID, err)
		if c.bridge != nil {
			_ = c.bridge.StopBridge(ctx, callID)
		}
		c.mu.Lock()
		if e, exists := c.calls[callID]; exists {
			e.record.State = model.CallStateRinging
			e.record.AnsweredAt = nil
			e.record.DelegationMode = model.CallDelegationHuman
			e.record.DelegatedAgentID = nil
			e.record.AIProvider = ""
			c.resetCallTimer(e.timer, c.ringTimeout)
		}
		c.mu.Unlock()
		return "", "", fmt.Errorf("persist answered: %w", err)
	}

	// Phase 3: AI 托管必录，启动 Egress 录音（失败不阻断通话）
	if c.recorder != nil && c.recorder.Enabled() {
		if egressID, rerr := c.recorder.StartRecording(ctx, callID); rerr == nil && egressID != "" {
			c.mu.Lock()
			if e, ok := c.calls[callID]; ok {
				e.egressID = egressID
			}
			c.mu.Unlock()
		} else if rerr != nil {
			callLogInfof("call trace: answer_with_ai start_recording error call=%d err=%v", callID, rerr)
		}
	}

	// P0 护栏：单通话时长上限 → 到点服务端自动挂断
	if spec.MaxCallSeconds > 0 {
		c.mu.Lock()
		if e, ok := c.calls[callID]; ok {
			e.timer = c.trackedAfterFunc(time.Duration(spec.MaxCallSeconds)*time.Second, func() {
				_ = c.endCall(context.Background(), callID, model.CallStateEnded, "max_duration")
			})
		}
		c.mu.Unlock()
	}

	// 通知 caller：对方已由 AI 代接
	callIDStr := fmt.Sprintf("%d", callID)
	c.notify(callerID, "call:peer_answered", map[string]any{
		"call_id": callIDStr,
		"mode":    "ai_delegated",
	})
	callLogInfof("call trace: controller answer_with_ai delegated call=%d callee=%d agent=%d", callID, calleeID, agentID)

	return token, url, nil
}

// DirectAICall 由 owner 直接拨打语音大模型 agent。
// 直接进入 AI_DELEGATED：建房间、发起方入会、用 spec 起 bridge。
// call_records 正常落库，sessionID 为真实 IM 会话 ID。
func (c *Controller) DirectAICall(ctx context.Context, callerID int64, sessionID string, spec VoiceBridgeSpec) (callID int64, tokenCaller, roomURL string, err error) {
	return c.DirectAICallWithID(ctx, snowflake.GenID(), callerID, sessionID, spec)
}

// DirectAICallWithID 由 owner 直接拨打语音大模型 agent，使用调用方提供的 callID。
// 供跨 WS 节点 guard 在创建前占用 Redis busy key。
func (c *Controller) DirectAICallWithID(ctx context.Context, callID, callerID int64, sessionID string, spec VoiceBridgeSpec) (int64, string, string, error) {
	agentID := spec.AgentID
	callLogInfof("call trace: controller direct_ai_call begin call=%d caller=%d agent=%d session=%s provider=%s", callID, callerID, agentID, sessionID, spec.Provider)

	c.mu.Lock()
	if c.isBusyLocked(callerID) {
		c.mu.Unlock()
		callLogInfof("call trace: controller direct_ai busy call=%d caller=%d", callID, callerID)
		return 0, "", "", ErrCallerBusy
	}
	now := time.Now()
	entry := &callEntry{
		record: model.CallRecord{
			ID:               callID,
			SessionID:        sessionID,
			CallerID:         callerID,
			CalleeID:         agentID,
			CallMode:         model.CallModeVoice,
			State:            model.CallStateAIDelegated,
			DelegationMode:   model.CallDelegationAIDelegated,
			DelegatedAgentID: &agentID,
			TextAgentID: func() *int64 {
				if spec.TextAgentID > 0 {
					id := spec.TextAgentID
					return &id
				}
				return nil
			}(),
			AIProvider: spec.Provider,
			StartedAt:  &now,
			AnsweredAt: &now,
		},
		aiSpec: spec,
	}
	c.calls[callID] = entry
	c.mu.Unlock()

	tokenCaller, _, roomURL, err := c.rooms.CreateRoom(ctx, callID, callerID, agentID)
	if err != nil {
		c.mu.Lock()
		delete(c.calls, callID)
		c.mu.Unlock()
		callLogInfof("call trace: controller direct_ai create_room_fail call=%d err=%v", callID, err)
		return 0, "", "", fmt.Errorf("create room: %w", err)
	}
	c.mu.Lock()
	entry.tokenCaller = tokenCaller
	entry.roomURL = roomURL
	c.mu.Unlock()

	// 持久化 call record（与 Invite 一致）
	if err := c.persist.Create(ctx, &entry.record); err != nil {
		_ = c.rooms.CloseRoom(ctx, callID)
		c.mu.Lock()
		delete(c.calls, callID)
		c.mu.Unlock()
		return 0, "", "", fmt.Errorf("persist call: %w", err)
	}

	if c.bridge != nil {
		spec.CallerID = callerID
		spec.SessionID = sessionID // 跨通话 dialog 连续性
		if err := c.bridge.StartBridge(ctx, callID, spec); err != nil {
			_ = c.rooms.CloseRoom(ctx, callID)
			c.mu.Lock()
			delete(c.calls, callID)
			c.mu.Unlock()
			endedAt := time.Now()
			if perr := c.persist.UpdateEnd(ctx, callID, model.CallStateError, "bridge_start_failed", endedAt, nil); perr != nil {
				callLogInfof("call trace: direct_ai_call persist bridge failure error call=%d err=%v", callID, perr)
			}
			return 0, "", "", fmt.Errorf("start bridge: %w", err)
		}
	}
	// AI 通话必须录音；Egress 不可用时保持通话可用并记录日志。
	if c.recorder != nil && c.recorder.Enabled() {
		if egressID, rerr := c.recorder.StartRecording(ctx, callID); rerr == nil && egressID != "" {
			c.mu.Lock()
			if e, ok := c.calls[callID]; ok {
				e.egressID = egressID
			}
			c.mu.Unlock()
		} else if rerr != nil {
			callLogInfof("call trace: direct_ai_call start_recording error call=%d err=%v", callID, rerr)
		}
	}
	// P0 护栏：直拨 AI 通话超时保护。
	// 如果 agent 配置了 MaxCallSeconds 则用配置值，否则用 10 分钟兜底。
	// 防止 bridge 卡死导致 callEntry 永不被清理（caller busy 锁死用户）。
	timeout := defaultDirectAICallTimeout
	if spec.MaxCallSeconds > 0 {
		timeout = time.Duration(spec.MaxCallSeconds) * time.Second
	}
	c.mu.Lock()
	if e, ok := c.calls[callID]; ok {
		e.timer = c.trackedAfterFunc(timeout, func() {
			callLogInfof("call trace: direct_ai_call timeout call=%d timeout=%s", callID, timeout)
			_ = c.endCall(context.Background(), callID, model.CallStateEnded, "call_timeout")
		})
	}
	c.mu.Unlock()

	callLogInfof("call trace: controller direct_ai_call delegated call=%d caller=%d agent=%d room_url=%s timeout=%s", callID, callerID, agentID, roomURL, timeout)
	return callID, tokenCaller, roomURL, nil
}

// Takeover 真人接管：静音 AI（保留 session，连接/上下文不断），接管期间 AI 持续静默。
func (c *Controller) Takeover(ctx context.Context, callID, calleeID int64) error {
	callLogInfof("call trace: controller takeover begin call=%d callee=%d", callID, calleeID)
	c.mu.Lock()
	entry, ok := c.calls[callID]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("call not found: %d", callID)
	}
	if entry.record.State != model.CallStateAIDelegated {
		c.mu.Unlock()
		return fmt.Errorf("call not ai_delegated, state=%d", entry.record.State)
	}
	if entry.record.CalleeID != calleeID {
		c.mu.Unlock()
		return fmt.Errorf("callee mismatch")
	}
	entry.record.State = model.CallStateHumanActive
	entry.record.DelegationMode = model.CallDelegationMixed // 曾经 AI 托管过
	callerID := entry.record.CallerID
	c.mu.Unlock()

	// 静音 AI：保留 session（连接/dialog 上下文不断），接管期间持续静默。
	if c.bridge != nil {
		if err := c.bridge.MuteBridge(ctx, callID); err != nil {
			// 回滚：MuteBridge 失败，AI 实际未静音，恢复 AI_DELEGATED
			c.mu.Lock()
			if e, exists := c.calls[callID]; exists {
				e.record.State = model.CallStateAIDelegated
				e.record.DelegationMode = model.CallDelegationAIDelegated
			}
			c.mu.Unlock()
			return fmt.Errorf("mute bridge: %w", err)
		}
	}
	if err := c.persist.UpdateHandover(ctx, callID, model.CallHandoverEvent{
		Action: "takeover", ActorID: calleeID, CreatedAt: time.Now(),
	}, model.CallStateHumanActive, model.CallDelegationMixed); err != nil {
		callLogInfof("call trace: persist takeover error call=%d err=%v", callID, err)
	}

	// 通知 caller：B 已接管，切回真人通话
	c.notify(callerID, "call:state", map[string]any{
		"call_id": fmt.Sprintf("%d", callID),
		"mode":    "human_active",
	})
	callLogInfof("call trace: controller takeover done call=%d callee=%d", callID, calleeID)
	return nil
}

// HandBack 处理 B 将通话交回给 AI（HUMAN_ACTIVE → AI_DELEGATED）。
func (c *Controller) HandBack(ctx context.Context, callID, calleeID int64) error {
	callLogInfof("call trace: controller hand_back begin call=%d callee=%d", callID, calleeID)
	c.mu.Lock()
	entry, ok := c.calls[callID]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("call not found: %d", callID)
	}
	if entry.record.State != model.CallStateHumanActive {
		c.mu.Unlock()
		return fmt.Errorf("call not human_active, state=%d", entry.record.State)
	}
	if entry.record.CalleeID != calleeID {
		c.mu.Unlock()
		return fmt.Errorf("callee mismatch")
	}
	entry.record.State = model.CallStateAIDelegated
	entry.record.DelegationMode = model.CallDelegationMixed
	callerID := entry.record.CallerID
	rebuildSpec := entry.aiSpec
	rebuildSpec.CallerID = entry.record.CallerID
	rebuildSpec.SessionID = entry.record.SessionID
	c.mu.Unlock()

	// 恢复 AI 发声。接管时 session 未关闭（仅静音），此处直接 unmute，
	// dialog 上下文连续，AI 接着之前的对话继续，无需重建。
	// unmute 失败（如接管期间 Provider 侧 session 已死）则降级重建：
	// Interrupt 挂起旧桥（handover 路径，抑制 BRIDGE_EXIT）后用原 spec 重新起桥，
	// dialog_id 取 session_id，Provider 侧上下文仍连续。
	if c.bridge != nil {
		if err := c.bridge.UnmuteBridge(ctx, callID); err != nil {
			callLogInfof("call trace: hand_back unmute failed call=%d err=%v, rebuilding bridge", callID, err)
			_ = c.bridge.InterruptBridge(ctx, callID)
			if rerr := c.bridge.StartBridge(ctx, callID, rebuildSpec); rerr != nil {
				// 回滚：Bridge 未恢复，恢复 HumanActive
				c.mu.Lock()
				if e, exists := c.calls[callID]; exists {
					e.record.State = model.CallStateHumanActive
					e.record.DelegationMode = model.CallDelegationMixed
				}
				c.mu.Unlock()
				return fmt.Errorf("unmute bridge: %v; rebuild bridge: %w", err, rerr)
			}
			callLogInfof("call trace: hand_back bridge rebuilt call=%d", callID)
		}
	}
	if err := c.persist.UpdateHandover(ctx, callID, model.CallHandoverEvent{
		Action: "hand_back", ActorID: calleeID, CreatedAt: time.Now(),
	}, model.CallStateAIDelegated, model.CallDelegationMixed); err != nil {
		callLogInfof("call trace: persist hand_back error call=%d err=%v", callID, err)
	}

	// 通知 caller：AI 已重新接管
	c.notify(callerID, "call:state", map[string]any{
		"call_id": fmt.Sprintf("%d", callID),
		"mode":    "ai_delegated",
	})
	callLogInfof("call trace: controller hand_back done call=%d callee=%d", callID, calleeID)
	return nil
}

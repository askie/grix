// Package callbridge 提供通过 NATS 控制 Voice Bridge Worker 的 BridgeManager 实现。
package callbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/nats-io/nats.go"
)

// NATS 主题与 voicebridge 包保持一致。
//
// 多节点路由：start 走广播主题（Python 侧 queue group 负载均衡，单节点处理），
// 回执携带 node_id；后续 mute/unmute/interrupt 及重建 start 发往节点定向主题
// <subject>.<node_id>，精确命中持有该通话 session 的节点。
const (
	subjectStart     = "voicebridge.control.start"
	subjectStop      = "voicebridge.control.stop"
	subjectInterrupt = "voicebridge.control.interrupt"
	subjectMute      = "voicebridge.control.mute"
	subjectUnmute    = "voicebridge.control.unmute"
)

// requestTimeout 是等待 voicebridge 确认的超时时间（var 以便测试注入短超时）。
var requestTimeout = 15 * time.Second

// NATSBridgeManager 通过 NATS 向 voicebridge 进程发送控制指令。
// 实现 call.BridgeManager 接口。
type NATSBridgeManager struct {
	nc *nats.Conn

	mu sync.Mutex
	// nodes 记录 call_id → 持有该通话 bridge 的 voicebridge 节点。
	// 通话的全部控制指令都由持有 callEntry 的本进程发出（ws 节点亲和），
	// 因此归属表放进程内存即可，无需外部存储。
	nodes map[int64]string
}

// NewNATSBridgeManager 创建 NATSBridgeManager。nc 为 nil 时退化为 noop（voicebridge 未部署时不影响主流程）。
func NewNATSBridgeManager(nc *nats.Conn) *NATSBridgeManager {
	return &NATSBridgeManager{nc: nc, nodes: make(map[int64]string)}
}

var _ call.BridgeManager = (*NATSBridgeManager)(nil)

// bridgeAck 是 voicebridge 的确认回执。node_id 用于建立 call→节点 归属。
type bridgeAck struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error"`
	NodeID string `json:"node_id"`
}

func (m *NATSBridgeManager) nodeOf(callID int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nodes[callID]
}

func (m *NATSBridgeManager) setNode(callID int64, node string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[callID] = node
}

func (m *NATSBridgeManager) clearNode(callID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.nodes, callID)
}

// nodeSubject 返回节点定向主题；node 为空时退化为广播主题（无归属信息时的回退路径）。
func nodeSubject(base, node string) string {
	if node == "" {
		return base
	}
	return base + "." + node
}

// requestDirected 向通话归属节点发送定向 Request；无归属时走广播主题。
// 定向请求失败视节点为不可用，清除归属（后续 start 回退 queue group 由存活节点接管）。
func (m *NATSBridgeManager) requestDirected(op, base string, callID int64, data []byte) (*nats.Msg, error) {
	node := m.nodeOf(callID)
	msg, err := m.nc.Request(nodeSubject(base, node), data, requestTimeout)
	if err != nil && node != "" {
		logger.L.Warnf("call trace: bridge %s directed node=%s unreachable call=%d err=%v", op, node, callID, err)
		m.clearNode(callID)
	}
	return msg, err
}

// buildStartPayload 构造 voicebridge.control.start 的消息体（含 BYOK 配置透传）。
func buildStartPayload(callID int64, spec call.VoiceBridgeSpec) ([]byte, error) {
	return json.Marshal(map[string]any{
		"call_id":          callID,
		"session_id":       spec.SessionID,
		"agent_id":         spec.AgentID,
		"caller_id":        spec.CallerID,
		"owner_id":         spec.OwnerID, // >0 时 voicebridge 接入主人插话音频（其语音不落 IM）
		"voice_provider":   spec.Provider,
		"model":            spec.Model,
		"endpoint":         spec.Endpoint,
		"api_key":          spec.APIKey,
		"system_prompt":    spec.SystemPrompt,
		"voice":            spec.Voice,
		"language":         spec.Language,
		"opening":          spec.Opening,
		"max_call_seconds": spec.MaxCallSeconds, // Python 侧 bridge 存活上限随 agent 配置对齐
		"relay_mode":       spec.RelayMode,      // true=语音大脑传声筒：豆包只念过场语 + 文字回复走事件300逐字念回
	})
}

// parseAck 解析 voicebridge 的确认回执；ok=false 时返回错误。
func parseAck(op string, data []byte) error {
	var ack bridgeAck
	if err := json.Unmarshal(data, &ack); err != nil || ack.OK {
		return nil // 解析失败按旧行为放行（兼容裸 ok 回执）
	}
	if ack.Error != "" {
		return fmt.Errorf("bridge %s rejected: %s", op, ack.Error)
	}
	return fmt.Errorf("bridge %s rejected", op)
}

func (m *NATSBridgeManager) StartBridge(_ context.Context, callID int64, spec call.VoiceBridgeSpec) error {
	if m.nc == nil {
		return nil
	}
	logger.L.Infof("call trace: bridge start begin call=%d agent=%d provider=%s model=%s", callID, spec.AgentID, spec.Provider, spec.Model)
	data, err := buildStartPayload(callID, spec)
	if err != nil {
		logger.L.Errorf("call trace: bridge start marshal error call=%d err=%v", callID, err)
		return fmt.Errorf("marshal start: %w", err)
	}
	// 已有归属（接管交回的重建）时定向原节点——旧 room 引用在该节点的
	// _handover_rooms，必须由它断开，否则新节点进房撞 DuplicateIdentity；
	// 定向超时（节点已下线，旧 room 已随进程消亡）则回退广播 queue group 由存活节点接管。
	hadNode := m.nodeOf(callID) != ""
	msg, err := m.requestDirected("start", subjectStart, callID, data)
	if err != nil && hadNode {
		msg, err = m.nc.Request(subjectStart, data, requestTimeout)
	}
	if err != nil {
		logger.L.Errorf("call trace: bridge start nats error call=%d err=%v", callID, err)
		return fmt.Errorf("bridge start request: %w", err)
	}
	// worker 显式拒绝（缺字段 / provider 不支持等）时视为失败，避免"通话已起但 AI 静默"。
	var ack bridgeAck
	if e := json.Unmarshal(msg.Data, &ack); e == nil {
		if !ack.OK {
			m.clearNode(callID)
			if ack.Error != "" {
				logger.L.Warnf("call trace: bridge start rejected call=%d error=%s", callID, ack.Error)
				return fmt.Errorf("bridge start rejected: %s", ack.Error)
			}
			logger.L.Warnf("call trace: bridge start rejected call=%d (no detail)", callID)
			return fmt.Errorf("bridge start rejected")
		}
		if ack.NodeID != "" {
			m.setNode(callID, ack.NodeID)
		}
	}
	logger.L.Infof("call trace: bridge start ok call=%d node=%s", callID, m.nodeOf(callID))
	return nil
}

func (m *NATSBridgeManager) StopBridge(_ context.Context, callID int64) error {
	if m.nc == nil {
		return nil
	}
	// stop 是无应答广播：非 owner 节点查不到本地 session 自动忽略。通话终结，清归属。
	m.clearNode(callID)
	data, _ := json.Marshal(map[string]any{"call_id": callID})
	if err := m.nc.Publish(subjectStop, data); err != nil {
		logger.L.Warnf("call trace: bridge stop error call=%d err=%v", callID, err)
		return err
	}
	logger.L.Infof("call trace: bridge stop published call=%d", callID)
	return nil
}

func (m *NATSBridgeManager) InterruptBridge(_ context.Context, callID int64) error {
	if m.nc == nil {
		return nil
	}
	data, _ := json.Marshal(map[string]any{"call_id": callID})
	// Interrupt 需要确认（接管时 AI 必须真正停止）。成功时归属保留——随后的重建 start 仍需定向同节点。
	_, err := m.requestDirected("interrupt", subjectInterrupt, callID, data)
	if err != nil {
		logger.L.Errorf("call trace: bridge interrupt error call=%d err=%v", callID, err)
		return fmt.Errorf("bridge interrupt request: %w", err)
	}
	logger.L.Infof("call trace: bridge interrupt ok call=%d", callID)
	return nil
}

// MuteBridge 静音 AI（接管）：保留 session，停止喂访客音频 + 停止发声。需确认。
func (m *NATSBridgeManager) MuteBridge(_ context.Context, callID int64) error {
	if m.nc == nil {
		return nil
	}
	data, _ := json.Marshal(map[string]any{"call_id": callID})
	// Mute 需要确认（接管时 AI 必须真正停止发声）
	msg, err := m.requestDirected("mute", subjectMute, callID, data)
	if err != nil {
		logger.L.Errorf("call trace: bridge mute error call=%d err=%v", callID, err)
		return fmt.Errorf("bridge mute request: %w", err)
	}
	if err := parseAck("mute", msg.Data); err != nil {
		logger.L.Errorf("call trace: bridge mute nack call=%d err=%v", callID, err)
		return err
	}
	logger.L.Infof("call trace: bridge mute ok call=%d", callID)
	return nil
}

// UnmuteBridge 恢复 AI 听 + 发声（交回）。需确认。
func (m *NATSBridgeManager) UnmuteBridge(_ context.Context, callID int64) error {
	if m.nc == nil {
		return nil
	}
	data, _ := json.Marshal(map[string]any{"call_id": callID})
	msg, err := m.requestDirected("unmute", subjectUnmute, callID, data)
	if err != nil {
		logger.L.Errorf("call trace: bridge unmute error call=%d err=%v", callID, err)
		return fmt.Errorf("bridge unmute request: %w", err)
	}
	if err := parseAck("unmute", msg.Data); err != nil {
		logger.L.Errorf("call trace: bridge unmute nack call=%d err=%v", callID, err)
		return err
	}
	logger.L.Infof("call trace: bridge unmute ok call=%d", callID)
	return nil
}

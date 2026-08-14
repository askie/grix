package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/gateway/provisioning"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// 中转开关服务端化（migration 111）的 WS 对齐协议，设计
// docs/frontend/gateway_relay_mobile_design.md §2.4（v5：事件驱动，无心跳）。
// 本文件只做协议适配：身份取自连接认证结果（conn.ownerID/conn.agentID，不可冒充）、
// seq 关联应答、上行限流；业务规则全部在 api/service/gateway_relay_sync.go。

// RelayStateSyncRequestPayload 是 connector 建连后主动对齐的上行请求（路径 A，可靠性主路径）。
// LocalEnabled  nil 表示 connector 未上报本机名单（新装无本地名单概念），服务端不建 state 行；
// 非 nil 且 state 行不存在时以首个上报设备的本机名单落 initial desired（行已存在则忽略）。
type RelayStateSyncRequestPayload struct {
	LocalEnabled *bool  `json:"local_enabled,omitempty"`
	LocalModel   string `json:"local_model,omitempty"`
}

// RelayStateCredentialPayload 是 sync 顺带重签时下发的专属虚拟Key（明文凭证只出现在
// 这一次 WS 应答里，不落日志、不落 state 表）。
type RelayStateCredentialPayload struct {
	APIKey           string `json:"api_key"`
	AnthropicBaseURL string `json:"anthropic_base_url"`
	OpenAIBaseURL    string `json:"openai_base_url"`
}

// RelayStateSyncResultPayload 是 relay_state_sync_request 的应答（seq 关联）。
// Enabled/Model/Revision 是服务端持有的 desired（唯一权威期望态）；connector 据此
// 本地执行接管/解除后，上行 relay_state_report 回执。
type RelayStateSyncResultPayload struct {
	Status    string `json:"status"` // ok / failed
	ErrorCode string `json:"error_code,omitempty"`
	ErrorMsg  string `json:"error_msg,omitempty"`

	Enabled  bool   `json:"enabled"`
	Model    string `json:"model"`
	Revision int64  `json:"revision"`

	Credential      *RelayStateCredentialPayload `json:"credential,omitempty"`
	ModelAutoFilled bool                         `json:"model_auto_filled,omitempty"`
}

// RelayStateReportPayload 是 connector 的事件驱动回执（sync 后回执 / 本地状态变化后上报，
// 设计 §2.4 路径 A 尾段与路径 C）。Revision 必须携带其对应的 desired 版本号，
// 服务端只接受 revision >= 当前 desired revision 的报告（回执幂等）。
type RelayStateReportPayload struct {
	Applied   bool   `json:"applied"`
	Revision  int64  `json:"revision"`
	ErrorCode string `json:"error_code,omitempty"`
}

// relay_state_sync_request 上行限流：sync 可能触发 Key 重签，必须防异常 connector
// 高频刷签发（设计 §2.4 限流防刷）。进程内滑动窗口（多副本各自独立，与
// auth_fail_limiter 同一取舍），按 (ownerID, agentID) 10 次/分钟。
const (
	relayStateSyncLimit  = 10
	relayStateSyncWindow = time.Minute
)

// relayStateSyncLimiter 全局限流器实例；var 便于测试替换。
var relayStateSyncLimiter = newRelayStateSyncLimiter()

type relayStateSyncLimiterImpl struct {
	mu      sync.Mutex
	entries map[string][]time.Time
	now     func() time.Time // 便于测试注入时钟
}

func newRelayStateSyncLimiter() *relayStateSyncLimiterImpl {
	return &relayStateSyncLimiterImpl{
		entries: make(map[string][]time.Time),
		now:     time.Now,
	}
}

// allow 计入一次 sync 请求并返回是否放行（窗口内第 limit+1 次起拒绝）。
func (l *relayStateSyncLimiterImpl) allow(ownerID, agentID int64) bool {
	if l == nil || ownerID <= 0 || agentID <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	key := fmt.Sprintf("%d|%d", ownerID, agentID)
	cutoff := now.Add(-relayStateSyncWindow)
	kept := l.entries[key][:0]
	for _, ts := range l.entries[key] {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= relayStateSyncLimit {
		l.entries[key] = kept
		return false
	}
	l.entries[key] = append(kept, now)
	return true
}

// handleRelayStateSyncRequest 处理 connector 上线后的中转状态对齐（路径 A）。
// 应答走本连接 seq 关联，无跨节点问题；agent 离线时 Redis 广播会丢的场景全部由这条兜住。
func (m *Manager) handleRelayStateSyncRequest(conn *agentConn, pkt *protocol.Packet) {
	if conn == nil || pkt == nil {
		return
	}
	m.refreshAgentLease(conn)

	fail := func(code, msg string) {
		conn.sendPayload(protocol.CmdRelayStateSyncResult, pkt.Seq, RelayStateSyncResultPayload{
			Status:    "failed",
			ErrorCode: code,
			ErrorMsg:  msg,
		})
	}

	var payload RelayStateSyncRequestPayload
	if len(pkt.Payload) > 0 {
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			conn.recordViolation()
			fail(strconv.Itoa(protocol.CodeInvalidPayload), "invalid payload")
			return
		}
	}

	if !relayStateSyncLimiter.allow(conn.ownerID, conn.agentID) {
		logger.L.Warnf("relay_state_sync_request rate limited agent=%d owner=%d", conn.agentID, conn.ownerID)
		fail(strconv.Itoa(errcode.ErrRateLimited.BizCode), errcode.ErrRateLimited.Msg)
		return
	}

	resp, ec := service.GatewayRelayStateSync(conn.ownerID, conn.agentID, payload.LocalEnabled, payload.LocalModel)
	if ec != nil {
		logger.L.Warnf("relay_state_sync_request failed agent=%d owner=%d biz=%d msg=%s",
			conn.agentID, conn.ownerID, ec.BizCode, ec.Msg)
		fail(strconv.Itoa(ec.BizCode), ec.Msg)
		return
	}

	result := RelayStateSyncResultPayload{
		Status:          "ok",
		Enabled:         resp.Enabled,
		Model:           resp.Model,
		Revision:        resp.Revision,
		ModelAutoFilled: resp.ModelAutoFilled,
	}
	if resp.Credential != nil {
		result.Credential = &RelayStateCredentialPayload{
			APIKey:           resp.Credential.VirtualKey,
			AnthropicBaseURL: resp.Credential.AnthropicBaseURL,
			OpenAIBaseURL:    resp.Credential.OpenAIBaseURL,
		}
	}
	conn.sendPayload(protocol.CmdRelayStateSyncResult, pkt.Seq, result)
}

// handleRelayStateReport 处理 connector 的事件驱动回执（无应答，幂等丢弃过期 revision）。
// flag 关闭、无 state 行、revision 过期等情况 service 层一律静默丢弃，不当协议错误处理。
func (m *Manager) handleRelayStateReport(conn *agentConn, pkt *protocol.Packet) {
	if conn == nil || pkt == nil {
		return
	}
	m.refreshAgentLease(conn)

	var payload RelayStateReportPayload
	if len(pkt.Payload) > 0 {
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			conn.recordViolation()
			conn.sendPayload("error", pkt.Seq, SendNackPayload{
				Code: 4001,
				Msg:  "invalid relay_state_report payload",
			})
			return
		}
	}

	written, ec := service.GatewayRelayStateReport(conn.ownerID, conn.agentID, payload.Applied, payload.Revision)
	if ec != nil {
		logger.L.Warnf("relay_state_report failed agent=%d owner=%d biz=%d msg=%s",
			conn.agentID, conn.ownerID, ec.BizCode, ec.Msg)
		return
	}
	logger.L.Infof("relay_state_report agent=%d owner=%d applied=%v revision=%d written=%v",
		conn.agentID, conn.ownerID, payload.Applied, payload.Revision, written)
}

// handleBroadcastApplyRelayState 在每个 ws 节点订阅到 apply_relay_state 广播时执行
// （路径 B：用户操作触发即时下发）。与 configure_gateway_provider 同一模式：广播在每个
// 节点都执行，先解析 agent.OwnerID 再按 (agentID, ownerID) 精确路由，持有该连接的节点
// 自然命中、其余节点 SendLocalActionForOwner 返回 false，保持"恰好一份"；agent 离线时
// 全部节点落空即静默丢弃，connector 下次上线走 sync 对齐兜底（设计 §2.4）。
func handleBroadcastApplyRelayState(cfg provisioning.RelayStateApplyConfig) {
	mgr := GetGlobalManager()
	if mgr == nil || cfg.AgentID <= 0 {
		return
	}
	if store.DB == nil {
		logger.L.Warnf("apply_relay_state skipped agent=%d: db unavailable, cannot resolve agent owner", cfg.AgentID)
		return
	}
	var agent model.Agent
	if err := store.DB.Select("id", "owner_id").First(&agent, cfg.AgentID).Error; err != nil || agent.OwnerID <= 0 {
		logger.L.Warnf("apply_relay_state skipped agent=%d: resolve owner failed err=%v owner=%d", cfg.AgentID, err, agent.OwnerID)
		return
	}
	actionID := fmt.Sprintf("apply-relay-state:%d", snowflake.GenID())
	action := protocol.LocalActionPayload{
		ActionID:   actionID,
		ActionType: provisioning.ApplyRelayStateActionType,
		Params: map[string]any{
			"enabled":  cfg.Enabled,
			"model":    cfg.Model,
			"revision": cfg.Revision,
		},
	}
	// 登记 pending（revision 记进 referenceID 兜底）以便 local_action_result 到达时写回
	// applied；发送失败（离线/老 connector 未声明能力位/连接不权威）立即撤销登记。
	pending := &pendingLocalAction{
		actionID:    actionID,
		kind:        provisioning.ApplyRelayStateActionType,
		agentID:     cfg.AgentID,
		ownerID:     agent.OwnerID,
		actionType:  provisioning.ApplyRelayStateActionType,
		referenceID: strconv.FormatInt(cfg.Revision, 10),
		// 覆盖 connector 侧停启 agent 进程的开销（实测常超 25s 默认超时）；
		// 超时后 timeoutPendingLocalAction 会把 applied 置 false（期望未达成）。
		timeoutMs: 120000,
	}
	mgr.storePendingLocalAction(pending)
	if mgr.SendLocalActionForOwner(cfg.AgentID, agent.OwnerID, action) {
		logger.L.Infof("apply_relay_state sent to agent=%d owner=%d revision=%d node=%s",
			cfg.AgentID, agent.OwnerID, cfg.Revision, mgr.getNodeID())
		return
	}
	mgr.deletePendingLocalAction(actionID)
}

// handleApplyRelayStateResult 处理 apply_relay_state 的 local_action_result：
// ok=true 置 applied=true，ok=false（期望未达成）置 false；revision 优先取回执带回值
// （契约 {ok, error_code?, revision}），缺失时回退下发时登记的 revision。
// 与 relay_state_report 同一套 revision 新鲜度门槛（service 层过期丢弃）。
func (m *Manager) handleApplyRelayStateResult(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) {
	if pending == nil {
		return
	}
	revision := int64(0)
	if result := localActionResultObject(payload.Result); result != nil {
		switch v := result["revision"].(type) {
		case float64:
			revision = int64(v)
		case json.Number:
			revision, _ = v.Int64()
		}
	}
	if revision <= 0 {
		revision, _ = strconv.ParseInt(strings.TrimSpace(pending.referenceID), 10, 64)
	}
	ok := strings.TrimSpace(payload.Status) == "ok"
	written, ec := service.GatewayRelayStateApplyResult(pending.ownerID, pending.agentID, revision, ok)
	if ec != nil {
		logger.L.Warnf("apply_relay_state result write failed agent=%d owner=%d biz=%d msg=%s",
			pending.agentID, pending.ownerID, ec.BizCode, ec.Msg)
		return
	}
	logger.L.Infof("apply_relay_state result agent=%d owner=%d ok=%v revision=%d written=%v",
		pending.agentID, pending.ownerID, ok, revision, written)
}

// agentHasAnyOnlineRoute 判断该 agent 是否还存在在线权威连接（跨节点，读 Redis 路由表）：
// 主路由 key 或 owner 集合里任一 owner 路由 key 有值即为在线。断连钩子在本地连接表已空
// 之后调用，确认跨节点也没有残留连接才把 relay applied 置 false。
func agentHasAnyOnlineRoute(agentID int64) bool {
	if agentID <= 0 || store.RDB == nil {
		return false
	}
	ctx := context.Background()
	if v, err := store.RDB.Get(ctx, agentRouteKey(agentID)).Result(); err == nil && strings.TrimSpace(v) != "" {
		return true
	}
	owners, err := store.RDB.SMembers(ctx, agentRouteOwnerSetKey(agentID)).Result()
	if err != nil {
		return false
	}
	for _, o := range owners {
		ownerID, perr := strconv.ParseInt(strings.TrimSpace(o), 10, 64)
		if perr != nil || ownerID <= 0 {
			continue
		}
		if v, gerr := store.RDB.Get(ctx, agentRouteKeyForOwner(agentID, ownerID)).Result(); gerr == nil && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

package agentapi

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/gateway/provisioning"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// relay_state_sync_request / relay_state_report / apply_relay_state 的 WS 协议适配层测试
// （设计 §2.4，v5：事件驱动，无心跳）。业务规则分支在 api/service/gateway_relay_sync_test.go。

func setRelayStateFlagForTest(t *testing.T, enabled bool) {
	t.Helper()
	original := config.C.Gateway.RelayStateEnabled
	config.C.Gateway.RelayStateEnabled = enabled
	t.Cleanup(func() { config.C.Gateway.RelayStateEnabled = original })
}

func newRelayStateConn(agentID, ownerID int64) *agentConn {
	return &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		capabilities: []string{"local_action_v1"},
		localActions: []string{provisioning.ApplyRelayStateActionType},
		send:         make(chan []byte, 8),
		done:         make(chan struct{}),
	}
}

func relayStatePacket(t *testing.T, cmd string, seq int64, payload any) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return &protocol.Packet{Cmd: cmd, Seq: seq, Payload: raw}
}

func readRelayStateSyncResult(t *testing.T, conn *agentConn) (int64, RelayStateSyncResultPayload) {
	t.Helper()
	select {
	case raw := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("unmarshal outbound packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdRelayStateSyncResult {
			t.Fatalf("expected cmd=%s, got %s", protocol.CmdRelayStateSyncResult, pkt.Cmd)
		}
		var result RelayStateSyncResultPayload
		if err := json.Unmarshal(pkt.Payload, &result); err != nil {
			t.Fatalf("unmarshal result payload: %v", err)
		}
		return pkt.Seq, result
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for relay_state_sync_result")
		return 0, RelayStateSyncResultPayload{}
	}
}

// 首报同步：身份取自连接认证结果，落 initial desired，enabled=true 且无有效 Key 时
// 顺带签发凭证随 seq 关联的应答下发。
func TestHandleRelayStateSyncRequest_SeedsDesiredAndRepliesSameSeq(t *testing.T) {
	setupRelayCredentialTest(t)
	setRelayStateFlagForTest(t, true)
	agent := model.Agent{ID: 8501, AgentName: "relay-agent", OwnerID: 8500, AgentClientType: model.AgentClientTypeClaude}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := newRelayStateConn(8501, 8500)

	enabled := true
	mgr.handleRelayStateSyncRequest(conn, relayStatePacket(t, protocol.CmdRelayStateSyncRequest, 42,
		RelayStateSyncRequestPayload{LocalEnabled: &enabled}))

	seq, result := readRelayStateSyncResult(t, conn)
	if seq != 42 {
		t.Fatalf("expected response seq=42 (request correlation), got %d", seq)
	}
	if result.Status != "ok" || !result.Enabled || result.Revision != 1 {
		t.Fatalf("unexpected sync result: %+v", result)
	}
	if result.Credential == nil || result.Credential.APIKey == "" {
		t.Fatalf("expected reissued credential with plaintext api_key, got %+v", result.Credential)
	}

	row, err := store.GetGatewayAgentRelayState(8501)
	if err != nil || !row.Enabled || row.Revision != 1 {
		t.Fatalf("expected initial desired persisted, row=%+v err=%v", row, err)
	}
}

// 上行限流：(ownerID, agentID) 10 次/分钟，超限应答 failed + 限流业务码——
// sync 可能触发 Key 重签，必须防异常 connector 高频刷签发。
func TestHandleRelayStateSyncRequest_RateLimited(t *testing.T) {
	setupRelayCredentialTest(t)
	setRelayStateFlagForTest(t, true)
	agent := model.Agent{ID: 8502, AgentName: "relay-agent", OwnerID: 8500, AgentClientType: model.AgentClientTypeClaude}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	original := relayStateSyncLimiter
	relayStateSyncLimiter = newRelayStateSyncLimiter()
	t.Cleanup(func() { relayStateSyncLimiter = original })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := newRelayStateConn(8502, 8500)

	enabled := true
	for i := 0; i < relayStateSyncLimit; i++ {
		mgr.handleRelayStateSyncRequest(conn, relayStatePacket(t, protocol.CmdRelayStateSyncRequest, int64(i+1),
			RelayStateSyncRequestPayload{LocalEnabled: &enabled}))
		_, result := readRelayStateSyncResult(t, conn)
		if result.Status != "ok" {
			t.Fatalf("sync #%d within limit must succeed, got %+v", i+1, result)
		}
	}

	mgr.handleRelayStateSyncRequest(conn, relayStatePacket(t, protocol.CmdRelayStateSyncRequest, 99,
		RelayStateSyncRequestPayload{LocalEnabled: &enabled}))
	seq, result := readRelayStateSyncResult(t, conn)
	if seq != 99 || result.Status != "failed" {
		t.Fatalf("expected rate-limited failure on seq=99, got seq=%d %+v", seq, result)
	}
	want := strconv.Itoa(errcode.ErrRateLimited.BizCode)
	if result.ErrorCode != want {
		t.Fatalf("expected error_code=%s, got %s", want, result.ErrorCode)
	}
}

// feature flag 关闭：sync 应答 failed + 26008，链路停用。
func TestHandleRelayStateSyncRequest_FlagDisabled(t *testing.T) {
	setupRelayCredentialTest(t)
	setRelayStateFlagForTest(t, false)
	agent := model.Agent{ID: 8503, AgentName: "relay-agent", OwnerID: 8500, AgentClientType: model.AgentClientTypeClaude}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := newRelayStateConn(8503, 8500)

	enabled := true
	mgr.handleRelayStateSyncRequest(conn, relayStatePacket(t, protocol.CmdRelayStateSyncRequest, 7,
		RelayStateSyncRequestPayload{LocalEnabled: &enabled}))

	seq, result := readRelayStateSyncResult(t, conn)
	if seq != 7 || result.Status != "failed" {
		t.Fatalf("expected failed on seq=7, got seq=%d %+v", seq, result)
	}
	want := strconv.Itoa(errcode.ErrGatewayRelayStateDisabled.BizCode)
	if result.ErrorCode != want {
		t.Fatalf("expected error_code=%s, got %s", want, result.ErrorCode)
	}
}

// report 回执：新鲜 revision 写回 applied/applied_at；过期 revision 静默丢弃。
func TestHandleRelayStateReport_WritesAppliedAndDropsStale(t *testing.T) {
	setupRelayCredentialTest(t)
	setRelayStateFlagForTest(t, true)
	agent := model.Agent{ID: 8504, AgentName: "relay-agent", OwnerID: 8500, AgentClientType: model.AgentClientTypeClaude}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	// desired revision 升到 2。
	if _, ec := service.GatewaySetAgentRelay(context.Background(), 8500, 8504, true, "", nil); ec != nil {
		t.Fatalf("set desired: %+v", ec)
	}
	if _, ec := service.GatewaySetAgentRelay(context.Background(), 8500, 8504, true, "", nil); ec != nil {
		t.Fatalf("bump desired: %+v", ec)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := newRelayStateConn(8504, 8500)

	// 过期 revision=1：丢弃。
	mgr.handleRelayStateReport(conn, relayStatePacket(t, protocol.CmdRelayStateReport, 1,
		RelayStateReportPayload{Applied: true, Revision: 1}))
	row, err := store.GetGatewayAgentRelayState(8504)
	if err != nil || row.Applied {
		t.Fatalf("stale report must not write applied, row=%+v err=%v", row, err)
	}

	// 当前 revision=2：写回。
	mgr.handleRelayStateReport(conn, relayStatePacket(t, protocol.CmdRelayStateReport, 2,
		RelayStateReportPayload{Applied: true, Revision: 2}))
	row, err = store.GetGatewayAgentRelayState(8504)
	if err != nil || !row.Applied || row.AppliedAt == nil {
		t.Fatalf("expected applied=true with applied_at, row=%+v err=%v", row, err)
	}
}

// 广播下发契约：apply_relay_state 的 local_action 携带 {enabled, model, revision}，
// 按 (agent, owner) 精确路由到声明了能力位的连接，并登记 pending 等待回执。
func TestHandleBroadcastApplyRelayState_SendsLocalAction(t *testing.T) {
	setupRelayCredentialTest(t)
	setRelayStateFlagForTest(t, true)
	agent := model.Agent{ID: 8505, AgentName: "relay-agent", OwnerID: 8500, AgentClientType: model.AgentClientTypeClaude}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := newRelayStateConn(8505, 8500)
	mgr.putConnForTest(conn)
	globalMu.Lock()
	prev := globalManager
	globalManager = mgr
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalManager = prev
		globalMu.Unlock()
	}()

	handleBroadcastApplyRelayState(provisioning.RelayStateApplyConfig{
		AgentID: 8505, Enabled: true, Model: "deepseek-v4-flash", Revision: 7,
	})

	var actionID string
	select {
	case raw := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("unmarshal outbound packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdLocalAction {
			t.Fatalf("expected cmd=%s, got %s", protocol.CmdLocalAction, pkt.Cmd)
		}
		var action protocol.LocalActionPayload
		if err := json.Unmarshal(pkt.Payload, &action); err != nil {
			t.Fatalf("unmarshal local_action payload: %v", err)
		}
		if action.ActionType != provisioning.ApplyRelayStateActionType {
			t.Fatalf("expected action_type=%s, got %s", provisioning.ApplyRelayStateActionType, action.ActionType)
		}
		if action.Params["enabled"] != true || action.Params["model"] != "deepseek-v4-flash" {
			t.Fatalf("unexpected params: %+v", action.Params)
		}
		if rev, ok := action.Params["revision"].(float64); !ok || int64(rev) != 7 {
			t.Fatalf("expected revision=7, got %+v", action.Params["revision"])
		}
		actionID = action.ActionID
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for apply_relay_state local_action")
	}

	// pending 已登记，等待 local_action_result 写回。
	mgr.localActionsMu.Lock()
	_, pendingOK := mgr.pendingLocalActions[actionID]
	mgr.localActionsMu.Unlock()
	if !pendingOK {
		t.Fatal("expected pending local action registered for apply_relay_state")
	}
}

// 老 connector 未声明 apply_relay_state 能力位：广播静默跳过（天然兼容），
// 不下发也不留 pending。
func TestHandleBroadcastApplyRelayState_SkipsUndeclaredConnector(t *testing.T) {
	setupRelayCredentialTest(t)
	setRelayStateFlagForTest(t, true)
	agent := model.Agent{ID: 8506, AgentName: "relay-agent", OwnerID: 8500, AgentClientType: model.AgentClientTypeClaude}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      8506,
		ownerID:      8500,
		capabilities: []string{"local_action_v1"},
		localActions: []string{"get_context"},
		send:         make(chan []byte, 8),
		done:         make(chan struct{}),
	}
	mgr.putConnForTest(conn)
	globalMu.Lock()
	prev := globalManager
	globalManager = mgr
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalManager = prev
		globalMu.Unlock()
	}()

	handleBroadcastApplyRelayState(provisioning.RelayStateApplyConfig{
		AgentID: 8506, Enabled: true, Model: "m", Revision: 3,
	})

	select {
	case raw := <-conn.send:
		t.Fatalf("old connector must not receive apply_relay_state, got %s", raw)
	case <-time.After(200 * time.Millisecond):
	}
	mgr.localActionsMu.Lock()
	pendingCount := len(mgr.pendingLocalActions)
	mgr.localActionsMu.Unlock()
	if pendingCount != 0 {
		t.Fatalf("failed send must not leave pending, got %d", pendingCount)
	}
}

// local_action_result 写回：apply_relay_state 的 ok 回执（带回 revision）把 applied
// 置 true；过期 revision 的回执被 service 层门槛丢弃。
func TestHandleLocalActionResult_ApplyRelayStateWritesApplied(t *testing.T) {
	setupRelayCredentialTest(t)
	setRelayStateFlagForTest(t, true)
	agent := model.Agent{ID: 8507, AgentName: "relay-agent", OwnerID: 8500, AgentClientType: model.AgentClientTypeClaude}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if _, ec := service.GatewaySetAgentRelay(context.Background(), 8500, 8507, true, "", nil); ec != nil {
		t.Fatalf("set desired: %+v", ec)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := newRelayStateConn(8507, 8500)
	mgr.putConnForTest(conn)
	globalMu.Lock()
	prev := globalManager
	globalManager = mgr
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalManager = prev
		globalMu.Unlock()
	}()

	// 广播下发（revision=1 与 desired 对齐）。
	handleBroadcastApplyRelayState(provisioning.RelayStateApplyConfig{
		AgentID: 8507, Enabled: true, Model: "", Revision: 1,
	})
	var action protocol.LocalActionPayload
	select {
	case raw := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("unmarshal outbound packet: %v", err)
		}
		if err := json.Unmarshal(pkt.Payload, &action); err != nil {
			t.Fatalf("unmarshal local_action: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for apply_relay_state local_action")
	}

	// connector 回执 {ok, revision}：写回 applied=true。
	mgr.handleLocalActionResult(conn, relayStatePacket(t, protocol.CmdLocalActionResult, 11,
		protocol.LocalActionResultPayload{
			ActionID: action.ActionID,
			Status:   "ok",
			Result:   map[string]any{"revision": 1},
		}))
	drainLocalActionAck(t, conn)
	row, err := store.GetGatewayAgentRelayState(8507)
	if err != nil || !row.Applied || row.AppliedAt == nil {
		t.Fatalf("expected applied=true after ok result, row=%+v err=%v", row, err)
	}

	// desired 升到 revision=2 后，先以 ok=false 的新鲜回执把 applied 置 false。
	if _, ec := service.GatewaySetAgentRelay(context.Background(), 8500, 8507, false, "", nil); ec != nil {
		t.Fatalf("bump desired: %+v", ec)
	}
	handleBroadcastApplyRelayState(provisioning.RelayStateApplyConfig{
		AgentID: 8507, Enabled: false, Model: "", Revision: 2,
	})
	action = readApplyRelayStateAction(t, conn)
	mgr.handleLocalActionResult(conn, relayStatePacket(t, protocol.CmdLocalActionResult, 12,
		protocol.LocalActionResultPayload{
			ActionID: action.ActionID,
			Status:   "failed",
			Result:   map[string]any{"revision": 2},
		}))
	drainLocalActionAck(t, conn)
	row, err = store.GetGatewayAgentRelayState(8507)
	if err != nil || row.Applied {
		t.Fatalf("expected applied=false after ok=false result, row=%+v err=%v", row, err)
	}

	// revision=1 的迟到回执（过期）：被 service 层门槛丢弃，applied 保持 false。
	handleBroadcastApplyRelayState(provisioning.RelayStateApplyConfig{
		AgentID: 8507, Enabled: false, Model: "", Revision: 2,
	})
	action = readApplyRelayStateAction(t, conn)
	mgr.handleLocalActionResult(conn, relayStatePacket(t, protocol.CmdLocalActionResult, 13,
		protocol.LocalActionResultPayload{
			ActionID: action.ActionID,
			Status:   "ok",
			Result:   map[string]any{"revision": 1},
		}))
	drainLocalActionAck(t, conn)
	row, err = store.GetGatewayAgentRelayState(8507)
	if err != nil || row.Applied {
		t.Fatalf("stale revision result must be dropped, row=%+v err=%v", row, err)
	}
}

func readApplyRelayStateAction(t *testing.T, conn *agentConn) protocol.LocalActionPayload {
	t.Helper()
	select {
	case raw := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("unmarshal outbound packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdLocalAction {
			t.Fatalf("expected cmd=%s, got %s", protocol.CmdLocalAction, pkt.Cmd)
		}
		var action protocol.LocalActionPayload
		if err := json.Unmarshal(pkt.Payload, &action); err != nil {
			t.Fatalf("unmarshal local_action: %v", err)
		}
		return action
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for apply_relay_state local_action")
		return protocol.LocalActionPayload{}
	}
}

func drainLocalActionAck(t *testing.T, conn *agentConn) {
	t.Helper()
	select {
	case <-conn.send:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for local_action_ack")
	}
}

// 断连钩子：最后一条权威连接断开时 applied 置 false；该 agent 还有其他权威连接
// 在线时不动（实际态由在线连接的下一次 sync/report 负责）。
func TestUnregister_LastDisconnectClearsApplied(t *testing.T) {
	setupRelayCredentialTest(t)
	setRelayStateFlagForTest(t, true)
	agent := model.Agent{ID: 8508, AgentName: "relay-agent", OwnerID: 8500, AgentClientType: model.AgentClientTypeClaude}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if _, ec := service.GatewaySetAgentRelay(context.Background(), 8500, 8508, true, "", nil); ec != nil {
		t.Fatalf("set desired: %+v", ec)
	}
	if err := store.SetGatewayAgentRelayStateApplied(8508, true, time.Now()); err != nil {
		t.Fatalf("seed applied: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	// 该 agent 还有另一条（共享）连接在线：断开其中一条，applied 保持。
	other := &agentConn{agentID: 8508, ownerID: 9999, send: make(chan []byte, 8), done: make(chan struct{})}
	conn := newRelayStateConn(8508, 8500)
	mgr.putConnForTest(other)
	mgr.putConnForTest(conn)
	mgr.unregister(conn)

	row, err := store.GetGatewayAgentRelayState(8508)
	if err != nil || !row.Applied {
		t.Fatalf("applied must stay while another authoritative conn is online, row=%+v err=%v", row, err)
	}

	// 最后一条断开：applied 置 false。
	mgr.unregister(other)
	row, err = store.GetGatewayAgentRelayState(8508)
	if err != nil || row.Applied {
		t.Fatalf("expected applied=false after last disconnect, row=%+v err=%v", row, err)
	}
}

// Redis 广播分发：apply_relay_state cmd 命中即消费。
func TestHandleRedisDispatch_RecognizesBroadcastApplyRelayState(t *testing.T) {
	payload, _ := json.Marshal(provisioning.RelayStateApplyConfig{AgentID: 1, Enabled: true, Revision: 1})
	if !HandleRedisDispatch(provisioning.RedisCmdApplyRelayState, payload) {
		t.Fatal("expected HandleRedisDispatch to handle broadcast apply_relay_state cmd")
	}
}

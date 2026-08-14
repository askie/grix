package agentapi

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/claude"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/gorilla/websocket"
)

// TestServeWS_DispatchAgentToSelfDoesNotDeadlockReadLoop 复现并防回归"agent 自派自死锁"：
// dispatch_agent 的目标 agent 就是发起请求的这条连接自己时，session_bind 的 local_action
// 会下发到同一条连接，其 local_action_result 必须由这条连接的读循环消费。若读循环同步
// 阻塞在 agent_invoke 处理里等待绑定结果，回包永远读不到，只能等 15s 绑定超时(4290)。
// agent_invoke 改为异步处理后，自派自应在远小于绑定超时的时间内以 code=0 完成。
func TestServeWS_DispatchAgentToSelfDoesNotDeadlockReadLoop(t *testing.T) {
	const (
		agentID = int64(91099)
		ownerID = int64(82099)
		apiKey  = "ak_test_self_dispatch_no_deadlock"
	)

	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	// actor 即目标：client_type=claude，走 binding 派发路径。
	seedAgentInvokeDispatchActor(t, testDB, ownerID, agentID, apiKey)
	seedAgentInvokeDispatchScope(t, agentID, agentscope.ScopeAgentDispatch)

	sendFn := func(ctx context.Context, req SendMessageReq) (*SendMessageResult, error) {
		return &SendMessageResult{MsgID: 13001}, nil
	}
	mgr := NewManager("", 30*time.Second, sendFn, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.adapterRegistry = agentadapter.NewRegistry()
	mgr.adapterRegistry.Register(claude.NewAdapter())

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id="+strconv.FormatInt(agentID, 10), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":         strconv.FormatInt(agentID, 10),
			"api_key":          apiKey,
			"client":           "claude-grix",
			"client_type":      model.AgentClientTypeClaude,
			"contract_version": 1,
			"host_type":        claude.Family,
			"host_version":     "1.2.0",
			"adapter_hint":     claude.AdapterID,
			"capabilities":     []string{"session_route", "local_action_v1", "agent_invoke"},
			"local_actions":    []string{"session_control", "claude_interaction_reply"},
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}
	var ack protocol.Packet
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}
	if ack.Cmd != "auth_ack" {
		t.Fatalf("expected auth_ack, got %s", ack.Cmd)
	}
	var authAck AuthAckPayload
	if err := json.Unmarshal(ack.Payload, &authAck); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if authAck.Code != 0 {
		t.Fatalf("expected auth success, got code=%d msg=%s", authAck.Code, authAck.Msg)
	}

	// 自派自：目标 agent_id 就是本连接的 agent。
	const invokeID = "inv-self-dispatch-1"
	invokePayload, err := json.Marshal(protocol.Packet{
		Cmd: protocol.CmdAgentInvoke,
		Seq: 2,
		Payload: mustMarshalRawJSON(t, protocol.AgentInvokePayload{
			InvokeID: invokeID,
			Action:   "dispatch_agent",
			Params: map[string]interface{}{
				"agent_id": strconv.FormatInt(agentID, 10),
				"cwd":      "/work/self",
				"task":     "自派自回归验证",
				"title":    "自派自回归",
			},
			TimeoutMS: 20_000,
		}),
	})
	if err != nil {
		t.Fatalf("marshal agent_invoke packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, invokePayload); err != nil {
		t.Fatalf("write agent_invoke packet: %v", err)
	}

	// 模拟 connector 侧读循环：收到 session_control 绑定动作立即回 ok。
	// 成功路径毫秒级完成；读超时给 5s 已远超正常路径，等不到即死锁复现，
	// 无需等满 15s 的绑定超时。
	deadline := time.Now().Add(5 * time.Second)
	sawBindAction := false
	var result protocol.AgentInvokeResultPayload
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read loop deadlocked: no agent_invoke_result before bind timeout (sawBindAction=%v): %v", sawBindAction, err)
		}
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		switch pkt.Cmd {
		case protocol.CmdLocalAction:
			var action protocol.LocalActionPayload
			if err := json.Unmarshal(pkt.Payload, &action); err != nil {
				t.Fatalf("unmarshal local_action: %v", err)
			}
			if action.ActionType != "session_control" {
				continue
			}
			sawBindAction = true
			bindResult, err := json.Marshal(protocol.Packet{
				Cmd: protocol.CmdLocalActionResult,
				Seq: 3,
				Payload: mustMarshalRawJSON(t, protocol.LocalActionResultPayload{
					ActionID: action.ActionID,
					Status:   "ok",
					Result: map[string]interface{}{
						"binding": map[string]interface{}{
							"provider_key":  "claude",
							"cwd":           "/work/self",
							"binding_id":    "bind-self-1",
							"worker_status": "ready",
						},
					},
				}),
			})
			if err != nil {
				t.Fatalf("marshal local_action_result: %v", err)
			}
			if err := conn.WriteMessage(websocket.TextMessage, bindResult); err != nil {
				t.Fatalf("write local_action_result: %v", err)
			}
		case protocol.CmdAgentInvokeResult:
			if err := json.Unmarshal(pkt.Payload, &result); err != nil {
				t.Fatalf("unmarshal agent_invoke_result: %v", err)
			}
			if result.InvokeID != invokeID {
				continue
			}
		default:
			continue
		}
		if result.InvokeID == invokeID {
			break
		}
	}

	if !sawBindAction {
		t.Fatalf("session_control bind action never reached the connector side")
	}
	if result.Code == 4290 {
		t.Fatalf("self dispatch hit bind timeout(4290) — read loop deadlock regressed: msg=%s", result.Msg)
	}
	if result.Code != 0 {
		t.Fatalf("self dispatch failed: code=%d msg=%s", result.Code, result.Msg)
	}
	data, _ := result.Data.(map[string]interface{})
	if data["mode"] != "binding" {
		t.Fatalf("expected mode=binding, got %v", data["mode"])
	}
	if sid, _ := data["session_id"].(string); strings.TrimSpace(sid) == "" {
		t.Fatalf("missing session_id in result data: %+v", data)
	}
}

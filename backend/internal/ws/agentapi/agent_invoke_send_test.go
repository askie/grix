package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestDispatchSendMsgPreservesFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
		msg  string
	}{
		{"permission", &SendError{Code: 4003, Msg: "permission denied"}, 4003, "permission denied"},
		{"not found", &SendError{Code: 4004, Msg: "session not found"}, 4004, "session not found"},
		{"validation", &SendError{Code: 4001, Msg: "invalid message"}, 4001, "invalid message"},
		{"wrapped", fmt.Errorf("bridge: %w", &SendError{Code: 4003, Msg: "permission denied"}), 4003, "permission denied"},
		{"server", &SendError{Code: 5001, Msg: "send message failed"}, 5001, "send message failed"},
		{"zero code", &SendError{Msg: "send message failed"}, 5001, "send message failed"},
		{"negative code", &SendError{Code: -1, Msg: "send message failed"}, 5001, "send message failed"},
		{"plain", errors.New("transport failed"), 5001, "transport failed"},
		{"empty typed message", fmt.Errorf("bridge: %w", &SendError{Code: 4003}), 4003, "bridge: "},
		{"typed nil", (*SendError)(nil), 5001, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			data, code, msg := dispatchSendMsg(101, 202, map[string]interface{}{
				"session_id": "private-session", "content": "test",
			}, agentInvokeHooks{sendMessage: func(req SendMessageReq) (*SendMessageResult, error) {
				calls++
				return nil, tt.err
			}})
			if calls != 1 || data != nil || code != tt.code || msg != tt.msg {
				t.Fatalf("calls=%d data=%v code=%d msg=%q, want nil/%d/%q", calls, data, code, msg, tt.code, tt.msg)
			}
		})
	}
}

func TestHandleAgentInvokeSendMsgKeepsIdentityAndFailure(t *testing.T) {
	for _, failure := range []bool{true, false} {
		t.Run(fmt.Sprintf("failure=%t", failure), func(t *testing.T) {
			calls := 0
			mgr := NewManager("", 30*time.Second, func(_ context.Context, req SendMessageReq) (*SendMessageResult, error) {
				calls++
				if req.AgentID != 101 || req.OwnerID != 202 || req.IdentityMode != "" || req.CallerID != 0 {
					t.Fatalf("untrusted identity override reached send handler: %+v", req)
				}
				if req.SessionID != "private-session" || req.Content != "test" || req.ClientMsgID != "client-1" || req.MsgType != 1 {
					t.Fatalf("send request changed: %+v", req)
				}
				if failure {
					return nil, &SendError{Code: 4003, Msg: "permission denied"}
				}
				return &SendMessageResult{MsgID: 301, InboxSeq: 302, CreatedAt: 303}, nil
			}, nil, nil, nil)
			defer mgr.Shutdown()
			conn := &agentConn{agentID: 101, ownerID: 202, clientID: "invoke-send-test", send: make(chan []byte, 1)}
			mgr.handleAgentInvoke(conn, makePacket(t, protocol.CmdAgentInvoke, 7, protocol.AgentInvokePayload{
				InvokeID: "invoke-send-1", Action: "send_msg", Params: map[string]interface{}{
					"session_id": "private-session", "content": "test", "client_msg_id": "client-1",
					"identity_mode": "delegate", "owner_id": "999", "agent_id": "999", "caller_id": "999",
				},
			}))
			if calls != 1 {
				t.Fatalf("send calls=%d, want 1 (no retry or identity fallback)", calls)
			}
			select {
			case wire := <-conn.send:
				var packet protocol.Packet
				if err := json.Unmarshal(wire, &packet); err != nil {
					t.Fatal(err)
				}
				var payload protocol.AgentInvokeResultPayload
				if err := json.Unmarshal(packet.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				if packet.Cmd != protocol.CmdAgentInvokeResult || packet.Seq != 7 || payload.InvokeID != "invoke-send-1" {
					t.Fatalf("unexpected response packet=%+v payload=%+v", packet, payload)
				}
				if failure {
					if payload.Code != 4003 || payload.Msg != "permission denied" || payload.Data != nil {
						t.Fatalf("denied send reported incorrectly: %+v", payload)
					}
				} else if payload.Code != 0 || payload.Msg != "" || payload.Data == nil {
					t.Fatalf("successful send reported incorrectly: %+v", payload)
				} else {
					data, ok := payload.Data.(map[string]interface{})
					if !ok || data["msg_id"] != float64(301) || data["inbox_seq"] != float64(302) || data["created_at"] != float64(303) {
						t.Fatalf("send receipt changed: %+v", payload.Data)
					}
				}
			default:
				t.Fatal("missing agent_invoke_result")
			}
		})
	}
}

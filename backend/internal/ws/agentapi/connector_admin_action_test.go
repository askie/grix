package agentapi

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

// TestHandleConnectorAdminPendingResult_Success 覆盖成功路径：连接器回执体是
// {ok:true, result:...} 信封，等待方拿到的必须是里面的 result，不是整个信封。
func TestHandleConnectorAdminPendingResult_Success(t *testing.T) {
	ch := make(chan *connectorAdminResponse, 1)
	pending := &pendingLocalAction{
		actionID:               "connector_admin:1:1",
		kind:                   ConnectorAdminActionType,
		actionType:             ConnectorAdminActionType,
		connectorAdminResultCh: ch,
	}

	var m *Manager
	m.handleConnectorAdminPendingResult(pending, protocol.LocalActionResultPayload{
		ActionID: pending.actionID,
		Status:   "ok",
		Result: map[string]any{
			"ok": true,
			"result": []any{
				map[string]any{"agentType": "claude", "label": "Claude"},
			},
		},
	})

	select {
	case resp := <-ch:
		if resp.Error != "" || resp.ErrorCode != "" {
			t.Fatalf("unexpected error: code=%q msg=%q", resp.ErrorCode, resp.Error)
		}
		list, ok := resp.Result.([]any)
		if !ok || len(list) != 1 {
			t.Fatalf("result=%#v want a one-element list", resp.Result)
		}
	default:
		t.Fatalf("expected a result on connectorAdminResultCh")
	}
}

// TestHandleConnectorAdminPendingResult_EnvelopeFailure 覆盖连接器受理了指令但业务
// 失败：status=ok 而信封 ok=false，必须变成错误回给客户端，不能当成功透传。
func TestHandleConnectorAdminPendingResult_EnvelopeFailure(t *testing.T) {
	ch := make(chan *connectorAdminResponse, 1)
	pending := &pendingLocalAction{
		actionID:               "connector_admin:1:2",
		kind:                   ConnectorAdminActionType,
		actionType:             ConnectorAdminActionType,
		connectorAdminResultCh: ch,
	}

	var m *Manager
	m.handleConnectorAdminPendingResult(pending, protocol.LocalActionResultPayload{
		ActionID: pending.actionID,
		Status:   "ok",
		Result: map[string]any{
			"ok":    false,
			"error": "already installing",
		},
	})

	select {
	case resp := <-ch:
		if resp.Error != "already installing" {
			t.Fatalf("error=%q want=%q", resp.Error, "already installing")
		}
		if resp.ErrorCode != ConnectorAdminErrFailed {
			t.Fatalf("error_code=%q want=%q", resp.ErrorCode, ConnectorAdminErrFailed)
		}
	default:
		t.Fatalf("expected a result on connectorAdminResultCh")
	}
}

// TestHandleConnectorAdminPendingResult_ObjectErrorEnvelope 覆盖连接器 4.8.0 的
// 失败回执形态：error 是对象 {code, message}，没有顶层 error_code。只认字符串的话
// 这些码会全部退化成 failed，客户端就分不出"连接器太老"和一次性失败。
func TestHandleConnectorAdminPendingResult_ObjectErrorEnvelope(t *testing.T) {
	cases := []struct {
		name    string
		code    string
		message string
	}{
		{"unsupported op", "unsupported_op", "unknown op: install"},
		{"remote admin disabled", "remote_admin_disabled", "remote admin is disabled"},
		{"forbidden", "forbidden", "actor is not allowed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan *connectorAdminResponse, 1)
			pending := &pendingLocalAction{
				actionID:               "connector_admin:1:6",
				kind:                   ConnectorAdminActionType,
				actionType:             ConnectorAdminActionType,
				connectorAdminResultCh: ch,
			}

			var m *Manager
			m.handleConnectorAdminPendingResult(pending, protocol.LocalActionResultPayload{
				ActionID: pending.actionID,
				Status:   "ok",
				Result: map[string]any{
					"ok": false,
					"error": map[string]any{
						"code":    tc.code,
						"message": tc.message,
					},
				},
			})

			select {
			case resp := <-ch:
				if resp.ErrorCode != tc.code {
					t.Fatalf("error_code=%q want=%q", resp.ErrorCode, tc.code)
				}
				if resp.Error != tc.message {
					t.Fatalf("error=%q want=%q", resp.Error, tc.message)
				}
			default:
				t.Fatalf("expected a result on connectorAdminResultCh")
			}
		})
	}
}

// TestHandleConnectorAdminPendingResult_ObjectErrorWithoutCode 覆盖对象形态但只带
// message：错误码退回 failed，文案仍要带出去，别让用户只看到一句通用失败。
func TestHandleConnectorAdminPendingResult_ObjectErrorWithoutCode(t *testing.T) {
	ch := make(chan *connectorAdminResponse, 1)
	pending := &pendingLocalAction{
		actionID:               "connector_admin:1:7",
		kind:                   ConnectorAdminActionType,
		actionType:             ConnectorAdminActionType,
		connectorAdminResultCh: ch,
	}

	var m *Manager
	m.handleConnectorAdminPendingResult(pending, protocol.LocalActionResultPayload{
		ActionID: pending.actionID,
		Status:   "ok",
		Result: map[string]any{
			"ok":    false,
			"error": map[string]any{"message": "npm exited 1"},
		},
	})

	select {
	case resp := <-ch:
		if resp.ErrorCode != ConnectorAdminErrFailed {
			t.Fatalf("error_code=%q want=%q", resp.ErrorCode, ConnectorAdminErrFailed)
		}
		if resp.Error != "npm exited 1" {
			t.Fatalf("error=%q want=%q", resp.Error, "npm exited 1")
		}
	default:
		t.Fatalf("expected a result on connectorAdminResultCh")
	}
}

// TestHandleConnectorAdminPendingResult_TopLevelErrorCode 覆盖顶层 error_code +
// error 为字符串的早期形态，保证换契约没把它读丢。
func TestHandleConnectorAdminPendingResult_TopLevelErrorCode(t *testing.T) {
	ch := make(chan *connectorAdminResponse, 1)
	pending := &pendingLocalAction{
		actionID:               "connector_admin:1:8",
		kind:                   ConnectorAdminActionType,
		actionType:             ConnectorAdminActionType,
		connectorAdminResultCh: ch,
	}

	var m *Manager
	m.handleConnectorAdminPendingResult(pending, protocol.LocalActionResultPayload{
		ActionID: pending.actionID,
		Status:   "ok",
		Result: map[string]any{
			"ok":         false,
			"error":      "invalid params",
			"error_code": "invalid_params",
		},
	})

	select {
	case resp := <-ch:
		if resp.ErrorCode != "invalid_params" {
			t.Fatalf("error_code=%q want=invalid_params", resp.ErrorCode)
		}
		if resp.Error != "invalid params" {
			t.Fatalf("error=%q want=%q", resp.Error, "invalid params")
		}
	default:
		t.Fatalf("expected a result on connectorAdminResultCh")
	}
}

// TestHandleConnectorAdminPendingResult_Unsupported 覆盖老连接器：收到未知
// action_type 时回 status=unsupported，后端必须原样带出 unsupported 错误码，
// 客户端据此提示"请升级连接器"，而不是当成一次普通失败让用户重试。
func TestHandleConnectorAdminPendingResult_Unsupported(t *testing.T) {
	ch := make(chan *connectorAdminResponse, 1)
	pending := &pendingLocalAction{
		actionID:               "connector_admin:1:3",
		kind:                   ConnectorAdminActionType,
		actionType:             ConnectorAdminActionType,
		connectorAdminResultCh: ch,
	}

	var m *Manager
	m.handleConnectorAdminPendingResult(pending, protocol.LocalActionResultPayload{
		ActionID: pending.actionID,
		Status:   "unsupported",
		ErrorMsg: "unknown action_type",
	})

	select {
	case resp := <-ch:
		if resp.ErrorCode != ConnectorAdminErrUnsupported {
			t.Fatalf("error_code=%q want=%q", resp.ErrorCode, ConnectorAdminErrUnsupported)
		}
	default:
		t.Fatalf("expected a result on connectorAdminResultCh")
	}
}

// TestHandleConnectorAdminPendingResult_BareResult 覆盖连接器直接把业务数据放在
// result 上、没有 {ok,...} 信封的情况：原样透传，不要误判成失败。
func TestHandleConnectorAdminPendingResult_BareResult(t *testing.T) {
	ch := make(chan *connectorAdminResponse, 1)
	pending := &pendingLocalAction{
		actionID:               "connector_admin:1:4",
		kind:                   ConnectorAdminActionType,
		actionType:             ConnectorAdminActionType,
		connectorAdminResultCh: ch,
	}

	var m *Manager
	m.handleConnectorAdminPendingResult(pending, protocol.LocalActionResultPayload{
		ActionID: pending.actionID,
		Status:   "ok",
		Result:   []any{map[string]any{"agentType": "codex"}},
	})

	select {
	case resp := <-ch:
		if resp.Error != "" {
			t.Fatalf("unexpected error: %q", resp.Error)
		}
		if list, ok := resp.Result.([]any); !ok || len(list) != 1 {
			t.Fatalf("result=%#v want a one-element list", resp.Result)
		}
	default:
		t.Fatalf("expected a result on connectorAdminResultCh")
	}
}

// TestSendConnectorAdminActionAndWait_OfflineFailsFast 覆盖 agent 不在线：下发不出去
// 必须立即返回 offline，不能挂到 18s 超时。
func TestSendConnectorAdminActionAndWait_OfflineFailsFast(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	started := time.Now()
	_, err := mgr.SendConnectorAdminActionAndWait(9801, 9801, "list_installable", nil)
	if err != ErrConnectorAdminAgentOffline {
		t.Fatalf("err=%v want=%v", err, ErrConnectorAdminAgentOffline)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("took %v, expected to fail fast", elapsed)
	}
}

// TestSendConnectorAdminActionAndWait_RejectsMissingOwner 覆盖 fail-closed：
// 没有 owner 上下文时绝不下发（否则可能落到别人的 connector 上）。
func TestSendConnectorAdminActionAndWait_RejectsMissingOwner(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	if _, err := mgr.SendConnectorAdminActionAndWait(9802, 0, "install", map[string]any{"agent_type": "claude"}); err == nil {
		t.Fatal("SendConnectorAdminActionAndWait(owner=0) MUST be rejected")
	}
}

// TestTimeoutPendingConnectorAdminAction 覆盖超时收口：pending 超时时必须给等待方
// 推一个 timeout 回执，不能让调用方的 select 悬着等到自己的定时器。
func TestTimeoutPendingConnectorAdminAction(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	ch := make(chan *connectorAdminResponse, 1)
	pending := &pendingLocalAction{
		actionID:               "connector_admin:1:5",
		kind:                   ConnectorAdminActionType,
		actionType:             ConnectorAdminActionType,
		agentID:                9803,
		ownerID:                9803,
		connectorAdminResultCh: ch,
	}
	mgr.storePendingLocalAction(pending)
	mgr.timeoutPendingLocalAction(pending.actionID)

	select {
	case resp := <-ch:
		if resp.ErrorCode != ConnectorAdminErrTimeout {
			t.Fatalf("error_code=%q want=%q", resp.ErrorCode, ConnectorAdminErrTimeout)
		}
	default:
		t.Fatalf("expected a timeout result on connectorAdminResultCh")
	}
}

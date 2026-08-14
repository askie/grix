package agentapi

import (
	"context"
	"encoding/json"
	"runtime/debug"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// handleAgentInvoke processes agent_invoke packets from the plugin.
// It parses the payload, dispatches to the action router, and sends back agent_invoke_result.
// 本函数由读循环以独立 goroutine 调起，没有 net/http 的 panic 兜底，
// 必须自带 recover：任何 action 处理中的 panic 只失败本次 invoke，不打挂 ws 进程。
func (m *Manager) handleAgentInvoke(conn *agentConn, pkt *protocol.Packet) {
	var invokeID, action string
	defer func() {
		if r := recover(); r != nil {
			logger.L.Errorf(
				"agent_invoke panic agent=%d owner=%d action=%s invoke_id=%s: %v\n%s",
				conn.agentID, conn.ownerID, action, invokeID, r, debug.Stack(),
			)
			conn.sendPayload(protocol.CmdAgentInvokeResult, pkt.Seq, protocol.AgentInvokeResultPayload{
				InvokeID: invokeID,
				Code:     5001,
				Msg:      "internal error",
			})
		}
	}()

	var payload protocol.AgentInvokePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload(protocol.CmdAgentInvokeResult, pkt.Seq, protocol.AgentInvokeResultPayload{
			Code: 4001,
			Msg:  "invalid agent_invoke payload",
		})
		return
	}

	invokeID = strings.TrimSpace(payload.InvokeID)
	action = strings.TrimSpace(payload.Action)

	if invokeID == "" {
		conn.sendPayload(protocol.CmdAgentInvokeResult, pkt.Seq, protocol.AgentInvokeResultPayload{
			Code: 4001,
			Msg:  "invoke_id required",
		})
		return
	}
	if action == "" {
		conn.sendPayload(protocol.CmdAgentInvokeResult, pkt.Seq, protocol.AgentInvokeResultPayload{
			InvokeID: invokeID,
			Code:     4001,
			Msg:      "action required",
		})
		return
	}

	timeout := time.Duration(payload.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if timeout > 60*time.Second {
		timeout = 60 * time.Second
	}

	_ = timeout // reserved for future context deadline

	start := time.Now()
	data, code, msg := dispatchAgentInvokeWithHooks(conn.agentID, conn.ownerID, action, payload.Params, agentInvokeHooks{
		sendMessage: func(req SendMessageReq) (*SendMessageResult, error) {
			return m.SendMessage(nil, req)
		},
		deleteMsg: func(ctx context.Context, agentID, ownerID int64, payload DeleteMsgPayload) error {
			return m.deleteMsgFn(ctx, agentID, ownerID, payload)
		},
		bindSession: func(agentID int64, sessionID, actorID, cwd, providerKey string) (*sessionBindResponse, error) {
			// 闭包捕获 conn.ownerID:dispatch_agent 由被共享者 B 触发时,
			// 必须把 session_bind 落到 B 的 connector 实例,而不是主人的。
			return m.SendSessionBindActionAndWait(agentID, conn.ownerID, sessionID, actorID, cwd, providerKey, "")
		},
	})
	elapsed := time.Since(start)

	logger.L.Infof(
		"agent_invoke action=%s agent=%d owner=%d invoke_id=%s code=%d elapsed=%s",
		action, conn.agentID, conn.ownerID, invokeID, code, elapsed,
	)

	conn.sendPayload(protocol.CmdAgentInvokeResult, pkt.Seq, protocol.AgentInvokeResultPayload{
		InvokeID: invokeID,
		Code:     code,
		Msg:      msg,
		Data:     data,
	})
}

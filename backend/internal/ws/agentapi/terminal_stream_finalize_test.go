package agentapi

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

type forceFinalizeCall struct {
	agentID   int64
	ownerID   int64
	sessionID string
}

type forceFinalizeRecorder struct {
	mu    sync.Mutex
	calls []forceFinalizeCall
}

func (r *forceFinalizeRecorder) handle(_ context.Context, agentID, ownerID int64, sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, forceFinalizeCall{agentID: agentID, ownerID: ownerID, sessionID: sessionID})
}

func (r *forceFinalizeRecorder) snapshot() []forceFinalizeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]forceFinalizeCall(nil), r.calls...)
}

// 终态 event_result 必须替连接器收尾该会话里还开着的流：老连接器只发
// is_finish=false 的错误文案就打终态，客户端否则要等僵尸流看门狗才清"正在输出"。
// 三种终态都要收尾，且对重复的终态报文幂等（不重复扫流）。
func TestDurableTerminalResultForceFinalizesSessionStreams(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
	}{
		{name: "failed", status: protocol.AgentEventResultFailed},
		{name: "canceled", status: protocol.AgentEventResultCanceled},
		{name: "responded", status: protocol.AgentEventResultResponded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installDurableLifecycleTestStores(t, true)
			event := durableLifecycleEvent("terminal-stream-"+tc.name, 6601, 6701)
			manager := NewManager("", time.Second, nil, nil, nil, nil)
			defer manager.Shutdown()
			recorder := &forceFinalizeRecorder{}
			manager.SetForceFinalizeStreamsHandler(recorder.handle)
			conn := &agentConn{
				agentID:      event.AgentID,
				ownerID:      event.OwnerID,
				capabilities: []string{"event_result_ack"},
				send:         make(chan []byte, 8),
			}
			manager.putConnForTest(conn)
			require.True(t, manager.PushDelegateEvent(event))
			requireDurablePacket(t, conn.send, protocol.CmdEventMsg)

			result := EventResultPayload{
				EventID: event.EventID,
				Status:  tc.status,
				Msg:     "stream left open by connector",
			}
			manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 41, result))
			requireDurablePacket(t, conn.send, protocol.CmdSendAck)

			calls := recorder.snapshot()
			require.Len(t, calls, 1)
			require.Equal(t, forceFinalizeCall{
				agentID:   event.AgentID,
				ownerID:   event.OwnerID,
				sessionID: event.SessionID,
			}, calls[0])

			// 重复的终态报文只补发 ack，不再扫一次流注册表。
			manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 42, result))
			requireDurablePacket(t, conn.send, protocol.CmdSendAck)
			require.Len(t, recorder.snapshot(), 1)
		})
	}
}

// 无 Redis 的本地终态路径同样要收尾流，会话号取本地待结算登记而非报文字段。
func TestLocalTerminalResultForceFinalizesSessionStreams(t *testing.T) {
	withoutDurableStores(t)
	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	recorder := &forceFinalizeRecorder{}
	manager.SetForceFinalizeStreamsHandler(recorder.handle)

	const (
		eventID   = "local-terminal-stream"
		sessionID = "session-local-terminal-stream"
		agentID   = int64(6801)
		ownerID   = int64(6901)
	)
	manager.eventAckWait = time.Minute
	manager.eventResultWait = time.Minute
	require.Equal(t, pendingEventRegistrationCreated, manager.registerPendingEventAck(DelegateEventPayload{
		EventID:   eventID,
		AgentID:   agentID,
		OwnerID:   ownerID,
		SessionID: sessionID,
		SenderID:  ownerID,
	}, 1))

	conn := &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		capabilities: []string{"event_result_ack"},
		send:         make(chan []byte, 8),
	}
	manager.putConnForTest(conn)

	result := EventResultPayload{
		EventID: eventID,
		Status:  protocol.AgentEventResultFailed,
		// event_result 报文本身不带会话身份，收尾只能按本地登记的会话号来。
		Msg: "stream left open by connector",
	}
	manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 51, result))
	requireDurablePacket(t, conn.send, protocol.CmdSendAck)

	calls := recorder.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, forceFinalizeCall{agentID: agentID, ownerID: ownerID, sessionID: sessionID}, calls[0])

	// 重复终态：本地路径此时已不再持有该事件的归属登记，回什么由既有逻辑决定，
	// 这里只关心不会再扫一次流。
	manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 52, result))
	require.Len(t, recorder.snapshot(), 1)
}

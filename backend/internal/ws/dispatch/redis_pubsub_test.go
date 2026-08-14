package dispatch

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

type testConn struct {
	deviceID string
	platform string
	packets  chan packetRecord
}

type packetRecord struct {
	cmd     string
	payload json.RawMessage
}

func (c *testConn) SendPayload(cmd string, seq int64, payload interface{}) {}

func (c *testConn) SendPacket(pkt *protocol.Packet) {
	c.sendPacket(pkt.Cmd, pkt.Payload)
}

func (c *testConn) AckPush(msgID int64) {}

func (c *testConn) NextSeq() int64 { return 1 }

func (c *testConn) Close() {}

func (c *testConn) GetUserID() int64 { return 0 }

func (c *testConn) GetDeviceID() string { return c.deviceID }

func (c *testConn) GetPlatform() string { return c.platform }

func (c *testConn) SetAuth(userID int64, sessionID, deviceID, platform string) {}

func (c *testConn) IsAuthed() bool { return true }

func (c *testConn) sendPacket(cmd string, payload json.RawMessage) {
	select {
	case c.packets <- packetRecord{cmd: cmd, payload: payload}:
	default:
	}
}

type testHub struct {
	conns map[int64][]handler.ConnInterface
}

func (h *testHub) Register(c handler.ConnInterface) {}

func (h *testHub) Unregister(c handler.ConnInterface) {}

func (h *testHub) RefreshAlive(c handler.ConnInterface) {}

func (h *testHub) GetUserConns(userID int64) []handler.ConnInterface {
	return h.conns[userID]
}

func (h *testHub) GetNodeID() string { return "node-test" }

func TestStartRedisSubRoutesAndStopsCleanly(t *testing.T) {
	logger.Init()

	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		store.RDB = previous
	}()

	connA := &testConn{deviceID: "device-a", packets: make(chan packetRecord, 4)}
	connB := &testConn{deviceID: "device-b", packets: make(chan packetRecord, 4)}
	hub := &testHub{
		conns: map[int64][]handler.ConnInterface{
			42: {connA, connB},
		},
	}

	stop := StartRedisSub("node-dispatch-test", hub)
	defer stop()

	payload, err := json.Marshal(map[string]any{"value": "ok"})
	require.NoError(t, err)

	data, err := json.Marshal(map[string]any{
		"user_id":           42,
		"cmd":               "push_custom",
		"payload":           json.RawMessage(payload),
		"target_device_id":  "device-a",
		"exclude_device_id": "device-b",
	})
	require.NoError(t, err)

	err = store.RDB.Publish(context.Background(), "chan:node-dispatch-test", data).Err()
	require.NoError(t, err)

	select {
	case pkt := <-connA.packets:
		require.Equal(t, "push_custom", pkt.cmd)
		require.JSONEq(t, `{"value":"ok"}`, string(pkt.payload))
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for redis dispatch packet")
	}

	select {
	case pkt := <-connB.packets:
		t.Fatalf("device-b should not receive packet, got cmd=%s", pkt.cmd)
	case <-time.After(200 * time.Millisecond):
	}

	stop()

	err = store.RDB.Publish(context.Background(), "chan:node-dispatch-test", data).Err()
	require.NoError(t, err)

	select {
	case pkt := <-connA.packets:
		t.Fatalf("subscriber should be stopped, got cmd=%s", pkt.cmd)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestStartRedisSubFiltersWidgetInternalPush 验证跨节点投递时，网站访客连接
// 不会收到 agent 内部过程卡片（工具执行/思考），而 App 连接照常收到；
// 同一访客的普通文字消息仍正常下发。
func TestStartRedisSubFiltersWidgetInternalPush(t *testing.T) {
	logger.Init()

	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() { store.RDB = previous }()

	widgetConn := &testConn{deviceID: "widget-a", platform: handler.WidgetPlatform, packets: make(chan packetRecord, 4)}
	appConn := &testConn{deviceID: "app-a", platform: "ios", packets: make(chan packetRecord, 4)}
	hub := &testHub{
		conns: map[int64][]handler.ConnInterface{
			77: {widgetConn, appConn},
		},
	}

	stop := StartRedisSub("node-widget-test", hub)
	defer stop()

	publishPush := func(content, extra string) {
		toolCard, err := json.Marshal(protocol.PushMsgPayload{MsgType: 1, Content: content, Extra: json.RawMessage(extra)})
		require.NoError(t, err)
		data, err := json.Marshal(map[string]any{
			"user_id": 77,
			"cmd":     protocol.CmdPushMsg,
			"payload": json.RawMessage(toolCard),
		})
		require.NoError(t, err)
		require.NoError(t, store.RDB.Publish(context.Background(), "chan:node-widget-test", data).Err())
	}

	// 内部工具卡片：App 收到，访客不收到。
	publishPush("[[Tools] skill_view: x](grix://card/tool_execution_group?d=1)",
		`{"channel_data":{"grix":{"toolExecution":{"summary_text":"skill_view: x"}}}}`)

	select {
	case pkt := <-appConn.packets:
		require.Equal(t, protocol.CmdPushMsg, pkt.cmd)
	case <-time.After(2 * time.Second):
		t.Fatal("app conn should receive the internal tool card")
	}
	select {
	case pkt := <-widgetConn.packets:
		t.Fatalf("widget visitor must NOT receive internal tool card, got cmd=%s", pkt.cmd)
	case <-time.After(200 * time.Millisecond):
	}

	// 普通文字：访客照常收到。
	publishPush("您好，有什么可以帮您？", "")
	select {
	case pkt := <-widgetConn.packets:
		require.Equal(t, protocol.CmdPushMsg, pkt.cmd)
	case <-time.After(2 * time.Second):
		t.Fatal("widget visitor must still receive normal text push")
	}
}

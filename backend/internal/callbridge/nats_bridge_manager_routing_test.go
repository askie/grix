package callbridge

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	logger.Init()
}

// connectTestNATS 连接本地 NATS；不可用则跳过（与 transcriptpipe 测试同模式）。
// AIBOT_TEST_NATS_URL 可覆盖地址：共享机器上的 NATS 挂着真实消费者（如 dev voicebridge）
// 会与测试的 mock 抢队列消息，CI 通过指向空端口让本测试干净跳过。
func connectTestNATS(t *testing.T) *nats.Conn {
	t.Helper()
	url := os.Getenv("AIBOT_TEST_NATS_URL")
	if url == "" {
		url = nats.DefaultURL
	}
	nc, err := nats.Connect(url, nats.Timeout(500*time.Millisecond))
	if err != nil {
		t.Skipf("NATS not available: %v", err)
	}
	t.Cleanup(func() { nc.Close() })
	return nc
}

func shortTimeout(t *testing.T) {
	t.Helper()
	old := requestTimeout
	requestTimeout = 500 * time.Millisecond
	t.Cleanup(func() { requestTimeout = old })
}

var testSpec = call.VoiceBridgeSpec{AgentID: 1, Provider: "openai_realtime", Model: "m", APIKey: "k"}

// respondOn 模拟一个 voicebridge 节点在 subject 上应答 ack。
func respondOn(t *testing.T, nc *nats.Conn, subject, queue, ackJSON string, got chan<- string) {
	t.Helper()
	cb := func(msg *nats.Msg) {
		if got != nil {
			got <- msg.Subject
		}
		_ = msg.Respond([]byte(ackJSON))
	}
	var sub *nats.Subscription
	var err error
	if queue != "" {
		sub, err = nc.QueueSubscribe(subject, queue, cb)
	} else {
		sub, err = nc.Subscribe(subject, cb)
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
}

func TestStartEstablishesNodeAffinityAndDirectedControl(t *testing.T) {
	nc := connectTestNATS(t)
	m := NewNATSBridgeManager(nc)
	ctx := context.Background()

	// 模拟节点 nodeA：queue group 接 start，定向主题接 mute/unmute
	respondOn(t, nc, subjectStart, "voicebridge", `{"ok":true,"node_id":"nodeA"}`, nil)
	gotMute := make(chan string, 1)
	respondOn(t, nc, subjectMute+".nodeA", "", `{"ok":true,"node_id":"nodeA"}`, gotMute)
	gotUnmute := make(chan string, 1)
	respondOn(t, nc, subjectUnmute+".nodeA", "", `{"ok":true,"node_id":"nodeA"}`, gotUnmute)

	require.NoError(t, m.StartBridge(ctx, 1, testSpec))
	assert.Equal(t, "nodeA", m.nodeOf(1), "start 回执应建立 call→节点 归属")

	require.NoError(t, m.MuteBridge(ctx, 1))
	assert.Equal(t, subjectMute+".nodeA", <-gotMute, "mute 应发往节点定向主题")

	require.NoError(t, m.UnmuteBridge(ctx, 1))
	assert.Equal(t, subjectUnmute+".nodeA", <-gotUnmute, "unmute 应发往节点定向主题")

	// stop 终结通话，清归属
	require.NoError(t, m.StopBridge(ctx, 1))
	assert.Equal(t, "", m.nodeOf(1), "stop 后归属应清除")
}

func TestStartRejectedClearsNode(t *testing.T) {
	nc := connectTestNATS(t)
	m := NewNATSBridgeManager(nc)

	respondOn(t, nc, subjectStart, "voicebridge", `{"ok":false,"error":"bad provider","node_id":"nodeA"}`, nil)

	err := m.StartBridge(context.Background(), 2, testSpec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad provider")
	assert.Equal(t, "", m.nodeOf(2))
}

func TestDirectedStartTimeoutFallsBackToQueueGroup(t *testing.T) {
	nc := connectTestNATS(t)
	shortTimeout(t)
	m := NewNATSBridgeManager(nc)

	// 归属指向已死节点；只有存活节点 nodeB 在 queue group 上
	m.setNode(7, "deadnode")
	respondOn(t, nc, subjectStart, "voicebridge", `{"ok":true,"node_id":"nodeB"}`, nil)

	require.NoError(t, m.StartBridge(context.Background(), 7, testSpec),
		"定向节点无响应时应回退广播 queue group 由存活节点接管")
	assert.Equal(t, "nodeB", m.nodeOf(7), "归属应更新为接管节点")
}

func TestDirectedMuteTimeoutClearsNode(t *testing.T) {
	nc := connectTestNATS(t)
	shortTimeout(t)
	m := NewNATSBridgeManager(nc)

	m.setNode(8, "deadnode")
	err := m.MuteBridge(context.Background(), 8)
	require.Error(t, err, "定向节点无响应时 mute 应失败（接管不能放行）")
	assert.Equal(t, "", m.nodeOf(8), "超时应清除归属，后续 start 走 queue group 重建")
}

func TestLegacyAckWithoutNodeIDKeepsBroadcast(t *testing.T) {
	nc := connectTestNATS(t)
	m := NewNATSBridgeManager(nc)

	// 旧版 voicebridge 回执无 node_id：不建立归属，后续控制走广播主题
	respondOn(t, nc, subjectStart, "voicebridge", `{"ok":true}`, nil)
	gotMute := make(chan string, 1)
	respondOn(t, nc, subjectMute, "", `{"ok":true}`, gotMute)

	ctx := context.Background()
	require.NoError(t, m.StartBridge(ctx, 9, testSpec))
	assert.Equal(t, "", m.nodeOf(9))
	require.NoError(t, m.MuteBridge(ctx, 9))
	assert.Equal(t, subjectMute, <-gotMute, "无归属时 mute 应走广播主题")
}

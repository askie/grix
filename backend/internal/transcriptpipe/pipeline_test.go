package transcriptpipe_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/callsegment"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/transcriptpipe"
	"github.com/askie/grix/backend/internal/voicerefiner"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	_ = snowflake.Init(1)
	logger.Init()
}

// connectTestNATS 连接本地 NATS；不可用则跳过。
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

func TestPipeline_HandleTranscript(t *testing.T) {
	nc := connectTestNATS(t)

	sendFn, calls, lenFn := callsegment.MockSendFn()
	writer := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)

	resolver := func(callID string) (string, int64, bool) {
		if callID == "call-123" {
			return "sess-abc", int64(1001), true
		}
		return "", 0, false
	}

	pipe := transcriptpipe.New(nc, writer, resolver)
	require.NoError(t, pipe.Start())
	defer pipe.Stop()

	// 发布 transcript 事件
	ev := transcriptpipe.TranscriptEvent{
		CallID:        "call-123",
		SegmentSeq:    1,
		SpeakerRole:   "caller",
		TranscriptRaw: "嗯 您好 请问您是张先生吗",
		Provider:      "openai_realtime",
		StartedAtMs:   1717000000000,
	}
	data, _ := json.Marshal(ev)
	require.NoError(t, nc.Publish("voicebridge.transcript.call-123", data))

	// 等待异步处理（handle 是异步 goroutine）
	assert.Eventually(t, func() bool {
		return lenFn() == 1
	}, 5*time.Second, 50*time.Millisecond)

	require.Equal(t, 1, lenFn())
	// 读取 calls 需在无并发时（lenFn 已确认写入完成）
	sent := (*calls)[0]
	assert.Equal(t, "sess-abc", sent.SessionID)
	assert.Equal(t, "嗯 您好 请问您是张先生吗", sent.Content)
}

func TestPipeline_UnknownCallID_Skipped(t *testing.T) {
	nc := connectTestNATS(t)

	sendFn, calls, _ := callsegment.MockSendFn()
	writer := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)
	resolver := func(_ string) (string, int64, bool) { return "", 0, false }

	pipe := transcriptpipe.New(nc, writer, resolver)
	require.NoError(t, pipe.Start())
	defer pipe.Stop()

	ev := transcriptpipe.TranscriptEvent{
		CallID: "unknown", SegmentSeq: 1, TranscriptRaw: "text",
	}
	data, _ := json.Marshal(ev)
	require.NoError(t, nc.Publish("voicebridge.transcript.unknown", data))

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, *calls, "unknown call_id should be skipped")
}

func TestPipeline_EmptyTranscript_Skipped(t *testing.T) {
	nc := connectTestNATS(t)

	sendFn, calls, _ := callsegment.MockSendFn()
	writer := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)
	resolver := func(_ string) (string, int64, bool) { return "sess", 1, true }

	pipe := transcriptpipe.New(nc, writer, resolver)
	require.NoError(t, pipe.Start())
	defer pipe.Stop()

	ev := transcriptpipe.TranscriptEvent{CallID: "call-1", SegmentSeq: 1, TranscriptRaw: ""}
	data, _ := json.Marshal(ev)
	require.NoError(t, nc.Publish("voicebridge.transcript.call-1", data))

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, *calls, "empty transcript should be skipped")
}

func TestPipeline_NilNATS_NoError(t *testing.T) {
	sendFn, _, _ := callsegment.MockSendFn()
	writer := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)
	pipe := transcriptpipe.New(nil, writer, func(_ string) (string, int64, bool) { return "", 0, false })
	require.NoError(t, pipe.Start()) // nil NATS 不报错
	pipe.Stop()
}

// 确保 Pipeline 可以在 ws 进程中使用（编译时验证）
var _ = context.Background

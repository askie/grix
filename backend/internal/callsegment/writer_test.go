package callsegment_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/callsegment"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/voicerefiner"
	"github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	_ = snowflake.Init(1)
	os.Exit(m.Run())
}

func TestWriter_Write_BasicFlow(t *testing.T) {
	sendFn, calls, _ := callsegment.MockSendFn()
	w := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)

	msgID, err := w.Write(context.Background(), callsegment.WriteReq{
		SessionID:     "sess-1",
		CallID:        "call-123",
		SegmentSeq:    1,
		SpeakerRole:   "caller",
		SpeakerUserID: 1001,
		OwnerID:       2001,
		TranscriptRaw: "嗯 您好 请问您是张先生吗",
		Provider:      "openai_realtime",
	})
	require.NoError(t, err)
	assert.NotZero(t, msgID)
	require.Len(t, *calls, 1)

	sent := (*calls)[0]
	assert.Equal(t, model.MsgTypeCallSegment, sent.MsgType)
	assert.Equal(t, "sess-1", sent.SessionID)
	// caller 且 SpeakerUserID≠OwnerID 时使用 caller 模式
	assert.Equal(t, agentmsg.ModeCaller, sent.IdentityMode)
	assert.Equal(t, int64(1001), sent.CallerID)
	assert.Equal(t, "voice-call-segment:call-123:1", sent.ClientMsgID)
	assert.Equal(t, "嗯 您好 请问您是张先生吗", sent.Content) // NoopRefiner 不改写

	// 验证 extra 结构
	var extra model.CallSegmentExtra
	require.NoError(t, json.Unmarshal(sent.Extra, &extra))
	assert.Equal(t, "call_segment", extra.Kind)
	assert.Equal(t, "call-123", extra.CallID)
	assert.Equal(t, 1, extra.SegmentSeq)
	assert.Equal(t, "caller", extra.SpeakerRole)
	assert.Equal(t, "final", extra.TranscriptStatus)
	assert.Equal(t, "openai_realtime", extra.TranscriptProvider)
}

// TestWriter_Write_DirectAICall_CallerUsesCallerMode 验证 DirectAICall 时用户文字用 caller 模式
func TestWriter_Write_DirectAICall_CallerUsesCallerMode(t *testing.T) {
	sendFn, calls, _ := callsegment.MockSendFn()
	w := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)

	_, err := w.Write(context.Background(), callsegment.WriteReq{
		SessionID:     "sess-direct",
		CallID:        "call-direct",
		SegmentSeq:    1,
		SpeakerRole:   "caller",
		SpeakerUserID: 1001,
		OwnerID:       1001,
		AgentID:       2001,
		DirectAICall:  true,
		TranscriptRaw: "你好",
		Provider:      "doubao_realtime",
	})
	require.NoError(t, err)
	require.Len(t, *calls, 1)
	sent := (*calls)[0]
	assert.Equal(t, agentmsg.ModeCaller, sent.IdentityMode, "DirectAICall caller should use ModeCaller")
	assert.Equal(t, int64(1001), sent.CallerID)
}

// TestWriter_Write_DirectAICall_AIBotUsesAIDirect 验证 DirectAICall 时 AI 回复用 ai_direct 模式
func TestWriter_Write_DirectAICall_AIBotUsesAIDirect(t *testing.T) {
	sendFn, calls, _ := callsegment.MockSendFn()
	w := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)

	_, err := w.Write(context.Background(), callsegment.WriteReq{
		SessionID:     "sess-direct",
		CallID:        "call-direct",
		SegmentSeq:    2,
		SpeakerRole:   "ai_bot",
		SpeakerUserID: 2001,
		OwnerID:       1001,
		AgentID:       2001,
		DirectAICall:  true,
		TranscriptRaw: "你好，有什么可以帮你的吗？",
		Provider:      "doubao_realtime",
	})
	require.NoError(t, err)
	require.Len(t, *calls, 1)
	sent := (*calls)[0]
	assert.Equal(t, agentmsg.ModeAIDirect, sent.IdentityMode, "DirectAICall ai_bot should use ModeAIDirect")
	assert.Equal(t, int64(2001), sent.AgentID)
}

// TestWriter_Write_CallerIsOwner_WithoutDirectAICall_UsesDelegate 非 DirectAICall 时 caller==owner 仍用 delegate
func TestWriter_Write_CallerIsOwner_WithoutDirectAICall_UsesDelegate(t *testing.T) {
	sendFn, calls, _ := callsegment.MockSendFn()
	w := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)

	_, err := w.Write(context.Background(), callsegment.WriteReq{
		SessionID:     "sess-owner-caller",
		CallID:        "call-oc",
		SegmentSeq:    1,
		SpeakerRole:   "caller",
		SpeakerUserID: 1001,
		OwnerID:       1001,
		TranscriptRaw: "你好",
		Provider:      "openai_realtime",
		// DirectAICall defaults to false
	})
	require.NoError(t, err)
	require.Len(t, *calls, 1)
	assert.Equal(t, agentmsg.ModeDelegate, (*calls)[0].IdentityMode)
}

func TestWriter_Write_AIBotUsesDelegate(t *testing.T) {
	sendFn, calls, _ := callsegment.MockSendFn()
	w := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)

	_, err := w.Write(context.Background(), callsegment.WriteReq{
		SessionID:     "sess-ai",
		CallID:        "call-ai",
		SegmentSeq:    1,
		SpeakerRole:   "ai_bot",
		SpeakerUserID: 2001,
		OwnerID:       1001,
		AgentID:       2001,
		TranscriptRaw: "你好，有什么可以帮你的吗？",
		Provider:      "openai_realtime",
	})
	require.NoError(t, err)
	require.Len(t, *calls, 1)

	sent := (*calls)[0]
	// AI 代接与 owner 一体，使用 delegate 模式（sender_id=OwnerID, sender_type=1）
	assert.Equal(t, agentmsg.ModeDelegate, sent.IdentityMode)
	assert.Equal(t, int64(2001), sent.AgentID)
	assert.Equal(t, int64(1001), sent.OwnerID)
}

func TestWriter_Write_CallerUsesDelegate(t *testing.T) {
	sendFn, calls, _ := callsegment.MockSendFn()
	w := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)

	_, err := w.Write(context.Background(), callsegment.WriteReq{
		SessionID:     "sess-human",
		CallID:        "call-human",
		SegmentSeq:    1,
		SpeakerRole:   "caller",
		SpeakerUserID: 1001,
		OwnerID:       1001,
		AgentID:       2001,
		TranscriptRaw: "你好",
		Provider:      "openai_realtime",
	})
	require.NoError(t, err)
	require.Len(t, *calls, 1)

	sent := (*calls)[0]
	// 人类说话应使用 delegate 模式，sender_id=OwnerID, sender_type=1
	assert.Equal(t, agentmsg.ModeDelegate, sent.IdentityMode)
}

func TestWriter_Write_RefinerApplied(t *testing.T) {
	// 自定义 Refiner：把原始文本改写为固定结果
	type mockRefiner struct{}
	mr := &mockRefiner{}
	_ = mr // 用 NoopRefiner 替代，验证 refined != raw 时 TranscriptRefined=true

	// 用一个会改写的 refiner
	type changingRefiner struct{}
	sendFn, calls, _ := callsegment.MockSendFn()

	// 包装一个会改写的 refiner
	refiner := &testRefiner{result: "您好，请问您是张先生吗？"}
	w := callsegment.New(refiner, sendFn, nil)

	_, err := w.Write(context.Background(), callsegment.WriteReq{
		SessionID:     "sess-2",
		CallID:        "call-456",
		SegmentSeq:    2,
		SpeakerRole:   "ai_bot",
		TranscriptRaw: "嗯 您好 那个 请问您是张先生吗",
	})
	require.NoError(t, err)

	sent := (*calls)[0]
	assert.Equal(t, "您好，请问您是张先生吗？", sent.Content)

	var extra model.CallSegmentExtra
	require.NoError(t, json.Unmarshal(sent.Extra, &extra))
	assert.Equal(t, "您好，请问您是张先生吗？", extra.Transcript)
	assert.Equal(t, "嗯 您好 那个 请问您是张先生吗", extra.TranscriptRaw)
	assert.True(t, extra.TranscriptRefined)
}

func TestWriter_Write_RefinerFails_FallbackToRaw(t *testing.T) {
	sendFn, calls, _ := callsegment.MockSendFn()
	w := callsegment.New(&failRefiner{}, sendFn, nil)

	_, err := w.Write(context.Background(), callsegment.WriteReq{
		SessionID:     "sess-3",
		CallID:        "call-789",
		SegmentSeq:    1,
		SpeakerRole:   "caller",
		TranscriptRaw: "原始文本",
	})
	require.NoError(t, err) // 降级不报错

	sent := (*calls)[0]
	assert.Equal(t, "原始文本", sent.Content) // 降级用原始文本
}

func TestWriter_Write_EmptyTranscript(t *testing.T) {
	sendFn, calls, _ := callsegment.MockSendFn()
	w := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)

	_, err := w.Write(context.Background(), callsegment.WriteReq{
		SessionID:     "sess-4",
		CallID:        "call-000",
		SegmentSeq:    1,
		SpeakerRole:   "caller",
		TranscriptRaw: "",
	})
	require.NoError(t, err)
	assert.Len(t, *calls, 1) // 空转写也写入（保留片段记录）
}

func TestWriter_Write_CountUpdaterCalled(t *testing.T) {
	sendFn, _, _ := callsegment.MockSendFn()
	var updatedCallIDs []string
	var mu sync.Mutex
	updater := func(_ context.Context, callID string) error {
		mu.Lock()
		updatedCallIDs = append(updatedCallIDs, callID)
		mu.Unlock()
		return nil
	}
	w := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, updater)

	_, err := w.Write(context.Background(), callsegment.WriteReq{
		SessionID: "sess-5", CallID: "call-cnt", SegmentSeq: 1,
		SpeakerRole: "caller", TranscriptRaw: "text",
	})
	require.NoError(t, err)

	// CountUpdater 是异步的，等待一下
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(updatedCallIDs) == 1
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, "call-cnt", updatedCallIDs[0])
}

func TestWriter_Write_DeduplicatesDoubaoHumanTranscript(t *testing.T) {
	sendFn, calls, lenFn := callsegment.MockSendFn()
	w := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)

	firstID, err := w.Write(context.Background(), callsegment.WriteReq{
		SessionID:     "sess-doubao",
		CallID:        "123",
		SegmentSeq:    1,
		SpeakerRole:   "caller",
		SpeakerUserID: 1001,
		TranscriptRaw: "豆包，豆包在不在？",
		Provider:      "doubao_realtime",
	})
	require.NoError(t, err)
	require.NotZero(t, firstID)

	secondID, err := w.Write(context.Background(), callsegment.WriteReq{
		SessionID:     "sess-doubao",
		CallID:        "123",
		SegmentSeq:    2,
		SpeakerRole:   "caller",
		SpeakerUserID: 1001,
		TranscriptRaw: "豆包，豆包在不在？",
		Provider:      "doubao_realtime",
	})
	require.NoError(t, err)

	assert.Equal(t, firstID, secondID)
	assert.Equal(t, 1, lenFn())
	require.Len(t, *calls, 1)
	assert.Equal(t, "voice-call-segment:123:1", (*calls)[0].ClientMsgID)
}

func TestWriter_Write_DoesNotDeduplicateDoubaoAITranscript(t *testing.T) {
	sendFn, _, lenFn := callsegment.MockSendFn()
	w := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)

	for seq := 1; seq <= 2; seq++ {
		_, err := w.Write(context.Background(), callsegment.WriteReq{
			SessionID:     "sess-doubao-ai",
			CallID:        "123",
			SegmentSeq:    seq,
			SpeakerRole:   "ai_bot",
			SpeakerUserID: 2001,
			TranscriptRaw: "我在，请问有什么可以帮你？",
			Provider:      "doubao_realtime",
		})
		require.NoError(t, err)
	}

	assert.Equal(t, 2, lenFn())
}

func TestWriter_Write_ConcurrentDoubaoHumanDuplicateReturnsExistingMsgID(t *testing.T) {
	var mu sync.Mutex
	sendCalls := 0
	msgID := snowflake.GenID()
	sendFn := func(_ context.Context, _ agentapi.SendMessageReq) (*agentapi.SendMessageResult, error) {
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		sendCalls++
		inboxSeq := int64(sendCalls)
		mu.Unlock()
		return &agentapi.SendMessageResult{MsgID: msgID, InboxSeq: inboxSeq}, nil
	}
	w := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)

	start := make(chan struct{})
	results := make(chan int64, 2)
	for seq := 1; seq <= 2; seq++ {
		seq := seq
		go func() {
			<-start
			id, err := w.Write(context.Background(), callsegment.WriteReq{
				SessionID:     "sess-doubao-concurrent",
				CallID:        "123",
				SegmentSeq:    seq,
				SpeakerRole:   "caller",
				SpeakerUserID: 1001,
				TranscriptRaw: "同一句话",
				Provider:      "doubao_realtime",
			})
			require.NoError(t, err)
			results <- id
		}()
	}
	close(start)

	firstID := <-results
	secondID := <-results

	assert.Equal(t, msgID, firstID)
	assert.Equal(t, msgID, secondID)
	mu.Lock()
	assert.Equal(t, 1, sendCalls)
	mu.Unlock()
}

// --- 测试辅助 ---

type testRefiner struct{ result string }

func (r *testRefiner) Refine(_ context.Context, _ string) (string, error) {
	return r.result, nil
}

type failRefiner struct{}

func (r *failRefiner) Refine(_ context.Context, raw string) (string, error) {
	return raw, assert.AnError
}

package callsegment

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNATSConsumer_handleMsg(t *testing.T) {
	sendFn, calls, lenFn := MockSendFn()
	refiner := &passthroughRefiner{}
	writer := New(refiner, sendFn, nil)

	lookup := func(_ context.Context, callID int64) (TranscriptRoute, error) {
		if callID == 123 {
			return TranscriptRoute{SessionID: "session-abc", OwnerID: 1001, AgentID: 2001}, nil
		}
		return TranscriptRoute{}, nil
	}

	c := &NATSConsumer{writer: writer, lookup: lookup}

	ev := transcriptEvent{
		CallID:        "123",
		SegmentSeq:    1,
		SpeakerRole:   "caller",
		TranscriptRaw: "hello world",
		Provider:      "openai_realtime",
		StartedAtMs:   1717000000000,
	}
	data, err := json.Marshal(ev)
	require.NoError(t, err)

	msg := &nats.Msg{Data: data, Subject: "voicebridge.transcript.123"}
	c.handleMsg(msg)

	// caller 转写走 debounce，handleMsg 后尚未写入；FlushCall 强制 flush。
	assert.Equal(t, 0, lenFn(), "caller 转写未 flush 时不应立即写入")
	c.FlushCall(123)
	assert.Equal(t, 1, lenFn(), "FlushCall 后应写入 1 条")

	require.Len(t, *calls, 1)
	req := (*calls)[0]
	assert.Equal(t, "session-abc", req.SessionID)
	assert.Equal(t, int16(6), req.MsgType)
	assert.Equal(t, "hello world", req.Content)

	var extra map[string]any
	require.NoError(t, json.Unmarshal(req.Extra, &extra))
	assert.Equal(t, "call_segment", extra["kind"])
	assert.Equal(t, "123", extra["call_id"])
	assert.EqualValues(t, 1, extra["segment_seq"])
	assert.Equal(t, "caller", extra["speaker_role"])
	assert.Equal(t, "hello world", extra["transcript"])
	assert.Equal(t, "openai_realtime", extra["transcript_provider"])
	assert.Equal(t, "final", extra["transcript_status"])
}

func TestNATSConsumer_handleMsg_InvalidCallID(t *testing.T) {
	sendFn, _, lenFn := MockSendFn()
	refiner := &passthroughRefiner{}
	writer := New(refiner, sendFn, nil)
	lookup := func(_ context.Context, _ int64) (TranscriptRoute, error) {
		return TranscriptRoute{SessionID: "s1", OwnerID: 1001, AgentID: 2001}, nil
	}

	c := &NATSConsumer{writer: writer, lookup: lookup}

	ev := map[string]any{"call_id": "not-a-number"}
	data, _ := json.Marshal(ev)
	c.handleMsg(&nats.Msg{Data: data, Subject: "voicebridge.transcript.nan"})

	assert.Equal(t, 0, lenFn(), "should not write for invalid call_id")
}

func TestNATSConsumer_handleMsg_EmptySession(t *testing.T) {
	sendFn, _, lenFn := MockSendFn()
	refiner := &passthroughRefiner{}
	writer := New(refiner, sendFn, nil)
	lookup := func(_ context.Context, _ int64) (TranscriptRoute, error) { return TranscriptRoute{}, nil }

	c := &NATSConsumer{writer: writer, lookup: lookup}

	ev := transcriptEvent{CallID: "999", SegmentSeq: 1, SpeakerRole: "ai_bot", TranscriptRaw: "hi"}
	data, _ := json.Marshal(ev)
	c.handleMsg(&nats.Msg{Data: data, Subject: "voicebridge.transcript.999"})

	assert.Equal(t, 0, lenFn(), "should not write for empty session_id")
}

// passthroughRefiner returns raw text unchanged.
type passthroughRefiner struct{}

func (p *passthroughRefiner) Refine(_ context.Context, raw string) (string, error) {
	return raw, nil
}

// TestNATSConsumer_callerDebounce 验证多句被聚合成一条消息写入，并触发一次 delegate。
func TestNATSConsumer_callerDebounce(t *testing.T) {
	fullRoute := TranscriptRoute{
		SessionID: "session-abc", OwnerID: 1001, AgentID: 2001,
		CallerID: 3001, CalleeID: 1001,
	}
	lookup := func(_ context.Context, callID int64) (TranscriptRoute, error) {
		if callID == 123 {
			return fullRoute, nil
		}
		return TranscriptRoute{}, nil
	}

	sendFn, calls, lenFn := MockSendFn()
	writer := New(&passthroughRefiner{}, sendFn, nil)
	c := &NATSConsumer{writer: writer, lookup: lookup}

	var delegateCalls []string
	c.SetDelegateTrigger(func(_ context.Context, _ string, _ int64, _ int64, content string, _ bool) {
		delegateCalls = append(delegateCalls, content)
	})

	sentences := []string{"你好，", "我想咨询一下，", "关于你们的产品。"}
	for i, s := range sentences {
		ev := transcriptEvent{CallID: "123", SegmentSeq: i + 1, SpeakerRole: "caller", TranscriptRaw: s}
		data, _ := json.Marshal(ev)
		c.handleMsg(&nats.Msg{Data: data, Subject: "voicebridge.transcript.123"})
	}

	// 未 flush 时不应有任何写入
	assert.Equal(t, 0, lenFn(), "debounce 期间不应写入")
	assert.Len(t, delegateCalls, 0, "debounce 期间不应触发 delegate")

	// flush 后应合并为一条
	c.FlushCall(123)
	assert.Equal(t, 1, lenFn(), "FlushCall 后应写入 1 条合并消息")
	require.Len(t, *calls, 1)
	assert.Equal(t, strings.Join(sentences, ""), (*calls)[0].Content, "内容应为所有句子拼接")
	assert.Len(t, delegateCalls, 1, "delegate 应只触发一次")
	assert.Equal(t, strings.Join(sentences, ""), delegateCalls[0])
}

// 接点A：转写触发文字托管的红线——仅 caller 触发，ai_bot/callee 绝不触发。
func TestNATSConsumer_handleMsg_DelegateTrigger(t *testing.T) {
	fullRoute := TranscriptRoute{
		SessionID: "session-abc", OwnerID: 1001, AgentID: 2001,
		CallerID: 3001, CalleeID: 1001,
	}
	lookup := func(_ context.Context, callID int64) (TranscriptRoute, error) {
		if callID == 123 {
			return fullRoute, nil
		}
		return TranscriptRoute{}, nil
	}

	type triggerCall struct {
		sessionID   string
		senderID    int64
		msgID       int64
		content     string
		selfTrigger bool
	}

	newConsumer := func() (*NATSConsumer, *[]triggerCall) {
		sendFn, _, _ := MockSendFn()
		writer := New(&passthroughRefiner{}, sendFn, nil)
		c := &NATSConsumer{writer: writer, lookup: lookup}
		var triggers []triggerCall
		c.SetDelegateTrigger(func(_ context.Context, sessionID string, senderID, msgID int64, content string, selfTrigger bool) {
			triggers = append(triggers, triggerCall{sessionID, senderID, msgID, content, selfTrigger})
		})
		return c, &triggers
	}

	send := func(c *NATSConsumer, role string) {
		ev := transcriptEvent{CallID: "123", SegmentSeq: 1, SpeakerRole: role, TranscriptRaw: "我想咨询一下"}
		data, _ := json.Marshal(ev)
		c.handleMsg(&nats.Msg{Data: data, Subject: "voicebridge.transcript.123"})
	}

	t.Run("caller triggers delegate after flush", func(t *testing.T) {
		c, triggers := newConsumer()
		send(c, "caller")
		assert.Len(t, *triggers, 0, "debounce 期间不触发")
		c.FlushCall(123)
		require.Len(t, *triggers, 1, "caller 转写 flush 后必须触发文字托管")
		tc := (*triggers)[0]
		assert.Equal(t, "session-abc", tc.sessionID)
		assert.EqualValues(t, 3001, tc.senderID, "senderID 应为访客(CallerID)")
		assert.Greater(t, tc.msgID, int64(0), "应携带已写入消息的 msgID")
		assert.Equal(t, "我想咨询一下", tc.content)
		assert.False(t, tc.selfTrigger, "访客代接通话不应标记 selfTrigger")
	})

	t.Run("direct AI call marks selfTrigger", func(t *testing.T) {
		directRoute := TranscriptRoute{
			SessionID: "session-direct", OwnerID: 1001, AgentID: 2001,
			CallerID: 1001, CalleeID: 2001, DirectAICall: true,
		}
		directLookup := func(_ context.Context, callID int64) (TranscriptRoute, error) {
			if callID == 456 {
				return directRoute, nil
			}
			return TranscriptRoute{}, nil
		}
		sendFn, _, _ := MockSendFn()
		writer := New(&passthroughRefiner{}, sendFn, nil)
		c := &NATSConsumer{writer: writer, lookup: directLookup}
		var triggers []triggerCall
		c.SetDelegateTrigger(func(_ context.Context, sessionID string, senderID, msgID int64, content string, selfTrigger bool) {
			triggers = append(triggers, triggerCall{sessionID, senderID, msgID, content, selfTrigger})
		})
		ev := transcriptEvent{CallID: "456", SegmentSeq: 1, SpeakerRole: "caller", TranscriptRaw: "看下重构进展"}
		data, _ := json.Marshal(ev)
		c.handleMsg(&nats.Msg{Data: data, Subject: "voicebridge.transcript.456"})
		c.FlushCall(456)
		require.Len(t, triggers, 1)
		assert.True(t, triggers[0].selfTrigger, "直拨通话转写必须标记 selfTrigger（说话人即托管 owner）")
		assert.EqualValues(t, 1001, triggers[0].senderID, "senderID 应为 owner 本人")
	})

	t.Run("ai_bot never triggers (循环红线)", func(t *testing.T) {
		c, triggers := newConsumer()
		send(c, "ai_bot")
		assert.Len(t, *triggers, 0, "ai_bot 转写绝不能触发，防止死循环")
	})

	t.Run("callee never triggers", func(t *testing.T) {
		c, triggers := newConsumer()
		send(c, "callee")
		assert.Len(t, *triggers, 0, "callee 转写不触发")
	})

	t.Run("nil trigger is safe", func(t *testing.T) {
		sendFn, _, lenFn := MockSendFn()
		writer := New(&passthroughRefiner{}, sendFn, nil)
		c := &NATSConsumer{writer: writer, lookup: lookup}
		send(c, "caller")
		c.FlushCall(123)
		assert.Equal(t, 1, lenFn(), "未注入 trigger 时仍正常写入，不 panic")
	})
}

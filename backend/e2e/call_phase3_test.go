package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/callsegment"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/voicerefiner"
	"github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

// TestCallPhase3_SegmentWriter_WritesMessage 验证 SegmentWriter 写入 msg_type=6 消息
func TestCallPhase3_SegmentWriter_WritesMessage(t *testing.T) {
	_ = snowflake.Init(1)
	testDB := setupE2E(t)
	store.DB = testDB.db.DB

	// 用真实 DB 的 sendFn（直接写 messages 表）
	var writtenMsgID int64
	sendFn := func(ctx context.Context, req agentapi.SendMessageReq) (*agentapi.SendMessageResult, error) {
		msg := model.Message{
			MsgID:      snowflake.GenID(),
			SessionID:  req.SessionID,
			SenderID:   req.OwnerID,
			SenderType: 3, // 系统
			MsgType:    req.MsgType,
			Content:    req.Content,
			Extra:      datatypes.JSON(req.Extra),
		}
		if err := store.DB.WithContext(ctx).Create(&msg).Error; err != nil {
			return nil, err
		}
		writtenMsgID = msg.MsgID
		return &agentapi.SendMessageResult{MsgID: msg.MsgID}, nil
	}

	writer := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, nil)

	msgID, err := writer.Write(context.Background(), callsegment.WriteReq{
		SessionID:     "sess-e2e-p3-1",
		CallID:        "call-e2e-001",
		SegmentSeq:    1,
		SpeakerRole:   "caller",
		SpeakerUserID: 9001,
		TranscriptRaw: "嗯 您好 请问您是张先生吗",
		Provider:      "openai_realtime",
		StartedAtMs:   1717000000000,
	})
	require.NoError(t, err)
	assert.NotZero(t, msgID)
	assert.Equal(t, writtenMsgID, msgID)

	// 验证 DB 中的消息
	var msg model.Message
	require.NoError(t, store.DB.Where("msg_id = ? AND session_id = ?", msgID, "sess-e2e-p3-1").First(&msg).Error)
	assert.Equal(t, model.MsgTypeCallSegment, msg.MsgType)
	assert.Equal(t, "嗯 您好 请问您是张先生吗", msg.Content)

	// 验证 extra 结构
	var extra model.CallSegmentExtra
	require.NoError(t, json.Unmarshal(msg.Extra, &extra))
	assert.Equal(t, "call_segment", extra.Kind)
	assert.Equal(t, "call-e2e-001", extra.CallID)
	assert.Equal(t, 1, extra.SegmentSeq)
	assert.Equal(t, "caller", extra.SpeakerRole)
	assert.Equal(t, "final", extra.TranscriptStatus)
	assert.Equal(t, "openai_realtime", extra.TranscriptProvider)
}

// TestCallPhase3_SegmentWriter_CountUpdater 验证 CountUpdater 递增 segment_count
func TestCallPhase3_SegmentWriter_CountUpdater(t *testing.T) {
	_ = snowflake.Init(1)
	testDB := setupE2E(t)
	store.DB = testDB.db.DB

	// 先创建一条 call_record
	callStore := store.NewCallRecordStore(store.DB)
	callID := snowflake.GenID()
	now := func() *interface{} { return nil }
	_ = now
	rec := model.CallRecord{
		ID:             callID,
		SessionID:      "sess-e2e-p3-2",
		CallerID:       9001,
		CalleeID:       9002,
		CallMode:       model.CallModeVoice,
		State:          model.CallStateActive,
		DelegationMode: model.CallDelegationHuman,
	}
	require.NoError(t, callStore.Create(context.Background(), &rec))

	// CountUpdater 调用 UpdateSegmentCount
	callIDStr := func(id int64) string {
		return string(rune(id)) // 简化：直接用 store
	}
	_ = callIDStr

	sendFn := func(_ context.Context, req agentapi.SendMessageReq) (*agentapi.SendMessageResult, error) {
		return &agentapi.SendMessageResult{MsgID: snowflake.GenID()}, nil
	}
	countUpdater := func(ctx context.Context, _ string) error {
		return callStore.UpdateSegmentCount(ctx, callID)
	}

	writer := callsegment.New(&voicerefiner.NoopRefiner{}, sendFn, countUpdater)

	// 写入 3 条片段
	for i := 1; i <= 3; i++ {
		_, err := writer.Write(context.Background(), callsegment.WriteReq{
			SessionID: "sess-e2e-p3-2", CallID: "call-e2e-002",
			SegmentSeq: i, SpeakerRole: "caller", TranscriptRaw: "text",
		})
		require.NoError(t, err)
	}

	// 等待异步 CountUpdater 完成
	assert.Eventually(t, func() bool {
		var got model.CallRecord
		store.DB.First(&got, callID)
		return got.SegmentCount == 3
	}, 3*time.Second, 50*time.Millisecond)
}

// TestCallPhase3_CallSegmentExtra_RoundTrip 验证 CallSegmentExtra JSON 序列化
func TestCallPhase3_CallSegmentExtra_RoundTrip(t *testing.T) {
	extra := model.CallSegmentExtra{
		Kind:               "call_segment",
		CallID:             "call-123",
		SegmentSeq:         5,
		SpeakerRole:        "ai_bot",
		SpeakerUserID:      "0",
		Transcript:         "您好，请问有什么可以帮您？",
		TranscriptRaw:      "嗯 您好 那个 请问有什么可以帮您",
		TranscriptStatus:   "final",
		TranscriptProvider: "openai_realtime",
		TranscriptRefined:  true,
		StartedAtMs:        1717000000000,
		EndedAtMs:          1717000002480,
	}

	data, err := extra.ToJSON()
	require.NoError(t, err)

	got, err := model.CallSegmentExtraFromJSON(data)
	require.NoError(t, err)
	assert.Equal(t, extra, *got)
}

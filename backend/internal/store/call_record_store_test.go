package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newCallStoreTestDB(t *testing.T) (*CallRecordStore, func()) {
	t.Helper()
	dsn := fmt.Sprintf("file:call_store_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.CallRecord{}))
	return NewCallRecordStore(db), func() { sqlDB.Close() }
}

func makeCallRecord(id int64) *model.CallRecord {
	now := time.Now()
	return &model.CallRecord{
		ID:             id,
		SessionID:      fmt.Sprintf("sess-%d", id),
		CallerID:       1001,
		CalleeID:       1002,
		CallMode:       model.CallModeVoice,
		State:          model.CallStateRinging,
		DelegationMode: model.CallDelegationHuman,
		StartedAt:      &now,
	}
}

func TestCallRecordStore_Create(t *testing.T) {
	s, cleanup := newCallStoreTestDB(t)
	defer cleanup()

	rec := makeCallRecord(1)
	require.NoError(t, s.Create(context.Background(), rec))

	var got model.CallRecord
	require.NoError(t, s.db.First(&got, rec.ID).Error)
	assert.Equal(t, rec.CallerID, got.CallerID)
	assert.Equal(t, model.CallStateRinging, got.State)
}

func TestCallRecordStore_UpdateAnswered(t *testing.T) {
	s, cleanup := newCallStoreTestDB(t)
	defer cleanup()

	rec := makeCallRecord(2)
	require.NoError(t, s.Create(context.Background(), rec))

	require.NoError(t, s.UpdateAnswered(context.Background(), rec.ID, time.Now()))

	var got model.CallRecord
	require.NoError(t, s.db.First(&got, rec.ID).Error)
	assert.Equal(t, model.CallStateActive, got.State)
	assert.NotNil(t, got.AnsweredAt)
}

func TestCallRecordStore_ListActiveDelegatedByOwner(t *testing.T) {
	s, cleanup := newCallStoreTestDB(t)
	defer cleanup()
	ctx := context.Background()
	const owner = int64(1002)

	// 构造各种状态/归属，验证仅返回 owner 名下 AI_DELEGATED / HUMAN_ACTIVE 两态。
	mk := func(id int64, calleeID int64, state int16) {
		rec := makeCallRecord(id)
		rec.CalleeID = calleeID
		rec.State = state
		require.NoError(t, s.Create(ctx, rec))
	}
	mk(1, owner, model.CallStateAIDelegated) // ✓
	mk(2, owner, model.CallStateHumanActive) // ✓
	mk(3, owner, model.CallStateRinging)     // ✗ 仅振铃，未代接
	mk(4, owner, model.CallStateEnded)       // ✗ 已结束
	mk(5, 9999, model.CallStateAIDelegated)  // ✗ 他人 owner

	got, err := s.ListActiveDelegatedByOwner(ctx, owner)
	require.NoError(t, err)

	ids := map[int64]bool{}
	for _, r := range got {
		ids[r.ID] = true
		assert.Equal(t, owner, r.CalleeID)
	}
	assert.Len(t, got, 2)
	assert.True(t, ids[1])
	assert.True(t, ids[2])
}

func TestCallRecordStore_UpdateEnd(t *testing.T) {
	s, cleanup := newCallStoreTestDB(t)
	defer cleanup()

	rec := makeCallRecord(3)
	require.NoError(t, s.Create(context.Background(), rec))

	dur := 42
	require.NoError(t, s.UpdateEnd(context.Background(), rec.ID, model.CallStateEnded, "hangup", time.Now(), &dur))

	var got model.CallRecord
	require.NoError(t, s.db.First(&got, rec.ID).Error)
	assert.Equal(t, model.CallStateEnded, got.State)
	assert.Equal(t, "hangup", got.EndReason)
	require.NotNil(t, got.DurationSeconds)
	assert.Equal(t, 42, *got.DurationSeconds)
}

func TestCallRecordStore_UpdateEnd_NilDuration(t *testing.T) {
	s, cleanup := newCallStoreTestDB(t)
	defer cleanup()

	rec := makeCallRecord(4)
	require.NoError(t, s.Create(context.Background(), rec))

	require.NoError(t, s.UpdateEnd(context.Background(), rec.ID, model.CallStateRejected, "rejected", time.Now(), nil))

	var got model.CallRecord
	require.NoError(t, s.db.First(&got, rec.ID).Error)
	assert.Equal(t, model.CallStateRejected, got.State)
	assert.Nil(t, got.DurationSeconds)
}

func TestCallRecordStore_UpdateAnsweredWithAI(t *testing.T) {
	s, cleanup := newCallStoreTestDB(t)
	defer cleanup()

	rec := makeCallRecord(5)
	require.NoError(t, s.Create(context.Background(), rec))

	agentID := int64(42)
	require.NoError(t, s.UpdateAnsweredWithAI(context.Background(), rec.ID, agentID, time.Now()))

	var got model.CallRecord
	require.NoError(t, s.db.First(&got, rec.ID).Error)
	assert.Equal(t, model.CallStateAIDelegated, got.State)
	assert.Equal(t, model.CallDelegationAIDelegated, got.DelegationMode)
	require.NotNil(t, got.DelegatedAgentID)
	assert.Equal(t, agentID, *got.DelegatedAgentID)
	assert.NotNil(t, got.AnsweredAt)
}

func TestCallRecordStore_UpdateRecordingURLs(t *testing.T) {
	s, cleanup := newCallStoreTestDB(t)
	defer cleanup()

	rec := makeCallRecord(6)
	require.NoError(t, s.Create(context.Background(), rec))

	require.NoError(t, s.UpdateRecordingURLs(context.Background(), rec.ID,
		"oss://caller.opus", "oss://callee.opus", "oss://ai.opus", "oss://mixed.opus"))

	var got model.CallRecord
	require.NoError(t, s.db.First(&got, rec.ID).Error)
	assert.Equal(t, "oss://caller.opus", got.RecordingCallerURL)
	assert.Equal(t, "oss://callee.opus", got.RecordingCalleeURL)
	assert.Equal(t, "oss://ai.opus", got.RecordingAIURL)
	assert.Equal(t, "oss://mixed.opus", got.RecordingMixedURL)
}

func TestCallRecordStore_UpdateSegmentCount(t *testing.T) {
	s, cleanup := newCallStoreTestDB(t)
	defer cleanup()

	rec := makeCallRecord(7)
	require.NoError(t, s.Create(context.Background(), rec))

	require.NoError(t, s.UpdateSegmentCount(context.Background(), rec.ID))
	require.NoError(t, s.UpdateSegmentCount(context.Background(), rec.ID))
	require.NoError(t, s.UpdateSegmentCount(context.Background(), rec.ID))

	var got model.CallRecord
	require.NoError(t, s.db.First(&got, rec.ID).Error)
	assert.Equal(t, 3, got.SegmentCount)
}

func TestCallRecordStore_ListActiveAIDelegatedBySession(t *testing.T) {
	s, cleanup := newCallStoreTestDB(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()
	stale := now.Add(-2 * time.Hour)
	mk := func(id int64, sessionID string, state int16, answeredAt *time.Time, provider string) {
		rec := makeCallRecord(id)
		rec.SessionID = sessionID
		rec.State = state
		rec.AnsweredAt = answeredAt
		rec.AIProvider = provider
		require.NoError(t, s.Create(ctx, rec))
	}
	mk(1, "sess-a", model.CallStateAIDelegated, &now, "doubao_realtime")   // ✓
	mk(2, "sess-a", model.CallStateHumanActive, &now, "doubao_realtime")   // ✗ 接管中不注入
	mk(3, "sess-a", model.CallStateEnded, &now, "doubao_realtime")         // ✗ 已结束
	mk(4, "sess-a", model.CallStateAIDelegated, &stale, "doubao_realtime") // ✗ 超时间窗（脏记录兜底）
	mk(5, "sess-b", model.CallStateAIDelegated, &now, "openai_realtime")   // ✗ 其他会话

	got, err := s.ListActiveAIDelegatedBySession(ctx, "sess-a", 40*time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, int64(1), got[0].ID)
	assert.Equal(t, "doubao_realtime", got[0].AIProvider)

	// 同会话两通活跃：调用方按歧义跳过——本方法如实返回 2 条
	mk(6, "sess-a", model.CallStateAIDelegated, &now, "doubao_realtime")
	got, err = s.ListActiveAIDelegatedBySession(ctx, "sess-a", 40*time.Minute)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

package e2e

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock room for e2e ---

type e2eMockRoom struct {
	mu      sync.Mutex
	created []int64
	closed  []int64
}

func (m *e2eMockRoom) CreateRoom(_ context.Context, callID int64, _, _ int64) (string, string, string, error) {
	m.mu.Lock()
	m.created = append(m.created, callID)
	m.mu.Unlock()
	return fmt.Sprintf("tok-caller-%d", callID), fmt.Sprintf("tok-callee-%d", callID), "wss://lk.test", nil
}

func (m *e2eMockRoom) CloseRoom(_ context.Context, callID int64) error {
	m.mu.Lock()
	m.closed = append(m.closed, callID)
	m.mu.Unlock()
	return nil
}

// --- e2e call test setup ---

type callE2ECtx struct {
	ctrl    *call.Controller
	room    *e2eMockRoom
	events  []notifyEvent
	eventMu sync.Mutex
	db      interface{ Close() }
}

type notifyEvent struct {
	userID  int64
	cmd     string
	payload any
}

func setupCallE2E(t *testing.T) *callE2ECtx {
	t.Helper()
	_ = snowflake.Init(1)

	testDB := setupE2E(t)
	store.DB = testDB.db.DB

	room := &e2eMockRoom{}
	persist := store.NewCallRecordStore(store.DB)

	ctx := &callE2ECtx{room: room, db: testDB.db}
	ctx.ctrl = call.New(room, persist, func(userID int64, cmd string, payload any) {
		ctx.eventMu.Lock()
		ctx.events = append(ctx.events, notifyEvent{userID, cmd, payload})
		ctx.eventMu.Unlock()
	})
	return ctx
}

func (c *callE2ECtx) findEvents(cmd string) []notifyEvent {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	var result []notifyEvent
	for _, e := range c.events {
		if e.cmd == cmd {
			result = append(result, e)
		}
	}
	return result
}

// --- tests ---

// TestCallFlow_InviteAnswerHangup 完整通话流程：发起→接听→挂断
func TestCallFlow_InviteAnswerHangup(t *testing.T) {
	ctx := setupCallE2E(t)
	defer ctx.db.Close()
	bg := context.Background()

	callID, tokenCaller, roomURL, err := ctx.ctrl.Invite(bg, 1001, 1002, "sess-e2e-1")
	require.NoError(t, err)
	assert.NotZero(t, callID)
	assert.Equal(t, fmt.Sprintf("tok-caller-%d", callID), tokenCaller)
	assert.Equal(t, "wss://lk.test", roomURL)

	// 验证 call_records 写入
	var rec model.CallRecord
	require.NoError(t, store.DB.First(&rec, callID).Error)
	assert.Equal(t, model.CallStateRinging, rec.State)
	assert.Equal(t, int64(1001), rec.CallerID)
	assert.Equal(t, int64(1002), rec.CalleeID)

	// B 接听
	tokenCallee, url, err := ctx.ctrl.Answer(bg, callID, 1002)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("tok-callee-%d", callID), tokenCallee)
	assert.Equal(t, "wss://lk.test", url)

	// 验证 answered_at 更新
	require.NoError(t, store.DB.First(&rec, callID).Error)
	assert.Equal(t, model.CallStateActive, rec.State)
	assert.NotNil(t, rec.AnsweredAt)

	// A 挂断
	err = ctx.ctrl.Hangup(bg, callID, 1001)
	require.NoError(t, err)

	// 验证 call:state 通知双方
	stateEvents := ctx.findEvents("call:state")
	assert.Len(t, stateEvents, 2)

	// 验证 call_records 最终状态
	require.NoError(t, store.DB.First(&rec, callID).Error)
	assert.Equal(t, model.CallStateEnded, rec.State)
	assert.NotNil(t, rec.EndedAt)
	assert.NotNil(t, rec.DurationSeconds)
}

// TestCallFlow_InviteReject A 发起 → B 拒接
func TestCallFlow_InviteReject(t *testing.T) {
	ctx := setupCallE2E(t)
	defer ctx.db.Close()
	bg := context.Background()

	callID, _, _, err := ctx.ctrl.Invite(bg, 2001, 2002, "sess-e2e-2")
	require.NoError(t, err)

	err = ctx.ctrl.Reject(bg, callID, 2002, "busy")
	require.NoError(t, err)

	// 双方收到 call:state
	stateEvents := ctx.findEvents("call:state")
	assert.Len(t, stateEvents, 2)

	// 验证 call_records 状态
	var rec model.CallRecord
	require.NoError(t, store.DB.First(&rec, callID).Error)
	assert.Equal(t, model.CallStateRejected, rec.State)
	assert.Equal(t, "rejected", rec.EndReason)
}

// TestCallFlow_InviteBusy B 已在通话中 → A 收到 busy 错误
func TestCallFlow_InviteBusy(t *testing.T) {
	ctx := setupCallE2E(t)
	defer ctx.db.Close()
	bg := context.Background()

	// B(3002) 先在一个通话中
	_, _, _, err := ctx.ctrl.Invite(bg, 3001, 3002, "sess-e2e-3a")
	require.NoError(t, err)

	// 再次呼叫 B(3002)
	_, _, _, err = ctx.ctrl.Invite(bg, 3003, 3002, "sess-e2e-3b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "busy")
}

// TestCallFlow_Timeout 超时未接 → 通话结束
func TestCallFlow_Timeout(t *testing.T) {
	ctx := setupCallE2E(t)
	defer ctx.db.Close()
	bg := context.Background()

	callID, _, _, err := ctx.ctrl.Invite(bg, 4001, 4002, "sess-e2e-4")
	require.NoError(t, err)

	// 模拟超时：直接 Hangup（超时内部也走 endCall）
	err = ctx.ctrl.Hangup(bg, callID, 4001)
	require.NoError(t, err)

	// 验证 call_records 状态
	var rec model.CallRecord
	require.NoError(t, store.DB.First(&rec, callID).Error)
	assert.Equal(t, model.CallStateEnded, rec.State)

	// 验证 Room 被关闭
	assert.Len(t, ctx.room.closed, 1)
}

// TestCallFlow_CallRecordDuration 验证通话时长计算
func TestCallFlow_CallRecordDuration(t *testing.T) {
	ctx := setupCallE2E(t)
	defer ctx.db.Close()
	bg := context.Background()

	callID, _, _, err := ctx.ctrl.Invite(bg, 5001, 5002, "sess-e2e-5")
	require.NoError(t, err)

	_, _, err = ctx.ctrl.Answer(bg, callID, 5002)
	require.NoError(t, err)

	// 等待一小段时间确保时长 > 0
	time.Sleep(10 * time.Millisecond)

	err = ctx.ctrl.Hangup(bg, callID, 5001)
	require.NoError(t, err)

	var rec model.CallRecord
	require.NoError(t, store.DB.First(&rec, callID).Error)
	assert.NotNil(t, rec.DurationSeconds)
	assert.GreaterOrEqual(t, *rec.DurationSeconds, 0)
}

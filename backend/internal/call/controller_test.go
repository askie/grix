package call_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	_ = snowflake.Init(1)
	os.Exit(m.Run())
}

// --- mock RoomProvider ---

type mockRoom struct {
	mu        sync.Mutex
	created   []int64
	closed    []int64
	createErr error
}

func (m *mockRoom) CreateRoom(_ context.Context, callID int64, _, _ int64) (string, string, string, error) {
	if m.createErr != nil {
		return "", "", "", m.createErr
	}
	m.mu.Lock()
	m.created = append(m.created, callID)
	m.mu.Unlock()
	return "tok-caller", "tok-callee", "wss://lk.test", nil
}

func (m *mockRoom) CloseRoom(_ context.Context, callID int64) error {
	m.mu.Lock()
	m.closed = append(m.closed, callID)
	m.mu.Unlock()
	return nil
}

// --- mock Persister ---

type mockPersist struct {
	mu          sync.Mutex
	created     []*model.CallRecord
	answered    []int64
	ended       []int64
	handovers   []model.CallHandoverEvent
	createErr   error
	answeredErr error
}

func (m *mockPersist) Create(_ context.Context, r *model.CallRecord) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.mu.Lock()
	m.created = append(m.created, r)
	m.mu.Unlock()
	return nil
}

func (m *mockPersist) UpdateAnswered(_ context.Context, callID int64, _ time.Time) error {
	if m.answeredErr != nil {
		return m.answeredErr
	}
	m.mu.Lock()
	m.answered = append(m.answered, callID)
	m.mu.Unlock()
	return nil
}

func (m *mockPersist) UpdateAnsweredWithAI(_ context.Context, callID, _ int64, _ time.Time) error {
	if m.answeredErr != nil {
		return m.answeredErr
	}
	m.mu.Lock()
	m.answered = append(m.answered, callID)
	m.mu.Unlock()
	return nil
}

func (m *mockPersist) UpdateEnd(_ context.Context, callID int64, _ int16, _ string, _ time.Time, _ *int) error {
	m.mu.Lock()
	m.ended = append(m.ended, callID)
	m.mu.Unlock()
	return nil
}
func (m *mockPersist) UpdateHandover(_ context.Context, _ int64, event model.CallHandoverEvent, _ int16, _ string) error {
	m.mu.Lock()
	m.handovers = append(m.handovers, event)
	m.mu.Unlock()
	return nil
}

func (m *mockPersist) UpdateRecordingURLs(_ context.Context, _ int64, _, _, _, _ string) error {
	return nil
}

// --- helpers ---

type notifyRecord struct {
	userID  int64
	cmd     string
	payload any
}

func newController() (*call.Controller, *mockRoom, *mockPersist, *[]notifyRecord) {
	room := &mockRoom{}
	persist := &mockPersist{}
	var events []notifyRecord
	var mu sync.Mutex
	notify := func(userID int64, cmd string, payload any) {
		mu.Lock()
		events = append(events, notifyRecord{userID, cmd, payload})
		mu.Unlock()
	}
	ctrl := call.New(room, persist, notify)
	return ctrl, room, persist, &events
}

// --- tests ---

func TestInviteAnswerHangup(t *testing.T) {
	ctrl, room, persist, events := newController()
	ctx := context.Background()

	callID, tokenCaller, roomURL, err := ctrl.Invite(ctx, 1, 2, "sess-1")
	require.NoError(t, err)
	assert.NotZero(t, callID)
	assert.Equal(t, "tok-caller", tokenCaller)
	assert.Equal(t, "wss://lk.test", roomURL)
	assert.Len(t, persist.created, 1)
	assert.Equal(t, model.CallStateRinging, persist.created[0].State)

	tokenCallee, url, err := ctrl.Answer(ctx, callID, 2)
	require.NoError(t, err)
	assert.Equal(t, "tok-callee", tokenCallee)
	assert.Equal(t, "wss://lk.test", url)
	assert.Len(t, persist.answered, 1)

	err = ctrl.Hangup(ctx, callID, 1)
	require.NoError(t, err)
	assert.Len(t, persist.ended, 1)
	assert.Len(t, room.closed, 1)
	// Answer 通知 caller peer_answered + Hangup 通知双方 call:state = 3 条事件
	assert.Len(t, *events, 3)
}

func TestInviteReject(t *testing.T) {
	ctrl, _, persist, events := newController()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-2")
	require.NoError(t, err)

	err = ctrl.Reject(ctx, callID, 2, "busy")
	require.NoError(t, err)
	assert.Len(t, persist.ended, 1)
	assert.Len(t, *events, 2)
}

func TestInviteTimeout(t *testing.T) {
	ctrl, _, persist, events := newController()
	ctx := context.Background()

	// 模拟超时：直接 Hangup（超时内部也走 endCall）
	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-3")
	require.NoError(t, err)

	err = ctrl.Hangup(ctx, callID, 1)
	require.NoError(t, err)
	assert.Len(t, persist.ended, 1)
	assert.Len(t, *events, 2)
}

// TestInviteRingTimeout 验证超时回调真实触发（使用极短超时）。
func TestInviteRingTimeout(t *testing.T) {
	room := &mockRoom{}
	persist := &mockPersist{}
	var events []notifyRecord
	var mu sync.Mutex
	notify := func(userID int64, cmd string, payload any) {
		mu.Lock()
		events = append(events, notifyRecord{userID, cmd, payload})
		mu.Unlock()
	}
	// 注入 10ms 超时
	ctrl := call.NewWithTimeout(room, persist, notify, 10*time.Millisecond)

	callID, _, _, err := ctrl.Invite(context.Background(), 1, 2, "sess-timeout")
	require.NoError(t, err)

	// 等待超时触发
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	eventCount := len(events)
	mu.Unlock()

	// 超时后双方应收到 call:state 通知
	assert.Equal(t, 2, eventCount, "both parties should be notified on timeout")
	assert.Len(t, persist.ended, 1, "call record should be ended")
	// 超时后通话不再存在，Hangup 应返回 not found
	err = ctrl.Hangup(context.Background(), callID, 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestInviteBusy(t *testing.T) {
	ctrl, _, _, _ := newController()
	ctx := context.Background()

	// callee=2 先在一个通话中
	_, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-4")
	require.NoError(t, err)

	// 再次呼叫 callee=2 应返回 busy
	_, _, _, err = ctrl.Invite(ctx, 3, 2, "sess-5")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "busy")
}

func TestCallerBusy(t *testing.T) {
	ctrl, _, _, _ := newController()
	ctx := context.Background()

	// caller=1 已在通话中（作为 callee）
	_, _, _, err := ctrl.Invite(ctx, 3, 1, "sess-6")
	require.NoError(t, err)

	// caller=1 再发起呼叫，callee=1 busy
	_, _, _, err = ctrl.Invite(ctx, 2, 1, "sess-7")
	require.Error(t, err)
}

func TestAnswerWrongCallee(t *testing.T) {
	ctrl, _, _, _ := newController()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-8")
	require.NoError(t, err)

	_, _, err = ctrl.Answer(ctx, callID, 99) // 错误的 callee
	require.Error(t, err)
	assert.Contains(t, err.Error(), "callee mismatch")
}

func TestAnswerPersistUpdateAnsweredFailure(t *testing.T) {
	room := &mockRoom{}
	persist := &mockPersist{answeredErr: fmt.Errorf("db timeout")}
	ctrl := call.New(room, persist, func(_ int64, _ string, _ any) {})

	callID, _, _, err := ctrl.Invite(context.Background(), 1, 2, "sess-answer-fail")
	require.NoError(t, err)

	// Answer 应返回错误（不静默忽略 DB 失败）
	_, _, err = ctrl.Answer(context.Background(), callID, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist answered")
}

func TestAnswerAlreadyActive(t *testing.T) {
	ctrl, _, _, _ := newController()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-double-answer")
	require.NoError(t, err)

	// 第一次接听成功
	_, _, err = ctrl.Answer(ctx, callID, 2)
	require.NoError(t, err)

	// 第二次接听应失败（已是 ACTIVE 状态）
	_, _, err = ctrl.Answer(ctx, callID, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ringing")
}

func TestHangupNotInCall(t *testing.T) {
	ctrl, _, _, _ := newController()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-9")
	require.NoError(t, err)

	err = ctrl.Hangup(ctx, callID, 99) // 不在通话中的用户
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in call")
}

func TestConcurrentInviteSameCallee(t *testing.T) {
	// 两个 goroutine 同时对同一 callee 发起 Invite，只有一个应成功
	room := &mockRoom{}
	persist := &mockPersist{}
	ctrl := call.New(room, persist, func(_ int64, _ string, _ any) {})

	var wg sync.WaitGroup
	results := make([]error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			_, _, _, err := ctrl.Invite(context.Background(), int64(100+idx), 200, fmt.Sprintf("sess-concurrent-%d", idx))
			results[idx] = err
		}()
	}
	wg.Wait()

	// 只有一个应成功，其余应返回 ErrCalleeBusy
	successCount := 0
	for _, err := range results {
		if err == nil {
			successCount++
		} else {
			assert.ErrorIs(t, err, call.ErrCalleeBusy)
		}
	}
	assert.Equal(t, 1, successCount, "exactly one concurrent invite should succeed")
}

func TestConcurrentHangup(t *testing.T) {
	ctrl, _, _, _ := newController()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-10")
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ctrl.Hangup(ctx, callID, 1)
		}()
	}
	wg.Wait()
	// 不应 panic，第一次成功，后续返回 not found（忽略错误）
}

func TestInvitePersistCreateFailure(t *testing.T) {
	room := &mockRoom{}
	persist := &mockPersist{createErr: fmt.Errorf("db down")}
	ctrl := call.New(room, persist, func(_ int64, _ string, _ any) {})

	_, _, _, err := ctrl.Invite(context.Background(), 1, 2, "sess-fail")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist call")
	// Room 应被关闭（回滚）
	assert.Len(t, room.closed, 1)
}

func TestRejectCalleeMismatch(t *testing.T) {
	ctrl, _, _, _ := newController()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-mismatch")
	require.NoError(t, err)

	// 用错误的 calleeID 拒接
	err = ctrl.Reject(ctx, callID, 99, "busy")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "callee mismatch")
}

func TestShutdownCleansUpActiveCalls(t *testing.T) {
	ctrl, room, persist, events := newController()
	ctx := context.Background()

	// 创建两个通话
	id1, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-shutdown-1")
	require.NoError(t, err)
	id2, _, _, err := ctrl.Invite(ctx, 3, 4, "sess-shutdown-2")
	require.NoError(t, err)

	ctrl.Shutdown(ctx)

	// 两个通话都应被结束
	assert.Len(t, persist.ended, 2)
	assert.Len(t, room.closed, 2)
	// 双方各收到 call:state，共 4 条通知
	assert.Len(t, *events, 4)

	// Shutdown 后 Hangup 应返回 not found
	err = ctrl.Hangup(ctx, id1, 1)
	assert.Error(t, err)
	err = ctrl.Hangup(ctx, id2, 3)
	assert.Error(t, err)
}

// Shutdown 之后再邀请必须被拒绝——否则新通话插进 c.calls 却拿不到响铃定时器
// （trackedAfterFunc 一看 bgClosing 就返回 nil），既不会超时收尾，也赶不上
// Shutdown 已经跑完的那次收尾扫描，状态永远悬在 RINGING。
func TestInviteRejectedAfterShutdown(t *testing.T) {
	ctrl, _, _, _ := newController()
	ctx := context.Background()

	ctrl.Shutdown(ctx)

	_, _, _, err := ctrl.Invite(ctx, 5, 6, "sess-after-shutdown")
	require.Error(t, err)
	assert.ErrorIs(t, err, call.ErrControllerShuttingDown)
}

func TestInviteSelfCall(t *testing.T) {
	ctrl, _, _, _ := newController()
	_, _, _, err := ctrl.Invite(context.Background(), 1, 1, "sess-self")
	require.Error(t, err)
	assert.ErrorIs(t, err, call.ErrSelfCall)
}

func TestInviteCallerBusy(t *testing.T) {
	ctrl, _, _, _ := newController()
	ctx := context.Background()

	// caller=1 已在通话中（作为 caller）
	_, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-caller-busy-1")
	require.NoError(t, err)

	// caller=1 再次发起呼叫 → 应返回 caller busy
	_, _, _, err = ctrl.Invite(ctx, 1, 3, "sess-caller-busy-2")
	require.Error(t, err)
	assert.ErrorIs(t, err, call.ErrCallerBusy)
}

// TestAnswerNotifiesCaller 验证 Answer 后 caller 收到 call:peer_answered 通知。
func TestAnswerNotifiesCaller(t *testing.T) {
	ctrl, _, _, events := newController()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-notify")
	require.NoError(t, err)

	_, _, err = ctrl.Answer(ctx, callID, 2)
	require.NoError(t, err)

	// caller(1) 应收到 call:peer_answered
	found := false
	for _, e := range *events {
		if e.userID == 1 && e.cmd == "call:peer_answered" {
			found = true
			break
		}
	}
	assert.True(t, found, "caller should receive call:peer_answered after Answer")
}

func TestInviteCalleeBusySentinel(t *testing.T) {
	ctrl, _, _, _ := newController()
	ctx := context.Background()

	_, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-callee-busy-1")
	require.NoError(t, err)

	_, _, _, err = ctrl.Invite(ctx, 3, 2, "sess-callee-busy-2")
	require.Error(t, err)
	assert.ErrorIs(t, err, call.ErrCalleeBusy)
}

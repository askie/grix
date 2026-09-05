package liveactivity

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type capturedTask struct {
	userID  int64
	payload protocol.LiveActivityPayload
}

type taskRecorder struct {
	mu    sync.Mutex
	tasks []capturedTask
}

func (r *taskRecorder) record(userID int64, payload protocol.LiveActivityPayload) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks = append(r.tasks, capturedTask{userID: userID, payload: payload})
	return nil
}

func (r *taskRecorder) all() []capturedTask {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]capturedTask, len(r.tasks))
	copy(out, r.tasks)
	return out
}

func (r *taskRecorder) byEvent(event string) []capturedTask {
	var out []capturedTask
	for _, task := range r.all() {
		if task.payload.Event == event {
			out = append(out, task)
		}
	}
	return out
}

func setupNotifierTest(t *testing.T) (*testutil.TestDB, *taskRecorder) {
	t.Helper()
	logger.Init()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	recorder := &taskRecorder{}
	originalPublish := publishTask
	originalDebounce := startDebounce
	originalTitleWindow := titleCoalesceWindow
	publishTask = recorder.record
	// 测试里没人愿意真等 10 秒；行为（到点复查一次）与生产完全一样。
	startDebounce = 20 * time.Millisecond
	titleCoalesceWindow = 20 * time.Millisecond

	t.Cleanup(func() {
		publishTask = originalPublish
		startDebounce = originalDebounce
		titleCoalesceWindow = originalTitleWindow
		pendingStartsMu.Lock()
		pendingStarts = make(map[string]struct{})
		pendingStartsMu.Unlock()
		testDB.Close()
		store.DB = nil
		store.RDB = nil
	})
	return testDB, recorder
}

func seedRunningState(t *testing.T, run Run, state, title string) {
	t.Helper()
	started := time.Now().UTC()
	row := model.SessionAgentState{
		SessionID: run.SessionID,
		OwnerID:   run.UserID,
		AgentID:   run.AgentID,
		State:     state,
		TaskTitle: title,
		StartedAt: &started,
		UpdatedAt: started,
	}
	if err := store.DB.Save(&row).Error; err != nil {
		t.Fatalf("seed chat_states: %v", err)
	}
}

func setState(t *testing.T, run Run, state string) {
	t.Helper()
	if err := store.DB.Model(&model.SessionAgentState{}).
		Where("session_id = ? AND owner_id = ?", run.SessionID, run.UserID).
		Update("state", state).Error; err != nil {
		t.Fatalf("update chat_states: %v", err)
	}
}

func waitForDebounce() { time.Sleep(120 * time.Millisecond) }

func TestOnRunningStartsCardAfterDebounce(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	run := Run{UserID: 7001, AgentID: 8001, SessionID: "session-start"}
	seedRunningState(t, run, model.SessionAgentStateRunning, "重构支付模块")

	OnRunning(run)
	waitForDebounce()

	starts := recorder.byEvent(protocol.LiveActivityEventStart)
	if len(starts) != 1 {
		t.Fatalf("expected 1 start, got %d (%+v)", len(starts), recorder.all())
	}
	payload := starts[0].payload
	if payload.ContentState.Phase != protocol.LiveActivityPhaseRunning {
		t.Fatalf("start phase = %s, want running", payload.ContentState.Phase)
	}
	if payload.ContentState.Title != "重构支付模块" {
		t.Fatalf("start title = %q, want the chat_states task title", payload.ContentState.Title)
	}
	if payload.Attributes.SessionID != run.SessionID {
		t.Fatalf("start attributes session = %q", payload.Attributes.SessionID)
	}
	if !hasLiveCard(run.UserID, run.SessionID) {
		t.Fatal("start should have indexed the card")
	}
}

// 观察窗口存在的全部理由：绝大多数 run 活不过 10 秒，锁屏上不该闪一下卡片。
func TestOnRunningSkipsStartWhenRunEndsWithinDebounce(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	run := Run{UserID: 7002, AgentID: 8002, SessionID: "session-short"}
	seedRunningState(t, run, model.SessionAgentStateRunning, "短任务")

	OnRunning(run)
	setState(t, run, model.SessionAgentStateCompleted)
	waitForDebounce()

	if tasks := recorder.all(); len(tasks) != 0 {
		t.Fatalf("expected no push for a run shorter than the debounce, got %+v", tasks)
	}
	if hasLiveCard(run.UserID, run.SessionID) {
		t.Fatal("no card should have been indexed")
	}
}

func TestOnWaitingSendsExactlyOneAlertingUpdate(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	run := Run{UserID: 7003, AgentID: 8003, SessionID: "session-waiting"}
	seedRunningState(t, run, model.SessionAgentStateRunning, "部署")

	OnRunning(run)
	waitForDebounce()

	setState(t, run, model.SessionAgentStateWaitingApproval)
	OnWaiting(run, protocol.LiveActivityPhaseWaitingApproval, "要删除生产数据库")

	updates := recorder.byEvent(protocol.LiveActivityEventUpdate)
	if len(updates) != 1 {
		t.Fatalf("expected exactly 1 update, got %d", len(updates))
	}
	payload := updates[0].payload
	if payload.Alert == nil {
		t.Fatal("a waiting update must carry an alert")
	}
	if payload.ContentState.Phase != protocol.LiveActivityPhaseWaitingApproval {
		t.Fatalf("update phase = %s", payload.ContentState.Phase)
	}
	if payload.Alert.Body != "要删除生产数据库" {
		t.Fatalf("alert body = %q, want the agent summary", payload.Alert.Body)
	}
}

// 没开过卡就别发更新：主人关了推送、或 run 太短没熬过观察窗口，都属于这种。
func TestOnWaitingWithoutCardSendsNothing(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	run := Run{UserID: 7004, AgentID: 8004, SessionID: "session-no-card"}
	seedRunningState(t, run, model.SessionAgentStateWaitingApproval, "无卡")

	OnWaiting(run, protocol.LiveActivityPhaseWaitingApproval, "审批")

	if tasks := recorder.all(); len(tasks) != 0 {
		t.Fatalf("expected no push without a live card, got %+v", tasks)
	}
}

func TestOnTerminalEndsCardAndClearsIndex(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	run := Run{UserID: 7005, AgentID: 8005, SessionID: "session-terminal"}
	seedRunningState(t, run, model.SessionAgentStateRunning, "跑完了")

	OnRunning(run)
	waitForDebounce()

	before := time.Now()
	OnTerminal(run, protocol.LiveActivityPhaseCompleted, "")

	ends := recorder.byEvent(protocol.LiveActivityEventEnd)
	if len(ends) != 1 {
		t.Fatalf("expected 1 end, got %d", len(ends))
	}
	payload := ends[0].payload
	if payload.ContentState.Phase != protocol.LiveActivityPhaseCompleted {
		t.Fatalf("end phase = %s", payload.ContentState.Phase)
	}
	wantDismissal := before.Add(dismissalDelay).UnixMilli()
	if payload.DismissalAtMs < wantDismissal {
		t.Fatalf("dismissal_at_ms = %d, want at least now+5min (%d)", payload.DismissalAtMs, wantDismissal)
	}
	if hasLiveCard(run.UserID, run.SessionID) {
		t.Fatal("end must clear the card index")
	}

	// 并发 / 重投的终态包不该再收一次卡。
	OnTerminal(run, protocol.LiveActivityPhaseCompleted, "")
	if got := len(recorder.byEvent(protocol.LiveActivityEventEnd)); got != 1 {
		t.Fatalf("a second terminal must not publish another end, got %d", got)
	}
}

func TestStartEvictsOldestCardBeyondLimit(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	const userID = int64(7006)

	for i := 0; i < maxActivitiesPerUser+1; i++ {
		run := Run{UserID: userID, AgentID: 9000 + int64(i), SessionID: fmt.Sprintf("session-cap-%d", i)}
		seedRunningState(t, run, model.SessionAgentStateRunning, fmt.Sprintf("任务 %d", i))
		OnRunning(run)
		waitForDebounce()
	}

	if got := len(recorder.byEvent(protocol.LiveActivityEventStart)); got != maxActivitiesPerUser+1 {
		t.Fatalf("expected %d starts, got %d", maxActivitiesPerUser+1, got)
	}
	ends := recorder.byEvent(protocol.LiveActivityEventEnd)
	if len(ends) != 1 {
		t.Fatalf("expected the oldest card to be ended once, got %d", len(ends))
	}
	if ends[0].payload.SessionID != "session-cap-0" {
		t.Fatalf("evicted %q, want the oldest card session-cap-0", ends[0].payload.SessionID)
	}

	cards, err := store.RDB.HGetAll(context.Background(), activityIndexKey(userID)).Result()
	if err != nil {
		t.Fatalf("read card index: %v", err)
	}
	if len(cards) != maxActivitiesPerUser {
		t.Fatalf("card index holds %d entries, want %d", len(cards), maxActivitiesPerUser)
	}
	if _, still := cards["session-cap-0"]; still {
		t.Fatal("the evicted session must be gone from the index")
	}
}

func TestOnResumedUpdatesRunningOnlyWithLiveCard(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	run := Run{UserID: 7007, AgentID: 8007, SessionID: "session-resume"}
	seedRunningState(t, run, model.SessionAgentStateRunning, "继续")

	OnResumed(run)
	if tasks := recorder.all(); len(tasks) != 0 {
		t.Fatalf("resume without a card must publish nothing, got %+v", tasks)
	}

	OnRunning(run)
	waitForDebounce()
	setState(t, run, model.SessionAgentStateWaitingApproval)
	OnWaiting(run, protocol.LiveActivityPhaseWaitingApproval, "批一下")
	OnResumed(run)

	updates := recorder.byEvent(protocol.LiveActivityEventUpdate)
	if len(updates) != 2 {
		t.Fatalf("expected waiting + resumed updates, got %d", len(updates))
	}
	resumed := updates[1].payload
	if resumed.ContentState.Phase != protocol.LiveActivityPhaseRunning {
		t.Fatalf("resumed phase = %s, want running", resumed.ContentState.Phase)
	}
	if resumed.Alert != nil {
		t.Fatal("a resumed update must not alert")
	}
}

func TestOnTitleChangedCoalescesToOneUpdate(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	run := Run{UserID: 7008, AgentID: 8008, SessionID: "session-title"}
	seedRunningState(t, run, model.SessionAgentStateRunning, "旧标题")

	OnRunning(run)
	waitForDebounce()

	OnTitleChanged(run.SessionID)
	OnTitleChanged(run.SessionID)
	if err := store.DB.Model(&model.SessionAgentState{}).
		Where("session_id = ?", run.SessionID).
		Update("task_title", "新标题").Error; err != nil {
		t.Fatalf("rename: %v", err)
	}
	OnTitleChanged(run.SessionID)
	waitForDebounce()

	updates := recorder.byEvent(protocol.LiveActivityEventUpdate)
	if len(updates) != 1 {
		t.Fatalf("three renames within the window must collapse to 1 update, got %d", len(updates))
	}
	if updates[0].payload.ContentState.Title != "新标题" {
		t.Fatalf("title update carried %q", updates[0].payload.ContentState.Title)
	}
}

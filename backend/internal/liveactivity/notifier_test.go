package liveactivity

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/notification"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
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

// 把 run 生命周期通知的推送通道全关掉的主人，锁屏上也不该冒出卡片。
// 开卡是唯一一次判定推送偏好的地方，等待转换同样要过这道门。
func TestWaitingStartRespectsPushPreference(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	run := Run{UserID: 7004, AgentID: 8004, SessionID: "session-push-off"}
	seedRunningState(t, run, model.SessionAgentStateWaitingApproval, "无卡")
	disablePushChannel(t, run.UserID)

	OnWaiting(run, protocol.LiveActivityPhaseWaitingApproval, "审批")

	if tasks := recorder.all(); len(tasks) != 0 {
		t.Fatalf("expected no card when push is off for every lifecycle event, got %+v", tasks)
	}
	if hasLiveCard(run.UserID, run.SessionID) {
		t.Fatal("no card should have been indexed")
	}
}

// disablePushChannel 把该用户全部 run 生命周期通知改成只走 TTS。
func disablePushChannel(t *testing.T, userID int64) {
	t.Helper()
	for _, key := range []string{
		notification.EventApprovalRequested,
		notification.EventAgentQuestion,
		notification.EventTaskCompleted,
		notification.EventTaskFailed,
		notification.EventTaskStoppedUnexpected,
	} {
		row := model.NotificationPref{
			UserID:   userID,
			EventKey: key,
			Enabled:  true,
			Channels: datatypes.JSON([]byte(`["tts"]`)),
		}
		if err := store.DB.Save(&row).Error; err != nil {
			t.Fatalf("seed notification pref %s: %v", key, err)
		}
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

// agent 一上来就要审批是常事：那条 run 整个生命周期都不会经过"跑满 10 秒"，
// 等观察窗口等于最需要卡片的那种 run 反而没有卡。
func TestOnWaitingStartsCardImmediatelyWhenNoneExists(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	run := Run{UserID: 7009, AgentID: 8009, SessionID: "session-early-waiting"}
	seedRunningState(t, run, model.SessionAgentStateWaitingApproval, "删库前先问一句")

	OnWaiting(run, protocol.LiveActivityPhaseWaitingApproval, "要删除生产数据库")

	// 不等防抖：调用返回时卡就该已经开出去了。
	tasks := recorder.all()
	if len(tasks) != 1 {
		t.Fatalf("expected the card to start synchronously, got %+v", tasks)
	}
	payload := tasks[0].payload
	if payload.Event != protocol.LiveActivityEventStart {
		t.Fatalf("event = %s, want start", payload.Event)
	}
	if payload.ContentState.Phase != protocol.LiveActivityPhaseWaitingApproval {
		t.Fatalf("start phase = %s, want the waiting phase", payload.ContentState.Phase)
	}
	if payload.Alert == nil || payload.Alert.Body != "要删除生产数据库" {
		t.Fatalf("an early-waiting start must carry the alert, got %+v", payload.Alert)
	}
	if !hasLiveCard(run.UserID, run.SessionID) {
		t.Fatal("the card must be indexed like any other start")
	}

	// 卡已经开了，之后的等待转换回到普通的 update 路径。
	OnWaiting(run, protocol.LiveActivityPhaseWaitingQuestion, "选哪个分支")
	if got := len(recorder.byEvent(protocol.LiveActivityEventStart)); got != 1 {
		t.Fatalf("a second waiting transition must not start another card, got %d starts", got)
	}
	if got := len(recorder.byEvent(protocol.LiveActivityEventUpdate)); got != 1 {
		t.Fatalf("expected 1 update after the card exists, got %d", got)
	}
}

// 观察窗口里转成等待、而 OnWaiting 那一路没赶上（后台协程竞争）时的兜底：
// 复查看到的是 waiting_*，照样开卡，阶段按实际状态填。
func TestDebouncedStartOpensCardInWaitingPhase(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	run := Run{UserID: 7010, AgentID: 8010, SessionID: "session-debounced-waiting"}
	seedRunningState(t, run, model.SessionAgentStateRunning, "等下要问")

	OnRunning(run)
	setState(t, run, model.SessionAgentStateWaitingQuestion)
	waitForDebounce()

	starts := recorder.byEvent(protocol.LiveActivityEventStart)
	if len(starts) != 1 {
		t.Fatalf("expected 1 start, got %d (%+v)", len(starts), recorder.all())
	}
	if starts[0].payload.ContentState.Phase != protocol.LiveActivityPhaseWaitingQuestion {
		t.Fatalf("start phase = %s, want waiting_question", starts[0].payload.ContentState.Phase)
	}
	// alert 归 OnWaiting 管：这条兜底路径只知道状态，不知道 agent 在问什么。
	if starts[0].payload.Alert != nil {
		t.Fatal("the debounced fallback start must not alert")
	}
}

// start 推出去到设备把活动 token 报回来之间的空窗里，状态变化谁也收不到。
func TestOnTokenRegisteredResendsCurrentState(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	run := Run{UserID: 7011, AgentID: 8011, SessionID: "session-token-catchup"}
	seedRunningState(t, run, model.SessionAgentStateRunning, "跑着")

	OnRunning(run)
	waitForDebounce()
	// 空窗期：这次等待转换发出去了，但后端还没有这张卡的 token。
	setState(t, run, model.SessionAgentStateWaitingQuestion)

	OnTokenRegistered(run.UserID, run.SessionID)

	updates := recorder.byEvent(protocol.LiveActivityEventUpdate)
	if len(updates) != 1 {
		t.Fatalf("expected exactly 1 catch-up update, got %d", len(updates))
	}
	payload := updates[0].payload
	if payload.ContentState.Phase != protocol.LiveActivityPhaseWaitingQuestion {
		t.Fatalf("catch-up phase = %s, want the current chat_states phase", payload.ContentState.Phase)
	}
	// token 会随系统轮转反复报上来，每次都响一下就成了骚扰。
	if payload.Alert != nil {
		t.Fatal("a catch-up update must not alert")
	}

	// 幂等：再报一次 token 只是再对齐一次，不会开新卡也不会收卡。
	OnTokenRegistered(run.UserID, run.SessionID)
	if got := len(recorder.byEvent(protocol.LiveActivityEventUpdate)); got != 2 {
		t.Fatalf("a repeated registration should re-align once more, got %d updates", got)
	}
	if got := len(recorder.byEvent(protocol.LiveActivityEventStart)); got != 1 {
		t.Fatalf("catch-up must never start a card, got %d starts", got)
	}
	if got := len(recorder.byEvent(protocol.LiveActivityEventEnd)); got != 0 {
		t.Fatalf("catch-up on a live card must not end it, got %d ends", got)
	}
}

// 终态赶在空窗里发生时，那次 end 一个 token 都没有可发，设备上的卡会一直挂着。
func TestOnTokenRegisteredEndsCardThatAlreadyFinished(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	run := Run{UserID: 7012, AgentID: 8012, SessionID: "session-token-late"}
	seedRunningState(t, run, model.SessionAgentStateRunning, "已经跑完了")

	OnRunning(run)
	waitForDebounce()
	setState(t, run, model.SessionAgentStateCompleted)
	OnTerminal(run, protocol.LiveActivityPhaseCompleted, "")

	before := time.Now()
	OnTokenRegistered(run.UserID, run.SessionID)

	ends := recorder.byEvent(protocol.LiveActivityEventEnd)
	if len(ends) != 2 {
		t.Fatalf("expected the lost end to be re-sent once the token exists, got %d ends", len(ends))
	}
	payload := ends[1].payload
	if payload.ContentState.Phase != protocol.LiveActivityPhaseCompleted {
		t.Fatalf("catch-up end phase = %s", payload.ContentState.Phase)
	}
	if payload.DismissalAtMs < before.Add(dismissalDelay).UnixMilli() {
		t.Fatalf("catch-up end dismissal_at_ms = %d, want at least now+5min", payload.DismissalAtMs)
	}
}

// 索引还在、状态却已经是终态（终态写库与收卡之间的窄缝）：按正常收卡走，
// 顺带把索引清掉，不留一张停在 running 的僵尸卡。
func TestOnTokenRegisteredEndsWhenStateTerminalButCardIndexed(t *testing.T) {
	_, recorder := setupNotifierTest(t)
	run := Run{UserID: 7013, AgentID: 8013, SessionID: "session-token-race"}
	seedRunningState(t, run, model.SessionAgentStateRunning, "刚刚失败")

	OnRunning(run)
	waitForDebounce()
	setState(t, run, model.SessionAgentStateFailed)

	OnTokenRegistered(run.UserID, run.SessionID)

	if got := len(recorder.byEvent(protocol.LiveActivityEventEnd)); got != 1 {
		t.Fatalf("expected 1 end, got %d", got)
	}
	if got := len(recorder.byEvent(protocol.LiveActivityEventUpdate)); got != 0 {
		t.Fatalf("a terminal state must not produce a running update, got %d", got)
	}
	if hasLiveCard(run.UserID, run.SessionID) {
		t.Fatal("the card index must be cleared")
	}
}

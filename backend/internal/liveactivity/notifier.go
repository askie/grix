// Package liveactivity turns chat_states transitions into ActivityKit
// (Live Activity) push tasks: one persistent lock-screen / Dynamic Island card
// per agent run, updated in place until the run ends.
//
// It only *publishes* tasks onto the existing offline-push subject
// (im.push.offline.<user_id>, cmd "live_activity"); the push service owns token
// resolution and the APNs call. Both the ws service (run lifecycle) and the api
// service (session rename) call in, so the package sits outside internal/ws.
package liveactivity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/notification"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const (
	// maxActivitiesPerUser 是同一个主人锁屏上并存的卡片上限。系统本身也有配额，
	// 排在后面的卡片会被静默丢弃，不如我们自己按"最旧的先退场"来控。
	maxActivitiesPerUser = 3
	// activityKeyTTL 与 push 侧存活动 token 的 TTL 一致：活动本来就只活几小时，
	// 一天之后还留着的索引一定是漏清的残留。
	activityKeyTTL = 24 * time.Hour
)

// startDebounce 是 run 进入 running 到开卡之间的观察窗口。绝大多数 run（一次
// 工具调用往返）活不过 10 秒，立刻开卡只会让锁屏闪一下卡片又消失。
// 变量而非常量：测试要把它调小，不然每个用例都得真等 10 秒。
var startDebounce = 10 * time.Second

// titleCoalesceWindow 合并同一会话的标题变更。改名时前端会连着写几次。
var titleCoalesceWindow = 5 * time.Second

var errJetStreamUnavailable = errors.New("live activity: jetstream not initialized")

// publishTask 是唯一的出口，测试里替换它来断言发了什么。
var publishTask = func(userID int64, payload protocol.LiveActivityPayload) error {
	if store.JS == nil {
		return errJetStreamUnavailable
	}
	data, err := json.Marshal(map[string]any{
		"user_id": userID,
		"cmd":     protocol.CmdLiveActivity,
		"payload": payload,
	})
	if err != nil {
		return err
	}
	_, err = store.JS.Publish(fmt.Sprintf("im.push.offline.%d", userID), data)
	return err
}

// Run 标识一次 agent 运行。与 chat_states 的主键 (session_id, owner_id) 对齐。
type Run struct {
	UserID    int64
	AgentID   int64
	SessionID string
}

func (r Run) valid() bool {
	return r.UserID > 0 && strings.TrimSpace(r.SessionID) != ""
}

// activityIndexKey 记录该主人当前有哪些会话的卡片在锁屏上：hash session_id →
// 开卡时刻（毫秒）。用它做三张上限的淘汰，以及判断某次转换是"开卡"还是"改卡"。
// 每活动的 token 由 api 侧写在 im:la:tokens:<user_id>:<session_id>，push 侧读。
func activityIndexKey(userID int64) string {
	return fmt.Sprintf("im:la:sessions:%d", userID)
}

var (
	pendingStartsMu sync.Mutex
	// pendingStarts 保证同一会话同时只有一个开卡计时器在跑：连续几次 run 注册
	// （排队的事件逐条起跑）不该排出一串定时器，最后连发几张卡。
	pendingStarts = make(map[string]struct{})
)

func pendingKey(run Run) string {
	return fmt.Sprintf("%d:%s", run.UserID, run.SessionID)
}

// OnRunning 在 run 写入 running 之后调用。
//
// 卡还没开：起一个 10 秒的观察窗口，到点复查 chat_states 仍在 running 才开卡。
// 卡已经在锁屏上：说明主人刚处理完一次等待、同一会话又跑起来了，立刻改卡。
func OnRunning(run Run) {
	if !run.valid() {
		return
	}
	if hasLiveCard(run.UserID, run.SessionID) {
		publishRunningUpdate(run)
		return
	}

	key := pendingKey(run)
	pendingStartsMu.Lock()
	if _, exists := pendingStarts[key]; exists {
		pendingStartsMu.Unlock()
		return
	}
	pendingStarts[key] = struct{}{}
	pendingStartsMu.Unlock()

	time.AfterFunc(startDebounce, func() {
		pendingStartsMu.Lock()
		delete(pendingStarts, key)
		pendingStartsMu.Unlock()
		startIfStillRunning(run)
	})
}

// OnResumed 在主人处理完审批 / 提问、run 继续跑时调用（离线操作回调那条路）。
// chat_states 此时不写 running——阻塞与否是 run 内部的事——所以只改卡不碰库。
func OnResumed(run Run) {
	if !run.valid() || !hasLiveCard(run.UserID, run.SessionID) {
		return
	}
	publishRunningUpdate(run)
}

// OnWaiting 在 chat_states 转入 waiting_approval / waiting_question 之后调用。
// 这是唯一带 alert 的一次更新：卡片从"在跑"变成"等你"，值得响一下。
//
// 卡还没开的时候直接开卡，而且不等观察窗口：观察窗口存在的理由是"绝大多数 run
// 活不过 10 秒"，而一个正等着主人的 run 恰恰不会秒完——agent 一上来就要审批是
// 常事，等满 10 秒只会让最需要卡片的那种 run 反而没有卡。
func OnWaiting(run Run, phase, summary string) {
	if !run.valid() {
		return
	}
	if phase != protocol.LiveActivityPhaseWaitingApproval &&
		phase != protocol.LiveActivityPhaseWaitingQuestion {
		return
	}
	alertTitle, alertBody, detail := notification.LiveActivityPhaseCopy(run.UserID, phase, summary)
	var alert *protocol.LiveActivityAlert
	if alertTitle != "" {
		alert = &protocol.LiveActivityAlert{Title: alertTitle, Body: alertBody}
	}
	if !hasLiveCard(run.UserID, run.SessionID) {
		startCard(run, phase, detail, alert, "waiting")
		return
	}
	payload := buildPayload(run, protocol.LiveActivityEventUpdate, phase, detail)
	payload.Alert = alert
	publish(run.UserID, payload, "waiting")
}

// OnTokenRegistered 在某台设备把这张卡的活动 token 报上来之后调用。
//
// start 推出去、到设备把活动 token 报回来，中间有几秒空窗，这期间的 update 谁也
// 收不到（后端还没有这张卡的 token）。最典型的就是刚开卡就转等待。所以 token 一
// 落地就按 chat_states 的当前状态补一帧，把这张卡拉齐到真实状态。
//
// 补的这一帧不带 alert：token 会随系统轮转反复报上来，每次都响一下就成了骚扰；
// 而"要主人处理"本来还有审批 / 提问的通知横幅在响，卡片的提示音是锦上添花。
func OnTokenRegistered(userID int64, sessionID string) {
	run := Run{UserID: userID, SessionID: strings.TrimSpace(sessionID)}
	if !run.valid() {
		return
	}
	state, err := store.GetSessionAgentState(run.SessionID, userID)
	if err != nil {
		logger.L.Warnf("live activity: token catch-up state user=%d session=%s err=%v", userID, run.SessionID, err)
		return
	}
	if state == nil {
		return
	}
	run.AgentID = state.AgentID
	phase := phaseForState(state.State)

	if !hasLiveCard(userID, run.SessionID) {
		// 卡在 token 报上来之前就该结束了（终态赶在空窗里发生，那次 end 没有
		// 任何 token 可发）。设备上这张卡还挂着，补一次 end 收掉它。
		_, _, detail := notification.LiveActivityPhaseCopy(userID, phase, state.StopReason)
		payload := buildPayload(run, protocol.LiveActivityEventEnd, phase, detail)
		payload.DismissalAtMs = time.Now().Add(dismissalDelay).UnixMilli()
		publish(userID, payload, "token-catch-up-end")
		return
	}
	if protocol.IsLiveActivityTerminalPhase(phase) {
		// 索引还在但状态已经是终态：走正常收卡，顺带把索引清掉。
		OnTerminal(run, phase, state.StopReason)
		return
	}
	_, _, detail := notification.LiveActivityPhaseCopy(userID, phase, "")
	publish(
		userID,
		buildPayload(run, protocol.LiveActivityEventUpdate, phase, detail),
		"token-catch-up",
	)
}

// OnTerminal 在 run 结算为终态之后调用：卡片显示结果，5 分钟后自己从锁屏退场。
func OnTerminal(run Run, phase, reason string) {
	if !run.valid() || !protocol.IsLiveActivityTerminalPhase(phase) {
		return
	}
	if !clearLiveCard(run.UserID, run.SessionID) {
		// 卡本来就没开（run 太短没熬过观察窗口，或主人关了推送），无卡可结束。
		return
	}
	_, _, detail := notification.LiveActivityPhaseCopy(run.UserID, phase, reason)
	payload := buildPayload(run, protocol.LiveActivityEventEnd, phase, detail)
	payload.DismissalAtMs = time.Now().Add(dismissalDelay).UnixMilli()
	publish(run.UserID, payload, "terminal")
}

// dismissalDelay 是终态卡片留在锁屏上的时间。任务刚结束时主人多半不在看手机，
// 立刻消失等于白推一张卡；留太久又变成需要手动划掉的垃圾。
const dismissalDelay = 5 * time.Minute

var (
	titleTimersMu sync.Mutex
	titleTimers   = make(map[string]*time.Timer)
)

// OnTitleChanged 在会话标题变化后调用（改名）。同一会话 5 秒内只发一次。
// 会话可能同时是多个主人的 chat_states 行，逐个主人各自判断有没有活卡。
func OnTitleChanged(sessionID string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	titleTimersMu.Lock()
	if timer, ok := titleTimers[sid]; ok {
		timer.Stop()
	}
	titleTimers[sid] = time.AfterFunc(titleCoalesceWindow, func() {
		titleTimersMu.Lock()
		delete(titleTimers, sid)
		titleTimersMu.Unlock()
		publishTitleUpdate(sid)
	})
	titleTimersMu.Unlock()
}

func publishTitleUpdate(sessionID string) {
	if store.DB == nil {
		return
	}
	var rows []model.SessionAgentState
	if err := store.DB.
		Select("session_id", "owner_id", "agent_id", "state").
		Where("session_id = ?", sessionID).
		Where("state IN ?", []string{
			model.SessionAgentStateRunning,
			model.SessionAgentStateWaitingApproval,
			model.SessionAgentStateWaitingQuestion,
		}).
		Find(&rows).Error; err != nil {
		logger.L.Warnf("live activity: load states for title change session=%s err=%v", sessionID, err)
		return
	}
	for _, row := range rows {
		run := Run{UserID: row.OwnerID, AgentID: row.AgentID, SessionID: row.SessionID}
		if !run.valid() || !hasLiveCard(run.UserID, run.SessionID) {
			continue
		}
		publish(run.UserID, buildPayload(run, protocol.LiveActivityEventUpdate, row.State, ""), "title")
	}
}

func publishRunningUpdate(run Run) {
	publish(
		run.UserID,
		buildPayload(run, protocol.LiveActivityEventUpdate, protocol.LiveActivityPhaseRunning, ""),
		"running",
	)
}

// startIfStillRunning 是观察窗口到点后的复查。10 秒内跑完 / 失败 / 被停的 run
// 在这里被挡下，锁屏上一张卡都不会出现。
//
// 复查认的是"这个 run 还活着"，不只是 running：窗口里转成 waiting_* 的 run 照样
// 要开卡，阶段按实际状态填。正常情况下 OnWaiting 已经抢先开过卡了，这里是兜底。
func startIfStillRunning(run Run) {
	state, err := store.GetSessionAgentState(run.SessionID, run.UserID)
	if err != nil {
		logger.L.Warnf("live activity: recheck state user=%d session=%s err=%v", run.UserID, run.SessionID, err)
		return
	}
	if state == nil {
		return
	}
	phase := phaseForState(state.State)
	if protocol.IsLiveActivityTerminalPhase(phase) {
		return
	}
	if hasLiveCard(run.UserID, run.SessionID) {
		return
	}
	// alert 归 OnWaiting 管：那条路径知道 agent 到底在问什么，这里只知道状态。
	_, _, detail := notification.LiveActivityPhaseCopy(run.UserID, phase, "")
	startCard(run, phase, detail, nil, "debounced")
}

// startCard 开一张新卡：过一次推送偏好、腾出位置、发 start、记进索引。
// 调用方负责先确认这个会话还没有卡。
func startCard(run Run, phase, detail string, alert *protocol.LiveActivityAlert, reason string) {
	if !notification.LiveActivityPushEnabled(run.UserID) {
		return
	}

	evictOldestCardIfFull(run.UserID)

	payload := buildPayload(run, protocol.LiveActivityEventStart, phase, detail)
	payload.Alert = alert
	if err := publishTask(run.UserID, payload); err != nil {
		logger.L.Warnf("live activity: publish %s start user=%d session=%s err=%v",
			reason, run.UserID, run.SessionID, err)
		return
	}
	markLiveCard(run.UserID, run.SessionID)
}

// evictOldestCardIfFull 在开第 4 张卡之前先结束最旧的一张。
func evictOldestCardIfFull(userID int64) {
	if store.RDB == nil {
		return
	}
	ctx := context.Background()
	entries, err := store.RDB.HGetAll(ctx, activityIndexKey(userID)).Result()
	if err != nil {
		logger.L.Warnf("live activity: load card index user=%d err=%v", userID, err)
		return
	}
	for len(entries) >= maxActivitiesPerUser {
		oldestSession, oldestAt := "", int64(0)
		for sessionID, startedAt := range entries {
			ms := parseMillis(startedAt)
			if oldestSession == "" || ms < oldestAt {
				oldestSession, oldestAt = sessionID, ms
			}
		}
		if oldestSession == "" {
			return
		}
		endEvictedCard(userID, oldestSession)
		delete(entries, oldestSession)
	}
}

// endEvictedCard 结束一张被挤掉的卡。run 多半还在跑，所以按它当前的状态收尾，
// 并且立即消失（dismissal 不延后）——给新卡腾位置本来就不该再占着锁屏。
func endEvictedCard(userID int64, sessionID string) {
	run := Run{UserID: userID, SessionID: sessionID}
	phase := protocol.LiveActivityPhaseStopped
	if state, err := store.GetSessionAgentState(sessionID, userID); err == nil && state != nil {
		run.AgentID = state.AgentID
		phase = phaseForState(state.State)
	}
	clearLiveCard(userID, sessionID)
	payload := buildPayload(run, protocol.LiveActivityEventEnd, phase, "")
	payload.DismissalAtMs = time.Now().UnixMilli()
	publish(userID, payload, "evicted")
}

// phaseForState 把 chat_states 的状态名映射成卡片阶段。idle 在 chat_states 里
// 表示"主人停了"或"清扫收尾"，卡片上叫 stopped。
func phaseForState(state string) string {
	switch state {
	case model.SessionAgentStateRunning:
		return protocol.LiveActivityPhaseRunning
	case model.SessionAgentStateWaitingApproval:
		return protocol.LiveActivityPhaseWaitingApproval
	case model.SessionAgentStateWaitingQuestion:
		return protocol.LiveActivityPhaseWaitingQuestion
	case model.SessionAgentStateCompleted:
		return protocol.LiveActivityPhaseCompleted
	case model.SessionAgentStateFailed:
		return protocol.LiveActivityPhaseFailed
	default:
		return protocol.LiveActivityPhaseStopped
	}
}

// PhaseForState 供调用方把 chat_states 状态翻译成卡片阶段。
func PhaseForState(state string) string { return phaseForState(state) }

func buildPayload(run Run, event, phase, detail string) protocol.LiveActivityPayload {
	title, agentName := runDisplayInfo(run)
	return protocol.LiveActivityPayload{
		Event:     event,
		SessionID: run.SessionID,
		Attributes: protocol.LiveActivityAttributes{
			SessionID: run.SessionID,
			AgentID:   run.AgentID,
			AgentName: agentName,
		},
		ContentState: protocol.LiveActivityContentState{
			Phase:       phase,
			Title:       title,
			Detail:      detail,
			UpdatedAtMs: time.Now().UnixMilli(),
		},
	}
}

// runDisplayInfo 取卡片上那两行字：任务标题（chat_states 里已经按主人视角
// 反规范化好的 task_title）和 agent 名字。
func runDisplayInfo(run Run) (title, agentName string) {
	if state, err := store.GetSessionAgentState(run.SessionID, run.UserID); err == nil && state != nil {
		title = strings.TrimSpace(state.TaskTitle)
	}
	if run.AgentID > 0 && store.DB != nil {
		var agent model.Agent
		if err := store.DB.Select("agent_name").Where("id = ?", run.AgentID).Take(&agent).Error; err == nil {
			agentName = strings.TrimSpace(agent.AgentName)
		}
	}
	return title, agentName
}

func publish(userID int64, payload protocol.LiveActivityPayload, reason string) {
	if err := publishTask(userID, payload); err != nil {
		logger.L.Warnf(
			"live activity: publish %s user=%d session=%s event=%s err=%v",
			reason, userID, payload.SessionID, payload.Event, err,
		)
	}
}

func hasLiveCard(userID int64, sessionID string) bool {
	if store.RDB == nil {
		return false
	}
	exists, err := store.RDB.HExists(context.Background(), activityIndexKey(userID), sessionID).Result()
	if err != nil {
		logger.L.Warnf("live activity: card lookup user=%d session=%s err=%v", userID, sessionID, err)
		return false
	}
	return exists
}

func markLiveCard(userID int64, sessionID string) {
	if store.RDB == nil {
		return
	}
	ctx := context.Background()
	key := activityIndexKey(userID)
	if err := store.RDB.HSet(ctx, key, sessionID, time.Now().UnixMilli()).Err(); err != nil {
		logger.L.Warnf("live activity: mark card user=%d session=%s err=%v", userID, sessionID, err)
		return
	}
	store.RDB.Expire(ctx, key, activityKeyTTL)
}

// clearLiveCard 摘掉索引里的一张卡，返回它原本是否存在。返回值决定要不要真发
// end：没开过的卡不必结束，重复的终态（并发结算）也只会发一次。
func clearLiveCard(userID int64, sessionID string) bool {
	if store.RDB == nil {
		return false
	}
	removed, err := store.RDB.HDel(context.Background(), activityIndexKey(userID), sessionID).Result()
	if err != nil {
		logger.L.Warnf("live activity: clear card user=%d session=%s err=%v", userID, sessionID, err)
		return false
	}
	return removed > 0
}

func parseMillis(raw string) int64 {
	var ms int64
	if _, err := fmt.Sscanf(raw, "%d", &ms); err != nil {
		return 0
	}
	return ms
}

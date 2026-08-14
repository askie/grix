package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/reach"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
	"gorm.io/gorm/clause"
)

const (
	reachConsumerDurable   = "reach-sender-v1"
	reachConsumerQueue     = "reach-sender-v1"
	reachConsumerAckWait   = 10 * time.Minute
	reachAudienceBatchSize = 500
)

// StartReachConsumer subscribes the api process to reach events (app releases)
// and fans them out as in-app system messages + optional email. Follows the
// gateway_topup_consumer pattern: NATS durable queue, best-effort startup.
func StartReachConsumer(ctx context.Context) {
	if store.JS == nil {
		logger.L.Warn("reach consumer: jetstream unavailable, not starting")
		return
	}
	sub, err := store.JS.QueueSubscribe(
		reach.NATSSubjectReachEvent,
		reachConsumerQueue,
		func(msg *nats.Msg) { handleReachEvent(ctx, msg) },
		nats.ManualAck(),
		nats.Durable(reachConsumerDurable),
		nats.DeliverNew(),
		nats.AckWait(reachConsumerAckWait),
		nats.MaxDeliver(3),
	)
	if err != nil {
		logger.L.Warnf("reach consumer: subscribe failed: %v", err)
		return
	}
	logger.L.Infof("reach consumer subscribed subject=%s group=%s", reach.NATSSubjectReachEvent, reachConsumerQueue)
	go func() {
		<-ctx.Done()
		_ = sub.Drain()
	}()
}

func handleReachEvent(ctx context.Context, msg *nats.Msg) {
	var evt reach.AppReleaseEvent
	if err := json.Unmarshal(msg.Data, &evt); err != nil {
		logger.L.Errorf("reach consumer: bad event payload: %v", err)
		_ = msg.Ack()
		return
	}
	if evt.EventKey == "" || evt.ReleaseID == 0 {
		_ = msg.Ack()
		return
	}
	fillReleaseChannel(&evt)

	task, created, err := createAppReleaseReachTask(evt)
	if err != nil {
		logger.L.Warnf("reach consumer: create task err=%v", err)
		_ = msg.Nak()
		return
	}
	if !created {
		logger.L.Infof("reach consumer: duplicate release event skipped version=%s channel=%s release=%d",
			evt.Version, evt.Channel, evt.ReleaseID)
		_ = msg.Ack()
		return
	}

	// 发布不再自动群发：只落一条待发送草稿（预填默认中英文案），由管理
	// 后台编辑文案后手动触发发送（SendReachAnnouncement）。
	logger.L.Infof("reach consumer: announcement draft created task=%d version=%s channel=%s",
		task.ID, evt.Version, evt.Channel)
	_ = msg.Ack()
}

// fillReleaseChannel resolves a missing channel from the release row: events
// published by pre-channel api pods carry no channel, and without this their
// dedup key would differ from the one new events produce — a rolling upgrade
// window could then split one version into two keys and double-announce.
func fillReleaseChannel(evt *reach.AppReleaseEvent) {
	if strings.TrimSpace(evt.Channel) != "" || evt.ReleaseID == 0 {
		return
	}
	var release model.AppRelease
	if err := store.DB.Select("channel").Where("id = ?", evt.ReleaseID).First(&release).Error; err == nil {
		evt.Channel = release.Channel
	}
}

// createAppReleaseReachTask inserts the announcement draft for one app-release
// event, deduplicated at the task level: all releases of the same version (e.g.
// the iOS and macOS records published back-to-back by one release run) share one
// dedup key, so only the first event creates a task and every later one is
// skipped. The draft carries the default bilingual copy so the admin can edit
// it before sending. Returns created=false when a task for this version
// already exists.
func createAppReleaseReachTask(evt reach.AppReleaseEvent) (model.ReachTask, bool, error) {
	channelsJSON, _ := json.Marshal([]string{model.ReachChannelInApp, model.ReachChannelPush})
	contentJSON, _ := json.Marshal(DefaultAppReleaseAnnouncementContent(evt.Version, evt.Changelog))
	dedupKey := reachTaskDedupKey(evt)
	task := model.ReachTask{
		ID:        snowflake.GenID(),
		Kind:      model.ReachKindSystemEvent,
		EventKey:  evt.EventKey,
		DedupKey:  &dedupKey,
		Channels:  channelsJSON,
		Content:   contentJSON,
		Status:    model.ReachStatusDraft,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	res := store.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dedup_key"}},
		DoNothing: true,
	}).Create(&task)
	if res.Error != nil {
		return model.ReachTask{}, false, res.Error
	}
	return task, res.RowsAffected > 0, nil
}

// reachTaskDedupKey collapses per-platform release records into one announcement
// per channel+version — the channel matters because a beta 3.1.4 must not
// swallow the later stable 3.1.4 announcement. The release ID is only a
// fallback for malformed events without a version, so different fallback
// events never collapse into each other.
func reachTaskDedupKey(evt reach.AppReleaseEvent) string {
	version := strings.TrimSpace(evt.Version)
	if version == "" {
		return fmt.Sprintf("%s:release-%d", evt.EventKey, evt.ReleaseID)
	}
	return evt.EventKey + ":" + strings.TrimSpace(evt.Channel) + ":" + version
}

// reachEventFromTask rebuilds the announcement payload for a paused task being
// resumed, from the dedup key (channel+version) plus the newest published
// release of that version for the changelog. Tasks without a dedup key fall
// back to an empty payload, matching the old behavior.
func reachEventFromTask(task *model.ReachTask) reach.AppReleaseEvent {
	evt := reach.AppReleaseEvent{EventKey: task.EventKey}
	if task.DedupKey == nil {
		return evt
	}
	parts := strings.SplitN(*task.DedupKey, ":", 3)
	if len(parts) != 3 {
		return evt
	}
	evt.Channel = parts[1]
	evt.Version = parts[2]
	var release model.AppRelease
	if err := store.DB.
		Where("version = ? AND channel = ? AND published_at IS NOT NULL", evt.Version, evt.Channel).
		Order("published_at DESC").First(&release).Error; err == nil {
		evt.ReleaseID = release.ID
		evt.Changelog = release.Changelog
	}
	return evt
}

func executeReachTask(ctx context.Context, taskID, customerUserID int64, content ReachAnnouncementContent) {
	sent, skipped, finalStatus := fanOutAppRelease(ctx, taskID, customerUserID, content)
	logger.L.Infof("reach consumer: fan-out task=%d status=%s sent=%d skipped=%d",
		taskID, finalStatus, sent, skipped)

	statsJSON, _ := json.Marshal(map[string]int{"sent": sent, "skipped": skipped})
	// JSONB 列需要 string，[]byte 会被 pgx 简单协议当 bytea 处理（SQLSTATE 22P02）
	if err := store.DB.Model(&model.ReachTask{}).Where("id = ?", taskID).
		Updates(map[string]any{"stats": string(statsJSON), "updated_at": time.Now().UTC(), "status": finalStatus}).Error; err != nil {
		logger.L.Errorf("reach consumer: update final status failed task=%d status=%s err=%v", taskID, finalStatus, err)
	}
}

func fanOutAppRelease(ctx context.Context, taskID, customerUserID int64, content ReachAnnouncementContent) (sent, skipped int, finalStatus string) {
	var lastID int64
	for {
		if status, err := loadReachTaskStatus(taskID); err == nil {
			if status == model.ReachStatusPaused || status == model.ReachStatusCancelled {
				return sent, skipped, status
			}
		}

		var users []model.User
		if err := store.DB.
			Select("id, email").
			Where("status = ? AND id > ?", model.UserStatusActive, lastID).
			Order("id ASC").
			Limit(reachAudienceBatchSize).
			Find(&users).Error; err != nil {
			logger.L.Warnf("reach consumer: scan users after=%d err=%v", lastID, err)
			return sent, skipped, model.ReachStatusSending
		}
		if len(users) == 0 {
			break
		}
		for _, u := range users {
			lastID = u.ID
			if u.ID == customerUserID {
				skipped++
				continue
			}
			if deliverReachToUser(ctx, taskID, customerUserID, u, content) {
				sent++
			} else {
				skipped++
			}
		}
		// Heartbeat: a live fan-out must keep updated_at fresh, or a run longer
		// than the NATS AckWait would look stale and get a concurrent resume.
		store.DB.Model(&model.ReachTask{}).Where("id = ?", taskID).
			Update("updated_at", time.Now().UTC())
		if len(users) < reachAudienceBatchSize {
			break
		}
	}
	return sent, skipped, model.ReachStatusSent
}

func loadReachTaskStatus(taskID int64) (string, error) {
	var status string
	err := store.DB.Model(&model.ReachTask{}).
		Where("id = ?", taskID).
		Pluck("status", &status).Error
	return status, err
}

// PauseReachTask pauses a sending task. The fan-out loop will stop at the next
// batch boundary. Resume with ResumeReachTask.
func PauseReachTask(taskID int64) error {
	res := store.DB.Model(&model.ReachTask{}).
		Where("id = ? AND status = ?", taskID, model.ReachStatusSending).
		Updates(map[string]any{"status": model.ReachStatusPaused, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("task not in sending state")
	}
	logger.L.Infof("reach: paused task=%d", taskID)
	return nil
}

// CancelReachTask cancels a draft, scheduled, sending or paused task.
// Already-sent messages are not recalled; the task simply stops processing
// remaining users. Cancelling a draft or scheduled task prevents it from ever
// firing.
func CancelReachTask(taskID int64) error {
	res := store.DB.Model(&model.ReachTask{}).
		Where("id = ? AND status IN ?", taskID, []string{model.ReachStatusDraft, model.ReachStatusScheduled, model.ReachStatusSending, model.ReachStatusPaused}).
		Updates(map[string]any{"status": model.ReachStatusCancelled, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("task not in draft, scheduled, sending or paused state")
	}
	logger.L.Infof("reach: cancelled task=%d", taskID)
	return nil
}

// ResumeReachTask resumes an announcement fan-out: a paused task, or a task
// stuck in sending whose fan-out died (crash/redeploy) — detected by an
// updated_at heartbeat older than the ack window. Already-delivered users are
// skipped via send_log idempotency, so this is a resume, not a re-send.
func ResumeReachTask(ctx context.Context, taskID int64) error {
	var task model.ReachTask
	if err := store.DB.Where("id = ?", taskID).First(&task).Error; err != nil {
		return fmt.Errorf("task not found: %w", err)
	}
	if task.Kind != model.ReachKindSystemEvent {
		return errors.New("only system-event announcement tasks can be resumed")
	}
	stale := task.Status == model.ReachStatusSending && time.Since(task.UpdatedAt) >= reachConsumerAckWait
	if task.Status != model.ReachStatusPaused && !stale {
		return errors.New("task not paused (or sending but stalled)")
	}

	settings, err := systemsetting.GetAuthSettings()
	if err != nil {
		return err
	}
	customerUserID := settings.AutoAddCustomerUserID
	if customerUserID <= 0 {
		return errors.New("no customer account configured")
	}

	// The status+heartbeat predicate makes the flip race-safe: two concurrent
	// resumes (or a resume racing the still-alive fan-out's heartbeat) act once.
	res := store.DB.Model(&model.ReachTask{}).
		Where("id = ? AND status = ? AND updated_at = ?", taskID, task.Status, task.UpdatedAt).
		Updates(map[string]any{"status": model.ReachStatusSending, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("task state changed, not resumed")
	}

	content := announcementContentForTask(&task)

	go executeReachTask(ctx, taskID, customerUserID, content)
	logger.L.Infof("reach: resumed task=%d (was %s)", taskID, task.Status)
	return nil
}

func deliverReachToUser(ctx context.Context, taskID, customerUserID int64, user model.User, content ReachAnnouncementContent) bool {
	logRow := model.ReachSendLog{
		ID:      snowflake.GenID(),
		TaskID:  taskID,
		UserID:  user.ID,
		Channel: model.ReachChannelInApp,
		Status:  model.ReachSendStatusPending,
	}
	res := store.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&logRow)
	if res.Error != nil || res.RowsAffected == 0 {
		return false
	}

	msg, err := WriteAppReleaseSystemMessage(customerUserID, user.ID, content)
	if err != nil {
		logger.L.Warnf("reach consumer: write message user=%d task=%d err=%v", user.ID, taskID, err)
		store.DB.Model(&model.ReachSendLog{}).Where("id = ?", logRow.ID).
			Updates(map[string]any{"status": model.ReachSendStatusFailed, "error": err.Error()})
		return false
	}

	payload := protocol.PushMsgPayload{
		InboxSeq:    msg.InboxSeq,
		MsgID:       msg.MsgID,
		SessionID:   msg.SessionID,
		SessionType: 1,
		SenderID:    customerUserID,
		SenderType:  3,
		MsgType:     3,
		Content:     msg.Content,
		CreatedAt:   msg.CreatedAt.UnixMilli(),
	}
	if hasOnlineRealtimeRoute(user.ID) {
		pushRealtimeEvent(user.ID, protocol.CmdPushMsg, payload)
	} else {
		pushOfflineEvent(user.ID, protocol.CmdPushMsg, payload)
	}

	store.DB.Model(&model.ReachSendLog{}).Where("id = ?", logRow.ID).
		Update("status", model.ReachSendStatusSent)

	deliverReachEmail(taskID, user, content)
	return true
}

type ListReachTasksReq struct {
	Status   string
	Kind     string
	Page     int
	PageSize int
}

type ListReachTasksResult struct {
	Total int64             `json:"total"`
	Tasks []model.ReachTask `json:"tasks"`
}

func ListReachTasks(req ListReachTasksReq) (*ListReachTasksResult, error) {
	db := store.DB.Model(&model.ReachTask{})
	if req.Status != "" {
		db = db.Where("status = ?", req.Status)
	}
	if req.Kind != "" {
		db = db.Where("kind = ?", req.Kind)
	}
	var total int64
	db.Count(&total)

	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}
	var tasks []model.ReachTask
	db.Order("created_at DESC").
		Offset((req.Page - 1) * req.PageSize).
		Limit(req.PageSize).
		Find(&tasks)
	return &ListReachTasksResult{Total: total, Tasks: tasks}, nil
}

type ReachTaskDetail struct {
	model.ReachTask
	SendLogs []model.ReachSendLog `json:"send_logs,omitempty"`
}

func GetReachTask(taskID int64) (*ReachTaskDetail, error) {
	var task model.ReachTask
	if err := store.DB.Where("id = ?", taskID).First(&task).Error; err != nil {
		return nil, err
	}
	var logs []model.ReachSendLog
	store.DB.Where("task_id = ?", taskID).Order("created_at DESC").Limit(200).Find(&logs)
	return &ReachTaskDetail{ReachTask: task, SendLogs: logs}, nil
}

type ReachTaskStats struct {
	TaskID    int64            `json:"task_id,string"`
	Status    string           `json:"status"`
	Stats     datatypes.JSON   `json:"stats"`
	Channels  map[string]int64 `json:"channel_breakdown"`
	Regions   map[string]int64 `json:"region_breakdown"`
	Total     int64            `json:"total_logs"`
	Opened    int64            `json:"opened"`
	Clicked   int64            `json:"clicked"`
	OpenRate  float64          `json:"open_rate"`
	ClickRate float64          `json:"click_rate"`
}

func GetReachTaskStats(taskID int64) (*ReachTaskStats, error) {
	var task model.ReachTask
	if err := store.DB.Where("id = ?", taskID).First(&task).Error; err != nil {
		return nil, err
	}

	type channelCount struct {
		Channel string
		Count   int64
	}
	var counts []channelCount
	store.DB.Model(&model.ReachSendLog{}).
		Select("channel, count(*) as count").
		Where("task_id = ? AND status = ?", taskID, model.ReachSendStatusSent).
		Group("channel").
		Scan(&counts)

	breakdown := make(map[string]int64)
	var total int64
	for _, c := range counts {
		breakdown[c.Channel] = c.Count
		total += c.Count
	}

	type regionCount struct {
		Region string
		Count  int64
	}
	var regionCounts []regionCount
	store.DB.Model(&model.ReachSendLog{}).
		Select("region, count(*) as count").
		Where("task_id = ? AND status = ?", taskID, model.ReachSendStatusSent).
		Group("region").
		Scan(&regionCounts)
	regions := make(map[string]int64)
	for _, r := range regionCounts {
		key := r.Region
		if key == "" {
			key = "unknown"
		}
		regions[key] = r.Count
	}

	var opened, clicked int64
	store.DB.Model(&model.ReachSendLog{}).
		Where("task_id = ? AND opened_at IS NOT NULL", taskID).
		Count(&opened)
	store.DB.Model(&model.ReachSendLog{}).
		Where("task_id = ? AND clicked_at IS NOT NULL", taskID).
		Count(&clicked)

	var openRate, clickRate float64
	if total > 0 {
		openRate = float64(opened) / float64(total)
		clickRate = float64(clicked) / float64(total)
	}

	return &ReachTaskStats{
		TaskID:    taskID,
		Status:    task.Status,
		Stats:     task.Stats,
		Channels:  breakdown,
		Regions:   regions,
		Total:     total,
		Opened:    opened,
		Clicked:   clicked,
		OpenRate:  openRate,
		ClickRate: clickRate,
	}, nil
}

type ReachSubscriptionOverview struct {
	TotalSubscriptions int64 `json:"total_subscriptions"`
	Subscribed         int64 `json:"subscribed"`
	Unsubscribed       int64 `json:"unsubscribed"`
}

func GetReachSubscriptionOverview() (*ReachSubscriptionOverview, error) {
	var total, subscribed int64
	store.DB.Model(&model.ReachSubscription{}).Count(&total)
	store.DB.Model(&model.ReachSubscription{}).Where("subscribed = ?", true).Count(&subscribed)
	return &ReachSubscriptionOverview{
		TotalSubscriptions: total,
		Subscribed:         subscribed,
		Unsubscribed:       total - subscribed,
	}, nil
}

func deliverReachEmail(taskID int64, user model.User, content ReachAnnouncementContent) {
	if user.Email == "" {
		return
	}
	logRow := model.ReachSendLog{
		ID:      snowflake.GenID(),
		TaskID:  taskID,
		UserID:  user.ID,
		Channel: model.ReachChannelEmail,
		Status:  model.ReachSendStatusPending,
	}
	res := store.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&logRow)
	if res.Error != nil || res.RowsAffected == 0 {
		return
	}

	language, _ := loadUserPreferredLanguageWithDB(store.DB, user.ID)
	subject, body := ReachAnnouncementEmailContent(content, language)
	trackedBody := InjectEmailTracking(body, logRow.ID)
	if err := SendReachEmail(user.Email, subject, trackedBody); err != nil {
		logger.L.Warnf("reach consumer: email user=%d err=%v", user.ID, err)
		store.DB.Model(&model.ReachSendLog{}).Where("id = ?", logRow.ID).
			Updates(map[string]any{"status": model.ReachSendStatusFailed, "error": err.Error()})
		return
	}
	store.DB.Model(&model.ReachSendLog{}).Where("id = ?", logRow.ID).
		Update("status", model.ReachSendStatusSent)
}

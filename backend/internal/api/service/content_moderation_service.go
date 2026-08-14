package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/keywordmatcher"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/redis/go-redis/v9"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	contentModerationQueueKey                   = "im:content_moderation:queue"
	contentModerationWorkerCount                = 4
	contentModerationWorkerPopTimeout           = 2 * time.Second
	contentModerationWorkerRetryBackoff         = time.Second
	contentModerationRecoveryInterval           = 5 * time.Second
	contentModerationRecoveryBatchSize          = 128
	contentModerationProcessingLease            = 30 * time.Second
	contentModerationRetryBaseDelay             = 2 * time.Second
	contentModerationRetryMaxDelay              = 5 * time.Minute
	contentModerationRecallStatusPending        = "pending"
	contentModerationRecallStatusRevoked        = "revoked"
	contentModerationRecallStatusAlreadyRevoked = "already_revoked"
	contentModerationRecallStatusMessageMissing = "message_missing"
	contentModerationRecallStatusUnsupported    = "unsupported_sender"
	contentModerationRecallStatusFailed         = "revoke_failed"
)

var (
	contentModerationDispatchRunner = dispatchContentModerationTaskAsync
	contentModerationDeleteMessage  = DeleteMessage
	// contentModerationProcessTask 是 processContentModerationTask 的可替换入口。
	// worker 循环与入队失败兜底都走这个变量而非直接调用函数，让测试能在这里
	// 打桩、真正断言 worker 传下去的 ctx 是不可取消的——直接调函数名的话，
	// 测试只能另起一份逻辑复刻，改坏了生产代码测试也测不出来。
	contentModerationProcessTask = processContentModerationTask
)

var contentModerationRuntimeCache struct {
	mu        sync.RWMutex
	signature string
	runtime   contentModerationRuntime
}

type ContentModerationTask struct {
	SessionID string `json:"session_id"`
	MsgID     int64  `json:"msg_id,string"`
}

type contentModerationRuntime struct {
	settings systemsetting.ContentModerationSettings
	matcher  *keywordmatcher.Matcher
}

func ScheduleContentModeration(task ContentModerationTask) {
	if strings.TrimSpace(task.SessionID) == "" || task.MsgID <= 0 {
		return
	}
	if contentModerationDispatchRunner == nil {
		return
	}
	contentModerationDispatchRunner(task)
}

func dispatchContentModerationTaskAsync(task ContentModerationTask) {
	// 首次派发时惰性拉起 worker；生命周期由 Start/Stop 管（见 content_moderation_lifecycle.go）。
	StartContentModerationWorkers(context.Background())

	// 入队与兜底处理都挂到受管协程上：它们会写 DB/Redis，裸 goroutine 会活过服务关停。
	//
	// ⛔ 兜底分支不能再嵌套一层 spawnContentModerationTask：Stop() 先把 running 置
	// false 再开始等排空，嵌套的内层 spawn 一进来就看到 running==false，直接静默
	// 丢弃——不进 Redis、不进库，恢复扫描也找不到它，这条待审核消息就永远漏审了。
	// 而入队失败最容易发生的时刻恰恰是关停（ctx 一取消 RPush 必失败）。外层已经
	// 登记进计数、Stop 会等它跑完，兜底直接内联执行即可，不需要再 spawn 一次。
	spawnContentModerationTask(func(ctx context.Context) {
		if err := enqueueContentModerationTask(ctx, task); err != nil {
			logContentModerationWarnf(
				"enqueue content moderation task failed session=%s msg_id=%d err=%v",
				task.SessionID,
				task.MsgID,
				err,
			)
			contentModerationProcessTask(context.WithoutCancel(ctx), task)
		}
	})
}

func runContentModerationWorker(ctx context.Context) {
	for {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			return
		}
		if store.RDB == nil {
			if !waitContentModerationBackoff(ctx, contentModerationWorkerRetryBackoff) {
				return
			}
			continue
		}

		values, err := store.RDB.BLPop(ctx, contentModerationWorkerPopTimeout, contentModerationQueueKey).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			if errors.Is(err, context.Canceled) {
				// 关停：worker 的 ctx 被取消导致 BLPop 提前返回，这是正常收尾
				// 路径，不是 Redis 故障，别当 warn 打——顶层 for 循环下一轮的
				// ctx.Err() 检查会让它正常退出。真报了会淹没真实的 Redis 告警。
				continue
			}
			logContentModerationWarnf("pop content moderation task failed: %v", err)
			if !waitContentModerationBackoff(ctx, contentModerationWorkerRetryBackoff) {
				return
			}
			continue
		}
		if len(values) != 2 {
			continue
		}

		var task ContentModerationTask
		if err := json.Unmarshal([]byte(values[1]), &task); err != nil {
			logContentModerationWarnf("decode content moderation task failed err=%v raw=%s", err, values[1])
			continue
		}

		// ⛔ 处理任务必须用不可取消的 ctx，不能用 worker 的 ctx。
		// 任务已经被 BLPop 从队列里弹出来了——此时若因关停取消 ctx，第一个 DB 操作
		// 就会失败返回：任务从 Redis 没了，库里也没留下记录，恢复扫描找不着它，
		// 这条违规消息就永远不会被撤回。合规链路不许这么丢。
		// 关停要等它跑完（Stop 的计数覆盖了这段），而不是把它掐断。
		contentModerationProcessTask(context.WithoutCancel(ctx), task)
	}
}

func enqueueContentModerationTask(ctx context.Context, task ContentModerationTask) error {
	if strings.TrimSpace(task.SessionID) == "" || task.MsgID <= 0 {
		return errors.New("invalid content moderation task")
	}
	if store.RDB == nil {
		return errors.New("redis unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return store.RDB.RPush(ctx, contentModerationQueueKey, raw).Err()
}

func processContentModerationTask(ctx context.Context, task ContentModerationTask) {
	sid := strings.TrimSpace(task.SessionID)
	if sid == "" || task.MsgID <= 0 || store.DB == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	runtime, err := loadContentModerationRuntime()
	if err != nil {
		logContentModerationWarnf("load content moderation runtime failed: %v", err)
		return
	}
	if !runtime.settings.Enabled || runtime.matcher == nil {
		return
	}

	var msg model.Message
	if err := store.DB.WithContext(ctx).
		Select(
			"msg_id",
			"session_id",
			"sender_id",
			"sender_type",
			"msg_type",
			"content",
			"is_deleted",
			"is_revoked",
		).
		Where("msg_id = ? AND session_id = ?", task.MsgID, sid).
		Take(&msg).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logContentModerationWarnf(
				"load content moderation message failed session=%s msg_id=%d err=%v",
				sid,
				task.MsgID,
				err,
			)
		}
		return
	}

	if msg.MsgType != 1 || strings.TrimSpace(msg.Content) == "" {
		return
	}

	matchedKeywords := runtime.matcher.Match(msg.Content)
	if len(matchedKeywords) == 0 {
		return
	}

	event, claimed, err := claimContentModerationEvent(ctx, msg, matchedKeywords)
	if err != nil {
		logContentModerationWarnf(
			"claim content moderation event failed session=%s msg_id=%d err=%v",
			msg.SessionID,
			msg.MsgID,
			err,
		)
		return
	}
	if !claimed {
		return
	}

	recallStatus := contentModerationRecallStatusAlreadyRevoked
	switch {
	case msg.IsDeleted || msg.IsRevoked:
		recallStatus = contentModerationRecallStatusAlreadyRevoked
	default:
		recallStatus = recallContentModeratedMessage(ctx, msg)
	}

	hitCount := 1
	muteApplied := false
	if msg.SenderType == 1 {
		hitCount, err = countContentModerationHits(ctx, msg.SessionID, msg.SenderID, msg.SenderType)
		if err != nil {
			logContentModerationWarnf(
				"count content moderation hits failed session=%s sender=%d msg_id=%d err=%v",
				msg.SessionID,
				msg.SenderID,
				msg.MsgID,
				err,
			)
			hitCount = 1
		}
		if hitCount >= runtime.settings.HumanMuteThreshold {
			muteApplied, err = muteSessionMemberForContentModeration(ctx, msg.SessionID, msg.SenderID)
			if err != nil {
				logContentModerationWarnf(
					"mute session member for content moderation failed session=%s sender=%d msg_id=%d err=%v",
					msg.SessionID,
					msg.SenderID,
					msg.MsgID,
					err,
				)
			}
		}
	}

	if recallStatus == contentModerationRecallStatusFailed {
		nextRetryAt := nextContentModerationRetryAt(event.RecallAttempts, time.Now().UTC())
		if err := rescheduleContentModerationEvent(
			ctx,
			event.ID,
			recallStatus,
			hitCount,
			muteApplied,
			nextRetryAt,
		); err != nil {
			logContentModerationWarnf(
				"reschedule content moderation event failed session=%s msg_id=%d event_id=%d err=%v",
				msg.SessionID,
				msg.MsgID,
				event.ID,
				err,
			)
		}
		writeContentModerationAuditLog(msg, matchedKeywords, recallStatus, hitCount, muteApplied, event.RecallAttempts)
		return
	}

	if err := finalizeContentModerationEvent(ctx, event.ID, recallStatus, hitCount, muteApplied); err != nil {
		logContentModerationWarnf(
			"finalize content moderation event failed session=%s msg_id=%d event_id=%d err=%v",
			msg.SessionID,
			msg.MsgID,
			event.ID,
			err,
		)
	}

	writeContentModerationAuditLog(msg, matchedKeywords, recallStatus, hitCount, muteApplied, event.RecallAttempts)
}

func loadContentModerationRuntime() (contentModerationRuntime, error) {
	settings, err := systemsetting.GetContentModerationSettings()
	if err != nil {
		return contentModerationRuntime{}, err
	}

	signature := buildContentModerationRuntimeSignature(settings)

	contentModerationRuntimeCache.mu.RLock()
	cached := contentModerationRuntimeCache.runtime
	cachedSignature := contentModerationRuntimeCache.signature
	contentModerationRuntimeCache.mu.RUnlock()
	if cachedSignature == signature {
		return cached, nil
	}

	runtime := contentModerationRuntime{settings: settings}
	if settings.Enabled && len(settings.Keywords) > 0 {
		runtime.matcher = keywordmatcher.Compile(settings.Keywords)
	}

	contentModerationRuntimeCache.mu.Lock()
	contentModerationRuntimeCache.signature = signature
	contentModerationRuntimeCache.runtime = runtime
	contentModerationRuntimeCache.mu.Unlock()
	return runtime, nil
}

func buildContentModerationRuntimeSignature(settings systemsetting.ContentModerationSettings) string {
	keywords := append([]string(nil), settings.Keywords...)
	sortKeywords(keywords)
	return strconv.FormatBool(settings.Enabled) +
		"|" +
		strconv.Itoa(settings.HumanMuteThreshold) +
		"|" +
		strings.Join(keywords, "\x00")
}

func claimContentModerationEvent(
	ctx context.Context,
	msg model.Message,
	matchedKeywords []string,
) (model.ContentModerationEvent, bool, error) {
	raw, err := json.Marshal(matchedKeywords)
	if err != nil {
		return model.ContentModerationEvent{}, false, err
	}
	now := time.Now().UTC()

	event := model.ContentModerationEvent{
		SessionID:       msg.SessionID,
		MsgID:           msg.MsgID,
		SenderID:        msg.SenderID,
		SenderType:      msg.SenderType,
		MatchedKeywords: datatypes.JSON(raw),
		RecallStatus:    contentModerationRecallStatusPending,
		NextRetryAt:     &now,
	}
	tx := store.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "msg_id"}},
		DoNothing: true,
	}).Create(&event)
	if tx.Error != nil {
		return model.ContentModerationEvent{}, false, tx.Error
	}

	leaseUntil := now.Add(contentModerationProcessingLease)
	claimTx := store.DB.WithContext(ctx).
		Model(&model.ContentModerationEvent{}).
		Where(
			"msg_id = ? AND recall_status IN ? AND (next_retry_at IS NULL OR next_retry_at <= ?)",
			msg.MsgID,
			[]string{contentModerationRecallStatusPending, contentModerationRecallStatusFailed},
			now,
		).
		Updates(map[string]any{
			"session_id":       msg.SessionID,
			"sender_id":        msg.SenderID,
			"sender_type":      msg.SenderType,
			"matched_keywords": datatypes.JSON(raw),
			"next_retry_at":    leaseUntil,
			"recall_attempts":  gorm.Expr("recall_attempts + 1"),
		})
	if claimTx.Error != nil {
		return model.ContentModerationEvent{}, false, claimTx.Error
	}
	if claimTx.RowsAffected == 0 {
		return model.ContentModerationEvent{}, false, nil
	}

	var claimed model.ContentModerationEvent
	if err := store.DB.WithContext(ctx).
		Select("id", "msg_id", "recall_attempts").
		Where("msg_id = ?", msg.MsgID).
		Take(&claimed).Error; err != nil {
		return model.ContentModerationEvent{}, false, err
	}
	return claimed, true, nil
}

func recallContentModeratedMessage(ctx context.Context, msg model.Message) string {
	actor := MessageDeleteActor{}
	switch msg.SenderType {
	case 1:
		actor.UserID = msg.SenderID
	case 2:
		actor.AgentID = msg.SenderID
	default:
		return contentModerationRecallStatusUnsupported
	}

	if err := contentModerationDeleteMessage(ctx, msg.SessionID, msg.MsgID, actor); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return contentModerationRecallStatusMessageMissing
		}
		if status := detectContentModerationRecallStatus(ctx, msg); status != contentModerationRecallStatusFailed {
			return status
		}
		logContentModerationWarnf(
			"revoke content moderated message failed session=%s msg_id=%d sender=%d sender_type=%d err=%v",
			msg.SessionID,
			msg.MsgID,
			msg.SenderID,
			msg.SenderType,
			err,
		)
		return contentModerationRecallStatusFailed
	}
	return contentModerationRecallStatusRevoked
}

func detectContentModerationRecallStatus(ctx context.Context, msg model.Message) string {
	var current model.Message
	if err := store.DB.WithContext(ctx).
		Select("msg_id", "is_deleted", "is_revoked").
		Where("msg_id = ? AND session_id = ?", msg.MsgID, msg.SessionID).
		Take(&current).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return contentModerationRecallStatusMessageMissing
		}
		return contentModerationRecallStatusFailed
	}
	if current.IsDeleted || current.IsRevoked {
		return contentModerationRecallStatusAlreadyRevoked
	}
	return contentModerationRecallStatusFailed
}

func countContentModerationHits(
	ctx context.Context,
	sessionID string,
	senderID int64,
	senderType int16,
) (int, error) {
	var count int64
	if err := store.DB.WithContext(ctx).
		Model(&model.ContentModerationEvent{}).
		Where(
			"session_id = ? AND sender_id = ? AND sender_type = ?",
			sessionID,
			senderID,
			senderType,
		).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func muteSessionMemberForContentModeration(
	ctx context.Context,
	sessionID string,
	memberID int64,
) (bool, error) {
	if strings.TrimSpace(sessionID) == "" || memberID <= 0 {
		return false, nil
	}

	var target model.SessionMember
	if err := store.DB.WithContext(ctx).
		Select("session_id", "member_id", "member_type", "is_speak_muted").
		Where(
			"session_id = ? AND member_id = ? AND member_type = ?",
			sessionID,
			memberID,
			1,
		).
		Take(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if target.IsSpeakMuted {
		return false, nil
	}

	now := time.Now().UTC()
	if err := store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SessionMember{}).
			Where(
				"session_id = ? AND member_id = ? AND member_type = ?",
				sessionID,
				memberID,
				1,
			).
			Updates(map[string]any{
				"is_speak_muted": true,
			}).Error; err != nil {
			return err
		}
		return tx.Model(&model.Session{}).
			Where("session_id = ?", sessionID).
			Update("updated_at", now).Error
	}); err != nil {
		return false, err
	}

	humanMemberIDs, err := listSessionHumanMemberIDs(sessionID)
	if err != nil {
		logContentModerationWarnf(
			"list session human members after content moderation mute failed session=%s member=%d err=%v",
			sessionID,
			memberID,
			err,
		)
		return true, nil
	}
	notifySessionMemberChanged(
		sessionID,
		"speaking",
		0,
		humanMemberIDs,
		sessionMemberChangedNotifyMeta{
			MemberID: memberID,
		},
	)
	return true, nil
}

func finalizeContentModerationEvent(
	ctx context.Context,
	eventID int64,
	recallStatus string,
	hitCount int,
	muteApplied bool,
) error {
	if eventID <= 0 {
		return nil
	}
	return store.DB.WithContext(ctx).
		Model(&model.ContentModerationEvent{}).
		Where("id = ?", eventID).
		Updates(map[string]any{
			"recall_status": recallStatus,
			"hit_count":     hitCount,
			"mute_applied":  muteApplied,
			"next_retry_at": nil,
		}).Error
}

func rescheduleContentModerationEvent(
	ctx context.Context,
	eventID int64,
	recallStatus string,
	hitCount int,
	muteApplied bool,
	nextRetryAt time.Time,
) error {
	if eventID <= 0 {
		return nil
	}
	return store.DB.WithContext(ctx).
		Model(&model.ContentModerationEvent{}).
		Where("id = ?", eventID).
		Updates(map[string]any{
			"recall_status": recallStatus,
			"hit_count":     hitCount,
			"mute_applied":  muteApplied,
			"next_retry_at": nextRetryAt,
		}).Error
}

func writeContentModerationAuditLog(
	msg model.Message,
	matchedKeywords []string,
	recallStatus string,
	hitCount int,
	muteApplied bool,
	recallAttempts int,
) {
	sessionID := msg.SessionID
	msgID := msg.MsgID
	var userID *int64
	if msg.SenderType == 1 {
		senderID := msg.SenderID
		userID = &senderID
	}

	WriteAuditLog(WriteAuditLogReq{
		EventType: "msg_moderated",
		UserID:    userID,
		SessionID: &sessionID,
		MsgID:     &msgID,
		Detail: map[string]any{
			"sender_id":        msg.SenderID,
			"sender_type":      msg.SenderType,
			"matched_keywords": matchedKeywords,
			"recall_status":    recallStatus,
			"recall_attempts":  recallAttempts,
			"hit_count":        hitCount,
			"mute_applied":     muteApplied,
		},
	})
}

func runContentModerationRecoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(contentModerationRecoveryInterval)
	defer ticker.Stop()

	for {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			return
		}
		recoverPendingContentModerationTasks(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func recoverPendingContentModerationTasks(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if store.DB == nil || store.RDB == nil {
		return
	}

	now := time.Now().UTC()
	var tasks []ContentModerationTask
	if err := store.DB.WithContext(ctx).
		Model(&model.ContentModerationEvent{}).
		Select("session_id", "msg_id").
		Where(
			"recall_status IN ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?",
			[]string{contentModerationRecallStatusPending, contentModerationRecallStatusFailed},
			now,
		).
		Order("next_retry_at ASC, id ASC").
		Limit(contentModerationRecoveryBatchSize).
		Scan(&tasks).Error; err != nil {
		logContentModerationWarnf("load pending content moderation tasks failed: %v", err)
		return
	}
	for _, task := range tasks {
		if err := enqueueContentModerationTask(ctx, task); err != nil {
			logContentModerationWarnf(
				"requeue content moderation task failed session=%s msg_id=%d err=%v",
				task.SessionID,
				task.MsgID,
				err,
			)
		}
	}
}

func nextContentModerationRetryAt(attempts int, now time.Time) time.Time {
	if attempts <= 0 {
		attempts = 1
	}
	delay := contentModerationRetryBaseDelay
	for step := 1; step < attempts; step++ {
		delay *= 2
		if delay >= contentModerationRetryMaxDelay {
			delay = contentModerationRetryMaxDelay
			break
		}
	}
	return now.Add(delay)
}

func waitContentModerationBackoff(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func sortKeywords(values []string) {
	if len(values) < 2 {
		return
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i] < values[j]
	})
}

func logContentModerationWarnf(format string, args ...any) {
	if logger.L == nil {
		return
	}
	logger.L.Warnf(format, args...)
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
)

func TestProcessContentModerationTaskRevokesAndMutesHumanSender(t *testing.T) {
	cleanup := setupContentModerationServiceTest(t)
	defer cleanup()

	const (
		sessionID = "moderation-group-human"
		senderID  = int64(9101)
		peerID    = int64(9102)
	)
	seedContentModerationGroupSession(t, sessionID, senderID, peerID)

	if err := systemsetting.SaveContentModerationSettings(systemsetting.ContentModerationSettings{
		Enabled:            true,
		Keywords:           []string{"敏感词", "forbidden"},
		HumanMuteThreshold: 2,
	}, nil); err != nil {
		t.Fatalf("save moderation settings error: %v", err)
	}

	seedContentModerationMessage(t, sessionID, 70001, senderID, 1, "第一条包含敏感词的消息")
	processContentModerationTask(context.Background(), ContentModerationTask{
		SessionID: sessionID,
		MsgID:     70001,
	})

	assertModerationMessageRevoked(t, sessionID, 70001)
	assertMemberMuted(t, sessionID, senderID, false)

	seedContentModerationMessage(t, sessionID, 70002, senderID, 1, "second forbidden message")
	processContentModerationTask(context.Background(), ContentModerationTask{
		SessionID: sessionID,
		MsgID:     70002,
	})

	assertModerationMessageRevoked(t, sessionID, 70002)
	assertMemberMuted(t, sessionID, senderID, true)

	var secondEvent model.ContentModerationEvent
	if err := store.DB.Where("msg_id = ?", 70002).First(&secondEvent).Error; err != nil {
		t.Fatalf("load second moderation event error: %v", err)
	}
	if secondEvent.HitCount != 2 {
		t.Fatalf("second event hit_count = %d, want 2", secondEvent.HitCount)
	}
	if !secondEvent.MuteApplied {
		t.Fatal("expected second event to apply mute")
	}

	var auditCount int64
	if err := store.DB.Model(&model.AuditLog{}).
		Where("event_type = ?", "msg_moderated").
		Count(&auditCount).Error; err != nil {
		t.Fatalf("count moderation audit logs error: %v", err)
	}
	if auditCount != 2 {
		t.Fatalf("moderation audit log count = %d, want 2", auditCount)
	}
}

func TestProcessContentModerationTaskDeduplicatesByMessageID(t *testing.T) {
	cleanup := setupContentModerationServiceTest(t)
	defer cleanup()

	const (
		sessionID = "moderation-group-dedup"
		senderID  = int64(9201)
		peerID    = int64(9202)
	)
	seedContentModerationGroupSession(t, sessionID, senderID, peerID)

	if err := systemsetting.SaveContentModerationSettings(systemsetting.ContentModerationSettings{
		Enabled:            true,
		Keywords:           []string{"forbidden"},
		HumanMuteThreshold: 3,
	}, nil); err != nil {
		t.Fatalf("save moderation settings error: %v", err)
	}

	seedContentModerationMessage(t, sessionID, 71001, senderID, 1, "forbidden once")
	task := ContentModerationTask{SessionID: sessionID, MsgID: 71001}
	processContentModerationTask(context.Background(), task)
	processContentModerationTask(context.Background(), task)

	var eventCount int64
	if err := store.DB.Model(&model.ContentModerationEvent{}).
		Where("msg_id = ?", 71001).
		Count(&eventCount).Error; err != nil {
		t.Fatalf("count moderation events error: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("moderation event count = %d, want 1", eventCount)
	}
}

func TestProcessContentModerationTaskRevokesAgentMessageWithoutMute(t *testing.T) {
	cleanup := setupContentModerationServiceTest(t)
	defer cleanup()

	const (
		sessionID = "moderation-group-agent"
		ownerID   = int64(9301)
		peerID    = int64(9302)
		agentID   = int64(9303)
	)
	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: model.SessionTypeGroup,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1, LastActiveAt: now, JoinedAt: now},
	}
	if err := store.DB.Create(&members).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}

	if err := systemsetting.SaveContentModerationSettings(systemsetting.ContentModerationSettings{
		Enabled:            true,
		Keywords:           []string{"sensitive"},
		HumanMuteThreshold: 2,
	}, nil); err != nil {
		t.Fatalf("save moderation settings error: %v", err)
	}

	seedContentModerationMessage(t, sessionID, 72001, agentID, 2, "agent sensitive output")
	processContentModerationTask(context.Background(), ContentModerationTask{
		SessionID: sessionID,
		MsgID:     72001,
	})

	assertModerationMessageRevoked(t, sessionID, 72001)
	assertMemberMuted(t, sessionID, ownerID, false)
	assertMemberMuted(t, sessionID, peerID, false)

	var event model.ContentModerationEvent
	if err := store.DB.Where("msg_id = ?", 72001).First(&event).Error; err != nil {
		t.Fatalf("load moderation event error: %v", err)
	}
	if event.MuteApplied {
		t.Fatal("agent moderation event should not apply mute")
	}
}

func TestProcessContentModerationTaskRetriesFailedRecall(t *testing.T) {
	cleanup := setupContentModerationServiceTest(t)
	defer cleanup()

	const (
		sessionID = "moderation-group-retry"
		senderID  = int64(9401)
		peerID    = int64(9402)
		msgID     = int64(73001)
	)
	seedContentModerationGroupSession(t, sessionID, senderID, peerID)

	if err := systemsetting.SaveContentModerationSettings(systemsetting.ContentModerationSettings{
		Enabled:            true,
		Keywords:           []string{"forbidden"},
		HumanMuteThreshold: 3,
	}, nil); err != nil {
		t.Fatalf("save moderation settings error: %v", err)
	}

	seedContentModerationMessage(t, sessionID, msgID, senderID, 1, "retry forbidden content")

	originalDeleteMessage := contentModerationDeleteMessage
	deleteCalls := 0
	contentModerationDeleteMessage = func(ctx context.Context, sessionID string, msgID int64, actor MessageDeleteActor) error {
		deleteCalls++
		if deleteCalls == 1 {
			return errors.New("temporary delete failure")
		}
		return originalDeleteMessage(ctx, sessionID, msgID, actor)
	}

	task := ContentModerationTask{SessionID: sessionID, MsgID: msgID}
	processContentModerationTask(context.Background(), task)

	var firstEvent model.ContentModerationEvent
	if err := store.DB.Where("msg_id = ?", msgID).First(&firstEvent).Error; err != nil {
		t.Fatalf("load first moderation event error: %v", err)
	}
	if firstEvent.RecallStatus != contentModerationRecallStatusFailed {
		t.Fatalf("first recall_status=%s want=%s", firstEvent.RecallStatus, contentModerationRecallStatusFailed)
	}
	if firstEvent.RecallAttempts != 1 {
		t.Fatalf("first recall_attempts=%d want=1", firstEvent.RecallAttempts)
	}
	if firstEvent.NextRetryAt == nil || !firstEvent.NextRetryAt.After(time.Now().UTC()) {
		t.Fatal("expected first next_retry_at to be scheduled in the future")
	}

	var msgAfterFailure model.Message
	if err := store.DB.Where("session_id = ? AND msg_id = ?", sessionID, msgID).First(&msgAfterFailure).Error; err != nil {
		t.Fatalf("reload failed moderation message error: %v", err)
	}
	if msgAfterFailure.IsDeleted || msgAfterFailure.IsRevoked {
		t.Fatal("message should remain visible after failed recall attempt")
	}

	retryAt := time.Now().UTC().Add(-time.Second)
	if err := store.DB.Model(&model.ContentModerationEvent{}).
		Where("id = ?", firstEvent.ID).
		Update("next_retry_at", retryAt).Error; err != nil {
		t.Fatalf("force moderation retry_at error: %v", err)
	}

	processContentModerationTask(context.Background(), task)
	assertModerationMessageRevoked(t, sessionID, msgID)

	var secondEvent model.ContentModerationEvent
	if err := store.DB.Where("id = ?", firstEvent.ID).First(&secondEvent).Error; err != nil {
		t.Fatalf("reload retried moderation event error: %v", err)
	}
	if secondEvent.RecallStatus != contentModerationRecallStatusRevoked {
		t.Fatalf("second recall_status=%s want=%s", secondEvent.RecallStatus, contentModerationRecallStatusRevoked)
	}
	if secondEvent.RecallAttempts != 2 {
		t.Fatalf("second recall_attempts=%d want=2", secondEvent.RecallAttempts)
	}
	if secondEvent.NextRetryAt != nil {
		t.Fatalf("expected terminal moderation event next_retry_at cleared, got=%v", secondEvent.NextRetryAt)
	}
}

func TestRecoverPendingContentModerationTasksRequeuesDueFailures(t *testing.T) {
	cleanup := setupContentModerationServiceTest(t)
	defer cleanup()

	now := time.Now().UTC()
	future := now.Add(time.Minute)
	past := now.Add(-time.Second)
	events := []model.ContentModerationEvent{
		{
			SessionID:       "recover-session-due",
			MsgID:           74001,
			SenderID:        9501,
			SenderType:      1,
			MatchedKeywords: []byte(`["forbidden"]`),
			RecallStatus:    contentModerationRecallStatusFailed,
			RecallAttempts:  1,
			NextRetryAt:     &past,
		},
		{
			SessionID:       "recover-session-future",
			MsgID:           74002,
			SenderID:        9502,
			SenderType:      1,
			MatchedKeywords: []byte(`["forbidden"]`),
			RecallStatus:    contentModerationRecallStatusFailed,
			RecallAttempts:  1,
			NextRetryAt:     &future,
		},
	}
	if err := store.DB.Create(&events).Error; err != nil {
		t.Fatalf("seed moderation recovery events error: %v", err)
	}

	recoverPendingContentModerationTasks(context.Background())

	queued, err := store.RDB.LRange(context.Background(), contentModerationQueueKey, 0, -1).Result()
	if err != nil {
		t.Fatalf("load moderation recovery queue error: %v", err)
	}
	if len(queued) != 1 {
		t.Fatalf("queued recovery tasks=%d want=1", len(queued))
	}

	var task ContentModerationTask
	if err := json.Unmarshal([]byte(queued[0]), &task); err != nil {
		t.Fatalf("unmarshal queued recovery task error: %v", err)
	}
	if task.SessionID != "recover-session-due" || task.MsgID != 74001 {
		t.Fatalf("queued recovery task=%s/%d want recover-session-due/74001", task.SessionID, task.MsgID)
	}

	ttl := store.RDB.TTL(context.Background(), contentModerationQueueKey).Val()
	if ttl >= 0 {
		t.Fatalf("moderation queue ttl=%s want persistent list", fmt.Sprint(ttl))
	}
}

func setupContentModerationServiceTest(t *testing.T) func() {
	t.Helper()
	logger.Init()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	systemsetting.InvalidateContentModerationSettingsCache()
	resetContentModerationRuntimeCache()

	return func() {
		// 先真正停掉 worker（取消 ctx 并等它们退出），再还原全局与关库。
		// 原来只是把 sync.Once 重置掉——协程其实还在跑，会活过本用例去读下一个
		// 用例的 DB/logger（-race 必红）。
		StopContentModerationWorkers()
		contentModerationDispatchRunner = dispatchContentModerationTaskAsync
		contentModerationDeleteMessage = DeleteMessage
		systemsetting.InvalidateContentModerationSettingsCache()
		resetContentModerationRuntimeCache()
		_ = store.RDB.Close()
		testDB.Close()
	}
}

func resetContentModerationRuntimeCache() {
	contentModerationRuntimeCache.mu.Lock()
	contentModerationRuntimeCache.signature = ""
	contentModerationRuntimeCache.runtime = contentModerationRuntime{}
	contentModerationRuntimeCache.mu.Unlock()
}

func seedContentModerationGroupSession(t *testing.T, sessionID string, ownerID, peerID int64) {
	t.Helper()

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: model.SessionTypeGroup,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
	}
	if err := store.DB.Create(&members).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}
}

func seedContentModerationMessage(
	t *testing.T,
	sessionID string,
	msgID int64,
	senderID int64,
	senderType int16,
	content string,
) {
	t.Helper()

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   senderID,
		SenderType: senderType,
		MsgType:    1,
		Content:    content,
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}
	if err := store.DB.Model(&model.Session{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]any{
			"last_msg_id":      msgID,
			"last_msg_summary": content,
			"updated_at":       now,
		}).Error; err != nil {
		t.Fatalf("update session summary error: %v", err)
	}
}

func assertModerationMessageRevoked(t *testing.T, sessionID string, msgID int64) {
	t.Helper()

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_id = ?", sessionID, msgID).First(&msg).Error; err != nil {
		t.Fatalf("reload message error: %v", err)
	}
	if !msg.IsDeleted {
		t.Fatalf("msg %d expected IsDeleted=true", msgID)
	}
	if !msg.IsRevoked {
		t.Fatalf("msg %d expected IsRevoked=true", msgID)
	}
}

func assertMemberMuted(t *testing.T, sessionID string, memberID int64, want bool) {
	t.Helper()

	var member model.SessionMember
	if err := store.DB.Where(
		"session_id = ? AND member_id = ? AND member_type = 1",
		sessionID,
		memberID,
	).First(&member).Error; err != nil {
		t.Fatalf("reload session member error: %v", err)
	}
	if member.IsSpeakMuted != want {
		t.Fatalf("member %d IsSpeakMuted = %v, want %v", memberID, member.IsSpeakMuted, want)
	}
}

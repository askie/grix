package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	logger.Init()
}

func TestGetReachTaskStats(t *testing.T) {
	setupReachTestDB(t)

	taskID := snowflake.GenID()
	statsJSON, _ := json.Marshal(map[string]int{"sent": 5, "skipped": 2})
	require.NoError(t, store.DB.Create(&model.ReachTask{
		ID:       taskID,
		Kind:     model.ReachKindMarketing,
		Channels: []byte(`["in_app","email"]`),
		Status:   model.ReachStatusSent,
		Stats:    statsJSON,
	}).Error)

	store.DB.Create(&model.ReachSendLog{ID: snowflake.GenID(), TaskID: taskID, UserID: 1, Channel: model.ReachChannelInApp, Status: model.ReachSendStatusSent})
	store.DB.Create(&model.ReachSendLog{ID: snowflake.GenID(), TaskID: taskID, UserID: 2, Channel: model.ReachChannelInApp, Status: model.ReachSendStatusSent})
	store.DB.Create(&model.ReachSendLog{ID: snowflake.GenID(), TaskID: taskID, UserID: 1, Channel: model.ReachChannelEmail, Status: model.ReachSendStatusSent})
	store.DB.Create(&model.ReachSendLog{ID: snowflake.GenID(), TaskID: taskID, UserID: 3, Channel: model.ReachChannelInApp, Status: model.ReachSendStatusFailed})

	stats, err := GetReachTaskStats(taskID)
	require.NoError(t, err)
	assert.Equal(t, model.ReachStatusSent, stats.Status)
	assert.Equal(t, int64(2), stats.Channels[model.ReachChannelInApp])
	assert.Equal(t, int64(1), stats.Channels[model.ReachChannelEmail])
	assert.Equal(t, int64(3), stats.Total)
}

func TestGetReachSubscriptionOverview(t *testing.T) {
	setupReachTestDB(t)

	EnsureReachSubscription(1001, "cn")
	EnsureReachSubscription(1002, "cn")
	EnsureReachSubscription(1003, "global")

	overview, err := GetReachSubscriptionOverview()
	require.NoError(t, err)
	assert.Equal(t, int64(3), overview.TotalSubscriptions)
	assert.Equal(t, int64(2), overview.Subscribed)
	assert.Equal(t, int64(1), overview.Unsubscribed)
}

func TestScheduledTask_FiredWhenDue(t *testing.T) {
	setupReachTestDB(t)

	past := time.Now().UTC().Add(-1 * time.Minute)
	tpl, _ := CreateReachTemplate(CreateReachTemplateReq{Name: "定时", Title: "测试定时"})

	channelsJSON, _ := json.Marshal([]string{"in_app"})
	task := model.ReachTask{
		ID:          snowflake.GenID(),
		Kind:        model.ReachKindMarketing,
		TemplateID:  tpl.ID,
		Channels:    channelsJSON,
		Status:      model.ReachStatusScheduled,
		ScheduledAt: &past,
	}
	require.NoError(t, store.DB.Create(&task).Error)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fireDueReachTasks(ctx)
	time.Sleep(50 * time.Millisecond)

	var loaded model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", task.ID).First(&loaded).Error)
	// 触发后状态离开 scheduled；测试环境未配置客服账号，异步执行会将任务标记 failed，
	// 因此这里只断言"已被触发"，不锁定异步执行的终态。
	assert.NotEqual(t, model.ReachStatusScheduled, loaded.Status, "scheduled task should be fired")
	assert.NotEqual(t, model.ReachStatusCancelled, loaded.Status)
}

func TestScheduledTask_NotFiredIfFuture(t *testing.T) {
	setupReachTestDB(t)

	future := time.Now().UTC().Add(1 * time.Hour)
	tpl, _ := CreateReachTemplate(CreateReachTemplateReq{Name: "未来", Title: "未来定时"})

	channelsJSON, _ := json.Marshal([]string{"in_app"})
	task := model.ReachTask{
		ID:          snowflake.GenID(),
		Kind:        model.ReachKindMarketing,
		TemplateID:  tpl.ID,
		Channels:    channelsJSON,
		Status:      model.ReachStatusScheduled,
		ScheduledAt: &future,
	}
	require.NoError(t, store.DB.Create(&task).Error)

	fireDueReachTasks(t.Context())

	var loaded model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", task.ID).First(&loaded).Error)
	assert.Equal(t, model.ReachStatusScheduled, loaded.Status, "future task should remain scheduled")
}

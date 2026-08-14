package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReachTestUserIDs(t *testing.T) {
	ids, err := parseReachTestUserIDs(nil)
	require.NoError(t, err)
	assert.Nil(t, ids)

	ids, err = parseReachTestUserIDs(&ReachAudience{})
	require.NoError(t, err)
	assert.Nil(t, ids)

	ids, err = parseReachTestUserIDs(&ReachAudience{TestUserIDs: []string{"1001", " 2002 "}})
	require.NoError(t, err)
	assert.Equal(t, []int64{1001, 2002}, ids)

	_, err = parseReachTestUserIDs(&ReachAudience{TestUserIDs: []string{"abc"}})
	assert.Error(t, err)

	_, err = parseReachTestUserIDs(&ReachAudience{TestUserIDs: []string{"-5"}})
	assert.Error(t, err)

	over := make([]string, reachTestUserIDLimit+1)
	for i := range over {
		over[i] = fmt.Sprintf("%d", i+1)
	}
	_, err = parseReachTestUserIDs(&ReachAudience{TestUserIDs: over})
	assert.Error(t, err)
}

func TestCancelReachTask_ScheduledCanCancel(t *testing.T) {
	setupReachTestDB(t)

	future := time.Now().UTC().Add(1 * time.Hour)
	task := model.ReachTask{
		ID:          snowflake.GenID(),
		Kind:        model.ReachKindMarketing,
		Channels:    []byte(`["in_app"]`),
		Status:      model.ReachStatusScheduled,
		ScheduledAt: &future,
	}
	require.NoError(t, store.DB.Create(&task).Error)

	require.NoError(t, CancelReachTask(task.ID))
	var loaded model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", task.ID).First(&loaded).Error)
	assert.Equal(t, model.ReachStatusCancelled, loaded.Status)

	fireDueReachTasks(context.Background())
	require.NoError(t, store.DB.Where("id = ?", task.ID).First(&loaded).Error)
	assert.Equal(t, model.ReachStatusCancelled, loaded.Status, "cancelled task must never fire")
}

func TestDeleteReachTemplate_RefusedWhenReferencedByActiveTask(t *testing.T) {
	setupReachTestDB(t)

	tpl, err := CreateReachTemplate(CreateReachTemplateReq{Name: "在用模板", Title: "标题"})
	require.NoError(t, err)

	future := time.Now().UTC().Add(1 * time.Hour)
	task := model.ReachTask{
		ID:          snowflake.GenID(),
		Kind:        model.ReachKindMarketing,
		TemplateID:  tpl.ID,
		Channels:    []byte(`["in_app"]`),
		Status:      model.ReachStatusScheduled,
		ScheduledAt: &future,
	}
	require.NoError(t, store.DB.Create(&task).Error)

	err = DeleteReachTemplate(tpl.ID)
	require.Error(t, err, "deleting a template referenced by an active task must fail")
	assert.Contains(t, err.Error(), "referenced")

	require.NoError(t, CancelReachTask(task.ID))
	require.NoError(t, DeleteReachTemplate(tpl.ID), "delete succeeds once the task is cancelled")
}

func TestScheduler_TemplateMissing_MarksTaskFailed(t *testing.T) {
	setupReachTestDB(t)

	past := time.Now().UTC().Add(-1 * time.Minute)
	task := model.ReachTask{
		ID:          snowflake.GenID(),
		Kind:        model.ReachKindMarketing,
		TemplateID:  snowflake.GenID(), // 不存在的模板
		Channels:    []byte(`["in_app"]`),
		Status:      model.ReachStatusScheduled,
		ScheduledAt: &past,
	}
	require.NoError(t, store.DB.Create(&task).Error)

	fireDueReachTasks(context.Background())

	var loaded model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", task.ID).First(&loaded).Error)
	assert.Equal(t, model.ReachStatusFailed, loaded.Status, "task with missing template must be marked failed, not stuck in sending")
}

func TestExecuteMarketingTestSend_OnlyTargetsListedUsers(t *testing.T) {
	setupReachTestDB(t)

	const customerID = int64(9001)
	target := model.User{ID: 1001, Username: "u1001", Email: "u1001@t.local", Status: model.UserStatusActive, Region: "cn"}
	unsubscribed := model.User{ID: 1002, Username: "u1002", Email: "u1002@t.local", Status: model.UserStatusActive, Region: "cn"}
	bystander := model.User{ID: 1003, Username: "u1003", Email: "u1003@t.local", Status: model.UserStatusActive, Region: "cn"}
	require.NoError(t, store.DB.Create(&target).Error)
	require.NoError(t, store.DB.Create(&unsubscribed).Error)
	require.NoError(t, store.DB.Create(&bystander).Error)

	// 退订用户在试发时也应收到（试发是管理员显式指定目标预览内容）
	_, err := EnsureReachSubscription(unsubscribed.ID, "cn")
	require.NoError(t, err)
	require.NoError(t, UpdateReachSubscription(unsubscribed.ID, false))

	tpl, err := CreateReachTemplate(CreateReachTemplateReq{Name: "试发", Title: "试发标题", InAppBody: "试发内容"})
	require.NoError(t, err)

	task := model.ReachTask{
		ID:         snowflake.GenID(),
		Kind:       model.ReachKindMarketing,
		TemplateID: tpl.ID,
		Channels:   []byte(`["in_app"]`),
		Status:     model.ReachStatusSending,
	}
	require.NoError(t, store.DB.Create(&task).Error)

	executeMarketingTestSend(context.Background(), task.ID, customerID, *tpl,
		map[string]bool{model.ReachChannelInApp: true}, []int64{target.ID, unsubscribed.ID})

	var logs []model.ReachSendLog
	store.DB.Where("task_id = ?", task.ID).Find(&logs)
	gotUsers := make(map[int64]bool)
	for _, l := range logs {
		gotUsers[l.UserID] = true
		assert.Equal(t, model.ReachSendStatusSent, l.Status)
	}
	assert.True(t, gotUsers[target.ID])
	assert.True(t, gotUsers[unsubscribed.ID], "test send bypasses subscription preference")
	assert.False(t, gotUsers[bystander.ID], "users not in the test list must not receive anything")

	var loaded model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", task.ID).First(&loaded).Error)
	assert.Equal(t, model.ReachStatusSent, loaded.Status)
}

func TestCreateMarketingReachTask_RejectsInvalidTestUserIDs(t *testing.T) {
	setupReachTestDB(t)

	tpl, err := CreateReachTemplate(CreateReachTemplateReq{Name: "校验", Title: "标题"})
	require.NoError(t, err)

	_, err = CreateMarketingReachTask(context.Background(), CreateMarketingTaskReq{
		TemplateID: tpl.ID,
		Channels:   []string{"in_app"},
		Audience:   &ReachAudience{TestUserIDs: []string{"not-a-number"}},
	}, 1)
	assert.Error(t, err)
}

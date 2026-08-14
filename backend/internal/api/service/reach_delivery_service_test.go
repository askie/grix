package service

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/clause"
)

func setupReachTestDB(t *testing.T) {
	t.Helper()
	_ = snowflake.Init(1)
	testDB := testutil.NewTestDB()
	original := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = original
		testDB.Close()
	})
}

func TestWriteAppReleaseSystemMessage_WritesSystemMessage(t *testing.T) {
	setupReachTestDB(t)

	const customerID = int64(1001)
	const targetID = int64(2002)

	msg, err := WriteAppReleaseSystemMessage(customerID, targetID, DefaultAppReleaseAnnouncementContent("2.5.0", "修复若干问题"))
	require.NoError(t, err)
	assert.NotZero(t, msg.MsgID)
	assert.NotEmpty(t, msg.SessionID)
	assert.NotZero(t, msg.InboxSeq)
	assert.Contains(t, msg.Content, "2.5.0")
	assert.Contains(t, msg.Content, "修复若干问题")

	var stored model.Message
	require.NoError(t, store.DB.Where("msg_id = ?", msg.MsgID).First(&stored).Error)
	assert.Equal(t, int16(3), stored.SenderType)
	assert.Equal(t, int16(3), stored.MsgType)
	assert.Equal(t, customerID, stored.SenderID)

	var inboxCount int64
	store.DB.Model(&model.UserInbox{}).
		Where("user_id = ? AND msg_id = ?", targetID, msg.MsgID).Count(&inboxCount)
	assert.Equal(t, int64(1), inboxCount)

	var member model.SessionMember
	require.NoError(t, store.DB.
		Where("session_id = ? AND member_id = ?", msg.SessionID, targetID).
		First(&member).Error)
	assert.Equal(t, 1, member.UnreadCount)
}

func TestWriteAppReleaseSystemMessage_ReusesSameSession(t *testing.T) {
	setupReachTestDB(t)

	const customerID = int64(1001)
	const targetID = int64(2002)

	first, err := WriteAppReleaseSystemMessage(customerID, targetID, DefaultAppReleaseAnnouncementContent("2.5.0", ""))
	require.NoError(t, err)
	second, err := WriteAppReleaseSystemMessage(customerID, targetID, DefaultAppReleaseAnnouncementContent("2.6.0", ""))
	require.NoError(t, err)

	assert.Equal(t, first.SessionID, second.SessionID, "both notices land in the same customer session")
	assert.NotEqual(t, first.MsgID, second.MsgID)
}

func TestReachSendLog_UniqueIndexEnforcesIdempotency(t *testing.T) {
	setupReachTestDB(t)

	taskID := snowflake.GenID()
	row := func() *model.ReachSendLog {
		return &model.ReachSendLog{
			ID:      snowflake.GenID(),
			TaskID:  taskID,
			UserID:  2002,
			Channel: model.ReachChannelInApp,
			Status:  model.ReachSendStatusPending,
		}
	}

	res1 := store.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(row())
	require.NoError(t, res1.Error)
	assert.Equal(t, int64(1), res1.RowsAffected)

	res2 := store.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(row())
	require.NoError(t, res2.Error)
	assert.Equal(t, int64(0), res2.RowsAffected)

	other := row()
	other.UserID = 3003
	res3 := store.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(other)
	require.NoError(t, res3.Error)
	assert.Equal(t, int64(1), res3.RowsAffected)
}

func TestReachTask_CanCreateAndQuery(t *testing.T) {
	setupReachTestDB(t)

	task := model.ReachTask{
		ID:       snowflake.GenID(),
		Kind:     model.ReachKindSystemEvent,
		EventKey: "app_release_published",
		Channels: []byte(`["in_app","push"]`),
		Status:   model.ReachStatusSending,
	}
	require.NoError(t, store.DB.Create(&task).Error)

	var loaded model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", task.ID).First(&loaded).Error)
	assert.Equal(t, model.ReachKindSystemEvent, loaded.Kind)
	assert.Equal(t, model.ReachStatusSending, loaded.Status)
}

func TestPauseReachTask_OnlySendingCanPause(t *testing.T) {
	setupReachTestDB(t)

	task := model.ReachTask{
		ID:       snowflake.GenID(),
		Kind:     model.ReachKindSystemEvent,
		Channels: []byte(`["in_app"]`),
		Status:   model.ReachStatusSending,
	}
	require.NoError(t, store.DB.Create(&task).Error)

	require.NoError(t, PauseReachTask(task.ID))
	var loaded model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", task.ID).First(&loaded).Error)
	assert.Equal(t, model.ReachStatusPaused, loaded.Status)

	assert.Error(t, PauseReachTask(task.ID), "pausing a paused task should fail")
}

func TestCancelReachTask_SendingOrPausedCanCancel(t *testing.T) {
	setupReachTestDB(t)

	t1 := model.ReachTask{ID: snowflake.GenID(), Kind: model.ReachKindSystemEvent, Channels: []byte(`[]`), Status: model.ReachStatusSending}
	require.NoError(t, store.DB.Create(&t1).Error)
	require.NoError(t, CancelReachTask(t1.ID))
	var l1 model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", t1.ID).First(&l1).Error)
	assert.Equal(t, model.ReachStatusCancelled, l1.Status)

	t2 := model.ReachTask{ID: snowflake.GenID(), Kind: model.ReachKindSystemEvent, Channels: []byte(`[]`), Status: model.ReachStatusPaused}
	require.NoError(t, store.DB.Create(&t2).Error)
	require.NoError(t, CancelReachTask(t2.ID))

	t3 := model.ReachTask{ID: snowflake.GenID(), Kind: model.ReachKindSystemEvent, Channels: []byte(`[]`), Status: model.ReachStatusSent}
	require.NoError(t, store.DB.Create(&t3).Error)
	assert.Error(t, CancelReachTask(t3.ID), "cancelling a sent task should fail")

	t4 := model.ReachTask{ID: snowflake.GenID(), Kind: model.ReachKindSystemEvent, Channels: []byte(`[]`), Status: model.ReachStatusDraft}
	require.NoError(t, store.DB.Create(&t4).Error)
	require.NoError(t, CancelReachTask(t4.ID), "a draft announcement must be cancellable")
	var l4 model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", t4.ID).First(&l4).Error)
	assert.Equal(t, model.ReachStatusCancelled, l4.Status)
}

func TestFanOutRespectsTaskStatus(t *testing.T) {
	setupReachTestDB(t)

	task := model.ReachTask{
		ID:       snowflake.GenID(),
		Kind:     model.ReachKindSystemEvent,
		Channels: []byte(`["in_app"]`),
		Status:   model.ReachStatusPaused,
	}
	require.NoError(t, store.DB.Create(&task).Error)

	content := DefaultAppReleaseAnnouncementContent("1.0", "")
	_, _, finalStatus := fanOutAppRelease(context.Background(), task.ID, 9999, content)
	assert.Equal(t, model.ReachStatusPaused, finalStatus, "fan-out should stop immediately when task is paused")
}

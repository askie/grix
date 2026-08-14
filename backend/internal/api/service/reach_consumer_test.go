package service

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/reach"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedReachCustomerAccount points the auth settings at a test customer account
// and restores the empty default (cache included) when the test ends.
func seedReachCustomerAccount(t *testing.T, customerUserID int64) {
	t.Helper()
	require.NoError(t, systemsetting.SaveAuthSettings(
		systemsetting.AuthSettings{AutoAddCustomerUserID: customerUserID}, nil))
	t.Cleanup(systemsetting.InvalidateAuthSettingsCache)
}

func TestCreateAppReleaseReachTask_DedupSameVersion(t *testing.T) {
	setupReachTestDB(t)

	iosEvt := reach.AppReleaseEvent{
		EventKey:  reach.EventAppReleasePublished,
		ReleaseID: 101,
		Version:   "3.1.4",
		Channel:   "stable",
		Platform:  "ios",
	}
	macEvt := reach.AppReleaseEvent{
		EventKey:  reach.EventAppReleasePublished,
		ReleaseID: 102,
		Version:   "3.1.4",
		Channel:   "stable",
		Platform:  "macos",
	}

	first, created, err := createAppReleaseReachTask(iosEvt)
	require.NoError(t, err)
	require.True(t, created)
	require.NotZero(t, first.ID)

	_, created, err = createAppReleaseReachTask(macEvt)
	require.NoError(t, err)
	assert.False(t, created, "same-version event must not create a second task")

	var count int64
	store.DB.Model(&model.ReachTask{}).
		Where("event_key = ?", reach.EventAppReleasePublished).Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestCreateAppReleaseReachTask_DifferentVersionsCreateTasks(t *testing.T) {
	setupReachTestDB(t)

	_, created, err := createAppReleaseReachTask(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 201, Version: "3.1.4", Channel: "stable",
	})
	require.NoError(t, err)
	require.True(t, created)

	_, created, err = createAppReleaseReachTask(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 202, Version: "3.1.5", Channel: "stable",
	})
	require.NoError(t, err)
	assert.True(t, created, "a new version must create its own task")

	var count int64
	store.DB.Model(&model.ReachTask{}).
		Where("event_key = ?", reach.EventAppReleasePublished).Count(&count)
	assert.Equal(t, int64(2), count)
}

func TestCreateAppReleaseReachTask_BetaDoesNotSwallowStable(t *testing.T) {
	setupReachTestDB(t)

	_, created, err := createAppReleaseReachTask(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 211, Version: "3.1.4", Channel: "beta",
	})
	require.NoError(t, err)
	require.True(t, created)

	_, created, err = createAppReleaseReachTask(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 212, Version: "3.1.4", Channel: "stable",
	})
	require.NoError(t, err)
	assert.True(t, created, "stable release must announce even after a beta of the same version")
}

func TestCreateAppReleaseReachTask_EmptyVersionFallsBackToReleaseID(t *testing.T) {
	setupReachTestDB(t)

	_, created, err := createAppReleaseReachTask(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 301,
	})
	require.NoError(t, err)
	require.True(t, created)

	// A different release without a version must not be swallowed by the first.
	_, created, err = createAppReleaseReachTask(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 302,
	})
	require.NoError(t, err)
	assert.True(t, created)

	// Redelivery of the same release (same ID) is still deduplicated.
	_, created, err = createAppReleaseReachTask(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 302,
	})
	require.NoError(t, err)
	assert.False(t, created)
}

func TestReachTaskDedupKey(t *testing.T) {
	assert.Equal(t, "app_release_published:stable:3.1.4", reachTaskDedupKey(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 1, Version: " 3.1.4 ", Channel: "stable",
	}))
	assert.Equal(t, "app_release_published:release-9", reachTaskDedupKey(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 9,
	}))
}

func TestCreateAppReleaseReachTask_CreatesDraftWithDefaultContent(t *testing.T) {
	setupReachTestDB(t)

	task, created, err := createAppReleaseReachTask(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 701, Version: "3.5.0", Channel: "stable",
		Changelog: "修复若干问题",
	})
	require.NoError(t, err)
	require.True(t, created)

	var loaded model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", task.ID).First(&loaded).Error)
	assert.Equal(t, model.ReachStatusDraft, loaded.Status, "publish must only create a draft, never auto-send")

	content := parseReachAnnouncementContent(&loaded)
	assert.Contains(t, content.ZH.Title, "3.5.0")
	assert.Contains(t, content.EN.Title, "3.5.0")
	assert.Equal(t, "修复若干问题", content.ZH.Body)

	// No fan-out happened: not a single send log exists.
	var logs int64
	store.DB.Model(&model.ReachSendLog{}).Where("task_id = ?", task.ID).Count(&logs)
	assert.Zero(t, logs)
}

func TestUpdateReachAnnouncementContent(t *testing.T) {
	setupReachTestDB(t)

	task, created, err := createAppReleaseReachTask(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 702, Version: "3.5.1", Channel: "stable",
	})
	require.NoError(t, err)
	require.True(t, created)

	edited := DefaultAppReleaseAnnouncementContent("3.5.1", "带来了全新体验")
	edited.ZH.Title = "Grix 3.5.1 重磅更新"
	require.NoError(t, UpdateReachAnnouncementContent(task.ID, edited))

	var loaded model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", task.ID).First(&loaded).Error)
	got := parseReachAnnouncementContent(&loaded)
	assert.Equal(t, "Grix 3.5.1 重磅更新", got.ZH.Title)
	assert.Equal(t, "带来了全新体验", got.ZH.Body)

	// Titles are required in both languages.
	bad := edited
	bad.EN.Title = " "
	assert.Error(t, UpdateReachAnnouncementContent(task.ID, bad))

	// Only drafts are editable — a sending snapshot must stay stable.
	require.NoError(t, store.DB.Model(&model.ReachTask{}).Where("id = ?", task.ID).
		Update("status", model.ReachStatusSending).Error)
	assert.Error(t, UpdateReachAnnouncementContent(task.ID, edited))
}

func TestSendReachAnnouncement_DraftFansOutOnce(t *testing.T) {
	setupReachTestDB(t)
	seedReachCustomerAccount(t, 1001)

	require.NoError(t, store.DB.Create(&model.User{
		ID: 1001, Username: "customer", Email: "cs@test.local", Nickname: "小虾妹", Status: model.UserStatusActive,
	}).Error)
	require.NoError(t, store.DB.Create(&model.User{
		ID: 2002, Username: "user2002", Email: "u2002@test.local", Nickname: "老用户", Status: model.UserStatusActive,
	}).Error)

	task, created, err := createAppReleaseReachTask(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 801, Version: "3.6.0", Channel: "stable",
		Changelog: "新增群发可控开关",
	})
	require.NoError(t, err)
	require.True(t, created)

	require.NoError(t, SendReachAnnouncement(context.Background(), task.ID))

	// Fan-out runs async; wait for the terminal status.
	require.Eventually(t, func() bool {
		var after model.ReachTask
		if err := store.DB.Where("id = ?", task.ID).First(&after).Error; err != nil {
			return false
		}
		return after.Status == model.ReachStatusSent
	}, 5*time.Second, 20*time.Millisecond)

	var logs int64
	store.DB.Model(&model.ReachSendLog{}).
		Where("task_id = ? AND user_id = ? AND channel = ? AND status = ?",
			task.ID, int64(2002), model.ReachChannelInApp, model.ReachSendStatusSent).
		Count(&logs)
	assert.Equal(t, int64(1), logs)

	var msg model.Message
	require.NoError(t, store.DB.Where("sender_id = ?", int64(1001)).First(&msg).Error)
	assert.Contains(t, msg.Content, "3.6.0")
	assert.Contains(t, msg.Content, "新增群发可控开关")

	// A second send must be rejected: the draft is gone.
	assert.Error(t, SendReachAnnouncement(context.Background(), task.ID))
}

func TestResumeReachTask_RescuesStaleSending(t *testing.T) {
	setupReachTestDB(t)
	seedReachCustomerAccount(t, 1001)

	task, created, err := createAppReleaseReachTask(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 802, Version: "3.6.1", Channel: "stable",
	})
	require.NoError(t, err)
	require.True(t, created)

	// A draft is not resumable — it must go through the explicit send action.
	assert.Error(t, ResumeReachTask(context.Background(), task.ID))

	// A fresh sending task (live fan-out heartbeat) must NOT be resumed.
	require.NoError(t, store.DB.Model(&model.ReachTask{}).Where("id = ?", task.ID).
		Update("status", model.ReachStatusSending).Error)
	assert.Error(t, ResumeReachTask(context.Background(), task.ID))

	// A sending task idle past the ack window is a crashed fan-out: resume it.
	stale := time.Now().UTC().Add(-2 * reachConsumerAckWait)
	require.NoError(t, store.DB.Model(&model.ReachTask{}).Where("id = ?", task.ID).
		Update("updated_at", stale).Error)
	require.NoError(t, ResumeReachTask(context.Background(), task.ID))

	require.Eventually(t, func() bool {
		var after model.ReachTask
		if err := store.DB.Where("id = ?", task.ID).First(&after).Error; err != nil {
			return false
		}
		return after.Status == model.ReachStatusSent
	}, 5*time.Second, 20*time.Millisecond)
}

func TestFillReleaseChannel_LegacyEventMatchesNewKey(t *testing.T) {
	setupReachTestDB(t)

	now := time.Now().UTC()
	require.NoError(t, store.DB.Create(&model.AppRelease{
		ID: 601, Version: "3.1.4", BuildNumber: 690, Platform: "ios", Channel: "stable",
		Status: model.ReleaseStatusPublished, PublishedAt: &now,
	}).Error)

	// A pre-channel pod publishes the event without a channel; after filling,
	// its dedup key must equal the one a new pod's event produces.
	legacy := reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 601, Version: "3.1.4",
	}
	fillReleaseChannel(&legacy)
	assert.Equal(t, "stable", legacy.Channel)
	assert.Equal(t,
		reachTaskDedupKey(reach.AppReleaseEvent{
			EventKey: reach.EventAppReleasePublished, ReleaseID: 602, Version: "3.1.4", Channel: "stable",
		}),
		reachTaskDedupKey(legacy))

	// Unknown release ID degrades to the empty channel instead of erroring.
	missing := reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 999, Version: "3.1.4",
	}
	fillReleaseChannel(&missing)
	assert.Empty(t, missing.Channel)

	// An event that already carries a channel is left untouched.
	beta := reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 601, Version: "3.1.4", Channel: "beta",
	}
	fillReleaseChannel(&beta)
	assert.Equal(t, "beta", beta.Channel)
}

func TestReachEventFromTask_RebuildsVersionAndChangelog(t *testing.T) {
	setupReachTestDB(t)

	now := time.Now().UTC()
	require.NoError(t, store.DB.Create(&model.AppRelease{
		ID: 501, Version: "3.3.0", BuildNumber: 700, Platform: "ios", Channel: "stable",
		Changelog: "修复若干问题", Status: model.ReleaseStatusPublished, PublishedAt: &now,
	}).Error)

	key := "app_release_published:stable:3.3.0"
	evt := reachEventFromTask(&model.ReachTask{
		EventKey: reach.EventAppReleasePublished, DedupKey: &key,
	})
	assert.Equal(t, "3.3.0", evt.Version)
	assert.Equal(t, "stable", evt.Channel)
	assert.Equal(t, "修复若干问题", evt.Changelog)
	assert.Equal(t, int64(501), evt.ReleaseID)

	// Tasks without a dedup key (legacy rows / release-id fallback) degrade gracefully.
	evt = reachEventFromTask(&model.ReachTask{EventKey: reach.EventAppReleasePublished})
	assert.Empty(t, evt.Version)
	fallback := "app_release_published:release-9"
	evt = reachEventFromTask(&model.ReachTask{EventKey: reach.EventAppReleasePublished, DedupKey: &fallback})
	assert.Empty(t, evt.Version)
}

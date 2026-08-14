package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackReachOpen(t *testing.T) {
	setupReachTestDB(t)

	logID := snowflake.GenID()
	require.NoError(t, store.DB.Create(&model.ReachSendLog{
		ID: logID, TaskID: snowflake.GenID(), UserID: 1,
		Channel: model.ReachChannelEmail, Status: model.ReachSendStatusSent,
	}).Error)

	require.NoError(t, TrackReachOpen(logID))

	var log model.ReachSendLog
	require.NoError(t, store.DB.Where("id = ?", logID).First(&log).Error)
	assert.NotNil(t, log.OpenedAt, "opened_at should be set")
}

func TestTrackReachOpen_Idempotent(t *testing.T) {
	setupReachTestDB(t)

	logID := snowflake.GenID()
	require.NoError(t, store.DB.Create(&model.ReachSendLog{
		ID: logID, TaskID: snowflake.GenID(), UserID: 1,
		Channel: model.ReachChannelEmail, Status: model.ReachSendStatusSent,
	}).Error)

	require.NoError(t, TrackReachOpen(logID))
	var log model.ReachSendLog
	store.DB.Where("id = ?", logID).First(&log)
	firstOpenedAt := log.OpenedAt

	time.Sleep(10 * time.Millisecond)
	require.NoError(t, TrackReachOpen(logID))

	store.DB.Where("id = ?", logID).First(&log)
	assert.Equal(t, firstOpenedAt, log.OpenedAt, "second open should not overwrite")
}

func TestTrackReachClick(t *testing.T) {
	setupReachTestDB(t)

	logID := snowflake.GenID()
	require.NoError(t, store.DB.Create(&model.ReachSendLog{
		ID: logID, TaskID: snowflake.GenID(), UserID: 1,
		Channel: model.ReachChannelEmail, Status: model.ReachSendStatusSent,
	}).Error)

	require.NoError(t, TrackReachClick(logID))

	var log model.ReachSendLog
	require.NoError(t, store.DB.Where("id = ?", logID).First(&log).Error)
	assert.NotNil(t, log.ClickedAt, "clicked_at should be set")
}

func TestInjectEmailTracking(t *testing.T) {
	html := `<html><body><p>Hello</p></body></html>`
	result := InjectEmailTracking(html, 12345)
	assert.Contains(t, result, `<img src=`)
	assert.Contains(t, result, `/v1/reach/t/o/12345`)
	assert.Contains(t, result, `</body>`)
}

func TestInjectEmailTracking_NoBodyTag(t *testing.T) {
	html := `<p>Hello world</p>`
	result := InjectEmailTracking(html, 12345)
	assert.Contains(t, result, `/v1/reach/t/o/12345`)
}

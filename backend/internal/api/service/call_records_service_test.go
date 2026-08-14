package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedCallRecord(t *testing.T, id, callerID, calleeID int64, state int16) {
	t.Helper()
	now := time.Now()
	rec := model.CallRecord{
		ID:             id,
		SessionID:      "sess-test",
		CallerID:       callerID,
		CalleeID:       calleeID,
		CallMode:       model.CallModeVoice,
		State:          state,
		DelegationMode: model.CallDelegationHuman,
		StartedAt:      &now,
	}
	require.NoError(t, store.DB.Create(&rec).Error)
}

func TestListCallRecords_BasicPagination(t *testing.T) {
	testDB := setupServiceTest(t)
	defer testDB.Close()

	// 为 userID=100 创建 3 条记录（2 作为 caller，1 作为 callee）
	seedCallRecord(t, 1001, 100, 200, model.CallStateEnded)
	seedCallRecord(t, 1002, 100, 201, model.CallStateEnded)
	seedCallRecord(t, 1003, 300, 100, model.CallStateEnded)
	// 不相关的记录
	seedCallRecord(t, 1004, 400, 500, model.CallStateEnded)

	resp, err := ListCallRecords(100, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(3), resp.Total)
	assert.Len(t, resp.Items, 3)
	assert.Equal(t, 1, resp.Page)
	assert.Equal(t, 10, resp.PageSize)
}

func TestListCallRecords_Pagination(t *testing.T) {
	testDB := setupServiceTest(t)
	defer testDB.Close()

	for i := int64(1); i <= 5; i++ {
		seedCallRecord(t, 2000+i, 200, 300, model.CallStateEnded)
	}

	// 第 1 页，每页 2 条
	resp, err := ListCallRecords(200, 1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(5), resp.Total)
	assert.Len(t, resp.Items, 2)

	// 第 3 页，每页 2 条 → 1 条
	resp, err = ListCallRecords(200, 3, 2)
	require.NoError(t, err)
	assert.Len(t, resp.Items, 1)
}

func TestListCallRecords_Empty(t *testing.T) {
	testDB := setupServiceTest(t)
	defer testDB.Close()

	resp, err := ListCallRecords(999, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp.Total)
	assert.Empty(t, resp.Items)
}

func TestListCallRecords_TimestampConversion(t *testing.T) {
	testDB := setupServiceTest(t)
	defer testDB.Close()

	seedCallRecord(t, 3001, 300, 400, model.CallStateEnded)

	resp, err := ListCallRecords(300, 1, 10)
	require.NoError(t, err)
	require.Len(t, resp.Items, 1)
	// started_at 应转换为毫秒时间戳
	assert.NotNil(t, resp.Items[0].StartedAt)
	assert.Greater(t, *resp.Items[0].StartedAt, int64(0))
}

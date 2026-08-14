package store_test

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
)

// setupRelayStateStoreTest 准备内存库并播种一对 agent/wallet 父行
// （state 表的 agent_id/wallet_id 都有外键，虽然 sqlite 默认不强制，播种让用例贴近真实约束）。
func setupRelayStateStoreTest(t *testing.T, agentID, walletID int64) {
	t.Helper()
	testDB := testutil.NewTestDB()
	t.Cleanup(testDB.Close)
	previousDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() { store.DB = previousDB })

	require.NoError(t, store.DB.Create(&model.Agent{
		ID: agentID, AgentName: "relay-state-agent", OwnerID: 7001,
	}).Error)
	require.NoError(t, store.DB.Create(&model.GatewayWallet{
		ID: walletID, OwnerID: 7001,
	}).Error)
}

func int64Ptr(v int64) *int64 { return &v }

// 乐观锁：expected_revision 与服务端当前不一致必须拒绝并报冲突；一致才写、revision+1。
func TestUpsertGatewayAgentRelayStateDesired_OptimisticLock(t *testing.T) {
	setupRelayStateStoreTest(t, 7101, 7100)

	// 首次写入（无 expected）：revision 从 1 开始。
	row, err := store.UpsertGatewayAgentRelayStateDesired(7101, 7100, true, "deepseek-v4-flash", nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), row.Revision)
	require.True(t, row.Enabled)
	require.Equal(t, "deepseek-v4-flash", row.RelayModel)

	// 过期 revision 写 → 冲突，库内值不变。
	_, err = store.UpsertGatewayAgentRelayStateDesired(7101, 7100, false, "", int64Ptr(0))
	require.ErrorIs(t, err, store.ErrGatewayAgentRelayStateRevisionConflict)
	latest, err := store.GetGatewayAgentRelayState(7101)
	require.NoError(t, err)
	require.Equal(t, int64(1), latest.Revision)
	require.True(t, latest.Enabled)

	// 正确 revision → 写入成功且 +1。
	row, err = store.UpsertGatewayAgentRelayStateDesired(7101, 7100, false, "", int64Ptr(1))
	require.NoError(t, err)
	require.Equal(t, int64(2), row.Revision)
	require.False(t, row.Enabled)
	require.Equal(t, "", row.RelayModel)

	// 同一 revision 再用一次 → 冲突（已被上一写消费）。
	_, err = store.UpsertGatewayAgentRelayStateDesired(7101, 7100, true, "m", int64Ptr(1))
	require.ErrorIs(t, err, store.ErrGatewayAgentRelayStateRevisionConflict)
}

// 行不存在时带 expected_revision 视为首次迁移写入（前端拿不到 revision 就不会传，
// 传了的按新建处理），从 revision 1 开始。
func TestUpsertGatewayAgentRelayStateDesired_ExpectedRevisionOnMissingRowCreates(t *testing.T) {
	setupRelayStateStoreTest(t, 7201, 7200)

	row, err := store.UpsertGatewayAgentRelayStateDesired(7201, 7200, true, "deepseek-v4-flash", int64Ptr(0))
	require.NoError(t, err)
	require.Equal(t, int64(1), row.Revision)
	require.True(t, row.Enabled)
}

// 不传 expected_revision 时 last-write-wins：覆盖写、revision 在库内自增。
func TestUpsertGatewayAgentRelayStateDesired_LastWriteWins(t *testing.T) {
	setupRelayStateStoreTest(t, 7301, 7300)

	_, err := store.UpsertGatewayAgentRelayStateDesired(7301, 7300, true, "m1", nil)
	require.NoError(t, err)

	row, err := store.UpsertGatewayAgentRelayStateDesired(7301, 7300, false, "m2", nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), row.Revision)
	require.False(t, row.Enabled)
	require.Equal(t, "m2", row.RelayModel)
}

// SetApplied 只写回 actual 字段，不碰 desired/revision；无 desired 行的 agent 不回执建行。
func TestSetGatewayAgentRelayStateApplied(t *testing.T) {
	setupRelayStateStoreTest(t, 7401, 7400)
	setupRelayStateStorelessAgent(t, 7402)

	_, err := store.UpsertGatewayAgentRelayStateDesired(7401, 7400, true, "m", nil)
	require.NoError(t, err)

	appliedAt := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.SetGatewayAgentRelayStateApplied(7401, true, appliedAt))

	row, err := store.GetGatewayAgentRelayState(7401)
	require.NoError(t, err)
	require.True(t, row.Applied)
	require.NotNil(t, row.AppliedAt)
	require.True(t, appliedAt.Equal(*row.AppliedAt))
	// desired 与 revision 不被回执触碰。
	require.True(t, row.Enabled)
	require.Equal(t, int64(1), row.Revision)

	// 无 desired 行的 agent：回执丢弃，不建行。
	require.NoError(t, store.SetGatewayAgentRelayStateApplied(7402, true, appliedAt))
	_, err = store.GetGatewayAgentRelayState(7402)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}

// 按钱包批量取回：GET /v1/gateway/agents 的装配路径。
func TestListGatewayAgentRelayStatesByWallet(t *testing.T) {
	setupRelayStateStoreTest(t, 7501, 7500)
	setupRelayStateStorelessAgent(t, 7502)

	_, err := store.UpsertGatewayAgentRelayStateDesired(7501, 7500, true, "m", nil)
	require.NoError(t, err)
	_, err = store.UpsertGatewayAgentRelayStateDesired(7502, 7500, false, "", nil)
	require.NoError(t, err)

	rows, err := store.ListGatewayAgentRelayStatesByWallet(7500)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// 别的钱包不混入。
	rows, err = store.ListGatewayAgentRelayStatesByWallet(9999)
	require.NoError(t, err)
	require.Empty(t, rows)
}

// setupRelayStateStorelessAgent 额外播种一个没有 state 行的 agent（复用当前测试库）。
func setupRelayStateStorelessAgent(t *testing.T, agentID int64) {
	t.Helper()
	require.NoError(t, store.DB.Create(&model.Agent{
		ID: agentID, AgentName: "relay-state-agent-extra", OwnerID: 7001,
	}).Error)
}

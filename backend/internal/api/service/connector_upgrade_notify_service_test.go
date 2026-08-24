package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupConnectorUpgradeNotifyTest(t *testing.T) {
	t.Helper()
	logger.Init()
	testDB := testutil.NewTestDB()
	origDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = origDB
		testDB.Close()
	})

	now := time.Now()
	recent := now.Add(-2 * 24 * time.Hour)
	stale := now.Add(-60 * 24 * time.Hour)
	agents := []model.Agent{
		{ID: 1001, AgentName: "old-a1", OwnerID: 10, ConnectorVersion: "3.25.0", ConnectorVersionSeenAt: &recent},
		{ID: 1002, AgentName: "old-a2", OwnerID: 10, ConnectorVersion: "0.1.0", ConnectorVersionSeenAt: &recent},
		{ID: 1003, AgentName: "new-b", OwnerID: 20, ConnectorVersion: "4.2.3", ConnectorVersionSeenAt: &recent},
		{ID: 1004, AgentName: "stale-c", OwnerID: 30, ConnectorVersion: "3.0.0", ConnectorVersionSeenAt: &stale},
		{ID: 1005, AgentName: "no-version-d", OwnerID: 40},
		{ID: 1006, AgentName: "old-e-disabled", OwnerID: 50, Status: 2, ConnectorVersion: "3.0.0", ConnectorVersionSeenAt: &recent},
		{ID: 1007, AgentName: "boundary-f", OwnerID: 60, ConnectorVersion: "3.34.1", ConnectorVersionSeenAt: &recent},
		{ID: 1008, AgentName: "hermes-g", OwnerID: 70, ConnectorClient: "hermes-agent", ConnectorVersion: "1.13.5", ConnectorVersionSeenAt: &recent},
		{ID: 1009, AgentName: "openclaw-h", OwnerID: 80, ConnectorClient: "openclaw-grix", ConnectorVersion: "0.4.31", ConnectorVersionSeenAt: &recent},
		{ID: 1010, AgentName: "legacy-row-i", OwnerID: 90, ConnectorClient: "", ConnectorVersion: "3.0.0", ConnectorVersionSeenAt: &recent},
	}
	for i := range agents {
		if agents[i].Status == 0 {
			agents[i].Status = 1
		}
		if agents[i].ConnectorClient == "" && agents[i].ID < 1008 {
			agents[i].ConnectorClient = ConnectorClientGrixConnector
		}
		require.NoError(t, store.DB.Create(&agents[i]).Error)
	}
}

func TestNotifyConnectorUpgradeDryRunGroupsByOwner(t *testing.T) {
	setupConnectorUpgradeNotifyTest(t)
	called := false
	orig := sendConnectorUpgradeReach
	sendConnectorUpgradeReach = func(context.Context, SendDirectUserReachReq) (*SendDirectUserReachResult, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { sendConnectorUpgradeReach = orig })

	res, err := NotifyConnectorUpgrade(context.Background(), ConnectorUpgradeNotifyReq{
		BelowVersion: "3.34.1", TargetVersion: "4.2.3", DryRun: true,
	})
	require.NoError(t, err)
	assert.True(t, res.DryRun)
	assert.False(t, called)
	require.Len(t, res.Users, 1)
	assert.Equal(t, int64(10), res.Users[0].UserID)
	require.Len(t, res.Users[0].Agents, 2)
	assert.Equal(t, "3.25.0", res.Users[0].Agents[0].ConnectorVersion)
	assert.Equal(t, "0.1.0", res.Users[0].Agents[1].ConnectorVersion)
}

func TestNotifyConnectorUpgradeSendsOnePerUserWithDedupKey(t *testing.T) {
	setupConnectorUpgradeNotifyTest(t)
	var sent []SendDirectUserReachReq
	orig := sendConnectorUpgradeReach
	sendConnectorUpgradeReach = func(_ context.Context, req SendDirectUserReachReq) (*SendDirectUserReachResult, error) {
		sent = append(sent, req)
		return &SendDirectUserReachResult{Status: model.ReachStatusSent, Channel: model.ReachChannelEmail}, nil
	}
	t.Cleanup(func() { sendConnectorUpgradeReach = orig })

	res, err := NotifyConnectorUpgrade(context.Background(), ConnectorUpgradeNotifyReq{
		BelowVersion: "3.34.1", TargetVersion: "4.2.3",
	})
	require.NoError(t, err)
	require.Len(t, sent, 1)
	assert.Equal(t, 1, res.SentCount)
	assert.Equal(t, 0, res.FailedCount)
	assert.Equal(t, model.ReachStatusSent, res.Users[0].Status)
	assert.Equal(t, model.ReachChannelEmail, res.Users[0].Channel)

	req := sent[0]
	assert.Equal(t, int64(10), req.UserID)
	assert.Equal(t, ConnectorUpgradeNoticeEventKey, req.EventKey)
	assert.Equal(t, "connector_upgrade:10:4.2.3", req.DedupKey)
	assert.Contains(t, req.LongText, "old-a1")
	assert.Contains(t, req.LongText, "`0.1.0`")
	assert.Contains(t, req.LongText, "npm install -g grix-connector@latest")
	assert.Contains(t, req.LongText, "grix-connector restart")
	assert.Contains(t, req.LongText, "**4.2.3**")
	assert.Contains(t, req.ShortText, "4.2.3")
}

func TestNotifyConnectorUpgradeSeenWindow(t *testing.T) {
	setupConnectorUpgradeNotifyTest(t)
	res, err := NotifyConnectorUpgrade(context.Background(), ConnectorUpgradeNotifyReq{
		BelowVersion: "3.34.1", TargetVersion: "4.2.3", SeenWithinDays: 90, DryRun: true,
	})
	require.NoError(t, err)
	ids := make([]int64, 0, len(res.Users))
	for _, u := range res.Users {
		ids = append(ids, u.UserID)
	}
	assert.Equal(t, []int64{10, 30}, ids, "90 天窗口应把 stale 的 owner 30 也纳入；disabled、无版本、非 grix-connector 客户端的仍排除")
}

func TestNotifyConnectorUpgradeRejectsBadVersion(t *testing.T) {
	setupConnectorUpgradeNotifyTest(t)
	_, err := NotifyConnectorUpgrade(context.Background(), ConnectorUpgradeNotifyReq{BelowVersion: "latest", TargetVersion: "4.2.3"})
	require.Error(t, err)
	_, err = NotifyConnectorUpgrade(context.Background(), ConnectorUpgradeNotifyReq{BelowVersion: "3.34.1", TargetVersion: ""})
	require.Error(t, err)
}

func TestRecordAgentConnectorVersion(t *testing.T) {
	setupConnectorUpgradeNotifyTest(t)
	RecordAgentConnectorVersion(1005, " grix-connector ", " 4.2.3 ")
	var a model.Agent
	require.NoError(t, store.DB.First(&a, 1005).Error)
	assert.Equal(t, ConnectorClientGrixConnector, a.ConnectorClient)
	assert.Equal(t, "4.2.3", a.ConnectorVersion)
	require.NotNil(t, a.ConnectorVersionSeenAt)
	assert.WithinDuration(t, time.Now(), *a.ConnectorVersionSeenAt, time.Minute)

	// 空版本不覆盖已有值
	RecordAgentConnectorVersion(1005, "hermes-agent", "")
	require.NoError(t, store.DB.First(&a, 1005).Error)
	assert.Equal(t, "4.2.3", a.ConnectorVersion)
	assert.False(t, strings.Contains(a.ConnectorVersion, " "))
}

package agentapi

// agent WS 连接安全（阶段0）守卫测试：
// 连接日志落库/回填、Redis 在线信息生命周期、异地标记、握手封禁拦截。

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/ipgeo"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	connSecAgentID = int64(94001)
	connSecOwnerID = int64(86001)
)

func newConnSecManager(t *testing.T) (*Manager, func()) {
	t.Helper()
	logger.Init()
	config.C.JWT.Secret = "test-jwt-secret-for-conn-security-012345"
	config.C.Server.AgentIPRuleHmacSecret = ""
	originalDB := store.DB
	originalRDB := store.RDB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	cleanup := func() {
		store.DB = originalDB
		store.RDB = originalRDB
		testDB.Close()
	}
	m := NewManager("", time.Second, nil, nil, nil, nil)
	m.SetNodeID("connsec-node-1")
	mgrCleanup := func() {
		m.Shutdown()
		cleanup()
	}
	return m, mgrCleanup
}

func connSecConn(agentID, ownerID int64, clientIP string) *agentConn {
	return &agentConn{
		agentID:     agentID,
		ownerID:     ownerID,
		isPrimary:   true,
		clientType:  "claude",
		clientIP:    clientIP,
		connectedAt: time.Now(),
		send:        make(chan []byte, 16),
	}
}

func TestRecordAndFinalizeConnection(t *testing.T) {
	m, cleanup := newConnSecManager(t)
	defer cleanup()

	conn := connSecConn(connSecAgentID, connSecOwnerID, "114.114.114.114")
	recordAgentConnection(m, conn)
	require.NotZero(t, conn.connLogID)

	var entry model.AgentConnectionLog
	require.NoError(t, store.DB.First(&entry, conn.connLogID).Error)
	assert.Equal(t, connSecAgentID, entry.AgentID)
	assert.Equal(t, connSecOwnerID, entry.OwnerID)
	assert.Equal(t, "114.114.114.114", entry.ClientIP)
	assert.Contains(t, entry.IPLocation, "中国")
	assert.Equal(t, "connsec-node-1", entry.NodeID)
	assert.False(t, entry.GeoChanged)
	assert.Nil(t, entry.DisconnectedAt)

	// Redis 在线信息随租约写入
	m.refreshConnInfo(conn, time.Minute)
	raw, err := store.RDB.Get(context.Background(), pkgagentapi.ConnInfoKey(connSecAgentID, connSecOwnerID)).Result()
	require.NoError(t, err)
	assert.Contains(t, raw, "114.114.114.114")

	// 断开回填 + 清理，且首个 reason 生效
	finalizeAgentConnection(conn, "kicked_by_owner")
	finalizeAgentConnection(conn, disconnectReasonClosed)
	require.NoError(t, store.DB.First(&entry, conn.connLogID).Error)
	require.NotNil(t, entry.DisconnectedAt)
	assert.Equal(t, "kicked_by_owner", entry.DisconnectReason)
	_, err = store.RDB.Get(context.Background(), pkgagentapi.ConnInfoKey(connSecAgentID, connSecOwnerID)).Result()
	assert.Error(t, err, "conninfo 应在断开后被清理")
}

func TestGeoChangedFlag(t *testing.T) {
	m, cleanup := newConnSecManager(t)
	defer cleanup()

	// 上一次连接在"另一个地方"
	prev := &model.AgentConnectionLog{
		ID:          snowflake.GenID(),
		AgentID:     connSecAgentID,
		OwnerID:     connSecOwnerID,
		ClientIP:    "8.8.8.8",
		IPLocation:  ipgeo.Lookup("8.8.8.8"),
		ConnectedAt: time.Now().Add(-time.Hour),
	}
	require.NotEmpty(t, prev.IPLocation)
	require.NoError(t, store.DB.Create(prev).Error)

	conn := connSecConn(connSecAgentID, connSecOwnerID, "114.114.114.114")
	recordAgentConnection(m, conn)
	var entry model.AgentConnectionLog
	require.NoError(t, store.DB.First(&entry, conn.connLogID).Error)
	assert.True(t, entry.GeoChanged, "换了归属地应打异地标记")

	// 同归属地再连一次：不打标记
	conn2 := connSecConn(connSecAgentID, connSecOwnerID, "114.114.114.114")
	recordAgentConnection(m, conn2)
	var entry2 model.AgentConnectionLog
	require.NoError(t, store.DB.First(&entry2, conn2.connLogID).Error)
	assert.False(t, entry2.GeoChanged)
}

func TestHandshakeBanCheck(t *testing.T) {
	_, cleanup := newConnSecManager(t)
	defer cleanup()

	// 未配置规则时不拦截
	assert.False(t, checkAgentIPBanned(connSecAgentID, "203.0.113.7"))

	rule := &model.AgentIPRule{
		ID:        snowflake.GenID(),
		AgentID:   connSecAgentID,
		RuleType:  model.AgentIPRuleTypeBan,
		IPCIDR:    "203.0.113.7",
		Signature: security.SignAgentIPRule(connSecAgentID, model.AgentIPRuleTypeBan, "203.0.113.7"),
	}
	require.NoError(t, store.DB.Create(rule).Error)

	assert.True(t, checkAgentIPBanned(connSecAgentID, "203.0.113.7"))
	assert.False(t, checkAgentIPBanned(connSecAgentID, "203.0.113.8"))
	// 其他 agent 不受影响
	assert.False(t, checkAgentIPBanned(connSecAgentID+1, "203.0.113.7"))
	// 空 IP（解析失败）不拦截
	assert.False(t, checkAgentIPBanned(connSecAgentID, ""))
}

func TestFinalizeDoesNotDeleteSuccessorConnInfo(t *testing.T) {
	m, cleanup := newConnSecManager(t)
	defer cleanup()

	// 顶号场景：旧连接断开回填时不得误删新连接刚写入的在线信息
	oldConn := connSecConn(connSecAgentID, connSecOwnerID, "114.114.114.114")
	recordAgentConnection(m, oldConn)
	m.refreshConnInfo(oldConn, time.Minute)

	newConn := connSecConn(connSecAgentID, connSecOwnerID, "114.114.114.114")
	recordAgentConnection(m, newConn)
	m.refreshConnInfo(newConn, time.Minute)

	finalizeAgentConnection(oldConn, "replaced_by_new_connection")
	raw, err := store.RDB.Get(context.Background(), pkgagentapi.ConnInfoKey(connSecAgentID, connSecOwnerID)).Result()
	require.NoError(t, err, "新连接的在线信息不应被旧连接清理误删")
	assert.Contains(t, raw, "log_id")
}

package agentapi

// 覆盖 agent_connection_logs 的三条断开回填路径,防止再退化成生产环境里
// 大量 disconnected_at 一直是 NULL 的脏数据(优雅关停漏写 / 顶号旧连接漏写 /
// 进程被强杀后启动对账漏兜底)。

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialAndAuthAgent 建一条真实的 agent WS 连接并完成鉴权,返回底层 ws 连接
// (调用方负责 Close)。用于需要经过完整 ServeWS 生命周期(而不是直接调用
// finalizeAgentConnection)的场景,比如验证 Manager.Shutdown 的收尾。
func dialAndAuthAgent(t *testing.T, wsURL string, agentID int64, apiKey string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id": strconv.FormatInt(agentID, 10),
			"api_key":  apiKey,
			"client":   "claude",
		}),
	})
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, authPayload))

	_, raw, err := conn.ReadMessage()
	require.NoError(t, err)
	var ack protocol.Packet
	require.NoError(t, json.Unmarshal(raw, &ack))
	require.Equal(t, "auth_ack", ack.Cmd)
	var payload AuthAckPayload
	require.NoError(t, json.Unmarshal(ack.Payload, &payload))
	require.Equal(t, 0, payload.Code, "auth should succeed: msg=%s", payload.Msg)
	return conn
}

// 场景一:优雅关停(Manager.Shutdown)必须把本节点持有的连接日志回填 disconnected_at。
func TestManagerShutdownFinalizesConnectionLog(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB, originalRDB := store.DB, store.RDB
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		store.DB = originalDB
		store.RDB = originalRDB
	})

	const (
		agentID = int64(94101)
		ownerID = int64(86101)
		apiKey  = "ak_test_shutdown_finalize"
	)
	agent := model.Agent{
		ID:           agentID,
		AgentName:    "shutdown-finalize-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	require.NoError(t, store.DB.Create(&agent).Error)

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	mgr.SetNodeID("shutdown-node")
	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/?agent_id=94101"
	conn := dialAndAuthAgent(t, wsURL, agentID, apiKey)
	defer conn.Close()

	var beforeShutdown model.AgentConnectionLog
	require.NoError(t, store.DB.Where("agent_id = ?", agentID).First(&beforeShutdown).Error)
	assert.Nil(t, beforeShutdown.DisconnectedAt, "连接建立时不应带断开时间")
	assert.Equal(t, "shutdown-node", beforeShutdown.NodeID)

	// 优雅关停:等价于滚动发布 / 进程正常退出前的收尾。
	mgr.Shutdown()

	var afterShutdown model.AgentConnectionLog
	require.NoError(t, store.DB.First(&afterShutdown, beforeShutdown.ID).Error)
	require.NotNil(t, afterShutdown.DisconnectedAt, "优雅关停必须回填本节点连接的 disconnected_at")
	assert.Equal(t, disconnectReasonClosed, afterShutdown.DisconnectReason)
}

// 场景二:同一 agent 新连接顶替旧连接时,旧连接的日志必须回填断开信息,
// 不能让旧记录永远停在"仍在线"。
func TestSupersedingConnectionFinalizesOldConnectionLog(t *testing.T) {
	m, cleanup := newConnSecManager(t)
	defer cleanup()

	oldConn := connSecConn(connSecAgentID, connSecOwnerID, "1.1.1.1")
	recordAgentConnection(m, oldConn)
	require.NotZero(t, oldConn.connLogID)
	require.True(t, m.attachConn(oldConn))

	newConn := connSecConn(connSecAgentID, connSecOwnerID, "2.2.2.2")
	recordAgentConnection(m, newConn)
	require.NotZero(t, newConn.connLogID)
	require.True(t, m.attachConn(newConn), "新连接顶号应当成功接管")

	var oldEntry model.AgentConnectionLog
	require.NoError(t, store.DB.First(&oldEntry, oldConn.connLogID).Error)
	require.NotNil(t, oldEntry.DisconnectedAt, "被顶替的旧连接必须回填断开时间")
	assert.Equal(t, "replaced_by_new_connection", oldEntry.DisconnectReason)

	var newEntry model.AgentConnectionLog
	require.NoError(t, store.DB.First(&newEntry, newConn.connLogID).Error)
	assert.Nil(t, newEntry.DisconnectedAt, "新连接自己的记录不应被误关")
}

// 场景三:启动对账只关闭本节点的存量脏记录,不动别的节点、不动本次启动之后
// 才建立的新连接,也不覆盖已经正常回填过的记录。
func TestReconcileStaleConnectionLogsOnStartup(t *testing.T) {
	m, cleanup := newConnSecManager(t) // node_id = "connsec-node-1"
	defer cleanup()

	staleOwnNode := model.AgentConnectionLog{
		ID:          snowflake.GenID(),
		AgentID:     connSecAgentID,
		OwnerID:     connSecOwnerID,
		NodeID:      "connsec-node-1",
		ConnectedAt: time.Now().Add(-2 * time.Hour),
	}
	require.NoError(t, store.DB.Create(&staleOwnNode).Error)

	futureOwnNode := model.AgentConnectionLog{
		ID:          snowflake.GenID(),
		AgentID:     connSecAgentID,
		OwnerID:     connSecOwnerID,
		NodeID:      "connsec-node-1",
		ConnectedAt: time.Now().Add(2 * time.Hour),
	}
	require.NoError(t, store.DB.Create(&futureOwnNode).Error)

	staleOtherNode := model.AgentConnectionLog{
		ID:          snowflake.GenID(),
		AgentID:     connSecAgentID,
		OwnerID:     connSecOwnerID,
		NodeID:      "some-other-node",
		ConnectedAt: time.Now().Add(-2 * time.Hour),
	}
	require.NoError(t, store.DB.Create(&staleOtherNode).Error)

	closedAt := time.Now().Add(-time.Hour)
	alreadyClosed := model.AgentConnectionLog{
		ID:               snowflake.GenID(),
		AgentID:          connSecAgentID,
		OwnerID:          connSecOwnerID,
		NodeID:           "connsec-node-1",
		ConnectedAt:      time.Now().Add(-3 * time.Hour),
		DisconnectedAt:   &closedAt,
		DisconnectReason: "closed",
	}
	require.NoError(t, store.DB.Create(&alreadyClosed).Error)

	m.ReconcileStaleConnectionLogsOnStartup()

	var gotStale model.AgentConnectionLog
	require.NoError(t, store.DB.First(&gotStale, staleOwnNode.ID).Error)
	require.NotNil(t, gotStale.DisconnectedAt, "本节点的存量脏记录必须被关闭")
	assert.Equal(t, "startup_reconcile", gotStale.DisconnectReason)

	var gotFuture model.AgentConnectionLog
	require.NoError(t, store.DB.First(&gotFuture, futureOwnNode.ID).Error)
	assert.Nil(t, gotFuture.DisconnectedAt, "本次启动之后才建立的连接不应被对账误关")

	var gotOtherNode model.AgentConnectionLog
	require.NoError(t, store.DB.First(&gotOtherNode, staleOtherNode.ID).Error)
	assert.Nil(t, gotOtherNode.DisconnectedAt, "绝不能动别的节点的记录,防止滚动发布误关真实在线连接")

	var gotClosed model.AgentConnectionLog
	require.NoError(t, store.DB.First(&gotClosed, alreadyClosed.ID).Error)
	require.NotNil(t, gotClosed.DisconnectedAt)
	assert.Equal(t, "closed", gotClosed.DisconnectReason, "已经正常回填过的记录不应被覆盖")
	assert.WithinDuration(t, closedAt, *gotClosed.DisconnectedAt, time.Second)
}

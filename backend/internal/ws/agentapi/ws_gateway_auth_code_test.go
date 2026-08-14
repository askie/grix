package agentapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// swapAuthFailLimiter 用全新限流器替换全局实例，避免测试间状态串扰。
func swapAuthFailLimiter(t *testing.T, l *authFailLimiter) {
	t.Helper()
	orig := agentAuthFailLimiter
	agentAuthFailLimiter = l
	t.Cleanup(func() { agentAuthFailLimiter = orig })
}

func setupAuthCodeTest(t *testing.T) *Manager {
	t.Helper()
	testDB := testutil.NewTestDB()
	origDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = origDB
		testDB.Close()
	})
	swapAuthFailLimiter(t, newAuthFailLimiter(authFailWindow, authFailThreshold, authFailBlockDuration))
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	t.Cleanup(mgr.Shutdown)
	return mgr
}

// dialAndAuth 建连并发送 auth 包，返回 auth_ack 的 payload；服务端未回包直接断开时返回 nil。
func dialAndAuth(t *testing.T, srvURL string, dialQuery string, authFields map[string]any) *AuthAckPayload {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srvURL, "http")+dialQuery, nil)
	require.NoError(t, err, "dial websocket")
	defer conn.Close()

	raw, err := json.Marshal(protocol.Packet{
		Cmd:     "auth",
		Seq:     1,
		Payload: mustMarshalRawJSON(t, authFields),
	})
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, raw))

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, ackRaw, err := conn.ReadMessage()
	if err != nil {
		return nil
	}
	var ack protocol.Packet
	require.NoError(t, json.Unmarshal(ackRaw, &ack))
	require.Equal(t, "auth_ack", ack.Cmd)
	var payload AuthAckPayload
	require.NoError(t, json.Unmarshal(ack.Payload, &payload))
	return &payload
}

func createAuthCodeAgent(t *testing.T, agentID, ownerID int64, status int16, apiKey string) {
	t.Helper()
	require.NoError(t, store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "auth-code-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       status,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}).Error)
}

func TestServeWS_DeletedAgentAuthAck10008(t *testing.T) {
	mgr := setupAuthCodeTest(t)
	const agentID, apiKey = int64(96001), "ak_test_deleted_agent"
	createAuthCodeAgent(t, agentID, 86001, model.AgentStatusDeleted, apiKey)

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	ack := dialAndAuth(t, srv.URL, "", map[string]any{
		"agent_id":    "96001",
		"api_key":     apiKey,
		"client":      "openclaw-grix",
		"client_type": model.AgentClientTypeOpenClaw,
	})
	require.NotNil(t, ack)
	assert.Equal(t, 10008, ack.Code)
	assert.Equal(t, "agent deleted", ack.Msg)
}

func TestServeWS_MissingAgentAuthAck10008(t *testing.T) {
	mgr := setupAuthCodeTest(t)
	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	ack := dialAndAuth(t, srv.URL, "", map[string]any{
		"agent_id":    "96002",
		"api_key":     "ak_whatever",
		"client":      "openclaw-grix",
		"client_type": model.AgentClientTypeOpenClaw,
	})
	require.NotNil(t, ack)
	assert.Equal(t, 10008, ack.Code)
	assert.Equal(t, "agent deleted", ack.Msg)
}

func TestServeWS_DisabledAgentAuthAck10001(t *testing.T) {
	mgr := setupAuthCodeTest(t)
	const agentID, apiKey = int64(96003), "ak_test_disabled_agent"
	createAuthCodeAgent(t, agentID, 86003, model.AgentStatusDisabled, apiKey)

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	ack := dialAndAuth(t, srv.URL, "", map[string]any{
		"agent_id":    "96003",
		"api_key":     apiKey,
		"client":      "openclaw-grix",
		"client_type": model.AgentClientTypeOpenClaw,
	})
	require.NotNil(t, ack)
	assert.Equal(t, 10001, ack.Code)
	assert.Equal(t, "auth failed", ack.Msg)
}

func TestServeWS_WrongAPIKeyAuthAck10001(t *testing.T) {
	mgr := setupAuthCodeTest(t)
	const agentID, apiKey = int64(96004), "ak_test_active_agent"
	createAuthCodeAgent(t, agentID, 86004, model.AgentStatusActive, apiKey)

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	ack := dialAndAuth(t, srv.URL, "", map[string]any{
		"agent_id":    "96004",
		"api_key":     "ak_wrong_key",
		"client":      "openclaw-grix",
		"client_type": model.AgentClientTypeOpenClaw,
	})
	require.NotNil(t, ack)
	assert.Equal(t, 10001, ack.Code)
	assert.Equal(t, "auth failed", ack.Msg)
}

// 反复认证失败触发限流后：带 agent_id query 的握手直接被 429 拒绝（不升级）。
func TestServeWS_AuthFailLimiterRejectsHandshakeWithQuery(t *testing.T) {
	mgr := setupAuthCodeTest(t)
	// 阈值 0：一次失败即封锁，避免测试真的打 31 次
	swapAuthFailLimiter(t, newAuthFailLimiter(authFailWindow, 0, authFailBlockDuration))

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	// 第一次认证失败（agent 不存在 → 10008），同时计入限流并触发封锁
	ack := dialAndAuth(t, srv.URL, "/?agent_id=96005", map[string]any{
		"agent_id":    "96005",
		"api_key":     "ak_whatever",
		"client":      "openclaw-grix",
		"client_type": model.AgentClientTypeOpenClaw,
	})
	require.NotNil(t, ack)
	require.Equal(t, 10008, ack.Code)

	// 封锁期内再次握手：upgrade 前即被拒，HTTP 429
	_, resp, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=96005", nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
}

// 认证错误分类：已删除→errAgentDeleted；密钥错→普通错误；DB 故障→errAuthInternal（不计限流）。
func TestAuthenticateAgentErrorClassification(t *testing.T) {
	mgr := setupAuthCodeTest(t)
	const apiKey = "ak_test_classify"
	createAuthCodeAgent(t, 96007, 86007, model.AgentStatusDeleted, apiKey)
	createAuthCodeAgent(t, 96008, 86008, model.AgentStatusActive, apiKey)

	// 已删除 → errAgentDeleted，且不是内部错误
	_, _, err := mgr.authenticateAgent(96007, apiKey, 0)
	require.ErrorIs(t, err, errAgentDeleted)
	require.NotErrorIs(t, err, errAuthInternal)

	// 密钥错 → 普通认证失败，两个 sentinel 都不命中（计入限流）
	_, _, err = mgr.authenticateAgent(96008, "ak_wrong", 0)
	require.Error(t, err)
	require.NotErrorIs(t, err, errAgentDeleted)
	require.NotErrorIs(t, err, errAuthInternal)

	// DB 故障（连接关闭）→ errAuthInternal，不误判为已删除
	sqlDB, dbErr := store.DB.DB()
	require.NoError(t, dbErr)
	require.NoError(t, sqlDB.Close())
	_, _, err = mgr.authenticateAgent(96008, apiKey, 0)
	require.ErrorIs(t, err, errAuthInternal)
	require.NotErrorIs(t, err, errAgentDeleted)
}

// 服务端内部错误（DB 故障）不计入限流：阈值 0 下连续两次失败，第二次仍能正常拿到
// 10001 而非在 upgrade 前被 429 拒绝（若被误计数，第一次就会触发封锁）。
func TestServeWS_InternalErrorNotCounted(t *testing.T) {
	mgr := setupAuthCodeTest(t)
	swapAuthFailLimiter(t, newAuthFailLimiter(authFailWindow, 0, authFailBlockDuration))
	createAuthCodeAgent(t, 96009, 86009, model.AgentStatusActive, "ak_x")

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	sqlDB, err := store.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	for i := 0; i < 2; i++ {
		ack := dialAndAuth(t, srv.URL, "/?agent_id=96009", map[string]any{
			"agent_id": "96009", "api_key": "ak_x",
			"client": "openclaw-grix", "client_type": model.AgentClientTypeOpenClaw,
		})
		require.NotNil(t, ack, "第 %d 次连接不应被限流拒绝", i+1)
		assert.Equal(t, 10001, ack.Code, "第 %d 次应返回普通认证失败", i+1)
	}
}

// agent_id 只在 auth payload、不带 URL query 时，封锁期内认证前直接断开、不回 auth_ack。
func TestServeWS_AuthFailLimiterDropsPayloadOnlyConn(t *testing.T) {
	mgr := setupAuthCodeTest(t)
	swapAuthFailLimiter(t, newAuthFailLimiter(authFailWindow, 0, authFailBlockDuration))

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	// 触发封锁
	ack := dialAndAuth(t, srv.URL, "", map[string]any{
		"agent_id":    "96006",
		"api_key":     "ak_whatever",
		"client":      "openclaw-grix",
		"client_type": model.AgentClientTypeOpenClaw,
	})
	require.NotNil(t, ack)
	require.Equal(t, 10008, ack.Code)

	// 封锁期内重连（无 query）：服务端直接断开，客户端读不到 auth_ack
	ack = dialAndAuth(t, srv.URL, "", map[string]any{
		"agent_id":    "96006",
		"api_key":     "ak_whatever",
		"client":      "openclaw-grix",
		"client_type": model.AgentClientTypeOpenClaw,
	})
	assert.Nil(t, ack, "封锁期内应直接断开而非返回 auth_ack")
}

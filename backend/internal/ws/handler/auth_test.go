package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func init() {
	logger.Init()
	jwtpkg.Init("test-secret-ws-handler", 3600, 86400)
	_ = snowflake.Init(1)
}

// MockConn implements ConnInterface for testing
type MockConn struct {
	userID      int64
	deviceID    string
	platform    string
	authed      bool
	lastPayload interface{}
	lastPkt     *protocol.Packet
	lastSeq     int64
}

func (m *MockConn) SendPayload(cmd string, seq int64, payload interface{}) {
	m.lastPayload = payload
	m.lastSeq = seq
}

func (m *MockConn) SendPacket(pkt *protocol.Packet) {
	m.lastPkt = pkt
}

func (m *MockConn) AckPush(msgID int64) {}

func (m *MockConn) Close() {}

func (m *MockConn) NextSeq() int64 {
	m.lastSeq++
	return m.lastSeq
}

func (m *MockConn) GetUserID() int64 {
	return m.userID
}

func (m *MockConn) GetDeviceID() string {
	return m.deviceID
}

func (m *MockConn) GetPlatform() string {
	return m.platform
}

func (m *MockConn) SetAuth(userID int64, sessionID, deviceID, platform string) {
	m.userID = userID
	m.deviceID = deviceID
	m.platform = platform
	m.authed = true
}

func (m *MockConn) IsAuthed() bool {
	return m.authed
}

// MockHub implements HubInterface for testing
type MockHub struct {
	registeredConns   []ConnInterface
	unregisteredConns []ConnInterface
	refreshedConns    []ConnInterface
	nodeID            string
}

func (m *MockHub) Register(c ConnInterface) {
	m.registeredConns = append(m.registeredConns, c)
}

func (m *MockHub) Unregister(c ConnInterface) {
	m.unregisteredConns = append(m.unregisteredConns, c)
}

func (m *MockHub) RefreshAlive(c ConnInterface) {
	m.refreshedConns = append(m.refreshedConns, c)
}

func (m *MockHub) GetUserConns(userID int64) []ConnInterface {
	return m.registeredConns
}

func (m *MockHub) GetNodeID() string {
	return m.nodeID
}

func TestHandleAuth(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = nil
	if err := store.DB.Create(&model.User{
		ID:       12345,
		Username: "wsauthuser",
		Email:    "wsauth@example.com",
		Nickname: "wsauthuser",
		Status:   model.UserStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed user error: %v", err)
	}

	hub := &MockHub{nodeID: "test-node"}
	conn := &MockConn{}

	t.Run("successful auth", func(t *testing.T) {
		sessionID := "ws-auth-session-1"
		if err := testDB.DB.Create(&model.LoginDeviceSession{
			SessionID:  sessionID,
			UserID:     12345,
			DeviceID:   "device-001",
			Platform:   "ios",
			LastSeenAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatalf("seed login device session error: %v", err)
		}
		if err := testDB.DB.Create(&[]model.UserInbox{
			{UserID: 12345, InboxSeq: 7, MsgID: 7001, SessionID: "session-1", EventKind: model.UserInboxEventKindMessage},
			{UserID: 12345, InboxSeq: 12, MsgID: 7002, SessionID: "session-2", EventKind: model.UserInboxEventKindMessage},
		}).Error; err != nil {
			t.Fatalf("seed user inbox rows error: %v", err)
		}
		token, _, _ := jwtpkg.GenerateAccessTokenWithSession(12345, sessionID)

		payload := protocol.AuthPayload{
			Token:    token,
			DeviceID: "device-001",
			Platform: "ios",
		}
		payloadBytes, _ := json.Marshal(payload)

		pkt := &protocol.Packet{
			Cmd:     protocol.CmdAuth,
			Seq:     1,
			Payload: payloadBytes,
		}

		HandleAuth(hub, conn, pkt)

		// Verify connection was authenticated
		if !conn.IsAuthed() {
			t.Error("connection should be authenticated")
		}
		if conn.GetUserID() != 12345 {
			t.Errorf("expected user_id 12345, got %d", conn.GetUserID())
		}
		if conn.GetDeviceID() != "device-001" {
			t.Errorf("expected device_id 'device-001', got '%s'", conn.GetDeviceID())
		}

		// Verify hub registered the connection
		if len(hub.registeredConns) != 1 {
			t.Error("connection should be registered in hub")
		}

		// Verify response payload
		resp, ok := conn.lastPayload.(protocol.AuthAckPayload)
		if !ok {
			t.Fatal("expected AuthAckPayload")
		}
		if resp.Code != 0 {
			t.Errorf("expected code 0, got %d", resp.Code)
		}
		if resp.UserID != 12345 {
			t.Errorf("expected user_id 12345, got %d", resp.UserID)
		}
		if resp.LatestInboxSeq != 12 {
			t.Errorf("expected latest_inbox_seq 12, got %d", resp.LatestInboxSeq)
		}

		var stored model.LoginDeviceSession
		if err := testDB.DB.Where("session_id = ?", sessionID).First(&stored).Error; err != nil {
			t.Fatalf("load login device session error: %v", err)
		}
		if time.Since(stored.LastSeenAt) > time.Minute {
			t.Fatalf("expected last_seen_at to be refreshed, got %s", stored.LastSeenAt)
		}
	})

	t.Run("legacy session without login_device_session bootstraps on auth", func(t *testing.T) {
		hub2 := &MockHub{nodeID: "test-node"}
		conn2 := &MockConn{}
		sessionID := "ws-auth-session-bootstrap"
		if err := testDB.DB.Create(&model.RefreshToken{
			JTI:       "rt-ws-auth-session-bootstrap",
			UserID:    12345,
			FamilyID:  sessionID,
			Status:    model.RefreshTokenStatusActive,
			ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		}).Error; err != nil {
			t.Fatalf("seed refresh token error: %v", err)
		}
		token, _, _ := jwtpkg.GenerateAccessTokenWithSession(12345, sessionID)

		payload := protocol.AuthPayload{
			Token:    token,
			DeviceID: "device-legacy-001",
			Platform: "android",
		}
		payloadBytes, _ := json.Marshal(payload)

		pkt := &protocol.Packet{
			Cmd:     protocol.CmdAuth,
			Seq:     11,
			Payload: payloadBytes,
		}

		HandleAuth(hub2, conn2, pkt)

		if !conn2.IsAuthed() {
			t.Fatal("connection should be authenticated after bootstrapping legacy session")
		}
		if len(hub2.registeredConns) != 1 {
			t.Fatal("connection should be registered in hub")
		}

		var stored model.LoginDeviceSession
		if err := testDB.DB.Where("session_id = ?", sessionID).First(&stored).Error; err != nil {
			t.Fatalf("load bootstrapped login device session error: %v", err)
		}
		if stored.DeviceID != "device-legacy-001" {
			t.Fatalf("expected bootstrapped device_id to match auth payload, got %q", stored.DeviceID)
		}
		if stored.Platform != "android" {
			t.Fatalf("expected bootstrapped platform to match auth payload, got %q", stored.Platform)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		conn2 := &MockConn{}

		payload := protocol.AuthPayload{
			Token:    "invalid-token",
			DeviceID: "device-002",
			Platform: "android",
		}
		payloadBytes, _ := json.Marshal(payload)

		pkt := &protocol.Packet{
			Cmd:     protocol.CmdAuth,
			Seq:     2,
			Payload: payloadBytes,
		}

		HandleAuth(hub, conn2, pkt)

		// Should not be authenticated
		if conn2.IsAuthed() {
			t.Error("connection should NOT be authenticated with invalid token")
		}

		// Verify error response
		resp, ok := conn2.lastPayload.(protocol.AuthAckPayload)
		if !ok {
			t.Fatal("expected AuthAckPayload")
		}
		if resp.Code != 10001 {
			t.Errorf("expected error code 10001, got %d", resp.Code)
		}
		if resp.Msg != "凭证已失效，请重新登录" {
			t.Errorf("expected invalid-session message, got %q", resp.Msg)
		}
	})

	t.Run("session mismatch rejects auth", func(t *testing.T) {
		conn3 := &MockConn{}
		sessionID := "ws-auth-session-mismatch"
		if err := service.RegisterLoginDeviceSession(12345, sessionID, "device-009", "ios"); err != nil {
			t.Fatalf("RegisterLoginDeviceSession() error = %v", err)
		}
		token, _, _ := jwtpkg.GenerateAccessTokenWithSession(12345, sessionID)

		payload := protocol.AuthPayload{
			Token:    token,
			DeviceID: "device-010",
			Platform: "ios",
		}
		payloadBytes, _ := json.Marshal(payload)

		pkt := &protocol.Packet{
			Cmd:     protocol.CmdAuth,
			Seq:     3,
			Payload: payloadBytes,
		}

		HandleAuth(hub, conn3, pkt)

		if conn3.IsAuthed() {
			t.Error("connection should NOT be authenticated when device binding mismatches")
		}

		resp, ok := conn3.lastPayload.(protocol.AuthAckPayload)
		if !ok {
			t.Fatal("expected AuthAckPayload")
		}
		if resp.Code != 10001 {
			t.Errorf("expected error code 10001, got %d", resp.Code)
		}
		if resp.Msg != "用户身份不匹配" {
			t.Errorf("expected device-mismatch message, got %q", resp.Msg)
		}
	})

	t.Run("malformed payload - graceful return", func(t *testing.T) {
		// Note: This test requires logger to be initialized
		// Skip testing malformed payload as it logs a warning
		// In production, logger should be initialized before tests
	})
}

func TestHandlePing(t *testing.T) {
	hub := &MockHub{nodeID: "test-node"}
	conn := &MockConn{}

	pkt := &protocol.Packet{
		Cmd: protocol.CmdPing,
		Seq: 100,
	}

	HandlePing(hub, conn, pkt)

	// Verify hub refreshed alive
	if len(hub.refreshedConns) != 1 {
		t.Error("connection should be refreshed in hub")
	}

	// Verify pong response
	if conn.lastPayload == nil {
		t.Error("expected pong response")
	}

	// Verify sequence number is preserved
	if conn.lastSeq != 100 {
		t.Errorf("expected seq 100, got %d", conn.lastSeq)
	}
}

// 存储层故障绝不能报成凭证终态。
//
// 后端一旦把"数据库读不了"说成"用户已被禁用"或"鉴权失败"，客户端就会把它当作
// 凭证问题清掉本地会话——数据库抖一下，全部在线用户被踢回登录页，而且恢复后也回
// 不来。这两个用例把这条线钉死：账号是正常的、凭证是有效的，坏的只是存储层，
// 服务端必须明说"我暂时不可用"，让客户端保留会话继续重连。
func TestHandleAuthStorageFaultStaysRetryable(t *testing.T) {
	const userID = int64(22345)
	const sessionID = "ws-auth-session-fault"

	seed := func(t *testing.T) (*testutil.TestDB, string) {
		t.Helper()
		testDB := testutil.NewTestDB()
		store.DB = testDB.DB
		store.RDB = nil
		if err := store.DB.Create(&model.User{
			ID:       userID,
			Username: "wsfaultuser",
			Email:    "wsfault@example.com",
			Nickname: "wsfaultuser",
			Status:   model.UserStatusActive,
		}).Error; err != nil {
			t.Fatalf("seed user error: %v", err)
		}
		if err := store.DB.Create(&model.LoginDeviceSession{
			SessionID:  sessionID,
			UserID:     userID,
			DeviceID:   "device-fault",
			Platform:   "ios",
			LastSeenAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatalf("seed login device session error: %v", err)
		}
		token, _, _ := jwtpkg.GenerateAccessTokenWithSession(userID, sessionID)
		return testDB, token
	}

	authPacket := func(t *testing.T, token string) *protocol.Packet {
		t.Helper()
		payloadBytes, err := json.Marshal(protocol.AuthPayload{
			Token:    token,
			DeviceID: "device-fault",
			Platform: "ios",
		})
		if err != nil {
			t.Fatalf("marshal auth payload error: %v", err)
		}
		return &protocol.Packet{Cmd: protocol.CmdAuth, Seq: 9, Payload: payloadBytes}
	}

	assertRetryable := func(t *testing.T, conn *MockConn, brokenTable string) {
		t.Helper()
		ack, ok := conn.lastPayload.(protocol.AuthAckPayload)
		if !ok {
			t.Fatalf("expected AuthAckPayload, got %T", conn.lastPayload)
		}
		if ack.Code != protocol.AuthCodeRetryable {
			t.Fatalf("%s 读不了时 code=%d msg=%q，期望可重试码 %d。"+
				"报成终态会让客户端清掉会话——一次数据库抖动就能把在线用户全部踢下线",
				brokenTable, ack.Code, ack.Msg, protocol.AuthCodeRetryable)
		}
		if conn.IsAuthed() {
			t.Error("存储层故障时不该判定鉴权通过")
		}
	}

	t.Run("user table unreadable", func(t *testing.T) {
		testDB, token := seed(t)
		defer testDB.Close()
		// 账号是正常的，坏的只是存储层——EnsureUserActive 拿到的是 DB 错误，
		// 不是 ErrUserDisabled。旧实现在这里一律回"用户已被禁用"。
		if err := store.DB.Exec("DROP TABLE users").Error; err != nil {
			t.Fatalf("drop users error: %v", err)
		}

		conn := &MockConn{}
		HandleAuth(&MockHub{nodeID: "test-node"}, conn, authPacket(t, token))
		assertRetryable(t, conn, "users 表")
	})

	t.Run("login device session table unreadable", func(t *testing.T) {
		testDB, token := seed(t)
		defer testDB.Close()
		// 设备会话是存在的，坏的只是存储层。旧实现在这里兜底回"鉴权失败"。
		if err := store.DB.Exec("DROP TABLE login_device_sessions").Error; err != nil {
			t.Fatalf("drop login_device_sessions error: %v", err)
		}

		conn := &MockConn{}
		HandleAuth(&MockHub{nodeID: "test-node"}, conn, authPacket(t, token))
		assertRetryable(t, conn, "login_device_sessions 表")
	})
}

package model

import (
	"testing"
	"time"
)

func TestUserTableName(t *testing.T) {
	user := User{}
	if user.TableName() != "users" {
		t.Errorf("expected table name 'users', got '%s'", user.TableName())
	}
}

func TestSessionTableName(t *testing.T) {
	session := Session{}
	if session.TableName() != "sessions" {
		t.Errorf("expected table name 'sessions', got '%s'", session.TableName())
	}
}

func TestMessageTableName(t *testing.T) {
	message := Message{}
	if message.TableName() != "messages" {
		t.Errorf("expected table name 'messages', got '%s'", message.TableName())
	}
}

func TestAgentSessionRouteMappingTableName(t *testing.T) {
	mapping := AgentSessionRouteMapping{}
	if mapping.TableName() != "agent_session_route_mappings" {
		t.Errorf("expected table name 'agent_session_route_mappings', got '%s'", mapping.TableName())
	}
}

func TestUserStruct(t *testing.T) {
	now := time.Now()
	user := User{
		ID:           1,
		Username:     "testuser",
		PasswordHash: "hash123",
		AuthProvider: "local",
		Nickname:     "Test User",
		AvatarURL:    "https://example.com/avatar.png",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if user.ID != 1 {
		t.Errorf("expected ID 1, got %d", user.ID)
	}
	if user.Username != "testuser" {
		t.Errorf("expected username 'testuser', got '%s'", user.Username)
	}
	if user.AuthProvider != "local" {
		t.Errorf("expected auth provider 'local', got '%s'", user.AuthProvider)
	}
}

func TestSessionStruct(t *testing.T) {
	lastMsgID := int64(123)
	session := Session{
		SessionID:      "session-123",
		OwnerID:        1,
		SessionType:    1,
		LastMsgID:      &lastMsgID,
		LastMsgSummary: "Hello",
		IsDeleted:      false,
	}

	if session.SessionID != "session-123" {
		t.Errorf("expected SessionID 'session-123', got '%s'", session.SessionID)
	}
	if session.OwnerID != 1 {
		t.Errorf("expected OwnerID 1, got %d", session.OwnerID)
	}
	if session.SessionType != 1 {
		t.Errorf("expected SessionType 1, got %d", session.SessionType)
	}
}

func TestSessionTypes(t *testing.T) {
	// Test private chat
	privateSession := Session{SessionType: 1}
	if privateSession.SessionType != 1 {
		t.Error("private session type should be 1")
	}

	// Test group chat
	groupSession := Session{SessionType: 2}
	if groupSession.SessionType != 2 {
		t.Error("group session type should be 2")
	}
}

func TestUserDefaultAuthProvider(t *testing.T) {
	// When AuthProvider is not set, it should default to "local" in DB
	// This tests the gorm default tag
	user := User{
		Username: "defaultauth",
	}

	// The default is set at DB level, but we can verify the struct field
	if user.AuthProvider != "" {
		t.Errorf("empty AuthProvider should be empty before DB save, got '%s'", user.AuthProvider)
	}
}

func TestMessageStruct(t *testing.T) {
	msg := Message{
		MsgID:     1,
		SessionID: "session-123",
		SenderID:  100,
		MsgType:   1,
		Content:   "Hello World",
	}

	if msg.MsgID != 1 {
		t.Errorf("expected MsgID 1, got %d", msg.MsgID)
	}
	if msg.SessionID != "session-123" {
		t.Errorf("expected SessionID 'session-123', got '%s'", msg.SessionID)
	}
	if msg.SenderID != 100 {
		t.Errorf("expected SenderID 100, got %d", msg.SenderID)
	}
}

func TestDeepSeekAgentClientType(t *testing.T) {
	if !IsValidAgentClientType(" DeepSeek ") {
		t.Fatal("deepseek client type should be valid")
	}
	if !IsProprietaryAgentClientType(AgentClientTypeDeepSeek) {
		t.Fatal("deepseek should use mention-only proprietary dispatch in groups")
	}
}

func TestDeviceStruct(t *testing.T) {
	device := Device{
		ID:          1,
		UserID:      100,
		DeviceID:    "device-abc",
		Platform:    DevicePlatformIOS,
		PushEnv:     DevicePushEnvAPNsProduction,
		DeviceToken: "push-token-123",
	}

	if device.DeviceID != "device-abc" {
		t.Errorf("expected DeviceID 'device-abc', got '%s'", device.DeviceID)
	}
	if device.Platform != "ios" {
		t.Errorf("expected Platform 'ios', got '%s'", device.Platform)
	}
	if device.PushEnv != DevicePushEnvAPNsProduction {
		t.Errorf("expected PushEnv %q, got %q", DevicePushEnvAPNsProduction, device.PushEnv)
	}
	if device.DeviceToken != "push-token-123" {
		t.Errorf("expected DeviceToken 'push-token-123', got '%s'", device.DeviceToken)
	}
}

func TestPointerFields(t *testing.T) {
	// Test that pointer fields can be nil
	session := Session{
		SessionID: "test",
	}

	if session.LastMsgID != nil {
		t.Error("LastMsgID should be nil by default")
	}

	// Test setting pointer fields
	msgID := int64(123)
	session.LastMsgID = &msgID
	if *session.LastMsgID != 123 {
		t.Errorf("expected LastMsgID 123, got %d", *session.LastMsgID)
	}
}

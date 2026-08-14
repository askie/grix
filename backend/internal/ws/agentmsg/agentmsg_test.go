package agentmsg

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
)

func setupAgentMsgTest(t *testing.T) func() {
	t.Helper()

	previousDB := store.DB
	previousRDB := store.RDB

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	logger.Init()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake init error: %v", err)
	}

	return func() {
		if store.RDB != nil {
			_ = store.RDB.Close()
		}
		testDB.Close()
		store.DB = previousDB
		store.RDB = previousRDB
	}
}

func createGroupSession(t *testing.T, sessionID string, ownerID int64, opts ...func(*model.Session)) {
	t.Helper()

	session := &model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: model.SessionTypeGroup,
	}
	for _, opt := range opts {
		opt(session)
	}
	if err := store.DB.Create(session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
}

func createSessionMember(t *testing.T, member model.SessionMember) {
	t.Helper()

	if err := store.DB.Create(&member).Error; err != nil {
		t.Fatalf("create session member error session=%s member=%d type=%d: %v", member.SessionID, member.MemberID, member.MemberType, err)
	}
}

func seedRoute(t *testing.T, userID int64, deviceNodePairs map[string]string) {
	t.Helper()

	ctx := context.Background()
	key := fmt.Sprintf("im:ws:route:%d", userID)
	for deviceID, nodeID := range deviceNodePairs {
		if err := store.RDB.HSet(ctx, key, deviceID, nodeID).Err(); err != nil {
			t.Fatalf("seed route user=%d device=%s node=%s error: %v", userID, deviceID, nodeID, err)
		}
	}
}

func subscribeChannel(t *testing.T, channel string) *redis.PubSub {
	t.Helper()

	ctx := context.Background()
	sub := store.RDB.Subscribe(ctx, channel)
	_, _ = sub.ReceiveTimeout(ctx, 200*time.Millisecond)
	return sub
}

func readEnvelopeMessage(t *testing.T, sub *redis.PubSub) map[string]any {
	t.Helper()

	msg, err := sub.ReceiveMessage(context.Background())
	if err != nil {
		t.Fatalf("receive pubsub message error: %v", err)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
		t.Fatalf("unmarshal pubsub message error: %v", err)
	}
	return envelope
}

package agentapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func init() {
	_ = snowflake.Init(7)
}

func TestPublishConnectorUpgradePush_PublishesToBroadcastChannel(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previous
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pubsub := store.RDB.Subscribe(ctx, BroadcastChannel())
	defer pubsub.Close()
	// 等待订阅确认，避免 Publish 早于订阅就绪导致消息丢失（CI 慢机器竞态）。
	if _, err := pubsub.Receive(ctx); err != nil {
		t.Fatalf("subscribe confirm failed: %v", err)
	}

	if _, err := PublishConnectorUpgradePush(); err != nil {
		t.Fatalf("PublishConnectorUpgradePush errored with mock redis: %v", err)
	}

	select {
	case msg := <-pubsub.Channel():
		var envelope struct {
			Cmd     string          `json:"cmd"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
			t.Fatalf("unmarshal envelope: %v", err)
		}
		if envelope.Cmd != redisCmdBroadcastConnectorUpgradePush {
			t.Fatalf("cmd=%s want=%s", envelope.Cmd, redisCmdBroadcastConnectorUpgradePush)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for broadcast publish")
	}
}

func TestPublishConnectorUpgradePush_ReturnsErrorWhenNoRedis(t *testing.T) {
	previous := store.RDB
	store.RDB = nil
	defer func() { store.RDB = previous }()

	if _, err := PublishConnectorUpgradePush(); err == nil {
		t.Fatal("expected error when redis is nil")
	}
}

func TestHandleRedisDispatch_RecognizesBroadcastConnectorUpgradePush(t *testing.T) {
	// 不依赖 globalManager - HandleRedisDispatch 收到 broadcast cmd 时
	// 用 goroutine 异步执行 handler；这里只验证 dispatch 把 cmd 识别为已处理。
	if !HandleRedisDispatch(redisCmdBroadcastConnectorUpgradePush, json.RawMessage(`{}`)) {
		t.Fatal("expected HandleRedisDispatch to handle broadcast connector_upgrade_push cmd")
	}
}

package provisioning

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestPublishConfigureGatewayProvider_PublishesToBroadcastChannel(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previous
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pubsub := store.RDB.Subscribe(ctx, BroadcastChannel)
	defer pubsub.Close()
	if _, err := pubsub.Receive(ctx); err != nil {
		t.Fatalf("subscribe confirm failed: %v", err)
	}

	cfg := GatewayProviderConfig{
		AgentID:       12345,
		APIKey:        "gwk_live_xxx",
		OpenAIBaseURL: "https://grix.dhf.pub/openai",
		Model:         "deepseek-chat",
	}
	if err := PublishConfigureGatewayProvider(cfg); err != nil {
		t.Fatalf("PublishConfigureGatewayProvider errored with mock redis: %v", err)
	}

	select {
	case msg := <-pubsub.Channel():
		var envelope struct {
			Cmd     string                `json:"cmd"`
			Payload GatewayProviderConfig `json:"payload"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
			t.Fatalf("unmarshal envelope: %v", err)
		}
		if envelope.Cmd != RedisCmdConfigureGatewayProvider {
			t.Fatalf("cmd=%s want=%s", envelope.Cmd, RedisCmdConfigureGatewayProvider)
		}
		if envelope.Payload.AgentID != cfg.AgentID || envelope.Payload.APIKey != cfg.APIKey {
			t.Fatalf("payload mismatch: got %+v want %+v", envelope.Payload, cfg)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for broadcast publish")
	}
}

func TestPublishConfigureGatewayProvider_ReturnsErrorWhenNoRedis(t *testing.T) {
	previous := store.RDB
	store.RDB = nil
	defer func() { store.RDB = previous }()

	if err := PublishConfigureGatewayProvider(GatewayProviderConfig{AgentID: 1}); err == nil {
		t.Fatal("expected error when redis is nil")
	}
}

package ws

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestNotifyAgentStateSyncDoesNotFanoutRejectedStaleState(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		store.RDB.Close()
		store.RDB = previous
	}()

	const (
		ownerID  = int64(9101)
		agentID  = int64(9201)
		oldEpoch = int64(1_700_000_000_000_000)
		newEpoch = int64(1_700_000_000_000_100)
	)
	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:ws:route:9101", "device-a", "node-b").Err(); err != nil {
		t.Fatalf("seed user route: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-b")
	defer pubsub.Close()
	if _, err := pubsub.Receive(ctx); err != nil {
		t.Fatalf("subscribe remote node channel: %v", err)
	}
	channel := pubsub.Channel()

	server := &Server{hub: NewHub("node-a"), nodeID: "node-a"}
	server.notifyAgentStateSync(
		ownerID,
		handler.BuildAgentStatePayloadWithEpoch(
			agentID,
			protocol.AgentStateOnline,
			true,
			time.Now().Add(time.Minute).UnixMilli(),
			newEpoch,
		),
	)
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatal("accepted online state was not fanned out")
	}

	server.notifyAgentStateSync(
		ownerID,
		handler.BuildAgentStatePayloadWithEpoch(
			agentID,
			protocol.AgentStateOffline,
			false,
			0,
			oldEpoch,
		),
	)
	select {
	case message := <-channel:
		t.Fatalf("rejected stale offline state was fanned out: %s", message.Payload)
	case <-time.After(100 * time.Millisecond):
	}
}

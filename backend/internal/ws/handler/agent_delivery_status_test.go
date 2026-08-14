package handler

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type deliveryStatusMockConn struct {
	userID int64
	seq    int64
	sent   []protocol.AgentDeliveryStatusPayload
	batch  []protocol.AgentDeliveryStatusBatchPayload
}

func (c *deliveryStatusMockConn) SendPayload(cmd string, seq int64, payload interface{}) {
	switch cmd {
	case protocol.CmdAgentDeliveryStatus:
		if typed, ok := payload.(protocol.AgentDeliveryStatusPayload); ok {
			c.sent = append(c.sent, typed)
		}
	case protocol.CmdAgentDeliveryStatusBatch:
		if typed, ok := payload.(protocol.AgentDeliveryStatusBatchPayload); ok {
			c.batch = append(c.batch, typed)
		}
	}
}

func (c *deliveryStatusMockConn) SendPacket(pkt *protocol.Packet) {}
func (c *deliveryStatusMockConn) AckPush(msgID int64)             {}
func (c *deliveryStatusMockConn) Close()                          {}
func (c *deliveryStatusMockConn) NextSeq() int64 {
	c.seq++
	return c.seq
}
func (c *deliveryStatusMockConn) GetUserID() int64                                           { return c.userID }
func (c *deliveryStatusMockConn) GetDeviceID() string                                        { return "dev-status" }
func (c *deliveryStatusMockConn) GetPlatform() string                                        { return "" }
func (c *deliveryStatusMockConn) SetAuth(userID int64, sessionID, deviceID, platform string) {}
func (c *deliveryStatusMockConn) IsAuthed() bool                                             { return true }

func TestRecordAndLoadAgentDeliveryStatuses(t *testing.T) {
	store.RDB = testutil.NewMockRedis()
	defer store.RDB.Close()

	RecordAgentDeliveryStatus(context.Background(), protocol.AgentDeliveryStatusPayload{
		OwnerID:      1001,
		TriggerMsgID: 2001,
		Status:       protocol.AgentDeliveryStatusQueued,
		UpdatedAt:    100,
	})
	RecordAgentDeliveryStatus(context.Background(), protocol.AgentDeliveryStatusPayload{
		OwnerID:      1001,
		TriggerMsgID: 2001,
		Status:       protocol.AgentDeliveryStatusReceived,
		UpdatedAt:    200,
	})
	RecordAgentDeliveryStatus(context.Background(), protocol.AgentDeliveryStatusPayload{
		OwnerID:      1001,
		TriggerMsgID: 2002,
		Status:       protocol.AgentDeliveryStatusFailed,
		UpdatedAt:    150,
	})

	statuses := LoadAgentDeliveryStatuses(context.Background(), 1001, 10)
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got=%d", len(statuses))
	}
	if statuses[0].TriggerMsgID != 2002 || statuses[0].Status != protocol.AgentDeliveryStatusFailed {
		t.Fatalf("first status=%#v", statuses[0])
	}
	if statuses[1].TriggerMsgID != 2001 || statuses[1].Status != protocol.AgentDeliveryStatusReceived {
		t.Fatalf("second status=%#v", statuses[1])
	}
}

func TestPushStoredAgentDeliveryStatuses(t *testing.T) {
	store.RDB = testutil.NewMockRedis()
	defer store.RDB.Close()

	RecordAgentDeliveryStatus(context.Background(), protocol.AgentDeliveryStatusPayload{
		OwnerID:      3001,
		TriggerMsgID: 4001,
		Status:       protocol.AgentDeliveryStatusTimeout,
		UpdatedAt:    1234,
	})

	conn := &deliveryStatusMockConn{userID: 3001}
	PushStoredAgentDeliveryStatuses(conn)

	if len(conn.batch) != 1 {
		t.Fatalf("expected 1 batch push, got=%d", len(conn.batch))
	}
	if len(conn.batch[0].Items) != 1 {
		t.Fatalf("expected 1 item in batch, got=%d", len(conn.batch[0].Items))
	}
	got := conn.batch[0].Items[0]
	if got.TriggerMsgID != 4001 || got.Status != protocol.AgentDeliveryStatusTimeout {
		t.Fatalf("unexpected pushed status=%#v", got)
	}
	if len(conn.sent) != 0 {
		t.Fatalf("expected 0 single-status push, got=%d", len(conn.sent))
	}
}

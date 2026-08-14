package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	wsprotocol "github.com/askie/grix/backend/internal/ws/protocol"
)

type visitorToolbarMockConn struct {
	userID int64
	seq    int64
	sent   []struct {
		cmd     string
		seq     int64
		payload any
	}
}

func (c *visitorToolbarMockConn) SendPayload(cmd string, seq int64, payload interface{}) {
	c.sent = append(c.sent, struct {
		cmd     string
		seq     int64
		payload any
	}{cmd: cmd, seq: seq, payload: payload})
}
func (c *visitorToolbarMockConn) SendPacket(pkt *wsprotocol.Packet)                          {}
func (c *visitorToolbarMockConn) AckPush(msgID int64)                                        {}
func (c *visitorToolbarMockConn) NextSeq() int64                                             { c.seq++; return c.seq }
func (c *visitorToolbarMockConn) Close()                                                     {}
func (c *visitorToolbarMockConn) GetUserID() int64                                           { return c.userID }
func (c *visitorToolbarMockConn) GetDeviceID() string                                        { return "" }
func (c *visitorToolbarMockConn) GetPlatform() string                                        { return "" }
func (c *visitorToolbarMockConn) SetAuth(userID int64, sessionID, deviceID, platform string) {}
func (c *visitorToolbarMockConn) IsAuthed() bool                                             { return true }

func TestHandleAgentToolbarGet_VisitorSession(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB

	now := time.Now().UTC()
	if err := store.DB.Create(&model.WidgetSession{
		ID:           11,
		SiteID:       22,
		OwnerUserID:  33,
		VisitorID:    44,
		VisitorKey:   "vk",
		SessionID:    "ws-visitor-1",
		Status:       model.WidgetSessionStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("seed widget session error: %v", err)
	}

	raw, _ := json.Marshal(wsprotocol.AgentToolbarGetPayload{SessionID: "ws-visitor-1"})
	conn := &visitorToolbarMockConn{userID: 33}
	HandleAgentToolbarGet(nil, conn, &wsprotocol.Packet{Seq: 100, Payload: raw})

	if len(conn.sent) != 1 {
		t.Fatalf("sent=%d want=1", len(conn.sent))
	}
	if conn.sent[0].cmd != wsprotocol.CmdAgentToolbarGetResp {
		t.Fatalf("cmd=%s want=%s", conn.sent[0].cmd, wsprotocol.CmdAgentToolbarGetResp)
	}
	resp, ok := conn.sent[0].payload.(wsprotocol.AgentToolbarGetRespPayload)
	if !ok {
		t.Fatalf("payload type=%T", conn.sent[0].payload)
	}
	if resp.Code != 0 {
		t.Fatalf("code=%d want=0", resp.Code)
	}
	if resp.Snapshot.ToolbarID != visitorToolbarID {
		t.Fatalf("toolbar_id=%s want=%s", resp.Snapshot.ToolbarID, visitorToolbarID)
	}
	if len(resp.Snapshot.Items) != 3 {
		t.Fatalf("items=%d want=3", len(resp.Snapshot.Items))
	}
}

func TestHandleAgentToolbarAction_VisitorBanSync(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB

	now := time.Now().UTC()
	if err := store.DB.Create(&model.WidgetSession{
		ID:           12,
		SiteID:       23,
		OwnerUserID:  34,
		VisitorID:    45,
		VisitorKey:   "vk2",
		SessionID:    "ws-visitor-2",
		Status:       model.WidgetSessionStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("seed widget session error: %v", err)
	}

	raw, _ := json.Marshal(wsprotocol.AgentToolbarActionPayload{
		SessionID:      "ws-visitor-2",
		ToolbarID:      visitorToolbarID,
		ItemID:         "visitor_ban",
		ActionID:       "visitor_ban",
		ClientActionID: "ca-1",
		Event:          "click",
	})
	conn := &visitorToolbarMockConn{userID: 34}
	HandleAgentToolbarAction(nil, conn, &wsprotocol.Packet{Seq: 101, Payload: raw})

	if len(conn.sent) != 2 {
		t.Fatalf("sent=%d want=2", len(conn.sent))
	}
	ack, ok := conn.sent[0].payload.(wsprotocol.AgentToolbarActionAckPayload)
	if !ok {
		t.Fatalf("ack type=%T", conn.sent[0].payload)
	}
	if !ack.Accepted {
		t.Fatalf("ack accepted=false code=%s msg=%s", ack.Code, ack.Msg)
	}
	if conn.sent[1].cmd != wsprotocol.CmdAgentToolbarSync {
		t.Fatalf("sync cmd=%s want=%s", conn.sent[1].cmd, wsprotocol.CmdAgentToolbarSync)
	}
	sync, ok := conn.sent[1].payload.(wsprotocol.AgentToolbarSnapshotPayload)
	if !ok {
		t.Fatalf("sync type=%T", conn.sent[1].payload)
	}
	if sync.ToolbarID != visitorToolbarID {
		t.Fatalf("sync toolbar=%s want=%s", sync.ToolbarID, visitorToolbarID)
	}
	var banDisabled bool
	for _, item := range sync.Items {
		if item.ItemID == "visitor_ban" {
			banDisabled = item.Disabled
			break
		}
	}
	if !banDisabled {
		t.Fatalf("visitor_ban should be disabled after ban")
	}
}

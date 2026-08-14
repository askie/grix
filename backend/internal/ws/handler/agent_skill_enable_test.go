package handler

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// skillActionMockConn is a concurrency-safe ConnInterface mock: unlike
// sendMsgMockConn (send_msg_test.go), HandleAgentSkillEnable/Disable deliver
// their response from a background goroutine, so reads/writes to `sent` need
// a lock to avoid a data race between the test goroutine and the handler's.
type skillActionMockConn struct {
	userID int64
	mu     sync.Mutex
	sent   []sentPayload
}

func (c *skillActionMockConn) SendPayload(cmd string, seq int64, payload interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, sentPayload{cmd: cmd, seq: seq, payload: payload})
}

func (c *skillActionMockConn) SendPacket(pkt *protocol.Packet)                            {}
func (c *skillActionMockConn) AckPush(msgID int64)                                        {}
func (c *skillActionMockConn) NextSeq() int64                                             { return 1 }
func (c *skillActionMockConn) Close()                                                     {}
func (c *skillActionMockConn) GetUserID() int64                                           { return c.userID }
func (c *skillActionMockConn) GetDeviceID() string                                        { return "device-skill-action" }
func (c *skillActionMockConn) GetPlatform() string                                        { return "" }
func (c *skillActionMockConn) SetAuth(userID int64, sessionID, deviceID, platform string) {}
func (c *skillActionMockConn) IsAuthed() bool                                             { return true }

func (c *skillActionMockConn) snapshot() []sentPayload {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]sentPayload(nil), c.sent...)
}

func findAgentSkillEnableResp(sent []sentPayload) (protocol.AgentSkillEnableRespPayload, bool) {
	for _, item := range sent {
		if item.cmd != protocol.CmdAgentSkillEnableResp {
			continue
		}
		resp, ok := item.payload.(protocol.AgentSkillEnableRespPayload)
		if ok {
			return resp, true
		}
	}
	return protocol.AgentSkillEnableRespPayload{}, false
}

func findAgentSkillDisableResp(sent []sentPayload) (protocol.AgentSkillDisableRespPayload, bool) {
	for _, item := range sent {
		if item.cmd != protocol.CmdAgentSkillDisableResp {
			continue
		}
		resp, ok := item.payload.(protocol.AgentSkillDisableRespPayload)
		if ok {
			return resp, true
		}
	}
	return protocol.AgentSkillDisableRespPayload{}, false
}

func waitForAgentSkillResp(t *testing.T, conn *skillActionMockConn, cmd string) []sentPayload {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sent := conn.snapshot()
		for _, item := range sent {
			if item.cmd == cmd {
				return sent
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", cmd)
	return nil
}

func TestHandleAgentSkillEnable_InvalidPayload(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	conn := &skillActionMockConn{userID: 9601}
	HandleAgentSkillEnable(nil, conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentSkillEnable,
		Seq:     1,
		Payload: json.RawMessage(`not-json`),
	})

	resp, ok := findAgentSkillEnableResp(conn.snapshot())
	if !ok {
		t.Fatalf("agent_skill_enable_resp not sent")
	}
	if resp.Error != "invalid payload" {
		t.Fatalf("error=%q want=%q", resp.Error, "invalid payload")
	}
}

func TestHandleAgentSkillEnable_MissingFields(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	conn := &skillActionMockConn{userID: 9602}
	payload, _ := json.Marshal(protocol.AgentSkillEnablePayload{
		AgentID:   1,
		SessionID: "sess-1",
		Name:      "",
		Scope:     "global",
	})
	HandleAgentSkillEnable(nil, conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentSkillEnable,
		Seq:     1,
		Payload: payload,
	})

	resp, ok := findAgentSkillEnableResp(conn.snapshot())
	if !ok {
		t.Fatalf("agent_skill_enable_resp not sent")
	}
	if resp.Error == "" {
		t.Fatalf("expected validation error, got empty")
	}
}

func TestHandleAgentSkillEnable_Forbidden(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		requesterID = int64(9611)
		otherOwner  = int64(9612)
		agentID     = int64(9613)
	)
	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		OwnerID:      otherOwner,
		AgentName:    "skill-enable-forbidden-agent",
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	conn := &skillActionMockConn{userID: requesterID}
	payload, _ := json.Marshal(protocol.AgentSkillEnablePayload{
		AgentID:   agentID,
		SessionID: "sess-1",
		Name:      "grix-log-locator",
		Scope:     "global",
	})
	HandleAgentSkillEnable(nil, conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentSkillEnable,
		Seq:     1,
		Payload: payload,
	})

	resp, ok := findAgentSkillEnableResp(conn.snapshot())
	if !ok {
		t.Fatalf("agent_skill_enable_resp not sent")
	}
	if resp.Error != "forbidden" {
		t.Fatalf("error=%q want=%q", resp.Error, "forbidden")
	}
}

func TestHandleAgentSkillEnable_AgentOffline(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(wsagentapi.NewManager("", time.Second, nil, nil, nil, nil))
	defer wsagentapi.SetGlobal(prevManager)

	const (
		ownerID = int64(9621)
		agentID = int64(9622)
	)
	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		OwnerID:      ownerID,
		AgentName:    "skill-enable-offline-agent",
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	conn := &skillActionMockConn{userID: ownerID}
	payload, _ := json.Marshal(protocol.AgentSkillEnablePayload{
		AgentID:   agentID,
		SessionID: "sess-1",
		Name:      "grix-log-locator",
		Scope:     "global",
	})
	HandleAgentSkillEnable(nil, conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentSkillEnable,
		Seq:     1,
		Payload: payload,
	})

	sent := waitForAgentSkillResp(t, conn, protocol.CmdAgentSkillEnableResp)
	resp, ok := findAgentSkillEnableResp(sent)
	if !ok {
		t.Fatalf("agent_skill_enable_resp not sent")
	}
	if resp.Error == "" {
		t.Fatalf("expected error when agent offline, got success resp=%+v", resp)
	}
	if resp.Name != "grix-log-locator" || resp.Scope != "global" {
		t.Fatalf("resp echo mismatch: %+v", resp)
	}
}

func TestHandleAgentSkillDisable_MissingFields(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	conn := &skillActionMockConn{userID: 9631}
	payload, _ := json.Marshal(protocol.AgentSkillDisablePayload{
		AgentID:   1,
		SessionID: "sess-1",
		Name:      "grix-log-locator",
		Scope:     "",
	})
	HandleAgentSkillDisable(nil, conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentSkillDisable,
		Seq:     1,
		Payload: payload,
	})

	resp, ok := findAgentSkillDisableResp(conn.snapshot())
	if !ok {
		t.Fatalf("agent_skill_disable_resp not sent")
	}
	if resp.Error == "" {
		t.Fatalf("expected validation error, got empty")
	}
}

func TestHandleAgentSkillDisable_Forbidden(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		requesterID = int64(9641)
		otherOwner  = int64(9642)
		agentID     = int64(9643)
	)
	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		OwnerID:      otherOwner,
		AgentName:    "skill-disable-forbidden-agent",
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	conn := &skillActionMockConn{userID: requesterID}
	payload, _ := json.Marshal(protocol.AgentSkillDisablePayload{
		AgentID:   agentID,
		SessionID: "sess-1",
		Name:      "grix-log-locator",
		Scope:     "global",
	})
	HandleAgentSkillDisable(nil, conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentSkillDisable,
		Seq:     1,
		Payload: payload,
	})

	resp, ok := findAgentSkillDisableResp(conn.snapshot())
	if !ok {
		t.Fatalf("agent_skill_disable_resp not sent")
	}
	if resp.Error != "forbidden" {
		t.Fatalf("error=%q want=%q", resp.Error, "forbidden")
	}
}

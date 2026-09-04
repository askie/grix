package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func findConnectorAdminResp(sent []sentPayload) (protocol.AgentConnectorAdminRespPayload, bool) {
	for _, item := range sent {
		if item.cmd != protocol.CmdAgentConnectorAdminResp {
			continue
		}
		if resp, ok := item.payload.(protocol.AgentConnectorAdminRespPayload); ok {
			return resp, true
		}
	}
	return protocol.AgentConnectorAdminRespPayload{}, false
}

func TestHandleAgentConnectorAdmin_InvalidPayload(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	conn := &skillActionMockConn{userID: 9701}
	HandleAgentConnectorAdmin(nil, conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentConnectorAdmin,
		Seq:     1,
		Payload: json.RawMessage(`not-json`),
	})

	resp, ok := findConnectorAdminResp(conn.snapshot())
	if !ok {
		t.Fatal("agent_connector_admin_resp not sent")
	}
	if resp.ErrorCode != "invalid_payload" {
		t.Fatalf("error_code=%q want=invalid_payload", resp.ErrorCode)
	}
}

// 未知 op 必须在鉴权/下发之前挡掉，不能让任意字符串透传到连接器上。
func TestHandleAgentConnectorAdmin_UnknownOp(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	conn := &skillActionMockConn{userID: 9702}
	payload, _ := json.Marshal(protocol.AgentConnectorAdminPayload{AgentID: 1, Op: "rm_rf"})
	HandleAgentConnectorAdmin(nil, conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentConnectorAdmin,
		Seq:     1,
		Payload: payload,
	})

	resp, ok := findConnectorAdminResp(conn.snapshot())
	if !ok {
		t.Fatal("agent_connector_admin_resp not sent")
	}
	if resp.ErrorCode != "invalid_payload" {
		t.Fatalf("error_code=%q want=invalid_payload", resp.ErrorCode)
	}
}

// 被共享者拿到的是别人机器的安装/创建权限，必须一律 forbidden——
// 哪怕共享关系有效（能正常用这个 agent 聊天）也不行。
func TestHandleAgentConnectorAdmin_ForbiddenForSharee(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		shareeID = int64(9711)
		ownerID  = int64(9712)
		agentID  = int64(9713)
	)
	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		OwnerID:      ownerID,
		AgentName:    "connector-admin-shared-agent",
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.AgentShare{
		ID: 97131, AgentID: agentID, OwnerID: ownerID,
		SharedTo: shareeID, Status: model.AgentShareStatusActive,
	}).Error; err != nil {
		t.Fatalf("create share error: %v", err)
	}

	conn := &skillActionMockConn{userID: shareeID}
	payload, _ := json.Marshal(protocol.AgentConnectorAdminPayload{
		AgentID: agentID,
		Op:      "list_installable",
	})
	HandleAgentConnectorAdmin(nil, conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentConnectorAdmin,
		Seq:     1,
		Payload: payload,
	})

	resp, ok := findConnectorAdminResp(conn.snapshot())
	if !ok {
		t.Fatal("agent_connector_admin_resp not sent")
	}
	if resp.ErrorCode != "forbidden" {
		t.Fatalf("error_code=%q want=forbidden", resp.ErrorCode)
	}
}

// 主人本人 + agent 不在线：回 offline 错误码，客户端据此提示"该主机无在线 agent"，
// 而不是笼统地报失败或让用户误以为连接器太老。
func TestHandleAgentConnectorAdmin_OfflineChannelAgent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(wsagentapi.NewManager("", time.Second, nil, nil, nil, nil))
	defer wsagentapi.SetGlobal(prevManager)

	const (
		ownerID = int64(9721)
		agentID = int64(9722)
	)
	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		OwnerID:      ownerID,
		AgentName:    "connector-admin-offline-agent",
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	conn := &skillActionMockConn{userID: ownerID}
	payload, _ := json.Marshal(protocol.AgentConnectorAdminPayload{
		AgentID: agentID,
		Op:      "list_installable",
	})
	HandleAgentConnectorAdmin(nil, conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentConnectorAdmin,
		Seq:     1,
		Payload: payload,
	})

	sent := waitForAgentSkillResp(t, conn, protocol.CmdAgentConnectorAdminResp)
	resp, ok := findConnectorAdminResp(sent)
	if !ok {
		t.Fatal("agent_connector_admin_resp not sent")
	}
	if resp.ErrorCode != wsagentapi.ConnectorAdminErrOffline {
		t.Fatalf("error_code=%q want=%q", resp.ErrorCode, wsagentapi.ConnectorAdminErrOffline)
	}
}

// 写操作限频：超过每分钟上限后必须直接回 rate_limited，不再下发。
func TestHandleAgentConnectorAdmin_WriteRateLimited(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(wsagentapi.NewManager("", time.Second, nil, nil, nil, nil))
	defer wsagentapi.SetGlobal(prevManager)

	const (
		ownerID = int64(9731)
		agentID = int64(9732)
	)
	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		OwnerID:      ownerID,
		AgentName:    "connector-admin-rate-agent",
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	payload, _ := json.Marshal(protocol.AgentConnectorAdminPayload{
		AgentID: agentID,
		Op:      "install",
		Args:    map[string]any{"agent_type": "claude"},
	})
	for i := 0; i < connectorAdminWriteRateLimit; i++ {
		conn := &skillActionMockConn{userID: ownerID}
		HandleAgentConnectorAdmin(nil, conn, &protocol.Packet{
			Cmd: protocol.CmdAgentConnectorAdmin, Seq: int64(i + 1), Payload: payload,
		})
		sent := waitForAgentSkillResp(t, conn, protocol.CmdAgentConnectorAdminResp)
		resp, _ := findConnectorAdminResp(sent)
		if resp.ErrorCode == "rate_limited" {
			t.Fatalf("rate limited too early at attempt %d", i+1)
		}
	}

	conn := &skillActionMockConn{userID: ownerID}
	HandleAgentConnectorAdmin(nil, conn, &protocol.Packet{
		Cmd: protocol.CmdAgentConnectorAdmin, Seq: 99, Payload: payload,
	})
	resp, ok := findConnectorAdminResp(conn.snapshot())
	if !ok {
		t.Fatal("agent_connector_admin_resp not sent")
	}
	if resp.ErrorCode != "rate_limited" {
		t.Fatalf("error_code=%q want=rate_limited", resp.ErrorCode)
	}
}

// 审计日志不得带 api_key 之类的秘密。
func TestConnectorAdminAuditArgsDropsSecrets(t *testing.T) {
	got := connectorAdminAuditArgs(map[string]any{
		"agent_name": "claude-1",
		"api_key":    "sk-super-secret",
		"ws_url":     "wss://example.invalid/ws",
	})
	if got != `{"agent_name":"claude-1"}` {
		t.Fatalf("audit args=%s want only whitelisted fields", got)
	}
}

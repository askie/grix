package handler

import (
	"encoding/json"
	"strconv"
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

func seedConnectorAdminOwner(t *testing.T, ownerID int64) {
	t.Helper()
	suffix := strconv.FormatInt(ownerID, 10)
	if err := store.DB.Create(&model.User{
		ID:           ownerID,
		Username:     "connector_admin_owner_" + suffix,
		Email:        "connector_admin_owner_" + suffix + "@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     "Owner",
		Status:       model.UserStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed owner error: %v", err)
	}
}

func seedConnectorAdminChannelAgent(t *testing.T, ownerID, agentID int64) {
	t.Helper()
	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		OwnerID:      ownerID,
		AgentName:    "connector-admin-channel-" + strconv.FormatInt(agentID, 10),
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create channel agent error: %v", err)
	}
}

// stubConnectorAdminDispatch 替掉真实下发，让用例能精确控制连接器回执。
func stubConnectorAdminDispatch(
	t *testing.T,
	fn func(op string, args map[string]any) (*wsagentapi.ConnectorAdminResult, error),
) {
	t.Helper()
	original := connectorAdminDispatch
	connectorAdminDispatch = func(
		_ *wsagentapi.Manager, _, _ int64, op string, args map[string]any,
	) (*wsagentapi.ConnectorAdminResult, error) {
		return fn(op, args)
	}
	t.Cleanup(func() { connectorAdminDispatch = original })
}

func runConnectorAdminCreate(
	t *testing.T,
	ownerID, channelAgentID int64,
	agentName string,
) protocol.AgentConnectorAdminRespPayload {
	t.Helper()
	conn := &skillActionMockConn{userID: ownerID}
	payload, _ := json.Marshal(protocol.AgentConnectorAdminPayload{
		AgentID: channelAgentID,
		Op:      "create_agent",
		Args:    map[string]any{"agent_name": agentName, "client_type": "claude"},
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
	return resp
}

func loadConnectorAdminAgentByName(t *testing.T, ownerID int64, name string) (model.Agent, bool) {
	t.Helper()
	var agent model.Agent
	err := store.DB.Where("owner_id = ? AND agent_name = ?", ownerID, name).First(&agent).Error
	if err != nil {
		return model.Agent{}, false
	}
	return agent, true
}

// 回滚守卫：连接器受理了 add_agent 但业务失败（ok=false）时，刚建出来的 Agent 行
// 必须被删掉。不回滚的话用户名下会多一个永远连不上的孤儿 agent，还占着 agent 配额
// 和名字，只能手工清理。
func TestHandleAgentConnectorAdmin_CreateAgentRollsBackOnConnectorFailure(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(wsagentapi.NewManager("", time.Second, nil, nil, nil, nil))
	defer wsagentapi.SetGlobal(prevManager)

	const (
		ownerID        = int64(9741)
		channelAgentID = int64(9742)
	)
	seedConnectorAdminOwner(t, ownerID)
	seedConnectorAdminChannelAgent(t, ownerID, channelAgentID)

	var sawAddAgent bool
	stubConnectorAdminDispatch(t, func(op string, args map[string]any) (*wsagentapi.ConnectorAdminResult, error) {
		if op != "add_agent" {
			t.Errorf("unexpected op=%s", op)
		}
		sawAddAgent = true
		// 下发时必须已经拿到了新 agent 的 ws_url / api_key，否则连接器根本连不上。
		for _, key := range []string{"ws_url", "api_key", "agent_id", "name"} {
			if value, _ := args[key].(string); value == "" {
				t.Errorf("add_agent args missing %s: %#v", key, args)
			}
		}
		return &wsagentapi.ConnectorAdminResult{
			ErrorCode: "remote_admin_disabled",
			Error:     "remote admin is disabled",
		}, nil
	})

	resp := runConnectorAdminCreate(t, ownerID, channelAgentID, "rollback-me")

	if !sawAddAgent {
		t.Fatal("add_agent was never dispatched")
	}
	if resp.ErrorCode != "remote_admin_disabled" {
		t.Fatalf("error_code=%q want=remote_admin_disabled", resp.ErrorCode)
	}
	// 行必须真的被建出来过（否则这条断言会被"压根没建"的实现空过），
	// 并且已经回滚成已删除状态。
	agent, found := loadConnectorAdminAgentByName(t, ownerID, "rollback-me")
	if !found {
		t.Fatal("expected the created agent row to exist and be rolled back")
	}
	if agent.Status != 3 {
		t.Fatalf("orphan agent left behind: id=%d status=%d", agent.ID, agent.Status)
	}
}

// 下发根本发不出去（agent 离线 / 老连接器未声明能力）时同样要回滚。
func TestHandleAgentConnectorAdmin_CreateAgentRollsBackOnDispatchError(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(wsagentapi.NewManager("", time.Second, nil, nil, nil, nil))
	defer wsagentapi.SetGlobal(prevManager)

	const (
		ownerID        = int64(9751)
		channelAgentID = int64(9752)
	)
	seedConnectorAdminOwner(t, ownerID)
	seedConnectorAdminChannelAgent(t, ownerID, channelAgentID)

	stubConnectorAdminDispatch(t, func(string, map[string]any) (*wsagentapi.ConnectorAdminResult, error) {
		return nil, wsagentapi.ErrConnectorAdminUnsupported
	})

	resp := runConnectorAdminCreate(t, ownerID, channelAgentID, "rollback-me-too")

	if resp.ErrorCode != wsagentapi.ConnectorAdminErrUnsupported {
		t.Fatalf("error_code=%q want=%q", resp.ErrorCode, wsagentapi.ConnectorAdminErrUnsupported)
	}
	agent, found := loadConnectorAdminAgentByName(t, ownerID, "rollback-me-too")
	if !found {
		t.Fatal("expected the created agent row to exist and be rolled back")
	}
	if agent.Status != 3 {
		t.Fatalf("orphan agent left behind: id=%d status=%d", agent.ID, agent.Status)
	}
}

// 正向对照：连接器回成功时 Agent 行必须留下，回执带上客户端要用的字段。
// 没有这条，上面两条"删掉了就算过"的断言会被一个永远失败的实现骗过去。
func TestHandleAgentConnectorAdmin_CreateAgentKeepsAgentOnSuccess(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(wsagentapi.NewManager("", time.Second, nil, nil, nil, nil))
	defer wsagentapi.SetGlobal(prevManager)

	const (
		ownerID        = int64(9761)
		channelAgentID = int64(9762)
	)
	seedConnectorAdminOwner(t, ownerID)
	seedConnectorAdminChannelAgent(t, ownerID, channelAgentID)

	stubConnectorAdminDispatch(t, func(string, map[string]any) (*wsagentapi.ConnectorAdminResult, error) {
		return &wsagentapi.ConnectorAdminResult{Result: map[string]any{"ok": true}}, nil
	})

	resp := runConnectorAdminCreate(t, ownerID, channelAgentID, "keep-me")

	if resp.ErrorCode != "" || resp.Error != "" {
		t.Fatalf("unexpected error: code=%q msg=%q", resp.ErrorCode, resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result=%#v want a map", resp.Result)
	}
	if result["agent_id"] == "" || result["api_key"] == "" {
		t.Fatalf("result missing agent_id/api_key: %#v", result)
	}
	agent, found := loadConnectorAdminAgentByName(t, ownerID, "keep-me")
	if !found || agent.Status == 3 {
		t.Fatalf("created agent should survive a successful add_agent: found=%v", found)
	}
}

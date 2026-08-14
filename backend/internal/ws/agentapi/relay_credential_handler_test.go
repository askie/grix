package agentapi

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func setupRelayCredentialTest(t *testing.T) {
	t.Helper()
	testDB := testutil.NewTestDB()
	t.Cleanup(testDB.Close)

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() { store.DB = originalDB })

	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake: %v", err)
	}
}

func newRelayCredentialConn(agentID, ownerID int64) *agentConn {
	return &agentConn{
		agentID: agentID,
		ownerID: ownerID,
		send:    make(chan []byte, 4),
		done:    make(chan struct{}),
	}
}

func readRelayCredentialResult(t *testing.T, conn *agentConn) (int64, RelayCredentialResultPayload) {
	t.Helper()
	select {
	case raw := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("unmarshal outbound packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdRelayCredentialResult {
			t.Fatalf("expected cmd=%s, got %s", protocol.CmdRelayCredentialResult, pkt.Cmd)
		}
		var result RelayCredentialResultPayload
		if err := json.Unmarshal(pkt.Payload, &result); err != nil {
			t.Fatalf("unmarshal result payload: %v", err)
		}
		return pkt.Seq, result
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for relay_credential_result")
		return 0, RelayCredentialResultPayload{}
	}
}

func relayCredentialRequestPacket(t *testing.T, seq int64, payload RelayCredentialRequestPayload) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return &protocol.Packet{Cmd: protocol.CmdRelayCredentialRequest, Seq: seq, Payload: raw}
}

// 成功路径：connector 用自己的连接申请凭证，服务端按连接认证身份签发，
// 明文 Key 随 seq 关联的应答下发。
func TestHandleRelayCredentialRequest_IssuesKeyOnSameSeq(t *testing.T) {
	setupRelayCredentialTest(t)
	agent := model.Agent{ID: 8101, AgentName: "relay-agent", OwnerID: 8100, AgentClientType: model.AgentClientTypeClaude}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := newRelayCredentialConn(8101, 8100)

	mgr.handleRelayCredentialRequest(conn, relayCredentialRequestPacket(t, 42, RelayCredentialRequestPayload{
		AnthropicBaseURL: "https://grix.dhf.pub/anthropic/v1",
		OpenAIBaseURL:    "https://grix.dhf.pub/openai/v1",
	}))

	seq, result := readRelayCredentialResult(t, conn)
	if seq != 42 {
		t.Fatalf("expected response seq=42 (request correlation), got %d", seq)
	}
	if result.Status != "ok" {
		t.Fatalf("expected status ok, got %+v", result)
	}
	if result.APIKey == "" {
		t.Fatal("expected plaintext api_key in result")
	}
	if result.AnthropicBaseURL != "https://grix.dhf.pub/anthropic/v1" || result.OpenAIBaseURL != "https://grix.dhf.pub/openai/v1" {
		t.Fatalf("expected base URLs echoed back, got %+v", result)
	}
}

// 共享连接（ownerID 不是 agent 主人）触不到主人的钱包：签发必须被拒绝，
// 不能借 WS 通道越权拿到别人 agent 的中转 Key。
func TestHandleRelayCredentialRequest_ForbidsNonOwner(t *testing.T) {
	setupRelayCredentialTest(t)
	agent := model.Agent{ID: 8201, AgentName: "relay-agent", OwnerID: 8200, AgentClientType: model.AgentClientTypeClaude}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := newRelayCredentialConn(8201, 9999)

	mgr.handleRelayCredentialRequest(conn, relayCredentialRequestPacket(t, 1, RelayCredentialRequestPayload{}))

	_, result := readRelayCredentialResult(t, conn)
	if result.Status != "failed" {
		t.Fatalf("expected failed, got %+v", result)
	}
	want := errcode.ErrAgentForbidden.BizCode
	if got := result.ErrorCode; got != strconv.Itoa(want) {
		t.Fatalf("expected error_code=%d, got %s", want, got)
	}
}

// 畸形 base_url 在服务端源头拦住：它会被原样写进 connector 的路由配置。
func TestHandleRelayCredentialRequest_RejectsInvalidBaseURL(t *testing.T) {
	setupRelayCredentialTest(t)
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := newRelayCredentialConn(8301, 8300)

	mgr.handleRelayCredentialRequest(conn, relayCredentialRequestPacket(t, 7, RelayCredentialRequestPayload{
		AnthropicBaseURL: "not-a-url",
	}))

	seq, result := readRelayCredentialResult(t, conn)
	if seq != 7 || result.Status != "failed" {
		t.Fatalf("expected failed on seq=7, got seq=%d %+v", seq, result)
	}
	if result.APIKey != "" {
		t.Fatal("failed result must not carry api_key")
	}
}

func TestHandleRelayCredentialRequest_RejectsMalformedPayload(t *testing.T) {
	setupRelayCredentialTest(t)
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := newRelayCredentialConn(8401, 8400)

	mgr.handleRelayCredentialRequest(conn, &protocol.Packet{
		Cmd:     protocol.CmdRelayCredentialRequest,
		Seq:     9,
		Payload: json.RawMessage(`{not json`),
	})

	seq, result := readRelayCredentialResult(t, conn)
	if seq != 9 || result.Status != "failed" {
		t.Fatalf("expected failed on seq=9, got seq=%d %+v", seq, result)
	}
}

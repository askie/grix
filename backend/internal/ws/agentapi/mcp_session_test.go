package agentapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupMcpSessionTest(t *testing.T) func() {
	t.Helper()
	previousDB := store.DB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	_ = snowflake.Init(1)
	return func() {
		testDB.Close()
		store.DB = previousDB
	}
}

// getOrCreate 必须按 (agentID, sessionID) 幂等：同一对端恒返回同一会话与 mcp_session_id。
func TestMcpSessionStore_GetOrCreateIdempotent(t *testing.T) {
	_ = snowflake.Init(1)
	s := newMcpSessionStore()

	a := s.getOrCreate(100, 1, "sess-A")
	b := s.getOrCreate(100, 1, "sess-A")
	if a.id != b.id {
		t.Fatalf("expected same mcp_session id, got %s vs %s", a.id, b.id)
	}
	if a != b {
		t.Fatalf("expected same session pointer")
	}

	// 不同 session 应是不同会话
	c := s.getOrCreate(100, 1, "sess-B")
	if c.id == a.id {
		t.Fatalf("different sessionID must yield different mcp_session")
	}
	// 不同 agent 即使相同 sessionID 也应隔离
	d := s.getOrCreate(200, 1, "sess-A")
	if d.id == a.id {
		t.Fatalf("different agentID must yield different mcp_session")
	}
}

// get 按 mcp_session_id 取回；remove 后取不到。
func TestMcpSessionStore_GetAndRemove(t *testing.T) {
	_ = snowflake.Init(1)
	s := newMcpSessionStore()
	sess := s.getOrCreate(100, 1, "sess-A")

	if got := s.get(sess.id); got == nil || got.id != sess.id {
		t.Fatalf("get by id failed")
	}
	s.remove(sess.id)
	if got := s.get(sess.id); got != nil {
		t.Fatalf("expected nil after remove")
	}
	// remove 后 getOrCreate 应新建（不复用已删的）
	again := s.getOrCreate(100, 1, "sess-A")
	if again.id == sess.id {
		t.Fatalf("expected fresh session after remove")
	}
}

// removeByAgent 清理该 agent 全部会话（byID/byKey/byAgent 都清）；防泄漏回归测试。
func TestMcpSessionStore_RemoveByAgent(t *testing.T) {
	_ = snowflake.Init(1)
	s := newMcpSessionStore()
	s1 := s.getOrCreate(100, 1, "sess-A")
	s2 := s.getOrCreate(100, 1, "sess-B")
	other := s.getOrCreate(200, 1, "sess-A")

	s.removeByAgent(100)

	if s.get(s1.id) != nil || s.get(s2.id) != nil {
		t.Fatalf("agent 100 sessions should be cleared")
	}
	// 其它 agent 不受影响
	if s.get(other.id) == nil {
		t.Fatalf("agent 200 session must survive")
	}
	// byKey 也应清理：重新 getOrCreate 得到新 id
	fresh := s.getOrCreate(100, 1, "sess-A")
	if fresh.id == s1.id {
		t.Fatalf("byKey not cleared, got stale session")
	}
	// byAgent 清理：removeByAgent 后该 agent 列表为空，再删不 panic
	s.removeByAgent(100)
}

// persistMcpSession/loadMcpSession 往返：跨节点共享元数据，APP 回帧能在其它
// ws 节点（内存无此会话）按 mcp_session_id 还原 agentID/ownerID/sessionID；
// deleteMcpSessionMeta 后应取不到。跨节点回帧路由闭环的关键回归测试。
func TestMcpSessionRedisMetaRoundtrip(t *testing.T) {
	originalRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() { store.RDB = originalRDB }()

	sess := &mcpSession{id: "mcp_42", agentID: 7001, ownerID: 9001, sessionID: "sess-X"}
	persistMcpSession(sess)

	got := loadMcpSession("mcp_42")
	if got == nil {
		t.Fatalf("expected to load persisted session meta")
	}
	if got.agentID != sess.agentID || got.ownerID != sess.ownerID || got.sessionID != sess.sessionID {
		t.Fatalf("meta mismatch: got agent=%d owner=%d session=%s", got.agentID, got.ownerID, got.sessionID)
	}

	// 未知 id 返回 nil
	if loadMcpSession("mcp_does_not_exist") != nil {
		t.Fatalf("unknown id must return nil")
	}

	// 删除后取不到
	deleteMcpSessionMeta([]string{"mcp_42"})
	if loadMcpSession("mcp_42") != nil {
		t.Fatalf("expected nil after delete")
	}
}

// checkToolsCallAuth（闸3）：非 tools/call 帧放行；tools/call 按 agent 能力(scope)
// 校验——已授予对应 scope 才放行，未授予或未登记工具一律拒绝。
func TestCheckToolsCallAuth(t *testing.T) {
	cleanup := setupMcpSessionTest(t)
	defer cleanup()

	const agentID = int64(7700)
	// 仅授予 grix_open_chat 能力；grix_local_search/grix_open_page 不授予。
	if err := store.DB.Create(&model.AgentAPIScope{
		AgentID: agentID, Scope: agentscope.ScopeAppOpenChat,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed scope failed: %v", err)
	}
	sess := &mcpSession{id: "mcp_1", agentID: agentID, ownerID: 1, sessionID: "s"}

	// initialize（非 tools/call）应放行
	initFrame := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if allowed, _ := checkToolsCallAuth(sess, initFrame); !allowed {
		t.Fatalf("initialize must be allowed")
	}

	// tools/list（非 tools/call）应放行（过滤在闸2）
	listFrame := json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if allowed, _ := checkToolsCallAuth(sess, listFrame); !allowed {
		t.Fatalf("tools/list must be allowed")
	}

	// tools/call grix_open_chat：已授权 → 放行，并解析出 tool name
	callOk := json.RawMessage(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"grix_open_chat","arguments":{}}}`)
	if allowed, name := checkToolsCallAuth(sess, callOk); !allowed || name != "grix_open_chat" {
		t.Fatalf("authorized tool must be allowed, got allowed=%v name=%q", allowed, name)
	}

	// tools/call grix_local_search：未授权 → 拒绝
	callNo := json.RawMessage(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"grix_local_search","arguments":{}}}`)
	if allowed, _ := checkToolsCallAuth(sess, callNo); allowed {
		t.Fatalf("unauthorized tool must be denied")
	}

	// tools/call 未登记工具 → 拒绝
	callUnknown := json.RawMessage(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"no_such_tool"}}`)
	if allowed, _ := checkToolsCallAuth(sess, callUnknown); allowed {
		t.Fatalf("unregistered tool must be denied")
	}

	// 非法 JSON 不应 panic，放行交由下游处理
	if allowed, _ := checkToolsCallAuth(sess, json.RawMessage(`not-json`)); !allowed {
		t.Fatalf("malformed frame should pass through")
	}
}

// filterToolsListResponse（闸2）：按 agent 能力过滤 tools/list 响应，
// 只保留已授权工具；非 tools/list 响应原样透传。
func TestFilterToolsListResponse(t *testing.T) {
	cleanup := setupMcpSessionTest(t)
	defer cleanup()

	const agentID = int64(7800)
	if err := store.DB.Create(&model.AgentAPIScope{
		AgentID: agentID, Scope: agentscope.ScopeAppOpenChat,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed scope failed: %v", err)
	}
	sess := &mcpSession{id: "mcp_2", agentID: agentID, ownerID: 1, sessionID: "s"}

	frame := json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"grix_local_search"},{"name":"grix_open_chat"},{"name":"grix_open_page"}]}}`)
	out := filterToolsListResponse(sess, frame)

	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal filtered: %v", err)
	}
	if len(resp.Result.Tools) != 1 || resp.Result.Tools[0].Name != "grix_open_chat" {
		t.Fatalf("expected only grix_open_chat, got %+v", resp.Result.Tools)
	}
	// id / jsonrpc 等外层字段须保留
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if string(envelope["id"]) != "1" {
		t.Fatalf("id must be preserved, got %s", envelope["id"])
	}

	// 非 tools/list 响应原样透传
	other := json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"content":[]}}`)
	if string(filterToolsListResponse(sess, other)) != string(other) {
		t.Fatalf("non-tools/list response must pass through unchanged")
	}
}

// extractJsonRpcID：能从帧提取 id，缺失返回 null。
func TestExtractJsonRpcID(t *testing.T) {
	if got := string(extractJsonRpcID(json.RawMessage(`{"id":7}`))); got != "7" {
		t.Fatalf("expected id 7, got %s", got)
	}
	if got := string(extractJsonRpcID(json.RawMessage(`{"method":"x"}`))); got != "null" {
		t.Fatalf("expected null for missing id, got %s", got)
	}
}

// verifyAgentSessionMembership：agent 是 session 成员(member_type=2)才放行。会话归属校验（闸1）。
func TestVerifyAgentSessionMembership(t *testing.T) {
	cleanup := setupMcpSessionTest(t)
	defer cleanup()

	const agentID = int64(5001)
	const sessionID = "sess-member-test"

	// 未加入 → 拒绝
	if verifyAgentSessionMembership(agentID, sessionID) {
		t.Fatalf("non-member must be rejected")
	}

	// 加入为 agent 成员(member_type=2) → 放行
	if err := store.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     agentID,
		MemberType:   2,
		JoinedAt:     time.Now(),
		LastActiveAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed member failed: %v", err)
	}
	if !verifyAgentSessionMembership(agentID, sessionID) {
		t.Fatalf("agent member must be allowed")
	}

	// 人类成员(member_type=1)不算 agent 成员
	const humanID = int64(6001)
	if err := store.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     humanID,
		MemberType:   1,
		JoinedAt:     time.Now(),
		LastActiveAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed human failed: %v", err)
	}
	if verifyAgentSessionMembership(humanID, sessionID) {
		t.Fatalf("human member must not pass agent membership check")
	}
}

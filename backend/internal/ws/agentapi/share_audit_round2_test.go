package agentapi

// 本文件:对抗审查二轮(A 档 + B 档 + C 档)新增逻辑的守卫测试。
// 任何回归到"按 owner 路由"以前的行为(单 agentID 全局共享一条连接 / 单一 route key /
// 单一 capabilities key / 跨 owner 误清等)都会让这里的测试失败,防止后续不小心改回去。

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// ---------- 公共 fixture ----------

const (
	auditAgentID = int64(93001)
	auditOwnerA  = int64(85001) // 主人
	auditUserB   = int64(85002) // 被共享者
	auditUserC   = int64(85003) // 第二个被共享者(用于多 owner 场景)
)

func newAuditManager(t *testing.T) (*Manager, func()) {
	t.Helper()
	originalDB := store.DB
	originalRDB := store.RDB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	cleanup := func() {
		store.DB = originalDB
		store.RDB = originalRDB
		testDB.Close()
	}
	m := NewManager("", time.Second, nil, nil, nil, nil)
	m.SetNodeID("audit-node-1")
	mgrCleanup := func() {
		m.Shutdown()
		cleanup()
	}
	return m, mgrCleanup
}

func auditConn(agentID, ownerID int64, isPrimary bool) *agentConn {
	return &agentConn{
		agentID:   agentID,
		ownerID:   ownerID,
		isPrimary: isPrimary,
		send:      make(chan []byte, 16),
	}
}

// ============================================================
// A 档 守卫
// ============================================================

// A1: claude actor 必须做 agent.OwnerID==ownerID 归属校验。
// 防止被共享者借用主人的 agent 调 access_control 接管主人的接入权。
func TestGuardA1_EnsureClaudeAgentActorChecksOwnership(t *testing.T) {
	_, cleanup := newAuditManager(t)
	defer cleanup()

	// seed 一个 claude agent,主人是 A
	if err := store.DB.Exec(
		"INSERT INTO agents (id, owner_id, agent_name, provider_type, agent_client_type, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		auditAgentID, auditOwnerA, "claude-actor", 3, "claude", 1, time.Now(), time.Now(),
	).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	// 主人(A)调,应通过
	if err := ensureAccessControlActor(auditAgentID, auditOwnerA); err != nil {
		t.Fatalf("owner should pass: %v", err)
	}
	// 被共享者(B)调,应被拒(归属不符)
	if err := ensureAccessControlActor(auditAgentID, auditUserB); err == nil {
		t.Fatal("non-owner caller MUST be rejected (即便 agent 是 claude 类型,B 不是 owner 就不能调 claude_access)")
	}
	// ownerID<=0 也拒
	if err := ensureAccessControlActor(auditAgentID, 0); err == nil {
		t.Fatal("ownerID=0 must be rejected")
	}
}

// A4: PushToAgent 必须按 (agentID, ownerID) 路由。
// 防止 B↔X 私聊里 B 撤回/编辑的 event_revoke / event_edit 错投到主人 A 的 connector。
func TestGuardA4_PushToAgentRoutesByOwner(t *testing.T) {
	m, cleanup := newAuditManager(t)
	defer cleanup()

	connA := auditConn(auditAgentID, auditOwnerA, true)
	connB := auditConn(auditAgentID, auditUserB, false)
	m.putConnForTest(connA)
	m.putConnForTest(connB)

	revokePayload := protocol.AgentRevokeEventPayload{
		SessionID: "sess-test", MsgID: 1001, EventID: "evt-1",
	}
	// 按 ownerID=B 路由:只 B 收到,A 不应收到
	if ok := m.PushToAgent(auditAgentID, auditUserB, "event_revoke", revokePayload); !ok {
		t.Fatal("PushToAgent(B) should succeed")
	}
	select {
	case <-connB.send:
		// OK
	case <-time.After(200 * time.Millisecond):
		t.Fatal("B's conn should receive the event")
	}
	select {
	case <-connA.send:
		t.Fatal("A's conn MUST NOT receive B's event (隔离破坏)")
	case <-time.After(100 * time.Millisecond):
		// OK,A 收不到才对
	}

	// ownerID=0 非法:fail-closed 拒绝,绝不回退主连接(否则 B 的撤回会串到 A)。
	if ok := m.PushToAgent(auditAgentID, 0, "event_revoke", protocol.AgentRevokeEventPayload{
		SessionID: "sess-test", MsgID: 1002, EventID: "evt-2",
	}); ok {
		t.Fatal("PushToAgent(owner=0) MUST be rejected (fail-closed)")
	}
	select {
	case <-connA.send:
		t.Fatal("rejected owner=0 push MUST NOT reach primary A")
	case <-time.After(100 * time.Millisecond):
		// OK,A 收不到才对
	}
}

// A3: dispatchAgentCommand 必须按 owner 严格路由。
// 防止被共享者 B 点 stop 时 event_stop 落到主人 A 的 connector(B 的 run 不停、A 被误打扰)。
func TestGuardA3_DispatchAgentCommandRoutesByOwner(t *testing.T) {
	m, cleanup := newAuditManager(t)
	defer cleanup()

	connA := auditConn(auditAgentID, auditOwnerA, true)
	connB := auditConn(auditAgentID, auditUserB, false)
	m.putConnForTest(connA)
	m.putConnForTest(connB)

	// 按 owner=B 派 stop:必须只 B 收到
	if ok := m.dispatchAgentCommand(auditAgentID, auditUserB, protocol.CmdEventStop, map[string]any{"reason": "test"}); !ok {
		t.Fatal("dispatchAgentCommand(B) should succeed")
	}
	select {
	case <-connB.send:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("B's conn should receive event_stop")
	}
	select {
	case <-connA.send:
		t.Fatal("A's conn MUST NOT receive B's event_stop")
	case <-time.After(100 * time.Millisecond):
	}

	// owner=0 非法:fail-closed 拒绝,绝不回退主连接(否则 B 的 stop 会打扰 A)。
	if ok := m.dispatchAgentCommand(auditAgentID, 0, protocol.CmdEventStop, map[string]any{"reason": "fallback"}); ok {
		t.Fatal("dispatchAgentCommand(owner=0) MUST be rejected (fail-closed)")
	}
	select {
	case <-connA.send:
		t.Fatal("rejected owner=0 command MUST NOT reach primary A")
	case <-time.After(100 * time.Millisecond):
	}
}

// A5: KickAgent 必须踢该 agent 全部 owner 连接(主连接 + 所有共享连接)。
// 防止删 agent / 重置 api key 时,被共享者残留连接继续读主人本机数据。
func TestGuardA5_KickAgentKicksAllOwnerConnections(t *testing.T) {
	m, cleanup := newAuditManager(t)
	defer cleanup()

	connA := auditConn(auditAgentID, auditOwnerA, true)
	connB := auditConn(auditAgentID, auditUserB, false)
	connC := auditConn(auditAgentID, auditUserC, false)
	m.putConnForTest(connA)
	m.putConnForTest(connB)
	m.putConnForTest(connC)

	m.KickAgent(auditAgentID, "agent_deleted")

	// 三条连接都必须被关闭
	for _, c := range []*agentConn{connA, connB, connC} {
		select {
		case _, ok := <-c.send:
			if ok {
				// 有消息可读说明 send 通道还没 close,等一下
				select {
				case _, ok2 := <-c.send:
					if ok2 {
						t.Fatalf("conn owner=%d send chan should be closed by KickAgent", c.ownerID)
					}
				case <-time.After(100 * time.Millisecond):
				}
			}
		case <-time.After(100 * time.Millisecond):
		}
		if !c.closed.Load() {
			t.Fatalf("conn owner=%d MUST be closed after KickAgent", c.ownerID)
		}
	}
}

// ============================================================
// B 档 守卫
// ============================================================

// B1a: refreshAgentRoute 必须按"主连接写主+owner key,共享连接只写 owner key"的双 key 策略。
// 同时把 owner 加入 owner 集合,供 loadAgentRouteAllNodes 跨节点扫描。
func TestGuardB1_RefreshAgentRouteWritesOwnerKey(t *testing.T) {
	m, cleanup := newAuditManager(t)
	defer cleanup()
	ctx := context.Background()
	ttl := 30 * time.Second

	// 主连接(A):同时写主 key + owner key + 加入 owner 集合
	connA := auditConn(auditAgentID, auditOwnerA, true)
	m.refreshAgentRoute(connA, ttl)
	if v, _ := store.RDB.Get(ctx, agentRouteKey(auditAgentID)).Result(); v != m.getNodeID() {
		t.Fatalf("primary should write main route key, got %q", v)
	}
	if v, _ := store.RDB.Get(ctx, agentRouteKeyForOwner(auditAgentID, auditOwnerA)).Result(); v != m.getNodeID() {
		t.Fatalf("primary should write owner route key, got %q", v)
	}
	members, _ := store.RDB.SMembers(ctx, agentRouteOwnerSetKey(auditAgentID)).Result()
	if !containsString(members, strconv.FormatInt(auditOwnerA, 10)) {
		t.Fatalf("owner A should be in owner set, got %v", members)
	}

	// 共享连接(B):只写 owner key,不动主 key
	connB := auditConn(auditAgentID, auditUserB, false)
	m.refreshAgentRoute(connB, ttl)
	if v, _ := store.RDB.Get(ctx, agentRouteKeyForOwner(auditAgentID, auditUserB)).Result(); v != m.getNodeID() {
		t.Fatalf("shared conn should write owner B route key, got %q", v)
	}
	// owner 集合应同时包含 A 和 B
	members, _ = store.RDB.SMembers(ctx, agentRouteOwnerSetKey(auditAgentID)).Result()
	if !containsString(members, strconv.FormatInt(auditUserB, 10)) {
		t.Fatalf("owner B should be in owner set, got %v", members)
	}

	// loadAgentRouteForOwner 优先返 owner key,找不到回退主路由
	if got := loadAgentRouteForOwner(ctx, auditAgentID, auditUserB); got != m.getNodeID() {
		t.Fatalf("loadAgentRouteForOwner(B) should return owner-specific node, got %q", got)
	}
	// 不存在的 owner(C)回退主路由
	if got := loadAgentRouteForOwner(ctx, auditAgentID, auditUserC); got != m.getNodeID() {
		t.Fatalf("unknown owner should fall back to primary route, got %q", got)
	}
}

// B1b: clearAgentRouteForOwner 必须只清自己 owner 的路由 key,
// 不能影响主路由 key 或其他 owner 的路由(LB 抖动场景下尤其关键)。
func TestGuardB1_ClearAgentRouteForOwnerOnlyClearsOwn(t *testing.T) {
	m, cleanup := newAuditManager(t)
	defer cleanup()
	ctx := context.Background()
	ttl := 30 * time.Second

	connA := auditConn(auditAgentID, auditOwnerA, true)
	connB := auditConn(auditAgentID, auditUserB, false)
	m.refreshAgentRoute(connA, ttl)
	m.refreshAgentRoute(connB, ttl)

	// 清 B 的 owner 路由
	m.clearAgentRouteForOwner(auditAgentID, auditUserB)

	// B 的 owner key 应该被清
	if v, err := store.RDB.Get(ctx, agentRouteKeyForOwner(auditAgentID, auditUserB)).Result(); err == nil && v != "" {
		t.Fatalf("owner B route should be cleared, still got %q", v)
	}
	// A 的 owner key + 主 key 不能被影响
	if v, _ := store.RDB.Get(ctx, agentRouteKeyForOwner(auditAgentID, auditOwnerA)).Result(); v != m.getNodeID() {
		t.Fatalf("owner A route MUST NOT be touched, got %q", v)
	}
	if v, _ := store.RDB.Get(ctx, agentRouteKey(auditAgentID)).Result(); v != m.getNodeID() {
		t.Fatalf("main route MUST NOT be touched, got %q", v)
	}
}

// B1c: loadAgentRouteAllNodes 必须能扫到该 agent 在所有节点的连接位置(去重)。
// 用于撤销共享 / KickAgent 等跨节点广播场景。
func TestGuardB1_LoadAgentRouteAllNodesReturnsAllNodes(t *testing.T) {
	m, cleanup := newAuditManager(t)
	defer cleanup()
	ctx := context.Background()

	// 模拟多节点:手动写多个 owner 路由,分别指向不同 node
	if err := store.RDB.Set(ctx, agentRouteKey(auditAgentID), "node-1", time.Minute).Err(); err != nil {
		t.Fatalf("set main route: %v", err)
	}
	if err := store.RDB.Set(ctx, agentRouteKeyForOwner(auditAgentID, auditOwnerA), "node-1", time.Minute).Err(); err != nil {
		t.Fatalf("set owner A route: %v", err)
	}
	if err := store.RDB.Set(ctx, agentRouteKeyForOwner(auditAgentID, auditUserB), "node-2", time.Minute).Err(); err != nil {
		t.Fatalf("set owner B route: %v", err)
	}
	store.RDB.SAdd(ctx, agentRouteOwnerSetKey(auditAgentID), strconv.FormatInt(auditOwnerA, 10), strconv.FormatInt(auditUserB, 10))

	nodes := loadAgentRouteAllNodes(ctx, auditAgentID)
	if !containsString(nodes, "node-1") {
		t.Fatalf("nodes should include node-1, got %v", nodes)
	}
	if !containsString(nodes, "node-2") {
		t.Fatalf("nodes should include node-2 (B 所在节点必须被扫到,否则跨节点撤销通知漏发), got %v", nodes)
	}
	// 去重:node-1 不能重复出现
	count := 0
	for _, n := range nodes {
		if n == "node-1" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("node-1 should appear exactly once, got %d in %v", count, nodes)
	}
	_ = m // 仅用 m 拿 ctx,实际函数是包级函数
}

// B2: 跨节点 MCP 帧 forwardedMcpFrame 必须带 OwnerID;
// handleForwardedMcpFrame 必须按 (agentID, ownerID) 严格选连接。
func TestGuardB2_ForwardedMcpFrameCarriesOwnerAndRoutes(t *testing.T) {
	m, cleanup := newAuditManager(t)
	defer cleanup()

	connA := auditConn(auditAgentID, auditOwnerA, true)
	connB := auditConn(auditAgentID, auditUserB, false)
	m.putConnForTest(connA)
	m.putConnForTest(connB)

	frame := json.RawMessage(`{"jsonrpc":"2.0","id":1}`)

	// 模拟跨节点帧:目标 ownerID=B,必须只 B 收到
	m.handleForwardedMcpFrame(forwardedMcpFrame{
		AgentID: auditAgentID, OwnerID: auditUserB, SessionID: "s-b", Frame: frame,
	})
	select {
	case <-connB.send:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("B's conn should receive forwarded mcp frame")
	}
	select {
	case <-connA.send:
		t.Fatal("A's conn MUST NOT receive B's forwarded mcp frame")
	case <-time.After(100 * time.Millisecond):
	}

	// 兜底已改为 fail-closed:OwnerID=0 的转发帧直接丢弃,绝不回退主连接
	// (否则 B 的 MCP 回帧会落到主人 A 的 connector)。
	m.handleForwardedMcpFrame(forwardedMcpFrame{
		AgentID: auditAgentID, OwnerID: 0, SessionID: "s-a", Frame: frame,
	})
	select {
	case <-connA.send:
		t.Fatal("owner=0 frame MUST be dropped, not delivered to primary A (fail-closed)")
	case <-time.After(100 * time.Millisecond):
		// OK:丢弃才对
	}
	select {
	case <-connB.send:
		t.Fatal("owner=0 frame MUST NOT reach B either")
	case <-time.After(100 * time.Millisecond):
	}
}

// B4: localConnIsAuthoritativeForOwner 必须按 (agentID, ownerID) 维度判定。
// 防止 B 在 node2 的本地连接被误判为"过时"并把 local_action 错投到 node1(A 的节点)。
func TestGuardB4_LocalConnAuthoritativeByOwner(t *testing.T) {
	m, cleanup := newAuditManager(t)
	defer cleanup()
	ctx := context.Background()

	// 模拟:A 的连接在 node-1(主路由),B 的连接在 node-2(本节点)
	store.RDB.Set(ctx, agentRouteKey(auditAgentID), "node-1", time.Minute)
	store.RDB.Set(ctx, agentRouteKeyForOwner(auditAgentID, auditOwnerA), "node-1", time.Minute)
	store.RDB.Set(ctx, agentRouteKeyForOwner(auditAgentID, auditUserB), m.getNodeID(), time.Minute)

	// 旧的全局判定函数 localConnIsAuthoritative(只看主路由)已删除:
	// 它会把 B 在本节点的连接误判为"非权威"并把 local_action 错投到 node-1(A 的节点)。
	// 现在只有按 (agentID, ownerID) 维度的 localConnIsAuthoritativeForOwner。

	// 按 (agentID, B) 判定,B 的路由确实指本节点 → 权威 ✓
	if !m.localConnIsAuthoritativeForOwner(auditAgentID, auditUserB) {
		t.Fatal("localConnIsAuthoritativeForOwner(B) should be true: B 在本节点(audit-node-1)")
	}
	// 按 (agentID, A) 判定,A 的路由指 node-1 ≠ 本节点 → 非权威 ✓
	if m.localConnIsAuthoritativeForOwner(auditAgentID, auditOwnerA) {
		t.Fatal("localConnIsAuthoritativeForOwner(A) should be false: A 在 node-1 不是本节点")
	}
}

// B5: CleanupMcpSessionsForAgentOwner 必须只清属于 (agentID, ownerID) 的 mcp session,
// 不影响其它 owner 还在用的会话。
func TestGuardB5_CleanupMcpSessionByOwnerKeepsOthers(t *testing.T) {
	_ = snowflake.Init(1)
	m, cleanup := newAuditManager(t)
	defer cleanup()

	// 准备:A 和 B 各有一条 mcp session
	sessA := m.mcpSessions.getOrCreate(auditAgentID, auditOwnerA, "sess-A")
	sessB := m.mcpSessions.getOrCreate(auditAgentID, auditUserB, "sess-B")
	if sessA == nil || sessB == nil {
		t.Fatal("setup failed")
	}

	// 清 B 的:只能清 B 的,A 的必须保留
	m.CleanupMcpSessionsForAgentOwner(auditAgentID, auditUserB)
	if m.mcpSessions.get(sessB.id) != nil {
		t.Fatal("B's mcp session MUST be removed")
	}
	if m.mcpSessions.get(sessA.id) == nil {
		t.Fatal("A's mcp session MUST survive B's cleanup (隔离不能被打破)")
	}

	// 再清 A 的,A 也消失
	m.CleanupMcpSessionsForAgentOwner(auditAgentID, auditOwnerA)
	if m.mcpSessions.get(sessA.id) != nil {
		t.Fatal("A's mcp session should be removed after its own cleanup")
	}
}

// ============================================================
// C 档 守卫
// ============================================================

// C2: refreshAgentCapabilities 必须按 *agentConn 写主+owner key;
// loadAgentCapabilitiesForOwner 必须优先 owner key,找不到回退主 key。
func TestGuardC2_AgentCapabilitiesByOwner(t *testing.T) {
	m, cleanup := newAuditManager(t)
	defer cleanup()
	ctx := context.Background()
	ttl := 30 * time.Second

	connA := auditConn(auditAgentID, auditOwnerA, true)
	connA.localActions = []string{"action_main", "exec_approve"}
	connB := auditConn(auditAgentID, auditUserB, false)
	connB.localActions = []string{"action_shared_only"} // 模拟 B 版本异构

	m.refreshAgentCapabilities(connA, ttl)
	m.refreshAgentCapabilities(connB, ttl)

	// 主 key 写的是 A 的能力集(主连接)
	main := loadAgentCapabilities(ctx, auditAgentID)
	if !containsString(main, "exec_approve") {
		t.Fatalf("main capabilities should include A's actions, got %v", main)
	}

	// A 维度查到的也是 A 的
	a := loadAgentCapabilitiesForOwner(ctx, auditAgentID, auditOwnerA)
	if !containsString(a, "action_main") {
		t.Fatalf("owner A capabilities should match its connection, got %v", a)
	}

	// B 维度查到的是 B 的独立能力集
	b := loadAgentCapabilitiesForOwner(ctx, auditAgentID, auditUserB)
	if !containsString(b, "action_shared_only") {
		t.Fatalf("owner B capabilities should match its connection (异构 connector 不能被主 key 覆盖), got %v", b)
	}
	if containsString(b, "exec_approve") {
		t.Fatalf("owner B should NOT see A's capabilities, got %v", b)
	}

	// 不存在的 owner 回退主 key
	c := loadAgentCapabilitiesForOwner(ctx, auditAgentID, auditUserC)
	if !containsString(c, "exec_approve") {
		t.Fatalf("unknown owner should fall back to main key, got %v", c)
	}

	// 连接断时清 owner capabilities
	m.clearAgentRouteForOwner(auditAgentID, auditUserB)
	if data, err := store.RDB.Get(ctx, agentCapabilitiesKeyForOwner(auditAgentID, auditUserB)).Bytes(); err == nil && len(data) > 0 {
		t.Fatalf("owner B capabilities should be cleared on disconnect, got %s", string(data))
	}
}

// C3: sendShareRevokedNotice 对非 hermes 走 "kicked"; 对 hermes 走 error(在 hermes 服务端白名单内)。
// 都必须能让 connector 知道是被服务端踢而非纯网络断。
func TestGuardC3_ShareRevokedNoticeHermesUsesError(t *testing.T) {
	c := auditConn(auditAgentID, auditOwnerA, true) // 非 hermes
	sendShareRevokedNotice(c, "share_revoked")

	select {
	case raw := <-c.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("decode packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdKicked {
			t.Fatalf("non-hermes conn should receive %q, got %q", protocol.CmdKicked, pkt.Cmd)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("non-hermes conn should receive kicked")
	}

	// hermes 模拟:clientType=hermes,该路径走 error
	cH := auditConn(auditAgentID, auditUserB, false)
	cH.clientType = "hermes"
	sendShareRevokedNotice(cH, "share_revoked")
	select {
	case raw := <-cH.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("decode packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdError {
			t.Fatalf("hermes conn should receive %q (kicked not in its allowlist), got %q", protocol.CmdError, pkt.Cmd)
		}
		var nack SendNackPayload
		if err := json.Unmarshal(pkt.Payload, &nack); err != nil {
			t.Fatalf("decode nack payload: %v", err)
		}
		if nack.Code != protocol.CodeUnauthorized {
			t.Fatalf("hermes error code should be CodeUnauthorized(%d), got %d", protocol.CodeUnauthorized, nack.Code)
		}
		if nack.Msg != "share_revoked" {
			t.Fatalf("hermes error msg should carry reason, got %q", nack.Msg)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("hermes conn should receive error packet")
	}
}

// ---------- helpers ----------

func containsString(arr []string, target string) bool {
	for _, s := range arr {
		if s == target {
			return true
		}
	}
	return false
}

// 显式引用避免未用导入(部分常量 / 类型在 helper 中被引用但 gofmt 看不到)
var _ = fmt.Sprintf

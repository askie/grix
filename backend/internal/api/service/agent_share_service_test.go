package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

const (
	shareOwnerA = int64(82001) // agent 主人
	shareUserB  = int64(82002) // 被共享者
	shareUserC  = int64(82003) // 无关账户
	shareAgentX = int64(91001)
)

func setupShareTest(t *testing.T) func() {
	t.Helper()
	testDB := testutil.NewTestDB()
	originalDB := store.DB
	store.DB = testDB.DB
	originalRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	cleanup := func() {
		featuregate.InvalidateCache()
		store.DB = originalDB
		store.RDB = originalRDB
		testDB.Close()
	}
	// 共享前置：被共享者必须是有效账户，AgentShareCreate 会校验 user 存在与状态。
	// shareUserB 是被共享对象；shareUserC 也建出来留作"有效但未被授权"的对照组。
	for _, uid := range []int64{shareUserB, shareUserC} {
		if err := store.DB.Create(&model.User{
			ID:           uid,
			Username:     "share_user_" + strconv.FormatInt(uid, 10),
			Email:        "share_user_" + strconv.FormatInt(uid, 10) + "@example.com",
			PasswordHash: "x",
			AuthProvider: "local",
			Status:       model.UserStatusActive,
		}).Error; err != nil {
			cleanup()
			t.Fatalf("seed user %d: %v", uid, err)
		}
	}
	agent := model.Agent{
		ID:           shareAgentX,
		AgentName:    "share-agent",
		OwnerID:      shareOwnerA,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		cleanup()
		t.Fatalf("create agent: %v", err)
	}
	// 共享接口受 feature gate `agent_share` 保护,测试默认开启全量,
	// 这样既不绕过 gate 校验,也覆盖了 gate 开启时的真实行为。
	if err := featuregate.SaveGate(agentShareGateKey, "Agent 共享", model.FeatureStatusEnabled); err != nil {
		cleanup()
		t.Fatalf("seed feature gate: %v", err)
	}
	return cleanup
}

func TestCanUseAgent_OwnerAndSharedAndStranger(t *testing.T) {
	defer setupShareTest(t)()

	// 主人本人可用
	if ok, err := canUseAgent(shareOwnerA, shareAgentX); err != nil || !ok {
		t.Fatalf("owner should be able to use agent, ok=%v err=%v", ok, err)
	}
	// 未共享前，B 不可用
	if ok, err := canUseAgent(shareUserB, shareAgentX); err != nil || ok {
		t.Fatalf("non-shared user must NOT use agent, ok=%v err=%v", ok, err)
	}
	// 共享给 B 后，B 可用
	if ec := AgentShareCreate(shareOwnerA, shareAgentX, shareUserB); ec != nil {
		t.Fatalf("share create: %+v", ec)
	}
	if ok, err := canUseAgent(shareUserB, shareAgentX); err != nil || !ok {
		t.Fatalf("shared user should be able to use agent, ok=%v err=%v", ok, err)
	}
	// 无关账户 C 始终不可用
	if ok, err := canUseAgent(shareUserC, shareAgentX); err != nil || ok {
		t.Fatalf("stranger must NOT use agent, ok=%v err=%v", ok, err)
	}
}

func TestAgentShareCreate_Idempotent(t *testing.T) {
	defer setupShareTest(t)()

	if ec := AgentShareCreate(shareOwnerA, shareAgentX, shareUserB); ec != nil {
		t.Fatalf("first share: %+v", ec)
	}
	if ec := AgentShareCreate(shareOwnerA, shareAgentX, shareUserB); ec != nil {
		t.Fatalf("second share (idempotent): %+v", ec)
	}
	var count int64
	store.DB.Model(&model.AgentShare{}).
		Where("agent_id = ? AND shared_to = ? AND status = ?", shareAgentX, shareUserB, model.AgentShareStatusActive).
		Count(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 active share after idempotent create, got %d", count)
	}
}

func TestAgentShareCreate_RejectsNonOwnerAndSelf(t *testing.T) {
	defer setupShareTest(t)()

	// 非主人不能共享(注意:gate 检查走在归属检查之前,以发起者 C 自己的 gate 状态评估;
	// C 未开 gate 会先在 gate 关被拒,这里给 C 开一遍 gate 后再验证"归属拒绝"语义。)
	if err := featuregate.SaveGate(agentShareGateKey, "Agent 共享", model.FeatureStatusEnabled); err != nil {
		t.Fatalf("ensure gate: %v", err)
	}
	if ec := AgentShareCreate(shareUserC, shareAgentX, shareUserB); ec == nil || ec.BizCode != errcode.ErrAgentForbidden.BizCode {
		t.Fatalf("non-owner share should be forbidden, got %+v", ec)
	}
	// 不能共享给自己
	if ec := AgentShareCreate(shareOwnerA, shareAgentX, shareOwnerA); ec == nil || ec.BizCode != errcode.ErrBadRequest.BizCode {
		t.Fatalf("self share should be bad request, got %+v", ec)
	}
}

// 共享对象必须是真实存在的账户:防止 A 把 agent 共享给孤儿/已封禁/已注销的 user_id,
// 否则 connector 端会为该 user_id 维护一条永远没人用的 shared 实例,白白占资源。
func TestAgentShareCreate_RejectsNonexistentOrInactiveUser(t *testing.T) {
	defer setupShareTest(t)()

	// 完全不存在的 user_id 被拒
	const ghostUserID = int64(82099)
	if ec := AgentShareCreate(shareOwnerA, shareAgentX, ghostUserID); ec == nil || ec.BizCode != errcode.ErrBadRequest.BizCode {
		t.Fatalf("share to nonexistent user should be bad request, got %+v", ec)
	}

	// 存在但被封禁的账户也被拒
	const bannedUserID = int64(82098)
	if err := store.DB.Create(&model.User{
		ID:           bannedUserID,
		Username:     "share_user_banned",
		Email:        "share_user_banned@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Status:       model.UserStatusBanned,
	}).Error; err != nil {
		t.Fatalf("seed banned user: %v", err)
	}
	if ec := AgentShareCreate(shareOwnerA, shareAgentX, bannedUserID); ec == nil || ec.BizCode != errcode.ErrBadRequest.BizCode {
		t.Fatalf("share to banned user should be bad request, got %+v", ec)
	}
}

// gate 关闭/未创建时,即使是主人也不能直调 API 创建共享(防 curl/老前端绕过)。
func TestAgentShareCreate_RequiresFeatureGate(t *testing.T) {
	defer setupShareTest(t)()
	// 把 gate 切回 disabled(白名单为空也算关)
	if err := featuregate.SaveGate(agentShareGateKey, "Agent 共享", model.FeatureStatusDisabled); err != nil {
		t.Fatalf("disable gate: %v", err)
	}
	featuregate.InvalidateCache()
	if ec := AgentShareCreate(shareOwnerA, shareAgentX, shareUserB); ec == nil || ec.BizCode != errcode.ErrAgentForbidden.BizCode {
		t.Fatalf("create without gate must be forbidden, got %+v", ec)
	}
}

func TestAgentShareRevoke_RemovesAccess(t *testing.T) {
	defer setupShareTest(t)()

	if ec := AgentShareCreate(shareOwnerA, shareAgentX, shareUserB); ec != nil {
		t.Fatalf("share: %+v", ec)
	}
	if ec := AgentShareRevoke(shareOwnerA, shareAgentX, shareUserB); ec != nil {
		t.Fatalf("revoke: %+v", ec)
	}
	if ok, err := canUseAgent(shareUserB, shareAgentX); err != nil || ok {
		t.Fatalf("revoked user must lose access, ok=%v err=%v", ok, err)
	}
	// 非主人不能撤销
	if ec := AgentShareRevoke(shareUserC, shareAgentX, shareUserB); ec == nil || ec.BizCode != errcode.ErrAgentForbidden.BizCode {
		t.Fatalf("non-owner revoke should be forbidden, got %+v", ec)
	}
}

func TestAgentSharedWithMe_ListsSharedAgents(t *testing.T) {
	defer setupShareTest(t)()

	// 共享前，B 的「分享给我的」为空
	before, err := AgentSharedWithMe(shareUserB)
	if err != nil {
		t.Fatalf("shared-with-me before: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("expected 0 shared agents before, got %d", len(before))
	}
	if ec := AgentShareCreate(shareOwnerA, shareAgentX, shareUserB); ec != nil {
		t.Fatalf("share: %+v", ec)
	}
	after, err := AgentSharedWithMe(shareUserB)
	if err != nil {
		t.Fatalf("shared-with-me after: %v", err)
	}
	if len(after) != 1 || after[0].ID != shareAgentX {
		t.Fatalf("expected agent %d in shared-with-me, got %+v", shareAgentX, after)
	}
	// 安全守卫:被共享者侧不应拿到主人的 user_id（最小信息原则）。
	if after[0].OwnerID != 0 {
		t.Fatalf("shared-with-me must NOT leak owner_id to sharee; got %d", after[0].OwnerID)
	}
}

// 主人删除 agent 时，应级联把 agent_shares 行 status 改为撤销，
// 避免孤儿记录残留 + 让被共享者侧的连接同步被踢。
func TestAgentDelete_CascadesAgentShares(t *testing.T) {
	defer setupShareTest(t)()

	if ec := AgentShareCreate(shareOwnerA, shareAgentX, shareUserB); ec != nil {
		t.Fatalf("share: %+v", ec)
	}
	var active int64
	store.DB.Model(&model.AgentShare{}).
		Where("agent_id = ? AND status = ?", shareAgentX, model.AgentShareStatusActive).
		Count(&active)
	if active != 1 {
		t.Fatalf("share row should be active before delete, got %d", active)
	}

	if ec := AgentDelete(shareOwnerA, shareAgentX); ec != nil {
		t.Fatalf("delete agent: %+v", ec)
	}

	store.DB.Model(&model.AgentShare{}).
		Where("agent_id = ? AND status = ?", shareAgentX, model.AgentShareStatusActive).
		Count(&active)
	if active != 0 {
		t.Fatalf("active shares must be cleared after agent delete, got %d", active)
	}
}

func TestValidateDirectSessionAgentPeer_AllowsSharedRejectsStranger(t *testing.T) {
	defer setupShareTest(t)()

	if ec := AgentShareCreate(shareOwnerA, shareAgentX, shareUserB); ec != nil {
		t.Fatalf("share: %+v", ec)
	}
	// 主人可建私聊
	if err := validateDirectSessionAgentPeer(shareOwnerA, shareAgentX); err != nil {
		t.Fatalf("owner should build direct session: %v", err)
	}
	// 被共享者可建私聊
	if err := validateDirectSessionAgentPeer(shareUserB, shareAgentX); err != nil {
		t.Fatalf("shared user should build direct session: %v", err)
	}
	// 无关账户被拒
	if err := validateDirectSessionAgentPeer(shareUserC, shareAgentX); !errors.Is(err, ErrMemberAgentNotOwned) {
		t.Fatalf("stranger should be rejected with ErrMemberAgentNotOwned, got %v", err)
	}
}

// B3 守卫: loadAgentRouteAllNodesForShareSync 必须扫到所有 owner 路由节点并去重。
// 撤销共享通知依赖此函数广播到每个 owner 连接所在的节点;漏一个节点该节点上的失授权连接
// 不会被踢,直到 connector 主连接补推或本地心跳过期。
func TestGuardB3_LoadAgentRouteAllNodesForShareSync_CoversOwnerNodes(t *testing.T) {
	defer setupShareTest(t)()
	ctx := context.Background()

	// 模拟:主路由在 node-1,共享给 B 的连接也在 node-1(同节点),C 的连接在 node-2(跨节点)
	if err := store.RDB.Set(ctx, fmt.Sprintf("im:agent_api:route:%d", shareAgentX), "node-1", time.Minute).Err(); err != nil {
		t.Fatalf("set main route: %v", err)
	}
	if err := store.RDB.Set(ctx, fmt.Sprintf("im:agent_api:route:%d:%d", shareAgentX, shareUserB), "node-1", time.Minute).Err(); err != nil {
		t.Fatalf("set owner B route: %v", err)
	}
	if err := store.RDB.Set(ctx, fmt.Sprintf("im:agent_api:route:%d:%d", shareAgentX, shareUserC), "node-2", time.Minute).Err(); err != nil {
		t.Fatalf("set owner C route: %v", err)
	}
	store.RDB.SAdd(ctx, fmt.Sprintf("im:agent_api:route_owners:%d", shareAgentX),
		strconv.FormatInt(shareUserB, 10),
		strconv.FormatInt(shareUserC, 10),
	)

	nodes := loadAgentRouteAllNodesForShareSync(ctx, shareAgentX)
	got := map[string]int{}
	for _, n := range nodes {
		got[n]++
	}
	if got["node-1"] == 0 {
		t.Fatalf("nodes must include node-1, got %v", nodes)
	}
	if got["node-2"] == 0 {
		t.Fatalf("nodes must include node-2 (C 所在节点必须被广播,否则撤销不到), got %v", nodes)
	}
	if got["node-1"] != 1 {
		t.Fatalf("node-1 should appear exactly once after dedup, got %d in %v", got["node-1"], nodes)
	}
}

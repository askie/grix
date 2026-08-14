package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	connSecSvcOwner    = int64(87001)
	connSecSvcStranger = int64(87002)
	connSecSvcAgent    = int64(95001)
)

func setupConnSecSvcTest(t *testing.T) {
	t.Helper()
	logger.Init()
	config.C.JWT.Secret = "test-test-test-test-test-test-test-test"
	config.C.Server.AgentIPRuleHmacSecret = ""
	testDB := testutil.NewTestDB()
	origDB, origRDB := store.DB, store.RDB
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		store.DB = origDB
		store.RDB = origRDB
		testDB.Close()
	})
	require.NoError(t, store.DB.Create(&model.Agent{
		ID:      connSecSvcAgent,
		OwnerID: connSecSvcOwner,
	}).Error)
}

func seedOnlineConn(t *testing.T, agentID, ownerID int64, ip, nodeID string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, store.RDB.SAdd(ctx, pkgagentapi.RouteOwnerSetKey(agentID), ownerID).Err())
	require.NoError(t, store.RDB.Set(ctx, pkgagentapi.RouteKeyForOwner(agentID, ownerID), nodeID, time.Minute).Err())
	info := pkgagentapi.ConnInfo{
		LogID:       ownerID * 10,
		AgentID:     agentID,
		OwnerID:     ownerID,
		IsPrimary:   true,
		ClientIP:    ip,
		IPLocation:  "中国 江苏省 南京市",
		NodeID:      nodeID,
		ConnectedAt: time.Now().UnixMilli(),
	}
	raw, _ := json.Marshal(info)
	require.NoError(t, store.RDB.Set(ctx, pkgagentapi.ConnInfoKey(agentID, ownerID), raw, time.Minute).Err())
}

func TestAgentOnlineConnectionsOwnership(t *testing.T) {
	setupConnSecSvcTest(t)
	seedOnlineConn(t, connSecSvcAgent, connSecSvcOwner, "114.114.114.114", "node-a")

	// 属主可见
	items, ec := AgentOnlineConnections(connSecSvcOwner, connSecSvcAgent)
	require.Nil(t, ec)
	require.Len(t, items, 1)
	assert.Equal(t, "114.114.114.114", items[0].ClientIP)

	// 非属主 403
	_, ec = AgentOnlineConnections(connSecSvcStranger, connSecSvcAgent)
	require.NotNil(t, ec)
	assert.Equal(t, errcode.ErrAgentForbidden.BizCode, ec.BizCode)
}

func TestAgentIPRuleCRUD(t *testing.T) {
	setupConnSecSvcTest(t)
	meta := AuditMeta{ActorID: connSecSvcOwner, ClientIP: "1.2.3.4"}

	rule, ec := AgentIPRuleCreate(connSecSvcOwner, connSecSvcAgent, AgentIPRuleCreateReq{
		RuleType: "ban", IPCIDR: "203.0.113.9/24", Remark: "test",
	}, meta)
	require.Nil(t, ec)
	assert.Equal(t, "203.0.113.0/24", rule.IPCIDR, "CIDR 应归一化")
	assert.NotEmpty(t, rule.Signature)

	// 重复创建 → 已存在
	_, ec = AgentIPRuleCreate(connSecSvcOwner, connSecSvcAgent, AgentIPRuleCreateReq{
		RuleType: "ban", IPCIDR: "203.0.113.0/24",
	}, meta)
	require.NotNil(t, ec)
	assert.Equal(t, errcode.ErrAgentIPRuleExists.BizCode, ec.BizCode)

	// 非法类型 / 非法 IP
	_, ec = AgentIPRuleCreate(connSecSvcOwner, connSecSvcAgent, AgentIPRuleCreateReq{RuleType: "block", IPCIDR: "1.2.3.4"}, meta)
	require.NotNil(t, ec)
	_, ec = AgentIPRuleCreate(connSecSvcOwner, connSecSvcAgent, AgentIPRuleCreateReq{RuleType: "ban", IPCIDR: "bad"}, meta)
	require.NotNil(t, ec)

	// 列表
	items, ec := AgentIPRuleListSvc(connSecSvcOwner, connSecSvcAgent)
	require.Nil(t, ec)
	require.Len(t, items, 1)

	// 越权删除被拒
	ecDel := AgentIPRuleDelete(connSecSvcStranger, connSecSvcAgent, rule.ID, meta)
	require.NotNil(t, ecDel)

	// 属主删除成功
	ecDel = AgentIPRuleDelete(connSecSvcOwner, connSecSvcAgent, rule.ID, meta)
	require.Nil(t, ecDel)
	items, _ = AgentIPRuleListSvc(connSecSvcOwner, connSecSvcAgent)
	assert.Len(t, items, 0)

	// 审计已写
	var auditCount int64
	store.DB.Model(&model.AuditLog{}).Where("event_type IN ?", []string{"agent_ip_rule_create", "agent_ip_rule_delete"}).Count(&auditCount)
	assert.GreaterOrEqual(t, auditCount, int64(2))
}

func TestAgentConnectionKickBanIP(t *testing.T) {
	setupConnSecSvcTest(t)
	seedOnlineConn(t, connSecSvcAgent, connSecSvcOwner, "203.0.113.7", "node-a")
	meta := AuditMeta{ActorID: connSecSvcOwner, ClientIP: "1.2.3.4"}

	// 订阅 node-a 的踢线频道，验证跨节点指令发布
	sub := store.RDB.Subscribe(context.Background(), "chan:node-a")
	defer sub.Close()
	_, err := sub.Receive(context.Background())
	require.NoError(t, err)

	resp, ec := AgentConnectionKick(connSecSvcOwner, connSecSvcAgent, AgentConnectionKickReq{
		OwnerID: connSecSvcOwner,
		BanIP:   true,
		Remark:  "偷跑",
	}, meta)
	require.Nil(t, ec)
	assert.Equal(t, []int64{connSecSvcOwner}, resp.KickedOwners)
	assert.Equal(t, []string{"203.0.113.7"}, resp.BannedIPs)

	// 封禁规则已生成且带有效签名
	var rules []model.AgentIPRule
	require.NoError(t, store.DB.Where("agent_id = ?", connSecSvcAgent).Find(&rules).Error)
	require.Len(t, rules, 1)
	assert.Equal(t, model.AgentIPRuleTypeBan, rules[0].RuleType)
	assert.Equal(t, "203.0.113.7", rules[0].IPCIDR)

	// 收到 kick_agent 指令且带 owner_id
	select {
	case msg := <-sub.Channel():
		assert.Contains(t, msg.Payload, "kick_agent")
		assert.Contains(t, msg.Payload, `"owner_id":"87001"`)
	case <-time.After(2 * time.Second):
		t.Fatal("未收到 kick_agent 发布")
	}

	// 非属主不可踢
	_, ec = AgentConnectionKick(connSecSvcStranger, connSecSvcAgent, AgentConnectionKickReq{}, meta)
	require.NotNil(t, ec)
}

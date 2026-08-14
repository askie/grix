package security

import (
	"testing"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ipGuardTestIDSeq int64 = 900000

func setupIPGuardTest(t *testing.T) {
	t.Helper()
	logger.Init()
	config.C.JWT.Secret = "test-jwt-secret-for-ip-guard-0123456789"
	config.C.Server.AgentIPRuleHmacSecret = ""
	testDB := testutil.NewTestDB()
	origDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = origDB
		testDB.Close()
	})
}

func insertRule(t *testing.T, agentID int64, ruleType, cidr string, sign bool) *model.AgentIPRule {
	t.Helper()
	ipGuardTestIDSeq++
	rule := &model.AgentIPRule{
		ID:       ipGuardTestIDSeq,
		AgentID:  agentID,
		RuleType: ruleType,
		IPCIDR:   cidr,
	}
	if sign {
		rule.Signature = SignAgentIPRule(agentID, ruleType, cidr)
	} else {
		rule.Signature = "forged-signature"
	}
	require.NoError(t, store.DB.Create(rule).Error)
	return rule
}

func TestSignAndVerifyAgentIPRule(t *testing.T) {
	setupIPGuardTest(t)
	sig := SignAgentIPRule(101, model.AgentIPRuleTypeBan, "203.0.113.7")
	assert.NotEmpty(t, sig)

	rule := &model.AgentIPRule{AgentID: 101, RuleType: model.AgentIPRuleTypeBan, IPCIDR: "203.0.113.7", Signature: sig}
	assert.True(t, VerifyAgentIPRule(rule))

	// 篡改 IP 后验签失败
	rule.IPCIDR = "203.0.113.8"
	assert.False(t, VerifyAgentIPRule(rule))
}

func TestIsAgentIPBanned(t *testing.T) {
	setupIPGuardTest(t)
	insertRule(t, 101, model.AgentIPRuleTypeBan, "203.0.113.7", true)
	insertRule(t, 0, model.AgentIPRuleTypeBan, "198.51.100.0/24", true)

	// agent 级单 IP 封禁
	assert.True(t, IsAgentIPBanned(101, "203.0.113.7"))
	assert.False(t, IsAgentIPBanned(101, "203.0.113.8"))
	// 其他 agent 不受 101 的规则影响
	assert.False(t, IsAgentIPBanned(102, "203.0.113.7"))
	// 全局 CIDR 封禁对所有 agent 生效
	assert.True(t, IsAgentIPBanned(101, "198.51.100.66"))
	assert.True(t, IsAgentIPBanned(102, "198.51.100.66"))
	assert.False(t, IsAgentIPBanned(102, "198.51.101.66"))
	// 空 IP 不封
	assert.False(t, IsAgentIPBanned(101, ""))
}

func TestTamperedRuleIgnored(t *testing.T) {
	setupIPGuardTest(t)
	// 伪造签名的封禁规则（模拟直改数据库）不生效
	insertRule(t, 101, model.AgentIPRuleTypeBan, "203.0.113.7", false)
	assert.False(t, IsAgentIPBanned(101, "203.0.113.7"))
}

func TestAgentIPAllowlistState(t *testing.T) {
	setupIPGuardTest(t)
	// 未配置白名单
	exists, matched := AgentIPAllowlistState(101, "203.0.113.7")
	assert.False(t, exists)
	assert.False(t, matched)

	insertRule(t, 101, model.AgentIPRuleTypeAllow, "203.0.113.0/24", true)
	exists, matched = AgentIPAllowlistState(101, "203.0.113.7")
	assert.True(t, exists)
	assert.True(t, matched)
	exists, matched = AgentIPAllowlistState(101, "8.8.8.8")
	assert.True(t, exists)
	assert.False(t, matched)
}

func TestNormalizeIPCIDR(t *testing.T) {
	got, err := NormalizeIPCIDR(" 203.0.113.7 ")
	require.NoError(t, err)
	assert.Equal(t, "203.0.113.7", got)

	got, err = NormalizeIPCIDR("198.51.100.9/24")
	require.NoError(t, err)
	assert.Equal(t, "198.51.100.0/24", got)

	_, err = NormalizeIPCIDR("not-an-ip")
	assert.Error(t, err)
	_, err = NormalizeIPCIDR("")
	assert.Error(t, err)
}

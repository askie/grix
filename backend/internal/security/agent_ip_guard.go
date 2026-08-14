package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"strings"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// agent IP 规则守卫：为 agent WS 握手提供 IP 封禁判断与规则防篡改验签。
//
// 防篡改模型：每条规则入库时用服务端密钥对 (agent_id, rule_type, ip_cidr) 算 HMAC
// 存入 signature 列；加载时逐条验签，签名不符（例如有人直改数据库）的规则
// 拒绝生效并打告警日志。签名防"改"，不防"删"——删除由审计日志追责。

// agentIPRuleSigningKey 派生 HMAC 密钥：专用密钥优先，空时回退 JWT secret，
// 域分隔避免同一 JWT secret 被跨用途复用（与 notification token 同一先例）。
func agentIPRuleSigningKey() []byte {
	secret := strings.TrimSpace(config.C.Server.AgentIPRuleHmacSecret)
	if secret == "" {
		secret = strings.TrimSpace(config.C.JWT.Secret)
	}
	sum := sha256.Sum256([]byte("agent-ip-rule:" + secret))
	return sum[:]
}

// SignAgentIPRule 计算规则签名（base64url，无填充）。
func SignAgentIPRule(agentID int64, ruleType, ipCIDR string) string {
	mac := hmac.New(sha256.New, agentIPRuleSigningKey())
	fmt.Fprintf(mac, "%d\n%s\n%s", agentID, strings.TrimSpace(ruleType), strings.TrimSpace(ipCIDR))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyAgentIPRule 验证规则签名是否有效。
func VerifyAgentIPRule(rule *model.AgentIPRule) bool {
	if rule == nil {
		return false
	}
	expected := SignAgentIPRule(rule.AgentID, rule.RuleType, rule.IPCIDR)
	return hmac.Equal([]byte(expected), []byte(rule.Signature))
}

// NormalizeIPCIDR 归一化单 IP / CIDR 输入；非法输入返回错误。
// 单 IP 归一化为标准字符串形式，CIDR 归一化为网络地址形式（如 1.2.3.9/24 → 1.2.3.0/24）。
func NormalizeIPCIDR(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("empty ip/cidr")
	}
	if strings.Contains(trimmed, "/") {
		_, network, err := net.ParseCIDR(trimmed)
		if err != nil {
			return "", fmt.Errorf("invalid cidr: %s", raw)
		}
		return network.String(), nil
	}
	parsed := net.ParseIP(trimmed)
	if parsed == nil {
		return "", fmt.Errorf("invalid ip: %s", raw)
	}
	return parsed.String(), nil
}

// matchIPRule 判断 ip 是否命中规则的 IP/CIDR。
func matchIPRule(ruleCIDR, ip string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return false
	}
	trimmedRule := strings.TrimSpace(ruleCIDR)
	if strings.Contains(trimmedRule, "/") {
		_, network, err := net.ParseCIDR(trimmedRule)
		if err != nil {
			return false
		}
		return network.Contains(parsed)
	}
	ruleIP := net.ParseIP(trimmedRule)
	return ruleIP != nil && ruleIP.Equal(parsed)
}

// loadValidAgentIPRules 加载对某 agent 生效的指定类型规则（含全局 agent_id=0），
// 验签失败的规则剔除并告警。
func loadValidAgentIPRules(agentID int64, ruleType string) []model.AgentIPRule {
	if store.DB == nil {
		return nil
	}
	var rules []model.AgentIPRule
	if err := store.DB.Where(
		"(agent_id = ? OR agent_id = 0) AND rule_type = ?",
		agentID, ruleType,
	).Find(&rules).Error; err != nil {
		logger.L.Errorf("agent ip rules load failed agent=%d type=%s err=%v", agentID, ruleType, err)
		return nil
	}
	valid := rules[:0]
	for i := range rules {
		if !VerifyAgentIPRule(&rules[i]) {
			logger.L.Errorf(
				"agent ip rule signature mismatch (possible tampering), rule ignored: id=%d agent=%d type=%s cidr=%s",
				rules[i].ID, rules[i].AgentID, rules[i].RuleType, rules[i].IPCIDR,
			)
			continue
		}
		valid = append(valid, rules[i])
	}
	return valid
}

// IsAgentIPBanned 判断 ip 是否被该 agent（或全局）封禁。命中即拒绝握手。
func IsAgentIPBanned(agentID int64, ip string) bool {
	if strings.TrimSpace(ip) == "" {
		return false
	}
	for _, rule := range loadValidAgentIPRules(agentID, model.AgentIPRuleTypeBan) {
		if matchIPRule(rule.IPCIDR, ip) {
			return true
		}
	}
	return false
}

// AgentIPAllowlistState 返回白名单状态：
// exists=该 agent 是否配置了白名单；matched=ip 是否命中。
// 阶段0 只用于记录/观测，不做拦截（严格模式留给后续阶段用开关放开）。
func AgentIPAllowlistState(agentID int64, ip string) (exists bool, matched bool) {
	rules := loadValidAgentIPRules(agentID, model.AgentIPRuleTypeAllow)
	if len(rules) == 0 {
		return false, false
	}
	for _, rule := range rules {
		if matchIPRule(rule.IPCIDR, ip) {
			return true, true
		}
	}
	return true, false
}

package model

import "time"

const (
	// AgentIPRuleTypeBan 封禁：命中即拒绝 WS 握手（阶段0 即强制生效）。
	AgentIPRuleTypeBan = "ban"
	// AgentIPRuleTypeAllow 白名单：阶段0 只记录是否命中，不拦截（严格模式留给后续阶段）。
	AgentIPRuleTypeAllow = "allow"
)

// AgentIPRule 是 agent WS 连接的 IP 规则（黑/白名单）。
// AgentID=0 表示全局规则，对所有 agent 生效。IPCIDR 支持单 IP 或 CIDR 网段。
// Signature 是服务端 HMAC 签名（见 security.SignAgentIPRule）：
// 加载时逐条验签，签名不符的规则拒绝生效并告警，防止直改数据库篡改规则。
// 签名只覆盖 (AgentID, RuleType, IPCIDR) 三个决定规则生效范围的字段，不含 Remark；
// 直改 Remark 不会触发防篡改告警。
type AgentIPRule struct {
	ID        int64     `gorm:"primaryKey" json:"id,string"`
	AgentID   int64     `gorm:"uniqueIndex:uq_agent_ip_rules_scope,priority:1;not null;default:0" json:"agent_id,string"`
	RuleType  string    `gorm:"size:16;uniqueIndex:uq_agent_ip_rules_scope,priority:2;not null" json:"rule_type"`
	IPCIDR    string    `gorm:"column:ip_cidr;size:64;uniqueIndex:uq_agent_ip_rules_scope,priority:3;not null" json:"ip_cidr"`
	Remark    string    `gorm:"size:255;not null;default:''" json:"remark"`
	CreatedBy int64     `gorm:"not null;default:0" json:"created_by,string"`
	Signature string    `gorm:"size:88;not null;default:''" json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (AgentIPRule) TableName() string { return "agent_ip_rules" }

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

// agent WS 连接安全管控（阶段0）：在线连接查询、连接日志、踢线（可连带封 IP）、
// IP 黑/白名单管理。owner 管自己的 agent；admin 侧复用 *ForAdmin 变体（免属主校验，
// 且可管理全局规则 agent_id=0）。

// AuditMeta 审计上下文（操作者来源 IP / UA），由 handler 从请求提取。
type AuditMeta struct {
	ActorID   int64
	ClientIP  string
	UserAgent string
}

func loadOwnedAgentForSecurity(userID, agentID int64) (*model.Agent, *errcode.ErrCode) {
	if agentID <= 0 {
		return nil, &errcode.ErrBadRequest
	}
	var agent model.Agent
	if err := store.DB.Select("id", "owner_id").First(&agent, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrAgentNotFound
		}
		return nil, &errcode.ErrInternal
	}
	if agent.OwnerID != userID {
		return nil, &errcode.ErrAgentForbidden
	}
	return &agent, nil
}

// --- 在线连接 ---

// AgentOnlineConnections 返回某 agent 当前所有在线连接的实时来源信息（跨节点）。
func AgentOnlineConnections(userID, agentID int64) ([]pkgagentapi.ConnInfo, *errcode.ErrCode) {
	if _, ec := loadOwnedAgentForSecurity(userID, agentID); ec != nil {
		return nil, ec
	}
	return agentOnlineConnections(agentID), nil
}

// AgentOnlineConnectionsForAdmin admin 侧免属主校验。
func AgentOnlineConnectionsForAdmin(agentID int64) []pkgagentapi.ConnInfo {
	return agentOnlineConnections(agentID)
}

func agentOnlineConnections(agentID int64) []pkgagentapi.ConnInfo {
	result := make([]pkgagentapi.ConnInfo, 0, 4)
	if store.RDB == nil || agentID <= 0 {
		return result
	}
	ctx := context.Background()
	owners, err := store.RDB.SMembers(ctx, pkgagentapi.RouteOwnerSetKey(agentID)).Result()
	if err != nil {
		return result
	}
	for _, ownerRaw := range owners {
		ownerID, err := strconv.ParseInt(strings.TrimSpace(ownerRaw), 10, 64)
		if err != nil || ownerID <= 0 {
			continue
		}
		raw, err := store.RDB.Get(ctx, pkgagentapi.ConnInfoKey(agentID, ownerID)).Result()
		if err != nil || raw == "" {
			continue
		}
		var info pkgagentapi.ConnInfo
		if err := json.Unmarshal([]byte(raw), &info); err != nil {
			continue
		}
		result = append(result, info)
	}
	return result
}

// --- 连接日志 ---

type AgentConnectionLogListResp struct {
	Total int64                      `json:"total"`
	Items []model.AgentConnectionLog `json:"items"`
}

// AgentConnectionLogList 分页拉取连接日志，可只筛异地记录。
func AgentConnectionLogList(userID, agentID int64, page, pageSize int, onlyGeoChanged bool) (*AgentConnectionLogListResp, *errcode.ErrCode) {
	if _, ec := loadOwnedAgentForSecurity(userID, agentID); ec != nil {
		return nil, ec
	}
	return agentConnectionLogList(agentID, page, pageSize, onlyGeoChanged)
}

// AgentConnectionLogListForAdmin admin 侧免属主校验。
func AgentConnectionLogListForAdmin(agentID int64, page, pageSize int, onlyGeoChanged bool) (*AgentConnectionLogListResp, *errcode.ErrCode) {
	return agentConnectionLogList(agentID, page, pageSize, onlyGeoChanged)
}

func agentConnectionLogList(agentID int64, page, pageSize int, onlyGeoChanged bool) (*AgentConnectionLogListResp, *errcode.ErrCode) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	query := store.DB.Model(&model.AgentConnectionLog{}).Where("agent_id = ?", agentID)
	if onlyGeoChanged {
		query = query.Where("geo_changed = ?", true)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	items := make([]model.AgentConnectionLog, 0, pageSize)
	if err := query.Order("connected_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&items).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	return &AgentConnectionLogListResp{Total: total, Items: items}, nil
}

// --- 踢线 ---

type AgentConnectionKickReq struct {
	// OwnerID > 0 只踢该 owner 的单条连接；为 0 踢该 agent 全部连接。
	OwnerID int64 `json:"owner_id,string"`
	// BanIP 为 true 时把被踢连接的来源 IP 加入该 agent 的封禁名单（踢线并封 IP）。
	BanIP  bool   `json:"ban_ip"`
	Remark string `json:"remark"`
}

type AgentConnectionKickResp struct {
	KickedOwners []int64  `json:"kicked_owners"`
	BannedIPs    []string `json:"banned_ips"`
}

// AgentConnectionKick 踢线（可连带封 IP）。
func AgentConnectionKick(userID, agentID int64, req AgentConnectionKickReq, meta AuditMeta) (*AgentConnectionKickResp, *errcode.ErrCode) {
	if _, ec := loadOwnedAgentForSecurity(userID, agentID); ec != nil {
		return nil, ec
	}
	return agentConnectionKick(agentID, req, meta)
}

// AgentConnectionKickForAdmin admin 侧免属主校验。
func AgentConnectionKickForAdmin(agentID int64, req AgentConnectionKickReq, meta AuditMeta) (*AgentConnectionKickResp, *errcode.ErrCode) {
	return agentConnectionKick(agentID, req, meta)
}

func agentConnectionKick(agentID int64, req AgentConnectionKickReq, meta AuditMeta) (*AgentConnectionKickResp, *errcode.ErrCode) {
	online := agentOnlineConnections(agentID)
	targets := online
	if req.OwnerID > 0 {
		targets = targets[:0]
		for _, info := range online {
			if info.OwnerID == req.OwnerID {
				targets = append(targets, info)
			}
		}
	}

	resp := &AgentConnectionKickResp{KickedOwners: make([]int64, 0, len(targets)), BannedIPs: make([]string, 0, 2)}

	// 先封 IP 再踢线，封禁即时生效，防止被踢连接立刻原 IP 重连。
	if req.BanIP {
		seen := make(map[string]struct{}, len(targets))
		for _, info := range targets {
			ip := strings.TrimSpace(info.ClientIP)
			if ip == "" {
				continue
			}
			if _, ok := seen[ip]; ok {
				continue
			}
			seen[ip] = struct{}{}
			if ec := createAgentIPRule(agentID, model.AgentIPRuleTypeBan, ip, req.Remark, meta); ec != nil && ec.BizCode != errcode.ErrAgentIPRuleExists.BizCode {
				return nil, ec
			}
			resp.BannedIPs = append(resp.BannedIPs, ip)
		}
	}

	reason := "kicked_by_owner"
	if req.OwnerID > 0 {
		publishAgentKick(agentID, req.OwnerID, reason)
	} else {
		publishAgentKickAllNodes(agentID, reason)
	}
	for _, info := range targets {
		resp.KickedOwners = append(resp.KickedOwners, info.OwnerID)
	}

	WriteAuditLog(WriteAuditLogReq{
		EventType: "agent_connection_kick",
		UserID:    &meta.ActorID,
		Detail: map[string]any{
			"agent_id":   fmt.Sprintf("%d", agentID),
			"owner_id":   fmt.Sprintf("%d", req.OwnerID),
			"ban_ip":     req.BanIP,
			"banned_ips": resp.BannedIPs,
			"remark":     req.Remark,
		},
		ClientIP:  meta.ClientIP,
		UserAgent: meta.UserAgent,
	})
	return resp, nil
}

// publishAgentKick 向某 owner 连接所在节点发布单连接踢线。
func publishAgentKick(agentID, ownerID int64, reason string) {
	if store.RDB == nil {
		return
	}
	ctx := context.Background()
	nodeID, err := store.RDB.Get(ctx, pkgagentapi.RouteKeyForOwner(agentID, ownerID)).Result()
	if err != nil || strings.TrimSpace(nodeID) == "" {
		return
	}
	publishKickToNode(ctx, nodeID, agentID, ownerID, reason)
}

// publishAgentKickAllNodes 向该 agent 所有有连接的节点广播整体踢线。
// 既有 publishKickAgent 只发主路由节点，共享连接散在其他节点时会漏踢，这里按 owner 路由全量覆盖。
func publishAgentKickAllNodes(agentID int64, reason string) {
	if store.RDB == nil {
		return
	}
	ctx := context.Background()
	nodes := make(map[string]struct{}, 2)
	if nodeID, err := store.RDB.Get(ctx, pkgagentapi.RouteKey(agentID)).Result(); err == nil && strings.TrimSpace(nodeID) != "" {
		nodes[strings.TrimSpace(nodeID)] = struct{}{}
	}
	owners, _ := store.RDB.SMembers(ctx, pkgagentapi.RouteOwnerSetKey(agentID)).Result()
	for _, ownerRaw := range owners {
		ownerID, err := strconv.ParseInt(strings.TrimSpace(ownerRaw), 10, 64)
		if err != nil || ownerID <= 0 {
			continue
		}
		if nodeID, err := store.RDB.Get(ctx, pkgagentapi.RouteKeyForOwner(agentID, ownerID)).Result(); err == nil && strings.TrimSpace(nodeID) != "" {
			nodes[strings.TrimSpace(nodeID)] = struct{}{}
		}
	}
	for nodeID := range nodes {
		publishKickToNode(ctx, nodeID, agentID, 0, reason)
	}
}

func publishKickToNode(ctx context.Context, nodeID string, agentID, ownerID int64, reason string) {
	payload := map[string]string{
		"agent_id": fmt.Sprintf("%d", agentID),
		"reason":   reason,
	}
	if ownerID > 0 {
		payload["owner_id"] = fmt.Sprintf("%d", ownerID)
	}
	rawPayload, _ := json.Marshal(payload)
	raw, _ := json.Marshal(map[string]any{
		"cmd":     "kick_agent",
		"payload": json.RawMessage(rawPayload),
	})
	_ = store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", nodeID), raw).Err()
}

// --- IP 规则（黑/白名单）---

type AgentIPRuleCreateReq struct {
	RuleType string `json:"rule_type" binding:"required"`
	IPCIDR   string `json:"ip_cidr" binding:"required"`
	Remark   string `json:"remark"`
}

// AgentIPRuleListSvc 列出该 agent 的 IP 规则（不含全局规则，全局规则走 admin）。
func AgentIPRuleListSvc(userID, agentID int64) ([]model.AgentIPRule, *errcode.ErrCode) {
	if _, ec := loadOwnedAgentForSecurity(userID, agentID); ec != nil {
		return nil, ec
	}
	return listAgentIPRules(agentID)
}

// AgentIPRuleListForAdmin admin 侧：agentID=0 返回全局规则。
func AgentIPRuleListForAdmin(agentID int64) ([]model.AgentIPRule, *errcode.ErrCode) {
	return listAgentIPRules(agentID)
}

func listAgentIPRules(agentID int64) ([]model.AgentIPRule, *errcode.ErrCode) {
	rules := make([]model.AgentIPRule, 0, 8)
	if err := store.DB.Where("agent_id = ?", agentID).
		Order("created_at DESC").Limit(500).Find(&rules).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	return rules, nil
}

// AgentIPRuleCreate owner 为自己的 agent 增加规则。
func AgentIPRuleCreate(userID, agentID int64, req AgentIPRuleCreateReq, meta AuditMeta) (*model.AgentIPRule, *errcode.ErrCode) {
	if _, ec := loadOwnedAgentForSecurity(userID, agentID); ec != nil {
		return nil, ec
	}
	return agentIPRuleCreate(agentID, req, meta)
}

// AgentIPRuleCreateForAdmin admin 侧：agentID=0 表示全局规则。
func AgentIPRuleCreateForAdmin(agentID int64, req AgentIPRuleCreateReq, meta AuditMeta) (*model.AgentIPRule, *errcode.ErrCode) {
	if agentID < 0 {
		return nil, &errcode.ErrBadRequest
	}
	return agentIPRuleCreate(agentID, req, meta)
}

func agentIPRuleCreate(agentID int64, req AgentIPRuleCreateReq, meta AuditMeta) (*model.AgentIPRule, *errcode.ErrCode) {
	ruleType := strings.TrimSpace(strings.ToLower(req.RuleType))
	if ruleType != model.AgentIPRuleTypeBan && ruleType != model.AgentIPRuleTypeAllow {
		return nil, &errcode.ErrBadRequest
	}
	if ec := createAgentIPRule(agentID, ruleType, req.IPCIDR, req.Remark, meta); ec != nil {
		return nil, ec
	}
	var rule model.AgentIPRule
	normalized, _ := security.NormalizeIPCIDR(req.IPCIDR)
	if err := store.DB.Where(
		"agent_id = ? AND rule_type = ? AND ip_cidr = ?", agentID, ruleType, normalized,
	).First(&rule).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	return &rule, nil
}

// agentIPRuleRemarkMaxLen 对应 migration 094 中 agent_ip_rules.remark VARCHAR(255)。
const agentIPRuleRemarkMaxLen = 255

// createAgentIPRule 归一化 + 签名 + 入库 + 审计。已存在同规则时返回 ErrAgentIPRuleExists。
func createAgentIPRule(agentID int64, ruleType, ipCIDR, remark string, meta AuditMeta) *errcode.ErrCode {
	normalized, err := security.NormalizeIPCIDR(ipCIDR)
	if err != nil {
		return &errcode.ErrBadRequest
	}
	trimmedRemark := strings.TrimSpace(remark)
	if len([]rune(trimmedRemark)) > agentIPRuleRemarkMaxLen {
		return &errcode.ErrBadRequest
	}
	rule := &model.AgentIPRule{
		ID:        snowflake.GenID(),
		AgentID:   agentID,
		RuleType:  ruleType,
		IPCIDR:    normalized,
		Remark:    trimmedRemark,
		CreatedBy: meta.ActorID,
		Signature: security.SignAgentIPRule(agentID, ruleType, normalized),
	}
	if err := store.DB.Create(rule).Error; err != nil {
		var existing model.AgentIPRule
		if lookupErr := store.DB.Where(
			"agent_id = ? AND rule_type = ? AND ip_cidr = ?", agentID, ruleType, normalized,
		).First(&existing).Error; lookupErr == nil {
			return &errcode.ErrAgentIPRuleExists
		}
		return &errcode.ErrInternal
	}
	WriteAuditLog(WriteAuditLogReq{
		EventType: "agent_ip_rule_create",
		UserID:    &meta.ActorID,
		Detail: map[string]any{
			"agent_id":  fmt.Sprintf("%d", agentID),
			"rule_type": ruleType,
			"ip_cidr":   normalized,
			"remark":    strings.TrimSpace(remark),
		},
		ClientIP:  meta.ClientIP,
		UserAgent: meta.UserAgent,
	})
	return nil
}

// AgentIPRuleDelete owner 删除自己 agent 的规则。
func AgentIPRuleDelete(userID, agentID, ruleID int64, meta AuditMeta) *errcode.ErrCode {
	if _, ec := loadOwnedAgentForSecurity(userID, agentID); ec != nil {
		return ec
	}
	return agentIPRuleDelete(agentID, ruleID, meta)
}

// AgentIPRuleDeleteForAdmin admin 侧：可删任意 agent 的规则与全局规则（agentID=0）。
func AgentIPRuleDeleteForAdmin(agentID, ruleID int64, meta AuditMeta) *errcode.ErrCode {
	return agentIPRuleDelete(agentID, ruleID, meta)
}

func agentIPRuleDelete(agentID, ruleID int64, meta AuditMeta) *errcode.ErrCode {
	if ruleID <= 0 {
		return &errcode.ErrBadRequest
	}
	var rule model.AgentIPRule
	if err := store.DB.Where("id = ? AND agent_id = ?", ruleID, agentID).First(&rule).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &errcode.ErrNotFound
		}
		return &errcode.ErrInternal
	}
	if err := store.DB.Delete(&model.AgentIPRule{}, rule.ID).Error; err != nil {
		return &errcode.ErrInternal
	}
	WriteAuditLog(WriteAuditLogReq{
		EventType: "agent_ip_rule_delete",
		UserID:    &meta.ActorID,
		Detail: map[string]any{
			"agent_id":  fmt.Sprintf("%d", agentID),
			"rule_type": rule.RuleType,
			"ip_cidr":   rule.IPCIDR,
			"rule_id":   fmt.Sprintf("%d", rule.ID),
		},
		ClientIP:  meta.ClientIP,
		UserAgent: meta.UserAgent,
	})
	return nil
}

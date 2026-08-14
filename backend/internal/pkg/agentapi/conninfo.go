package agentapi

import "fmt"

// agent WS 在线连接的 Redis key 与实时信息结构。
// ws 服务写入（随连接租约续期），api 服务的管控接口跨节点读取，
// 两边共用此处定义避免 key 格式漂移。

// ConnInfo 是某条在线 agent 连接的实时来源信息（阶段0 安全观测）。
type ConnInfo struct {
	LogID       int64  `json:"log_id,string"`
	AgentID     int64  `json:"agent_id,string"`
	OwnerID     int64  `json:"owner_id,string"`
	IsPrimary   bool   `json:"is_primary"`
	ClientType  string `json:"client_type"`
	ClientIP    string `json:"client_ip"`
	IPLocation  string `json:"ip_location"`
	NodeID      string `json:"node_id"`
	ConnectedAt int64  `json:"connected_at"` // unix ms
}

// ConnInfoKey 在线连接实时信息 key，值为 ConnInfo JSON，TTL 同连接租约。
func ConnInfoKey(agentID, ownerID int64) string {
	return fmt.Sprintf("im:agent_api:conninfo:%d:%d", agentID, ownerID)
}

// RouteKey agent 主连接路由 key（值为 nodeID）。
func RouteKey(agentID int64) string {
	return fmt.Sprintf("im:agent_api:route:%d", agentID)
}

// RouteKeyForOwner (agentID, ownerID) 维度的路由 key（值为 nodeID）。
func RouteKeyForOwner(agentID, ownerID int64) string {
	return fmt.Sprintf("im:agent_api:route:%d:%d", agentID, ownerID)
}

// RouteOwnerSetKey 该 agent 当前有连接的 ownerID 集合 key。
func RouteOwnerSetKey(agentID int64) string {
	return fmt.Sprintf("im:agent_api:route_owners:%d", agentID)
}

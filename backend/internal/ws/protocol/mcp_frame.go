package protocol

import "encoding/json"

const (
	// CmdMcpFrame 是 MCP 帧透传信封，双向使用：
	// - Agent WS 上行（Connector → 后端）
	// - Human WS 下行（后端 → APP）
	// - Human WS 上行（APP → 后端）
	// - Agent WS 下行（后端 → Connector）
	CmdMcpFrame = "mcp_frame"
)

// McpFramePayload 是 mcp_frame 报文的外层信封。
//
// 上行（Connector → 后端）携带业务 SessionID，后端据此按 (agentID, sessionID)
// 幂等映射出 McpSessionID；下行（后端 → APP）携带 McpSessionID，APP 回帧时原样
// 带回。Frame 整体透传，后端仅解析 method/id 以便对 tools/call 做授权介入（闸3）
// 及在拒绝时回 JSON-RPC error，不解析工具语义。
type McpFramePayload struct {
	McpSessionID string          `json:"mcp_session_id,omitempty"` // 不透明会话句柄（下行/APP 回帧使用）
	SessionID    string          `json:"session_id,omitempty"`     // 业务会话 id（Connector 上行每帧携带，后端据此映射会话）
	Frame        json.RawMessage `json:"frame"`                    // 原始 MCP JSON-RPC 帧
}

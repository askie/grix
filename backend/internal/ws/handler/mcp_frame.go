package handler

import (
	"encoding/json"

	"github.com/askie/grix/backend/internal/pkg/logger"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// HandleMcpFrame 处理从 APP（Human WS）上行的 mcp_frame 回帧，转发给对应 Connector。
func HandleMcpFrame(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.McpFramePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("[mcp-frame] invalid payload from app user=%d err=%v", conn.GetUserID(), err)
		return
	}

	mgr := wsagentapi.GetGlobalManager()
	if mgr == nil {
		logger.L.Warnf("[mcp-frame] agent api manager unavailable user=%d", conn.GetUserID())
		return
	}

	mgr.HandleMcpFrameFromApp(conn.GetUserID(), payload)
}

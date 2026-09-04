package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// 连接器管理指令：手机端没有本机 127 admin API，改用一台在线 agent 当通道，
// 由后端转成 connector_admin local_action 下发到该 agent 所在的 connector。
const (
	connectorAdminOpListInstallable = "list_installable"
	connectorAdminOpInstall         = "install"
	connectorAdminOpInstallProgress = "install_progress"
	connectorAdminOpAddAgent        = "add_agent"
	connectorAdminOpProbe           = "probe"
	// create_agent 由后端组合完成：先建 Agent 行拿 api_key，再下发 add_agent。
	connectorAdminOpCreateAgent = "create_agent"
)

// 写操作限频：同一主人每分钟最多这么多次装/建。读操作（列表/进度/探测）不限。
const (
	connectorAdminWriteRateWindow = time.Minute
	connectorAdminWriteRateLimit  = 10
)

// connectorAdminDispatch 是「下发给连接器并等回执」的唯一出口。做成变量是为了让
// 测试能注入连接器回执，覆盖"连接器受理了但业务失败（ok=false）"这条分支——
// 那条分支必须把刚建的 Agent 行回滚掉，而真连接是起不出来的。
var connectorAdminDispatch = func(
	mgr *wsagentapi.Manager,
	agentID, ownerID int64,
	op string,
	args map[string]any,
) (*wsagentapi.ConnectorAdminResult, error) {
	return mgr.SendConnectorAdminActionAndWait(agentID, ownerID, op, args)
}

var connectorAdminReadOps = map[string]bool{
	connectorAdminOpListInstallable: true,
	connectorAdminOpInstallProgress: true,
	connectorAdminOpProbe:           true,
}

var connectorAdminWriteOps = map[string]bool{
	connectorAdminOpInstall:     true,
	connectorAdminOpAddAgent:    true,
	connectorAdminOpCreateAgent: true,
}

// HandleAgentConnectorAdmin 处理 agent_connector_admin。
//
// 鉴权：请求者必须是该 agent 的主人本人；被共享者一律 forbidden。下发用
// SendConnectorAdminActionAndWait(agentID, ownerID=userID)，只落主连接，
// 不会落到共享连接上。
func HandleAgentConnectorAdmin(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	userID := conn.GetUserID()

	var payload protocol.AgentConnectorAdminPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		respondConnectorAdmin(conn, pkt.Seq, protocol.AgentConnectorAdminRespPayload{
			Error:     "invalid payload",
			ErrorCode: "invalid_payload",
		})
		return
	}

	op := strings.TrimSpace(payload.Op)
	if payload.AgentID == 0 || op == "" {
		respondConnectorAdmin(conn, pkt.Seq, protocol.AgentConnectorAdminRespPayload{
			Error:     "agent_id and op are required",
			ErrorCode: "invalid_payload",
		})
		return
	}
	if !connectorAdminReadOps[op] && !connectorAdminWriteOps[op] {
		respondConnectorAdmin(conn, pkt.Seq, protocol.AgentConnectorAdminRespPayload{
			Error:     "unknown op",
			ErrorCode: "invalid_payload",
		})
		return
	}

	// 只有主人本人可以用自己的 agent 当管理通道：被共享者拿到的是别人机器的
	// 安装/创建权限，必须挡死。AgentGet 对非主人返回 forbidden。
	agent, ec := apiservice.AgentGet(userID, payload.AgentID)
	if ec != nil || agent == nil {
		logger.L.Warnf("[connector-admin] forbidden user=%d agent=%d op=%s", userID, payload.AgentID, op)
		respondConnectorAdmin(conn, pkt.Seq, protocol.AgentConnectorAdminRespPayload{
			Error:     "forbidden",
			ErrorCode: "forbidden",
		})
		return
	}

	if connectorAdminWriteOps[op] {
		if !allowConnectorAdminWrite(userID) {
			respondConnectorAdmin(conn, pkt.Seq, protocol.AgentConnectorAdminRespPayload{
				Error:     "too many requests",
				ErrorCode: "rate_limited",
			})
			return
		}
		logger.L.Infof("[connector-admin] audit write user=%d channel_agent=%d op=%s args=%s",
			userID, payload.AgentID, op, connectorAdminAuditArgs(payload.Args))
	}

	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		respondConnectorAdmin(conn, pkt.Seq, protocol.AgentConnectorAdminRespPayload{
			Error:     "service unavailable",
			ErrorCode: "unavailable",
		})
		return
	}

	// 等回执最长 18s，必须离开读泵。
	seq := pkt.Seq
	go func() {
		var resp protocol.AgentConnectorAdminRespPayload
		if op == connectorAdminOpCreateAgent {
			resp = runConnectorAdminCreateAgent(mgr, userID, payload.AgentID, payload.Args)
		} else {
			resp = runConnectorAdminPassthrough(mgr, userID, payload.AgentID, op, payload.Args)
		}
		respondConnectorAdmin(conn, seq, resp)
	}()
}

func runConnectorAdminPassthrough(
	mgr *wsagentapi.Manager,
	userID, agentID int64,
	op string,
	args map[string]any,
) protocol.AgentConnectorAdminRespPayload {
	result, err := connectorAdminDispatch(mgr, agentID, userID, op, args)
	if err != nil {
		return connectorAdminErrorResp(err)
	}
	if result.ErrorCode != "" || result.Error != "" {
		return protocol.AgentConnectorAdminRespPayload{
			Error:     result.Error,
			ErrorCode: result.ErrorCode,
		}
	}
	return protocol.AgentConnectorAdminRespPayload{Result: result.Result}
}

// runConnectorAdminCreateAgent 由后端组合「建 Agent 行 → 下发 add_agent」。
// 下发失败就把刚建的 Agent 行删掉，不给用户留下一个连不上的孤儿 agent。
func runConnectorAdminCreateAgent(
	mgr *wsagentapi.Manager,
	userID, channelAgentID int64,
	args map[string]any,
) protocol.AgentConnectorAdminRespPayload {
	agentName := strings.TrimSpace(connectorAdminArgString(args, "agent_name"))
	if agentName == "" {
		return protocol.AgentConnectorAdminRespPayload{
			Error:     "agent_name is required",
			ErrorCode: "invalid_payload",
		}
	}
	clientType := strings.TrimSpace(connectorAdminArgString(args, "client_type"))

	created, ec := apiservice.AgentCreateAPIForOwner(userID, agentName, "", "", "", clientType, false)
	if ec != nil {
		return protocol.AgentConnectorAdminRespPayload{
			Error:     ec.Msg,
			ErrorCode: strconv.Itoa(ec.BizCode),
		}
	}

	wsURL := strings.TrimSpace(created.APIEndpoint)
	if wsURL == "" {
		// 服务端没配 agent-api 域名时连接器无从下手，不留孤儿行。
		rollbackConnectorAdminAgent(userID, created.ID, "missing api endpoint")
		return protocol.AgentConnectorAdminRespPayload{
			Error:     "agent api endpoint is not configured",
			ErrorCode: "unavailable",
		}
	}

	addArgs := map[string]any{
		"name":        created.AgentName,
		"ws_url":      wsURL,
		"agent_id":    strconv.FormatInt(created.ID, 10),
		"api_key":     created.APIKey,
		"client_type": clientType,
	}
	result, err := connectorAdminDispatch(mgr, channelAgentID, userID, connectorAdminOpAddAgent, addArgs)
	if err != nil {
		rollbackConnectorAdminAgent(userID, created.ID, err.Error())
		return connectorAdminErrorResp(err)
	}
	if result.ErrorCode != "" || result.Error != "" {
		rollbackConnectorAdminAgent(userID, created.ID, result.Error)
		return protocol.AgentConnectorAdminRespPayload{
			Error:     result.Error,
			ErrorCode: result.ErrorCode,
		}
	}

	logger.L.Infof("[connector-admin] audit created agent user=%d channel_agent=%d new_agent=%d client_type=%s",
		userID, channelAgentID, created.ID, clientType)

	// 只回客户端本来就能从 REST 创建接口拿到的字段。
	return protocol.AgentConnectorAdminRespPayload{Result: map[string]any{
		"agent_id":     strconv.FormatInt(created.ID, 10),
		"agent_name":   created.AgentName,
		"api_key":      created.APIKey,
		"api_key_hint": created.APIKeyHint,
		"api_endpoint": created.APIEndpoint,
		"client_type":  created.AgentClientType,
		"session_id":   created.SessionID,
	}}
}

func rollbackConnectorAdminAgent(userID, agentID int64, reason string) {
	logger.L.Warnf("[connector-admin] rollback created agent user=%d agent=%d reason=%s", userID, agentID, reason)
	if ec := apiservice.AgentDelete(userID, agentID); ec != nil {
		logger.L.Errorf("[connector-admin] rollback failed user=%d agent=%d biz=%d msg=%s",
			userID, agentID, ec.BizCode, ec.Msg)
	}
}

func connectorAdminErrorResp(err error) protocol.AgentConnectorAdminRespPayload {
	switch {
	case errors.Is(err, wsagentapi.ErrConnectorAdminUnsupported):
		return protocol.AgentConnectorAdminRespPayload{
			Error:     err.Error(),
			ErrorCode: wsagentapi.ConnectorAdminErrUnsupported,
		}
	case errors.Is(err, wsagentapi.ErrConnectorAdminAgentOffline):
		return protocol.AgentConnectorAdminRespPayload{
			Error:     err.Error(),
			ErrorCode: wsagentapi.ConnectorAdminErrOffline,
		}
	case errors.Is(err, wsagentapi.ErrConnectorAdminTimeout):
		return protocol.AgentConnectorAdminRespPayload{
			Error:     err.Error(),
			ErrorCode: wsagentapi.ConnectorAdminErrTimeout,
		}
	default:
		return protocol.AgentConnectorAdminRespPayload{
			Error:     err.Error(),
			ErrorCode: wsagentapi.ConnectorAdminErrFailed,
		}
	}
}

func respondConnectorAdmin(conn ConnInterface, seq int64, resp protocol.AgentConnectorAdminRespPayload) {
	conn.SendPayload(protocol.CmdAgentConnectorAdminResp, seq, resp)
}

// allowConnectorAdminWrite 按主人维度给写操作限频。Redis 不可用时放行——限频是
// 防滥用，不是安全边界，鉴权已经在上面做过了。
func allowConnectorAdminWrite(userID int64) bool {
	if store.RDB == nil || userID <= 0 {
		return true
	}
	ctx := context.Background()
	key := fmt.Sprintf("im:connector_admin:rate:%d", userID)
	count, err := store.RDB.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if count == 1 {
		store.RDB.Expire(ctx, key, connectorAdminWriteRateWindow)
	}
	return count <= connectorAdminWriteRateLimit
}

// connectorAdminAuditArgs 只把审计需要的字段写进日志，绝不落 api_key 之类的秘密。
func connectorAdminAuditArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	safe := make(map[string]any, len(args))
	for _, key := range []string{"agent_type", "agent_name", "client_type", "name"} {
		if value, ok := args[key]; ok {
			safe[key] = value
		}
	}
	encoded, err := json.Marshal(safe)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func connectorAdminArgString(args map[string]any, key string) string {
	if len(args) == 0 {
		return ""
	}
	value, _ := args[key].(string)
	return value
}

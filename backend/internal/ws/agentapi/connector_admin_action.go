package agentapi

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// ConnectorAdminActionType 是连接器管理指令的 local_action 类型。connector 守护进程
// 既是 local_action 的接收方，也是本地 admin API（127.0.0.1:<adminPort>）的宿主，
// 所以手机端「安装/创建 agent」不再需要直连 127，统一走这一条反向通道。
const ConnectorAdminActionType = "connector_admin"

// connectorAdminActionTimeout 是后端等 connector 回执的上限。install 在连接器侧是
// 异步受理（立即回 started），因此不会顶到这个上限；list/add_agent/probe 都是秒级。
const connectorAdminActionTimeout = 18 * time.Second

// 连接器管理指令的错误码。客户端据此区分「连接器太老」和一次性失败：
// unsupported → 提示升级连接器；offline → 提示该主机没有在线 agent。
const (
	ConnectorAdminErrOffline     = "offline"
	ConnectorAdminErrUnsupported = "unsupported"
	ConnectorAdminErrTimeout     = "timeout"
	ConnectorAdminErrFailed      = "failed"
)

var (
	ErrConnectorAdminAgentOffline = errors.New("agent not connected")
	ErrConnectorAdminUnsupported  = errors.New("connector does not support connector_admin")
	ErrConnectorAdminTimeout      = errors.New("connector did not respond in time")
)

// connectorAdminResponse 是 connector_admin local_action 的同步回执。
// 连接器回执体约定为 {ok, error, result}：ok=false 时 Error 取 error 字段，
// ok=true 时 Result 是该 op 对应的业务数据（原样透传给客户端）。
type connectorAdminResponse struct {
	Result    any
	ErrorCode string
	Error     string
}

// ConnectorAdminResult 是给 ws handler 用的对外结果类型。
type ConnectorAdminResult struct {
	Result    any
	ErrorCode string
	Error     string
}

func (m *Manager) handleConnectorAdminPendingResult(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) {
	if pending == nil || pending.connectorAdminResultCh == nil {
		return
	}
	resp := &connectorAdminResponse{}
	switch strings.TrimSpace(payload.Status) {
	case "ok":
		// 正常路径：连接器恒回 status=ok，成败一律看下面的 {ok, result, error} 信封。
	case "unsupported":
		resp.ErrorCode = ConnectorAdminErrUnsupported
		resp.Error = firstNonEmpty(strings.TrimSpace(payload.ErrorMsg), strings.TrimSpace(payload.ErrorCode), "connector does not support connector_admin")
		pending.connectorAdminResultCh <- resp
		return
	case "timeout":
		resp.ErrorCode = ConnectorAdminErrTimeout
		resp.Error = firstNonEmpty(strings.TrimSpace(payload.ErrorMsg), strings.TrimSpace(payload.ErrorCode), "connector timed out")
		pending.connectorAdminResultCh <- resp
		return
	default:
		resp.ErrorCode = firstNonEmpty(strings.TrimSpace(payload.ErrorCode), ConnectorAdminErrFailed)
		resp.Error = firstNonEmpty(strings.TrimSpace(payload.ErrorMsg), strings.TrimSpace(payload.ErrorCode), "connector_admin failed")
		pending.connectorAdminResultCh <- resp
		return
	}

	object := localActionResultObject(payload.Result)
	if object == nil {
		// 老实现可能直接把业务数据放在 result 上，没有 {ok,...} 信封；照原样透传。
		resp.Result = payload.Result
		pending.connectorAdminResultCh <- resp
		return
	}
	if ok, present := object["ok"].(bool); present && !ok {
		code, message := connectorAdminErrorFields(object)
		resp.ErrorCode = firstNonEmpty(code, ConnectorAdminErrFailed)
		resp.Error = firstNonEmpty(message, "connector_admin failed")
		pending.connectorAdminResultCh <- resp
		return
	}
	if inner, present := object["result"]; present {
		resp.Result = inner
	} else {
		resp.Result = object
	}
	pending.connectorAdminResultCh <- resp
}

// SendConnectorAdminActionAndWait 把一条连接器管理指令下发给 agentID 所在的 connector
// 并同步等待回执。ownerID 必须是该 agent 的主人（调用方负责鉴权），按 owner 精确路由，
// 绝不会落到被共享者的连接上。跨节点由 Redis 转发（见 local_action_forward.go）。
func (m *Manager) SendConnectorAdminActionAndWait(agentID, ownerID int64, op string, args map[string]any) (*ConnectorAdminResult, error) {
	if m == nil {
		return nil, ErrConnectorAdminAgentOffline
	}
	op = strings.TrimSpace(op)
	if agentID <= 0 || ownerID <= 0 || op == "" {
		return nil, ErrConnectorAdminAgentOffline
	}

	actionID := fmt.Sprintf("connector_admin:%d:%d", agentID, snowflake.GenID())
	// actor_id 是请求者 user_id，连接器按它记日志。
	params := map[string]any{"op": op, "actor_id": strconv.FormatInt(ownerID, 10)}
	if len(args) > 0 {
		params["args"] = args
	} else {
		params["args"] = map[string]any{}
	}

	ch := make(chan *connectorAdminResponse, 1)
	pending := &pendingLocalAction{
		actionID:               actionID,
		kind:                   ConnectorAdminActionType,
		agentID:                agentID,
		ownerID:                ownerID,
		actionType:             ConnectorAdminActionType,
		connectorAdminResultCh: ch,
	}
	action := protocol.LocalActionPayload{
		ActionID:   actionID,
		ActionType: ConnectorAdminActionType,
		Params:     params,
	}

	dispatchedAt := time.Now()
	if !m.sendLocalActionWithPendingForOwner(agentID, ownerID, action, pending) {
		// 送不出去有两种原因：agent 根本不在线，或者连接器版本太老没声明
		// connector_admin。用路由表区分，好让客户端给出正确提示。
		if !m.agentReachableForOwner(agentID, ownerID) {
			logger.L.Warnf("[connector-admin] agent offline agent=%d owner=%d op=%s", agentID, ownerID, op)
			return nil, ErrConnectorAdminAgentOffline
		}
		logger.L.Warnf("[connector-admin] action not declared agent=%d owner=%d op=%s", agentID, ownerID, op)
		return nil, ErrConnectorAdminUnsupported
	}

	timer := time.NewTimer(connectorAdminActionTimeout)
	defer timer.Stop()

	var resp *connectorAdminResponse
	select {
	case resp = <-ch:
	case <-m.stopping():
		// 本节点关停：连接已断，回包不会再来。但结果可能与关停信号同时就绪，
		// 先把已到的结果取走再放弃。
		select {
		case resp = <-ch:
		default:
			m.deletePendingLocalAction(actionID)
			return nil, ErrConnectorAdminTimeout
		}
	case <-timer.C:
		m.deletePendingLocalAction(actionID)
		logger.L.Warnf("[connector-admin] timeout agent=%d owner=%d op=%s waited=%dms",
			agentID, ownerID, op, time.Since(dispatchedAt).Milliseconds())
		return nil, ErrConnectorAdminTimeout
	}

	if resp == nil {
		return nil, ErrConnectorAdminTimeout
	}
	logger.L.Infof("[connector-admin] result agent=%d owner=%d op=%s waited=%dms err_code=%q",
		agentID, ownerID, op, time.Since(dispatchedAt).Milliseconds(), resp.ErrorCode)
	return &ConnectorAdminResult{Result: resp.Result, ErrorCode: resp.ErrorCode, Error: resp.Error}, nil
}

// agentReachableForOwner 判断该 owner 的 agent 连接是否存在（本节点连接或 Redis 路由）。
// 只用于把「送不出去」区分成 offline / unsupported，不作为下发前置条件。
func (m *Manager) agentReachableForOwner(agentID, ownerID int64) bool {
	if m == nil || agentID <= 0 || ownerID <= 0 {
		return false
	}
	if conn := m.lookupConnForOwner(agentID, ownerID); conn != nil {
		return true
	}
	return loadAgentRouteForOwner(context.Background(), agentID, ownerID) != ""
}

// connectorAdminErrorFields 从失败信封里取出错误码与错误文案。
// 连接器 4.8.0 回的是对象形态 {ok:false, error:{code, message}}；早期形态是
// error 为一个字符串、错误码另放在顶层 error_code 上。三种都要认——只认字符串的话，
// unsupported_op / remote_admin_disabled / forbidden 这些码会全部退化成 failed，
// 客户端就再也分不出"连接器太老"和一次性失败，"请升级连接器"的提示永远出不来。
func connectorAdminErrorFields(object map[string]any) (code string, message string) {
	switch raw := object["error"].(type) {
	case string:
		message = strings.TrimSpace(raw)
	case map[string]any:
		code = strings.TrimSpace(connectorAdminString(raw["code"]))
		message = strings.TrimSpace(connectorAdminString(raw["message"]))
	}
	if code == "" {
		code = strings.TrimSpace(connectorAdminString(object["error_code"]))
	}
	return code, message
}

func connectorAdminString(value any) string {
	s, _ := value.(string)
	return s
}

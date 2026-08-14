package errcode

import "net/http"

type ErrCode struct {
	HTTPStatus int
	BizCode    int
	Msg        string // 中文消息，通过 i18n.LocalizeMessage 自动转换为目标语言
}

var (
	Success             = ErrCode{http.StatusOK, 0, "success"}
	ErrUnauthorized     = ErrCode{http.StatusUnauthorized, 10001, "未授权或 Access Token 已过期"}
	ErrRefreshExpired   = ErrCode{http.StatusUnauthorized, 10002, "Refresh Token 已过期或被吊销"}
	ErrBadRequest       = ErrCode{http.StatusBadRequest, 10003, "请求参数验证错误"}
	ErrNotFound         = ErrCode{http.StatusNotFound, 10004, "资源不存在"}
	ErrRateLimited      = ErrCode{http.StatusTooManyRequests, 10005, "请求过于频繁，请稍后再试"}
	ErrInternal         = ErrCode{http.StatusInternalServerError, 50001, "服务端内部异常"}
	ErrContentViolation = ErrCode{http.StatusBadRequest, 40001, "内容违规"}

	// Agent errors
	ErrAgentNotFound           = ErrCode{http.StatusNotFound, 20001, "Agent 不存在"}
	ErrAgentNameExists         = ErrCode{http.StatusConflict, 20002, "同名 Agent 已存在"}
	ErrAgentForbidden          = ErrCode{http.StatusForbidden, 20003, "无权操作该 Agent"}
	ErrAgentLimitExceed        = ErrCode{http.StatusBadRequest, 20004, "Agent 数量超出限制"}
	ErrAgentInvalidType        = ErrCode{http.StatusBadRequest, 20005, "无效的 Provider 类型"}
	ErrAgentEndpointBad        = ErrCode{http.StatusBadRequest, 20006, "本地端点地址不合法"}
	ErrAgentCtxTooLarge        = ErrCode{http.StatusBadRequest, 20007, "Context 文件超过 64KB 限制"}
	ErrAgentScopeForbidden     = ErrCode{http.StatusForbidden, 20011, "agent scope forbidden"}
	ErrAgentScopeInvalid       = ErrCode{http.StatusBadRequest, 20012, "invalid agent scope"}
	ErrAgentScopeTargetInvalid = ErrCode{http.StatusBadRequest, 20013, "agent scope target invalid"}
	ErrAgentIPRuleExists       = ErrCode{http.StatusConflict, 20014, "IP 规则已存在"}

	// Gateway errors（大模型计费网关 C端自助）
	ErrGatewayKeyNotFound           = ErrCode{http.StatusNotFound, 26001, "虚拟Key不存在"}
	ErrGatewayKeyForbidden          = ErrCode{http.StatusForbidden, 26002, "无权操作该虚拟Key"}
	ErrGatewayUnsupportedClientType = ErrCode{http.StatusBadRequest, 26003, "该Agent类型暂不支持Grix中转"}
	ErrGatewayModelNotServable      = ErrCode{http.StatusBadRequest, 26004, "所选模型当前不可用，请从可用模型清单中选择"}
	ErrGatewayModelMapTooLarge      = ErrCode{http.StatusBadRequest, 26005, "模型映射数量或名称长度超出上限"}
	ErrGatewayRelayModelRequired    = ErrCode{http.StatusBadRequest, 26006, "该Agent类型启用中转必须选择模型"}
	ErrGatewayRelayStateConflict    = ErrCode{http.StatusConflict, 26007, "中转开关状态已被其他端修改，请刷新后重试"}
	ErrGatewayRelayStateDisabled    = ErrCode{http.StatusServiceUnavailable, 26008, "中转开关功能暂未开放"}

	// 自定义技能同步 errors（docs/architecture/38）
	ErrSkillNameRequired = ErrCode{http.StatusBadRequest, 27001, "技能名称不能为空"}
	ErrSkillNameTooLong  = ErrCode{http.StatusBadRequest, 27002, "技能名称过长（上限 100 字符）"}
	ErrSkillNameExists   = ErrCode{http.StatusConflict, 27003, "同名技能已存在"}
	ErrSkillNotFound     = ErrCode{http.StatusNotFound, 27004, "技能不存在"}
	ErrSkillContentEmpty = ErrCode{http.StatusBadRequest, 27005, "技能内容不能为空"}
	ErrSkillContentTooLarge = ErrCode{http.StatusBadRequest, 27006, "技能内容超过上限"}
	ErrSkillSystemReadonly  = ErrCode{http.StatusForbidden, 27007, "系统内置技能只读"}
	ErrSkillNameInvalid     = ErrCode{http.StatusBadRequest, 27008, "技能名称含非法字符（不能包含路径分隔符、..、前导点或控制字符）"}
	ErrSkillNameReserved    = ErrCode{http.StatusForbidden, 27009, "grix- 前缀技能为平台保留，不可上传"}
)

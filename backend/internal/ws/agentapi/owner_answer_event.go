package agentapi

import (
	"strings"

	"github.com/askie/grix/backend/internal/grixactions"
)

// owner_answer_event.go 判定「主人回答类事件」，与 no_reply.go 的协议事件判定并列。
//
// 主人在手机上点问答卡回复、或点审批卡放行时，回执内容会作为一条普通 event_msg
// 派给连接器（非 claude 家族的连接器没有 local_action 回包通道）。连接器几十毫秒
// 就回 responded —— 一旦把它当成新一轮任务 run，chat_states 会被这条秒回的"任务"
// 覆写成 completed，正在跑的那轮任务从此再也翻不回 waiting_question，手表首页的
// 待办因此永远为空（2026-09-05 CN 线上 e42b7055 会话实证）。
//
// 这类事件是对当前任务的回答，不是新任务：既不写 running，也不写终态。
func isOwnerAnswerEvent(evt DelegateEventPayload) bool {
	if evt.OwnerID <= 0 || evt.SenderID != evt.OwnerID {
		return false
	}
	return isOwnerAnswerContent(evt.Content)
}

// isOwnerAnswerContent 只认卡片与内部指令的固定形态（问答回复卡 URI、
// exec-approval-resolution / approve / deny 指令），不做任何文本启发式：
// 漏判只是少一次状态修正，误判会让真实任务从任务态里消失。
func isOwnerAnswerContent(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	if _, matched, _ := grixactions.ParseQuestionReply(trimmed); matched {
		return true
	}
	// 客户端渲染后的回执是 markdown 链接（"[已回复](grix://card/agent_question_reply?...)"），
	// 整串 url.Parse 认不出来，按内嵌 URI 再认一次。
	if _, ok := parseGrixCardURI(trimmed, "agent_question_reply"); ok {
		return true
	}
	if isHermesFallbackApprovalReply(trimmed) {
		return true
	}
	return parseExecApprovalCommand(trimmed).matched
}

// isHermesFallbackApprovalReply 认 hermes 兜底审批改写出来的纯文本裁决。
// rewriteHermesFallbackApprovalResolution 会把审批卡回传改写成这几条文本、
// 并跳过审批拦截器直接当普通消息投给连接器，所以它们同样是"主人的回答"。
// 其中 "/deny" 不是 exec approval 指令语法的一部分，parseExecApprovalCommand
// 认不出来，必须按改写产物本身判定 —— 直接取同一个生成函数的输出，避免两处漂移。
func isHermesFallbackApprovalReply(content string) bool {
	switch strings.TrimSpace(content) {
	case hermesFallbackApprovalTextReply("allow-once"),
		hermesFallbackApprovalTextReply("allow-always"),
		hermesFallbackApprovalTextReply("deny"):
		return true
	}
	return false
}

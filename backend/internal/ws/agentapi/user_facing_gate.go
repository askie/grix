package agentapi

import (
	"strings"
)

// 主动内部任务（当前只有客服 coach 快照）的用户可见性闸门。
//
// 这类事件不是用户发来的消息：用户没有提问，模型输出里的分析、计划、决策
// 自述一旦投递就是内部信息外泄。只靠提示词约束和 /no_reply 静默挡不住——
// 模型仍会先发一段自述，再发引导语，两条都到达用户。闸门把默认改成不投递：
// 只有模型显式用 /to_user 标出的正文才会发给用户，标记之前的文字一律丢弃。
const ToUserCommand = "/to_user"

const ToUserProtocolInstruction = `投递协议：本次任务的输出默认不会发给用户。要发给用户的正文，必须另起一行以 /to_user 开头；服务端会去掉这个标记，把它后面的内容作为消息发出去。标记之前的任何文字（分析、计划、决策说明）都会被丢弃，绝不会到达用户。全文只写一次 /to_user，正文写在标记之后，正文之后不要再写任何内容——标记之后的内容会原样发给用户。不需要给用户发消息时，不要写 /to_user。`

// AppendToUserProtocolInstruction 把投递协议追加到内部任务正文末尾。
// 去重按指令原文判定：任务正文里出现 /to_user 字面量（例如提示词里提到该协议）
// 不能让指令静默不挂，否则闸门照常生效而模型无从得知协议，输出会被全吞。
func AppendToUserProtocolInstruction(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || strings.Contains(trimmed, ToUserProtocolInstruction) {
		return trimmed
	}
	return trimmed + "\n\n" + ToUserProtocolInstruction
}

// ShouldGateUserFacingOutput 判断某个下发事件的输出是否受闸门约束。
func ShouldGateUserFacingOutput(evt DelegateEventPayload) bool {
	return isUserFacingGatedEvent(evt)
}

func (m *Manager) IsUserFacingGatedContext(eventID string) bool {
	return m.matchDelegateEventContext(eventID, isUserFacingGatedEventID, isUserFacingGatedEvent)
}

func IsUserFacingGatedContext(eventID string) bool {
	if mgr := GetGlobal(); mgr != nil {
		return mgr.IsUserFacingGatedContext(eventID)
	}
	return isUserFacingGatedEventID(eventID)
}

// ResolveUserFacingGateEventID 在连接器漏传 event_id 时按 owner+session 回落到
// 当前 active run。闸门以 eventID 为键，空 eventID 会让闸门整个失效，而这类
// 输出恰恰是最需要拦住的。
func ResolveUserFacingGateEventID(eventID string, ownerID int64, sessionID string) string {
	if trimmed := strings.TrimSpace(eventID); trimmed != "" {
		return trimmed
	}
	mgr := GetGlobal()
	if mgr == nil || ownerID <= 0 || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	run := mgr.LookupActiveRunBySessionOwner(ownerID, sessionID)
	if run == nil {
		return ""
	}
	return strings.TrimSpace(run.EventID)
}

func isUserFacingGatedEvent(evt DelegateEventPayload) bool {
	if strings.Contains(strings.ToLower(strings.TrimSpace(evt.EventType)), "customer_coach") {
		return true
	}
	return isUserFacingGatedEventID(evt.EventID)
}

func isUserFacingGatedEventID(eventID string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(eventID)), "customer_coach:")
}

// ExtractToUserOutput 取出模型显式标给用户的正文。取最后一个 /to_user 标记，
// 因为过程自述总是写在正文之前；标记之前的内容全部丢弃。ok=false 表示这条
// 输出没有任何面向用户的正文，必须整条吞掉。
func ExtractToUserOutput(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	markerLine := -1
	sameLineText := ""
	for i, line := range lines {
		tail, ok := cutCommandPrefix(strings.TrimSpace(line), ToUserCommand)
		if !ok {
			continue
		}
		markerLine = i
		sameLineText = strings.TrimSpace(strings.TrimLeft(tail, " \t:："))
	}
	if markerLine < 0 {
		return "", false
	}
	segments := make([]string, 0, 2)
	if sameLineText != "" {
		segments = append(segments, sameLineText)
	}
	if markerLine+1 < len(lines) {
		segments = append(segments, strings.Join(lines[markerLine+1:], "\n"))
	}
	deliverable := strings.TrimSpace(strings.Join(segments, "\n"))
	if deliverable == "" {
		return "", false
	}
	return deliverable, true
}

// GateUserFacingOutput 返回一条输出在该事件下真正可投递的正文。
// ok=false 表示这条输出必须被静默吞掉，不入库也不推送。
func GateUserFacingOutput(content string, eventID string) (string, bool) {
	if !IsUserFacingGatedContext(eventID) {
		return content, true
	}
	return ExtractToUserOutput(content)
}

// cutCommandPrefix 按协议命令语义切分：命令后必须是结尾或非标识符字符，
// 避免 /to_user_x、/no_reply2 这类 token 被当成命令。
func cutCommandPrefix(trimmed string, command string) (string, bool) {
	if !strings.HasPrefix(trimmed, command) {
		return "", false
	}
	tail := trimmed[len(command):]
	if tail == "" {
		return tail, true
	}
	c := tail[0]
	if c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
		return "", false
	}
	return tail, true
}

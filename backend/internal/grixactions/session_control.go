package grixactions

import "strings"

type SessionControlCommand struct {
	Verb           string
	Cwd            string
	CardInstanceID string
}

func ParseSessionControlCommand(raw string) (SessionControlCommand, bool, error) {
	if submit, matched, err := ParseOpenSessionSubmit(raw); matched {
		if err != nil {
			return SessionControlCommand{}, true, err
		}
		return SessionControlCommand{
			Verb:           "open",
			Cwd:            strings.TrimSpace(submit.Cwd),
			CardInstanceID: strings.TrimSpace(submit.CardInstanceID),
		}, true, nil
	}

	// 纯文本 /grix 命令不再拦截。用户在聊天框输入的 /grix 开头内容
	// 已在 PushDelegateEvent 中被加前导空格转义，作为普通消息发给 agent。
	// 卡片按钮走 URI 格式（grix://），在上面 ParseOpenSessionSubmit 中匹配。
	return SessionControlCommand{}, false, nil
}

var (
	errSessionControlCwdRequired = simpleParseError("cwd required")
	errSessionControlUsage       = simpleParseError("usage")
)

type simpleParseError string

func (e simpleParseError) Error() string { return string(e) }

package handler

import (
	"encoding/json"
	"strings"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

// WidgetPlatform 标识嵌入式网站访客连接（frame.html）。访客端用轻量 markdown
// 渲染消息正文，无法识别 grix://card 卡片，因此 agent 的内部过程消息（工具执行、
// 思考等卡片）若投递给访客，会被当成普通文字显示出 fallback（如
// "[Tools] skill_view: ..."）。这类内部信息本就不该出现在访客侧。
const WidgetPlatform = "web_widget"

// widgetHideEnvelope 仅解析判别"是否对访客隐藏"所需的内部过程标记。
type widgetHideEnvelope struct {
	ChannelData struct {
		Grix struct {
			ToolExecution json.RawMessage `json:"toolExecution"`
			Thinking      json.RawMessage `json:"thinking"`
		} `json:"grix"`
	} `json:"channel_data"`
}

// shouldHideFromWidget 判断一条消息是否属于"不应展示给网站访客"的内部过程内容。
// 命中即不投递给访客（owner 的 App 等正常端不受影响，照旧渲染卡片）。
//   - 正文是 grix://card 卡片链接：访客端渲染不了，会漏出 fallback 文字——兜底全覆盖
//     （工具执行 / 工具执行分组 / 思考 / 状态等所有卡片类型）。
//   - extra 带 channel_data.grix.toolExecution / thinking 内部标记：与语音侧
//     ShouldInjectVoiceMessage 同口径的双保险。
func shouldHideFromWidget(content string, extraRaw []byte) bool {
	if strings.Contains(content, "grix://card/") {
		return true
	}
	if len(extraRaw) > 0 {
		var env widgetHideEnvelope
		if json.Unmarshal(extraRaw, &env) == nil {
			if len(env.ChannelData.Grix.ToolExecution) > 0 || len(env.ChannelData.Grix.Thinking) > 0 {
				return true
			}
		}
	}
	return false
}

// WidgetDropPush 判断一条待投递的 push 是否应对某个连接（按平台）丢弃。
// 仅 web_widget 访客连接 + push_msg 命令 + 内部过程内容三者同时满足才丢弃，
// 其余连接、其余命令一律放行，保证只收窄访客端、不影响任何既有链路。
func WidgetDropPush(platform, cmd, content string, extra []byte) bool {
	if platform != WidgetPlatform || cmd != protocol.CmdPushMsg {
		return false
	}
	return shouldHideFromWidget(content, extra)
}

// WidgetDropPushRaw 是 WidgetDropPush 的原始载荷版本，供跨节点分发（payload 为
// json.RawMessage）使用。非 web_widget 连接 / 非 push_msg 命令直接短路返回，
// 只有访客连接的 push 才解析载荷，避免给主链路连接增加无谓开销。
func WidgetDropPushRaw(platform, cmd string, rawPayload []byte) bool {
	if platform != WidgetPlatform || cmd != protocol.CmdPushMsg {
		return false
	}
	var pm protocol.PushMsgPayload
	if json.Unmarshal(rawPayload, &pm) != nil {
		return false
	}
	return shouldHideFromWidget(pm.Content, pm.Extra)
}

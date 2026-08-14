package handler

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

// 真实线上形状：工具执行卡片正文是 grix://card 链接 + fallback 文字，
// extra 带 channel_data.grix.toolExecution 标记。
const widgetToolCardContent = "[[Tools] skill_view: lightking-customer-service-style]" +
	"(grix://card/tool_execution_group?d=%7B%22count%22%3A1%7D)"

const widgetToolCardExtra = `{"agent_id":"2064873167004372992",` +
	`"channel_data":{"grix":{"toolExecution":{"summary_text":"skill_view: lightking-customer-service-style"}}},` +
	`"agent_api_origin":true}`

const widgetThinkingExtra = `{"channel_data":{"grix":{"thinking":{"content":"让我想想..."}}}}`

func TestShouldHideFromWidget(t *testing.T) {
	cases := []struct {
		name    string
		content string
		extra   string
		want    bool
	}{
		{"工具执行卡片正文(grix card)-隐藏", widgetToolCardContent, widgetToolCardExtra, true},
		{"工具执行extra标记但正文已清-隐藏", "skill_view: x", widgetToolCardExtra, true},
		{"思考过程extra-隐藏", "让我想想...", widgetThinkingExtra, true},
		{"任意grix card正文-隐藏", "[卡片](grix://card/whatever?d=1)", "", true},
		{"普通文字回复-放行", "您好，请问有什么可以帮您？", "", false},
		{"普通文字带agent_id无内部标记-放行", "已为您查询", `{"agent_id":"1","agent_api_origin":true}`, false},
		{"图片消息-放行", "![image](https://x/a.png)", "", false},
		{"非法extra不误判-放行", "正常文字", "not-json", false},
		{"空内容空extra-放行", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldHideFromWidget(tc.content, []byte(tc.extra))
			if got != tc.want {
				t.Fatalf("shouldHideFromWidget(%q, %q) = %v, want %v", tc.content, tc.extra, got, tc.want)
			}
		})
	}
}

func TestWidgetDropPush_OnlyWidgetPushMsg(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		cmd      string
		content  string
		extra    string
		want     bool
	}{
		{"访客+push_msg+工具卡片-丢弃", WidgetPlatform, protocol.CmdPushMsg, widgetToolCardContent, widgetToolCardExtra, true},
		{"访客+push_msg+普通文字-放行", WidgetPlatform, protocol.CmdPushMsg, "你好", "", false},
		{"App端+push_msg+工具卡片-放行(App要显示卡片)", "ios", protocol.CmdPushMsg, widgetToolCardContent, widgetToolCardExtra, false},
		{"Web端+push_msg+工具卡片-放行", "web", protocol.CmdPushMsg, widgetToolCardContent, widgetToolCardExtra, false},
		{"访客+非push_msg+工具卡片-放行", WidgetPlatform, protocol.CmdPing, widgetToolCardContent, widgetToolCardExtra, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := WidgetDropPush(tc.platform, tc.cmd, tc.content, []byte(tc.extra))
			if got != tc.want {
				t.Fatalf("WidgetDropPush(%q,%q,...) = %v, want %v", tc.platform, tc.cmd, got, tc.want)
			}
		})
	}
}

func TestWidgetDropPushRaw_ParsesPayload(t *testing.T) {
	internal, _ := json.Marshal(protocol.PushMsgPayload{
		MsgType: 1,
		Content: widgetToolCardContent,
		Extra:   json.RawMessage(widgetToolCardExtra),
	})
	normal, _ := json.Marshal(protocol.PushMsgPayload{
		MsgType: 1,
		Content: "您好",
	})

	if !WidgetDropPushRaw(WidgetPlatform, protocol.CmdPushMsg, internal) {
		t.Fatal("widget visitor should drop internal tool card push")
	}
	if WidgetDropPushRaw(WidgetPlatform, protocol.CmdPushMsg, normal) {
		t.Fatal("widget visitor must still receive normal text push")
	}
	if WidgetDropPushRaw("ios", protocol.CmdPushMsg, internal) {
		t.Fatal("non-widget platform must never be filtered")
	}
	if WidgetDropPushRaw(WidgetPlatform, protocol.CmdPushMsg, []byte("not-json")) {
		t.Fatal("unparseable payload must not be dropped")
	}
}

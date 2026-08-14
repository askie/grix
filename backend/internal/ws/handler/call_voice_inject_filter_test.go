package handler

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

// TestShouldInjectVoiceMessage 覆盖语音注入的内容过滤：只放行给人看的最终文字回复，
// 排除转写片段、工具执行卡片、思考过程。extra 形状取自线上真实消息。
func TestShouldInjectVoiceMessage(t *testing.T) {
	cases := []struct {
		name    string
		msgType int16
		extra   string
		want    bool
	}{
		{
			name:    "真实文字回复-放行",
			msgType: model.MsgTypeText,
			extra:   `{"agent_id":"2064873167004372992","agent_api_origin":true}`,
			want:    true,
		},
		{
			name:    "空extra文字回复-放行",
			msgType: model.MsgTypeText,
			extra:   ``,
			want:    true,
		},
		{
			name:    "工具执行卡片-拦截",
			msgType: model.MsgTypeText,
			extra:   `{"agent_id":"2064873167004372992","channel_data":{"grix":{"toolExecution":{"summary_text":"skill_view: grix-product-knowledge"}}},"agent_api_origin":true}`,
			want:    false,
		},
		{
			name:    "工具卡片无agent_id-拦截",
			msgType: model.MsgTypeText,
			extra:   `{"channel_data":{"grix":{"toolExecution":{"summary_text":"skill_view"}}}}`,
			want:    false,
		},
		{
			name:    "思考过程-拦截",
			msgType: model.MsgTypeText,
			extra:   `{"channel_data":{"grix":{"thinking":{"content":"让我想想..."}}}}`,
			want:    false,
		},
		{
			name:    "通话转写片段-拦截",
			msgType: model.MsgTypeCallSegment,
			extra:   `{"kind":"call_segment","transcript":"喂喂。"}`,
			want:    false,
		},
		{
			name:    "转写片段即便无标记也拦截",
			msgType: model.MsgTypeCallSegment,
			extra:   ``,
			want:    false,
		},
		{
			name:    "普通channel_data无工具/思考-放行",
			msgType: model.MsgTypeText,
			extra:   `{"channel_data":{"grix":{"other":1}}}`,
			want:    true,
		},
		{
			name:    "非法extra-放行(不因解析失败误拦真回复)",
			msgType: model.MsgTypeText,
			extra:   `not-json`,
			want:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldInjectVoiceMessage(tc.msgType, []byte(tc.extra))
			if got != tc.want {
				t.Fatalf("ShouldInjectVoiceMessage(%d, %q) = %v, want %v", tc.msgType, tc.extra, got, tc.want)
			}
		})
	}
}

package agentapi

import "testing"

func TestExtractToUserOutput(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
		ok      bool
	}{
		{
			name:    "pure narration is withheld",
			content: "用户是刚注册的新用户，还没有任何 Agent，按后端判定引导创建第一个 Agent。",
			ok:      false,
		},
		{
			name:    "marker prefix on a single line",
			content: "/to_user 您好，建议先创建一个 Agent 试试。",
			want:    "您好，建议先创建一个 Agent 试试。",
			ok:      true,
		},
		{
			name:    "narration before the marker is dropped",
			content: "按后端判定引导创建第一个 Agent。\n之前同类场景口径一致。\n/to_user 您好，建议先创建一个 Agent 试试。",
			want:    "您好，建议先创建一个 Agent 试试。",
			ok:      true,
		},
		{
			name:    "marker on its own line keeps the following body",
			content: "内部分析。\n/to_user\n您好，\n建议先创建一个 Agent。",
			want:    "您好，\n建议先创建一个 Agent。",
			ok:      true,
		},
		{
			name:    "full width colon after the marker",
			content: "/to_user：您好，建议先创建一个 Agent。",
			want:    "您好，建议先创建一个 Agent。",
			ok:      true,
		},
		{
			name:    "last marker wins",
			content: "/to_user 草稿一\n重新组织一下措辞。\n/to_user 定稿。",
			want:    "定稿。",
			ok:      true,
		},
		{
			// 当前语义：标记之后的一切都会原样投递，尾随自述堵不住，只能靠
			// 协议文字约束。钉住这个行为，改协议前先看到它变化。
			name:    "trailing narration after the body is still delivered",
			content: "/to_user 您好，建议先创建一个 Agent。\n（按后端快照判定发出）",
			want:    "您好，建议先创建一个 Agent。\n（按后端快照判定发出）",
			ok:      true,
		},
		{
			name:    "marker with empty body is withheld",
			content: "内部分析。\n/to_user   ",
			ok:      false,
		},
		{
			name:    "identifier-like token is not the marker",
			content: "/to_user_draft 这段只是草稿说明。",
			ok:      false,
		},
		{
			name:    "no_reply alone stays withheld by the gate",
			content: NoReplyCommand,
			ok:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExtractToUserOutput(tc.content)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (content=%q)", ok, tc.ok, tc.content)
			}
			if got != tc.want {
				t.Fatalf("content = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGateUserFacingOutputOnlyAppliesToGatedEvents(t *testing.T) {
	narration := "用户还没有任何 Agent，按后端判定引导创建第一个 Agent。"

	got, ok := GateUserFacingOutput(narration, "2093128058734641152")
	if !ok || got != narration {
		t.Fatalf("normal event must pass through unchanged: content=%q ok=%v", got, ok)
	}

	// 无 Manager 时空 eventID 直通：闸门以 eventID 为键，回落靠
	// ResolveUserFacingGateEventID 在调用点补齐，这里钉住直通行为本身。
	got, ok = GateUserFacingOutput(narration, "")
	if !ok || got != narration {
		t.Fatalf("empty event id must pass through unchanged: content=%q ok=%v", got, ok)
	}
	if resolved := ResolveUserFacingGateEventID("  ", 0, ""); resolved != "" {
		t.Fatalf("unresolvable gate event id = %q, want empty", resolved)
	}
	if resolved := ResolveUserFacingGateEventID(" customer_coach:1:client_open:2 ", 0, ""); resolved != "customer_coach:1:client_open:2" {
		t.Fatalf("explicit gate event id must be kept, got %q", resolved)
	}

	if _, ok := GateUserFacingOutput(narration, "customer_coach:2030840865701756928:client_open:1"); ok {
		t.Fatal("coach event narration must be withheld")
	}

	got, ok = GateUserFacingOutput(
		narration+"\n/to_user 您好，建议先创建一个 Agent。",
		"customer_coach:2030840865701756928:client_open:1",
	)
	if !ok || got != "您好，建议先创建一个 Agent。" {
		t.Fatalf("coach event marked body must be delivered: content=%q ok=%v", got, ok)
	}
}

func TestShouldGateUserFacingOutput(t *testing.T) {
	cases := []struct {
		name string
		evt  DelegateEventPayload
		want bool
	}{
		{
			name: "coach snapshot by event type",
			evt:  DelegateEventPayload{EventType: "customer_coach_snapshot", EventID: "evt-1"},
			want: true,
		},
		{
			name: "coach snapshot by event id",
			evt:  DelegateEventPayload{EventID: "customer_coach:1:client_open:2"},
			want: true,
		},
		{
			name: "dispatch task is not gated",
			evt:  DelegateEventPayload{EventType: "dispatch_task", EventID: "dispatch:1"},
		},
		{
			name: "system task is not gated",
			evt:  DelegateEventPayload{EventType: "system_notice", EventID: "system:1"},
		},
		{
			name: "user message is not gated",
			evt:  DelegateEventPayload{EventType: "user_chat", EventID: "2093128058734641152"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldGateUserFacingOutput(tc.evt); got != tc.want {
				t.Fatalf("ShouldGateUserFacingOutput = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAppendToUserProtocolInstruction(t *testing.T) {
	appended := AppendToUserProtocolInstruction("内部任务正文")
	if appended == "内部任务正文" {
		t.Fatal("instruction was not appended")
	}
	if AppendToUserProtocolInstruction(appended) != appended {
		t.Fatal("instruction must not be appended twice")
	}
	// 任务正文里提到 /to_user 字面量不能让指令静默不挂，否则闸门生效而模型
	// 不知道协议，输出会被全吞。
	mentionsMarker := "沉默时不要写 " + ToUserCommand + "。"
	if AppendToUserProtocolInstruction(mentionsMarker) == mentionsMarker {
		t.Fatal("instruction must still be appended when the body only mentions the marker")
	}
	if AppendToUserProtocolInstruction("   ") != "" {
		t.Fatal("empty content must stay empty")
	}
}

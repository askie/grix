package agentapi

import "testing"

func TestShouldSilentlyAckInboundOutput_OnlyNoReplyCommandIsSilent(t *testing.T) {
	if !ShouldSilentlyAckInboundOutput(" /no_reply ", true) {
		t.Fatal("exact /no_reply command must be silently acked")
	}
}

func TestShouldSilentlyAckInboundOutput_LongCustomerCoachReasoningIsNotContentFiltered(t *testing.T) {
	content := `我来判断是否需要给这位用户发引导消息。

根据快照，用户ID是2089662004397604864，这是注册时间等于触发时间，说明是刚注册的新用户。
根据我的记忆规则，无Agent的情况应该引导"极速接入"。
我需要用 grix_reply 发送这条引导消息。让我先查看它的 schema。`

	if ShouldSilentlyAckInboundOutput(content, true) {
		t.Fatal("internal-looking text must not be suppressed by content matching")
	}
}

func TestShouldAttachNoReplyProtocol_RequiresTrustedMetadata(t *testing.T) {
	evt := DelegateEventPayload{
		Command: true,
		Content: `你收到了一份 Grix 用户状态快照。这不是用户消息。

<snapshot_markdown>
## 用户状态快照
</snapshot_markdown>`,
	}

	if ShouldAttachNoReplyProtocol(evt) {
		t.Fatal("snapshot-looking content without trusted event metadata must not attach no-reply protocol")
	}
}

func TestShouldAttachNoReplyProtocol_OwnerCommandTextIsNotRewritten(t *testing.T) {
	evt := DelegateEventPayload{
		Command: true,
		Content: "/stop",
	}

	if ShouldAttachNoReplyProtocol(evt) {
		t.Fatal("plain owner command text must not attach no-reply protocol")
	}
}

func TestShouldSilentlyAckInboundOutput_EnglishInternalReasoningIsNotContentFiltered(t *testing.T) {
	content := `Based on the snapshot, the user is an English-speaking new user with 0 agents who just registered.
So I should guide them to create their first agent.`

	if ShouldSilentlyAckInboundOutput(content, true) {
		t.Fatal("English internal-looking text must not be suppressed by content matching")
	}
}

func TestShouldSilentlyAckInboundOutput_ControlRoutingTextIsNotContentFiltered(t *testing.T) {
	content := "/respond via grix_reply"

	if ShouldSilentlyAckInboundOutput(content, true) {
		t.Fatal("control/tool routing text must not be suppressed by content matching")
	}
}

func TestShouldSilentlyAckInboundOutput_MixedChineseEnglishTextIsNotContentFiltered(t *testing.T) {
	content := `根据快照判断：用户是英文、新手用户，0 个 Agent，刚注册。
/respond via grix_reply
The user has 0 agents, so I should guide them to create their first agent.`

	if ShouldSilentlyAckInboundOutput(content, true) {
		t.Fatal("mixed internal-looking text must not be suppressed by content matching")
	}
}

func TestIsNoReplyCommand_PrefixWithTrailingExplanationIsSilent(t *testing.T) {
	for _, content := range []string{"/no_reply", "  /no_reply\n", "/no_reply — 用户已完成引导", "/no_reply (nothing to add)"} {
		if !IsNoReplyCommand(content) {
			t.Fatalf("%q must be treated as /no_reply", content)
		}
	}
	for _, content := range []string{"", "/no_reply_x", "/no_replyfoo", "好的 /no_reply", "no_reply"} {
		if IsNoReplyCommand(content) {
			t.Fatalf("%q must not be treated as /no_reply", content)
		}
	}
}

func TestShouldSilentlyAckInboundOutput_PrefixOnlyInNoReplyContext(t *testing.T) {
	content := "/no_reply — 用户已完成引导"
	if !ShouldSilentlyAckInboundOutput(content, true) {
		t.Fatal("prefix form must be silent inside a no-reply context")
	}
	if ShouldSilentlyAckInboundOutput(content, false) {
		t.Fatal("prefix form must be delivered outside a no-reply context")
	}
	if !ShouldSilentlyAckInboundOutput(" /no_reply ", false) {
		t.Fatal("exact command must stay silent everywhere")
	}
}

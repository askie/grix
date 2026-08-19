package agentapi

import "testing"

func TestShouldSilentlyAckInboundOutput_LongCustomerCoachReasoning(t *testing.T) {
	content := `我来判断是否需要给这位用户发引导消息。

根据快照，用户ID是2089662004397604864，这是注册时间等于触发时间，说明是刚注册的新用户。
根据我的记忆规则，无Agent的情况应该引导"极速接入"。
我需要用 grix_reply 发送这条引导消息。让我先查看它的 schema。`

	if !ShouldSilentlyAckInboundOutput(content, true) {
		t.Fatal("long internal customer coach reasoning must be silently acked in no-reply context")
	}
}

func TestShouldSilentlyAckInboundOutput_InternalReasoningRequiresContext(t *testing.T) {
	content := "客户问我：为什么系统根据快照判断用户状态？我需要用自然语言解释一下。"

	if ShouldSilentlyAckInboundOutput(content, false) {
		t.Fatal("internal-looking text without no-reply context must not be suppressed")
	}
}

func TestShouldAttachNoReplyProtocol_SnapshotContent(t *testing.T) {
	evt := DelegateEventPayload{
		Command: true,
		Content: `你收到了一份 Grix 用户状态快照。这不是用户消息。

<snapshot_markdown>
## 用户状态快照
</snapshot_markdown>`,
	}

	if !ShouldAttachNoReplyProtocol(evt) {
		t.Fatal("command snapshot event must attach no-reply protocol")
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

func TestShouldSilentlyAckInboundOutput_EnglishInternalReasoning(t *testing.T) {
	content := `Based on the snapshot, the user is an English-speaking new user with 0 agents who just registered.
So I should guide them to create their first agent.`

	if !ShouldSilentlyAckInboundOutput(content, true) {
		t.Fatal("English internal customer coach reasoning must be silently acked in no-reply context")
	}
}

func TestShouldSilentlyAckInboundOutput_EnglishAgentCountReasoning(t *testing.T) {
	content := "The user has 0 agents, so I should guide them to create one."

	if !ShouldSilentlyAckInboundOutput(content, true) {
		t.Fatal("internal reasoning mentioning agent count must be suppressed in no-reply context")
	}
}

func TestShouldSilentlyAckInboundOutput_RespondViaGrixReplyControlRouting(t *testing.T) {
	content := "/respond via grix_reply"

	if !ShouldSilentlyAckInboundOutput(content, true) {
		t.Fatal("control/tool routing statement /respond via grix_reply must be suppressed")
	}
}

func TestShouldSilentlyAckInboundOutput_MixedChineseEnglishLeak(t *testing.T) {
	content := `根据快照判断：用户是英文、新手用户，0 个 Agent，刚注册。
/respond via grix_reply
The user has 0 agents, so I should guide them to create their first agent.`

	if !ShouldSilentlyAckInboundOutput(content, true) {
		t.Fatal("mixed internal reasoning leak must be suppressed in no-reply context")
	}
}

func TestShouldSilentlyAckInboundOutput_EnglishNaturalCopyNotSuppressed(t *testing.T) {
	content := `Welcome to Grix! Would you like to create your own agent to get started?`

	if ShouldSilentlyAckInboundOutput(content, true) {
		t.Fatal("natural English customer copy must not be suppressed even in no-reply context")
	}
}

func TestShouldSilentlyAckInboundOutput_EnglishControlRoutingRequiresContext(t *testing.T) {
	content := "/respond via grix_reply"

	if ShouldSilentlyAckInboundOutput(content, false) {
		t.Fatal("control routing without no-reply context must not be suppressed")
	}
}

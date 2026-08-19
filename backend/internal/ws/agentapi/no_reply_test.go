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

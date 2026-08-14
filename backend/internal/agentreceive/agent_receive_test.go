package agentreceive

import "testing"

func TestEvaluateGroupModeAllAlwaysDispatches(t *testing.T) {
	// 群聊里既无公共触发也无 @，ModeAll 仍应投递（有问必答）。
	decision, err := Evaluate(
		nil,
		Policy{Mode: ModeAll, BacklogCount: DefaultBacklogCount},
		MessageTrigger{SessionType: 2, MsgID: 1},
		false,
		false,
	)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !decision.Dispatch {
		t.Fatal("dispatch=false want=true")
	}
}

func TestNormalizePreservesModeAll(t *testing.T) {
	mode, _ := Normalize(ModeAll, DefaultBacklogCount)
	if mode != ModeAll {
		t.Fatalf("mode=%d want=%d", mode, ModeAll)
	}
}

func TestEvaluateGroupModeNormalNeedsPublicTrigger(t *testing.T) {
	decision, err := Evaluate(
		nil,
		Policy{Mode: ModeNormal, BacklogCount: DefaultBacklogCount},
		MessageTrigger{SessionType: 2, MsgID: 1},
		false,
		false,
	)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Dispatch {
		t.Fatalf("dispatch=%v want=false", decision.Dispatch)
	}
}

func TestEvaluateGroupModeNormalAllowsColdStartOrContinuation(t *testing.T) {
	decision, err := Evaluate(
		nil,
		Policy{Mode: ModeNormal, BacklogCount: DefaultBacklogCount},
		MessageTrigger{SessionType: 2, MsgID: 1},
		true,
		false,
	)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !decision.Dispatch {
		t.Fatal("dispatch=false want=true")
	}
	if !decision.ClearBufferOnAccept {
		t.Fatal("clear_buffer_on_accept=false want=true")
	}
}

func TestEvaluateGroupModeMentionOnlyRejectsQuoteAndColdStart(t *testing.T) {
	decision, err := Evaluate(
		nil,
		Policy{Mode: ModeMentionOnly, BacklogCount: DefaultBacklogCount},
		MessageTrigger{SessionType: 2, MsgID: 1},
		true,
		false,
	)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Dispatch {
		t.Fatalf("dispatch=%v want=false", decision.Dispatch)
	}
}

func TestEvaluateGroupModeMentionOnlyAllowsExplicitMention(t *testing.T) {
	decision, err := Evaluate(
		nil,
		Policy{Mode: ModeMentionOnly, BacklogCount: DefaultBacklogCount},
		MessageTrigger{SessionType: 2, MsgID: 1},
		true,
		true,
	)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !decision.Dispatch {
		t.Fatal("dispatch=false want=true")
	}
}

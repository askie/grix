package agentreceive

import "testing"

func TestEvaluateGroupNormalAndLegacyModeAllAlwaysDispatch(t *testing.T) {
	for _, mode := range []int16{ModeNormal, ModeAll} {
		decision, err := Evaluate(
			nil,
			Policy{Mode: mode, BacklogCount: DefaultBacklogCount},
			MessageTrigger{SessionType: 2, MsgID: 1},
			false,
		)
		if err != nil {
			t.Fatalf("Evaluate() mode=%d error = %v", mode, err)
		}
		if !decision.Dispatch {
			t.Fatalf("Evaluate() mode=%d dispatch=false want=true", mode)
		}
		if !decision.ClearBufferOnAccept {
			t.Fatalf("Evaluate() mode=%d clear_buffer_on_accept=false want=true", mode)
		}
	}
}

func TestNormalizeMapsLegacyModeAllToNormal(t *testing.T) {
	mode, _ := Normalize(ModeAll, DefaultBacklogCount)
	if mode != ModeNormal {
		t.Fatalf("mode=%d want=%d", mode, ModeNormal)
	}
}

func TestEvaluateGroupModeMentionOnlyRejectsPublicMessage(t *testing.T) {
	decision, err := Evaluate(
		nil,
		Policy{Mode: ModeMentionOnly, BacklogCount: DefaultBacklogCount},
		MessageTrigger{SessionType: 2, MsgID: 1},
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
	)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !decision.Dispatch {
		t.Fatal("dispatch=false want=true")
	}
}

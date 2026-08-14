package agentapi

import (
	"reflect"
	"testing"
)

func TestTryInterceptDelegateEvent_FamilyAwareOrder(t *testing.T) {
	mgr := NewManager("", 0, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.delegateEventInterceptors = nil

	var calls []string
	mgr.registerDelegateEventInterceptor("gemini-first", "gemini", func(evt DelegateEventPayload) bool {
		calls = append(calls, "gemini-first")
		return false
	})
	mgr.registerDelegateEventInterceptor("generic-second", "", func(evt DelegateEventPayload) bool {
		calls = append(calls, "generic-second")
		return true
	})
	mgr.registerDelegateEventInterceptor("claude-third", "claude", func(evt DelegateEventPayload) bool {
		calls = append(calls, "claude-third")
		return true
	})

	mgr.putConnForTest(&agentConn{
		agentID:    42,
		clientType: "gemini",
		adapterID:  "gemini/base",
	})

	ok := mgr.tryInterceptDelegateEvent(DelegateEventPayload{
		AgentID:   42,
		SessionID: "sess-1",
		Content:   "hello",
	})
	if !ok {
		t.Fatal("tryInterceptDelegateEvent should report handled")
	}

	wantCalls := []string{"gemini-first", "generic-second"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls=%v want=%v", calls, wantCalls)
	}
}

func TestTryInterceptDelegateEvent_UnknownFamilyFallsBackToInterceptors(t *testing.T) {
	mgr := NewManager("", 0, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.delegateEventInterceptors = nil
	mgr.registerDelegateEventInterceptor("claude", "claude", func(evt DelegateEventPayload) bool {
		return true
	})

	ok := mgr.tryInterceptDelegateEvent(DelegateEventPayload{
		AgentID:   77,
		SessionID: "sess-unknown",
		Content:   "/question req-1 accept",
	})
	if !ok {
		t.Fatal("unknown family should still run registered interceptors for compatibility")
	}
}

func TestRegisterDefaultDelegateEventInterceptors_OrderAndFamily(t *testing.T) {
	mgr := NewManager("", 0, nil, nil, nil, nil)
	defer mgr.Shutdown()
	if len(mgr.delegateEventInterceptors) == 0 {
		t.Fatal("default interceptors should be registered")
	}

	names := make([]string, 0, len(mgr.delegateEventInterceptors))
	families := make([]string, 0, len(mgr.delegateEventInterceptors))
	for _, interceptor := range mgr.delegateEventInterceptors {
		names = append(names, interceptor.name)
		families = append(families, interceptor.family)
	}

	wantNames := []string{
		"access_approval_reply",
		"gemini_open_session_submit",
		"gemini_question_reply",
		"session_control",
		"claude_question_command",
		"exec_approval_command",
		"gemini_question_requirement",
	}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("interceptor names=%v want=%v", names, wantNames)
	}

	wantFamilies := []string{
		"",
		"gemini",
		"gemini",
		"",
		"claude",
		"",
		"gemini",
	}
	if !reflect.DeepEqual(families, wantFamilies) {
		t.Fatalf("interceptor families=%v want=%v", families, wantFamilies)
	}
}

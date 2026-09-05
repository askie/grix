package core

import (
	"context"
	"testing"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
)

type stopActionRecordingPackage struct {
	testPackage
	requests []toolprotocol.ActionRequest
}

func (p *stopActionRecordingPackage) HandleAction(_ context.Context, in ActionInput) (toolprotocol.ActionResult, error) {
	p.requests = append(p.requests, in.Request)
	return p.result, nil
}

func stopBySessionService(pkg Package, items []toolprotocol.Item) *Service {
	return NewService(
		testResolver{buildInput: BuildInput{
			OwnerID: 1001,
			Session: SessionInfo{SessionID: "sess-stop"},
			Agent:   AgentInfo{AgentID: 9001, ClientType: "test"},
			Runtime: toolruntime.Profile{Online: true},
			Run: toolruntime.RunState{
				HasActiveRun: len(items) > 0,
				RunID:        "run-1",
				CanStop:      true,
			},
		}},
		testRegistry{pkg: pkg},
		&testCache{reserveOK: true},
		noopNotifier{},
		&recordingExecutor{},
	)
}

// 有活动 run 时，StopOutputBySession 交给 agent 包的请求必须与工具栏按钮点击完全一致，
// 后者才是真正下发 event_stop 的那条链路。
func TestStopOutputBySessionMatchesToolbarButtonRequest(t *testing.T) {
	items := []toolprotocol.Item{{
		ItemID:   "stop_output",
		Kind:     toolprotocol.ItemKindButton,
		ActionID: "stop_output",
	}}
	pkg := &stopActionRecordingPackage{testPackage: testPackage{
		snapshot: toolprotocol.Snapshot{Visible: true, Items: items},
		result: toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh,
			Code:    "accepted",
			Message: "已提交停止请求",
		},
	}}
	svc := stopBySessionService(pkg, items)

	ack, err := svc.StopOutputBySession(context.Background(), 1001, "sess-stop", 9001, "slash_stop:9001:7001")
	if err != nil {
		t.Fatalf("StopOutputBySession() err = %v", err)
	}
	if !ack.Accepted || ack.Code != "accepted" {
		t.Fatalf("ack = %+v, want accepted", ack)
	}
	if ack.Message != "已提交停止请求" {
		t.Fatalf("ack.Message = %q, want the toolbar stop wording", ack.Message)
	}
	if len(pkg.requests) != 1 {
		t.Fatalf("package HandleAction calls = %d, want 1", len(pkg.requests))
	}
	got := pkg.requests[0]
	if got.ItemID != "stop_output" || got.ActionID != "stop_output" || got.Event != "click" {
		t.Fatalf("request = %+v, want the stop_output click the toolbar button sends", got)
	}
	if got.SessionID != "sess-stop" || got.TargetAgentID != 9001 {
		t.Fatalf("request target = %+v, want session sess-stop agent 9001", got)
	}
	if got.ClientActionID != "slash_stop:9001:7001" {
		t.Fatalf("request.ClientActionID = %q, want the caller supplied idempotency key", got.ClientActionID)
	}
}

// 无活动 run 时快照里根本没有停止按钮：不能报错，也不能落到 agent 包上，
// 提示沿用工具栏「当前没有可停止的输出」。
func TestStopOutputBySessionWithoutStopItem(t *testing.T) {
	pkg := &stopActionRecordingPackage{testPackage: testPackage{
		snapshot: toolprotocol.Snapshot{Visible: true, Items: []toolprotocol.Item{{
			ItemID:   "session_control",
			Kind:     toolprotocol.ItemKindSelect,
			ActionID: "session_control",
		}}},
	}}
	svc := stopBySessionService(pkg, nil)

	ack, err := svc.StopOutputBySession(context.Background(), 1001, "sess-stop", 9001, "slash_stop:9001:7001")
	if err != nil {
		t.Fatalf("StopOutputBySession() err = %v, want no error", err)
	}
	if ack.Accepted {
		t.Fatalf("ack.Accepted = true, want rejected without an active run")
	}
	if ack.Code != "stop_unavailable" {
		t.Fatalf("ack.Code = %q, want stop_unavailable", ack.Code)
	}
	if ack.Message != stopUnavailableMessage {
		t.Fatalf("ack.Message = %q, want %q", ack.Message, stopUnavailableMessage)
	}
	if len(pkg.requests) != 0 {
		t.Fatalf("package HandleAction calls = %d, want 0", len(pkg.requests))
	}
}

// 停止按钮存在但被禁用（run 正在 stopping）时同样按「没有可停止的输出」拒绝，不重复停止。
func TestStopOutputBySessionWithDisabledStopItem(t *testing.T) {
	items := []toolprotocol.Item{{
		ItemID:   "stop_output",
		Kind:     toolprotocol.ItemKindButton,
		ActionID: "stop_output",
		Disabled: true,
	}}
	pkg := &stopActionRecordingPackage{testPackage: testPackage{
		snapshot: toolprotocol.Snapshot{Visible: true, Items: items},
	}}
	svc := stopBySessionService(pkg, items)

	ack, err := svc.StopOutputBySession(context.Background(), 1001, "sess-stop", 9001, "slash_stop:9001:7001")
	if err != nil {
		t.Fatalf("StopOutputBySession() err = %v", err)
	}
	if ack.Accepted || ack.Code != "stop_unavailable" {
		t.Fatalf("ack = %+v, want stop_unavailable rejection", ack)
	}
	if len(pkg.requests) != 0 {
		t.Fatalf("package HandleAction calls = %d, want 0", len(pkg.requests))
	}
}

package core

import (
	"context"
	"fmt"
	"testing"
	"time"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

type testResolver struct {
	buildInput BuildInput
	err        error
}

func (r testResolver) Resolve(_ context.Context, _ int64, _ string, _ int64) (BuildInput, error) {
	return r.buildInput, r.err
}

type testRegistry struct {
	pkg Package
}

func (r testRegistry) Resolve(_ MatchContext) Package {
	return r.pkg
}

type testPackage struct {
	snapshot toolprotocol.Snapshot
	result   toolprotocol.ActionResult
}

func (p testPackage) Key() string               { return "test" }
func (p testPackage) Match(_ MatchContext) bool { return true }
func (p testPackage) Build(_ context.Context, _ BuildInput) (toolprotocol.Snapshot, error) {
	return p.snapshot, nil
}
func (p testPackage) HandleAction(_ context.Context, _ ActionInput) (toolprotocol.ActionResult, error) {
	return p.result, nil
}

type countingPackage struct {
	testPackage
	handled int
}

func (p *countingPackage) HandleAction(_ context.Context, _ ActionInput) (toolprotocol.ActionResult, error) {
	p.handled++
	return p.result, nil
}

type testCache struct {
	snapshot            toolprotocol.Snapshot
	reserveOK           bool
	reserveAck          ActionAck
	completed           []ActionAck
	saveChanged         bool
	contextWarmReserved map[string]struct{}
	rateLimitReserved   map[string]struct{}
}

func (c *testCache) LoadSnapshot(_ context.Context, _ int64, _ string, _ int64) (toolprotocol.Snapshot, bool, error) {
	return c.snapshot, true, nil
}
func (c *testCache) SaveSnapshot(_ context.Context, _ int64, snapshot toolprotocol.Snapshot) (toolprotocol.Snapshot, bool, error) {
	if snapshot.Revision == 0 {
		snapshot.Revision = c.snapshot.Revision
		if snapshot.Revision == 0 {
			snapshot.Revision = 2
		}
	}
	c.snapshot = snapshot
	return snapshot, c.saveChanged, nil
}
func (c *testCache) DeleteSnapshot(_ context.Context, _ int64, _ string, _ int64) error { return nil }
func (c *testCache) ListIndexedSessions(_ context.Context, _ int64, _ int64) ([]string, error) {
	return nil, nil
}
func (c *testCache) ReserveContextWarm(_ context.Context, ownerID, agentID int64, sessionID string, _ time.Duration) (bool, error) {
	if c.contextWarmReserved == nil {
		c.contextWarmReserved = map[string]struct{}{}
	}
	key := fmt.Sprintf("%d:%d:%s", ownerID, agentID, sessionID)
	if _, ok := c.contextWarmReserved[key]; ok {
		return false, nil
	}
	c.contextWarmReserved[key] = struct{}{}
	return true, nil
}
func (c *testCache) ReserveRateLimitFetch(_ context.Context, ownerID int64, accountKey string, _ time.Duration) (bool, error) {
	if c.rateLimitReserved == nil {
		c.rateLimitReserved = map[string]struct{}{}
	}
	key := fmt.Sprintf("%d:%s", ownerID, accountKey)
	if _, ok := c.rateLimitReserved[key]; ok {
		return false, nil
	}
	c.rateLimitReserved[key] = struct{}{}
	return true, nil
}
func (c *testCache) ReserveAction(_ context.Context, _ int64, _ string, _ int64, _ string) (bool, ActionAck, error) {
	return c.reserveOK, c.reserveAck, nil
}
func (c *testCache) CompleteAction(_ context.Context, _ int64, _ string, _ int64, _ string, ack ActionAck) error {
	c.completed = append(c.completed, ack)
	return nil
}

type noopNotifier struct{}

func (noopNotifier) Sync(context.Context, int64, toolprotocol.Snapshot) error { return nil }

type recordingNotifier struct {
	calls int
}

func (n *recordingNotifier) Sync(context.Context, int64, toolprotocol.Snapshot) error {
	n.calls++
	return nil
}

type noopExecutor struct{}

func (noopExecutor) DispatchLocalAction(context.Context, LocalActionRequest) error { return nil }
func (noopExecutor) StopOutput(context.Context, StopOutputRequest) error           { return nil }
func (noopExecutor) SendStopText(context.Context, StopOutputRequest) error         { return nil }

type recordingExecutor struct {
	localActions []LocalActionRequest
	clears       []StopOutputRequest
}

func (e *recordingExecutor) DispatchLocalAction(_ context.Context, req LocalActionRequest) error {
	e.localActions = append(e.localActions, req)
	return nil
}

func (e *recordingExecutor) StopOutput(context.Context, StopOutputRequest) error   { return nil }
func (e *recordingExecutor) SendStopText(context.Context, StopOutputRequest) error { return nil }
func (e *recordingExecutor) ClearComposingState(_ context.Context, req StopOutputRequest) error {
	e.clears = append(e.clears, req)
	return nil
}

func TestGetSnapshotRejectsForbidden(t *testing.T) {
	svc := NewService(
		testResolver{err: ErrSessionForbidden},
		testRegistry{},
		&testCache{},
		noopNotifier{},
		noopExecutor{},
	)
	if _, err := svc.GetSnapshot(context.Background(), 1001, "sess-1", 0); err != ErrSessionForbidden {
		t.Fatalf("GetSnapshot() err = %v, want %v", err, ErrSessionForbidden)
	}
}

// run 执行期间 agent_output_status 会持续刷新工具栏，导致 revision 漂移。
// 此时只要动作针对当前快照仍然有效（item 存在、未禁用、选项合法），就应正常受理，
// 不再因 revision 不一致被静默拒绝。
func TestHandleActionAllowsRevisionDriftWhenItemValid(t *testing.T) {
	cache := &testCache{reserveOK: true, snapshot: toolprotocol.Snapshot{Revision: 7}}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID: 1001,
			Session: SessionInfo{SessionID: "sess-1"},
			Agent:   AgentInfo{AgentID: 9001, ClientType: "test"},
			Runtime: toolruntime.Profile{Online: true},
			Run:     toolruntime.RunState{HasActiveRun: true, RunID: "run-1", CanStop: true},
		}},
		testRegistry{pkg: testPackage{
			snapshot: toolprotocol.Snapshot{
				Visible: true,
				Items: []toolprotocol.Item{{
					ItemID:   "select_mode",
					Kind:     toolprotocol.ItemKindSelect,
					ActionID: "select_mode",
					Options:  []toolprotocol.Option{{OptionID: "kiro_planner", Label: "规划模式"}},
				}},
			},
			result: toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeAcceptedNoStateChange, Code: "accepted"},
		}},
		cache,
		noopNotifier{},
		noopExecutor{},
	)
	ack, err := svc.HandleAction(context.Background(), 1001, toolprotocol.ActionRequest{
		SessionID:      "sess-1",
		ToolbarID:      "agent-toolbar:test:v1",
		Revision:       1, // 客户端持有的旧 revision，与当前快照(7)不一致
		ItemID:         "select_mode",
		ActionID:       "select_mode",
		OptionID:       "kiro_planner",
		ClientActionID: "act-1",
		Event:          "select",
	})
	if err != nil {
		t.Fatalf("HandleAction() err = %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("ack.Accepted = false (code=%q msg=%q), want accepted despite revision drift", ack.Code, ack.Message)
	}
	if ack.Code != "accepted" {
		t.Fatalf("ack.Code = %q, want accepted", ack.Code)
	}
}

// 安全网：即便 revision 漂移，针对当前快照已失效的选项仍必须被拒绝（前端会据此弹提示）。
func TestHandleActionRejectsInvalidOptionDespiteRevisionDrift(t *testing.T) {
	cache := &testCache{reserveOK: true, snapshot: toolprotocol.Snapshot{Revision: 7}}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID: 1001,
			Session: SessionInfo{SessionID: "sess-1"},
			Agent:   AgentInfo{AgentID: 9001, ClientType: "test"},
			Runtime: toolruntime.Profile{Online: true},
		}},
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{
			Visible: true,
			Items: []toolprotocol.Item{{
				ItemID:   "select_mode",
				Kind:     toolprotocol.ItemKindSelect,
				ActionID: "select_mode",
				Options:  []toolprotocol.Option{{OptionID: "kiro_planner", Label: "规划模式"}},
			}},
		}}},
		cache,
		noopNotifier{},
		noopExecutor{},
	)
	ack, err := svc.HandleAction(context.Background(), 1001, toolprotocol.ActionRequest{
		SessionID:      "sess-1",
		ToolbarID:      "agent-toolbar:test:v1",
		Revision:       1,
		ItemID:         "select_mode",
		ActionID:       "select_mode",
		OptionID:       "ghost_option", // 当前快照里不存在的选项
		ClientActionID: "act-2",
		Event:          "select",
	})
	if err != nil {
		t.Fatalf("HandleAction() err = %v", err)
	}
	if ack.Accepted {
		t.Fatalf("ack.Accepted = true, want rejected for invalid option")
	}
	if ack.Code != "invalid_action" {
		t.Fatalf("ack.Code = %q, want invalid_action", ack.Code)
	}
}

func TestHandleActionRejectsTargetAgentMismatch(t *testing.T) {
	cache := &testCache{reserveOK: true}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID:  1001,
			Session:  SessionInfo{SessionID: "sess-1"},
			Agent:    AgentInfo{AgentID: 9001, ClientType: "test"},
			Language: "en",
		}},
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{
			Visible: true,
			Items: []toolprotocol.Item{{
				ItemID:   "stop_output",
				Kind:     toolprotocol.ItemKindButton,
				ActionID: "stop_output",
			}},
		}}},
		cache,
		noopNotifier{},
		noopExecutor{},
	)
	ack, err := svc.HandleAction(context.Background(), 1001, toolprotocol.ActionRequest{
		SessionID:      "sess-1",
		TargetAgentID:  9002,
		ToolbarID:      "agent-toolbar:test:v1",
		Revision:       2,
		ItemID:         "stop_output",
		ActionID:       "stop_output",
		ClientActionID: "act-1",
		Event:          "click",
	})
	if err != nil {
		t.Fatalf("HandleAction() err = %v", err)
	}
	if ack.Accepted {
		t.Fatalf("ack.Accepted = true, want false")
	}
	if ack.Code != "agent_mismatch" {
		t.Fatalf("ack.Code = %q, want agent_mismatch", ack.Code)
	}
}

func TestHandleActionClearsComposingForStaleStopOutput(t *testing.T) {
	cache := &testCache{reserveOK: true}
	exec := &recordingExecutor{}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID:  1001,
			Session:  SessionInfo{SessionID: "sess-stale-stop"},
			Agent:    AgentInfo{AgentID: 9001, ClientType: "test"},
			Language: "en",
		}},
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{
			Visible: true,
			Items: []toolprotocol.Item{{
				ItemID:   "select_mode",
				Kind:     toolprotocol.ItemKindSelect,
				ActionID: "select_mode",
				Options:  []toolprotocol.Option{{OptionID: "default", Label: "Default"}},
			}},
		}}},
		cache,
		noopNotifier{},
		exec,
	)
	ack, err := svc.HandleAction(context.Background(), 1001, toolprotocol.ActionRequest{
		SessionID:      "sess-stale-stop",
		TargetAgentID:  9001,
		ToolbarID:      "agent-toolbar:test:v1",
		Revision:       2,
		ItemID:         "stop_output",
		ActionID:       "stop_output",
		ClientActionID: "act-stop-stale",
		Event:          "click",
	})
	if err != nil {
		t.Fatalf("HandleAction() err = %v", err)
	}
	if ack.Accepted {
		t.Fatalf("ack.Accepted = true, want rejected because latest snapshot no longer has stop_output")
	}
	if len(exec.clears) != 1 {
		t.Fatalf("ClearComposingState calls = %d, want 1", len(exec.clears))
	}
	clear := exec.clears[0]
	if clear.OwnerID != 1001 || clear.AgentID != 9001 || clear.SessionID != "sess-stale-stop" {
		t.Fatalf("ClearComposingState req = %+v, want owner=1001 agent=9001 session=sess-stale-stop", clear)
	}
}

func TestHandleActionReturnsDuplicateAck(t *testing.T) {
	cache := &testCache{
		reserveOK: false,
		reserveAck: ActionAck{
			SessionID:       "sess-1",
			ToolbarID:       "agent-toolbar:test:v1",
			ClientActionID:  "act-1",
			Accepted:        true,
			Code:            "accepted",
			CurrentRevision: 2,
			UpdatedAt:       1,
		},
	}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID: 1001,
			Session: SessionInfo{SessionID: "sess-1"},
			Agent:   AgentInfo{AgentID: 9001, ClientType: "test"},
			Runtime: toolruntime.Profile{Online: true},
		}},
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{
			Visible: true,
			Items: []toolprotocol.Item{{
				ItemID:   "session_control",
				Kind:     toolprotocol.ItemKindSelect,
				ActionID: "session_control",
				Options:  []toolprotocol.Option{{OptionID: "status", Label: "status"}},
			}},
		}}},
		cache,
		noopNotifier{},
		noopExecutor{},
	)
	ack, err := svc.HandleAction(context.Background(), 1001, toolprotocol.ActionRequest{
		SessionID:      "sess-1",
		ToolbarID:      "agent-toolbar:test:v1",
		Revision:       2,
		ItemID:         "session_control",
		ActionID:       "session_control",
		ClientActionID: "act-1",
		Event:          "select",
		OptionID:       "status",
	})
	if err != nil {
		t.Fatalf("HandleAction() err = %v", err)
	}
	if !ack.Duplicate {
		t.Fatalf("ack.Duplicate = false, want true")
	}
	if !ack.Accepted {
		t.Fatalf("ack.Accepted = false, want true")
	}
}

func TestValidateActionRequestAcceptsCPKindAsButton(t *testing.T) {
	err := validateActionRequest(
		toolprotocol.Item{
			ItemID: "cp_action",
			Kind:   "cp",
		},
		toolprotocol.ActionRequest{Event: "click"},
	)
	if err != nil {
		t.Fatalf("validateActionRequest() err = %v, want nil", err)
	}
}

func TestValidateActionRequestAcceptsProgressClick(t *testing.T) {
	err := validateActionRequest(
		toolprotocol.Item{
			ItemID: "thread_compact",
			Kind:   toolprotocol.ItemKindProgress,
		},
		toolprotocol.ActionRequest{Event: "click"},
	)
	if err != nil {
		t.Fatalf("validateActionRequest() err = %v, want nil", err)
	}
}

func TestValidateActionRequestToggleListEvents(t *testing.T) {
	item := toolprotocol.Item{
		ItemID: "dsh_plugins",
		Kind:   toolprotocol.ItemKindToggleList,
		Toggles: []toolprotocol.ToggleItem{
			{ID: "@acme/dsh-notes", Locked: false},
			{ID: "@grix/dsh-bridge", Locked: true},
		},
	}
	for _, event := range []string{"click", "refresh", "enable"} {
		optionID := ""
		if event == "enable" {
			optionID = "@acme/dsh-notes"
		}
		if err := validateActionRequest(item, toolprotocol.ActionRequest{Event: event, OptionID: optionID}); err != nil {
			t.Fatalf("event %s err=%v, want nil", event, err)
		}
	}
	if err := validateActionRequest(item, toolprotocol.ActionRequest{Event: "disable", OptionID: "@grix/dsh-bridge"}); err == nil {
		t.Fatal("locked disable err=nil, want error")
	}
	if err := validateActionRequest(item, toolprotocol.ActionRequest{Event: "enable", OptionID: "missing"}); err == nil {
		t.Fatal("missing enable err=nil, want error")
	}
	if err := validateActionRequest(
		toolprotocol.Item{ItemID: "dsh_plugins", Kind: toolprotocol.ItemKindButton},
		toolprotocol.ActionRequest{Event: "enable", OptionID: "@acme/dsh-notes"},
	); err == nil {
		t.Fatal("button enable err=nil, want error")
	}
}

func TestHandleActionToggleListEnableReachesPackage(t *testing.T) {
	pkg := &countingPackage{testPackage: testPackage{
		snapshot: toolprotocol.Snapshot{
			Visible: true,
			Items: []toolprotocol.Item{{
				ItemID:   "dsh_plugins",
				Kind:     toolprotocol.ItemKindToggleList,
				ActionID: "dsh_plugins",
				Toggles:  []toolprotocol.ToggleItem{{ID: "@acme/dsh-notes"}},
			}},
		},
		result: toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeAcceptedNoStateChange, Code: "accepted"},
	}}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID: 1001,
			Session: SessionInfo{SessionID: "sess-1"},
			Agent:   AgentInfo{AgentID: 9001, ClientType: "deepseek"},
			Runtime: toolruntime.Profile{Online: true},
		}},
		testRegistry{pkg: pkg},
		&testCache{reserveOK: true, snapshot: toolprotocol.Snapshot{Revision: 2}},
		noopNotifier{},
		noopExecutor{},
	)
	ack, err := svc.HandleAction(context.Background(), 1001, toolprotocol.ActionRequest{
		SessionID:      "sess-1",
		ToolbarID:      "agent-toolbar:test:v1",
		Revision:       2,
		ItemID:         "dsh_plugins",
		ActionID:       "dsh_plugins",
		ClientActionID: "act-enable",
		Event:          "enable",
		OptionID:       "@acme/dsh-notes",
	})
	if err != nil {
		t.Fatalf("HandleAction() err=%v", err)
	}
	if !ack.Accepted || ack.Code != "accepted" {
		t.Fatalf("ack=%+v, want accepted", ack)
	}
	if pkg.handled != 1 {
		t.Fatalf("package handled=%d, want 1", pkg.handled)
	}
}

func TestHandleActionRejectsButtonEnableBeforePackage(t *testing.T) {
	pkg := &countingPackage{testPackage: testPackage{
		snapshot: toolprotocol.Snapshot{
			Visible: true,
			Items: []toolprotocol.Item{{
				ItemID:   "dsh_plugins",
				Kind:     toolprotocol.ItemKindButton,
				ActionID: "dsh_plugins",
			}},
		},
		result: toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeAcceptedNoStateChange, Code: "accepted"},
	}}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID: 1001,
			Session: SessionInfo{SessionID: "sess-1"},
			Agent:   AgentInfo{AgentID: 9001, ClientType: "deepseek"},
			Runtime: toolruntime.Profile{Online: true},
		}},
		testRegistry{pkg: pkg},
		&testCache{reserveOK: true, snapshot: toolprotocol.Snapshot{Revision: 2}},
		noopNotifier{},
		noopExecutor{},
	)
	ack, err := svc.HandleAction(context.Background(), 1001, toolprotocol.ActionRequest{
		SessionID:      "sess-1",
		ToolbarID:      "agent-toolbar:test:v1",
		Revision:       2,
		ItemID:         "dsh_plugins",
		ActionID:       "dsh_plugins",
		ClientActionID: "act-enable",
		Event:          "enable",
		OptionID:       "@acme/dsh-notes",
	})
	if err != nil {
		t.Fatalf("HandleAction() err=%v", err)
	}
	if ack.Accepted || ack.Code != "invalid_action" {
		t.Fatalf("ack=%+v, want invalid_action", ack)
	}
	if pkg.handled != 0 {
		t.Fatalf("package handled=%d, want 0", pkg.handled)
	}
}

func TestLocalizeSnapshotTranslatesToggleLockReason(t *testing.T) {
	snapshot := localizeSnapshot(toolprotocol.Snapshot{
		Items: []toolprotocol.Item{{
			ItemID:    "dsh_plugins",
			Label:     "插件",
			BadgeText: "需重启",
			Value:     "restart_required",
			Toggles: []toolprotocol.ToggleItem{{
				ID:         "@grix/dsh-bridge",
				LockReason: "Grix Bridge 由连接器安装，不能开关",
			}},
		}},
	}, "en")
	item := snapshot.Items[0]
	if item.Label != "Plugins" || item.BadgeText != "Restart required" || item.Value != "restart_required" {
		t.Fatalf("item=%+v", item)
	}
	if item.Toggles[0].LockReason != "Grix Bridge is installed by the connector; cannot be toggled" {
		t.Fatalf("lock_reason=%q", item.Toggles[0].LockReason)
	}
}

func TestGetSnapshotWarmsCodexContextWhenMetadataMissing(t *testing.T) {
	cache := &testCache{}
	executor := &recordingExecutor{}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID: 1001,
			Session: SessionInfo{SessionID: "sess-1"},
			Agent: AgentInfo{
				AgentID:    9001,
				ClientType: model.AgentClientTypeCodex,
			},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: []string{"get_context"},
			},
			Binding: BindingInfo{
				Cwd:          "/tmp/workspace",
				WorkerStatus: "ready",
				Meta:         map[string]any{},
			},
		}},
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{Visible: true}}},
		cache,
		noopNotifier{},
		executor,
	)

	if _, err := svc.GetSnapshot(context.Background(), 1001, "sess-1", 0); err != nil {
		t.Fatalf("GetSnapshot() err = %v", err)
	}
	if len(executor.localActions) != 1 {
		t.Fatalf("local actions = %d, want 1", len(executor.localActions))
	}
	if got := executor.localActions[0].ActionType; got != "get_context" {
		t.Fatalf("action_type = %q, want get_context", got)
	}
}

func TestGetSnapshotSkipsCodexWarmWithoutBinding(t *testing.T) {
	cache := &testCache{}
	executor := &recordingExecutor{}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID: 1001,
			Session: SessionInfo{SessionID: "sess-1"},
			Agent: AgentInfo{
				AgentID:    9001,
				ClientType: model.AgentClientTypeCodex,
			},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: []string{"get_context"},
			},
			Binding: BindingInfo{Meta: map[string]any{}},
		}},
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{Visible: true}}},
		cache,
		noopNotifier{},
		executor,
	)

	if _, err := svc.GetSnapshot(context.Background(), 1001, "sess-1", 0); err != nil {
		t.Fatalf("GetSnapshot() err = %v", err)
	}
	if len(executor.localActions) != 0 {
		t.Fatalf("local actions = %d, want 0", len(executor.localActions))
	}
}

func TestGetSnapshotSkipsCodexWarmWhenWorkerStopped(t *testing.T) {
	cache := &testCache{}
	executor := &recordingExecutor{}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID: 1001,
			Session: SessionInfo{SessionID: "sess-1"},
			Agent: AgentInfo{
				AgentID:    9001,
				ClientType: model.AgentClientTypeCodex,
			},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: []string{"get_context"},
			},
			Binding: BindingInfo{
				BindingID:    "binding-1",
				WorkerStatus: " stopped ",
				Meta:         map[string]any{},
			},
		}},
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{Visible: true}}},
		cache,
		noopNotifier{},
		executor,
	)

	if _, err := svc.GetSnapshot(context.Background(), 1001, "sess-1", 0); err != nil {
		t.Fatalf("GetSnapshot() err = %v", err)
	}
	if len(executor.localActions) != 0 {
		t.Fatalf("local actions = %d, want 0", len(executor.localActions))
	}
}

func TestGetSnapshotDedupesCodexWarmWithinCooldown(t *testing.T) {
	cache := &testCache{}
	executor := &recordingExecutor{}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID: 1001,
			Session: SessionInfo{SessionID: "sess-1"},
			Agent: AgentInfo{
				AgentID:    9001,
				ClientType: model.AgentClientTypeCodex,
			},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: []string{"get_context"},
			},
			Binding: BindingInfo{
				BindingID:    "binding-1",
				WorkerStatus: "ready",
				Meta:         map[string]any{},
			},
		}},
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{Visible: true}}},
		cache,
		noopNotifier{},
		executor,
	)

	if _, err := svc.GetSnapshot(context.Background(), 1001, "sess-1", 0); err != nil {
		t.Fatalf("first GetSnapshot() err = %v", err)
	}
	if _, err := svc.GetSnapshot(context.Background(), 1001, "sess-1", 0); err != nil {
		t.Fatalf("second GetSnapshot() err = %v", err)
	}
	if len(executor.localActions) != 1 {
		t.Fatalf("local actions = %d, want 1", len(executor.localActions))
	}
}

func TestGetSnapshotSkipsCodexWarmWhenMetadataReady(t *testing.T) {
	cache := &testCache{}
	executor := &recordingExecutor{}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID: 1001,
			Session: SessionInfo{SessionID: "sess-1"},
			Agent: AgentInfo{
				AgentID:    9001,
				ClientType: model.AgentClientTypeCodex,
			},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: []string{"get_context"},
			},
			Binding: BindingInfo{
				Cwd:          "/tmp/workspace",
				WorkerStatus: "ready",
				Meta: map[string]any{
					"model_id":         "gpt-5.4",
					"mode_id":          "default",
					"available_models": []any{map[string]any{"id": "gpt-5.4"}},
				},
			},
		}},
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{Visible: true}}},
		cache,
		noopNotifier{},
		executor,
	)

	if _, err := svc.GetSnapshot(context.Background(), 1001, "sess-1", 0); err != nil {
		t.Fatalf("GetSnapshot() err = %v", err)
	}
	if len(executor.localActions) != 0 {
		t.Fatalf("local actions = %d, want 0", len(executor.localActions))
	}
}

func TestRefreshSessionForceSyncWhenLocalActionResultAndSnapshotUnchanged(t *testing.T) {
	cache := &testCache{saveChanged: false}
	notifier := &recordingNotifier{}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID: 1001,
			Session: SessionInfo{SessionID: "sess-1"},
			Agent:   AgentInfo{AgentID: 9001, ClientType: "test"},
			Runtime: toolruntime.Profile{Online: true},
		}},
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{
			Visible: true,
			Items:   []toolprotocol.Item{},
		}}},
		cache,
		notifier,
		noopExecutor{},
	)

	if err := svc.RefreshSession(context.Background(), 1001, "sess-1", "local_action_result"); err != nil {
		t.Fatalf("RefreshSession() err = %v", err)
	}
	if notifier.calls != 1 {
		t.Fatalf("notifier calls = %d, want 1", notifier.calls)
	}
}

func TestRefreshSessionSkipsSyncWhenUnchangedAndReasonNotForced(t *testing.T) {
	cache := &testCache{saveChanged: false}
	notifier := &recordingNotifier{}
	svc := NewService(
		testResolver{buildInput: BuildInput{
			OwnerID: 1001,
			Session: SessionInfo{SessionID: "sess-1"},
			Agent:   AgentInfo{AgentID: 9001, ClientType: "test"},
			Runtime: toolruntime.Profile{Online: true},
		}},
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{
			Visible: true,
			Items:   []toolprotocol.Item{},
		}}},
		cache,
		notifier,
		noopExecutor{},
	)

	if err := svc.RefreshSession(context.Background(), 1001, "sess-1", "agent_output_status"); err != nil {
		t.Fatalf("RefreshSession() err = %v", err)
	}
	if notifier.calls != 0 {
		t.Fatalf("notifier calls = %d, want 0", notifier.calls)
	}
}

func TestRefreshSessionRateLimitFetchDedupesByAccount(t *testing.T) {
	cache := &testCache{saveChanged: true}
	executor := &recordingExecutor{}
	resolver := testResolver{buildInput: BuildInput{
		OwnerID: 1001,
		Session: SessionInfo{SessionID: "sess-1"},
		Agent: AgentInfo{
			AgentID:    9001,
			ClientType: model.AgentClientTypeClaude,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"get_rate_limits"},
		},
		Binding: BindingInfo{
			ProviderKey: "claude",
			BindingID:   "binding-1",
		},
	}}
	svc := NewService(
		resolver,
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{Visible: true}}},
		cache,
		noopNotifier{},
		executor,
	)

	if err := svc.RefreshSession(context.Background(), 1001, "sess-1", "local_action_result"); err != nil {
		t.Fatalf("first RefreshSession() err = %v", err)
	}
	if err := svc.RefreshSession(context.Background(), 1001, "sess-1", "local_action_result"); err != nil {
		t.Fatalf("second RefreshSession() err = %v", err)
	}
	if len(executor.localActions) != 1 {
		t.Fatalf("local actions = %d, want 1", len(executor.localActions))
	}
}

// show_queue 按钮的数量徽标由后端工具栏侧统一配置:运行中显示 1,空闲不显示。
func TestNormalizeSnapshotShowQueueBadgeReflectsActiveRun(t *testing.T) {
	base := toolprotocol.Snapshot{
		Visible: true,
		Items: []toolprotocol.Item{{
			ItemID:   "session_control",
			Kind:     toolprotocol.ItemKindButton,
			ActionID: "session_control",
		}},
	}
	findQueue := func(items []toolprotocol.Item) (toolprotocol.Item, bool) {
		for _, it := range items {
			if it.ItemID == "show_queue" {
				return it, true
			}
		}
		return toolprotocol.Item{}, false
	}

	running := normalizeSnapshot(base, BuildInput{Run: toolruntime.RunState{HasActiveRun: true}}, nil)
	q, ok := findQueue(running.Items)
	if !ok {
		t.Fatalf("show_queue button missing while running")
	}
	if q.BadgeText != "1" {
		t.Fatalf("show_queue badge = %q, want 1 while running", q.BadgeText)
	}

	idle := normalizeSnapshot(base, BuildInput{Run: toolruntime.RunState{}}, nil)
	q2, _ := findQueue(idle.Items)
	if q2.BadgeText != "" {
		t.Fatalf("show_queue badge = %q, want empty while idle", q2.BadgeText)
	}
}

// library_skills 是纯 runtime 透传数据（技能库启用，方案 v2）：不受 Visible 影响，
// Package.Build() 不产出它，统一由 normalizeSnapshot 从 buildInput.Runtime 灌入。
func TestNormalizeSnapshotPassesThroughLibrarySkills(t *testing.T) {
	librarySkills := []toolruntime.LibrarySkillEntry{
		{
			Name:   "grix-log-locator",
			Digest: "abc123",
			EnableScopes: toolruntime.LibrarySkillEnableScopes{
				Global:  "link",
				Project: "none",
			},
		},
	}

	visible := normalizeSnapshot(toolprotocol.Snapshot{Visible: true}, BuildInput{
		Runtime: toolruntime.Profile{LibrarySkills: librarySkills},
	}, nil)
	if len(visible.LibrarySkills) != 1 || visible.LibrarySkills[0].Name != "grix-log-locator" {
		t.Fatalf("library_skills not passed through when visible: %+v", visible.LibrarySkills)
	}

	// 即使工具栏本身不可见（没有其它按钮），技能库状态依然应该透传，
	// 因为它走独立的弹窗入口，不依赖 Items 是否为空。
	hidden := normalizeSnapshot(toolprotocol.Snapshot{Visible: false}, BuildInput{
		Runtime: toolruntime.Profile{LibrarySkills: librarySkills},
	}, nil)
	if len(hidden.LibrarySkills) != 1 || hidden.LibrarySkills[0].EnableScopes.Global != "link" {
		t.Fatalf("library_skills not passed through when hidden: %+v", hidden.LibrarySkills)
	}

	empty := normalizeSnapshot(toolprotocol.Snapshot{Visible: true}, BuildInput{}, nil)
	if len(empty.LibrarySkills) != 0 {
		t.Fatalf("library_skills should be empty when runtime has none: %+v", empty.LibrarySkills)
	}
}

package agenttoolbar_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/acp"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

// commandTextExecutor 是带 core.CommandTextSender 能力的执行器替身。
type commandTextExecutor struct {
	packageTestExecutor
	sent    []core.CommandTextRequest
	sendErr error
}

func (e *commandTextExecutor) SendCommandText(_ context.Context, req core.CommandTextRequest) error {
	if e.sendErr != nil {
		return e.sendErr
	}
	e.sent = append(e.sent, req)
	return nil
}

func selectItemMeta() []any {
	return []any{
		map[string]any{
			"id":    "env",
			"kind":  "select",
			"label": "环境",
			"value": "staging",
			"options": []any{
				map[string]any{"id": "staging", "label": "预发"},
				map[string]any{"id": "prod", "label": "生产"},
			},
		},
	}
}

func infoItemMeta() []any {
	return []any{
		map[string]any{
			"id":         "howto",
			"kind":       "info",
			"label":      "怎么用",
			"info_title": "部署说明",
			"info_text":  "选好环境后直接说部署。",
		},
	}
}

func customBuildInput(custom []any) core.BuildInput {
	in := acpBuildInput(map[string]any{"custom_toolbar": custom}, []string{"session_control"})
	return in
}

// 自定义项一经声明就常驻：空闲时工具栏也要出得来，否则 agent 给用户的入口永远看不见。
func TestACPCustomToolbar_VisibleWhenIdle(t *testing.T) {
	snapshot, err := acp.New().Build(context.Background(), customBuildInput(selectItemMeta()))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !snapshot.Visible {
		t.Fatal("snapshot invisible although the agent declared a toolbar item")
	}
	item, ok := snapshot.FindItem("custom_env")
	if !ok {
		t.Fatalf("custom_env not found, items=%+v", snapshot.Items)
	}
	if item.Kind != toolprotocol.ItemKindSelect || item.ActionID != acp.ActionIDCustomSelect {
		t.Fatalf("kind=%q action=%q, want select/%s", item.Kind, item.ActionID, acp.ActionIDCustomSelect)
	}
	if item.Value != "staging" || item.BadgeText != "预发" {
		t.Fatalf("value=%q badge=%q, want staging/预发", item.Value, item.BadgeText)
	}
	if len(item.Options) != 2 {
		t.Fatalf("options = %d, want 2", len(item.Options))
	}
	if item.Disabled {
		t.Fatalf("select disabled while online and idle, tooltip=%q", item.Tooltip)
	}
}

// info 项是纯客户端行为：必须带 client: 前缀的 local_action 和说明正文，
// 否则前端会把它当普通按钮发回后端。
func TestACPCustomToolbar_InfoItemIsClientSide(t *testing.T) {
	snapshot, err := acp.New().Build(context.Background(), customBuildInput(infoItemMeta()))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	item, ok := snapshot.FindItem("custom_howto")
	if !ok {
		t.Fatalf("custom_howto not found, items=%+v", snapshot.Items)
	}
	if item.Kind != toolprotocol.ItemKindButton {
		t.Fatalf("kind = %q, want button", item.Kind)
	}
	if !strings.HasPrefix(item.LocalAction, "client:") {
		t.Fatalf("local_action = %q, want a client: prefixed action", item.LocalAction)
	}
	if item.ConfirmTitle != "部署说明" || item.ConfirmText != "选好环境后直接说部署。" {
		t.Fatalf("confirm title/text = %q/%q", item.ConfirmTitle, item.ConfirmText)
	}
}

// 跑任务期间禁用下拉：ACP 一个会话同一时刻只能有一轮。
func TestACPCustomToolbar_SelectDisabledWhileRunning(t *testing.T) {
	in := customBuildInput(selectItemMeta())
	in.Run = toolruntime.RunState{HasActiveRun: true, CanStop: true, RunID: "run-1"}
	snapshot, err := acp.New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	item, ok := snapshot.FindItem("custom_env")
	if !ok {
		t.Fatal("custom_env not found")
	}
	if !item.Disabled {
		t.Fatal("select enabled while a run is active")
	}
}

// 不合规的上报整包丢弃：渲染半套工具栏比不渲染更难排查。
func TestACPCustomToolbar_RejectsInvalidPayloadWholesale(t *testing.T) {
	cases := map[string][]any{
		"id 含大写":     {map[string]any{"id": "Env", "kind": "select", "label": "环境", "options": []any{map[string]any{"id": "a", "label": "甲"}}}},
		"kind 未知":    {map[string]any{"id": "env", "kind": "button", "label": "环境"}},
		"select 无选项": {map[string]any{"id": "env", "kind": "select", "label": "环境", "options": []any{}}},
		"info 无正文":   {map[string]any{"id": "howto", "kind": "info", "label": "怎么用"}},
		"label 超长":   {map[string]any{"id": "env", "kind": "select", "label": strings.Repeat("字", 21), "options": []any{map[string]any{"id": "a", "label": "甲"}}}},
		"项数超限": func() []any {
			out := make([]any, 0, 7)
			for _, id := range []string{"a", "b", "c", "d", "e", "f", "g"} {
				out = append(out, map[string]any{"id": id, "kind": "info", "label": id, "info_text": "x"})
			}
			return out
		}(),
		"一项坏掉整包丢": {
			map[string]any{"id": "ok", "kind": "info", "label": "好的", "info_text": "x"},
			map[string]any{"id": "BAD", "kind": "info", "label": "坏的", "info_text": "x"},
		},
	}
	for name, custom := range cases {
		t.Run(name, func(t *testing.T) {
			snapshot, err := acp.New().Build(context.Background(), customBuildInput(custom))
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			for _, item := range snapshot.Items {
				if strings.HasPrefix(item.ItemID, "custom_") {
					t.Fatalf("rendered %q from an invalid payload", item.ItemID)
				}
			}
		})
	}
}

// 外部 agent 报一个叫 stop_output 的 id 也撞不掉内建项。
func TestACPCustomToolbar_IDNamespaceIsolatesBuiltins(t *testing.T) {
	in := customBuildInput([]any{
		map[string]any{"id": "stop_output", "kind": "info", "label": "假的", "info_text": "x"},
	})
	in.Run = toolruntime.RunState{HasActiveRun: true, CanStop: true, RunID: "run-1"}
	snapshot, err := acp.New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	builtin, ok := snapshot.FindItem("stop_output")
	if !ok {
		t.Fatal("builtin stop_output was displaced by the agent payload")
	}
	if builtin.ActionID != "stop_output" || builtin.Kind != toolprotocol.ItemKindButton {
		t.Fatalf("builtin stop_output altered: %+v", builtin)
	}
	if _, ok := snapshot.FindItem("custom_stop_output"); !ok {
		t.Fatal("the agent item was not rendered under the custom_ namespace")
	}
}

// 用户选择通过主人身份的命令文本回传；local_action 没有 run 上下文，
// agent 回的话发不出去，所以这条路必须是命令文本。
func TestACPCustomToolbar_SelectSendsCommandText(t *testing.T) {
	executor := &commandTextExecutor{}
	result, err := acp.New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: customBuildInput(selectItemMeta()),
		Request: toolprotocol.ActionRequest{
			ItemID:   "custom_env",
			ActionID: acp.ActionIDCustomSelect,
			OptionID: "prod",
		},
		Executor: executor,
	})
	if err != nil {
		t.Fatalf("HandleAction() error = %v", err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh {
		t.Fatalf("outcome = %q code=%q", result.Outcome, result.Code)
	}
	if len(executor.sent) != 1 {
		t.Fatalf("command texts = %d, want 1", len(executor.sent))
	}
	sent := executor.sent[0]
	if sent.Content != "grix://toolbar/select?item=env&option=prod" {
		t.Fatalf("content = %q", sent.Content)
	}
	if sent.SessionID != "sess-acp" || sent.AgentID != 9101 || sent.OwnerID != 1001 {
		t.Fatalf("routing = %+v", sent)
	}
	// 命令文本不能走 local_action 通道。
	if len(executor.localActions) != 0 {
		t.Fatalf("local actions = %d, want 0", len(executor.localActions))
	}
}

// 前端可能拿着旧快照点过来，agent 也可能刚撤掉该项：一律以当前快照复核。
func TestACPCustomToolbar_SelectRejectsUnknownItemOrOption(t *testing.T) {
	cases := []toolprotocol.ActionRequest{
		{ItemID: "custom_gone", ActionID: acp.ActionIDCustomSelect, OptionID: "prod"},
		{ItemID: "custom_env", ActionID: acp.ActionIDCustomSelect, OptionID: "nope"},
		{ItemID: "custom_howto", ActionID: acp.ActionIDCustomSelect, OptionID: "prod"},
	}
	for _, req := range cases {
		executor := &commandTextExecutor{}
		result, err := acp.New().HandleAction(context.Background(), core.ActionInput{
			BuildInput: customBuildInput(append(selectItemMeta(), infoItemMeta()...)),
			Request:    req,
			Executor:   executor,
		})
		if err != nil {
			t.Fatalf("HandleAction(%+v) error = %v", req, err)
		}
		if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "invalid_option" {
			t.Fatalf("req=%+v outcome=%q code=%q, want rejected/invalid_option", req, result.Outcome, result.Code)
		}
		if len(executor.sent) != 0 {
			t.Fatalf("req=%+v dispatched %d command texts", req, len(executor.sent))
		}
	}
}

// 离线 / 运行中 / 通道不可用都要当场拒绝，不静默吞掉用户的点击。
func TestACPCustomToolbar_SelectRejectsWhenUndeliverable(t *testing.T) {
	offline := customBuildInput(selectItemMeta())
	offline.Runtime = toolruntime.Profile{Online: false, LocalActions: []string{"session_control"}}

	running := customBuildInput(selectItemMeta())
	running.Run = toolruntime.RunState{HasActiveRun: true, CanStop: true, RunID: "run-1"}

	cases := []struct {
		name     string
		in       core.BuildInput
		executor core.Executor
		wantCode string
	}{
		{"离线", offline, &commandTextExecutor{}, "agent_offline"},
		{"运行中", running, &commandTextExecutor{}, "run_active"},
		{"通道不可用", customBuildInput(selectItemMeta()), &commandTextExecutor{sendErr: errors.New("agent channel unavailable")}, "dispatch_failed"},
		{"执行器不支持命令文本", customBuildInput(selectItemMeta()), &packageTestExecutor{}, "dispatch_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, err := acp.New().HandleAction(context.Background(), core.ActionInput{
				BuildInput: c.in,
				Request: toolprotocol.ActionRequest{
					ItemID:   "custom_env",
					ActionID: acp.ActionIDCustomSelect,
					OptionID: "prod",
				},
				Executor: c.executor,
			})
			if err != nil {
				t.Fatalf("HandleAction() error = %v", err)
			}
			if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != c.wantCode {
				t.Fatalf("outcome=%q code=%q, want rejected/%s", result.Outcome, result.Code, c.wantCode)
			}
		})
	}
}

// 防回归：同样跑在 ACP 适配器上的厂商 CLI 各有自己的工具栏包，不读这份元数据。
func TestACPCustomToolbar_OnlyGenericACPReadsTheMeta(t *testing.T) {
	in := customBuildInput(selectItemMeta())
	in.Agent.ClientType = model.AgentClientTypeGemini
	if acp.New().Match(core.MatchContext{Agent: in.Agent}) {
		t.Fatal("the generic ACP package matched a gemini agent")
	}
}

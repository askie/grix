package agenttoolbar_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/codewhale"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/copilot"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/gemini"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/kimi"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/kiro"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/opencode"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/pi"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/qwen"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/reasonix"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
)

// 模型/模式选择器抽公共构建器前后的线上输出金样：Build() 的 select_model /
// select_mode Item 必须逐字段相同，老连接器上报的 meta 形状不变即老连接器不受影响。
// 重新生成：UPDATE_SELECT_GOLDEN=1 go test ./internal/agenttoolbar -run TestSelectGolden

var selectGoldenPackages = map[string]core.Package{
	"codewhale": codewhale.New(),
	"copilot":   copilot.New(),
	"gemini":    gemini.New(),
	"kimi":      kimi.New(),
	"kiro":      kiro.New(),
	"opencode":  opencode.New(),
	"pi":        pi.New(),
	"qwen":      qwen.New(),
	"reasonix":  reasonix.New(),
}

func selectGoldenModels() []any {
	return []any{
		map[string]any{"id": "m-a", "displayName": "Model A"},
		map[string]any{"id": "m-b", "display_name": "Model B"},
		map[string]any{"id": "m-c"},
		map[string]any{"id": "m-a", "displayName": "dup"},
		map[string]any{"id": ""},
		"junk",
	}
}

func selectGoldenModes() []any {
	return []any{
		map[string]any{"id": "full_auto", "displayName": "Full Auto"},
		map[string]any{"id": "approval", "displayName": "Approval"},
		map[string]any{"id": "plan"},
	}
}

type selectGoldenScenario struct {
	name    string
	online  bool
	actions []string
	meta    map[string]any
	run     toolruntime.RunState
}

func selectGoldenScenarios() []selectGoldenScenario {
	full := []string{"session_control", "set_model", "set_mode", "get_session_usage", "get_rate_limits"}
	return []selectGoldenScenario{
		{name: "online-full", online: true, actions: full,
			meta: map[string]any{"model_id": "m-b", "mode_id": "approval", "available_models": selectGoldenModels(), "available_modes": selectGoldenModes()}},
		{name: "online-current-unknown", online: true, actions: full,
			meta: map[string]any{"model_id": "ghost", "mode_id": "ghost", "available_models": selectGoldenModels(), "available_modes": selectGoldenModes()}},
		{name: "online-empty-current", online: true, actions: full,
			meta: map[string]any{"available_models": selectGoldenModels(), "available_modes": selectGoldenModes()}},
		{name: "online-no-lists", online: true, actions: full,
			meta: map[string]any{"model_id": "m-b", "mode_id": "approval"}},
		{name: "online-no-meta", online: true, actions: full, meta: nil},
		{name: "online-no-actions", online: true, actions: []string{"session_control"},
			meta: map[string]any{"model_id": "m-b", "mode_id": "approval", "available_models": selectGoldenModels(), "available_modes": selectGoldenModes()}},
		{name: "offline", online: false, actions: full,
			meta: map[string]any{"model_id": "m-b", "mode_id": "approval", "available_models": selectGoldenModels(), "available_modes": selectGoldenModes()}},
		{name: "online-active-run", online: true, actions: full,
			run:  toolruntime.RunState{HasActiveRun: true, CanStop: true, State: "running"},
			meta: map[string]any{"model_id": "m-b", "mode_id": "approval", "available_models": selectGoldenModels(), "available_modes": selectGoldenModes()}},
	}
}

func selectGoldenInput(key string, sc selectGoldenScenario) core.BuildInput {
	return core.BuildInput{
		OwnerID:  7,
		Session:  core.SessionInfo{SessionID: "sess-1", SessionType: 1},
		Agent:    core.AgentInfo{AgentID: 11, OwnerID: 7, ClientType: key},
		Language: "zh",
		Runtime: toolruntime.Profile{
			AgentID: 11, OwnerID: 7, ClientType: key, Online: sc.online, LocalActions: sc.actions,
		},
		Binding: core.BindingInfo{
			ProviderKey: key, BindingID: "b-1", Cwd: "/work/demo", Status: "bound", WorkerStatus: "ready", Meta: sc.meta,
		},
		Run: sc.run,
	}
}

func TestSelectGolden(t *testing.T) {
	update := os.Getenv("UPDATE_SELECT_GOLDEN") == "1"
	for key, pkg := range selectGoldenPackages {
		for _, sc := range selectGoldenScenarios() {
			snap, err := pkg.Build(context.Background(), selectGoldenInput(key, sc))
			if err != nil {
				t.Fatalf("%s/%s build: %v", key, sc.name, err)
			}
			got, _ := json.MarshalIndent(toolprotocol.Snapshot{Visible: snap.Visible, Items: snap.Items}, "", "  ")
			path := filepath.Join("testdata", "select-golden", key+"-"+sc.name+".json")
			if update {
				if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s/%s: golden missing (%v); run with UPDATE_SELECT_GOLDEN=1", key, sc.name, err)
			}
			if string(want) != string(got)+"\n" {
				t.Errorf("%s/%s: snapshot drifted from golden\n--- want\n%s\n--- got\n%s", key, sc.name, want, got)
			}
		}
	}
}

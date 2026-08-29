package shared

import (
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
)

func TestParseMetaOptionsContract(t *testing.T) {
	meta := map[string]any{"available_models": []any{
		map[string]any{"id": "a", "displayName": "A"},
		map[string]any{"id": "b", "display_name": "B"},
		map[string]any{"id": "c", "label": "C"},
		map[string]any{"id": "d", "name": "D"},
		map[string]any{"id": "e"},
		map[string]any{"id": "A", "displayName": "dup-case"},
		map[string]any{"id": ""},
		"junk",
	}}
	got := ParseMetaOptions(meta, "available_models")
	want := []string{"a=A", "b=B", "c=C", "d=D", "e=e"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d: %+v", len(got), len(want), got)
	}
	for i, opt := range got {
		if opt.OptionID+"="+opt.Label != want[i] {
			t.Fatalf("option[%d]=%s=%s want %s", i, opt.OptionID, opt.Label, want[i])
		}
	}
	if ParseMetaOptions(nil, "available_models") != nil {
		t.Fatal("nil meta must yield nil")
	}
	if ParseMetaOptions(map[string]any{"available_models": []any{"junk"}}, "available_models") != nil {
		t.Fatal("no valid entries must yield nil (serializes as null)")
	}
}

func TestOptionLabel(t *testing.T) {
	opts := ParseMetaOptions(map[string]any{"k": []any{map[string]any{"id": "GPT-X", "displayName": "GPT X"}}}, "k")
	if OptionLabel("GPT-X", opts) != "GPT X" || OptionLabel("gpt-x", opts) != "GPT X" {
		t.Fatal("exact and case-insensitive match must resolve label")
	}
	if OptionLabel("ghost", opts) != "ghost" || OptionLabel("  ", opts) != "" {
		t.Fatal("unknown falls back to id, empty stays empty")
	}
}

func TestBuildSelectStates(t *testing.T) {
	spec := ModelSelect("Demo")
	spec.Options = ParseMetaOptions(map[string]any{"k": []any{map[string]any{"id": "m"}}}, "k")
	in := func(online bool, actions ...string) core.BuildInput {
		return core.BuildInput{Runtime: toolruntime.Profile{Online: online, LocalActions: actions}}
	}
	cases := []struct {
		name     string
		in       core.BuildInput
		spec     SelectSpec
		disabled bool
		tooltip  string
	}{
		{"ready", in(true, "set_model"), spec, false, "切换 Demo 模型"},
		{"offline", in(false, "set_model"), spec, true, "Demo 当前离线"},
		{"undeclared", in(true), spec, true, "当前插件未声明 set_model"},
		{"waiting", in(true, "set_model"), ModelSelect("Demo"), true, "等待 Demo 模型列表同步"},
		{"static", in(false), func() SelectSpec { s := spec; s.StaticTooltip = true; return s }(), true, "切换 Demo 模型"},
		{"mode-no-wait", in(true, "set_mode"), ModeSelect("Demo"), false, "切换 Demo 模式"},
	}
	for _, tc := range cases {
		item := BuildSelect(tc.in, tc.spec)
		if item.Disabled != tc.disabled || item.Tooltip != tc.tooltip {
			t.Fatalf("%s: disabled=%v tooltip=%q", tc.name, item.Disabled, item.Tooltip)
		}
	}
	empty := ModelSelect("Demo")
	empty.EmptyOptionsTooltip = "暂无可用模型"
	if got := BuildSelect(in(true, "set_model"), empty).Tooltip; got != "暂无可用模型" {
		t.Fatalf("custom empty tooltip: %q", got)
	}
}

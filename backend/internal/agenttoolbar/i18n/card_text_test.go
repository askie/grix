package i18n

import "testing"

func TestT_ReturnsLanguageSpecificText(t *testing.T) {
	if got := T("zh", "usage_timeout"); got != "用量查询超时。" {
		t.Fatalf("zh usage_timeout=%q", got)
	}
	if got := T("en", "usage_timeout"); got != "Usage query timed out." {
		t.Fatalf("en usage_timeout=%q", got)
	}
	if got := T("zh", "unknown_key_xyz"); got != "" {
		t.Fatalf("unknown key should return empty, got %q", got)
	}
}

func TestTf_InterpolatesArgsPerLanguage(t *testing.T) {
	if got := Tf("zh", "bound_path", "/workspace/demo"); got != "已绑定 /workspace/demo" {
		t.Fatalf("zh bound_path=%q", got)
	}
	if got := Tf("en", "bound_path", "/workspace/demo"); got != "Bound to /workspace/demo" {
		t.Fatalf("en bound_path=%q", got)
	}
	if got := Tf("en", "where_path", "Codex", "/repo"); got != "Current Codex workspace is /repo." {
		t.Fatalf("en where_path=%q", got)
	}
}

func TestNormalizeLanguage_DefaultsToZh(t *testing.T) {
	cases := map[string]string{
		"":       "zh",
		"zh":     "zh",
		"zh-CN":  "zh",
		"en":     "en",
		"en-US":  "en",
		"EN":     "en",
		"ja":     "zh",
		"random": "zh",
	}
	for input, want := range cases {
		if got := NormalizeLanguage(input); got != want {
			t.Fatalf("NormalizeLanguage(%q)=%q want=%q", input, got, want)
		}
	}
}

func TestLocalizeText_CodexToolbarSelections(t *testing.T) {
	cases := map[string]string{
		"推理力度":  "Reasoning Effort",
		"无":     "None",
		"极低":    "Minimal",
		"低":     "Low",
		"中":     "Medium",
		"高":     "High",
		"极高":    "Extra High",
		"最大":    "Max",
		"极限":    "Ultra",
		"标准":    "Standard",
		"快速":    "Fast",
		"沙箱模式":  "Sandbox Mode",
		"默认":    "Default",
		"完全访问":  "Full Access",
		"工作区写入": "Workspace Write",
		"只读":    "Read-only",
		"当前模型不支持调整推理力度":             "Current model does not support changing reasoning effort",
		"切换推理力度":                    "Switch reasoning effort",
		"当前模型不支持速度档":                "Current model does not support a speed tier",
		"切换速度档（快速档 1.5x 速度，消耗更多额度）": "Switch speed tier (Fast is 1.5x faster and uses more quota)",
		"切换沙箱模式（重启后生效）":             "Switch sandbox mode (takes effect after restart)",
	}
	for input, want := range cases {
		if got := LocalizeText("en", input); got != want {
			t.Errorf("LocalizeText(%q)=%q want %q", input, got, want)
		}
	}
}

func TestLocalizeText_OtherToolbarAdapters(t *testing.T) {
	cases := map[string]string{
		"Copilot 工作区":      "Copilot Workspace",
		"选择 Kiro 会话操作":     "Select Kiro session action",
		"等待 Cursor 模型列表同步": "Waiting for Cursor model list sync",
		"切换 Reasonix 审批模式": "Switch Reasonix approval mode",
		"切换 CodeWhale 模型":  "Switch CodeWhale model",
		"交互模式":             "Interactive Mode",
		"规划模式":             "Planning Mode",
		"自动驾驶":             "Autopilot",
		"人工确认":             "Manual Confirm",
		"自由模式":             "Free Mode",
		"计划模式":             "Plan Mode",
		"Cursor 模式无效":      "Invalid Cursor mode",
		"切换 Cursor 模式":     "Switch Cursor mode",
		"默认编码":             "Default Coding",
		"使用指南":             "Guidance",
		"会话已过期":            "Session expired",
		"查看支持的斜杠命令":        "View supported slash commands",
		"Hermes 模型由配置固定，点击仅展示当前模型": "Hermes model is fixed by configuration; click to view current model",
		"agy 配额耗尽": "agy quota exhausted",
		"团队 配额":    "团队 quota",
	}
	for input, want := range cases {
		if got := LocalizeText("en", input); got != want {
			t.Errorf("LocalizeText(%q)=%q want %q", input, got, want)
		}
	}
}

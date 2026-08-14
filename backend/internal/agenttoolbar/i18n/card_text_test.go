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

func TestLocalizeText_DeepSeekToolbar(t *testing.T) {
	cases := map[string]string{
		"DeepSeek 会话":                     "DeepSeek Session",
		"DeepSeek 当前离线":                   "DeepSeek is offline",
		"DeepSeek 余额刷新当前不可用":              "DeepSeek balance refresh is unavailable",
		"当前连接未声明会话操作":                     "Current connection does not declare session actions",
		"当前连接未声明 set_provider":            "Current connection does not declare set_provider",
		"选择会话操作":                          "Select session action",
		"查看工作目录":                          "View workspace",
		"关闭会话 Runtime":                    "Stop session runtime",
		"重启会话 Runtime":                    "Restart session runtime",
		"查看会话用量":                          "View session usage",
		"运行模式":                            "Run Mode",
		"选择运行模式":                          "Select run mode",
		"默认（工作区受限）":                       "Default (workspace-limited)",
		"自动（全权限）":                         "Auto (full access)",
		"默认（工作区受限）（待生效）":                  "Default (workspace-limited) (pending)",
		"自动（全权限）（应用失败）":                   "Auto (full access) (failed)",
		"供应商":                             "Provider",
		"选择模型供应商":                         "Select model provider",
		"等待 DeepSeek 供应商列表同步":             "Waiting for DeepSeek provider list sync",
		"等待 DeepSeek 模型列表同步":              "Waiting for DeepSeek model list sync",
		"当前任务运行中，暂不能切换":                   "A task is running; switching is unavailable",
		"设置已保存，等待 Runtime 生效":             "Settings saved, waiting for Runtime to apply",
		"设置已保存，等待 Runtime 生效；revision 12": "Settings saved, waiting for Runtime to apply; revision 12",
		"当前没有可用选项":                        "No options available",
		"上次应用失败，可重试":                      "Last apply failed; retry available",
		"切换后将在空闲状态重建当前会话 Runtime":         "Switching rebuilds the session Runtime when idle",
		"切换后将在空闲状态重建当前会话 Runtime；当前 Runtime: deepseek-v4-pro":      "Switching rebuilds the session Runtime when idle; current Runtime: deepseek-v4-pro",
		"设置已保存，等待 Runtime 生效；revision 12；当前 Runtime: approval":     "Settings saved, waiting for Runtime to apply; revision 12; current Runtime: approval",
		"上次应用失败，可重试；revision 12；apply_failed；当前 Runtime: approval": "Last apply failed; retry available; revision 12; apply_failed; current Runtime: approval",
		"默认（工作区受限）（应用失败）":                                          "Default (workspace-limited) (failed)",
		"点击刷新上下文和 DeepSeek 余额":                                     "Tap to refresh context and DeepSeek balance",
		"DeepSeek 剩余余额 ¥83.52，点击刷新":                                "DeepSeek remaining balance ¥83.52, tap to refresh",
		"剩余余额 $12.34，点击刷新":                                         "Remaining balance $12.34, tap to refresh",
		"OpenRouter 剩余余额，点击刷新":                                     "OpenRouter remaining balance, tap to refresh",
		"剩余余额，点击刷新":                                                "Remaining balance, tap to refresh",
		"会话上下文":                                                    "Session Context",
		"812K / 1M，剩余 188K":                                        "812K / 1M, 188K remaining",
		"已提交上下文和余额刷新请求":                                            "Context and balance refresh request submitted",
		"已提交会话用量查询":                                                "Session usage query submitted",
		"已提交停止当前输出请求":                                              "Stop Current Output request submitted",
		"供应商设置已提交":                                                 "Provider setting submitted",
		"模型设置已提交":                                                  "Model setting submitted",
		"运行模式设置已提交":                                                "Run mode setting submitted",
		"供应商不在当前可用列表中":                                             "Provider is not in the current catalog",
		"模型不在当前可用列表中":                                              "Model is not in the current catalog",
		"运行模式无效":                                                   "Invalid run mode",
		"当前任务运行中，无法切换设置":                                           "A task is running; settings cannot be changed",
		"已有设置正在应用，请稍后重试":                                           "A setting is already applying; try again later",
		"OpenCode Go（待生效）":                                         "OpenCode Go (pending)",
	}
	for input, want := range cases {
		if got := LocalizeText("en", input); got != want {
			t.Errorf("LocalizeText(%q)=%q want %q", input, got, want)
		}
	}
}

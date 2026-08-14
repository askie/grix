package i18n

import (
	"fmt"
	"strings"
)

func NormalizeLanguage(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(v, "en"):
		return "en"
	default:
		return "zh"
	}
}

var zhToEn = map[string]string{
	"自动":          "Auto",
	"计划":          "Plan",
	"默认":          "Default",
	"自动（安全）":      "Auto (Safe)",
	"模式":          "Mode",
	"模型":          "Model",
	"速度档":         "Speed Tier",
	"标准":          "Standard",
	"快速":          "Fast",
	"推理力度":        "Reasoning Effort",
	"无":           "None",
	"极低":          "Minimal",
	"低":           "Low",
	"中":           "Medium",
	"高":           "High",
	"极高":          "Extra High",
	"最大":          "Max",
	"极限":          "Ultra",
	"沙箱模式":        "Sandbox Mode",
	"完全访问":        "Full Access",
	"工作区写入":       "Workspace Write",
	"只读":          "Read-only",
	"沙箱":          "Sandbox",
	"会话已过期":       "Session expired",
	"分享文件":        "Share file",
	"查找联系人":       "Find contacts",
	"查看支持的斜杠命令":   "View supported slash commands",
	"默认编码":        "Default Coding",
	"规划模式":        "Planning Mode",
	"使用指南":        "Guidance",
	"交互模式":        "Interactive Mode",
	"自动驾驶":        "Autopilot",
	"无名":          "Unnamed",
	"暂无可用模型":      "No models available",
	"未选择推理力度":     "No reasoning effort selected",
	"未选择速度档":      "No speed tier selected",
	"未选择沙箱模式":     "No sandbox mode selected",
	"额度查询":        "Quota query",
	"积分":          "Credits",
	"配额耗尽":        "Quota exhausted",
	"耗尽":          "Exhausted",
	"已提交额度查询请求":   "Quota query request submitted",
	"已提交推理力度切换请求": "Reasoning effort change request submitted",
	"已提交速度档切换请求":  "Speed tier change request submitted",
	"已提交沙箱模式切换请求（重启后生效）": "Sandbox mode change request submitted (takes effect after restart)",
	"%.0f 积分":                        "%.0f credits",
	"切模型写 ~/.kimi 全局配置":              "Changing model writes to ~/.kimi global config",
	"当前 Hermes 模型由配置固定":              "Current Hermes model is fixed by configuration",
	"Hermes 模型由配置固定，点击仅展示当前模型":       "Hermes model is fixed by configuration; click to view current model",
	"压缩当前 Kiro 上下文":                  "Compact current Kiro context",
	"在线":                             "Online",
	"离线":                             "Offline",
	"查看状态":                           "View Status",
	"重启会话":                           "Restart Session",
	"停止会话":                           "Stop Session",
	"查看用量":                           "View Usage",
	"默认（需确认）":                        "Default (Confirm)",
	"自动编辑":                           "Auto Edit",
	"全自动":                            "Full Auto",
	"只读计划":                           "Read-Only Plan",
	"人工确认":                           "Manual Confirm",
	"自由模式":                           "Free Mode",
	"计划模式":                           "Plan Mode",
	"Cursor 模式无效":                    "Invalid Cursor mode",
	"停止当前输出":                         "Stop Current Output",
	"正在停止当前输出":                       "Stopping Current Output",
	"当前没有可停止的输出":                     "No active output to stop",
	"已提交停止请求":                        "Stop request submitted",
	"请求":                             "Request",
	"未选择模式":                          "No mode selected",
	"未选择模型":                          "No model selected",
	"已切换模式":                          "Mode switched",
	"已切换模型":                          "Model switched",
	"场景":                             "Scene",
	"当前连接未声明 set_preset":             "Current connection does not declare set_preset",
	"已提交模型切换请求":                      "Model switch request submitted",
	"已提交模式切换请求":                      "Mode switch request submitted",
	"已提交用量查询请求":                      "Usage query request submitted",
	"已提交余量查询请求":                      "Rate limit query request submitted",
	"余量查询":                           "Rate limit query",
	"已提交重启请求":                        "Restart request submitted",
	"已提交 thread_compact 请求":          "thread_compact request submitted",
	"已提交压缩请求":                        "Compact request submitted",
	"工具栏动作无效":                        "Invalid toolbar action",
	"工具栏选项无效":                        "Invalid toolbar option",
	"当前 agent 不在线":                   "Agent is offline",
	"当前 agent 未声明 session_control":   "Agent does not declare session_control",
	"当前 agent 未声明 set_model":         "Agent does not declare set_model",
	"当前 agent 未声明 set_mode":          "Agent does not declare set_mode",
	"当前 agent 未声明 get_session_usage": "Agent does not declare get_session_usage",
	"当前 agent 未声明 get_rate_limits":   "Agent does not declare get_rate_limits",
	"已提交会话操作":                        "Session operation submitted",
	"当前插件未声明 session_control":        "Current plugin does not declare session_control",
	"当前插件未声明 set_model":              "Current plugin does not declare set_model",
	"当前插件未声明 set_mode":               "Current plugin does not declare set_mode",
	"当前插件未声明 get_session_usage":      "Current plugin does not declare get_session_usage",
	"当前插件未声明 thread_compact":         "Current plugin does not declare thread_compact",
	"当前插件未声明 set_model，请升级并重启 grix-connector":    "Current plugin does not declare set_model, please upgrade and restart grix-connector",
	"当前插件未声明 set_mode，请升级并重启 grix-connector":     "Current plugin does not declare set_mode, please upgrade and restart grix-connector",
	"当前 Codex 插件未声明 set_model，请升级并重启 grix-codex": "Current Codex plugin does not declare set_model, please upgrade and restart grix-codex",
	"当前 Codex 插件未声明 set_mode，请升级并重启 grix-codex":  "Current Codex plugin does not declare set_mode, please upgrade and restart grix-codex",
	"技能":               "Skills",
	"暂无可用技能":           "No skills available",
	"查看可用技能":           "View available skills",
	"会话上下文":            "Session Context",
	"Kimi 会话上下文用量（只读）": "Kimi session context usage (read-only)",
	"上下文用量仅展示，Kimi 未提供压缩操作": "Context usage is display-only; Kimi does not provide compaction",
	"压缩上下文":                "Compact Context",
	"压缩当前会话上下文":            "Compact current session context",
	"压缩当前 Codex 上下文":       "Compact current Codex context",
	"剩余 %.1f%%":            "%.1f%% remaining",
	"切换 Codex 协作模式":        "Switch Codex collaboration mode",
	"切换 Codex 模型":          "Switch Codex model",
	"切换 Claude 执行模式并重启会话":  "Switch Claude execution mode and restart session",
	"切换 Claude 模型（重启会话生效）": "Switch Claude model (applies after session restart)",
	"切换 Gemini 模型":         "Switch Gemini model",
	"切换 Gemini 审批模式":       "Switch Gemini approval mode",
	"切换 Pi 模型":             "Switch Pi model",
	"切换 Qwen 模型":           "Switch Qwen model",
	"切换 Kimi 模型":           "Switch Kimi model",
	"切换 Kimi 模式":           "Switch Kimi mode",
	"切换 Qwen 审批模式":         "Switch Qwen approval mode",
	"当前模型不支持调整推理力度":        "Current model does not support changing reasoning effort",
	"切换推理力度":               "Switch reasoning effort",
	"当前模型不支持速度档":           "Current model does not support a speed tier",
	"切换速度档（快速档 1.5x 速度，消耗更多额度）": "Switch speed tier (Fast is 1.5x faster and uses more quota)",
	"切换沙箱模式（重启后生效）":             "Switch sandbox mode (takes effect after restart)",
	"查询当前会话用量":                  "Query current session usage",
	"查看费用":                      "View Cost",
	"回退":                        "Rollback",
	"回退对话":                      "Rewind Conversation",
	"重启":                        "Restart",
	"审批":                        "Approval",
	"审批模式":                      "Approval Mode",
	"清除对话":                      "Clear Conversation",
	"记忆管理":                      "Memory Management",
	"诊断":                        "Diagnostics",
	"选择模式":                      "Select Mode",
	"选择模型":                      "Select Model",
	"选择审批模式":                    "Select Approval Mode",
	"等待 Claude 模型列表同步":          "Waiting for Claude model list sync",
	"等待 Codex 模型列表同步":           "Waiting for Codex model list sync",
	"等待 Pi 模型列表同步":              "Waiting for Pi model list sync",
	"等待 Qwen 模型列表同步":            "Waiting for Qwen model list sync",
	"等待 Kimi 模型列表同步":            "Waiting for Kimi model list sync",
	"Gemini 自动 (2.5)":           "Gemini Auto (2.5)",
	"Gemini 工作区":                "Gemini Workspace",
	"%d小时%d分钟":                  "%dh%dm",
	"%d小时":                      "%dh",
	"%d分钟":                      "%dm",
	"0分钟":                       "0m",
}

func LocalizeText(lang, text string) string {
	if NormalizeLanguage(lang) != "en" {
		return text
	}
	t := strings.TrimSpace(text)
	if t == "" {
		return text
	}
	if mapped, ok := zhToEn[t]; ok {
		return mapped
	}
	if strings.HasPrefix(t, "选择 ") && strings.HasSuffix(t, " 会话操作") {
		agent := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "选择 "), " 会话操作"))
		if agent != "" {
			return fmt.Sprintf("Select %s session action", agent)
		}
	}
	if strings.HasPrefix(t, "当前 agent 未声明 ") {
		return "Agent does not declare " + strings.TrimSpace(strings.TrimPrefix(t, "当前 agent 未声明 "))
	}
	if strings.HasPrefix(t, "当前插件未声明 ") {
		return "Current plugin does not declare " + strings.TrimSpace(strings.TrimPrefix(t, "当前插件未声明 "))
	}
	if strings.HasPrefix(t, "等待 ") && strings.HasSuffix(t, " 模型列表同步") {
		agent := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "等待 "), " 模型列表同步"))
		if agent != "" {
			return fmt.Sprintf("Waiting for %s model list sync", agent)
		}
	}
	if strings.HasSuffix(t, " 配额耗尽") {
		plan := strings.TrimSpace(strings.TrimSuffix(t, " 配额耗尽"))
		if plan != "" {
			return fmt.Sprintf("%s quota exhausted", plan)
		}
	}
	if strings.HasSuffix(t, " 配额") {
		plan := strings.TrimSpace(strings.TrimSuffix(t, " 配额"))
		if plan != "" {
			return fmt.Sprintf("%s quota", plan)
		}
	}
	if strings.HasPrefix(t, "切换 ") {
		label := strings.TrimSpace(strings.TrimPrefix(t, "切换 "))
		for suffix, format := range map[string]string{
			" 审批模式": "Switch %s approval mode",
			" 模型":   "Switch %s model",
			" 模式":   "Switch %s mode",
		} {
			if strings.HasSuffix(label, suffix) {
				agent := strings.TrimSpace(strings.TrimSuffix(label, suffix))
				if agent != "" {
					return fmt.Sprintf(format, agent)
				}
			}
		}
	}
	if strings.HasSuffix(t, " 工作区") {
		agent := strings.TrimSpace(strings.TrimSuffix(t, " 工作区"))
		if agent != "" {
			return fmt.Sprintf("%s Workspace", agent)
		}
	}
	if strings.HasPrefix(t, "已提交") && strings.HasSuffix(t, "请求") {
		mid := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "已提交"), "请求"))
		if mid != "" {
			return fmt.Sprintf("%s request submitted", LocalizeText(lang, mid))
		}
	}
	if strings.HasPrefix(t, "剩余 ") && strings.HasSuffix(t, "%") {
		return strings.TrimSpace(strings.TrimPrefix(t, "剩余 ")) + " remaining"
	}
	if strings.HasSuffix(t, " 当前离线") {
		agent := strings.TrimSpace(strings.TrimSuffix(t, " 当前离线"))
		if agent != "" {
			return fmt.Sprintf("%s is offline", agent)
		}
	}
	if strings.HasSuffix(t, " 会话") {
		agent := strings.TrimSpace(strings.TrimSuffix(t, " 会话"))
		if agent != "" {
			return fmt.Sprintf("%s Session", agent)
		}
	}
	if strings.Contains(t, " 会话操作\n工作目录: ") {
		parts := strings.SplitN(t, " 会话操作\n工作目录: ", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("%s Session Actions\nWorkspace: %s", strings.TrimSpace(parts[0]), parts[1])
		}
	}
	if strings.HasSuffix(t, " 会话操作") {
		agent := strings.TrimSpace(strings.TrimSuffix(t, " 会话操作"))
		if agent != "" {
			return fmt.Sprintf("%s Session Actions", agent)
		}
	}
	return text
}

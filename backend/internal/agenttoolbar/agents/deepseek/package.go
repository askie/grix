// Package deepseek implements the server-driven toolbar for DeepSeek Harness.
package deepseek

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/shared"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

type Package struct{}

func New() *Package            { return &Package{} }
func (p *Package) Key() string { return model.AgentClientTypeDeepSeek }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeDeepSeek
}

func (p *Package) Build(_ context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
	if !hasSessionBinding(in.Binding) {
		return toolprotocol.Snapshot{Visible: false, Items: []toolprotocol.Item{}}, nil
	}

	items := make([]toolprotocol.Item, 0, 10)
	runState := strings.TrimSpace(in.Run.State)
	if in.Run.HasActiveRun && (in.Run.CanStop || runState == "stopping") {
		items = append(items, toolprotocol.Item{
			ItemID:   "stop_output",
			GroupID:  "run_control",
			Kind:     toolprotocol.ItemKindButton,
			ActionID: "stop_output",
			Icon:     "stop",
			Variant:  "danger",
			Disabled: !in.Run.CanStop,
			Tooltip:  stopOutputTooltip(runState),
			Loading:  runState == "stopping",
			Selected: runState == "stopping",
		})
	}

	items = append(items, buildSessionControlItem(in))

	canRefreshQuota := in.Runtime.Online && in.Runtime.HasLocalAction("get_rate_limits")
	quotaItems := shared.BuildProviderQuotaItems(shared.ParseProviderQuota(in.Binding.Meta), canRefreshQuota)
	for i := range quotaItems {
		quotaItems[i].Disabled = !canRefreshQuota
		if !canRefreshQuota {
			quotaItems[i].Tooltip = "DeepSeek 余额刷新当前不可用"
		}
	}
	items = append(items, quotaItems...)

	if contextItem, ok := buildContextUsageItem(in); ok {
		items = append(items, contextItem)
	}

	if profileItem, ok := buildProfileItem(in); ok {
		items = append(items, profileItem)
	}
	if presetItem, ok := buildPresetItem(in); ok {
		items = append(items, presetItem)
	}
	items = append(items, buildModeItem(in))
	if providerItem, ok := buildProviderItem(in); ok {
		items = append(items, providerItem)
	}
	items = append(items, buildModelItem(in))
	items = append(items, buildThinkingItem(in))
	if effortItem, ok := buildReasoningEffortItem(in); ok {
		items = append(items, effortItem)
	}
	if pluginItem, ok := buildPluginsItem(in); ok {
		items = append(items, pluginItem)
	}
	if skillItem, ok := buildSkillsItem(in); ok {
		items = append(items, skillItem)
	}

	return toolprotocol.Snapshot{Visible: true, Items: items}, nil
}

func buildSessionControlItem(in core.BuildInput) toolprotocol.Item {
	hasSessionControl := in.Runtime.HasLocalAction("session_control")
	hasUsage := in.Runtime.HasLocalAction("get_session_usage")
	disabled := !in.Runtime.Online || (!hasSessionControl && !hasUsage)
	tooltip := strings.TrimSpace(in.Binding.Cwd)
	if !in.Runtime.Online {
		tooltip = "DeepSeek 当前离线"
	} else if !hasSessionControl && !hasUsage {
		tooltip = "当前连接未声明会话操作"
	}

	badge := "离线"
	if cwd := strings.TrimSpace(in.Binding.Cwd); cwd != "" {
		badge = shared.PathBase(cwd)
	} else if worker := strings.TrimSpace(in.Binding.WorkerStatus); worker != "" {
		badge = worker
	} else if in.Runtime.Online {
		badge = "在线"
	}

	return toolprotocol.Item{
		ItemID:      "session_control",
		GroupID:     "session_control",
		Kind:        toolprotocol.ItemKindSelect,
		ActionID:    "session_control",
		Icon:        "status",
		Variant:     "secondary",
		Disabled:    disabled,
		Tooltip:     tooltip,
		BadgeText:   badge,
		Placeholder: "选择会话操作",
		Options: []toolprotocol.Option{
			{OptionID: "status", Label: "查看状态", Disabled: !hasSessionControl},
			{OptionID: "where", Label: "查看工作目录", Disabled: !hasSessionControl},
			{OptionID: "stop", Label: "关闭会话 Runtime", Disabled: !hasSessionControl},
			{OptionID: "restart", Label: "重启会话 Runtime", Disabled: !hasSessionControl},
			{OptionID: "unbind", Label: "解绑", Disabled: !hasSessionControl},
			{OptionID: "usage", Label: "查看会话用量", Disabled: !hasUsage},
		},
	}
}

func buildModeItem(in core.BuildInput) toolprotocol.Item {
	state := settingsState(in.Binding.Meta)
	value := metaString(in.Binding.Meta, "mode_id")
	if value == "" {
		value = "approval"
	}
	disabled, tooltip := settingsSelectorState(in, "set_mode", state, true)
	badge, variant := settingsBadge(modeLabel(value), state)
	tooltip = appendSettingsProjection(tooltip, in.Binding.Meta, "applied_mode_id")

	return toolprotocol.Item{
		ItemID:      "select_mode",
		GroupID:     "mode_control",
		Kind:        toolprotocol.ItemKindSelect,
		ActionID:    "select_mode",
		Label:       "",
		Icon:        "shield",
		Variant:     variant,
		Disabled:    disabled,
		Loading:     state == "pending",
		Tooltip:     tooltip,
		Value:       value,
		BadgeText:   badge,
		Placeholder: "选择权限",
		Options: []toolprotocol.Option{
			{OptionID: "approval", Label: "默认"},
			{OptionID: "full_auto", Label: "自动"},
		},
	}
}

func buildThinkingItem(in core.BuildInput) toolprotocol.Item {
	state := settingsState(in.Binding.Meta)
	value := metaString(in.Binding.Meta, "thinking_mode")
	if value != "disabled" {
		value = "enabled"
	}
	disabled, tooltip := settingsSelectorState(in, "set_thinking", state, true)
	tooltip = appendSettingsProjection(tooltip, in.Binding.Meta, "applied_thinking_mode")
	label := "开启"
	if value == "disabled" {
		label = "关闭"
	}
	badge, variant := settingsBadge(label, state)
	return toolprotocol.Item{
		ItemID: "select_thinking", GroupID: "thinking_control", Kind: toolprotocol.ItemKindSelect,
		ActionID: "select_thinking", Icon: "spark", Variant: variant, Disabled: disabled,
		Loading: state == "pending", Tooltip: tooltip, Value: value, BadgeText: badge,
		Label: "Thinking", Placeholder: "Thinking", Options: []toolprotocol.Option{
			{OptionID: "enabled", Label: "开启"},
			{OptionID: "disabled", Label: "关闭"},
		},
	}
}

func buildReasoningEffortItem(in core.BuildInput) (toolprotocol.Item, bool) {
	// Thinking 关闭时推理力度无意义，直接不渲染档次按钮。
	if metaString(in.Binding.Meta, "thinking_mode") == "disabled" {
		return toolprotocol.Item{}, false
	}
	state := settingsState(in.Binding.Meta)
	value := metaString(in.Binding.Meta, "reasoning_effort")
	if value != "max" {
		value = "high"
	}
	disabled, tooltip := settingsSelectorState(in, "set_reasoning_effort", state, true)
	tooltip = appendSettingsProjection(tooltip, in.Binding.Meta, "applied_reasoning_effort")
	label := "高"
	if value == "max" {
		label = "最高"
	}
	badge, variant := settingsBadge(label, state)
	return toolprotocol.Item{
		ItemID: "select_reasoning_effort", GroupID: "effort_control", Kind: toolprotocol.ItemKindSelect,
		ActionID: "select_reasoning_effort", Icon: "spark", Variant: variant, Disabled: disabled,
		Loading: state == "pending", Tooltip: tooltip, Value: value, BadgeText: badge,
		Placeholder: "推理力度", Options: []toolprotocol.Option{
			{OptionID: "high", Label: "高"},
			{OptionID: "max", Label: "最高"},
		},
	}, true
}

func buildPresetItem(in core.BuildInput) (toolprotocol.Item, bool) {
	if metaBool(in.Binding.Meta, "agent_preset_locked") {
		return toolprotocol.Item{}, false
	}
	options := presetOptions(in.Binding.Meta)
	if len(options) == 0 {
		return toolprotocol.Item{}, false
	}
	value := metaString(in.Binding.Meta, "agent_preset_id")
	if value == "" {
		value = "standard"
	}
	disabled, tooltip := presetSelectorState(in, len(options) > 0)
	return toolprotocol.Item{
		ItemID:      "select_preset",
		GroupID:     "preset_control",
		Kind:        toolprotocol.ItemKindSelect,
		ActionID:    "select_preset",
		Label:       "",
		Icon:        "layers",
		Variant:     "secondary",
		Disabled:    disabled,
		Tooltip:     tooltip,
		Value:       value,
		BadgeText:   optionLabel(value, options),
		Placeholder: "选择会话场景",
		Options:     protocolOptions(options),
	}, true
}

// createProfileOptionID 是 Profile 选择器尾部的伪选项：前端命中后弹输入框收集新
// Profile 名，再以 create_profile action 把名字放在 option_id 里发回。
const createProfileOptionID = "__create__"

// defaultDshProfileID 与 connector 的 DEFAULT_DSH_PROFILE_NAME 对齐：插件托管的
// web Profile 是兜底选中值。
const defaultDshProfileID = "web"

func buildProfileItem(in core.BuildInput) (toolprotocol.Item, bool) {
	// 与 preset 一致：会话创建后 Profile 锁定，整个选择器收起。
	if metaBool(in.Binding.Meta, "dsh_profile_locked") {
		return toolprotocol.Item{}, false
	}
	options := catalogOptions(in.Binding.Meta, "available_profiles")
	value := metaString(in.Binding.Meta, "dsh_profile")
	if value == "" {
		// 只在目录里确有 web 时才兜底选中它；目录里没有就不虚构选中值，
		// 留空由用户显式选择（同 select_model 的未选中语义）。
		if _, ok := findOption(defaultDshProfileID, options); ok {
			value = defaultDshProfileID
		}
	}
	if len(options) == 0 && metaString(in.Binding.Meta, "dsh_profile") == "" {
		// 旧版 connector 不上报 Profile 目录，不出项避免噪音。
		return toolprotocol.Item{}, false
	}
	disabled, tooltip := profileSelectorState(in, len(options) > 0)
	protocolOpts := protocolOptions(options)
	// 目录里真存在同名 profile（CLI 可建）时不再追加伪选项，避免撞名。
	_, createCollision := findOption(createProfileOptionID, options)
	if metaBool(in.Binding.Meta, "dsh_profile_create") && !createCollision {
		protocolOpts = append(protocolOpts, toolprotocol.Option{OptionID: createProfileOptionID, Label: "＋ 新建 Profile…"})
	}
	return toolprotocol.Item{
		ItemID:      "dsh_profile",
		GroupID:     "profile_control",
		Kind:        toolprotocol.ItemKindSelect,
		ActionID:    "select_profile",
		Label:       "",
		Icon:        "profile",
		Variant:     "secondary",
		Disabled:    disabled,
		Tooltip:     tooltip,
		Value:       value,
		BadgeText:   profileBadgeText(value, options),
		Placeholder: "选择 Profile",
		Options:     protocolOpts,
	}, true
}

func profileBadgeText(value string, options []option) string {
	return optionLabel(value, options)
}

func profileSelectorState(in core.BuildInput, hasOptions bool) (bool, string) {
	switch {
	case !in.Runtime.Online:
		return true, "DeepSeek 当前离线"
	case !in.Runtime.HasLocalAction("set_profile"):
		return true, "当前连接未声明 set_profile"
	case in.Run.HasActiveRun:
		return true, "当前任务运行中，暂不能切换"
	case !hasOptions:
		return true, "当前没有可用 Profile"
	default:
		return false, "创建会话前选择或新建 Profile；开始对话后不能再改"
	}
}

// normalizeDshProfileName 提前拒掉非法名字，不浪费一次到 connector 的往返。
// 规则基于 connector 的 sanitizeProfileName + assertSelectableDshProfileName
// （非空、非 . / ..、无路径分隔符和 NUL、拒绝保留名 headless）；
// ≤64 字符是 grix 自加的 UI 上限（connector 本身不设长度限制），按 rune 计数，
// 与前端输入框 maxLength: 64（Dart 按字符）对齐，中文名不会两端判定不一致。
func normalizeDshProfileName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" || name == "." || name == ".." || utf8.RuneCountInString(name) > 64 {
		return "", false
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return "", false
	}
	if name == "headless" {
		// headless 是 connector 的一次性 DSH profile，不能作为 Grix Profile。
		return "", false
	}
	return name, true
}

func presetSelectorState(in core.BuildInput, hasOptions bool) (bool, string) {
	switch {
	case !in.Runtime.Online:
		return true, "DeepSeek 当前离线"
	case !in.Runtime.HasLocalAction("set_preset"):
		return true, "当前连接未声明 set_preset"
	case in.Run.HasActiveRun:
		return true, "当前任务运行中，暂不能切换"
	case !hasOptions:
		return true, "当前没有可用场景"
	default:
		return false, "创建会话前选择场景；选定并开始对话后不能再改"
	}
}

func buildProviderItem(in core.BuildInput) (toolprotocol.Item, bool) {
	options := catalogOptions(in.Binding.Meta, "available_providers")
	if len(options) == 0 {
		return toolprotocol.Item{}, false
	}
	state := settingsState(in.Binding.Meta)
	disabled, tooltip := settingsSelectorState(in, "set_provider", state, true)
	value := metaString(in.Binding.Meta, "provider_id")
	tooltip = appendSettingsProjection(tooltip, in.Binding.Meta, "applied_provider_id")
	// failed 态切 warning variant，前端据此渲染叹号图标。
	variant := "primary"
	if state == "failed" {
		variant = "warning"
	}

	return toolprotocol.Item{
		ItemID:      "select_provider",
		GroupID:     "provider_control",
		Kind:        toolprotocol.ItemKindSelect,
		ActionID:    "select_provider",
		Label:       "",
		Icon:        "server",
		Variant:     variant,
		Disabled:    disabled,
		Loading:     state == "pending",
		Tooltip:     tooltip,
		Value:       value,
		BadgeText:   optionLabel(value, options),
		Placeholder: "选择供应商",
		Options:     protocolOptions(options),
	}, true
}

func buildModelItem(in core.BuildInput) toolprotocol.Item {
	options := modelOptions(in.Binding.Meta)
	state := settingsState(in.Binding.Meta)
	disabled, tooltip := settingsSelectorState(in, "set_model", state, len(options) > 0)
	if len(options) == 0 && state != "pending" && in.Runtime.Online && in.Runtime.HasLocalAction("set_model") {
		tooltip = "等待 DeepSeek 模型列表同步"
	}
	value := metaString(in.Binding.Meta, "model_id")
	if _, ok := findOption(value, options); !ok {
		value = ""
	}
	badge, variant := settingsBadge(optionLabel(value, options), state)
	if variant != "warning" {
		variant = "primary"
	}
	tooltip = appendSettingsProjection(tooltip, in.Binding.Meta, "applied_model_id")

	return toolprotocol.Item{
		ItemID:      "select_model",
		GroupID:     "model_control",
		Kind:        toolprotocol.ItemKindSelect,
		ActionID:    "select_model",
		Label:       "",
		Icon:        "cpu",
		Variant:     variant,
		Disabled:    disabled,
		Loading:     state == "pending",
		Tooltip:     tooltip,
		Value:       value,
		BadgeText:   badge,
		Placeholder: "选择模型",
		Options:     protocolOptions(options),
	}
}

func settingsSelectorState(in core.BuildInput, action, state string, hasOptions bool) (bool, string) {
	switch {
	case !in.Runtime.Online:
		return true, "DeepSeek 当前离线"
	case !in.Runtime.HasLocalAction(action):
		return true, "当前连接未声明 " + action
	case in.Run.HasActiveRun:
		return true, "当前任务运行中，暂不能切换"
	case state == "pending":
		return true, settingsStatusText(in.Binding.Meta, "设置已保存，等待 Runtime 生效")
	case !hasOptions:
		return true, "当前没有可用选项"
	case state == "failed":
		return false, settingsStatusText(in.Binding.Meta, "上次应用失败，可重试")
	default:
		return false, "切换后将在空闲状态重建当前会话 Runtime"
	}
}

func settingsStatusText(meta map[string]any, fallback string) string {
	parts := []string{fallback}
	if revision, ok := metaNumber(meta, "settings_revision"); ok {
		parts = append(parts, fmt.Sprintf("revision %.0f", revision))
	}
	if code := metaString(meta, "settings_error_code"); code != "" {
		parts = append(parts, code)
	}
	return strings.Join(parts, "；")
}

func appendSettingsProjection(tooltip string, meta map[string]any, appliedKey string) string {
	applied := metaString(meta, appliedKey)
	if applied == "" {
		return tooltip
	}
	return strings.TrimSpace(tooltip + "；当前 Runtime: " + applied)
}

func buildContextUsageItem(in core.BuildInput) (toolprotocol.Item, bool) {
	raw, ok := in.Binding.Meta["context_window"]
	if !ok || raw == nil {
		return toolprotocol.Item{}, false
	}
	window, ok := raw.(map[string]any)
	if !ok {
		return toolprotocol.Item{}, false
	}
	usedPercentage, ok := metaNumber(window, "usedPercentage")
	if !ok {
		return toolprotocol.Item{}, false
	}
	usedPercentage = clampPercent(usedPercentage)
	used, _ := metaNumber(window, "usedTokens")
	total, _ := metaNumber(window, "totalTokens")
	remaining, hasRemaining := metaNumber(window, "remainingTokens")
	if !hasRemaining && total >= used {
		remaining = total - used
	}
	detail := fmt.Sprintf("%s / %s，剩余 %s", compactTokens(used), compactTokens(total), compactTokens(remaining))
	variant := "secondary"
	if usedPercentage >= 80 {
		variant = "warning"
	}
	canRefresh := in.Runtime.Online && in.Runtime.HasLocalAction("get_rate_limits")
	return toolprotocol.Item{
		ItemID:         "context_usage",
		GroupID:        "session_usage",
		Kind:           toolprotocol.ItemKindProgress,
		ActionID:       "get_rate_limits",
		Variant:        variant,
		Disabled:       !canRefresh,
		Percent:        usedPercentage,
		CenterText:     shared.PercentCenterText(usedPercentage) + "%",
		ProgressDesc:   "会话上下文",
		ProgressDetail: detail,
		Tooltip:        "点击刷新上下文和 DeepSeek 余额",
		LocalAction:    "get_rate_limits",
	}, true
}

func (p *Package) HandleAction(_ context.Context, in core.ActionInput) (toolprotocol.ActionResult, error) {
	switch strings.TrimSpace(in.Request.ActionID) {
	case "stop_output":
		return handleStopOutput(in)
	case "session_control":
		return handleSessionControl(in)
	case "select_preset":
		return handleSelectPreset(in)
	case "select_profile":
		return handleSelectProfile(in)
	case "create_profile":
		return handleCreateProfile(in)
	case "select_provider":
		return handleSelectProvider(in)
	case "select_model":
		return handleSelectModel(in)
	case "select_mode":
		return handleSelectMode(in)
	case "select_thinking":
		return handleSelectThinking(in)
	case "select_reasoning_effort":
		return handleSelectReasoningEffort(in)
	case "dsh_plugins":
		return handlePlugins(in)
	case "dsh_skills":
		return handleSkills(in)
	case "get_rate_limits", "provider_quota_balance", "provider_quota_error", "provider_quota_five_hour", "provider_quota_weekly_limit":
		return dispatch(in, "get_rate_limits", map[string]any{
			"session_id": in.BuildInput.Session.SessionID,
			"actor_id":   fmt.Sprintf("%d", in.BuildInput.OwnerID),
		}, 20_000, "已提交上下文和余额刷新请求")
	default:
		return rejected("invalid_action", "工具栏动作无效"), nil
	}
}

func handleStopOutput(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !in.BuildInput.Run.HasActiveRun || !in.BuildInput.Run.CanStop {
		return rejected("stop_unavailable", "当前没有可停止的输出"), nil
	}
	if err := in.Executor.StopOutput(context.Background(), core.StopOutputRequest{
		OwnerID: in.BuildInput.OwnerID, SessionID: in.BuildInput.Session.SessionID,
		AgentID: in.BuildInput.Agent.AgentID, RunID: in.BuildInput.Run.RunID,
	}); err != nil {
		return rejected("stop_failed", err.Error()), nil
	}
	return accepted(toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh, "已提交停止当前输出请求"), nil
}

func handleSessionControl(in core.ActionInput) (toolprotocol.ActionResult, error) {
	verb := strings.TrimSpace(in.Request.OptionID)
	if verb == "usage" {
		return dispatch(in, "get_session_usage", actionParams(in, nil), 20_000, "已提交会话用量查询")
	}
	switch verb {
	case "status", "where", "stop", "restart", "unbind":
		return dispatch(in, "session_control", actionParams(in, map[string]any{"verb": verb}), 15_000, "已提交会话操作")
	default:
		return rejected("invalid_option", "工具栏选项无效"), nil
	}
}

func handleSelectPreset(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if in.BuildInput.Run.HasActiveRun {
		return rejected("worker_busy", "当前任务运行中，无法切换场景"), nil
	}
	if metaBool(in.BuildInput.Binding.Meta, "agent_preset_locked") {
		return rejected("agent_preset_locked", "场景已锁定，当前会话不能更换"), nil
	}
	presetID := strings.TrimSpace(in.Request.OptionID)
	options := presetOptions(in.BuildInput.Binding.Meta)
	label, ok := findOption(presetID, options)
	if !ok {
		return rejected("invalid_option", "场景不在当前可用列表中"), nil
	}
	return dispatch(in, "set_preset", actionParams(in, map[string]any{
		"agent_preset_id": presetID, "display_label": label,
	}), 15_000, "场景设置已提交")
}

func handleSelectProfile(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if in.BuildInput.Run.HasActiveRun {
		return rejected("worker_busy", "当前任务运行中，无法切换 Profile"), nil
	}
	if metaBool(in.BuildInput.Binding.Meta, "dsh_profile_locked") {
		return rejected("profile_locked", "Profile 已锁定，当前会话不能更换"), nil
	}
	profileID := strings.TrimSpace(in.Request.OptionID)
	options := catalogOptions(in.BuildInput.Binding.Meta, "available_profiles")
	label, ok := findOption(profileID, options)
	if !ok {
		if profileID == createProfileOptionID {
			return rejected("profile_invalid", "Profile 名无效：不能为空、不能是 headless、不能包含路径分隔符"), nil
		}
		if metaBool(in.BuildInput.Binding.Meta, "dsh_profile_create") {
			return handleCreateProfile(in)
		}
		return rejected("invalid_option", "Profile 不在当前可用列表中"), nil
	}
	// set_profile 会装/启动目标 Profile 的 Bridge 并等 ready，比纯设置切换慢。
	return dispatch(in, "set_profile", actionParams(in, map[string]any{
		"profile_id": profileID, "display_label": label,
	}), 30_000, "Profile 设置已提交")
}

func handleCreateProfile(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if in.BuildInput.Run.HasActiveRun {
		return rejected("worker_busy", "当前任务运行中，无法新建 Profile"), nil
	}
	if metaBool(in.BuildInput.Binding.Meta, "dsh_profile_locked") {
		return rejected("profile_locked", "Profile 已锁定，当前会话不能新建"), nil
	}
	if !metaBool(in.BuildInput.Binding.Meta, "dsh_profile_create") {
		return rejected("profile_create_unavailable", "当前连接不支持新建 Profile"), nil
	}
	name, ok := normalizeDshProfileName(in.Request.OptionID)
	if !ok {
		return rejected("profile_invalid", "Profile 名无效：不能为空、不能是 headless、不能包含路径分隔符"), nil
	}
	if _, exists := findOption(name, catalogOptions(in.BuildInput.Binding.Meta, "available_profiles")); exists {
		return rejected("profile_exists", "同名 Profile 已存在，请直接选择"), nil
	}
	// create_profile 要建 Profile 目录并装/启动 Bridge、等 ready；connector 侧
	// dsh 命令自身超时就是 60s，冷机容易踩线，放宽到 120s。
	return dispatch(in, "create_profile", actionParams(in, map[string]any{
		"profile_id": name, "display_label": name,
	}), 120_000, "新建 Profile 已提交")
}

func handleSelectProvider(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if result, blocked := rejectSettingsChange(in); blocked {
		return result, nil
	}
	providerID := strings.TrimSpace(in.Request.OptionID)
	options := catalogOptions(in.BuildInput.Binding.Meta, "available_providers")
	label, ok := findOption(providerID, options)
	if !ok {
		return rejected("invalid_option", "供应商不在当前可用列表中"), nil
	}
	return dispatch(in, "set_provider", actionParams(in, map[string]any{
		"provider_id": providerID, "display_label": label,
	}), 15_000, "供应商设置已提交")
}

func handleSelectModel(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if result, blocked := rejectSettingsChange(in); blocked {
		return result, nil
	}
	modelID := strings.TrimSpace(in.Request.OptionID)
	options := modelOptions(in.BuildInput.Binding.Meta)
	label, ok := findOption(modelID, options)
	if !ok {
		return rejected("invalid_option", "模型不在当前可用列表中"), nil
	}
	return dispatch(in, "set_model", actionParams(in, map[string]any{
		"model_id": modelID, "display_label": label,
	}), 15_000, "模型设置已提交")
}

func handleSelectMode(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if result, blocked := rejectSettingsChange(in); blocked {
		return result, nil
	}
	modeID := strings.TrimSpace(in.Request.OptionID)
	if modeID != "approval" && modeID != "full_auto" {
		return rejected("invalid_option", "权限无效"), nil
	}
	return dispatch(in, "set_mode", actionParams(in, map[string]any{
		"mode_id": modeID, "display_label": modeLabel(modeID),
	}), 15_000, "权限设置已提交")
}

func handleSelectThinking(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if result, blocked := rejectSettingsChange(in); blocked {
		return result, nil
	}
	value := strings.TrimSpace(in.Request.OptionID)
	if value != "enabled" && value != "disabled" {
		return rejected("invalid_option", "Thinking 设置无效"), nil
	}
	return dispatch(in, "set_thinking", actionParams(in, map[string]any{
		"thinking_mode": value,
	}), 15_000, "Thinking 设置已提交")
}

func handleSelectReasoningEffort(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if result, blocked := rejectSettingsChange(in); blocked {
		return result, nil
	}
	if metaString(in.BuildInput.Binding.Meta, "thinking_mode") == "disabled" {
		return rejected("thinking_disabled", "Thinking 关闭时不能修改推理力度"), nil
	}
	value := strings.TrimSpace(in.Request.OptionID)
	if value != "high" && value != "max" {
		return rejected("invalid_option", "推理力度无效"), nil
	}
	return dispatch(in, "set_reasoning_effort", actionParams(in, map[string]any{
		"reasoning_effort": value,
	}), 15_000, "推理力度设置已提交")
}

func buildPluginsItem(in core.BuildInput) (toolprotocol.Item, bool) {
	toggles := pluginToggles(in.Binding.Meta)
	hasList := in.Runtime.HasLocalAction("dsh_list_plugins")
	if !hasList && len(toggles) == 0 {
		return toolprotocol.Item{}, false
	}
	restartRequired := metaBool(in.Binding.Meta, "dsh_plugin_restart_required")
	disabled := !in.Runtime.Online || !hasList || in.Run.HasActiveRun
	tooltip := "查看并开关已安装的 Profile 插件"
	switch {
	case !in.Runtime.Online:
		tooltip = "DeepSeek 当前离线"
	case !hasList:
		tooltip = "当前连接未声明 dsh_list_plugins"
	case in.Run.HasActiveRun:
		tooltip = "当前任务运行中，暂不能开关插件"
	case restartRequired:
		tooltip = "插件已更新，需重启 Profile 后生效"
	}
	badge := ""
	if restartRequired {
		badge = "需重启"
	} else if n := len(toggles); n > 0 {
		badge = fmt.Sprintf("%d", n)
	}
	value := ""
	if restartRequired {
		value = "restart_required"
	}
	return toolprotocol.Item{
		ItemID:      "dsh_plugins",
		GroupID:     "plugin_control",
		Kind:        toolprotocol.ItemKindToggleList,
		ActionID:    "dsh_plugins",
		Label:       "插件",
		Icon:        "puzzle",
		Variant:     "secondary",
		Disabled:    disabled,
		Tooltip:     tooltip,
		BadgeText:   badge,
		Value:       value,
		LocalAction: "client:toggle_list",
		Toggles:     toggles,
	}, true
}

func buildSkillsItem(in core.BuildInput) (toolprotocol.Item, bool) {
	hasList := in.Runtime.HasLocalAction("dsh_list_skills")
	if !hasList {
		if len(in.Runtime.Skills) == 0 {
			return toolprotocol.Item{}, false
		}
		return shared.BuildSkillsItem(in.Runtime.Skills), true
	}
	skills, toggles := sessionSkills(in.Binding.Meta)

	item := shared.BuildSkillsItem(skills)
	item.ActionID = "dsh_skills"
	item.ShowToggles = true
	item.Toggles = toggles
	item.Disabled = !in.Runtime.Online || in.Run.HasActiveRun
	item.BadgeText = fmt.Sprintf("%d/%d", enabledToggleCount(toggles), len(toggles))
	switch {
	case !in.Runtime.Online:
		item.Tooltip = "DeepSeek 当前离线"
	case in.Run.HasActiveRun:
		item.Tooltip = "当前任务运行中，暂不能开关技能"
	default:
		item.Tooltip = "查看并按会话开关技能"
	}
	return item, true
}

func handleSkills(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if in.BuildInput.Run.HasActiveRun {
		return rejected("worker_busy", "当前任务运行中，无法开关技能"), nil
	}
	event := strings.ToLower(strings.TrimSpace(in.Request.Event))
	name := strings.TrimSpace(in.Request.OptionID)
	switch event {
	case "enable", "disable":
		if name == "" {
			return rejected("invalid_option", "技能名无效"), nil
		}
		action := "dsh_enable_skill"
		message := "已提交启用技能"
		if event == "disable" {
			action = "dsh_disable_skill"
			message = "已提交禁用技能"
		}
		// Rebuilding a Profile Bridge session may consume its full 60s create
		// window. Keep the server-side local action alive long enough to receive
		// the authoritative binding-meta projection from the Connector.
		return dispatch(in, action, actionParams(in, map[string]any{"name": name}), 75_000, message)
	case "refresh", "list", "":
		return dispatch(in, "dsh_list_skills", actionParams(in, nil), 15_000, "已刷新技能列表")
	default:
		return rejected("invalid_option", "技能操作无效"), nil
	}
}

func sessionSkills(meta map[string]any) ([]toolruntime.SkillEntry, []toolprotocol.ToggleItem) {
	raw, ok := meta["dsh_skills"].([]any)
	if !ok {
		return nil, nil
	}
	skills := make([]toolruntime.SkillEntry, 0, len(raw))
	toggles := make([]toolprotocol.ToggleItem, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := metaString(entry, "name")
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		skills = append(skills, toolruntime.SkillEntry{
			Name:        name,
			Description: metaString(entry, "description"),
			Source:      metaString(entry, "source"),
			Path:        metaString(entry, "path"),
			Managed:     true,
		})
		toggles = append(toggles, toolprotocol.ToggleItem{
			ID:      name,
			Name:    name,
			Enabled: metaBool(entry, "enabled"),
		})
	}
	return skills, toggles
}

func enabledToggleCount(toggles []toolprotocol.ToggleItem) int {
	count := 0
	for _, toggle := range toggles {
		if toggle.Enabled {
			count++
		}
	}
	return count
}

func handlePlugins(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if in.BuildInput.Run.HasActiveRun {
		return rejected("worker_busy", "当前任务运行中，无法开关插件"), nil
	}
	event := strings.ToLower(strings.TrimSpace(in.Request.Event))
	name := strings.TrimSpace(in.Request.OptionID)
	switch event {
	case "enable":
		if name == "" {
			return rejected("invalid_option", "插件名无效"), nil
		}
		return dispatch(in, "dsh_enable_plugin", actionParams(in, map[string]any{"name": name}), 20_000, "已提交启用插件")
	case "disable":
		if name == "" {
			return rejected("invalid_option", "插件名无效"), nil
		}
		return dispatch(in, "dsh_disable_plugin", actionParams(in, map[string]any{"name": name}), 20_000, "已提交禁用插件")
	case "refresh", "list", "":
		return dispatch(in, "dsh_refresh_plugins", actionParams(in, nil), 15_000, "已刷新插件列表")
	default:
		return rejected("invalid_option", "插件操作无效"), nil
	}
}

func pluginToggles(meta map[string]any) []toolprotocol.ToggleItem {
	raw, ok := meta["dsh_plugins"].([]any)
	if !ok {
		return nil
	}
	toggles := make([]toolprotocol.ToggleItem, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := metaString(entry, "name")
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		toggles = append(toggles, toolprotocol.ToggleItem{
			ID:         name,
			Name:       name,
			Version:    metaString(entry, "version"),
			Enabled:    metaBool(entry, "enabled"),
			Locked:     metaBool(entry, "locked"),
			LockReason: metaString(entry, "lock_reason"),
		})
	}
	return toggles
}

func rejectSettingsChange(in core.ActionInput) (toolprotocol.ActionResult, bool) {
	if in.BuildInput.Run.HasActiveRun {
		return rejected("worker_busy", "当前任务运行中，无法切换设置"), true
	}
	if settingsState(in.BuildInput.Binding.Meta) == "pending" {
		return rejected("settings_pending", "已有设置正在应用，请稍后重试"), true
	}
	return toolprotocol.ActionResult{}, false
}

func dispatch(in core.ActionInput, actionType string, params map[string]any, timeout int, message string) (toolprotocol.ActionResult, error) {
	if !in.BuildInput.Runtime.Online {
		return rejected("agent_offline", "当前 agent 不在线"), nil
	}
	if !in.BuildInput.Runtime.HasLocalAction(actionType) {
		return rejected("local_action_unavailable", "当前 agent 未声明 "+actionType), nil
	}
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID: in.BuildInput.OwnerID, AgentID: in.BuildInput.Agent.AgentID,
		SessionID: in.BuildInput.Session.SessionID, ActionType: actionType,
		Params: params, TimeoutMs: timeout,
	}); err != nil {
		return rejected("dispatch_failed", err.Error()), nil
	}
	return accepted(toolprotocol.ActionOutcomeAcceptedNoStateChange, message), nil
}

func actionParams(in core.ActionInput, extra map[string]any) map[string]any {
	params := map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
		"actor_id":   fmt.Sprintf("%d", in.BuildInput.OwnerID),
	}
	for key, value := range extra {
		params[key] = value
	}
	return params
}

func accepted(outcome toolprotocol.ActionOutcome, message string) toolprotocol.ActionResult {
	return toolprotocol.ActionResult{Outcome: outcome, Code: "accepted", Message: message}
}

func rejected(code, message string) toolprotocol.ActionResult {
	return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: code, Message: message}
}

func hasSessionBinding(binding core.BindingInfo) bool {
	return strings.TrimSpace(binding.BindingID) != "" || strings.TrimSpace(binding.Cwd) != ""
}

type option struct{ ID, Label, Provider string }

func defaultDeepSeekPresets() []option {
	return []option{
		{ID: "standard", Label: "标准模式"},
		{ID: "code", Label: "PTC 模式"},
		{ID: "minimal", Label: "极简模式"},
		{ID: "cordis", Label: "创造模式"},
	}
}

func presetOptions(meta map[string]any) []option {
	options := catalogOptions(meta, "available_presets")
	if len(options) > 0 {
		return options
	}
	if metaString(meta, "agent_preset_id") == "" {
		return nil
	}
	return defaultDeepSeekPresets()
}

func catalogOptions(meta map[string]any, key string) []option {
	raw, ok := meta[key].([]any)
	if !ok {
		return nil
	}
	options := make([]option, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := metaString(entry, "id")
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		label := metaString(entry, "displayName")
		if label == "" {
			label = metaString(entry, "display_name")
		}
		if label == "" {
			label = metaString(entry, "name")
		}
		if label == "" {
			label = id
		}
		provider := metaString(entry, "provider_id")
		if provider == "" {
			provider = metaString(entry, "provider")
		}
		options = append(options, option{ID: id, Label: label, Provider: provider})
	}
	return options
}

func modelOptions(meta map[string]any) []option {
	return filterModelOptions(catalogOptions(meta, "available_models"), metaString(meta, "provider_id"))
}

func filterModelOptions(options []option, providerID string) []option {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" || !modelOptionsHaveProvider(options) {
		return options
	}
	filtered := make([]option, 0, len(options))
	for _, item := range options {
		if item.Provider == "" || item.Provider == providerID {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func modelOptionsHaveProvider(options []option) bool {
	for _, item := range options {
		if item.Provider != "" {
			return true
		}
	}
	return false
}

func protocolOptions(options []option) []toolprotocol.Option {
	result := make([]toolprotocol.Option, 0, len(options))
	for _, item := range options {
		result = append(result, toolprotocol.Option{OptionID: item.ID, Label: item.Label})
	}
	return result
}

func optionLabel(id string, options []option) string {
	label, ok := findOption(id, options)
	if ok {
		return label
	}
	return strings.TrimSpace(id)
}

func findOption(id string, options []option) (string, bool) {
	for _, item := range options {
		if item.ID == id {
			return item.Label, true
		}
	}
	return "", false
}

func stopOutputTooltip(runState string) string {
	if strings.TrimSpace(runState) == "stopping" {
		return "正在停止当前输出"
	}
	return "停止当前输出"
}

func modeLabel(id string) string {
	switch strings.TrimSpace(id) {
	case "approval":
		return "默认"
	case "full_auto":
		return "自动"
	default:
		return strings.TrimSpace(id)
	}
}

func settingsBadge(name, state string) (string, string) {
	badge := strings.TrimSpace(name)
	variant := "secondary"
	// failed 态不再追加文字后缀，由前端按 warning variant 渲染叹号图标。
	if state == "failed" {
		variant = "warning"
	}
	return badge, variant
}

// settingsPendingTimeout 是 pending 态的兜底有效期。pending 的清除完全依赖
// connector 主动回报 applied/failed；Runtime 重建过程中该上报一旦丢失，pending
// 会永久残留在 binding meta 里，三个设置选择器永远 loading 且拒绝一切新设置。
// 持久化侧写入 pending 时会打上 settings_pending_at 时间戳（见 agentapi 的
// normalizeSettingsStateMeta）；超时或没有时间戳的存量数据一律按非 pending 处理，
// 让选择器恢复可用、用户可重试。
const settingsPendingTimeout = 3 * time.Minute

func settingsState(meta map[string]any) string {
	state := strings.ToLower(metaString(meta, "settings_state"))
	if state != "pending" {
		return state
	}
	pendingAt, ok := metaNumber(meta, "settings_pending_at")
	if !ok || pendingAt <= 0 {
		return ""
	}
	if time.Since(time.UnixMilli(int64(pendingAt))) > settingsPendingTimeout {
		return ""
	}
	return "pending"
}

func metaString(meta map[string]any, key string) string {
	if len(meta) == 0 {
		return ""
	}
	value, _ := meta[key].(string)
	return strings.TrimSpace(value)
}

func metaBool(meta map[string]any, key string) bool {
	if len(meta) == 0 {
		return false
	}
	value, _ := meta[key].(bool)
	return value
}

func metaNumber(meta map[string]any, key string) (float64, bool) {
	if len(meta) == 0 {
		return 0, false
	}
	switch value := meta[key].(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	default:
		return 0, false
	}
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func compactTokens(value float64) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%.3gM", value/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.3gK", value/1_000)
	default:
		return fmt.Sprintf("%.0f", value)
	}
}

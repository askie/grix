package shared

import (
	"strings"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
)

// MetaString 读取 binding meta 里的字符串键并去空白；缺省或非字符串返回空串。
func MetaString(meta map[string]any, key string) string {
	if len(meta) == 0 {
		return ""
	}
	value, _ := meta[key].(string)
	return strings.TrimSpace(value)
}

// ParseMetaOptions 把连接器上报的 available_models / available_modes
// （`[{id, displayName|display_name|label|name}]`）解析成选择器选项：按 id 去重、
// 空 id 跳过、缺显示名时回落 id。这是各 agent 上报清单的统一契约。
func ParseMetaOptions(meta map[string]any, key string) []toolprotocol.Option {
	if len(meta) == 0 {
		return nil
	}
	list, ok := meta[key].([]any)
	if !ok {
		return nil
	}
	out := make([]toolprotocol.Option, 0, len(list))
	seen := map[string]struct{}{}
	for _, raw := range list {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := MetaString(entry, "id")
		if id == "" {
			continue
		}
		// 去重忽略大小写，保留首次出现的写法，避免仅大小写不同的重复项。
		if _, dup := seen[strings.ToLower(id)]; dup {
			continue
		}
		seen[strings.ToLower(id)] = struct{}{}
		label := ""
		for _, labelKey := range []string{"display_name", "displayName", "label", "name"} {
			if label = MetaString(entry, labelKey); label != "" {
				break
			}
		}
		if label == "" {
			label = id
		}
		out = append(out, toolprotocol.Option{OptionID: id, Label: label})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// OptionLabel 返回 optionID 对应的显示名（先精确后忽略大小写）；清单里没有时回落
// optionID 本身，optionID 为空时返回空串，由调用方决定空值展示。
func OptionLabel(optionID string, options []toolprotocol.Option) string {
	id := strings.TrimSpace(optionID)
	if id == "" {
		return ""
	}
	for _, option := range options {
		if option.OptionID == id {
			return option.Label
		}
	}
	for _, option := range options {
		if strings.EqualFold(option.OptionID, id) {
			return option.Label
		}
	}
	return id
}

// SelectSpec 描述一个模型/模式选择器。禁用判定与提示语由 BuildSelect 统一推导，
// 各 agent 只填自己的当前值、清单和少量文案差异。
type SelectSpec struct {
	ItemID      string
	GroupID     string
	ActionID    string
	Icon        string
	Placeholder string
	// Agent 是提示语里的 agent 显示名（"Kimi"），Noun 是被切换对象（"模型"/"审批模式"）。
	Agent string
	Noun  string
	// LocalAction 是连接器须声明的 local action（set_model / set_mode）。
	LocalAction string
	Label       string
	Value       string
	Badge       string
	Options     []toolprotocol.Option
	// WaitForOptions：清单由运行时上报，为空时置灰并提示 EmptyOptionsTooltip。
	WaitForOptions      bool
	EmptyOptionsTooltip string
	// StaticTooltip：提示语不随离线/未声明变化，始终是"切换 {Agent} {Noun}"。
	StaticTooltip bool
	// ReadyTooltip / UndeclaredTooltip 覆盖缺省的"切换 {Agent} {Noun}" / "当前插件未声明 {LocalAction}"。
	ReadyTooltip      string
	UndeclaredTooltip string
}

// ModelSelect 是模型选择器的缺省样式；调用方补 Value / Badge / Options。
func ModelSelect(agent string) SelectSpec {
	return SelectSpec{
		ItemID:         "select_model",
		GroupID:        "model_control",
		ActionID:       "select_model",
		Icon:           "cpu",
		Placeholder:    "选择模型",
		Agent:          agent,
		Noun:           "模型",
		LocalAction:    "set_model",
		WaitForOptions: true,
	}
}

// ModeSelect 是模式选择器的缺省样式；模式清单通常是静态白名单，不等待同步。
func ModeSelect(agent string) SelectSpec {
	return SelectSpec{
		ItemID:      "select_mode",
		GroupID:     "mode_control",
		ActionID:    "select_mode",
		Icon:        "shield",
		Placeholder: "选择模式",
		Agent:       agent,
		Noun:        "模式",
		LocalAction: "set_mode",
	}
}

// BuildSelect 按运行时状态推导 Disabled / Tooltip 并组装 Item。
func BuildSelect(in core.BuildInput, spec SelectSpec) toolprotocol.Item {
	noOptions := spec.WaitForOptions && len(spec.Options) == 0
	disabled := !in.Runtime.Online || !in.Runtime.HasLocalAction(spec.LocalAction) || noOptions
	tooltip := spec.ReadyTooltip
	if tooltip == "" {
		tooltip = "切换 " + spec.Agent + " " + spec.Noun
	}
	if !spec.StaticTooltip {
		switch {
		case !in.Runtime.Online:
			tooltip = spec.Agent + " 当前离线"
		case !in.Runtime.HasLocalAction(spec.LocalAction):
			tooltip = spec.UndeclaredTooltip
			if tooltip == "" {
				tooltip = "当前插件未声明 " + spec.LocalAction
			}
		case noOptions:
			tooltip = spec.EmptyOptionsTooltip
			if tooltip == "" {
				tooltip = "等待 " + spec.Agent + " 模型列表同步"
			}
		}
	}
	return toolprotocol.Item{
		ItemID:      spec.ItemID,
		GroupID:     spec.GroupID,
		Kind:        toolprotocol.ItemKindSelect,
		ActionID:    spec.ActionID,
		Label:       spec.Label,
		Icon:        spec.Icon,
		Variant:     "secondary",
		Disabled:    disabled,
		Tooltip:     tooltip,
		Value:       spec.Value,
		BadgeText:   spec.Badge,
		Placeholder: spec.Placeholder,
		Options:     spec.Options,
	}
}

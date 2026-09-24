package acp

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
)

// 通用 ACP agent 自己声明的工具栏项（binding meta 的 custom_toolbar 键）。
//
// 上报方是外部开发者写的 agent，内容一律当不可信输入：数量、长度、id 形态、
// 选项个数全部设硬上限，任何一项不合规就**整包丢弃**——渲染半套工具栏比不渲染
// 更难排查，而且会让用户以为剩下的项是 agent 的本意。
//
// 只有 client_type "acp" 的工具栏包读这个键。其余 client_type 的包各自构建，
// 根本不看它，所以外部 agent 无法借这条路影响任何厂商 CLI 的工具栏。
const (
	maxCustomItems       = 6
	maxCustomIDLen       = 32
	maxCustomLabelRunes  = 20
	maxCustomOptions     = 20
	maxCustomOptionRunes = 40
	maxCustomInfoRunes   = 500
	// customItemIDPrefix 隔离命名空间：外部 agent 报一个叫 stop_output 的 id
	// 也只会变成 custom_stop_output，撞不掉内建项。
	customItemIDPrefix = "custom_"
	customGroupID      = "custom"

	customKindSelect = "select"
	customKindInfo   = "info"

	// ActionIDCustomSelect 是自定义下拉框的动作；info 项是纯客户端行为，不回后端。
	ActionIDCustomSelect = "custom_select"
	// localActionCustomInfo 走既有的 client: 前缀约定（client:command_list /
	// client:toggle_list 同款），前端见到就本地弹说明，不发任何请求。
	localActionCustomInfo = "client:info"
)

type customOption struct {
	ID    string
	Label string
}

type customItem struct {
	ID        string
	Kind      string
	Label     string
	Value     string
	Options   []customOption
	InfoTitle string
	InfoText  string
}

// parseCustomItems 读 binding meta 的 custom_toolbar。返回 nil 表示没有可渲染的
// 自定义项（未上报、形状不对、或校验不通过）。
func parseCustomItems(meta map[string]any) []customItem {
	raw, ok := meta["custom_toolbar"]
	if !ok || raw == nil {
		return nil
	}
	list, ok := asAnySlice(raw)
	if !ok || len(list) == 0 || len(list) > maxCustomItems {
		return nil
	}
	items := make([]customItem, 0, len(list))
	seen := make(map[string]struct{}, len(list))
	for _, entry := range list {
		obj, ok := asStringMap(entry)
		if !ok {
			return nil
		}
		item, ok := parseCustomItem(obj)
		if !ok {
			return nil
		}
		if _, dup := seen[item.ID]; dup {
			return nil
		}
		seen[item.ID] = struct{}{}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func parseCustomItem(obj map[string]any) (customItem, bool) {
	id := strings.TrimSpace(metaText(obj["id"]))
	if !validCustomID(id) {
		return customItem{}, false
	}
	label := strings.TrimSpace(metaText(obj["label"]))
	if label == "" || utf8.RuneCountInString(label) > maxCustomLabelRunes {
		return customItem{}, false
	}
	kind := strings.ToLower(strings.TrimSpace(metaText(obj["kind"])))

	switch kind {
	case customKindSelect:
		rawOptions, ok := asAnySlice(obj["options"])
		if !ok || len(rawOptions) == 0 || len(rawOptions) > maxCustomOptions {
			return customItem{}, false
		}
		options := make([]customOption, 0, len(rawOptions))
		seen := make(map[string]struct{}, len(rawOptions))
		for _, rawOption := range rawOptions {
			optionObj, ok := asStringMap(rawOption)
			if !ok {
				return customItem{}, false
			}
			optionID := strings.TrimSpace(metaText(optionObj["id"]))
			optionLabel := strings.TrimSpace(metaText(optionObj["label"]))
			if !validCustomID(optionID) || optionLabel == "" ||
				utf8.RuneCountInString(optionLabel) > maxCustomOptionRunes {
				return customItem{}, false
			}
			if _, dup := seen[optionID]; dup {
				return customItem{}, false
			}
			seen[optionID] = struct{}{}
			options = append(options, customOption{ID: optionID, Label: optionLabel})
		}
		value := strings.TrimSpace(metaText(obj["value"]))
		// 当前值必须是自己列出的选项之一；否则清空，让下拉显示占位而不是一个
		// 用户点不出来的幽灵值。
		if value != "" {
			if _, ok := seen[value]; !ok {
				value = ""
			}
		}
		return customItem{ID: id, Kind: customKindSelect, Label: label, Value: value, Options: options}, true

	case customKindInfo:
		infoText := strings.TrimSpace(metaText(obj["info_text"]))
		if infoText == "" || utf8.RuneCountInString(infoText) > maxCustomInfoRunes {
			return customItem{}, false
		}
		infoTitle := strings.TrimSpace(metaText(obj["info_title"]))
		if utf8.RuneCountInString(infoTitle) > maxCustomLabelRunes {
			return customItem{}, false
		}
		if infoTitle == "" {
			infoTitle = label
		}
		return customItem{ID: id, Kind: customKindInfo, Label: label, InfoTitle: infoTitle, InfoText: infoText}, true

	default:
		return customItem{}, false
	}
}

// buildCustomItems 把校验过的自定义项转成工具栏项。
// runBusy 为真时下拉禁用：ACP 一个会话同一时刻只能有一轮，与内建的
// 模型/模式选择器保持同一条规则。
func buildCustomItems(items []customItem, online, runBusy bool) []toolprotocol.Item {
	out := make([]toolprotocol.Item, 0, len(items))
	for _, item := range items {
		switch item.Kind {
		case customKindSelect:
			options := make([]toolprotocol.Option, 0, len(item.Options))
			for _, option := range item.Options {
				options = append(options, toolprotocol.Option{OptionID: option.ID, Label: option.Label})
			}
			out = append(out, toolprotocol.Item{
				ItemID:      customItemIDPrefix + item.ID,
				GroupID:     customGroupID,
				Kind:        toolprotocol.ItemKindSelect,
				ActionID:    ActionIDCustomSelect,
				Label:       item.Label,
				Icon:        "tune",
				Variant:     "secondary",
				Disabled:    !online || runBusy,
				Tooltip:     customSelectTooltip(online, runBusy),
				Value:       item.Value,
				BadgeText:   optionLabelOf(item.Value, item.Options),
				Placeholder: item.Label,
				Options:     options,
			})
		case customKindInfo:
			out = append(out, toolprotocol.Item{
				ItemID:       customItemIDPrefix + item.ID,
				GroupID:      customGroupID,
				Kind:         toolprotocol.ItemKindButton,
				ActionID:     localActionCustomInfo,
				Label:        item.Label,
				Icon:         "info",
				Variant:      "secondary",
				LocalAction:  localActionCustomInfo,
				ConfirmTitle: item.InfoTitle,
				ConfirmText:  item.InfoText,
			})
		}
	}
	return out
}

// findCustomSelectOption 按工具栏项 ID 与选项 ID 在上报清单里复核一次。
// 动作必须以当前快照为准：前端可能拿着旧快照点过来，agent 也可能刚把该项撤掉。
func findCustomSelectOption(items []customItem, itemID, optionID string) (customItem, bool) {
	trimmedItem := strings.TrimPrefix(strings.TrimSpace(itemID), customItemIDPrefix)
	trimmedOption := strings.TrimSpace(optionID)
	for _, item := range items {
		if item.Kind != customKindSelect || item.ID != trimmedItem {
			continue
		}
		for _, option := range item.Options {
			if option.ID == trimmedOption {
				return item, true
			}
		}
		return customItem{}, false
	}
	return customItem{}, false
}

// buildCustomSelectCommand 是回传给 agent 的命令文本。
//
// 走 grix:// 前缀而不是 /grix：pushDelegateEvent 会给非白名单的 /grix 开头文本
// 加前导空格转义，grix:// 形态不受影响，agent 拿到的字节与这里构造的一致。
//
// 两个 id 都已经过 validCustomID（只剩 a-z0-9_），在 query 里本就安全，
// 因此不做转义——真要转义反而会让 agent 侧多一步解码。
func buildCustomSelectCommand(itemID, optionID string) string {
	return "grix://toolbar/select?item=" + itemID + "&option=" + optionID
}

// --- 小工具 ---

func validCustomID(value string) bool {
	if value == "" || len(value) > maxCustomIDLen {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '_' {
			continue
		}
		return false
	}
	return true
}

func metaText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func asAnySlice(value any) ([]any, bool) {
	list, ok := value.([]any)
	return list, ok
}

func asStringMap(value any) (map[string]any, bool) {
	obj, ok := value.(map[string]any)
	return obj, ok
}

func optionLabelOf(optionID string, options []customOption) string {
	if optionID == "" {
		return ""
	}
	for _, option := range options {
		if option.ID == optionID {
			return option.Label
		}
	}
	return ""
}

func customSelectTooltip(online, runBusy bool) string {
	switch {
	case !online:
		return "ACP agent 当前离线"
	case runBusy:
		return "当前有任务运行中，完成后可切换"
	default:
		return ""
	}
}

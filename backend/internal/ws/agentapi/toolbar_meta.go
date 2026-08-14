package agentapi

import "strings"

// defaultServiceTierID 是速度档的归位值：插件用 null 表示「标准档」，库里统一存成它。
const defaultServiceTierID = "default"

// toolbarMetaNullableKeys 列出「键存在即权威」的 meta 键——它们的空值是有语义的，
// 不能当成「插件没提到」而沿用旧值，否则工具栏会显示已经不成立的旧状态：
//
//	service_tier            null 表示档位已归位标准档（用户在 agent 侧换了模型、
//	                        或连接器守卫把不支持的档位归一），必须覆盖掉旧的具体档位；
//	available_service_tiers 空数组表示当前模型不支持速度档，必须清掉旧的可选列表，
//	                        否则选择器会继续显示上一个模型的档位；
//	available_efforts       同上——推理力度选择器也是「选项非空才渲染」，空列表必须
//	                        清掉，否则切到不支持 effort 的模型后仍显示上一个模型的档位，
//	                        用户点下去就是发一个当前模型不认的值。
//	effort / reasoning_effort
//	                        null 表示清除显式推理力度覆盖，回到模型默认；两种键都
//	                        允许显式清空，兼容新旧 connector 的字段命名。
//
// 其余键沿用「有值才覆盖」：插件只上报变化的字段，缺省即代表沿用。
//
// 注意：往这里加键的同时，要确认该键在两条路径的 meta 构造处都不会被 != nil 之类的
// 守卫提前滤掉——被滤掉的话这份名单对那条路径就是空话（见 local_action_handler.go）。
var toolbarMetaNullableKeys = map[string]struct{}{
	"service_tier":              {},
	"available_service_tiers":   {},
	"available_efforts":         {},
	"effort":                    {},
	"reasoning_effort":          {},
	"available_models":          {},
	"available_providers":       {},
	"available_presets":         {},
	"agent_preset_id":           {},
	"agent_preset_locked":       {},
	"applied_model_id":          {},
	"applied_mode_id":           {},
	"applied_provider_id":       {},
	"applied_settings_revision": {},
	"context_window":            {},
	"provider_quota":            {},
	"settings_error_code":       {},
}

func isToolbarMetaNullableKey(key string) bool {
	_, ok := toolbarMetaNullableKeys[key]
	return ok
}

// normalizeToolbarMetaValue 把 nullable 键的空值归位成库里的规范表示。
// service_tier 的空（null / ""）代表标准档，存成 "default"；
// available_service_tiers 的空数组保持原样，表示「该模型无可选档位」。
func normalizeToolbarMetaValue(key string, value any) any {
	if key == "service_tier" && !hasToolbarMetaValue(value) {
		return defaultServiceTierID
	}
	return value
}

// mergeToolbarMeta 把插件上报的 meta 合并进已存的 binding meta。
// 两条持久化路径（local_action 结果 persistToolbarBinding、update_binding_card
// 推送 persistBindingFromCard）都必须经由它落库——这是本函数存在的全部意义：
// 往 toolbarMetaNullableKeys 加一个键，两条路径就该同时生效。谁再在某条路径上
// 内联一份自己的合并循环，这个保证就破了，本次修的 bug 会换个键再犯一遍。
func mergeToolbarMeta(dst, src map[string]any) map[string]any {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string]any{}
	}
	for key, value := range src {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if isToolbarMetaNullableKey(key) {
			dst[key] = normalizeToolbarMetaValue(key, value)
			continue
		}
		if hasToolbarMetaValue(value) {
			dst[key] = value
		}
	}
	return dst
}

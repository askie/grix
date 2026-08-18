package agentapi

import (
	"strings"
	"time"
)

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
//	available_presets         同上——preset 目录以 connector 上报为准，空数组必须清掉旧列表；
//	available_profiles        同上——DSH Profile 目录以 connector 上报为准，空数组必须清掉旧列表。
//
// 其余键沿用「有值才覆盖」：插件只上报变化的字段，缺省即代表沿用。
//
// 注意：往这里加键的同时，要确认该键在两条路径的 meta 构造处都不会被 != nil 之类的
// 守卫提前滤掉——被滤掉的话这份名单对那条路径就是空话（见 local_action_handler.go）。
var toolbarMetaNullableKeys = map[string]struct{}{
	"service_tier":                {},
	"available_service_tiers":     {},
	"available_efforts":           {},
	"effort":                      {},
	"reasoning_effort":            {},
	"available_models":            {},
	"available_providers":         {},
	"available_presets":           {},
	"available_profiles":          {},
	"agent_preset_locked":         {},
	"applied_model_id":            {},
	"applied_mode_id":             {},
	"applied_provider_id":         {},
	"applied_settings_revision":   {},
	"context_window":              {},
	"provider_quota":              {},
	"settings_error_code":         {},
	"dsh_plugins":                 {},
	"dsh_plugin_restart_required": {},
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
	oldProvider := toolbarMetaString(dst, "provider_id", "providerId")
	newProvider := toolbarMetaString(src, "provider_id", "providerId")
	_, hasModels := src["available_models"]
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
	// 供应商切换后旧模型目录不再成立。payload 没带 available_models 时主动清空，
	// 避免工具栏继续列出上一个供应商的模型。
	if oldProvider != "" && newProvider != "" && oldProvider != newProvider && !hasModels {
		dst["available_models"] = []any{}
		if _, ok := src["model_id"]; !ok {
			if _, ok := src["modelId"]; !ok {
				delete(dst, "model_id")
				delete(dst, "modelId")
			}
		}
	}
	return dst
}

// normalizeSettingsStateMeta 在两条持久化路径落库前统一处理 settings_state：
//
//  1. update_binding_card 路径的 payload.Meta 是 connector 原样透传的，可能带
//     camelCase 的 settingsState——不归一到 snake_case 的话 applied/failed 落不到
//     读取键上，旧 pending 会永久残留（local_action 路径已由
//     copyToolbarProjectionValue 做过同样的归一）；
//  2. 进入 pending 时打服务端时间戳 settings_pending_at。pending 的清除完全依赖
//     connector 事后回报 applied/failed，一旦 Runtime 重建中丢失该上报，工具栏
//     三个设置选择器会永远 loading 且拒绝新设置；读取侧（deepseek package 的
//     settingsState）凭这个时间戳做超时自愈。
//  3. existing 是库里已存的 binding meta：若本次只是把「同一个 pending」重报一遍
//     （existing 已是 pending 且 settings_revision 未变），保留原
//     settings_pending_at 不再刷新。否则 connector 的周期性推送（如 DSH 适配器
//     30s 配额定时全量推、stop 退出前的状态推）会把同一个残留 pending 的时间戳
//     不断刷成 now，第 2 条的超时自愈永远不届满——这正是「连接器重启后工具栏
//     所有设置按钮永久 loading」的复发根因。
func normalizeSettingsStateMeta(meta, existing map[string]any, now time.Time) {
	if len(meta) == 0 {
		return
	}
	if value, ok := meta["settingsState"]; ok {
		if _, has := meta["settings_state"]; !has {
			meta["settings_state"] = value
		}
		delete(meta, "settingsState")
	}
	if state, _ := meta["settings_state"].(string); strings.EqualFold(strings.TrimSpace(state), "pending") {
		if isSamePendingRereport(meta, existing) {
			if stamped, ok := existing["settings_pending_at"]; ok {
				meta["settings_pending_at"] = stamped
				return
			}
		}
		meta["settings_pending_at"] = float64(now.UnixMilli())
	}
}

// isSamePendingRereport 判断本次 pending 是否只是对已存 pending 的重报：
// existing 已是 pending 且 settings_revision 未变（含双方都缺省）。revision 变了
// 说明是新的设置请求，必须重新计时；双方都缺 revision 时按同一笔处理——重报方
// 本来就不携带 revision 语义，刷新时间戳只会让残留 pending 永远不死。
func isSamePendingRereport(meta, existing map[string]any) bool {
	if len(existing) == 0 {
		return false
	}
	if state, _ := existing["settings_state"].(string); !strings.EqualFold(strings.TrimSpace(state), "pending") {
		return false
	}
	newRev, newOK := toolbarMetaRevision(meta, "settings_revision", "settingsRevision")
	oldRev, oldOK := toolbarMetaRevision(existing, "settings_revision", "settingsRevision")
	if newOK != oldOK {
		return false
	}
	return !newOK || newRev == oldRev
}

func toolbarMetaRevision(meta map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		switch value := meta[key].(type) {
		case float64:
			return value, true
		case int64:
			return float64(value), true
		case int:
			return float64(value), true
		}
	}
	return 0, false
}

func toolbarMetaString(meta map[string]any, keys ...string) string {
	if len(meta) == 0 {
		return ""
	}
	for _, key := range keys {
		value, _ := meta[key].(string)
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

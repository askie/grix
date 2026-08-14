package agentapi

import (
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
)

// 自定义技能多机器同步（docs/architecture/38）的对话式配置工具。
// 与 REST 入口读写同一份技能库；owner 隔离由连接的 ownerID 保证。
// 平台只存与分发，这里不解析技能内容、不校验语义。

// dispatchSkillSet 处理 skill_set：按名新建/更新技能；content 传空（或显式 null）即按名删除。
// 落库后触发多机同步（connector 拉取，见 §6.2；当前依赖 connector 轮询 /v1/skills）。
func dispatchSkillSet(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	name, _ := paramString(params, "name")
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, 4001, "name required"
	}
	content, hasContent := paramString(params, "content")
	// content 缺省或为空白 = 删除该技能（对话式解除）。
	if !hasContent || strings.TrimSpace(content) == "" {
		if _, present := params["content"]; !present {
			return nil, 4001, "content required (pass empty/null to delete)"
		}
		if ec := service.DeleteUserSkillByName(ownerID, name); ec != nil {
			return nil, ec.BizCode, ec.Msg
		}
		return map[string]interface{}{"deleted": true, "name": name}, 0, ""
	}
	skill, ec := service.UpsertUserSkillByName(ownerID, name, content)
	if ec != nil {
		return nil, ec.BizCode, ec.Msg
	}
	return skill, 0, ""
}

// dispatchSkillGet 处理 skill_get：带 name 返回该技能全文；不带 name 返回技能清单摘要。
func dispatchSkillGet(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	name, hasName := paramString(params, "name")
	if hasName && strings.TrimSpace(name) != "" {
		skill, ec := service.GetUserSkillByName(ownerID, strings.TrimSpace(name))
		if ec != nil {
			return nil, ec.BizCode, ec.Msg
		}
		return skill, 0, ""
	}
	items, ec := service.ListUserSkills(ownerID)
	if ec != nil {
		return nil, ec.BizCode, ec.Msg
	}
	return map[string]interface{}{"items": items}, 0, ""
}

package protocol

import toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"

type Snapshot struct {
	SessionID string `json:"session_id"`
	AgentID   int64  `json:"agent_id"`
	ToolbarID string `json:"toolbar_id"`
	Revision  int64  `json:"revision"`
	Visible   bool   `json:"visible"`
	UpdatedAt int64  `json:"updated_at"`
	Items     []Item `json:"items"`
	// LibrarySkills 是技能库全集 + 各作用域(global/project)启用状态透传
	// （技能库启用，方案 v2）：与 Items 里"技能"按钮的已生效命令列表并列，
	// 不受 Visible 影响，供前端渲染技能库管理弹窗（启用/停用）。
	LibrarySkills []toolruntime.LibrarySkillEntry `json:"library_skills"`

	// AuditEnabled 是对话审计开关的服务端状态（user+agent 维度）。nil 表示
	// 后端不接管该状态（功能未开放/无目标 agent），字段不下发，前端回退本地行为。
	AuditEnabled *bool `json:"audit_enabled,omitempty"`

	// OmitQueueButton 为构建期控制位:置位后规范化阶段不前置队列(show_queue)按钮,不参与序列化。
	OmitQueueButton bool `json:"-"`
	// OmitListSessionsButton 为构建期控制位:置位后规范化阶段不前置会话列表(list_sessions)按钮,不参与序列化。
	OmitListSessionsButton bool `json:"-"`
}

type Item struct {
	ItemID       string   `json:"item_id"`
	GroupID      string   `json:"group_id"`
	Kind         string   `json:"kind"`
	ActionID     string   `json:"action_id"`
	Label        string   `json:"label"`
	Icon         string   `json:"icon"`
	Variant      string   `json:"variant"`
	Disabled     bool     `json:"disabled"`
	Loading      bool     `json:"loading"`
	Selected     bool     `json:"selected"`
	Tooltip      string   `json:"tooltip"`
	BadgeText    string   `json:"badge_text"`
	ConfirmTitle string   `json:"confirm_title"`
	ConfirmText  string   `json:"confirm_text"`
	Value        string   `json:"value"`
	Placeholder  string   `json:"placeholder"`
	Options      []Option `json:"options"`

	// Progress-specific fields (kind == "progress")
	Percent        float64 `json:"percent"`
	CenterText     string  `json:"center_text"`
	ProgressDesc   string  `json:"progress_desc"`
	ProgressDetail string  `json:"progress_detail"`

	// Client-side local action fields
	LocalAction string        `json:"local_action"`
	Commands    []CommandItem `json:"commands"`
}

type Option struct {
	OptionID string `json:"option_id"`
	Label    string `json:"label"`
	Disabled bool   `json:"disabled"`
}

type CommandItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Exec        string `json:"exec"`
	Managed     bool   `json:"managed,omitempty"`
	SyncState   string `json:"sync_state,omitempty"`
}

type ActionRequest struct {
	SessionID      string `json:"session_id"`
	TargetAgentID  int64  `json:"target_agent_id"`
	ToolbarID      string `json:"toolbar_id"`
	Revision       int64  `json:"revision"`
	ItemID         string `json:"item_id"`
	ActionID       string `json:"action_id"`
	ClientActionID string `json:"client_action_id"`
	Event          string `json:"event"`
	OptionID       string `json:"option_id"`
}

type ActionOutcome string

const (
	ItemKindButton   = "button"
	ItemKindSelect   = "select"
	ItemKindProgress = "progress"

	ActionOutcomeAcceptedNoStateChange        ActionOutcome = "accepted_no_state_change"
	ActionOutcomeAcceptedWithImmediateRefresh ActionOutcome = "accepted_with_immediate_refresh"
	ActionOutcomeRejected                     ActionOutcome = "rejected"
)

type ActionResult struct {
	Outcome ActionOutcome
	Code    string
	Message string
}

func (s Snapshot) FindItem(itemID string) (Item, bool) {
	for _, item := range s.Items {
		if item.ItemID == itemID {
			return item, true
		}
	}
	return Item{}, false
}

func (i Item) FindOption(optionID string) (Option, bool) {
	for _, option := range i.Options {
		if option.OptionID == optionID {
			return option, true
		}
	}
	return Option{}, false
}

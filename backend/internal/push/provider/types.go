package provider

type PushPayload struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	SessionID string `json:"session_id"`
	// RecipientID 标识这条推送的目标账号。客户端收到/点击时据此比对当前登录账号，
	// 账号不一致则忽略，避免切换账号后误打开他人会话（空白页）。
	RecipientID     int64  `json:"recipient_id,omitempty"`
	Badge           int    `json:"badge,omitempty"`
	BadgeOnly       bool   `json:"badge_only,omitempty"`
	SenderAvatarURL string `json:"sender_avatar_url,omitempty"`
	SenderInitial   string `json:"sender_initial,omitempty"`
	// ForcePush skips the online-device check so the push is sent
	// even when the target device has an active WebSocket connection.
	ForcePush bool `json:"force_push,omitempty"`
	// TimeSensitive 标记为时间敏感通知（如来电），iOS 使用 priority=10 +
	// interruption-level=time-sensitive 以突破免打扰模式。
	TimeSensitive bool `json:"time_sensitive,omitempty"`
	// HighPriority 标记需最高优先级即时投递的消息（审批 / 语音拨号 / 来电等）。
	// 分级投递：高优先级用 apns-priority=10 立即送达；普通消息用 5 降级投递，
	// 由系统按节能策略择机送达，减少打扰。
	HighPriority bool `json:"high_priority,omitempty"`

	// --- Agent 通知离线交互字段（仅 Agent 通知使用）---

	// Category 对应 APNs category / Android channel，用于客户端渲染按钮
	// （如 "APPROVAL_REQUEST" / "AGENT_QUESTION"）。
	Category string `json:"category,omitempty"`
	// EventKey 标识 Agent 通知事件类型（如 "approval_requested"）。
	EventKey string `json:"event_key,omitempty"`
	// ActionToken 为可回调事件的离线操作凭证。
	ActionToken string `json:"action_token,omitempty"`
	// AvailableActions 列出该通知支持的离线操作（如 ["approve","deny","stop"]）。
	AvailableActions []string `json:"available_actions,omitempty"`
	// ImageURL 为图片消息的公网可读 https 图片地址，供 iOS 通知服务扩展下载成富媒体
	// 附件。取不到可下发的地址时留空，通知退化为纯文字。
	ImageURL string `json:"image_url,omitempty"`
	// Expiration 为 APNs apns-expiration 的 Unix 时间戳（秒）。0 表示不设过期。
	// 可回调事件须设为 action token 的 exp，确保 token 失效后系统丢弃该推送，
	// 避免出现"过期按钮"。
	Expiration int64 `json:"expiration,omitempty"`
}

type PushResult struct {
	Success    bool
	StatusCode int
	Reason     string
}

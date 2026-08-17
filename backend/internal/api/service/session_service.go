package service

import "errors"

const (
	dissolveSystemSummary     = "[system] group dissolved"
	dissolveSystemContent     = "group dissolved by owner"
	sessionCustomTitleMax     = 255
	sessionGroupNicknameMax   = 255
	sessionFallbackMaxRunes   = 24
	sessionSearchDefaultLimit = 20
)

var (
	ErrSessionPermissionDenied           = errors.New("permission denied")
	ErrSessionNotFound                   = errors.New("session not found")
	ErrSessionRoleDenied                 = errors.New("only owner/admin can manage members")
	ErrSessionOwnerRequired              = errors.New("only owner can manage member roles")
	ErrSessionDissolveDenied             = errors.New("only owner can dissolve group")
	ErrSessionInvalidType                = errors.New("session type not group")
	ErrSessionInvalidRole                = errors.New("invalid member role")
	ErrSessionMemberNotFound             = errors.New("session member not found")
	ErrSessionCannotRemoveOwner          = errors.New("cannot remove owner")
	ErrSessionCannotOperateSelf          = errors.New("cannot operate self")
	ErrSessionRemoveDenied               = errors.New("admin can only remove normal members")
	ErrSessionMemberSettingDenied        = errors.New("can only update your own member setting")
	ErrSessionTitleTooLong               = errors.New("session title too long")
	ErrSessionGroupNicknameTooLong       = errors.New("group nickname too long")
	ErrSessionAgentReceiveModeInvalid    = errors.New("invalid agent receive mode")
	ErrSessionAgentReceiveBacklogInvalid = errors.New("invalid agent receive backlog count")
	ErrInvalidMemberID                   = errors.New("invalid member_id")
	ErrInvalidMemberType                 = errors.New("invalid member_type")
	ErrMemberTypesMismatch               = errors.New("member_types length mismatch")
	ErrMemberUserNotFound                = errors.New("member user not found")
	ErrMemberNotFriend                   = errors.New("member is not friend")
	ErrMemberAgentNotFound               = errors.New("member agent not found")
	ErrMemberAgentNotOwned               = errors.New("member agent not owned by operator")
	ErrMemberAgentUnavailable            = errors.New("member agent unavailable")
	ErrMemberAgentVoiceNotAllowed        = errors.New("voice agent cannot be added to group")
	ErrSessionTargetGroupInviteRejected  = errors.New("one or more target users do not allow group invites")
	ErrSessionOwnerCannotLeave           = errors.New("group owner cannot leave the group; dissolve it instead")

	sessionMemberAddedOfflinePushRunner = pushSessionMemberAddedOfflinePush
)

type SessionItem struct {
	SessionID      string       `json:"session_id"`
	Title          string       `json:"title"`
	Peer           *SessionPeer `json:"peer"`
	SessionType    int16        `json:"session_type"`
	IsVisitor      bool         `json:"is_visitor"`
	LastMsg        string       `json:"last_msg"`
	// LastMsgTime 为「最后一条可见消息」的时间(unix 秒)，用于会话列表展示的时间，
	// 与用户点进会话看到的最后一条对齐；无可见消息时为 0，前端回退到活跃时间。
	LastMsgTime    int64        `json:"last_msg_time"`
	Unread         int          `json:"unread"`
	UpdatedAt      int64        `json:"updated_at"`
	IsPinned       bool         `json:"is_pinned"`
	PinnedAt       int64        `json:"pinned_at"`
	IsMuted        bool         `json:"is_muted"`
	FriendIsPinned bool         `json:"friend_is_pinned"`
	FriendPinnedAt int64        `json:"friend_pinned_at"`
	FriendIsMuted  bool         `json:"friend_is_muted"`
}

type SessionPeer struct {
	ID       int64  `json:"id,string"`
	Type     int16  `json:"type"`
	Nickname string `json:"nickname"`
	Username string `json:"username"`
}

type SessionListResp struct {
	HasMore bool          `json:"has_more"`
	List    []SessionItem `json:"list"`
	// Cursor 为服务端处理时刻（unix 秒）。客户端冷启动用全量 /sessions/list 建基线后，
	// 以它作为后续 /sessions/sync 的 since 起点，避免用客户端时钟导致增量漏拉 / 重拉。
	Cursor int64 `json:"cursor"`
}

// SessionSyncResp 是 /sessions/sync 增量同步的返回。List 为 since 之后有更新的会话；
// DeletedSessionIDs 为 since 之后被移除的会话（退群 / 被踢 / 群解散），客户端据此清本地；
// Cursor 为服务端处理时刻（unix 秒），客户端下次以它作为 since 续拉。
type SessionSyncResp struct {
	HasMore           bool          `json:"has_more"`
	List              []SessionItem `json:"list"`
	DeletedSessionIDs []string      `json:"deleted_session_ids"`
	Cursor            int64         `json:"cursor"`
}

type SessionSearchItem struct {
	SessionID   string `json:"session_id"`
	Title       string `json:"title"`
	SessionType int16  `json:"session_type"` // 1:私聊 2:群聊
}

type SessionSearchResp struct {
	HasMore bool                `json:"has_more"`
	List    []SessionSearchItem `json:"list"`
}

type sessionSearchKeyword struct {
	lowered string
	compact string
	tokens  []string
}

type rankedSessionSearchItem struct {
	item        SessionSearchItem
	score       int
	originIndex int
}

type SessionDetailMember struct {
	MemberID                 int64  `json:"member_id,string"`
	MemberType               int16  `json:"member_type"`
	Role                     int16  `json:"role"`
	LastReadMsgID            int64  `json:"last_read_msg_id,string"`
	Nickname                 string `json:"nickname,omitempty"`
	GroupNickname            string `json:"group_nickname,omitempty"`
	IsSpeakMuted             bool   `json:"is_speak_muted"`
	CanSpeakWhenAllMuted     bool   `json:"can_speak_when_all_muted"`
	AgentReceiveMode         int16  `json:"agent_receive_mode"`
	AgentReceiveBacklogCount int    `json:"agent_receive_backlog_count"`
	AgentReceiveEditable     bool   `json:"agent_receive_editable"`
}

type SessionDetailResp struct {
	SessionID             string                `json:"session_id"`
	GroupName             string                `json:"group_name,omitempty"`
	SessionType           int16                 `json:"session_type"`
	IsVisitor             bool                  `json:"is_visitor"`
	VisitorInfo           *SessionVisitorInfo   `json:"visitor_info,omitempty"`
	MemberCount           int                   `json:"member_count"`
	AllowMemberInvite     bool                  `json:"allow_member_invite"`
	AllMembersMuted       bool                  `json:"all_members_muted"`
	MemberInviteThreshold int                   `json:"member_invite_threshold"`
	Members               []SessionDetailMember `json:"members"`
}

type SessionVisitorInfo struct {
	SiteID       int64  `json:"site_id,string"`
	SiteName     string `json:"site_name"`
	VisitorID    int64  `json:"visitor_id,string"`
	VisitorKey   string `json:"visitor_key"`
	VisitorName  string `json:"visitor_name"`
	VisitorEmail string `json:"visitor_email"`
	LastPageURL  string `json:"last_page_url"`
	Status       int16  `json:"status"`
	LastActiveAt int64  `json:"last_active_at"`
}

type SessionAddMembersResp struct {
	SessionID   string `json:"session_id"`
	AddedCount  int    `json:"added_count"`
	MemberCount int    `json:"member_count"`
}

type SessionRemoveMembersResp struct {
	SessionID    string `json:"session_id"`
	RemovedCount int    `json:"removed_count"`
	MemberCount  int    `json:"member_count"`
}

type SessionUpdateMemberRoleResp struct {
	SessionID  string `json:"session_id"`
	MemberID   int64  `json:"member_id,string"`
	MemberType int16  `json:"member_type"`
	Role       int16  `json:"role"`
}

type SessionTransferOwnerResp struct {
	SessionID string `json:"session_id"`
	OwnerID   int64  `json:"owner_id,string"`
}

type SessionDissolveResp struct {
	SessionID string `json:"session_id"`
}

type SessionLeaveResp struct {
	SessionID string `json:"session_id"`
	Left      bool   `json:"left"`
}

type SessionRenameResp struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
}

type SessionSetGroupNicknameResp struct {
	SessionID     string `json:"session_id"`
	GroupNickname string `json:"group_nickname"`
}

type SessionMemberAgentReceiveSettingResp struct {
	SessionID                string `json:"session_id"`
	MemberID                 int64  `json:"member_id,string"`
	MemberType               int16  `json:"member_type"`
	AgentReceiveMode         int16  `json:"agent_receive_mode"`
	AgentReceiveBacklogCount int    `json:"agent_receive_backlog_count"`
}

type SessionPinResp struct {
	SessionID string `json:"session_id"`
	IsPinned  bool   `json:"is_pinned"`
	PinnedAt  int64  `json:"pinned_at"`
}

type SessionMuteResp struct {
	SessionID string `json:"session_id"`
	IsMuted   bool   `json:"is_muted"`
}

type CreateSessionResp struct {
	SessionID string `json:"session_id"`
	IsNew     bool   `json:"is_new"`
}

type memberIdentity struct {
	MemberID   int64
	MemberType int16
}

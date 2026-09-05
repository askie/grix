package customercoach

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/userpref"
	"github.com/askie/grix/backend/internal/store"
)

const maxSnapshotTextRunes = 800

type Snapshot struct {
	Event     EventSnapshot    `json:"event"`
	User      UserSnapshot     `json:"user"`
	Overview  OverviewSnapshot `json:"overview"`
	Agents    []AgentSnapshot  `json:"agents"`
	MainAgent *AgentSnapshot   `json:"main_agent,omitempty"`
	Sessions  SessionsSnapshot `json:"sessions"`
	Usage     UsageSnapshot    `json:"usage"`
}

type EventSnapshot struct {
	Type       string    `json:"type"`
	Source     string    `json:"source"`
	Scenario   string    `json:"scenario"`
	OccurredAt time.Time `json:"occurred_at"`
}

type UserSnapshot struct {
	ID        int64     `json:"id,string"`
	Locale    string    `json:"locale"`
	Region    string    `json:"region"`
	CreatedAt time.Time `json:"created_at"`
}

type OverviewSnapshot struct {
	AgentTotal           int   `json:"agent_total"`
	AgentOnline          int   `json:"agent_online"`
	GroupCount           int64 `json:"group_count"`
	HasGroup             bool  `json:"has_group"`
	MultiAgentGroupCount int64 `json:"multi_agent_group_count"`
	HasMultiAgentGroup   bool  `json:"has_multi_agent_group"`
	HasVoiceCall         bool  `json:"has_voice_call"`
}

type AgentSnapshot struct {
	ID              int64     `json:"id,string"`
	Name            string    `json:"name"`
	ClientType      string    `json:"client_type"`
	ProviderType    int16     `json:"provider_type"`
	Online          bool      `json:"online"`
	Introduction    string    `json:"introduction"`
	ScopeTotal      int       `json:"scope_total"`
	ScopeGranted    []string  `json:"scope_granted"`
	ScopeMissing    []string  `json:"scope_missing"`
	IsMain          bool      `json:"is_main"`
	MediaCapability string    `json:"media_capability"`
	VoiceProvider   string    `json:"voice_provider"`
	CreatedAt       time.Time `json:"created_at"`
}

type SessionsSnapshot struct {
	PrivateAgentSessions int64 `json:"private_agent_sessions"`
	GroupSessions        int64 `json:"group_sessions"`
	MultiAgentGroups     int64 `json:"multi_agent_groups"`
}

type UsageSnapshot struct {
	AgentMessageCount   int64 `json:"agent_message_count"`
	HasSentAgentMessage bool  `json:"has_sent_agent_message"`
	VoiceCallCount      int64 `json:"voice_call_count"`
	HasVoiceCall        bool  `json:"has_voice_call"`
}

func BuildSnapshot(ctx context.Context, userID int64, source, scenario string) (Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if userID <= 0 {
		return Snapshot{}, fmt.Errorf("invalid user id")
	}
	if store.DB == nil {
		return Snapshot{}, fmt.Errorf("db not initialized")
	}

	snapshot := Snapshot{
		Event: EventSnapshot{
			Type:       "user_opened_client",
			Source:     strings.TrimSpace(source),
			Scenario:   strings.TrimSpace(scenario),
			OccurredAt: time.Now().UTC(),
		},
	}
	if snapshot.Event.Source == "" {
		snapshot.Event.Source = "unknown"
	}
	if snapshot.Event.Scenario == "" {
		snapshot.Event.Scenario = "client_open"
	}

	var user model.User
	if err := store.DB.WithContext(ctx).
		Select("id", "region", "created_at").
		First(&user, userID).Error; err != nil {
		return Snapshot{}, err
	}
	snapshot.User = UserSnapshot{
		ID:        user.ID,
		Locale:    userpref.Language(ctx, userID),
		Region:    strings.TrimSpace(user.Region),
		CreatedAt: user.CreatedAt,
	}

	agents, err := loadAgentSnapshots(ctx, userID)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Agents = agents
	for i := range snapshot.Agents {
		if snapshot.Agents[i].Online {
			snapshot.Overview.AgentOnline++
		}
		if snapshot.Agents[i].IsMain && snapshot.MainAgent == nil {
			agentCopy := snapshot.Agents[i]
			snapshot.MainAgent = &agentCopy
		}
	}
	snapshot.Overview.AgentTotal = len(snapshot.Agents)

	sessions, err := loadSessionsSnapshot(ctx, userID)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Sessions = sessions
	snapshot.Overview.GroupCount = sessions.GroupSessions
	snapshot.Overview.HasGroup = sessions.GroupSessions > 0
	snapshot.Overview.MultiAgentGroupCount = sessions.MultiAgentGroups
	snapshot.Overview.HasMultiAgentGroup = sessions.MultiAgentGroups > 0

	usage, err := loadUsageSnapshot(ctx, userID)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Usage = usage
	snapshot.Overview.HasVoiceCall = usage.HasVoiceCall

	return snapshot, nil
}

func loadAgentSnapshots(ctx context.Context, userID int64) ([]AgentSnapshot, error) {
	resp, err := service.AgentListWithContext(ctx, userID, nil)
	if err != nil {
		return nil, err
	}
	allowed := agentscope.AllowedScopes()
	sort.Strings(allowed)

	agentIDs := make([]int64, 0, len(resp))
	for _, agent := range resp {
		if agent.ID > 0 {
			agentIDs = append(agentIDs, agent.ID)
		}
	}
	scopesByAgent, err := loadScopesByAgent(ctx, agentIDs)
	if err != nil {
		return nil, err
	}

	result := make([]AgentSnapshot, 0, len(resp))
	for _, agent := range resp {
		granted := scopesByAgent[agent.ID]
		missing := missingScopes(allowed, granted)
		isMain := agent.ProviderType == model.AgentProviderAPI && len(missing) == 0
		result = append(result, AgentSnapshot{
			ID:              agent.ID,
			Name:            strings.TrimSpace(agent.AgentName),
			ClientType:      strings.TrimSpace(agent.AgentClientType),
			ProviderType:    agent.ProviderType,
			Online:          agent.Online,
			Introduction:    strings.TrimSpace(agent.Introduction),
			ScopeTotal:      len(allowed),
			ScopeGranted:    granted,
			ScopeMissing:    missing,
			IsMain:          isMain,
			MediaCapability: strings.TrimSpace(agent.MediaCapability),
			VoiceProvider:   strings.TrimSpace(agent.VoiceProvider),
			CreatedAt:       time.Unix(agent.CreatedAt, 0).UTC(),
		})
	}
	return result, nil
}

func loadScopesByAgent(ctx context.Context, agentIDs []int64) (map[int64][]string, error) {
	result := make(map[int64][]string, len(agentIDs))
	if len(agentIDs) == 0 {
		return result, nil
	}
	var rows []model.AgentAPIScope
	if err := store.DB.WithContext(ctx).
		Select("agent_id", "scope").
		Where("agent_id IN ?", agentIDs).
		Order("scope ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if !agentscope.IsAllowed(row.Scope) {
			continue
		}
		result[row.AgentID] = append(result[row.AgentID], row.Scope)
	}
	for agentID := range result {
		sort.Strings(result[agentID])
	}
	return result, nil
}

func missingScopes(allowed, granted []string) []string {
	grantedSet := make(map[string]struct{}, len(granted))
	for _, scope := range granted {
		grantedSet[scope] = struct{}{}
	}
	missing := make([]string, 0, len(allowed))
	for _, scope := range allowed {
		if _, ok := grantedSet[scope]; !ok {
			missing = append(missing, scope)
		}
	}
	return missing
}

func loadSessionsSnapshot(ctx context.Context, userID int64) (SessionsSnapshot, error) {
	var out SessionsSnapshot
	if err := store.DB.WithContext(ctx).
		Table("sessions AS s").
		Joins("JOIN session_members AS me ON me.session_id = s.session_id AND me.member_id = ? AND me.member_type = 1", userID).
		Joins("JOIN session_members AS agent_member ON agent_member.session_id = s.session_id AND agent_member.member_type = 2").
		Where("s.session_type = ? AND s.is_deleted = ?", model.SessionTypeDirect, false).
		Distinct("s.session_id").
		Count(&out.PrivateAgentSessions).Error; err != nil {
		return out, err
	}
	if err := store.DB.WithContext(ctx).
		Table("sessions AS s").
		Joins("JOIN session_members AS me ON me.session_id = s.session_id AND me.member_id = ? AND me.member_type = 1", userID).
		Where("s.session_type = ? AND s.is_deleted = ?", model.SessionTypeGroup, false).
		Count(&out.GroupSessions).Error; err != nil {
		return out, err
	}
	if out.GroupSessions == 0 {
		return out, nil
	}
	if err := store.DB.WithContext(ctx).
		Table("sessions AS s").
		Joins("JOIN session_members AS me ON me.session_id = s.session_id AND me.member_id = ? AND me.member_type = 1", userID).
		Joins("JOIN session_members AS agent_member ON agent_member.session_id = s.session_id AND agent_member.member_type = 2").
		Where("s.session_type = ? AND s.is_deleted = ?", model.SessionTypeGroup, false).
		Group("s.session_id").
		Having("COUNT(agent_member.member_id) >= ?", 2).
		Count(&out.MultiAgentGroups).Error; err != nil {
		return out, err
	}
	return out, nil
}

// loadUsageSnapshot measures the two behavioural onboarding steps:
//   - HasSentAgentMessage: the user has sent a message in a session that
//     contains one of the user's OWN agents (shared or foreign agents, and the
//     support account's delegate, do not count as "used your agent").
//   - HasVoiceCall: the user took part in a call that was actually connected.
func loadUsageSnapshot(ctx context.Context, userID int64) (UsageSnapshot, error) {
	var out UsageSnapshot
	if err := store.DB.WithContext(ctx).
		Table("messages AS m").
		Joins("JOIN sessions AS s ON s.session_id = m.session_id").
		Joins("JOIN session_members AS me ON me.session_id = s.session_id AND me.member_id = ? AND me.member_type = 1", userID).
		Joins("JOIN session_members AS agent_member ON agent_member.session_id = s.session_id AND agent_member.member_type = 2").
		// Deleted agents are excluded so this matches AgentListWithContext,
		// which powers Overview.AgentTotal: otherwise a user who deleted every
		// agent still reads as "has used an agent" and gets the wrong step.
		Joins("JOIN agents AS own_agent ON own_agent.id = agent_member.member_id AND own_agent.owner_id = ? AND own_agent.status != ?", userID, model.AgentStatusDeleted).
		Where("m.sender_id = ? AND m.sender_type = ? AND m.is_deleted = ? AND m.is_revoked = ?", userID, int16(1), false, false).
		Distinct("m.msg_id").
		Count(&out.AgentMessageCount).Error; err != nil {
		return out, err
	}
	out.HasSentAgentMessage = out.AgentMessageCount > 0

	if err := store.DB.WithContext(ctx).
		Model(&model.CallRecord{}).
		Where("caller_id = ? OR callee_id = ?", userID, userID).
		// Only calls that were actually connected count as "tried voice":
		// ringing / rejected / missed / error attempts teach the user nothing.
		Where("answered_at IS NOT NULL OR state IN ?", []int16{
			model.CallStateActive,
			model.CallStateAIDelegated,
			model.CallStateHumanActive,
		}).
		Count(&out.VoiceCallCount).Error; err != nil {
		return out, err
	}
	out.HasVoiceCall = out.VoiceCallCount > 0
	return out, nil
}

func RenderMarkdown(snapshot Snapshot) string {
	var b strings.Builder
	b.WriteString("# Grix 用户状态快照\n\n")
	writeKV(&b, "触发事件", snapshot.Event.Type)
	writeKV(&b, "触发来源", snapshot.Event.Source)
	writeKV(&b, "触发场景", snapshot.Event.Scenario)
	writeKV(&b, "触发时间", formatTime(snapshot.Event.OccurredAt))
	writeKV(&b, "用户 ID", strconv.FormatInt(snapshot.User.ID, 10))
	writeKV(&b, "用户语言", snapshot.User.Locale)
	writeKV(&b, "用户区域", snapshot.User.Region)
	if !snapshot.User.CreatedAt.IsZero() {
		writeKV(&b, "注册时间", formatTime(snapshot.User.CreatedAt))
	}

	b.WriteString("\n## 总览\n\n")
	writeBullet(&b, "Agent 总数", strconv.Itoa(snapshot.Overview.AgentTotal))
	writeBullet(&b, "在线 Agent", strconv.Itoa(snapshot.Overview.AgentOnline))
	writeBullet(&b, "是否创建过群", yesNo(snapshot.Overview.HasGroup))
	writeBullet(&b, "是否有多 Agent 群", yesNo(snapshot.Overview.HasMultiAgentGroup))
	writeBullet(&b, "是否使用过语音", yesNo(snapshot.Overview.HasVoiceCall))

	b.WriteString("\n## 主 Agent 判断\n\n")
	b.WriteString("判定规则：拥有全部允许的 Scope 权限，才视为主 Agent。\n\n")
	if snapshot.MainAgent == nil {
		b.WriteString("当前主 Agent：无\n")
	} else {
		agent := snapshot.MainAgent
		b.WriteString("当前主 Agent：" + fallbackText(agent.Name, "未命名 Agent") + "\n\n")
		writeAgentDetails(&b, *agent)
	}

	b.WriteString("\n## Agent 列表\n\n")
	if len(snapshot.Agents) == 0 {
		b.WriteString("暂无 Agent。\n")
	} else {
		for i, agent := range snapshot.Agents {
			fmt.Fprintf(&b, "### %d. %s\n\n", i+1, fallbackText(agent.Name, "未命名 Agent"))
			writeAgentDetails(&b, agent)
			b.WriteString("\n")
		}
	}

	b.WriteString("## 会话与使用情况\n\n")
	writeBullet(&b, "Agent 私聊数量", formatInt64(snapshot.Sessions.PrivateAgentSessions))
	writeBullet(&b, "群聊数量", formatInt64(snapshot.Sessions.GroupSessions))
	writeBullet(&b, "多 Agent 群数量", formatInt64(snapshot.Sessions.MultiAgentGroups))
	writeBullet(&b, "是否发过 Agent 消息", yesNo(snapshot.Usage.HasSentAgentMessage))
	writeBullet(&b, "Agent 消息数量", formatInt64(snapshot.Usage.AgentMessageCount))
	writeBullet(&b, "是否有语音通话", yesNo(snapshot.Usage.HasVoiceCall))
	writeBullet(&b, "语音通话数量", formatInt64(snapshot.Usage.VoiceCallCount))

	b.WriteString("\n## 给客服 Agent 的要求\n\n")
	b.WriteString("这份快照是内部上下文，不是用户消息，不要复述。\n")
	b.WriteString("请根据用户状态和你对该用户的记忆，判断是否需要主动发一条引导。\n")
	b.WriteString("发给用户的只能是自然客服口吻的对话文字，每次只引导一个下一步动作。\n")
	b.WriteString("严禁把任何分析、推理、决策过程发给用户（如“快照显示”“按引导规则”“用户 N 小时后再次打开”），也不要提及快照、检测、规则等内部概念。\n")
	b.WriteString("如果不需要，必须只返回固定命令 /no_reply，不要返回“选择沉默”“无需引导”之类的说明。\n")
	b.WriteString("你是中国区客服，主动问候语和默认新手引导一律使用中文；只有当用户明确用英文提问、或当前对话已处于英文服务语境时，才用英文回复。\n")
	return b.String()
}

func writeAgentDetails(b *strings.Builder, agent AgentSnapshot) {
	writeBullet(b, "Agent ID", strconv.FormatInt(agent.ID, 10))
	writeBullet(b, "类型", fallbackText(agent.ClientType, providerTypeLabel(agent.ProviderType)))
	writeBullet(b, "在线", yesNo(agent.Online))
	writeBullet(b, "是否主 Agent", yesNo(agent.IsMain))
	writeBullet(b, "介绍", fallbackText(truncateRunes(agent.Introduction, maxSnapshotTextRunes), "无"))
	writeBullet(b, "Scope 完整度", fmt.Sprintf("%d / %d", len(agent.ScopeGranted), agent.ScopeTotal))
	writeBullet(b, "已有 Scope", joinOrNone(agent.ScopeGranted))
	writeBullet(b, "缺失 Scope", joinOrNone(agent.ScopeMissing))
	if agent.MediaCapability != "" {
		writeBullet(b, "媒体能力", agent.MediaCapability)
	}
	if agent.VoiceProvider != "" {
		writeBullet(b, "语音 Provider", agent.VoiceProvider)
	}
}

func writeKV(b *strings.Builder, key, value string) {
	if strings.TrimSpace(value) == "" {
		value = "未知"
	}
	b.WriteString(key + "：" + value + "\n")
}

func writeBullet(b *strings.Builder, key, value string) {
	if strings.TrimSpace(value) == "" {
		value = "未知"
	}
	b.WriteString("- " + key + "：" + value + "\n")
}

func yesNo(v bool) string {
	if v {
		return "是"
	}
	return "否"
}

func formatInt64(v int64) string {
	return strconv.FormatInt(v, 10)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "未知"
	}
	return t.UTC().Format(time.RFC3339)
}

func fallbackText(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func providerTypeLabel(providerType int16) string {
	switch providerType {
	case model.AgentProviderRemote:
		return "remote"
	case model.AgentProviderLocal:
		return "local"
	case model.AgentProviderAPI:
		return "agent_api"
	case model.AgentProviderVoice:
		return "voice"
	default:
		return fmt.Sprintf("provider_%d", providerType)
	}
}

func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "无"
	}
	return strings.Join(items, ", ")
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "...（已截断）"
}

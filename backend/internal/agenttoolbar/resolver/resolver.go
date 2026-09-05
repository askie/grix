package resolver

import (
	"context"
	"strings"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	toolstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/userpref"
	appstore "github.com/askie/grix/backend/internal/store"
)

type ActiveRunProvider interface {
	LoadRunState(ctx context.Context, ownerID int64, sessionID string, agentID int64) toolruntime.RunState
}

type Resolver struct {
	runProvider ActiveRunProvider
}

func New(runProvider ActiveRunProvider) *Resolver {
	return &Resolver{runProvider: runProvider}
}

func (r *Resolver) Resolve(ctx context.Context, ownerID int64, sessionID string, targetAgentID int64) (core.BuildInput, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sessionID = strings.TrimSpace(sessionID)
	if ownerID <= 0 || sessionID == "" {
		return core.BuildInput{}, core.ErrSessionForbidden
	}
	if err := service.EnsureHumanSessionAccessible(ctx, ownerID, sessionID); err != nil {
		return core.BuildInput{}, core.ErrSessionForbidden
	}
	if appstore.DB == nil {
		return core.BuildInput{}, core.ErrSessionForbidden
	}

	var session model.Session
	if err := appstore.DB.WithContext(ctx).
		Select("session_id", "session_type").
		Where("session_id = ?", sessionID).
		First(&session).Error; err != nil {
		return core.BuildInput{}, core.ErrPrivateAgentOnly
	}
	var resolvedAgentID int64
	switch session.SessionType {
	case model.SessionTypeDirect:
		var peer model.SessionMember
		if err := appstore.DB.WithContext(ctx).
			Where("session_id = ? AND member_type = ?", sessionID, 2).
			First(&peer).Error; err != nil {
			return core.BuildInput{}, core.ErrPrivateAgentOnly
		}
		resolvedAgentID = peer.MemberID
	case model.SessionTypeGroup:
		if targetAgentID <= 0 {
			return core.BuildInput{}, core.ErrPrivateAgentOnly
		}
		var targetMember model.SessionMember
		if err := appstore.DB.WithContext(ctx).
			Select("member_id").
			Where("session_id = ? AND member_type = ? AND member_id = ?", sessionID, 2, targetAgentID).
			First(&targetMember).Error; err != nil {
			return core.BuildInput{}, core.ErrSessionForbidden
		}
		resolvedAgentID = targetMember.MemberID
	default:
		return core.BuildInput{}, core.ErrPrivateAgentOnly
	}

	var agent model.Agent
	if err := appstore.DB.WithContext(ctx).
		Select("id", "owner_id", "provider_type", "agent_client_type", "status").
		First(&agent, resolvedAgentID).Error; err != nil {
		return core.BuildInput{}, core.ErrPrivateAgentOnly
	}
	// 工具栏属于「使用」能力：主人或有效被共享者可看/可操作。
	// 不能用 OwnerID==请求用户硬拦，否则共享私聊/群内 @ 共享 agent 会 403。
	// BuildInput.OwnerID 仍是请求用户（共享连接身份），便于按共享者隔离 run/local action。
	ok, err := service.CanUseAgent(ownerID, agent.ID)
	if err != nil {
		return core.BuildInput{}, err
	}
	if !ok {
		return core.BuildInput{}, core.ErrSessionForbidden
	}
	// 只有 ProviderType=3 (Agent API) 的 agent 支持工具栏
	if agent.ProviderType != model.AgentProviderAPI {
		return core.BuildInput{}, core.ErrToolbarUnavailable
	}

	clientType := model.NormalizeAgentClientType(agent.AgentClientType)
	profile, ok, err := toolruntime.LoadProfileForOwner(ctx, agent.ID, ownerID)
	if err != nil {
		return core.BuildInput{}, err
	}
	if !ok {
		profile = toolruntime.Profile{
			AgentID:    agent.ID,
			OwnerID:    agent.OwnerID,
			ClientType: clientType,
			Online:     false,
		}
	}
	if profile.ClientType == "" {
		profile.ClientType = clientType
	}

	binding, _, err := toolstore.LoadBinding(ctx, agent.ID, sessionID)
	if err != nil {
		return core.BuildInput{}, err
	}
	binding = mergeGeminiBinding(ctx, profile.ClientType, agent.ID, sessionID, binding)

	run := toolruntime.RunState{}
	if r != nil && r.runProvider != nil {
		run = r.loadAuthorizedAgentRunState(ctx, ownerID, agent.OwnerID, sessionID, resolvedAgentID)
	}

	return core.BuildInput{
		OwnerID: ownerID,
		Session: core.SessionInfo{
			SessionID:   sessionID,
			SessionType: session.SessionType,
		},
		Agent: core.AgentInfo{
			AgentID:      agent.ID,
			OwnerID:      agent.OwnerID,
			ProviderType: agent.ProviderType,
			ClientType:   profile.ClientType,
		},
		Language: LoadPreferredLanguage(ctx, ownerID),
		Runtime:  profile,
		Binding: core.BindingInfo{
			ProviderKey:  firstNonEmpty(binding.ProviderKey, profile.ClientType),
			BindingID:    binding.BindingID,
			Cwd:          binding.Cwd,
			Status:       binding.Status,
			WorkerStatus: binding.WorkerStatus,
			Meta:         binding.Meta,
		},
		Run: run,
		// 自定义斜杠命令在 core.normalizeSnapshot 里追加到内置命令之后。
		CustomSlashCommands: service.AgentSlashCommandsForToolbar(ctx, agent.ID, profile.ClientType),
	}, nil
}

// loadAuthorizedAgentRunState is called only after Resolve has verified both
// session access and CanUseAgent for the selected agent. Shared users keep their
// own owner-scoped lookup first; if it misses, the agent owner's scope is used
// only for the same session and selected agent.
func (r *Resolver) loadAuthorizedAgentRunState(
	ctx context.Context,
	viewerID int64,
	agentOwnerID int64,
	sessionID string,
	agentID int64,
) toolruntime.RunState {
	load := func(lookupOwnerID int64) toolruntime.RunState {
		run := r.runProvider.LoadRunState(ctx, lookupOwnerID, sessionID, agentID)
		if !run.HasActiveRun || run.AgentID != agentID {
			return toolruntime.RunState{}
		}
		return run
	}

	run := load(viewerID)
	if run.HasActiveRun || agentOwnerID <= 0 || agentOwnerID == viewerID {
		return run
	}
	return load(agentOwnerID)
}

// LoadPreferredLanguage 返回指定用户的语言偏好（统一走 userpref.Language 读取），
// 收窄到工具栏支持的 zh/en 二选一，供需要按用户语言下发文案的调用方（如
// agentapi 的卡片文案）复用。
func LoadPreferredLanguage(ctx context.Context, userID int64) string {
	return tooli18n.NormalizeLanguage(userpref.Language(ctx, userID))
}

func mergeGeminiBinding(ctx context.Context, clientType string, agentID int64, sessionID string, binding toolstore.BindingRecord) toolstore.BindingRecord {
	if clientType != model.AgentClientTypeGemini || appstore.DB == nil {
		return binding
	}
	var sessionCtx model.GeminiSessionContext
	err := appstore.DB.WithContext(ctx).
		Where("agent_id = ? AND session_id = ?", agentID, sessionID).
		First(&sessionCtx).Error
	if err != nil {
		return binding
	}
	if binding.Meta == nil {
		binding.Meta = map[string]any{}
	}
	if binding.Cwd == "" {
		binding.Cwd = strings.TrimSpace(sessionCtx.Cwd)
	}
	if binding.Meta["mode_id"] == nil && strings.TrimSpace(sessionCtx.ModeID) != "" {
		binding.Meta["mode_id"] = strings.TrimSpace(sessionCtx.ModeID)
	}
	if binding.Meta["model_id"] == nil && strings.TrimSpace(sessionCtx.ModelID) != "" {
		binding.Meta["model_id"] = strings.TrimSpace(sessionCtx.ModelID)
	}
	if binding.ProviderKey == "" {
		binding.ProviderKey = model.AgentClientTypeGemini
	}
	return binding
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/conversationaudit"
	"github.com/askie/grix/backend/internal/model"
)

type Resolver interface {
	Resolve(ctx context.Context, ownerID int64, sessionID string, targetAgentID int64) (BuildInput, error)
}

type Registry interface {
	Resolve(ctx MatchContext) Package
}

type Cache interface {
	LoadSnapshot(ctx context.Context, ownerID int64, sessionID string, agentID int64) (toolprotocol.Snapshot, bool, error)
	SaveSnapshot(ctx context.Context, ownerID int64, snapshot toolprotocol.Snapshot) (toolprotocol.Snapshot, bool, error)
	DeleteSnapshot(ctx context.Context, ownerID int64, sessionID string, agentID int64) error
	ListIndexedSessions(ctx context.Context, ownerID, agentID int64) ([]string, error)
	ReserveContextWarm(ctx context.Context, ownerID, agentID int64, sessionID string, ttl time.Duration) (bool, error)
	ReserveRateLimitFetch(ctx context.Context, ownerID int64, accountKey string, ttl time.Duration) (bool, error)
	ReserveAction(ctx context.Context, ownerID int64, sessionID string, agentID int64, clientActionID string) (bool, ActionAck, error)
	CompleteAction(ctx context.Context, ownerID int64, sessionID string, agentID int64, clientActionID string, ack ActionAck) error
}

type Notifier interface {
	Sync(ctx context.Context, ownerID int64, snapshot toolprotocol.Snapshot) error
}

type Service struct {
	resolver Resolver
	registry Registry
	cache    Cache
	notifier Notifier
	executor Executor
}

const codexContextWarmCooldown = 30 * time.Second
const rateLimitFetchCooldown = 5 * time.Second

func NewService(resolver Resolver, registry Registry, cache Cache, notifier Notifier, executor Executor) *Service {
	return &Service{
		resolver: resolver,
		registry: registry,
		cache:    cache,
		notifier: notifier,
		executor: executor,
	}
}

func (s *Service) GetSnapshot(ctx context.Context, ownerID int64, sessionID string, targetAgentID int64) (toolprotocol.Snapshot, error) {
	snapshot, _, _, _, err := s.buildAndStoreSnapshot(ctx, ownerID, sessionID, targetAgentID, "")
	return snapshot, err
}

func (s *Service) HandleAction(ctx context.Context, ownerID int64, req toolprotocol.ActionRequest) (ActionAck, error) {
	snapshot, buildInput, pkg, _, err := s.buildAndStoreSnapshot(ctx, ownerID, req.SessionID, req.TargetAgentID, "")
	if err != nil {
		return ActionAck{}, err
	}
	if req.TargetAgentID > 0 && req.TargetAgentID != snapshot.AgentID {
		return rejectAck(snapshot, req, "agent_mismatch", "toolbar target agent mismatch", buildInput.Language), nil
	}
	if isStopOutputAction(req) {
		s.clearComposingState(ctx, ownerID, req.SessionID, snapshot.AgentID)
	}
	if pkg == nil || !snapshot.Visible {
		return rejectAck(snapshot, req, "toolbar_unavailable", "toolbar unavailable", buildInput.Language), nil
	}
	if strings.TrimSpace(req.ToolbarID) != snapshot.ToolbarID {
		return rejectAck(snapshot, req, "toolbar_mismatch", "toolbar mismatch", buildInput.Language), nil
	}
	// 不再因 revision 漂移硬拒绝：run 执行期间 agent_output_status 会频繁刷新工具栏，
	// revision 持续变动会把"切模式/模型"等本未改变的动作误判为过期并静默丢弃。
	// 真正的有效性由下面针对"当前最新快照"的 FindItem/Disabled/validateActionRequest
	// 校验保证：item 不在、被禁用或选项失效才拒绝；否则按当前快照正常下发。
	item, ok := snapshot.FindItem(strings.TrimSpace(req.ItemID))
	if !ok || strings.TrimSpace(req.ActionID) != item.ActionID {
		return rejectAck(snapshot, req, "invalid_action", "toolbar action is invalid", buildInput.Language), nil
	}
	if item.Disabled {
		return rejectAck(snapshot, req, "item_disabled", "toolbar item is disabled", buildInput.Language), nil
	}
	if err := validateActionRequest(item, req); err != nil {
		return rejectAck(snapshot, req, "invalid_action", err.Error(), buildInput.Language), nil
	}

	reserved, existing, err := s.cache.ReserveAction(ctx, ownerID, req.SessionID, snapshot.AgentID, req.ClientActionID)
	if err == nil && !reserved {
		if existing.UpdatedAt <= 0 {
			existing = rejectAck(snapshot, req, "duplicate", "toolbar action is already processing", buildInput.Language)
		}
		existing.Duplicate = true
		if existing.CurrentRevision <= 0 {
			existing.CurrentRevision = snapshot.Revision
		}
		return existing, nil
	}
	if err != nil {
		return rejectAck(snapshot, req, "store_error", "failed to reserve toolbar action", buildInput.Language), nil
	}

	result, actionErr := pkg.HandleAction(ctx, ActionInput{
		BuildInput: buildInput,
		Snapshot:   snapshot,
		Item:       item,
		Request:    req,
		Executor:   s.executor,
	})
	ack := buildAckFromResult(snapshot, req, result, actionErr, buildInput.Language)
	_ = s.cache.CompleteAction(ctx, ownerID, req.SessionID, snapshot.AgentID, req.ClientActionID, ack)

	if result.Outcome == toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh {
		_ = s.RefreshSession(ctx, ownerID, req.SessionID, req.ActionID)
	}
	return ack, nil
}

func (s *Service) clearComposingState(ctx context.Context, ownerID int64, sessionID string, agentID int64) {
	clearer, ok := s.executor.(ComposingStateClearer)
	if !ok || clearer == nil {
		return
	}
	_ = clearer.ClearComposingState(ctx, StopOutputRequest{
		AgentID:   agentID,
		OwnerID:   ownerID,
		SessionID: sessionID,
	})
}

func isStopOutputAction(req toolprotocol.ActionRequest) bool {
	return strings.TrimSpace(req.ActionID) == "stop_output" ||
		strings.TrimSpace(req.ItemID) == "stop_output"
}

func (s *Service) InvalidateSession(ctx context.Context, ownerID int64, sessionID string) {
	if s == nil || s.cache == nil || ownerID <= 0 || strings.TrimSpace(sessionID) == "" {
		return
	}
	_ = s.cache.DeleteSnapshot(ctx, ownerID, sessionID, 0)
}

func (s *Service) RefreshSession(ctx context.Context, ownerID int64, sessionID, reason string) error {
	snapshot, _, _, changed, err := s.buildAndStoreSnapshot(ctx, ownerID, sessionID, 0, reason)
	if err != nil {
		_ = s.cache.DeleteSnapshot(ctx, ownerID, sessionID, 0)
		return err
	}
	if s.notifier == nil {
		return nil
	}
	if !changed && !forceSyncOnReason(reason) {
		return nil
	}
	return s.notifier.Sync(ctx, ownerID, snapshot)
}

func forceSyncOnReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "local_action_result", "local_action_timeout", "thread_compact_result", "skill_refresh":
		return true
	default:
		return false
	}
}

func (s *Service) RefreshSessionForAgent(ctx context.Context, ownerID int64, sessionID string, agentID int64, reason string) error {
	snapshot, _, _, changed, err := s.buildAndStoreSnapshot(ctx, ownerID, sessionID, agentID, reason)
	if err != nil {
		_ = s.cache.DeleteSnapshot(ctx, ownerID, sessionID, agentID)
		return err
	}
	if s.notifier == nil {
		return nil
	}
	if !changed && !forceSyncOnReason(reason) {
		return nil
	}
	return s.notifier.Sync(ctx, ownerID, snapshot)
}

func (s *Service) RefreshByAgent(ctx context.Context, ownerID, agentID int64, reason string) error {
	if s == nil || s.cache == nil || ownerID <= 0 || agentID <= 0 {
		return nil
	}
	sessions, err := s.cache.ListIndexedSessions(ctx, ownerID, agentID)
	if err != nil {
		return err
	}
	var firstErr error
	for _, sessionID := range sessions {
		if refreshErr := s.RefreshSession(ctx, ownerID, sessionID, reason); refreshErr != nil && firstErr == nil {
			firstErr = refreshErr
		}
	}
	return firstErr
}

func (s *Service) buildAndStoreSnapshot(ctx context.Context, ownerID int64, sessionID string, targetAgentID int64, reason string) (toolprotocol.Snapshot, BuildInput, Package, bool, error) {
	if s == nil || s.resolver == nil || s.registry == nil || s.cache == nil {
		return toolprotocol.Snapshot{}, BuildInput{}, nil, false, ErrToolbarUnavailable
	}
	buildInput, err := s.resolver.Resolve(ctx, ownerID, sessionID, targetAgentID)
	if err != nil {
		return toolprotocol.Snapshot{}, BuildInput{}, nil, false, err
	}
	match := MatchContext{
		OwnerID: ownerID,
		Session: buildInput.Session,
		Agent:   buildInput.Agent,
		Runtime: buildInput.Runtime,
	}
	pkg := s.registry.Resolve(match)

	snapshot := toolprotocol.Snapshot{
		SessionID: buildInput.Session.SessionID,
		AgentID:   buildInput.Agent.AgentID,
		ToolbarID: buildToolbarID("none"),
		Visible:   false,
		Items:     []toolprotocol.Item{},
	}
	if pkg != nil {
		snapshot, err = pkg.Build(ctx, buildInput)
		if err != nil {
			return toolprotocol.Snapshot{}, BuildInput{}, pkg, false, err
		}
	}
	snapshot = normalizeSnapshot(snapshot, buildInput, pkg)
	// 对话审计开关：后端接管后随快照下发（nil 时字段缺席，前端走本地回退）。
	snapshot.AuditEnabled = conversationaudit.SnapshotAuditEnabled(ownerID, snapshot.AgentID)
	saved, changed, err := s.cache.SaveSnapshot(ctx, ownerID, snapshot)
	s.maybeWarmCodexContext(buildInput)
	if shouldAutoFetchRateLimits(reason) {
		s.maybeAutoFetchClaudeRateLimits(buildInput)
	}
	return saved, buildInput, pkg, changed, err
}

func shouldAutoFetchRateLimits(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "local_action_result", "local_action_timeout", "thread_compact_result":
		return true
	default:
		return false
	}
}

func normalizeSnapshot(snapshot toolprotocol.Snapshot, buildInput BuildInput, pkg Package) toolprotocol.Snapshot {
	snapshot.SessionID = buildInput.Session.SessionID
	snapshot.AgentID = buildInput.Agent.AgentID
	if pkg != nil {
		snapshot.ToolbarID = buildToolbarID(pkg.Key())
	} else if strings.TrimSpace(snapshot.ToolbarID) == "" {
		snapshot.ToolbarID = buildToolbarID("none")
	}
	if snapshot.Items == nil {
		snapshot.Items = []toolprotocol.Item{}
	}
	// library_skills 是纯 runtime 透传数据(不受 Visible 影响)，各 Package.Build()
	// 不感知也不产出它，统一在这里从 Redis runtime profile 灌入。
	snapshot.LibrarySkills = buildInput.Runtime.LibrarySkills
	if !snapshot.Visible {
		snapshot.Items = []toolprotocol.Item{}
	}
	// 可见 toolbar 统一前置文件浏览按钮(前端 local_action 驱动)。
	// 普通 agent 仅在已有按钮时前置;声明 OmitQueueButton 的基础 agent(如 hermes/openclaw)
	// 即便没有其他按钮也保留文件浏览,且不前置队列按钮。
	if snapshot.Visible && (len(snapshot.Items) > 0 || snapshot.OmitQueueButton) {
		prefix := []toolprotocol.Item{}
		if !snapshot.OmitListSessionsButton {
			prefix = append(prefix, toolprotocol.Item{
				ItemID:      "list_sessions",
				GroupID:     "utility",
				Kind:        toolprotocol.ItemKindButton,
				ActionID:    "list_sessions",
				Icon:        "list_alt",
				Variant:     "primary",
				Tooltip:     "会话列表",
				LocalAction: "list_sessions",
			})
		}
		prefix = append(prefix, toolprotocol.Item{
			ItemID:      "browse_files",
			GroupID:     "utility",
			Kind:        toolprotocol.ItemKindButton,
			ActionID:    "browse_files",
			Icon:        "folder",
			Variant:     "primary",
			Tooltip:     "浏览远程文件",
			LocalAction: "browse_files",
		})
		if !snapshot.OmitQueueButton {
			queueItem := toolprotocol.Item{
				ItemID:      "show_queue",
				GroupID:     "utility",
				Kind:        toolprotocol.ItemKindButton,
				ActionID:    "show_queue",
				Variant:     "primary",
				Tooltip:     "查看队列",
				LocalAction: "show_queue",
			}
			// 运行中的事件计入队列数量:后端工具栏侧统一配置徽标,前端据此显示。
			if buildInput.Run.HasActiveRun {
				queueItem.BadgeText = "1"
			}
			prefix = append(prefix, queueItem)
		}
		snapshot.Items = append(prefix, snapshot.Items...)
	}
	snapshot = localizeSnapshot(snapshot, buildInput.Language)
	return snapshot
}

func validateActionRequest(item toolprotocol.Item, req toolprotocol.ActionRequest) error {
	switch normalizeItemKind(item.Kind) {
	case toolprotocol.ItemKindButton:
		if strings.TrimSpace(req.Event) != "click" {
			return fmt.Errorf("button action requires click event")
		}
	case toolprotocol.ItemKindProgress:
		if strings.TrimSpace(req.Event) != "click" {
			return fmt.Errorf("progress action requires click event")
		}
	case toolprotocol.ItemKindSelect:
		if strings.TrimSpace(req.Event) != "select" {
			return fmt.Errorf("select action requires select event")
		}
		option, ok := item.FindOption(strings.TrimSpace(req.OptionID))
		if !ok || option.Disabled {
			return fmt.Errorf("select option is invalid")
		}
	case toolprotocol.ItemKindToggleList:
		switch strings.TrimSpace(req.Event) {
		case "click", "refresh":
			return nil
		case "enable", "disable":
			toggle, ok := item.FindToggle(strings.TrimSpace(req.OptionID))
			if !ok || toggle.Locked {
				return fmt.Errorf("toggle option is invalid")
			}
			return nil
		default:
			return fmt.Errorf("toggle_list action requires click, refresh, enable, or disable")
		}
	default:
		return fmt.Errorf("toolbar item kind is invalid")
	}
	return nil
}

func normalizeItemKind(kind string) string {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	switch normalized {
	case "cp":
		return toolprotocol.ItemKindButton
	default:
		return normalized
	}
}

func buildToolbarID(key string) string {
	return fmt.Sprintf("agent-toolbar:%s:v1", strings.TrimSpace(key))
}

func rejectAck(snapshot toolprotocol.Snapshot, req toolprotocol.ActionRequest, code, message, language string) ActionAck {
	return ActionAck{
		SessionID:       strings.TrimSpace(req.SessionID),
		ToolbarID:       snapshot.ToolbarID,
		ClientActionID:  strings.TrimSpace(req.ClientActionID),
		Accepted:        false,
		Code:            strings.TrimSpace(code),
		Message:         tooli18n.LocalizeText(language, strings.TrimSpace(message)),
		CurrentRevision: snapshot.Revision,
		UpdatedAt:       time.Now().UnixMilli(),
	}
}

func buildAckFromResult(snapshot toolprotocol.Snapshot, req toolprotocol.ActionRequest, result toolprotocol.ActionResult, actionErr error, language string) ActionAck {
	if actionErr != nil {
		return rejectAck(snapshot, req, "action_failed", actionErr.Error(), language)
	}
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		return rejectAck(snapshot, req, result.Code, result.Message, language)
	}
	ack := ActionAck{
		SessionID:       snapshot.SessionID,
		ToolbarID:       snapshot.ToolbarID,
		ClientActionID:  strings.TrimSpace(req.ClientActionID),
		Accepted:        true,
		Code:            strings.TrimSpace(result.Code),
		Message:         tooli18n.LocalizeText(language, strings.TrimSpace(result.Message)),
		CurrentRevision: snapshot.Revision,
		UpdatedAt:       time.Now().UnixMilli(),
	}
	return ack
}

func localizeSnapshot(snapshot toolprotocol.Snapshot, language string) toolprotocol.Snapshot {
	for i := range snapshot.Items {
		item := &snapshot.Items[i]
		item.Label = tooli18n.LocalizeText(language, item.Label)
		item.Tooltip = tooli18n.LocalizeText(language, item.Tooltip)
		item.BadgeText = tooli18n.LocalizeText(language, item.BadgeText)
		item.ConfirmTitle = tooli18n.LocalizeText(language, item.ConfirmTitle)
		item.ConfirmText = tooli18n.LocalizeText(language, item.ConfirmText)
		item.Value = tooli18n.LocalizeText(language, item.Value)
		item.Placeholder = tooli18n.LocalizeText(language, item.Placeholder)
		item.CenterText = tooli18n.LocalizeText(language, item.CenterText)
		item.ProgressDesc = tooli18n.LocalizeText(language, item.ProgressDesc)
		item.ProgressDetail = tooli18n.LocalizeText(language, item.ProgressDetail)
		for j := range item.Options {
			item.Options[j].Label = tooli18n.LocalizeText(language, item.Options[j].Label)
		}
		for j := range item.Commands {
			item.Commands[j].Name = tooli18n.LocalizeText(language, item.Commands[j].Name)
			item.Commands[j].Description = tooli18n.LocalizeText(language, item.Commands[j].Description)
		}
		for j := range item.Toggles {
			item.Toggles[j].LockReason = tooli18n.LocalizeText(language, item.Toggles[j].LockReason)
		}
	}
	return snapshot
}

func (s *Service) maybeWarmCodexContext(in BuildInput) {
	if s == nil || s.cache == nil || s.executor == nil {
		return
	}
	if in.Agent.AgentID <= 0 || in.OwnerID <= 0 || strings.TrimSpace(in.Session.SessionID) == "" {
		return
	}
	if in.Agent.ClientType != model.AgentClientTypeCodex {
		return
	}
	if !hasWarmableCodexBinding(in.Binding) || isStoppedWorker(in.Binding) {
		return
	}
	if !in.Runtime.Online || !in.Runtime.HasLocalAction("get_context") {
		return
	}
	meta := in.Binding.Meta
	if meta != nil && metaValueString(meta, "model_id") != "" &&
		metaValueString(meta, "mode_id") != "" &&
		hasAvailableModels(meta["available_models"]) {
		return
	}
	reserved, err := s.cache.ReserveContextWarm(
		context.Background(),
		in.OwnerID,
		in.Agent.AgentID,
		in.Session.SessionID,
		codexContextWarmCooldown,
	)
	if err != nil || !reserved {
		return
	}
	_ = s.executor.DispatchLocalAction(context.Background(), LocalActionRequest{
		OwnerID:    in.OwnerID,
		AgentID:    in.Agent.AgentID,
		SessionID:  in.Session.SessionID,
		ActionType: "get_context",
		Params: map[string]any{
			"session_id": in.Session.SessionID,
		},
		TimeoutMs: 15_000,
	})
}

func hasWarmableCodexBinding(binding BindingInfo) bool {
	return strings.TrimSpace(binding.BindingID) != "" || strings.TrimSpace(binding.Cwd) != ""
}

func isStoppedWorker(binding BindingInfo) bool {
	return strings.EqualFold(strings.TrimSpace(binding.WorkerStatus), "stopped")
}

func hasAvailableModels(value any) bool {
	list, ok := value.([]any)
	return ok && len(list) > 0
}

func (s *Service) maybeAutoFetchClaudeRateLimits(in BuildInput) {
	if s == nil || s.cache == nil || s.executor == nil {
		return
	}
	if in.Agent.AgentID <= 0 || in.OwnerID <= 0 || strings.TrimSpace(in.Session.SessionID) == "" {
		return
	}
	switch in.Agent.ClientType {
	case model.AgentClientTypeClaude, model.AgentClientTypeCodex,
		model.AgentClientTypeGemini, model.AgentClientTypeQwen,
		model.AgentClientTypeKiro, model.AgentClientTypeCopilot,
		model.AgentClientTypeCursor:
	default:
		return
	}
	if strings.TrimSpace(in.Binding.BindingID) == "" && strings.TrimSpace(in.Binding.Cwd) == "" {
		return
	}
	if !in.Runtime.Online || !in.Runtime.HasLocalAction("get_rate_limits") {
		return
	}
	accountKey := rateLimitAccountKey(in)
	if accountKey == "" {
		return
	}
	reserved, err := s.cache.ReserveRateLimitFetch(
		context.Background(),
		in.OwnerID,
		accountKey,
		rateLimitFetchCooldown,
	)
	if err != nil || !reserved {
		return
	}
	_ = s.executor.DispatchLocalAction(context.Background(), LocalActionRequest{
		OwnerID:    in.OwnerID,
		AgentID:    in.Agent.AgentID,
		SessionID:  in.Session.SessionID,
		ActionType: "get_rate_limits",
		Params: map[string]any{
			"session_id": in.Session.SessionID,
		},
		TimeoutMs: 20_000,
	})
}

func rateLimitAccountKey(in BuildInput) string {
	provider := strings.TrimSpace(in.Binding.ProviderKey)
	bindingID := strings.TrimSpace(in.Binding.BindingID)
	if provider == "" || bindingID == "" {
		return ""
	}
	return provider + ":" + bindingID
}

func metaValueString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

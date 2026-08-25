package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/askie/grix/backend/internal/ws/agentstream"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
)

const (
	sessionActivityIndexTTL    = time.Hour
	humanInputActivityTTL      = 5 * time.Second
	humanViewingActivityTTL    = 12 * time.Second
	// Agent composing: connector renews about every 8–25s and sends
	// ttl_ms=30–45s (queue ticks use 45s so one late tick does not drop the
	// indicator). Keep default/min aligned with that renew window (+ a few
	// seconds), not a long independent frontend-style hold.
	nonHumanActivityTTL        = 30 * time.Second
	humanInputActivityMinTTL   = 2 * time.Second
	humanInputActivityMaxTTL   = 10 * time.Second
	humanViewingActivityMinTTL = 5 * time.Second
	humanViewingActivityMaxTTL = 30 * time.Second
	nonHumanActivityMinTTL     = 15 * time.Second
	nonHumanActivityMaxTTL     = 2 * time.Minute
)

var errSessionActivityPermissionDenied = errors.New("session activity permission denied")

var isCurrentAgentAPIRefEvent = func(ownerID int64, sessionID string, refEventID string) bool {
	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		return true
	}
	activeRun := mgr.LookupActiveRunBySessionOwner(ownerID, sessionID)
	if activeRun == nil {
		return false
	}
	refEventID = strings.TrimSpace(refEventID)
	if refEventID == "" {
		return true
	}
	return strings.TrimSpace(activeRun.EventID) == refEventID
}

func sessionActivityIndexKey(sessionID string) string {
	return fmt.Sprintf("im:activity_index:%s", sessionID)
}

func sessionActivityKey(
	sessionID string,
	actorType string,
	actorID int64,
	kind string,
) string {
	return fmt.Sprintf("im:activity:%s:%s:%d:%s", sessionID, actorType, actorID, kind)
}

func normalizeSessionActivityTTL(kind string, source string, requestedTTLMS int64) time.Duration {
	defaultTTL := nonHumanActivityTTL
	minTTL := nonHumanActivityMinTTL
	maxTTL := nonHumanActivityMaxTTL

	if kind == protocol.SessionActivityKindViewing {
		defaultTTL = humanViewingActivityTTL
		minTTL = humanViewingActivityMinTTL
		maxTTL = humanViewingActivityMaxTTL
	} else if source == protocol.SessionActivitySourceHumanInput {
		defaultTTL = humanInputActivityTTL
		minTTL = humanInputActivityMinTTL
		maxTTL = humanInputActivityMaxTTL
	}

	if requestedTTLMS <= 0 {
		return defaultTTL
	}

	requested := time.Duration(requestedTTLMS) * time.Millisecond
	switch {
	case requested < minTTL:
		return minTTL
	case requested > maxTTL:
		return maxTTL
	default:
		return requested
	}
}

func isSupportedSessionActivityKind(kind string) bool {
	return kind == protocol.SessionActivityKindComposing ||
		kind == protocol.SessionActivityKindViewing
}

func actorTypeFromSenderType(senderType int16) string {
	if senderType == 2 {
		return protocol.SessionActivityActorTypeAgent
	}
	return protocol.SessionActivityActorTypeHuman
}

func ensureHumanMembership(ctx context.Context, userID int64, sessionID string) error {
	if userID <= 0 || sessionID == "" {
		return errSessionActivityPermissionDenied
	}
	if err := sessionguard.ValidateSessionAvailable(ctx, nil, sessionID); err != nil {
		return err
	}

	var count int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return errSessionActivityPermissionDenied
	}
	return nil
}

func HandleSessionActivitySet(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.SessionActivitySetPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("session_activity_set payload error: %v", err)
		return
	}

	sessionID := payload.SessionID
	if err := validateSessionActivitySetPayload(sessionID, payload.Kind); err != nil {
		logger.L.Warnf("session_activity_set invalid payload user=%d session=%q: %v", conn.GetUserID(), sessionID, err)
		return
	}

	ctx := context.Background()
	if err := ensureHumanMembership(ctx, conn.GetUserID(), sessionID); err != nil {
		logger.L.Warnf("session_activity_set permission denied user=%d session=%s: %v", conn.GetUserID(), sessionID, err)
		return
	}
	if payload.Active && payload.Kind == protocol.SessionActivityKindComposing {
		if err := validateHumanSpeakTrigger(ctx, sessionID, conn.GetUserID()); err != nil {
			logger.L.Warnf(
				"session_activity_set speaking denied user=%d session=%s err=%v",
				conn.GetUserID(),
				sessionID,
				err,
			)
			return
		}
	}

	activity := protocol.SessionActivityPayload{
		SessionID:    sessionID,
		Kind:         payload.Kind,
		Active:       payload.Active,
		ActorID:      conn.GetUserID(),
		ActorType:    protocol.SessionActivityActorTypeHuman,
		ExecutorID:   conn.GetUserID(),
		ExecutorType: protocol.SessionActivityActorTypeHuman,
		Source:       protocol.SessionActivitySourceHumanInput,
		RefMsgID:     payload.RefMsgID,
		RefEventID:   payload.RefEventID,
		Activity:     payload.Activity,
	}

	if payload.Active {
		if err := UpsertSessionActivityWithTTL(ctx, hub, activity, payload.TTLMS); err != nil {
			logger.L.Warnf("session_activity_set upsert failed user=%d session=%s: %v", conn.GetUserID(), sessionID, err)
		}
		return
	}

	if err := ClearSessionActivity(ctx, hub, activity); err != nil {
		logger.L.Warnf("session_activity_set clear failed user=%d session=%s: %v", conn.GetUserID(), sessionID, err)
	}
}

func HandleSessionActivityList(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.SessionActivityListPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("session_activity_list payload error: %v", err)
		return
	}
	if payload.SessionID == "" {
		logger.L.Warnf("session_activity_list missing session_id user=%d", conn.GetUserID())
		return
	}

	ctx := context.Background()
	if err := ensureHumanMembership(ctx, conn.GetUserID(), payload.SessionID); err != nil {
		logger.L.Warnf("session_activity_list permission denied user=%d session=%s: %v", conn.GetUserID(), payload.SessionID, err)
		return
	}

	activities, err := ListSessionActivities(ctx, payload.SessionID)
	if err != nil {
		logger.L.Warnf("session_activity_list load failed user=%d session=%s: %v", conn.GetUserID(), payload.SessionID, err)
		return
	}
	conn.SendPayload(protocol.CmdSessionActivityListResp, pkt.Seq, protocol.SessionActivityListRespPayload{
		SessionID:  payload.SessionID,
		Activities: activities,
	})
}

func SetSessionActivityFromAgentAPI(
	ctx context.Context,
	hub HubInterface,
	agentID int64,
	ownerID int64,
	payload protocol.SessionActivitySetPayload,
) error {
	if err := validateSessionActivitySetPayload(payload.SessionID, payload.Kind); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := sessionguard.ValidateSessionAvailable(ctx, nil, payload.SessionID); err != nil {
		return err
	}

	// Active=false composing must clear every agent_api composing entry in the
	// session, not just the current ResolveIdentity actor key. Delegate↔direct
	// flips leave composing under a different actor; identity-keyed clear then
	// deletes a missing key and never broadcasts Active=false to the frontend.
	if !payload.Active && payload.Kind == protocol.SessionActivityKindComposing {
		return ClearAgentComposingActivityBySession(ctx, hub, payload.SessionID)
	}

	identity, err := agentmsg.ResolveIdentity(ctx, agentmsg.IdentityParams{
		Mode:      agentmsg.ModeAgentAPI,
		SessionID: payload.SessionID,
		OwnerID:   ownerID,
		AgentID:   agentID,
	})
	if err != nil {
		return err
	}
	if payload.Active && payload.Kind == protocol.SessionActivityKindComposing {
		// Queue-aware agents push queue_snapshot. Once the authoritative snapshot
		// is empty, ignore (and heal) composing ticks so the indicator cannot
		// keep running after the task queue has already drained to 0.
		// Agents that never emit queue_snapshot never set the idle flag.
		if wsagentapi.IsSessionQueueIdle(ctx, ownerID, payload.SessionID) {
			if HasAgentComposingActivity(ctx, payload.SessionID, agentID) {
				logger.L.Infof("agent composing tick ignored: queue idle, clearing stale composing session=%s agent=%d owner=%d", payload.SessionID, agentID, ownerID)
				return ClearAgentComposingActivityBySession(ctx, hub, payload.SessionID)
			}
			logger.L.Infof("agent composing tick ignored: queue idle session=%s agent=%d owner=%d", payload.SessionID, agentID, ownerID)
			return nil
		}
		if err := validateSessionSpeakTrigger(
			ctx,
			payload.SessionID,
			identity.SenderID,
			identity.SenderType,
		); err != nil {
			return err
		}
		// composing 信号由 agent 主动上报，代表"正在工作"，后端直接信任，
		// 不再用 active-run 校验是否 stale：各 agent 不一定有 active-run 概念，
		// 且 event_result 清掉 active-run 后会误杀仍在工作的 agent（如 kiro recovery）。
		// 过期由 composing 自身 TTL(30s) + agent 主动 active=false 兜底。
		// 续期活跃 stream 的 Redis 状态 key，防止 agent 工具调用期间无 chunk 输出导致 key 过期
		agentstream.RenewActiveStreams(ctx, agentID, agentmsg.DefaultBuilderTTL)
	}

	activity := protocol.SessionActivityPayload{
		SessionID:    payload.SessionID,
		Kind:         payload.Kind,
		Active:       payload.Active,
		ActorID:      identity.SenderID,
		ActorType:    actorTypeFromSenderType(identity.SenderType),
		ExecutorID:   agentID,
		ExecutorType: protocol.SessionActivityActorTypeAgent,
		Source:       protocol.SessionActivitySourceAgentAPI,
		RefMsgID:     payload.RefMsgID,
		RefEventID:   payload.RefEventID,
		Activity:     payload.Activity,
	}

	if payload.Active {
		return UpsertSessionActivityWithTTL(ctx, hub, activity, payload.TTLMS)
	}
	return ClearSessionActivity(ctx, hub, activity)
}

func UpsertSessionActivity(
	ctx context.Context,
	hub HubInterface,
	activity protocol.SessionActivityPayload,
) error {
	return UpsertSessionActivityWithTTL(ctx, hub, activity, 0)
}

func UpsertSessionActivityWithTTL(
	ctx context.Context,
	hub HubInterface,
	activity protocol.SessionActivityPayload,
	requestedTTLMS int64,
) error {
	if store.RDB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateSessionActivityPayload(activity); err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	ttl := normalizeSessionActivityTTL(activity.Kind, activity.Source, requestedTTLMS)
	activity.Active = true
	activity.UpdatedAt = now
	activity.ExpiresAt = now + ttl.Milliseconds()

	key := sessionActivityKey(activity.SessionID, activity.ActorType, activity.ActorID, activity.Kind)
	data, err := json.Marshal(activity)
	if err != nil {
		return err
	}

	pipe := store.RDB.TxPipeline()
	pipe.Set(ctx, key, data, ttl)
	pipe.ZAdd(ctx, sessionActivityIndexKey(activity.SessionID), redis.Z{
		Score:  float64(activity.ExpiresAt),
		Member: key,
	})
	pipe.Expire(ctx, sessionActivityIndexKey(activity.SessionID), sessionActivityIndexTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	broadcastSessionActivity(hub, ctx, activity)
	return nil
}

func ClearSessionActivity(
	ctx context.Context,
	hub HubInterface,
	activity protocol.SessionActivityPayload,
) error {
	if store.RDB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateSessionActivityPayload(activity); err != nil {
		return err
	}

	key := sessionActivityKey(activity.SessionID, activity.ActorType, activity.ActorID, activity.Kind)
	existing := loadSessionActivityByKey(ctx, key)
	if existing.SessionID == "" {
		pipe := store.RDB.TxPipeline()
		pipe.Del(ctx, key)
		pipe.ZRem(ctx, sessionActivityIndexKey(activity.SessionID), key)
		pipe.Expire(ctx, sessionActivityIndexKey(activity.SessionID), sessionActivityIndexTTL)
		_, err := pipe.Exec(ctx)
		return err
	}
	activity = existing

	pipe := store.RDB.TxPipeline()
	pipe.Del(ctx, key)
	pipe.ZRem(ctx, sessionActivityIndexKey(activity.SessionID), key)
	pipe.Expire(ctx, sessionActivityIndexKey(activity.SessionID), sessionActivityIndexTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	activity.Active = false
	activity.UpdatedAt = time.Now().UnixMilli()
	activity.ExpiresAt = 0
	broadcastSessionActivity(hub, ctx, activity)
	return nil
}

func ClearSessionActivityByRef(
	ctx context.Context,
	hub HubInterface,
	sessionID string,
	refMsgID string,
	refEventID string,
) error {
	if store.RDB == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	refMsgID = strings.TrimSpace(refMsgID)
	refEventID = strings.TrimSpace(refEventID)
	if sessionID == "" {
		return errors.New("session_id required")
	}
	if refMsgID == "" && refEventID == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	keys, err := store.RDB.ZRange(ctx, sessionActivityIndexKey(sessionID), 0, -1).Result()
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}

	staleKeys := make([]interface{}, 0)
	for _, key := range keys {
		activity := loadSessionActivityByKey(ctx, key)
		if activity.SessionID == "" || !activity.Active {
			staleKeys = append(staleKeys, key)
			continue
		}
		if activity.SessionID != sessionID {
			continue
		}
		match := false
		if refEventID != "" && activity.RefEventID == refEventID {
			match = true
		}
		if !match && refMsgID != "" && activity.RefMsgID == refMsgID {
			match = true
		}
		if !match {
			continue
		}
		if err := ClearSessionActivity(ctx, hub, activity); err != nil {
			return err
		}
	}
	if len(staleKeys) > 0 {
		_ = store.RDB.ZRem(ctx, sessionActivityIndexKey(sessionID), staleKeys...).Err()
	}
	return nil
}

func ListSessionActivities(ctx context.Context, sessionID string) ([]protocol.SessionActivityPayload, error) {
	if store.RDB == nil || sessionID == "" {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := time.Now().UnixMilli()
	indexKey := sessionActivityIndexKey(sessionID)
	if err := store.RDB.ZRemRangeByScore(ctx, indexKey, "-inf", fmt.Sprintf("(%d", now)).Err(); err != nil {
		return nil, err
	}

	keys, err := store.RDB.ZRangeByScore(ctx, indexKey, &redis.ZRangeBy{
		Min: fmt.Sprintf("%d", now),
		Max: "+inf",
	}).Result()
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, nil
	}

	activities := make([]protocol.SessionActivityPayload, 0, len(keys))
	staleKeys := make([]interface{}, 0)
	for _, key := range keys {
		activity := loadSessionActivityByKey(ctx, key)
		if activity.SessionID == "" || activity.ExpiresAt <= now || !activity.Active {
			staleKeys = append(staleKeys, key)
			continue
		}
		if activity.Kind == protocol.SessionActivityKindViewing {
			continue
		}
		activities = append(activities, activity)
	}
	if len(staleKeys) > 0 {
		_ = store.RDB.ZRem(ctx, indexKey, staleKeys...).Err()
	}

	sort.Slice(activities, func(i, j int) bool {
		if activities[i].UpdatedAt != activities[j].UpdatedAt {
			return activities[i].UpdatedAt < activities[j].UpdatedAt
		}
		if activities[i].ActorType != activities[j].ActorType {
			return activities[i].ActorType < activities[j].ActorType
		}
		return activities[i].ActorID < activities[j].ActorID
	})
	return activities, nil
}

func loadSessionActivityByKey(ctx context.Context, key string) protocol.SessionActivityPayload {
	if store.RDB == nil || key == "" {
		return protocol.SessionActivityPayload{}
	}

	text, err := store.RDB.Get(ctx, key).Result()
	if err != nil || text == "" {
		return protocol.SessionActivityPayload{}
	}

	var activity protocol.SessionActivityPayload
	if err := json.Unmarshal([]byte(text), &activity); err != nil {
		logger.L.Warnf("session activity decode error key=%s: %v", key, err)
		return protocol.SessionActivityPayload{}
	}
	return activity
}

func broadcastSessionActivity(hub HubInterface, ctx context.Context, activity protocol.SessionActivityPayload) {
	if activity.SessionID == "" {
		return
	}
	if activity.Kind == protocol.SessionActivityKindViewing {
		return
	}
	if hub == nil {
		agentmsg.BroadcastToSession(ctx, activity.SessionID, protocol.CmdSessionActivitySync, activity)
		return
	}

	var members []model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_type = 1", activity.SessionID).Find(&members).Error; err != nil {
		logger.L.Warnf("session activity load members failed session=%s: %v", activity.SessionID, err)
		return
	}

	for _, m := range members {
		broadcastToUser(hub, ctx, m.MemberID, protocol.CmdSessionActivitySync, activity)
	}
}

func validateSessionActivitySetPayload(sessionID string, kind string) error {
	if sessionID == "" {
		return errors.New("session_id required")
	}
	if !isSupportedSessionActivityKind(kind) {
		return fmt.Errorf("unsupported activity kind: %s", kind)
	}
	return nil
}

func validateSessionActivityPayload(activity protocol.SessionActivityPayload) error {
	if activity.SessionID == "" {
		return errors.New("session_id required")
	}
	if !isSupportedSessionActivityKind(activity.Kind) {
		return fmt.Errorf("unsupported activity kind: %s", activity.Kind)
	}
	if activity.ActorID <= 0 {
		return errors.New("actor_id required")
	}
	if activity.ActorType != protocol.SessionActivityActorTypeHuman &&
		activity.ActorType != protocol.SessionActivityActorTypeAgent {
		return fmt.Errorf("unsupported actor_type: %s", activity.ActorType)
	}
	if activity.ExecutorType != "" &&
		activity.ExecutorType != protocol.SessionActivityActorTypeHuman &&
		activity.ExecutorType != protocol.SessionActivityActorTypeAgent {
		return fmt.Errorf("unsupported executor_type: %s", activity.ExecutorType)
	}
	return nil
}

func ResolveSessionViewingUsers(
	ctx context.Context,
	sessionID string,
	userIDs []int64,
) map[int64]bool {
	result := map[int64]bool{}
	if store.RDB == nil || sessionID == "" || len(userIDs) == 0 {
		return result
	}
	if ctx == nil {
		ctx = context.Background()
	}

	uniqUserIDs := make([]int64, 0, len(userIDs))
	seen := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		uniqUserIDs = append(uniqUserIDs, userID)
	}
	if len(uniqUserIDs) == 0 {
		return result
	}

	pipe := store.RDB.Pipeline()
	existsCmds := make(map[int64]*redis.IntCmd, len(uniqUserIDs))
	for _, userID := range uniqUserIDs {
		key := sessionActivityKey(
			sessionID,
			protocol.SessionActivityActorTypeHuman,
			userID,
			protocol.SessionActivityKindViewing,
		)
		existsCmds[userID] = pipe.Exists(ctx, key)
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		logger.L.Warnf("resolve session viewing users pipeline failed session=%s: %v", sessionID, err)
	}

	for userID, cmd := range existsCmds {
		exists, err := cmd.Result()
		if err != nil {
			continue
		}
		if exists > 0 {
			result[userID] = true
		}
	}
	return result
}

func IsSessionViewingActive(ctx context.Context, sessionID string, userID int64) bool {
	if userID <= 0 {
		return false
	}
	return ResolveSessionViewingUsers(ctx, sessionID, []int64{userID})[userID]
}

// HasAgentComposingActivity reports whether the agent is currently working in
// the session, judged by an agent-API-sourced composing activity. It keys off
// Source (agent_api) rather than actor identity: in delegate mode the agent's
// composing is recorded under the human owner's identity (SenderID=ownerID),
// while in direct mode under the agent identity. Matching by Source covers
// both, so the toolbar reflects "agent is working" regardless of delegation.
// agentID is kept for signature compatibility but no longer constrains lookup.
func HasAgentComposingActivity(ctx context.Context, sessionID string, agentID int64) bool {
	if store.RDB == nil || sessionID == "" {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	activities, err := ListSessionActivities(ctx, sessionID)
	if err != nil {
		return false
	}
	for _, a := range activities {
		if a.Kind == protocol.SessionActivityKindComposing &&
			a.Active &&
			a.Source == protocol.SessionActivitySourceAgentAPI {
			return true
		}
	}
	return false
}

// ClearAgentComposingActivityBySession removes every active agent-API sourced
// composing activity in the session, regardless of which actor identity recorded
// it (direct mode records it under the agent id, delegate mode under the owner
// id). It broadcasts the inactive activity to the session so frontends stop
// showing the composing indicator immediately.
func ClearAgentComposingActivityBySession(ctx context.Context, hub HubInterface, sessionID string) error {
	if store.RDB == nil || sessionID == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	activities, err := ListSessionActivities(ctx, sessionID)
	if err != nil {
		return err
	}
	var lastErr error
	cleared := 0
	for _, a := range activities {
		if a.Kind != protocol.SessionActivityKindComposing || !a.Active || a.Source != protocol.SessionActivitySourceAgentAPI {
			continue
		}
		if err := ClearSessionActivity(ctx, hub, a); err != nil {
			lastErr = err
			continue
		}
		cleared++
	}
	if cleared > 0 {
		logger.L.Infof("cleared agent composing activities session=%s count=%d", sessionID, cleared)
	}
	return lastErr
}

// ClearSessionActivityByActorType removes a specific session activity entry
// by its actor type and kind. Used by the toolbar composing-stop path to
// clear the composing indicator. The activity TTL (30s for agent composing)
// provides a natural fallback if this delete is missed.
func ClearSessionActivityByActorType(ctx context.Context, sessionID string, actorID int64, kind string) {
	if store.RDB == nil || sessionID == "" || actorID <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := sessionActivityKey(sessionID, protocol.SessionActivityActorTypeAgent, actorID, kind)
	pipe := store.RDB.TxPipeline()
	pipe.Del(ctx, key)
	pipe.ZRem(ctx, sessionActivityIndexKey(sessionID), key)
	pipe.Expire(ctx, sessionActivityIndexKey(sessionID), sessionActivityIndexTTL)
	_, _ = pipe.Exec(ctx)
}

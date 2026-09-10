package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
)

const groupContinuationTTL = 90 * time.Second
const groupMentionAllContinuationTTL = 30 * time.Minute
const groupMessageTargetSnapshotTTL = 48 * time.Hour
const groupColdStartIdleThreshold = 30 * time.Minute

// agentAutoLoopChainCap bounds how many consecutive agent-to-agent quote
// replies (with no human message in between) can auto-trigger each other in
// a session before an explicit @mention is required to continue. Prevents
// unattended agent-to-agent ping-pong while still letting normal multi-turn
// agent collaboration (e.g. one agent reporting results back to another)
// trigger the next turn automatically, same as a human quoting an agent does.
const agentAutoLoopChainCap = 10
const agentAutoLoopChainTTL = 30 * time.Minute

// lastAgentContinuationLookback bounds how far back the last-agent-speaker
// continuation fallback scans when the most recent agent speaker is
// ModeMentionOnly and therefore ineligible to be picked up without an
// explicit @mention (see selectContinuableAgentTarget). Keeps a single quiet
// mention-only agent from starving continuation for the rest of the group
// indefinitely, while still bounding the query cost.
const lastAgentContinuationLookback = 20

type groupDispatchSemantics struct {
	MentionUserIDs         []int64
	ExplicitMentionUserIDs []int64
	TargetUserIDs          []int64
	ExplicitMentionAll     bool
	ContinuedMentionAll    bool
	Continued              bool
	ColdStart              bool
	SuppressContinuation   bool
}

type groupTargetSnapshot struct {
	TargetUserIDs []int64 `json:"target_user_ids,omitempty"`
	ColdStart     bool    `json:"cold_start,omitempty"`
}

type groupMentionAllContinuation struct {
	TargetUserIDs []int64 `json:"target_user_ids,omitempty"`
}

type lastSessionMessageMeta struct {
	MsgID      int64     `gorm:"column:msg_id"`
	SenderID   int64     `gorm:"column:sender_id"`
	SenderType int16     `gorm:"column:sender_type"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func resolveLiveGroupDispatchSemantics(
	ctx context.Context,
	sessionID string,
	senderID int64,
	senderType int16,
	quotedMessageID int64,
	content string,
	extraRaw json.RawMessage,
	allowContinuation bool,
) (groupDispatchSemantics, error) {
	return resolveLiveGroupDispatchSemanticsWithNormalization(
		ctx,
		sessionID,
		senderID,
		senderType,
		quotedMessageID,
		content,
		extraRaw,
		resolveGroupMentionDispatchNormalization(
			ctx,
			sessionID,
			senderID,
			senderType,
			quotedMessageID,
			content,
			extraRaw,
		),
		allowContinuation,
	)
}

func resolveLiveGroupDispatchSemanticsWithNormalization(
	ctx context.Context,
	sessionID string,
	senderID int64,
	senderType int16,
	quotedMessageID int64,
	content string,
	extraRaw json.RawMessage,
	normalization groupMentionNormalization,
	allowContinuation bool,
) (groupDispatchSemantics, error) {
	semantics := groupDispatchSemantics{
		MentionUserIDs:         normalization.MentionUserIDs,
		ExplicitMentionUserIDs: normalization.ExplicitMentionUserIDs,
		ExplicitMentionAll:     normalization.MentionAll,
	}
	if hasExplicitGroupMentionTargets(normalization) {
		semantics.TargetUserIDs = append([]int64(nil), semantics.MentionUserIDs...)
		return semantics, nil
	}

	if allowContinuation && supportsGroupMentionAllContinuationSenderType(senderType) && senderID > 0 {
		continuation, err := loadGroupMentionAllContinuation(ctx, sessionID, senderType, senderID)
		if err != nil {
			return semantics, err
		}
		if len(continuation.TargetUserIDs) > 0 {
			// @所有人 的延续同样是"接话"，不是明确 @：仅@触发的 agent 不能靠上一轮
			// 的 @所有人 被永久续上，否则等同于把它变成了 ModeAll。
			continuableTargetUserIDs, filterErr := excludeMentionOnlyAgentTargets(sessionID, continuation.TargetUserIDs)
			if filterErr != nil {
				return semantics, filterErr
			}
			if len(continuableTargetUserIDs) > 0 {
				semantics.MentionUserIDs = nil
				semantics.ExplicitMentionUserIDs = nil
				semantics.TargetUserIDs = continuableTargetUserIDs
				semantics.ContinuedMentionAll = true
				semantics.Continued = true
				return semantics, nil
			}
		}
	}

	if len(semantics.MentionUserIDs) > 0 {
		semantics.TargetUserIDs = append([]int64(nil), semantics.MentionUserIDs...)
		return semantics, nil
	}

	if allowContinuation && senderType == 1 && shouldSuppressGroupContinuation(content) {
		semantics.SuppressContinuation = true
	}

	if allowContinuation && senderType == 1 && senderID > 0 && !semantics.SuppressContinuation {
		targetUserIDs, err := loadGroupContinuationTargetIDs(ctx, sessionID, senderID)
		if err != nil {
			return semantics, err
		}
		if len(targetUserIDs) > 0 {
			if !isGroupContinuationStillConnected(sessionID, senderID, targetUserIDs) {
				_ = clearGroupContinuationTargetIDs(ctx, sessionID, senderID)
			} else {
				// 一对一接话同样不能落到仅@触发的 agent 身上，否则它回一次话就把
				// 自己钉死成对方的固定接话对象，永远不用再被明确 @。
				continuableTargetUserIDs, filterErr := excludeMentionOnlyAgentTargets(sessionID, targetUserIDs)
				if filterErr != nil {
					return semantics, filterErr
				}
				if len(continuableTargetUserIDs) > 0 {
					semantics.TargetUserIDs = continuableTargetUserIDs
					semantics.Continued = true
					return semantics, nil
				}
			}
		}
	}

	if allowContinuation && senderType == 1 && !semantics.SuppressContinuation {
		targetUserIDs, err := loadLastAgentContinuationTarget(sessionID)
		if err != nil {
			return semantics, err
		}
		if len(targetUserIDs) > 0 {
			semantics.TargetUserIDs = targetUserIDs
			semantics.Continued = true
			return semantics, nil
		}
	}

	coldStart, err := shouldTriggerGroupColdStart(ctx, sessionID, senderType, quotedMessageID)
	if err != nil {
		return semantics, err
	}
	semantics.ColdStart = coldStart
	return semantics, nil
}

func resolvePersistedGroupDispatchSemantics(
	ctx context.Context,
	sessionID string,
	senderID int64,
	senderType int16,
	triggerMsgID int64,
	quotedMessageID int64,
	content string,
	extraRaw json.RawMessage,
) (groupDispatchSemantics, error) {
	normalization := resolveGroupMentionDispatchNormalization(
		ctx,
		sessionID,
		senderID,
		senderType,
		quotedMessageID,
		content,
		extraRaw,
	)
	semantics := groupDispatchSemantics{
		MentionUserIDs:         normalization.MentionUserIDs,
		ExplicitMentionUserIDs: normalization.ExplicitMentionUserIDs,
		ExplicitMentionAll:     normalization.MentionAll,
	}
	if len(semantics.MentionUserIDs) > 0 {
		semantics.TargetUserIDs = append([]int64(nil), semantics.MentionUserIDs...)
		return semantics, nil
	}
	if !supportsGroupMentionAllContinuationSenderType(senderType) || senderID <= 0 || triggerMsgID <= 0 {
		return semantics, nil
	}

	snapshot, err := loadGroupMessageTargetSnapshot(ctx, sessionID, triggerMsgID)
	if err != nil {
		return semantics, err
	}
	semantics.TargetUserIDs = snapshot.TargetUserIDs
	semantics.Continued = len(snapshot.TargetUserIDs) > 0
	semantics.ColdStart = snapshot.ColdStart
	return semantics, nil
}

func recordGroupDispatchSemantics(
	ctx context.Context,
	sessionID string,
	senderID int64,
	senderType int16,
	triggerMsgID int64,
	semantics groupDispatchSemantics,
) error {
	if senderID <= 0 || sessionID == "" {
		return nil
	}

	if supportsGroupMentionAllContinuationSenderType(senderType) {
		if semantics.ExplicitMentionAll && len(semantics.TargetUserIDs) > 0 {
			if err := storeGroupMentionAllContinuation(ctx, sessionID, senderType, senderID, semantics.TargetUserIDs); err != nil {
				return err
			}
		} else if semantics.ContinuedMentionAll {
			if err := clearGroupMentionAllContinuation(ctx, sessionID, senderType, senderID); err != nil {
				return err
			}
		} else if len(semantics.ExplicitMentionUserIDs) > 0 {
			if err := clearGroupMentionAllContinuation(ctx, sessionID, senderType, senderID); err != nil {
				return err
			}
		}
	}

	if senderType != 1 {
		if (semantics.Continued || semantics.ColdStart) && triggerMsgID > 0 {
			return storeGroupMessageTargetSnapshot(ctx, sessionID, triggerMsgID, semantics.TargetUserIDs, semantics.ColdStart)
		}
		return nil
	}

	if semantics.SuppressContinuation {
		return clearGroupContinuationTargetIDs(ctx, sessionID, senderID)
	}
	if !semantics.ExplicitMentionAll && !semantics.ContinuedMentionAll && len(semantics.TargetUserIDs) == 1 {
		if err := storeGroupContinuationTargetIDs(ctx, sessionID, senderID, semantics.TargetUserIDs); err != nil {
			return err
		}
	} else {
		if err := clearGroupContinuationTargetIDs(ctx, sessionID, senderID); err != nil {
			return err
		}
	}
	if (semantics.Continued || semantics.ColdStart) && triggerMsgID > 0 {
		return storeGroupMessageTargetSnapshot(ctx, sessionID, triggerMsgID, semantics.TargetUserIDs, semantics.ColdStart)
	}
	return nil
}

func resolveGroupMentionDispatchNormalization(
	ctx context.Context,
	sessionID string,
	senderID int64,
	senderType int16,
	quotedMessageID int64,
	content string,
	extraRaw json.RawMessage,
) groupMentionNormalization {
	viewerUserID := int64(0)
	if senderType == 1 && senderID > 0 {
		viewerUserID = senderID
	}

	effectiveQuotedMsgID := quotedMessageID
	if senderType == 1 {
		// A human message breaks any pending agent-to-agent auto quote chain.
		resetAgentAutoLoopChain(ctx, sessionID)
	} else if senderType == 2 && quotedMessageID > 0 {
		quotedOwnerID, quotedOwnerType := ResolveQuotedMessageOwnerAndType(sessionID, quotedMessageID)
		if quotedOwnerType == 2 && quotedOwnerID > 0 && quotedOwnerID != senderID {
			// Agent quoting another agent's message: only let it imply
			// @mentioning the quoted agent while the auto-chain stays under
			// the loop-safety cap. Past the cap, quoting stops triggering the
			// other agent automatically — an explicit @mention (from a human
			// or the agent itself) is required to resume, which bounds
			// unattended agent-to-agent ping-pong without blocking normal
			// multi-turn agent collaboration (e.g. reporting results back).
			count, err := incrAgentAutoLoopChain(ctx, sessionID)
			if err != nil || count > agentAutoLoopChainCap {
				effectiveQuotedMsgID = 0
			}
		}
	}
	return resolveGroupMentionNormalization(
		sessionID,
		viewerUserID,
		senderID,
		content,
		effectiveQuotedMsgID,
		extraRaw,
	)
}

func agentAutoLoopChainKey(sessionID string) string {
	return fmt.Sprintf("im:group_target:agent_loop_chain:%s", sessionID)
}

// incrAgentAutoLoopChain increments the session's consecutive agent-to-agent
// auto quote-chain counter and refreshes its TTL. Returns (0, nil) when Redis
// is unavailable, matching this file's other store.RDB-gated helpers.
func incrAgentAutoLoopChain(ctx context.Context, sessionID string) (int64, error) {
	if store.RDB == nil || sessionID == "" {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := agentAutoLoopChainKey(sessionID)
	count, err := store.RDB.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	store.RDB.Expire(ctx, key, agentAutoLoopChainTTL)
	return count, nil
}

func resetAgentAutoLoopChain(ctx context.Context, sessionID string) {
	if store.RDB == nil || sessionID == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	store.RDB.Del(ctx, agentAutoLoopChainKey(sessionID))
}

func supportsGroupMentionAllContinuationSenderType(senderType int16) bool {
	return senderType == 1 || senderType == 2
}

func hasExplicitGroupMentionTargets(normalization groupMentionNormalization) bool {
	return normalization.MentionAll || normalization.HasExplicitMentions
}

func isGroupContinuationStillConnected(sessionID string, senderID int64, targetUserIDs []int64) bool {
	if sessionID == "" || senderID <= 0 || len(targetUserIDs) != 1 {
		return false
	}

	lastSenderID, err := loadLastSessionMessageSenderID(sessionID)
	if err != nil || lastSenderID <= 0 {
		return false
	}
	if lastSenderID == senderID {
		return true
	}
	return containsInt64(targetUserIDs, lastSenderID)
}

// loadLastAgentContinuationTarget is the bottom-of-the-ladder continuation
// fallback: nobody was @-mentioned and there is no directed 1:1 or
// @所有人 continuation, so an un-@'d message is offered to whichever agent
// spoke last. If the group has no active human message, the agent is not
// required. If the last speaker is ModeMentionOnly, it must not be picked up
// this way — that is exactly the customer-reported lock-in (a mention-only
// agent replies once, becomes "the last speaker", and every subsequent un-@'d
// message keeps landing back on it forever). Instead this walks back through
// recent messages (bounded by lastAgentContinuationLookback) for the nearest
// prior agent speaker that is NOT ModeMentionOnly, e.g. a normal-mode agent
// also in the group. If none is found within the lookback window, it returns
// no target — silently not dispatching is acceptable here, since the human
// deliberately limited that agent to explicit @mentions and there is no other
// safe target to guess.
func loadLastAgentContinuationTarget(sessionID string) ([]int64, error) {
	meta, ok, err := loadLastSessionMessageMeta(sessionID)
	if err != nil || !ok {
		return nil, err
	}
	if meta.SenderType != 2 || meta.SenderID <= 0 {
		return nil, nil
	}
	return selectContinuableAgentTarget(sessionID, meta.MsgID)
}

// selectContinuableAgentTarget scans up to lastAgentContinuationLookback of
// the most recent messages at or before uptoMsgID (newest first) and returns
// the nearest agent sender whose current receive mode is not ModeMentionOnly.
func selectContinuableAgentTarget(sessionID string, uptoMsgID int64) ([]int64, error) {
	rows, err := loadRecentSessionMessageSenders(sessionID, uptoMsgID, lastAgentContinuationLookback)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	candidateAgentIDs := make([]int64, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		if row.SenderType != 2 || row.SenderID <= 0 {
			continue
		}
		if _, ok := seen[row.SenderID]; ok {
			continue
		}
		seen[row.SenderID] = struct{}{}
		candidateAgentIDs = append(candidateAgentIDs, row.SenderID)
	}
	if len(candidateAgentIDs) == 0 {
		return nil, nil
	}

	modes, err := loadAgentReceiveModes(sessionID, candidateAgentIDs)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.SenderType != 2 || row.SenderID <= 0 {
			continue
		}
		mode, _ := agentreceive.Normalize(modes[row.SenderID], 0)
		if mode != agentreceive.ModeMentionOnly {
			return []int64{row.SenderID}, nil
		}
	}
	return nil, nil
}

// loadAgentReceiveModes batch-loads the current agent_receive_mode for the
// given agent (member_type=2) member IDs in sessionID. IDs that are not
// agent members of the session (e.g. a human ID mixed into the caller's
// list) are simply absent from the returned map.
func loadAgentReceiveModes(sessionID string, memberIDs []int64) (map[int64]int16, error) {
	if sessionID == "" || len(memberIDs) == 0 {
		return nil, nil
	}
	var rows []struct {
		MemberID         int64 `gorm:"column:member_id"`
		AgentReceiveMode int16 `gorm:"column:agent_receive_mode"`
	}
	if err := store.DB.Table("session_members").
		Select("member_id, agent_receive_mode").
		Where("session_id = ? AND member_type = 2 AND member_id IN ?", sessionID, memberIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	modes := make(map[int64]int16, len(rows))
	for _, row := range rows {
		modes[row.MemberID] = row.AgentReceiveMode
	}
	return modes, nil
}

// excludeMentionOnlyAgentTargets drops any ModeMentionOnly agent from a
// resolved continuation target list. Non-agent targets (humans) and agents
// that are not ModeMentionOnly pass through unchanged.
func excludeMentionOnlyAgentTargets(sessionID string, targetUserIDs []int64) ([]int64, error) {
	if len(targetUserIDs) == 0 {
		return targetUserIDs, nil
	}
	modes, err := loadAgentReceiveModes(sessionID, targetUserIDs)
	if err != nil {
		return nil, err
	}
	if len(modes) == 0 {
		return targetUserIDs, nil
	}
	filtered := make([]int64, 0, len(targetUserIDs))
	for _, id := range targetUserIDs {
		if mode, isAgent := modes[id]; isAgent {
			normalizedMode, _ := agentreceive.Normalize(mode, 0)
			if normalizedMode == agentreceive.ModeMentionOnly {
				continue
			}
		}
		filtered = append(filtered, id)
	}
	return filtered, nil
}

func loadLastSessionMessageSenderID(sessionID string) (int64, error) {
	meta, ok, err := loadLastSessionMessageMeta(sessionID)
	if err != nil || !ok {
		return 0, err
	}
	return meta.SenderID, nil
}

func loadLastSessionMessageMeta(sessionID string) (lastSessionMessageMeta, bool, error) {
	if sessionID == "" {
		return lastSessionMessageMeta{}, false, nil
	}

	var sessionRow struct {
		LastMsgID int64 `gorm:"column:last_msg_id"`
	}
	if err := store.DB.Model(&model.Session{}).
		Select("last_msg_id").
		Where("session_id = ?", sessionID).
		Take(&sessionRow).Error; err != nil {
		return lastSessionMessageMeta{}, false, err
	}
	if sessionRow.LastMsgID <= 0 {
		return lastSessionMessageMeta{}, false, nil
	}

	var meta lastSessionMessageMeta
	if err := store.DB.Model(&model.Message{}).
		Select("msg_id", "sender_id", "sender_type", "created_at").
		Where("session_id = ? AND msg_id = ?", sessionID, sessionRow.LastMsgID).
		Take(&meta).Error; err != nil {
		return lastSessionMessageMeta{}, false, err
	}
	if meta.MsgID <= 0 {
		return lastSessionMessageMeta{}, false, nil
	}
	return meta, true, nil
}

// loadRecentSessionMessageSenders returns up to limit of the most recent
// messages in sessionID at or before uptoMsgID, newest first. Used by the
// last-agent continuation fallback to walk back past a ModeMentionOnly
// speaker (see selectContinuableAgentTarget).
func loadRecentSessionMessageSenders(sessionID string, uptoMsgID int64, limit int) ([]lastSessionMessageMeta, error) {
	if sessionID == "" || uptoMsgID <= 0 || limit <= 0 {
		return nil, nil
	}
	var rows []lastSessionMessageMeta
	if err := store.DB.Model(&model.Message{}).
		Select("msg_id", "sender_id", "sender_type").
		Where("session_id = ? AND msg_id <= ?", sessionID, uptoMsgID).
		Order("msg_id DESC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func groupContinuationKey(sessionID string, senderID int64) string {
	return fmt.Sprintf("im:group_target:continue:%s:%d", sessionID, senderID)
}

func groupMessageTargetSnapshotKey(sessionID string, triggerMsgID int64) string {
	return fmt.Sprintf("im:group_target:msg:%s:%d", sessionID, triggerMsgID)
}

func groupMentionAllContinuationKey(sessionID string, senderType int16, senderID int64) string {
	return fmt.Sprintf("im:group_target:mention_all:%s:%d:%d", sessionID, senderType, senderID)
}

func loadGroupContinuationTargetIDs(ctx context.Context, sessionID string, senderID int64) ([]int64, error) {
	if store.RDB == nil || sessionID == "" || senderID <= 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := store.RDB.Get(ctx, groupContinuationKey(sessionID, senderID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snapshot, err := unmarshalGroupTargetSnapshot(raw)
	if err != nil {
		return nil, err
	}
	return snapshot.TargetUserIDs, nil
}

func loadGroupMentionAllContinuation(ctx context.Context, sessionID string, senderType int16, senderID int64) (groupMentionAllContinuation, error) {
	if store.RDB == nil || sessionID == "" || senderID <= 0 {
		return groupMentionAllContinuation{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := store.RDB.Get(ctx, groupMentionAllContinuationKey(sessionID, senderType, senderID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return groupMentionAllContinuation{}, nil
	}
	if err != nil {
		return groupMentionAllContinuation{}, err
	}
	return unmarshalGroupMentionAllContinuation(raw)
}

func storeGroupContinuationTargetIDs(ctx context.Context, sessionID string, senderID int64, targetUserIDs []int64) error {
	if store.RDB == nil || sessionID == "" || senderID <= 0 || len(targetUserIDs) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := marshalGroupTargetSnapshot(targetUserIDs, false)
	if err != nil {
		return err
	}
	return store.RDB.Set(ctx, groupContinuationKey(sessionID, senderID), raw, groupContinuationTTL).Err()
}

func storeGroupMentionAllContinuation(
	ctx context.Context,
	sessionID string,
	senderType int16,
	senderID int64,
	targetUserIDs []int64,
) error {
	if store.RDB == nil || sessionID == "" || senderID <= 0 || len(targetUserIDs) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := marshalGroupMentionAllContinuation(targetUserIDs)
	if err != nil {
		return err
	}
	return store.RDB.Set(ctx, groupMentionAllContinuationKey(sessionID, senderType, senderID), raw, groupMentionAllContinuationTTL).Err()
}

func clearGroupContinuationTargetIDs(ctx context.Context, sessionID string, senderID int64) error {
	if store.RDB == nil || sessionID == "" || senderID <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return store.RDB.Del(ctx, groupContinuationKey(sessionID, senderID)).Err()
}

func clearGroupMentionAllContinuation(ctx context.Context, sessionID string, senderType int16, senderID int64) error {
	if store.RDB == nil || sessionID == "" || senderID <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return store.RDB.Del(ctx, groupMentionAllContinuationKey(sessionID, senderType, senderID)).Err()
}

func loadGroupMessageTargetSnapshot(ctx context.Context, sessionID string, triggerMsgID int64) (groupTargetSnapshot, error) {
	if store.RDB == nil || sessionID == "" || triggerMsgID <= 0 {
		return groupTargetSnapshot{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := store.RDB.Get(ctx, groupMessageTargetSnapshotKey(sessionID, triggerMsgID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return groupTargetSnapshot{}, nil
	}
	if err != nil {
		return groupTargetSnapshot{}, err
	}
	return unmarshalGroupTargetSnapshot(raw)
}

func storeGroupMessageTargetSnapshot(ctx context.Context, sessionID string, triggerMsgID int64, targetUserIDs []int64, coldStart bool) error {
	if store.RDB == nil || sessionID == "" || triggerMsgID <= 0 || (len(targetUserIDs) == 0 && !coldStart) {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := marshalGroupTargetSnapshot(targetUserIDs, coldStart)
	if err != nil {
		return err
	}
	return store.RDB.Set(ctx, groupMessageTargetSnapshotKey(sessionID, triggerMsgID), raw, groupMessageTargetSnapshotTTL).Err()
}

func marshalGroupTargetSnapshot(targetUserIDs []int64, coldStart bool) ([]byte, error) {
	return json.Marshal(groupTargetSnapshot{
		TargetUserIDs: dedupePositiveTargetUserIDs(targetUserIDs),
		ColdStart:     coldStart,
	})
}

func marshalGroupMentionAllContinuation(targetUserIDs []int64) ([]byte, error) {
	return json.Marshal(groupMentionAllContinuation{
		TargetUserIDs: dedupePositiveTargetUserIDs(targetUserIDs),
	})
}

func unmarshalGroupTargetSnapshot(raw []byte) (groupTargetSnapshot, error) {
	if len(raw) == 0 {
		return groupTargetSnapshot{}, nil
	}
	var snapshot groupTargetSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return groupTargetSnapshot{}, err
	}
	snapshot.TargetUserIDs = dedupePositiveTargetUserIDs(snapshot.TargetUserIDs)
	return snapshot, nil
}

func unmarshalGroupMentionAllContinuation(raw []byte) (groupMentionAllContinuation, error) {
	if len(raw) == 0 {
		return groupMentionAllContinuation{}, nil
	}
	var snapshot groupMentionAllContinuation
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return groupMentionAllContinuation{}, err
	}
	snapshot.TargetUserIDs = dedupePositiveTargetUserIDs(snapshot.TargetUserIDs)
	if len(snapshot.TargetUserIDs) == 0 {
		return groupMentionAllContinuation{}, nil
	}
	return snapshot, nil
}

func shouldTriggerGroupColdStart(
	ctx context.Context,
	sessionID string,
	senderType int16,
	quotedMessageID int64,
) (bool, error) {
	if sessionID == "" || senderType != 1 || quotedMessageID > 0 {
		return false, nil
	}

	lastMessageAt, ok, err := loadLastSessionMessageCreatedAt(ctx, sessionID)
	if err != nil {
		return false, err
	}
	if !ok {
		// A brand-new group has no previous message to compare against. Treat the
		// first human message as a cold start so normal-mode agents get an initial
		// processing turn instead of only a record-only mirror.
		return true, nil
	}
	return time.Since(lastMessageAt) >= groupColdStartIdleThreshold, nil
}

func loadLastSessionMessageCreatedAt(ctx context.Context, sessionID string) (time.Time, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	meta, ok, err := loadLastSessionMessageMeta(sessionID)
	if err != nil {
		return time.Time{}, false, err
	}
	if !ok || meta.CreatedAt.IsZero() {
		return time.Time{}, false, nil
	}
	return meta.CreatedAt.UTC(), true, nil
}

func dedupePositiveTargetUserIDs(src []int64) []int64 {
	if len(src) == 0 {
		return nil
	}

	seen := make(map[int64]struct{}, len(src))
	dst := make([]int64, 0, len(src))
	for _, id := range src {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		dst = append(dst, id)
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

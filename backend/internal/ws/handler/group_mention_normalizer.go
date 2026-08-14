package handler

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/pkg/mention"
	"github.com/askie/grix/backend/internal/store"
)

const mentionAllExtraKey = "mention_all"
const explicitMentionExtraKey = "explicit_mention_user_ids"
const mentionAllToken = "所有人"

type groupMentionNormalization struct {
	MentionUserIDs         []int64
	ExplicitMentionUserIDs []int64
	HasExplicitMentions    bool
	MentionAll             bool
	ExtraRaw               json.RawMessage
}

func NormalizeGroupMentionExtra(
	sessionID string,
	viewerUserID int64,
	senderID int64,
	content string,
	quotedMessageID int64,
	extraRaw json.RawMessage,
) json.RawMessage {
	return resolveGroupMentionNormalization(
		sessionID,
		viewerUserID,
		senderID,
		content,
		quotedMessageID,
		extraRaw,
	).ExtraRaw
}

func resolveGroupMentionNormalization(
	sessionID string,
	viewerUserID int64,
	senderID int64,
	content string,
	quotedMessageID int64,
	extraRaw json.RawMessage,
) groupMentionNormalization {
	sanitizedExtra, mentionAll := splitMentionAll(extraRaw, content)
	explicitMentionUserIDs, hasExplicitMentions := loadExplicitMentionUserIDs(sanitizedExtra, content)
	quotedMentionOwnerID := ResolveQuotedMessageOwnerID(sessionID, quotedMessageID)
	shouldNormalize := mentionAll || mention.ShouldNormalize(
		sanitizedExtra,
		content,
		quotedMentionOwnerID,
	)
	if !shouldNormalize {
		hasExplicitTargeting := mentionAll || len(explicitMentionUserIDs) > 0
		extraWithExplicit := writeExplicitMentionUserIDs(sanitizedExtra, explicitMentionUserIDs)
		return groupMentionNormalization{
			MentionUserIDs:         mention.ParseUserIDs(sanitizedExtra, content),
			ExplicitMentionUserIDs: explicitMentionUserIDs,
			HasExplicitMentions:    hasExplicitTargeting,
			MentionAll:             mentionAll,
			ExtraRaw:               extraWithExplicit,
		}
	}

	mentionCandidates := ResolveMentionCandidatesForSession(sessionID, viewerUserID)
	if !hasExplicitMentions {
		explicitExtra := mention.NormalizeExtraWithCandidates(
			sanitizedExtra,
			content,
			mentionCandidates,
		)
		explicitMentionUserIDs = mention.ParseUserIDs(explicitExtra, content)
		if mentionAll {
			explicitMentionUserIDs = append(
				explicitMentionUserIDs,
				resolveMentionAllOtherMemberIDs(sessionID, senderID)...,
			)
			explicitMentionUserIDs = dedupePositiveTargetUserIDs(explicitMentionUserIDs)
		}
	}
	hasExplicitTargeting := mentionAll || len(explicitMentionUserIDs) > 0
	var normalizedExtra json.RawMessage
	if mentionAll {
		normalizedExtra = mention.NormalizeExtraWithCandidates(
			sanitizedExtra,
			content,
			mentionCandidates,
		)
	} else {
		normalizedExtra = mention.NormalizeExtraWithCandidates(
			sanitizedExtra,
			content,
			mentionCandidates,
			quotedMentionOwnerID,
		)
	}

	mentionUserIDs := mention.ParseUserIDs(normalizedExtra, content)
	if mentionAll {
		mentionUserIDs = append(
			mentionUserIDs,
			resolveMentionAllOtherMemberIDs(sessionID, senderID)...,
		)
		mentionUserIDs = dedupePositiveTargetUserIDs(mentionUserIDs)
		normalizedExtra = writeCanonicalMentionUserIDs(normalizedExtra, mentionUserIDs)
	}
	if len(explicitMentionUserIDs) == 0 && len(mentionUserIDs) > 0 {
		explicitMentionUserIDs = append([]int64(nil), mentionUserIDs...)
	}
	normalizedExtra = writeExplicitMentionUserIDs(normalizedExtra, explicitMentionUserIDs)

	return groupMentionNormalization{
		MentionUserIDs:         mentionUserIDs,
		ExplicitMentionUserIDs: explicitMentionUserIDs,
		HasExplicitMentions:    hasExplicitTargeting,
		MentionAll:             mentionAll,
		ExtraRaw:               normalizedExtra,
	}
}

func splitMentionAll(extraRaw json.RawMessage, content string) (json.RawMessage, bool) {
	sanitizedExtra, mentionAll := splitMentionAllExtra(extraRaw)
	if mentionAll {
		return sanitizedExtra, true
	}
	return sanitizedExtra, mention.ContainsMentionToken(content, mentionAllToken)
}

func splitMentionAllExtra(extraRaw json.RawMessage) (json.RawMessage, bool) {
	if len(extraRaw) == 0 || !bytes.Contains(extraRaw, []byte(mentionAllExtraKey)) {
		return cloneRawJSON(extraRaw), false
	}

	var extra map[string]any
	if err := json.Unmarshal(extraRaw, &extra); err != nil {
		return cloneRawJSON(extraRaw), false
	}

	mentionAll := parseMentionAllValue(extra[mentionAllExtraKey])
	delete(extra, mentionAllExtraKey)
	if len(extra) == 0 {
		return nil, mentionAll
	}

	merged, err := json.Marshal(extra)
	if err != nil {
		return cloneRawJSON(extraRaw), mentionAll
	}
	return json.RawMessage(merged), mentionAll
}

func parseMentionAllValue(raw any) bool {
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return false
		}
		if parsed, err := strconv.ParseBool(trimmed); err == nil {
			return parsed
		}
		return trimmed == "1"
	case float64:
		return value != 0
	default:
		return false
	}
}

func resolveMentionAllOtherMemberIDs(sessionID string, senderID int64) []int64 {
	if sessionID == "" {
		return nil
	}

	var rows []struct {
		MemberID int64 `gorm:"column:member_id"`
	}
	if err := store.DB.Table("session_members").
		Select("member_id").
		Where("session_id = ? AND member_type IN ? AND member_id != ?", sessionID, []int16{1, 2}, senderID).
		Order("joined_at ASC, member_id ASC").
		Scan(&rows).Error; err != nil {
		return nil
	}
	if len(rows) == 0 {
		return nil
	}

	memberIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		memberIDs = append(memberIDs, row.MemberID)
	}
	return dedupePositiveTargetUserIDs(memberIDs)
}

func int64SliceToStringSlice(ids []int64) []string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.FormatInt(id, 10)
	}
	return s
}

func writeCanonicalMentionUserIDs(extraRaw json.RawMessage, mentionUserIDs []int64) json.RawMessage {
	normalizedMentions := dedupePositiveTargetUserIDs(mentionUserIDs)
	if len(extraRaw) == 0 {
		if len(normalizedMentions) == 0 {
			return nil
		}
		merged, err := json.Marshal(map[string]any{
			"mention_user_ids": int64SliceToStringSlice(normalizedMentions),
		})
		if err != nil {
			return nil
		}
		return json.RawMessage(merged)
	}

	var extra map[string]any
	if err := json.Unmarshal(extraRaw, &extra); err != nil {
		return cloneRawJSON(extraRaw)
	}
	if len(normalizedMentions) == 0 {
		delete(extra, "mention_user_ids")
	} else {
		extra["mention_user_ids"] = int64SliceToStringSlice(normalizedMentions)
	}
	if len(extra) == 0 {
		return nil
	}

	merged, err := json.Marshal(extra)
	if err != nil {
		return cloneRawJSON(extraRaw)
	}
	return json.RawMessage(merged)
}

func loadExplicitMentionUserIDs(extraRaw json.RawMessage, content string) ([]int64, bool) {
	if len(extraRaw) == 0 {
		return nil, false
	}

	var extra map[string]any
	if err := json.Unmarshal(extraRaw, &extra); err != nil {
		return mention.ParseUserIDs(extraRaw, content), false
	}
	if raw, ok := extra[explicitMentionExtraKey]; ok {
		return parseMentionIDList(raw), true
	}
	return mention.ParseUserIDs(extraRaw, content), false
}

func writeExplicitMentionUserIDs(extraRaw json.RawMessage, mentionUserIDs []int64) json.RawMessage {
	normalizedMentions := dedupePositiveTargetUserIDs(mentionUserIDs)
	if len(extraRaw) == 0 {
		if len(normalizedMentions) == 0 {
			return nil
		}
		merged, err := json.Marshal(map[string]any{
			explicitMentionExtraKey: int64SliceToStringSlice(normalizedMentions),
		})
		if err != nil {
			return nil
		}
		return json.RawMessage(merged)
	}

	var extra map[string]any
	if err := json.Unmarshal(extraRaw, &extra); err != nil {
		return cloneRawJSON(extraRaw)
	}
	if len(normalizedMentions) == 0 {
		delete(extra, explicitMentionExtraKey)
	} else {
		extra[explicitMentionExtraKey] = int64SliceToStringSlice(normalizedMentions)
	}
	if len(extra) == 0 {
		return nil
	}

	merged, err := json.Marshal(extra)
	if err != nil {
		return cloneRawJSON(extraRaw)
	}
	return json.RawMessage(merged)
}

func stripGroupMentionExtra(extraRaw json.RawMessage) json.RawMessage {
	extraWithoutMentions := mention.RemoveMentionUserIDs(extraRaw)
	if len(extraWithoutMentions) == 0 {
		return nil
	}

	var extra map[string]any
	if err := json.Unmarshal(extraWithoutMentions, &extra); err != nil {
		return cloneRawJSON(extraWithoutMentions)
	}
	delete(extra, explicitMentionExtraKey)
	if len(extra) == 0 {
		return nil
	}

	merged, err := json.Marshal(extra)
	if err != nil {
		return cloneRawJSON(extraWithoutMentions)
	}
	return json.RawMessage(merged)
}

func parseMentionIDList(raw any) []int64 {
	values, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]interface{}); ok {
			values = typed
		} else {
			return nil
		}
	}

	ids := make([]int64, 0, len(values))
	for _, value := range values {
		switch typed := value.(type) {
		case float64:
			if typed > 0 {
				ids = append(ids, int64(typed))
			}
		case int64:
			if typed > 0 {
				ids = append(ids, typed)
			}
		case int:
			if typed > 0 {
				ids = append(ids, int64(typed))
			}
		case string:
			trimmed := strings.TrimSpace(typed)
			if trimmed == "" {
				continue
			}
			parsed, err := strconv.ParseInt(trimmed, 10, 64)
			if err == nil && parsed > 0 {
				ids = append(ids, parsed)
			}
		}
	}
	return dedupePositiveTargetUserIDs(ids)
}

func cloneRawJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	cloned := make([]byte, len(raw))
	copy(cloned, raw)
	return json.RawMessage(cloned)
}

// mergeMentionAndVisibleTo merges visible_to user IDs into the existing mention
// list, ensuring visible_to members are always treated as @mentioned targets.
func mergeMentionAndVisibleTo(mentionUserIDs, visibleTo []int64) []int64 {
	merged := make([]int64, 0, len(mentionUserIDs)+len(visibleTo))
	merged = append(merged, mentionUserIDs...)
	merged = append(merged, visibleTo...)
	return dedupePositiveTargetUserIDs(merged)
}

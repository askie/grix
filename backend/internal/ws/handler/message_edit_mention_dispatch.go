package handler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/mention"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm/clause"
)

// DispatchMessageEditMentionAdditions delivers an edited message's newly
// added @mentions the same way a brand-new message's mentions would be
// delivered — but only the members whose mention is new in this edit, and
// each (msg_id, member) pair fires at most once ever, even across repeated
// add/remove/add edits on the same message (see .agents/notes for why: a
// lead repeatedly tweaking a flowchart must not re-wake the same agent on
// every save).
//
// Group sessions only: private sessions strip mention_user_ids entirely
// (resolveGroupMentionNormalization's non-group branch), so there is no
// mention set to diff there — this is a no-op for any session that is not a
// group.
//
// Newly mentioned humans need no action here: they are already session
// members who received the message at send time, and the edit-sync push
// already carries the updated extra.mention_user_ids for client-side
// highlighting (see message_edit_service.go's buildMessageEditPayload).
// Only newly mentioned agents need an explicit delivery, because agent event
// routing — unlike the human inbox sync — is not part of the edit-sync path.
//
// hub may be nil (e.g. an HTTP-originated edit has no WS hub on hand):
// dispatchDirectSessionRoute's hub usage already nil-guards, and this
// function never fans out to agents' mirror targets, which would otherwise
// re-notify agents unrelated to this edit's new mention.
func DispatchMessageEditMentionAdditions(
	hub HubInterface,
	ctx context.Context,
	sessionID string,
	editorMemberID int64,
	editorMemberType int16,
	msgID int64,
	quotedMessageID int64,
	msgType int16,
	oldContent string,
	oldExtraRaw json.RawMessage,
	newContent string,
	newExtraRaw json.RawMessage,
) {
	if sessionID == "" || msgID <= 0 {
		return
	}
	if loadSessionType(sessionID) != 2 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	candidates := ResolveMentionCandidatesForSession(sessionID, editorMemberID)
	oldMentions := mention.ParseUserIDsWithCandidates(oldExtraRaw, oldContent, candidates)
	newMentions := mention.ParseUserIDsWithCandidates(newExtraRaw, newContent, candidates)
	added := newlyAddedMentionIDs(newMentions, oldMentions)
	if len(added) == 0 {
		return
	}

	memberTypes := loadSessionMemberTypes(sessionID, added)
	addedAgentIDs := make([]int64, 0, len(added))
	for _, id := range added {
		if memberTypes[id] == 2 {
			addedAgentIDs = append(addedAgentIDs, id)
		}
	}
	if len(addedAgentIDs) == 0 {
		return
	}

	toDispatch := make([]int64, 0, len(addedAgentIDs))
	for _, agentID := range addedAgentIDs {
		if claimMessageMentionDispatchReceipt(sessionID, msgID, agentID) {
			toDispatch = append(toDispatch, agentID)
		}
	}
	if len(toDispatch) == 0 {
		return
	}

	semantics := &groupDispatchSemantics{
		MentionUserIDs:         toDispatch,
		ExplicitMentionUserIDs: toDispatch,
		TargetUserIDs:          toDispatch,
	}
	route, err := resolveDirectSessionRoute(
		sessionID,
		2,
		editorMemberID,
		editorMemberType,
		msgID,
		quotedMessageID,
		msgType,
		newContent,
		newExtraRaw,
		semantics,
		nil,
		nil,
		false,
	)
	if err != nil {
		logger.L.Warnf(
			"resolve edit mention dispatch route failed session=%s msg=%d added=%v: %v",
			sessionID, msgID, toDispatch, err,
		)
		return
	}
	if route == nil {
		return
	}
	// Only the newly mentioned agent(s) should hear about this edit: drop any
	// mirror fan-out to unrelated API agents in the group that would
	// otherwise re-record a message they can already see.
	route.MirrorTargets = nil

	dispatchDirectSessionRoute(
		hub,
		ctx,
		sessionID,
		2,
		editorMemberID,
		editorMemberType,
		msgID,
		quotedMessageID,
		msgType,
		newContent,
		newExtraRaw,
		route,
		true,
	)
}

// newlyAddedMentionIDs returns the members present in newIDs but not oldIDs.
// Both inputs are already deduped positive IDs (mention.ParseUserIDsWithCandidates
// guarantees this).
func newlyAddedMentionIDs(newIDs, oldIDs []int64) []int64 {
	if len(newIDs) == 0 {
		return nil
	}
	old := make(map[int64]struct{}, len(oldIDs))
	for _, id := range oldIDs {
		old[id] = struct{}{}
	}
	added := make([]int64, 0, len(newIDs))
	for _, id := range newIDs {
		if id <= 0 {
			continue
		}
		if _, ok := old[id]; ok {
			continue
		}
		added = append(added, id)
	}
	return added
}

// loadSessionMemberTypes batch-resolves member_type for a set of member IDs
// in a session, so mention targets can be split into agents (member_type=2,
// which need explicit dispatch) and humans (which don't, see the doc comment
// on DispatchMessageEditMentionAdditions).
func loadSessionMemberTypes(sessionID string, memberIDs []int64) map[int64]int16 {
	if sessionID == "" || len(memberIDs) == 0 || store.DB == nil {
		return nil
	}
	var rows []struct {
		MemberID   int64 `gorm:"column:member_id"`
		MemberType int16 `gorm:"column:member_type"`
	}
	if err := store.DB.Table("session_members").
		Select("member_id, member_type").
		Where("session_id = ? AND member_id IN ?", sessionID, memberIDs).
		Scan(&rows).Error; err != nil {
		logger.L.Warnf("load session member types failed session=%s: %v", sessionID, err)
		return nil
	}
	types := make(map[int64]int16, len(rows))
	for _, row := range rows {
		types[row.MemberID] = row.MemberType
	}
	return types
}

// claimMessageMentionDispatchReceipt atomically claims (msg_id, member_id) in
// message_mention_dispatch_receipts. Returns true only when this call
// created the row — i.e. this is genuinely the first time this message's
// mention of this member is being dispatched — so a concurrent or repeated
// edit claiming the same pair never dispatches twice.
func claimMessageMentionDispatchReceipt(sessionID string, msgID, memberID int64) bool {
	if store.DB == nil || msgID <= 0 || memberID <= 0 {
		return false
	}
	row := model.MessageMentionDispatchReceipt{
		MsgID:     msgID,
		MemberID:  memberID,
		SessionID: sessionID,
		CreatedAt: time.Now().UTC(),
	}
	result := store.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if result.Error != nil {
		logger.L.Warnf(
			"claim message mention dispatch receipt failed session=%s msg=%d member=%d: %v",
			sessionID, msgID, memberID, result.Error,
		)
		return false
	}
	return result.RowsAffected == 1
}

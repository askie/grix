package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// A blocker record has the same lifetime as the approval card it points at.
const pendingOwnerBlockerTTL = approvalCardMsgIDTTL

// Blocker kinds. They mirror the two chat_states waiting phases.
const (
	PendingOwnerBlockerApproval = "approval"
	PendingOwnerBlockerQuestion = "question"
)

// PendingOwnerBlocker records what an owner-initiated action has to target when
// the session is blocked on the owner. The push notification carries the same
// data inside its single-use action token; the watch companion has no token, so
// it acts on the session and the server resolves the target from here.
type PendingOwnerBlocker struct {
	Kind              string `json:"kind"`
	AgentID           int64  `json:"agent_id"`
	ApprovalCommandID string `json:"approval_command_id,omitempty"`
	QuestionID        string `json:"question_id,omitempty"`
	QuestionMessageID int64  `json:"question_message_id,omitempty"`
	RunID             string `json:"run_id,omitempty"`
}

func pendingOwnerBlockerKey(ownerID int64, sessionID string) string {
	return fmt.Sprintf("im:agent_api:pending_owner_blocker:%d:%s", ownerID, strings.TrimSpace(sessionID))
}

// SavePendingOwnerBlocker overwrites the session's blocker record. A session is
// blocked on at most one thing at a time, so the newest card always wins.
func SavePendingOwnerBlocker(ctx context.Context, ownerID int64, sessionID string, blocker PendingOwnerBlocker) {
	if store.RDB == nil || ownerID <= 0 || strings.TrimSpace(sessionID) == "" || blocker.AgentID <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := json.Marshal(blocker)
	if err != nil {
		return
	}
	if err := store.RDB.Set(ctx, pendingOwnerBlockerKey(ownerID, sessionID), raw, pendingOwnerBlockerTTL).Err(); err != nil {
		logger.L.Warnf("save pending owner blocker owner=%d session=%s err=%v", ownerID, sessionID, err)
	}
}

// LoadPendingOwnerBlocker returns the session's blocker record, or nil when
// there is none.
func LoadPendingOwnerBlocker(ctx context.Context, ownerID int64, sessionID string) *PendingOwnerBlocker {
	if store.RDB == nil || ownerID <= 0 || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := store.RDB.Get(ctx, pendingOwnerBlockerKey(ownerID, sessionID)).Bytes()
	if err != nil || len(raw) == 0 {
		return nil
	}
	var blocker PendingOwnerBlocker
	if err := json.Unmarshal(raw, &blocker); err != nil {
		return nil
	}
	return &blocker
}

// ApprovalCardResolved reports whether the approval card for this request has
// already been settled. chat_states stays in waiting_approval until the run
// itself ends, so the card index — deleted the moment the card is edited with
// its result — is what tells a second client that the decision is already made.
// Unknown (no Redis, empty request id) counts as unresolved: refusing a real
// approval is worse than relaying a duplicate one, which the agent ignores.
func ApprovalCardResolved(ctx context.Context, agentID int64, sessionID, requestID string) bool {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(requestID) == "" {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	n, err := store.RDB.Exists(ctx, approvalCardMsgIDKey(agentID, sessionID, requestID)).Result()
	if err != nil {
		return false
	}
	return n == 0
}

package handler

import (
	"context"
	"encoding/json"

	"github.com/askie/grix/backend/internal/api/service"
	wshandler "github.com/askie/grix/backend/internal/ws/handler"
)

// dispatchEditMentionAdditions hands a successful edit off to the same
// newly-added-@mention delivery path used by the Agent API WS bridge (see
// ws/agent_api_bridge_events.go's handleAgentAPIEditMsg). No WS hub is
// available from an HTTP request, so hub is nil — dispatchDirectSessionRoute
// already tolerates that; see its and DispatchMessageEditMentionAdditions's
// doc comments. outcome is nil for a no-op edit (content and extra both
// unchanged), in which case there is nothing to diff.
func dispatchEditMentionAdditions(
	ctx context.Context,
	sessionID string,
	msgID int64,
	outcome *service.EditMentionDispatchContext,
	newContent string,
	newExtra json.RawMessage,
) {
	if outcome == nil {
		return
	}
	wshandler.DispatchMessageEditMentionAdditions(
		nil,
		ctx,
		sessionID,
		outcome.EditorMemberID,
		outcome.EditorMemberType,
		msgID,
		outcome.QuotedMessageID,
		outcome.MsgType,
		outcome.OldContent,
		outcome.OldExtra,
		newContent,
		newExtra,
	)
}

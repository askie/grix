package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/notification"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

func setupOwnerActionTest(t *testing.T) (*Server, func()) {
	t.Helper()
	prevDB, prevRDB := store.DB, store.RDB
	tdb := testutil.NewTestDB()
	rdb := testutil.NewMockRedis()
	store.DB, store.RDB = tdb.DB, rdb
	jwtpkg.Init("owner-action-test-secret", 3600, 7*86400)
	return &Server{}, func() {
		_ = rdb.Close()
		tdb.Close()
		store.DB, store.RDB = prevDB, prevRDB
	}
}

// activeUser creates an enabled user and returns a usable access token for it.
func activeUser(t *testing.T, userID int64) string {
	t.Helper()
	require.NoError(t, store.DB.Create(&model.User{
		ID:       userID,
		Username: fmt.Sprintf("watch-%d", userID),
		Email:    fmt.Sprintf("watch-%d@example.com", userID),
		Nickname: "watch",
		Status:   model.UserStatusActive,
	}).Error)
	token, _, err := jwtpkg.GenerateAccessTokenWithSession(userID, fmt.Sprintf("sid-%d", userID))
	require.NoError(t, err)
	return token
}

func ownerActionRequest(t *testing.T, s *Server, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, "/v1/owner-action", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.handleOwnerAction(w, req)
	return w
}

func seedWaitingApproval(t *testing.T, sessionID string, ownerID, agentID int64, approvalID string) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, store.DB.Create(&model.SessionAgentState{
		SessionID: sessionID,
		OwnerID:   ownerID,
		AgentID:   agentID,
		State:     model.SessionAgentStateWaitingApproval,
		LastRunID: "run-1",
		UpdatedAt: now,
	}).Error)
	agentapi.SavePendingOwnerBlocker(context.Background(), ownerID, sessionID, agentapi.PendingOwnerBlocker{
		Kind:              agentapi.PendingOwnerBlockerApproval,
		AgentID:           agentID,
		ApprovalCommandID: approvalID,
		RunID:             "run-1",
	})
	// The approval card index exists while the card is unsettled.
	require.NoError(t, store.RDB.Set(context.Background(),
		fmt.Sprintf("im:agent_api:approval_card:%d:%s:%s", agentID, sessionID, approvalID),
		int64(9001), time.Hour).Err())
}

func TestOwnerActionRejectsUnauthenticated(t *testing.T) {
	s, cleanup := setupOwnerActionTest(t)
	defer cleanup()

	w := ownerActionRequest(t, s, "", `{"session_id":"s1","action":"approve"}`)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	w = ownerActionRequest(t, s, "not-a-token", `{"session_id":"s1","action":"approve"}`)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

// A session the caller does not own must be indistinguishable from one that
// does not exist: both are 403, never 404.
func TestOwnerActionRejectsNonOwner(t *testing.T) {
	s, cleanup := setupOwnerActionTest(t)
	defer cleanup()
	token := activeUser(t, 4001)
	seedWaitingApproval(t, "sess-owned-by-other", 4002, 700, "cmd-1")

	w := ownerActionRequest(t, s, token, `{"session_id":"sess-owned-by-other","action":"approve"}`)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), `"ok":false`)
}

func TestOwnerActionRejectsUnknownAction(t *testing.T) {
	s, cleanup := setupOwnerActionTest(t)
	defer cleanup()
	token := activeUser(t, 4003)
	seedWaitingApproval(t, "sess-bad-action", 4003, 700, "cmd-1")

	for _, body := range []string{
		`{"session_id":"sess-bad-action","action":"delete"}`,
		`{"session_id":"sess-bad-action","action":""}`,
		`{"session_id":"","action":"approve"}`,
	} {
		w := ownerActionRequest(t, s, token, body)
		require.Equalf(t, http.StatusBadRequest, w.Code, "body=%s", body)
	}
}

func TestOwnerActionRejectsStaleState(t *testing.T) {
	s, cleanup := setupOwnerActionTest(t)
	defer cleanup()
	token := activeUser(t, 4004)

	// Terminal session: neither approve nor stop applies any more.
	require.NoError(t, store.DB.Create(&model.SessionAgentState{
		SessionID: "sess-done", OwnerID: 4004, AgentID: 700,
		State: model.SessionAgentStateCompleted, LastRunID: "run-1", UpdatedAt: time.Now().UTC(),
	}).Error)

	w := ownerActionRequest(t, s, token, `{"session_id":"sess-done","action":"approve"}`)
	require.Equal(t, http.StatusConflict, w.Code)
	w = ownerActionRequest(t, s, token, `{"session_id":"sess-done","action":"stop"}`)
	require.Equal(t, http.StatusConflict, w.Code)

	// Waiting on an approval is not waiting on a question.
	seedWaitingApproval(t, "sess-approval", 4004, 700, "cmd-1")
	w = ownerActionRequest(t, s, token, `{"session_id":"sess-approval","action":"reply","text":"hi"}`)
	require.Equal(t, http.StatusConflict, w.Code)
}

// chat_states stays in waiting_approval until the run ends, so a phone approval
// is invisible there. The approval card index is what closes the item.
func TestOwnerActionRejectsAlreadySettledApproval(t *testing.T) {
	s, cleanup := setupOwnerActionTest(t)
	defer cleanup()
	token := activeUser(t, 4005)
	seedWaitingApproval(t, "sess-settled", 4005, 700, "cmd-1")
	require.NoError(t, store.RDB.Del(context.Background(),
		"im:agent_api:approval_card:700:sess-settled:cmd-1").Err())

	w := ownerActionRequest(t, s, token, `{"session_id":"sess-settled","action":"approve"}`)
	require.Equal(t, http.StatusConflict, w.Code)
}

func TestOwnerActionRateLimitsPerUser(t *testing.T) {
	s, cleanup := setupOwnerActionTest(t)
	defer cleanup()
	token := activeUser(t, 4006)
	seedWaitingApproval(t, "sess-rl", 4006, 700, "cmd-1")

	// Every call is rejected downstream (no agent manager in this process);
	// what matters is that the user's budget is consumed before that.
	for i := 0; i < notifyRateMax; i++ {
		w := ownerActionRequest(t, s, token, `{"session_id":"sess-rl","action":"approve"}`)
		require.NotEqual(t, http.StatusTooManyRequests, w.Code, "call %d must be inside the budget", i)
	}
	w := ownerActionRequest(t, s, token, `{"session_id":"sess-rl","action":"approve"}`)
	require.Equal(t, http.StatusTooManyRequests, w.Code)

	// The budget is per user, so a second owner is unaffected.
	otherToken := activeUser(t, 4007)
	seedWaitingApproval(t, "sess-rl-2", 4007, 700, "cmd-1")
	w = ownerActionRequest(t, s, otherToken, `{"session_id":"sess-rl-2","action":"approve"}`)
	require.NotEqual(t, http.StatusTooManyRequests, w.Code)
}

// recordingExecutor captures the agent command an owner action produces.
type recordingExecutor struct {
	agentID   int64
	ownerID   int64
	sessionID string
	content   string
}

func (r *recordingExecutor) DispatchOwnerCommandText(agentID, ownerID int64, sessionID, content string) bool {
	r.agentID, r.ownerID, r.sessionID, r.content = agentID, ownerID, sessionID, content
	return true
}

func (r *recordingExecutor) RequestOutputStop(int64, string, string) (protocol.AgentOutputStopAckPayload, *agentapi.ActiveRunSnapshot, error) {
	return protocol.AgentOutputStopAckPayload{}, nil, nil
}

func (r *recordingExecutor) DispatchOutputStop(protocol.AgentOutputStopAckPayload, *agentapi.ActiveRunSnapshot) error {
	return nil
}

func (r *recordingExecutor) SendMessage(context.Context, agentapi.SendMessageReq) (*agentapi.SendMessageResult, error) {
	return &agentapi.SendMessageResult{}, nil
}

// The watch has no action token, so it resolves the approval from durable state.
// For one pending approval that must land on the agent as the very same command
// the push notification's callback would have sent.
func TestOwnerActionApproveMatchesNotifyCallback(t *testing.T) {
	s, cleanup := setupOwnerActionTest(t)
	defer cleanup()
	_ = s
	const (
		ownerID    = 4100
		agentID    = 700
		sessionID  = "sess-parity"
		approvalID = "approval-cmd-42"
	)
	seedWaitingApproval(t, sessionID, ownerID, agentID, approvalID)

	// Path A — the push notification: claims are signed into the action token.
	evt := notification.AgentNotificationEvent{
		EventKey:   notification.EventApprovalRequested,
		UserID:     ownerID,
		AgentID:    agentID,
		SessionID:  sessionID,
		RunID:      "run-1",
		RunEventID: "run-1",
		ActionMeta: &notification.ActionMeta{
			AvailableActions:  []string{notification.ActionApprove, notification.ActionDeny, notification.ActionStop},
			ApprovalCommandID: approvalID,
		},
	}
	pushClaims := notification.BuildClaims(&evt)
	token, err := notification.GenerateToken(pushClaims)
	require.NoError(t, err)
	parsed, err := notification.ParseToken(token)
	require.NoError(t, err)

	// Path B — the watch: claims are rebuilt from chat_states + the blocker.
	state, err := store.GetSessionAgentState(sessionID, ownerID)
	require.NoError(t, err)
	require.NotNil(t, state)
	watchClaims, stale := ownerActionClaims(context.Background(), ownerID, *state, notification.ActionApprove)
	require.Empty(t, stale)
	require.Equal(t, parsed.Target.ApprovalCommandID, watchClaims.Target.ApprovalCommandID)
	require.Equal(t, parsed.Target.SessionID, watchClaims.Target.SessionID)
	require.Equal(t, parsed.Target.AgentID, watchClaims.Target.AgentID)
	require.Equal(t, parsed.Target.RunID, watchClaims.Target.RunID)

	viaPush := &recordingExecutor{}
	_, err = executeNotifyAction(context.Background(), viaPush, parsed, notification.ActionApprove, "")
	require.NoError(t, err)
	viaWatch := &recordingExecutor{}
	_, err = executeNotifyAction(context.Background(), viaWatch, watchClaims, notification.ActionApprove, "")
	require.NoError(t, err)

	require.Equal(t, fmt.Sprintf("/approve %s allow", approvalID), viaWatch.content)
	require.Equal(t, *viaPush, *viaWatch)
}

func TestChatStateListShapeIsJSONStable(t *testing.T) {
	// The watch parses agent_id as a string (int64 overflows JS numbers);
	// a silent change of that encoding would break the companion.
	raw, err := json.Marshal(model.SessionAgentState{SessionID: "s", OwnerID: 1, AgentID: 2})
	require.NoError(t, err)
	require.Contains(t, string(raw), `"agent_id":"2"`)
}

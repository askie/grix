package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/notification"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const ownerActionMaxBodyBytes = 16 * 1024

// ownerActionRateKey buckets the blocker actions of one user together. The
// notify callback buckets per notification event; the watch acts on the current
// state instead of on an event, so the user is the only bucket available.
const ownerActionRateKey = "owner-action"

// ownerActionSendRateKey is a separate bucket for `send`. Clearing blockers is
// answering notifications and stays on the notify callback's budget; dictating
// messages is ordinary chatting, and sharing one bucket meant a talkative
// minute on the watch could lock the user out of approving anything.
const (
	ownerActionSendRateKey = "owner-action-send"
	ownerActionSendRateMax = 30
)

// ActionSend is the watch-only action: dictate a message into an agent session.
// The four blocker actions are shared with the notification callback and live in
// the notification package.
const ActionSend = "send"

type ownerActionReq struct {
	SessionID string `json:"session_id"`
	Action    string `json:"action"`
	Text      string `json:"text"`
}

// handleOwnerAction executes an owner action chosen from the current session
// state instead of from a push notification. It is the watch companion's only
// write: the watch has no action token (those are single-use and bound to one
// notification), so it names the session and the server resolves the target.
//
// Like the notify callback this must run in the ws service — agentapi.Manager
// owns the agent connections — and it enforces ownership and rate limits just
// as strictly.
func (s *Server) handleOwnerAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	userID, ok := authenticateBearer(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, ownerActionMaxBodyBytes)
	var req ownerActionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeNotifyJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "invalid payload"})
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	action := strings.TrimSpace(req.Action)
	if sessionID == "" || !isOwnerAction(action) {
		writeNotifyJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "invalid session_id or action"})
		return
	}
	rateKey, rateMax := ownerActionRateBucket(action)
	if !allowNotifyRateN(userID, rateKey, rateMax) {
		writeNotifyJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "message": "too many requests"})
		return
	}

	// Ownership: the chat_states row is keyed by (session_id, owner_id), so a
	// hit is proof the caller owns this session's agent run. A miss is a 403
	// and never a 404 — session existence is not the caller's to learn.
	state, err := store.GetSessionAgentState(sessionID, userID)
	if err != nil {
		logger.L.Warnf("owner-action state lookup user=%d session=%s err=%v", userID, sessionID, err)
		writeNotifyJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "action failed"})
		return
	}
	if state == nil {
		writeNotifyJSON(w, http.StatusForbidden, map[string]any{"ok": false, "message": "not the owner of this session"})
		return
	}

	if action == ActionSend {
		if err := s.sendOwnerMessage(userID, sessionID, req.Text); err != nil {
			logger.L.Warnf("owner-action send user=%d session=%s err=%v", userID, sessionID, err)
			writeNotifyJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeNotifyJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "已发送"})
		return
	}

	claims, stale := ownerActionClaims(r.Context(), userID, *state, action)
	if stale != "" {
		writeNotifyJSON(w, http.StatusConflict, map[string]any{"ok": false, "message": stale})
		return
	}

	mgr := agentapi.GetGlobalManager()
	if mgr == nil {
		writeNotifyJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "message": "service unavailable"})
		return
	}

	message, err := executeNotifyAction(r.Context(), mgr, claims, action, req.Text)
	if err != nil {
		logger.L.Warnf("owner-action action=%s user=%d session=%s err=%v", action, userID, sessionID, err)
		writeNotifyJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "action failed"})
		return
	}
	writeNotifyJSON(w, http.StatusOK, map[string]any{"ok": true, "message": message})
}

// ownerActionRateBucket picks the rate limit bucket for an action.
func ownerActionRateBucket(action string) (string, int64) {
	if action == ActionSend {
		return ownerActionSendRateKey, ownerActionSendRateMax
	}
	return ownerActionRateKey, notifyRateMax
}

func isOwnerAction(action string) bool {
	switch action {
	case notification.ActionApprove, notification.ActionDeny,
		notification.ActionStop, notification.ActionReply, ActionSend:
		return true
	}
	return false
}

// ownerActionClaims rebuilds the target the notification callback would have
// received in its action token, from the durable session state plus the pending
// blocker record. The second return value is a non-empty "stale" message when
// the action no longer applies — the watch drops the inbox item on a 409.
func ownerActionClaims(
	ctx context.Context,
	userID int64,
	state model.SessionAgentState,
	action string,
) (*notification.ActionTokenClaims, string) {
	target := notification.ActionTarget{
		SessionID: state.SessionID,
		AgentID:   state.AgentID,
		RunID:     state.LastRunID,
	}
	claims := &notification.ActionTokenClaims{
		UserID:   userID,
		EventKey: ownerActionRateKey,
		Target:   target,
	}

	// Stop only needs a live run, not a blocker: a task can be stopped while it
	// is running as well as while it waits.
	if action == notification.ActionStop {
		if !isNonTerminalChatState(state.State) {
			return nil, "任务已经结束"
		}
		return claims, ""
	}

	blocker := agentapi.LoadPendingOwnerBlocker(ctx, userID, state.SessionID)
	switch action {
	case notification.ActionApprove, notification.ActionDeny:
		if state.State != model.SessionAgentStateWaitingApproval {
			return nil, "该审批已不在等待中"
		}
		if blocker == nil || blocker.Kind != agentapi.PendingOwnerBlockerApproval ||
			strings.TrimSpace(blocker.ApprovalCommandID) == "" {
			return nil, "该审批已不在等待中"
		}
		if agentapi.ApprovalCardResolved(ctx, blocker.AgentID, state.SessionID, blocker.ApprovalCommandID) {
			return nil, "该审批已被处理"
		}
		claims.Target.ApprovalCommandID = blocker.ApprovalCommandID
	case notification.ActionReply:
		if state.State != model.SessionAgentStateWaitingQuestion {
			return nil, "该提问已不在等待中"
		}
		if blocker == nil || blocker.Kind != agentapi.PendingOwnerBlockerQuestion {
			return nil, "该提问已不在等待中"
		}
		claims.Target.QuestionID = blocker.QuestionID
		claims.Target.QuestionMessageID = blocker.QuestionMessageID
	}
	if blocker != nil && blocker.AgentID > 0 {
		claims.Target.AgentID = blocker.AgentID
	}
	if blocker != nil && strings.TrimSpace(blocker.RunID) != "" {
		claims.Target.RunID = blocker.RunID
	}
	return claims, ""
}

func isNonTerminalChatState(state string) bool {
	switch state {
	case model.SessionAgentStateRunning,
		model.SessionAgentStateWaitingApproval,
		model.SessionAgentStateWaitingQuestion:
		return true
	}
	return false
}

// sendOwnerMessage puts a message into the session as the owner through the
// very code the user WebSocket runs, so it persists, syncs and reaches the
// agent exactly like a message typed on the phone.
func (s *Server) sendOwnerMessage(userID int64, sessionID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("text is empty")
	}
	if s.hub == nil {
		return errors.New("send unavailable")
	}
	payload, err := json.Marshal(protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: fmt.Sprintf("owner_action_%d", snowflake.GenID()),
		MsgType:     1,
		Content:     text,
	})
	if err != nil {
		return err
	}
	conn := newOwnerActionConn(userID)
	handler.HandleSendMsg(s.hub, conn, &protocol.Packet{
		Cmd:     protocol.CmdSendMsg,
		Seq:     conn.NextSeq(),
		Payload: payload,
	})
	return conn.result()
}

// ownerActionConn is a detached ConnInterface: it carries the caller's identity
// into HandleSendMsg and captures the ack/nack instead of writing to a socket.
// It is never registered with the hub, so the owner's real devices still get
// their push through the normal broadcast.
type ownerActionConn struct {
	userID int64

	mu   sync.Mutex
	seq  int64
	nack *protocol.SendNackPayload
	sent bool
}

func newOwnerActionConn(userID int64) *ownerActionConn {
	return &ownerActionConn{userID: userID}
}

func (c *ownerActionConn) SendPayload(cmd string, _ int64, payload interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch cmd {
	case protocol.CmdSendAck:
		c.sent = true
	case protocol.CmdSendNack:
		if p, ok := payload.(protocol.SendNackPayload); ok {
			c.nack = &p
		}
	}
}

func (c *ownerActionConn) result() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.nack != nil {
		return fmt.Errorf("%s", c.nack.Msg)
	}
	if !c.sent {
		return errors.New("send did not complete")
	}
	return nil
}

func (c *ownerActionConn) SendPacket(*protocol.Packet) {}
func (c *ownerActionConn) AckPush(int64)               {}
func (c *ownerActionConn) NextSeq() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}
func (c *ownerActionConn) Close()                                {}
func (c *ownerActionConn) GetUserID() int64                      { return c.userID }
func (c *ownerActionConn) GetDeviceID() string                   { return "owner_action" }
func (c *ownerActionConn) GetPlatform() string                   { return "watch" }
func (c *ownerActionConn) SetAuth(int64, string, string, string) {}
func (c *ownerActionConn) IsAuthed() bool                        { return true }

// authenticateBearer validates an access token from the Authorization header
// with the same checks middleware.Auth() applies on the api service. The ws
// service is plain net/http, so the gin middleware cannot be reused directly.
func authenticateBearer(w http.ResponseWriter, r *http.Request) (int64, bool) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		writeNotifyJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "message": "missing or invalid authorization header"})
		return 0, false
	}
	claims, err := jwtpkg.ValidateAccessToken(strings.TrimPrefix(auth, "Bearer "))
	if err != nil ||
		security.IsAccessTokenRevoked(claims.ID) ||
		security.IsLoginSessionRevoked(claims.UserID, claims.SessionID) ||
		(claims.IssuedAt != nil && security.IsAccessTokenInvalidByPasswordChange(claims.UserID, claims.IssuedAt.Time)) {
		writeNotifyJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "message": "token invalid or expired"})
		return 0, false
	}
	if err := security.EnsureUserActive(claims.UserID); err != nil {
		if errors.Is(err, security.ErrUserDisabled) {
			writeNotifyJSON(w, http.StatusForbidden, map[string]any{"ok": false, "message": "用户已被禁用"})
		} else {
			writeNotifyJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "message": "token invalid or expired"})
		}
		return 0, false
	}
	return claims.UserID, true
}

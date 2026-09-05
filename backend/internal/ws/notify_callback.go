package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/liveactivity"
	"github.com/askie/grix/backend/internal/notification"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const (
	notifyCallbackMaxBodyBytes = 16 * 1024
	notifyNonceTTL             = 10 * time.Minute
	notifyRateWindow           = 5 * time.Minute
	notifyRateMax              = 10
)

type notifyCallbackReq struct {
	Token  string `json:"token"`
	Action string `json:"action"`
	Text   string `json:"text"`
}

// handleNotifyCallback is the offline interaction endpoint. The action token in
// the body is the only credential (no Authorization header). It validates the
// token, enforces one-time use + rate limit, then executes the owner action
// in-process — the agentapi.Manager that owns agent connections lives here in
// the ws service, so this is the only process that can approve/deny/stop/reply.
func (s *Server) handleNotifyCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, notifyCallbackMaxBodyBytes)
	var req notifyCallbackReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeNotifyJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "invalid payload"})
		return
	}

	claims, err := notification.ParseToken(req.Token)
	if err != nil {
		writeNotifyJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "message": "invalid or expired token"})
		return
	}
	action := strings.TrimSpace(req.Action)
	if !claims.Allows(action) {
		writeNotifyJSON(w, http.StatusForbidden, map[string]any{"ok": false, "message": "action not permitted"})
		return
	}
	if !allowNotifyRate(claims.UserID, claims.EventKey) {
		writeNotifyJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "message": "too many requests"})
		return
	}
	// One-time use: reject replays of the same nonce.
	if !consumeNotifyNonce(claims.Nonce) {
		writeNotifyJSON(w, http.StatusConflict, map[string]any{"ok": false, "message": "already processed"})
		return
	}

	mgr := agentapi.GetGlobalManager()
	if mgr == nil {
		writeNotifyJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "message": "service unavailable"})
		return
	}

	message, err := executeNotifyAction(r.Context(), mgr, claims, action, req.Text)
	if err != nil {
		logger.L.Warnf("notify-callback action=%s user=%d event=%s err=%v", action, claims.UserID, claims.EventKey, err)
		writeNotifyJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "action failed"})
		return
	}
	writeNotifyJSON(w, http.StatusOK, map[string]any{"ok": true, "message": message})
}

// ownerActionExecutor is the slice of agentapi.Manager an owner action needs.
// Both entry points — the notification callback and the watch's /v1/owner-action
// — run through executeNotifyAction, so naming the dependency here is what lets
// a test observe the command the two paths emit.
type ownerActionExecutor interface {
	DispatchOwnerCommandText(agentID, ownerID int64, sessionID, content string) bool
	RequestOutputStop(ownerID int64, sessionID string, eventID string) (protocol.AgentOutputStopAckPayload, *agentapi.ActiveRunSnapshot, error)
	DispatchOutputStop(ack protocol.AgentOutputStopAckPayload, run *agentapi.ActiveRunSnapshot) error
	SendMessage(ctx context.Context, req agentapi.SendMessageReq) (*agentapi.SendMessageResult, error)
}

// approvalCommandText is the exact command relayed to the agent for an approval
// decision. Both owner-action entry points build it from the same resolved
// approval_command_id.
func approvalCommandText(approvalCommandID string, allow bool) string {
	decision := "deny"
	if allow {
		decision = "allow"
	}
	return fmt.Sprintf("/approve %s %s", approvalCommandID, decision)
}

func executeNotifyAction(ctx context.Context, mgr ownerActionExecutor, claims *notification.ActionTokenClaims, action, text string) (string, error) {
	t := claims.Target
	switch action {
	case notification.ActionApprove:
		if !mgr.DispatchOwnerCommandText(t.AgentID, claims.UserID, t.SessionID,
			approvalCommandText(t.ApprovalCommandID, true)) {
			return "", fmt.Errorf("approve dispatch failed (agent offline?)")
		}
		resumeLiveActivity(claims.UserID, t.AgentID, t.SessionID)
		return "已批准，Agent 继续执行", nil

	case notification.ActionDeny:
		if !mgr.DispatchOwnerCommandText(t.AgentID, claims.UserID, t.SessionID,
			approvalCommandText(t.ApprovalCommandID, false)) {
			return "", fmt.Errorf("deny dispatch failed (agent offline?)")
		}
		resumeLiveActivity(claims.UserID, t.AgentID, t.SessionID)
		return "已拒绝", nil

	case notification.ActionStop:
		ack, run, err := mgr.RequestOutputStop(claims.UserID, t.SessionID, t.RunID)
		if err != nil {
			return "", err
		}
		if err := mgr.DispatchOutputStop(ack, run); err != nil {
			return "", err
		}
		return "已停止任务", nil

	case notification.ActionReply:
		if strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("reply text is empty")
		}
		_, err := mgr.SendMessage(ctx, agentapi.SendMessageReq{
			AgentID:         t.AgentID,
			OwnerID:         claims.UserID,
			IdentityMode:    "delegate",
			SessionID:       t.SessionID,
			MsgType:         1,
			Content:         text,
			QuotedMessageID: t.QuestionMessageID,
		})
		if err != nil {
			return "", err
		}
		resumeLiveActivity(claims.UserID, t.AgentID, t.SessionID)
		return "已回复", nil

	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

// resumeLiveActivity 把锁屏卡片从"等你"翻回"在跑"。主人处理完阻塞后 run 继续跑，
// 但 chat_states 不会因此再写一次 running（阻塞与否是 run 内部的事），所以这一帧
// 只能由主人动作本身触发。审批 / 提问两条离线操作入口都汇到这里。
func resumeLiveActivity(userID, agentID int64, sessionID string) {
	go liveactivity.OnResumed(liveactivity.Run{
		UserID:    userID,
		AgentID:   agentID,
		SessionID: sessionID,
	})
}

// consumeNotifyNonce returns true if the nonce was unused (and marks it used).
// Fails closed only on the duplicate case; on Redis errors it allows the action
// so a transient Redis blip doesn't block a legitimate approval.
func consumeNotifyNonce(nonce string) bool {
	if strings.TrimSpace(nonce) == "" || store.RDB == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	ok, err := store.RDB.SetNX(ctx, "notif:nonce:"+nonce, "1", notifyNonceTTL).Result()
	if err != nil {
		return true
	}
	return ok
}

func allowNotifyRate(userID int64, eventKey string) bool {
	return allowNotifyRateN(userID, eventKey, notifyRateMax)
}

// allowNotifyRateN is allowNotifyRate with an explicit budget. Blocker actions
// answer a notification and are naturally rare; dictating messages from the
// watch is ordinary chatting and needs its own, larger bucket.
func allowNotifyRateN(userID int64, eventKey string, max int64) bool {
	if store.RDB == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	key := fmt.Sprintf("notif:rl:%d:%s", userID, eventKey)
	n, err := store.RDB.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if n == 1 {
		store.RDB.Expire(ctx, key, notifyRateWindow)
	}
	return n <= max
}

func writeNotifyJSON(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

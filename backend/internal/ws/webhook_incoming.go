package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/webhook"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
)

const webhookMaxBodyBytes = 1 << 20
const webhookRateLimitMax = 60
const webhookRateLimitWindow = time.Minute

var webhookLimiter = newFixedWindowLimiter(60, time.Minute)
var webhookRedisRateScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
if current > tonumber(ARGV[2]) then
  return 0
end
return 1
`)

type fixedWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]windowCounter
}

type windowCounter struct {
	count int
	start time.Time
}

func newFixedWindowLimiter(limit int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{limit: limit, window: window, buckets: make(map[string]windowCounter)}
}

func (l *fixedWindowLimiter) Allow(key string) bool {
	if l == nil || key == "" {
		return false
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b.start.IsZero() || now.Sub(b.start) >= l.window {
		l.buckets[key] = windowCounter{count: 1, start: now}
		return true
	}
	if b.count >= l.limit {
		return false
	}
	b.count++
	l.buckets[key] = b
	return true
}

type webhookBridgeConn struct {
	userID   int64
	deviceID string
	seq      int64
	sent     []bridgeSentPayload
}

func (c *webhookBridgeConn) SendPayload(cmd string, seq int64, payload interface{}) {
	c.sent = append(c.sent, bridgeSentPayload{cmd: cmd, seq: seq, payload: payload})
}
func (c *webhookBridgeConn) SendPacket(pkt *protocol.Packet)                            {}
func (c *webhookBridgeConn) AckPush(msgID int64)                                        {}
func (c *webhookBridgeConn) Close()                                                     {}
func (c *webhookBridgeConn) NextSeq() int64                                             { return atomic.AddInt64(&c.seq, 1) }
func (c *webhookBridgeConn) GetUserID() int64                                           { return c.userID }
func (c *webhookBridgeConn) GetDeviceID() string                                        { return c.deviceID }
func (c *webhookBridgeConn) GetPlatform() string                                        { return "" }
func (c *webhookBridgeConn) SetAuth(userID int64, sessionID, deviceID, platform string) {}
func (c *webhookBridgeConn) IsAuthed() bool                                             { return true }

type webhookSender struct{ hub *Hub }

func (s webhookSender) SendAsUser(_ context.Context, req webhook.SendRequest) (*webhook.SendResult, error) {
	payload := protocol.SendMsgPayload{
		SessionID:   req.SessionID,
		ClientMsgID: req.ClientMsgID,
		MsgType:     req.MsgType,
		Content:     req.Content,
	}
	raw, _ := json.Marshal(payload)
	conn := &webhookBridgeConn{userID: req.UserID, deviceID: fmt.Sprintf("webhook_%d", req.UserID)}
	pkt := &protocol.Packet{Cmd: protocol.CmdSendMsg, Seq: conn.NextSeq(), Payload: raw}
	handler.HandleSendMsg(s.hub, conn, pkt)
	var ack *protocol.SendAckPayload
	var nack *protocol.SendNackPayload
	for i := range conn.sent {
		item := conn.sent[i]
		switch item.cmd {
		case protocol.CmdSendAck:
			if p, ok := item.payload.(protocol.SendAckPayload); ok {
				t := p
				ack = &t
			}
		case protocol.CmdSendNack:
			if p, ok := item.payload.(protocol.SendNackPayload); ok {
				t := p
				nack = &t
			}
		}
	}
	if nack != nil {
		return nil, errors.New(nack.Msg)
	}
	if ack == nil {
		return nil, errors.New("webhook send failed")
	}
	return &webhook.SendResult{MessageID: ack.MsgID, SentAt: time.UnixMilli(ack.CreatedAt).UTC()}, nil
}

func (s *Server) handleWebhookIncoming(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/v1/webhook/incoming/")
	if strings.TrimSpace(token) == "" || strings.Contains(token, "/") {
		writeWebhookJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	ip := clientIPForWebhook(r)
	if !allowWebhookRate(token, ip) {
		writeWebhookJSON(w, http.StatusTooManyRequests, map[string]any{"code": "WEBHOOK_RATE_LIMITED"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, webhookMaxBodyBytes)
	var req webhook.DeliverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeWebhookJSON(w, http.StatusBadRequest, map[string]any{"code": "WEBHOOK_INVALID_PAYLOAD"})
		return
	}
	svc := webhook.NewService()
	out, err := svc.Deliver(r.Context(), token, req, webhookSender{hub: s.hub})
	if err != nil {
		switch {
		case errors.Is(err, webhook.ErrNotFound):
			writeWebhookJSON(w, http.StatusNotFound, map[string]any{"code": "WEBHOOK_NOT_FOUND"})
		case errors.Is(err, webhook.ErrExpired):
			writeWebhookJSON(w, http.StatusGone, map[string]any{"code": "WEBHOOK_EXPIRED"})
		case errors.Is(err, webhook.ErrForbidden):
			writeWebhookJSON(w, http.StatusForbidden, map[string]any{"code": "WEBHOOK_FORBIDDEN"})
		case errors.Is(err, webhook.ErrInvalidPayload):
			writeWebhookJSON(w, http.StatusBadRequest, map[string]any{"code": "WEBHOOK_INVALID_PAYLOAD"})
		default:
			writeWebhookJSON(w, http.StatusInternalServerError, map[string]any{"code": "WEBHOOK_SEND_FAILED"})
		}
		return
	}
	writeWebhookJSON(w, http.StatusOK, map[string]any{
		"message_id": fmt.Sprintf("%d", out.MessageID),
		"sent_at":    out.SentAt.UTC().Format(time.RFC3339),
	})
}

func allowWebhookRate(token, ip string) bool {
	key := "webhook:in:rate:" + strings.TrimSpace(token) + ":" + strings.TrimSpace(ip)
	if store.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		defer cancel()
		ret, err := webhookRedisRateScript.Run(ctx, store.RDB, []string{key}, webhookRateLimitWindow.Milliseconds(), webhookRateLimitMax).Int()
		if err == nil {
			return ret == 1
		}
	}
	return webhookLimiter.Allow(key)
}

func clientIPForWebhook(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	host := strings.TrimSpace(r.RemoteAddr)
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	if parsed, err := netip.ParseAddr(host); err == nil {
		return parsed.String()
	}
	return "unknown"
}

func writeWebhookJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

package ws

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/gorilla/websocket"
)

const (
	writeWait                 = 10 * time.Second
	pongWait                  = 90 * time.Second
	pongWaitForBackground     = 5 * time.Minute
	fallbackIdleForBackground = 15 * time.Second
	pingInterval              = 30 * time.Second
	maxMessageSize            = 262_144
	pushAckWait               = 5 * time.Second
	pushAckWaitForMobile      = 20 * time.Second
	maxPushRetry              = 3
)

const (
	connAppStateUnknown int32 = iota
	connAppStateForeground
	connAppStateBackground
)

type pendingPushState struct {
	msgID   int64
	data    []byte
	cmd     string
	payload json.RawMessage
	retries int
	timer   *time.Timer
}

type Conn struct {
	ws                *websocket.Conn
	send              chan []byte
	userID            int64
	sessionID         string
	deviceID          string
	platform          string
	widgetOwnerID     int64
	widgetClientIP    string
	authed            bool
	seq               int64
	closeOnce         sync.Once
	closed            atomic.Bool
	appState          atomic.Int32
	appStateReported  atomic.Bool
	lastInboundUnixMs atomic.Int64
	pendingMu         sync.Mutex
	pendingPush       map[int64]*pendingPushState
	// drainCloseCode/drainCloseReason 由节点关停 drain 设置：WritePump 在 send
	// 通道耗尽后写出带该码的关闭帧（默认是空关闭帧、对端收到 1005），让客户端
	// 能区分「服务端主动关停」并立即重连，而不是等心跳超时才发现。
	drainCloseCode   atomic.Int32
	drainCloseReason atomic.Value // string
}

// Ensure Conn implements handler.ConnInterface
var _ handler.ConnInterface = (*Conn)(nil)

func NewConn(wsConn *websocket.Conn) *Conn {
	nowMs := time.Now().UnixMilli()
	conn := &Conn{
		ws:          wsConn,
		send:        make(chan []byte, 256),
		pendingPush: make(map[int64]*pendingPushState),
	}
	conn.lastInboundUnixMs.Store(nowMs)
	if wsConn != nil {
		wsConn.SetPongHandler(func(string) error {
			conn.markInboundActivity()
			return conn.refreshReadDeadline()
		})
	}
	return conn
}

func (c *Conn) GetUserID() int64    { return c.userID }
func (c *Conn) GetDeviceID() string { return c.deviceID }
func (c *Conn) GetPlatform() string { return c.platform }
func (c *Conn) IsAuthed() bool      { return c.authed }

// SetWidgetContext 记录 widget 访客连接的 owner 与真实客户端 IP，
// 供消息级 IP 封禁判定使用（仅 widget WS 连接会设置；ownerID<=0 表示非 widget 连接）。
func (c *Conn) SetWidgetContext(ownerUserID int64, clientIP string) {
	c.widgetOwnerID = ownerUserID
	c.widgetClientIP = clientIP
}

func (c *Conn) GetWidgetContext() (ownerUserID int64, clientIP string) {
	return c.widgetOwnerID, c.widgetClientIP
}

func (c *Conn) SetAuth(userID int64, sessionID, deviceID, platform string) {
	c.userID = userID
	c.sessionID = sessionID
	c.deviceID = deviceID
	c.platform = platform
	c.authed = true
	c.appState.Store(connAppStateForeground)
	c.appStateReported.Store(false)
	c.markInboundActivity()
}

func (c *Conn) NextSeq() int64 {
	return atomic.AddInt64(&c.seq, 1)
}

func (c *Conn) AckPush(msgID int64) {
	if msgID <= 0 {
		return
	}
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	state, ok := c.pendingPush[msgID]
	if !ok {
		return
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	delete(c.pendingPush, msgID)
}

func (c *Conn) SendPacket(pkt *protocol.Packet) {
	data, err := json.Marshal(pkt)
	if err != nil {
		logger.L.Errorf("marshal packet error: %v", err)
		return
	}

	msgID := int64(0)
	if pkt.Cmd == protocol.CmdPushMsg {
		msgID = extractPushMsgID(pkt.Payload)
		if msgID > 0 {
			c.trackPush(msgID, data)
		}
	}

	if !c.enqueue(data) {
		if msgID > 0 {
			c.AckPush(msgID)
		}
		logger.L.Warnf("send buffer full for user %d device %s, closing connection for pull_sync recovery", c.userID, c.deviceID)
		c.Close()
		c.closeWebsocket()
	}
}

func (c *Conn) SendPayload(cmd string, seq int64, payload interface{}) {
	raw, _ := json.Marshal(payload)
	c.SendPacket(&protocol.Packet{Cmd: cmd, Seq: seq, Payload: raw})
}

func (c *Conn) ReadPump(hub *Hub) {
	defer func() {
		hub.Unregister(c)
		c.Close()
		c.closeWebsocket()
	}()
	if c.ws == nil {
		return
	}
	c.ws.SetReadLimit(maxMessageSize)
	if err := c.refreshReadDeadline(); err != nil {
		logger.L.Warnf("set initial read deadline failed user=%d device=%s err=%v", c.userID, c.deviceID, err)
		return
	}

	for {
		_, message, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
				logger.L.Warnf(
					"read pump exit due to oversized websocket frame: user=%d device=%s platform=%s authed=%v max_message_size=%d err=%v",
					c.userID,
					c.deviceID,
					c.platform,
					c.authed,
					maxMessageSize,
					err,
				)
			} else if c.isMobileNormalDisconnect(err) || c.isMobileBackgroundTimeout(err) || c.isWebBackgroundTimeout(err) {
				logger.L.Infof(
					"read pump exit (mobile degraded) user=%d device=%s platform=%s app_state=%s app_state_reported=%v route_mode=%s idle_ms=%d err=%v",
					c.userID,
					c.deviceID,
					c.platform,
					c.appStateString(),
					c.appStateReported.Load(),
					c.mobileConnRoutingMode(),
					c.inboundIdleDuration().Milliseconds(),
					err,
				)
			} else {
				logger.L.Warnf(
					"read pump exit user=%d device=%s platform=%s app_state=%s app_state_reported=%v route_mode=%s idle_ms=%d authed=%v err=%v",
					c.userID,
					c.deviceID,
					c.platform,
					c.appStateString(),
					c.appStateReported.Load(),
					c.mobileConnRoutingMode(),
					c.inboundIdleDuration().Milliseconds(),
					c.authed,
					err,
				)
			}
			break
		}
		c.markInboundActivity()
		if err := c.refreshReadDeadline(); err != nil {
			logger.L.Warnf("refresh read deadline failed user=%d device=%s err=%v", c.userID, c.deviceID, err)
			break
		}

		var pkt protocol.Packet
		if err := json.Unmarshal(message, &pkt); err != nil {
			logger.L.Warnf("invalid packet: %v", err)
			continue
		}
		RoutePacket(hub, c, &pkt)
	}
}

func (c *Conn) WritePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.Close()
		c.closeWebsocket()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.ws.WriteMessage(websocket.CloseMessage, c.closeFramePayload())
				return
			}
			if err := c.ws.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Conn) Close() {
	c.closeOnce.Do(func() {
		c.closed.Store(true)

		// Collect pending entries and clear the map under the lock.
		// Offline push is dispatched outside the critical section to avoid
		// blocking I/O (NATS publish) while Hub's h.mu or pendingMu is held.
		c.pendingMu.Lock()
		pending := make([]*pendingPushState, 0, len(c.pendingPush))
		for _, state := range c.pendingPush {
			if state.timer != nil {
				state.timer.Stop()
			}
			pending = append(pending, state)
		}
		c.pendingPush = make(map[int64]*pendingPushState)
		c.pendingMu.Unlock()

		close(c.send)

		// 必须按 msgID 升序派发。这些补推会逐条挂进同一个 (用户,会话) 的合并窗口，
		// 而窗口是「后来者覆盖前者」——map 迭代是随机序，直接派发等于随机决定最后
		// 推哪一条，用户有相当概率收到的是「好的，我看一下」这种开场白，正是这批
		// 改动要消灭的症状。msgID 是雪花 ID、单调递增，升序派发让最新那条最后挂、
		// 赢下窗口。
		sort.Slice(pending, func(i, j int) bool {
			return pending[i].msgID < pending[j].msgID
		})
		for _, state := range pending {
			c.enqueueOfflinePushAfterAckTimeout(state)
		}
	})
}

// CloseWithCode 标记关停关闭码并触发连接关闭：WritePump 在 send 通道耗尽后
// 写出带该码的关闭帧，随后关闭底层 TCP。仅用于节点关停 drain。
func (c *Conn) CloseWithCode(code int, reason string) {
	c.drainCloseCode.Store(int32(code))
	c.drainCloseReason.Store(reason)
	c.Close()
}

// closeFramePayload 生成关闭帧内容：drain 设置了关闭码时返回带码带原因的帧，
// 否则保持原有空关闭帧行为。
func (c *Conn) closeFramePayload() []byte {
	code := int(c.drainCloseCode.Load())
	if code == 0 {
		return []byte{}
	}
	reason, _ := c.drainCloseReason.Load().(string)
	return websocket.FormatCloseMessage(code, reason)
}

func (c *Conn) refreshReadDeadline() error {
	if c.ws == nil {
		return nil
	}
	return c.ws.SetReadDeadline(time.Now().Add(c.readDeadlineWindow()))
}

func (c *Conn) enqueue(data []byte) (ok bool) {
	if c.closed.Load() {
		return false
	}
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}

func (c *Conn) trackPush(msgID int64, data []byte) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	if existing, ok := c.pendingPush[msgID]; ok && existing.timer != nil {
		existing.timer.Stop()
	}

	c.pendingPush[msgID] = &pendingPushState{
		msgID:   msgID,
		data:    data,
		cmd:     protocol.CmdPushMsg,
		payload: append(json.RawMessage(nil), extractPacketPayload(data)...),
		timer: time.AfterFunc(c.pushAckWaitForConn(), func() {
			c.onPushAckTimeout(msgID)
		}),
	}
}

func (c *Conn) onPushAckTimeout(msgID int64) {
	c.pendingMu.Lock()
	state, ok := c.pendingPush[msgID]
	if !ok {
		c.pendingMu.Unlock()
		return
	}

	if state.retries >= maxPushRetry {
		delete(c.pendingPush, msgID)
		c.pendingMu.Unlock()
		c.enqueueOfflinePushAfterAckTimeout(state)
		if c.shouldCloseAfterPushAckTimeout() {
			logger.L.Warnf(
				"push ack timeout exceeded msg_id=%d user=%d device=%s platform=%s app_state=%s app_state_reported=%v route_mode=%s idle_ms=%d, closing connection",
				msgID,
				c.userID,
				c.deviceID,
				c.platform,
				c.appStateString(),
				c.appStateReported.Load(),
				c.mobileConnRoutingMode(),
				c.inboundIdleDuration().Milliseconds(),
			)
			c.Close()
			c.closeWebsocket()
		} else {
			logger.L.Infof(
				"push ack timeout exceeded msg_id=%d user=%d device=%s platform=%s app_state=%s app_state_reported=%v route_mode=%s idle_ms=%d, keeping connection open for mobile recovery",
				msgID,
				c.userID,
				c.deviceID,
				c.platform,
				c.appStateString(),
				c.appStateReported.Load(),
				c.mobileConnRoutingMode(),
				c.inboundIdleDuration().Milliseconds(),
			)
		}
		return
	}

	state.retries++
	attempt := state.retries
	data := state.data
	state.timer = time.AfterFunc(c.pushAckWaitForConn(), func() {
		c.onPushAckTimeout(msgID)
	})
	c.pendingPush[msgID] = state
	c.pendingMu.Unlock()

	if !c.enqueue(data) {
		c.AckPush(msgID)
		logger.L.Warnf("push retry enqueue failed msg_id=%d user=%d device=%s, closing connection", msgID, c.userID, c.deviceID)
		c.Close()
		c.closeWebsocket()
		return
	}

	logger.L.Warnf(
		"retry push msg_id=%d attempt=%d user=%d device=%s platform=%s app_state=%s app_state_reported=%v route_mode=%s idle_ms=%d",
		msgID,
		attempt,
		c.userID,
		c.deviceID,
		c.platform,
		c.appStateString(),
		c.appStateReported.Load(),
		c.mobileConnRoutingMode(),
		c.inboundIdleDuration().Milliseconds(),
	)
}

func extractPushMsgID(raw json.RawMessage) int64 {
	var payload struct {
		MsgID int64 `json:"msg_id,string"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0
	}
	return payload.MsgID
}

func extractPushSenderID(raw json.RawMessage) int64 {
	var payload struct {
		SenderID int64 `json:"sender_id,string"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0
	}
	return payload.SenderID
}

func extractPacketPayload(raw []byte) json.RawMessage {
	var pkt protocol.Packet
	if err := json.Unmarshal(raw, &pkt); err != nil {
		return nil
	}
	return pkt.Payload
}

var enqueueOfflinePushAfterAckTimeout = func(userID int64, cmd string, payload json.RawMessage) error {
	if store.JS == nil {
		return nil
	}
	if userID <= 0 || cmd == "" || len(payload) == 0 {
		return nil
	}

	task, err := json.Marshal(map[string]any{
		"user_id": userID,
		"cmd":     cmd,
		"payload": json.RawMessage(payload),
	})
	if err != nil {
		return fmt.Errorf("marshal offline push task: %w", err)
	}

	_, err = store.JS.Publish(fmt.Sprintf("im.push.offline.%d", userID), task)
	if err != nil {
		return fmt.Errorf("publish offline push task: %w", err)
	}
	return nil
}

// enqueueOfflinePushAfterAckTimeout 补推一条没等到 ACK 的消息。
//
// 两个调用点（onPushAckTimeout 逐条超时、Close 拆卸时批量收尾）语境一致：连接上
// 这条消息没能确认送达，得靠离线推送兜底。AI 连发的多条消息在这里同样要合并，
// 否则开场白和正文各响一次——这条路对应 App 退后台/僵尸路由，正是窗口要管的场景。
//
// 批量派发时调用方必须按 msgID 升序，理由见 Close。
func (c *Conn) enqueueOfflinePushAfterAckTimeout(state *pendingPushState) {
	if state == nil || state.cmd != protocol.CmdPushMsg || len(state.payload) == 0 {
		return
	}

	// Sender's own multi-device sync should recover via pull_sync, not OS push.
	if senderID := extractPushSenderID(state.payload); senderID > 0 && senderID == c.userID {
		return
	}

	userID, cmd, raw := c.userID, state.cmd, state.payload
	deviceID := c.deviceID
	enqueue := func() {
		if err := enqueueOfflinePushAfterAckTimeout(userID, cmd, raw); err != nil {
			logger.L.Warnf("enqueue offline push after ack timeout failed user=%d device=%s err=%v", userID, deviceID, err)
		}
	}

	// 判定要读 SenderType/MsgType 等字段，故先解出结构体；真正入队仍用原始 payload，
	// 避免反序列化再序列化把结构体没定义的字段丢掉。
	var pm protocol.PushMsgPayload
	if err := json.Unmarshal(state.payload, &pm); err == nil {
		if handler.TryDebounceOfflinePush(userID, cmd, pm, enqueue) {
			return
		}
	}

	enqueue()
}

func (c *Conn) closeWebsocket() {
	if c.ws != nil {
		_ = c.ws.Close()
	}
}

func (c *Conn) isMobileNormalDisconnect(err error) bool {
	if !websocket.IsCloseError(err, websocket.CloseAbnormalClosure, websocket.CloseNoStatusReceived) {
		return false
	}
	platform := strings.ToLower(strings.TrimSpace(c.platform))
	return platform == "ios" || platform == "android"
}

func (c *Conn) isMobileBackgroundTimeout(err error) bool {
	if err == nil || !c.isMobilePlatform() {
		return false
	}
	if !c.isMobileBackgroundLike() {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "i/o timeout")
}

func (c *Conn) isWebBackgroundTimeout(err error) bool {
	if err == nil || c.isMobilePlatform() {
		return false
	}
	if !c.isBackgroundLike() {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "i/o timeout") ||
		websocket.IsCloseError(err, websocket.CloseAbnormalClosure, websocket.CloseNoStatusReceived)
}

func (c *Conn) shouldCloseAfterPushAckTimeout() bool {
	if !c.isMobilePlatform() {
		// Web client in reported background state: keep connection for recovery
		if c.isBackgroundLike() {
			return false
		}
		return true
	}
	return !c.isMobileBackgroundLike()
}

func (c *Conn) pushAckWaitForConn() time.Duration {
	if c.isBackgroundLike() {
		return pushAckWaitForMobile
	}
	return pushAckWait
}

func (c *Conn) readDeadlineWindow() time.Duration {
	if c.isBackgroundLike() {
		return pongWaitForBackground
	}
	return pongWait
}

func (c *Conn) isMobilePlatform() bool {
	platform := strings.ToLower(strings.TrimSpace(c.platform))
	return platform == "ios" || platform == "android"
}

func (c *Conn) setAppState(rawState string) bool {
	switch strings.ToLower(strings.TrimSpace(rawState)) {
	case "foreground":
		c.appState.Store(connAppStateForeground)
		c.appStateReported.Store(true)
		return true
	case "background":
		c.appState.Store(connAppStateBackground)
		c.appStateReported.Store(true)
		return true
	default:
		return false
	}
}

func (c *Conn) markInboundActivity() {
	c.lastInboundUnixMs.Store(time.Now().UnixMilli())
}

func (c *Conn) inboundIdleDuration() time.Duration {
	last := c.lastInboundUnixMs.Load()
	if last <= 0 {
		return 0
	}
	idle := time.Since(time.UnixMilli(last))
	if idle < 0 {
		return 0
	}
	return idle
}

func (c *Conn) isMobileBackgroundLike() bool {
	if !c.isMobilePlatform() {
		return false
	}
	if c.appState.Load() == connAppStateBackground {
		return true
	}
	if c.appStateReported.Load() {
		return false
	}
	return c.inboundIdleDuration() >= fallbackIdleForBackground
}

// isBackgroundLike returns true for mobile background-like states AND
// for non-mobile (web) clients that have explicitly reported background state.
func (c *Conn) isBackgroundLike() bool {
	if c.isMobilePlatform() {
		return c.isMobileBackgroundLike()
	}
	return c.appStateReported.Load() && c.appState.Load() == connAppStateBackground
}

func (c *Conn) mobileConnRoutingMode() string {
	if !c.isMobilePlatform() {
		return "non_mobile"
	}
	if c.appState.Load() == connAppStateBackground {
		if c.appStateReported.Load() {
			return "background_reported"
		}
		return "background_default"
	}
	if c.isMobileBackgroundLike() {
		return "background_fallback"
	}
	if c.appStateReported.Load() {
		return "foreground_reported"
	}
	return "foreground_unreported"
}

func (c *Conn) appStateString() string {
	switch c.appState.Load() {
	case connAppStateForeground:
		return "foreground"
	case connAppStateBackground:
		return "background"
	default:
		return "unknown"
	}
}

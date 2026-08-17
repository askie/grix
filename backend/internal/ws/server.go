package ws

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/acp"
	"github.com/askie/grix/backend/internal/agentadapter/agy"
	"github.com/askie/grix/backend/internal/agentadapter/claude"
	"github.com/askie/grix/backend/internal/agentadapter/codewhale"
	"github.com/askie/grix/backend/internal/agentadapter/codex"
	"github.com/askie/grix/backend/internal/agentadapter/copilot"
	"github.com/askie/grix/backend/internal/agentadapter/cursor"
	"github.com/askie/grix/backend/internal/agentadapter/deepseek"
	"github.com/askie/grix/backend/internal/agentadapter/gemini"
	"github.com/askie/grix/backend/internal/agentadapter/hermes"
	"github.com/askie/grix/backend/internal/agentadapter/kimi"
	"github.com/askie/grix/backend/internal/agentadapter/kiro"
	"github.com/askie/grix/backend/internal/agentadapter/openclaw"
	"github.com/askie/grix/backend/internal/agentadapter/opencode"
	"github.com/askie/grix/backend/internal/agentadapter/openhuman"
	"github.com/askie/grix/backend/internal/agentadapter/pi"
	"github.com/askie/grix/backend/internal/agentadapter/qwen"
	"github.com/askie/grix/backend/internal/agentadapter/reasonix"
	agenttoolbar "github.com/askie/grix/backend/internal/agenttoolbar"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/metrics"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/adapterlog"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/version"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/dispatch"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"net/http/pprof"

	"github.com/gorilla/websocket"
)

type Server struct {
	hub                         *Hub
	port                        int
	nodeID                      string
	allowedWebOrigins           string
	pprofSecret                 string
	agentAPIPath                string
	agentAPIHeartbeat           time.Duration
	widgetEnabled               bool
	agentAPIStreamShardLocks    [64]sync.Mutex
	agentAPIStreamFinishGrace   time.Duration
	agentAPIStreamStallReanchor time.Duration
	agentAPIStreamFinishMu      sync.Mutex
	agentAPIStreamFinishTimers  map[string]*time.Timer
	// agentAPIStreamFinishActive 统计「已经触发、正在跑」的收尾回调。
	// Timer.Stop() 只拦得住还没触发的；正在跑的那个会继续写 DB，必须等它跑完。
	agentAPIStreamFinishActive  int
	agentAPIStreamFinishClosing bool
	// agentAPIStreamFenceDrop* 把「fenced 流仍被持续推 chunk」的 drop 日志按
	// agent+client_msg_id 采样：交错 thinking 等场景下同一条流可产生数十条/秒的
	// 重复 drop，全量打 Info 会刷屏（线上实测 10 分钟 1166 条）。
	agentAPIStreamFenceDropMu     sync.Mutex
	agentAPIStreamFenceDropCounts map[string]int64
	agentAPIMgr                   *wsagentapi.Manager
	adapterLogMgr                 *adapterlog.Manager
	srvMu                         sync.Mutex // 保护 srv：Start 在协程里写，Shutdown 由信号触发读，两者天然并发
	srv                           *http.Server
	stopRedisSub                  func()
	cleanupOnce                   sync.Once
}

func NewServer(port int, nodeID, allowedWebOrigins, agentAPIPath string, heartbeatSec int, pprofSecret string, widgetEnabled bool) *Server {
	if strings.TrimSpace(agentAPIPath) == "" {
		agentAPIPath = "/v1/agent-api/ws"
	}
	if !strings.HasPrefix(agentAPIPath, "/") {
		agentAPIPath = "/" + agentAPIPath
	}
	heartbeat := time.Duration(heartbeatSec) * time.Second
	if heartbeatSec <= 0 {
		heartbeat = 30 * time.Second
	}

	hub := NewHub(nodeID)
	return &Server{
		hub:                         hub,
		port:                        port,
		nodeID:                      nodeID,
		allowedWebOrigins:           allowedWebOrigins,
		pprofSecret:                 pprofSecret,
		agentAPIPath:                agentAPIPath,
		agentAPIHeartbeat:           heartbeat,
		widgetEnabled:               widgetEnabled,
		agentAPIStreamFinishGrace:   150 * time.Millisecond,
		agentAPIStreamStallReanchor: 8 * time.Second,
	}
}

// runMetricsSampler 每 15 秒采样本节点人类/agent WS 连接数到 Prometheus 指标。
func (s *Server) runMetricsSampler() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		metrics.WSActiveConnections.WithLabelValues("human").Set(float64(s.hub.CountConns()))
		if s.agentAPIMgr != nil {
			metrics.WSActiveConnections.WithLabelValues("agent").Set(float64(s.agentAPIMgr.CountConns()))
		}
	}
}

func (s *Server) Start() error {
	return s.serve(nil)
}

// Serve runs on an already-bound listener. E2E tests use this to hold the
// port across heavy startup work and avoid the reserve→close→Listen race
// under parallel `go test ./...` packages.
func (s *Server) Serve(ln net.Listener) error {
	if ln == nil {
		return errors.New("ws: nil listener")
	}
	return s.serve(ln)
}

func (s *Server) serve(ln net.Listener) error {
	// Start Redis Pub/Sub subscriber for cross-node delivery
	s.stopRedisSub = dispatch.StartRedisSub(s.nodeID, s.hub)
	s.agentAPIMgr = wsagentapi.NewManager(s.allowedWebOrigins, s.agentAPIHeartbeat, s.handleAgentAPISend, s.handleAgentAPIStreamChunk, s.handleAgentAPIDeleteMsg, s.handleAgentAPIReactMsg)
	s.agentAPIMgr.SetNodeID(s.nodeID)
	// Agent state generations must be coordinated before a websocket can be
	// attached. If Redis allocation fails, ServeWS rejects authentication
	// instead of publishing an unordered online state.
	s.agentAPIMgr.SetConnectionEpochAllocator(handler.ReserveAgentConnectionEpoch)

	// Initialize adapter registry and register built-in adapters
	registry := agentadapter.NewRegistry()
	adapters := []agentadapter.AgentAdapter{
		openclaw.NewAdapter(),
		acp.NewAdapter(),
		claude.NewAdapter(),
		codex.NewAdapter(),
		cursor.NewAdapter(),
		deepseek.NewAdapter(),
		pi.NewAdapter(),
		gemini.NewAdapter(),
		hermes.NewAdapter(),
		qwen.NewAdapter(),
		openhuman.NewAdapter(),
		reasonix.NewAdapter(),
		codewhale.NewAdapter(),
		opencode.NewAdapter(),
		kiro.NewAdapter(),
		copilot.NewAdapter(),
		agy.NewAdapter(),
		kimi.NewAdapter(),
	}

	// Wrap adapters with logging decorator
	s.adapterLogMgr = adapterlog.NewManager(adapterlog.ConfigFromEnv())
	for _, a := range adapters {
		reg := agentadapter.NewLoggingAdapter(a, s.adapterLogMgr)
		registry.Register(reg)
	}
	s.agentAPIMgr.SetAdapterRegistry(registry)

	absRoot, _ := filepath.Abs(s.adapterLogMgr.LogRoot())
	for _, a := range adapters {
		logger.L.Infof("adapter log dir: %s/%s", absRoot, a.Family())
	}

	// Register client types derived from adapter families
	for _, a := range adapters {
		model.RegisterClientType(a.Family())
	}

	s.agentAPIMgr.SetDeliveryStatusHandler(s.notifyAgentDeliveryStatus)
	s.agentAPIMgr.SetOutputStatusHandler(s.notifyAgentOutputStatus)
	s.agentAPIMgr.SetEventLifecyclePacketHandler(s.notifyAgentEventLifecyclePacket)
	s.agentAPIMgr.SetSessionActivityHandler(s.handleAgentAPISessionActivitySet)
	s.agentAPIMgr.SetEditMsgHandler(s.handleAgentAPIEditMsg)
	s.agentAPIMgr.SetMediaUploadInitHandler(s.handleAgentAPIMediaUploadInit)
	s.agentAPIMgr.SetAgentStateHandler(s.notifyAgentStateSync)
	s.agentAPIMgr.SetStreamDisconnectHandler(s.handleAgentAPIStreamDisconnect)
	s.agentAPIMgr.SetForceFinalizeStreamsHandler(s.handleForceFinalizeSessionStreams)
	s.agentAPIMgr.SetHumanWsSendFn(func(ownerID int64, data []byte) {
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			return
		}
		s.deliverMcpFrameToOwner(ownerID, &pkt)
	})
	wsagentapi.SetGlobal(s.agentAPIMgr)
	s.initAgentToolbarService()
	service.SetAgentChannelBridge(serviceAgentChannelBridge{})

	// Heal session-agent states left non-terminal by a previous ws crash/restart:
	// the in-memory run is gone but the persisted state never flipped back. Runs
	// still alive in the cross-node durable record are preserved. Async so it
	// never blocks startup.
	// 走 Manager 的后台工作组：它要读写 DB，裸 goroutine 会活过关停（收尾后仍在读已关闭的库）。
	s.agentAPIMgr.GoBackground(s.agentAPIMgr.ReconcileLeakedSessionStatesOnStartup)

	// 僵尸 running 周期清扫：connector/agent 在任务结束后、终态上报前崩溃或重启时，
	// chat_states 行会永远停在 running（终态只由 event_result 写入，超时仅观测）。
	// 清扫器以保守条件兜底 settle 并广播终态，让前端的"正在输入"指示器得以清除。
	s.agentAPIMgr.StartStaleRunSweeper()

	// 周期性采样本节点 WS 连接数，写入 Prometheus 指标。WS 真实负载看连接数，
	// 而非 CPU——CPU 不高也可能连接已接近上限。
	go s.runMetricsSampler()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc(s.agentAPIPath, s.agentAPIMgr.ServeWS)
	mux.HandleFunc("/v1/widget/ws", s.handleWidgetWS)
	mux.HandleFunc("/v1/webhook/incoming/", s.handleWebhookIncoming)
	mux.HandleFunc("/v1/notify-callback", s.handleNotifyCallback)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(version.Get())
	})
	// /metrics 无鉴权会暴露内部指标；与 API 服务对齐，仅放行内网来源。
	mux.Handle("/metrics", middleware.InternalOnlyHTTP(metrics.Handler()))
	if s.pprofSecret != "" {
		mux.HandleFunc("/debug/pprof/", s.pprofAuth(pprof.Index))
		mux.HandleFunc("/debug/pprof/cmdline", s.pprofAuth(pprof.Cmdline))
		mux.HandleFunc("/debug/pprof/profile", s.pprofAuth(pprof.Profile))
		mux.HandleFunc("/debug/pprof/symbol", s.pprofAuth(pprof.Symbol))
		mux.HandleFunc("/debug/pprof/trace", s.pprofAuth(pprof.Trace))
	}
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		dbOk, redisOk, natsOk := store.ReadyCheck(3 * time.Second)
		if dbOk && redisOk {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"db":%t,"redis":%t,"nats":%t}`, dbOk, redisOk, natsOk)
	})

	addr := fmt.Sprintf(":%d", s.port)
	if ln != nil {
		addr = ln.Addr().String()
		if ta, ok := ln.Addr().(*net.TCPAddr); ok {
			s.port = ta.Port
		}
	}
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	s.srvMu.Lock()
	s.srv = srv
	s.srvMu.Unlock()

	logger.L.Infof("ws server starting on port %d, node=%s", s.port, s.nodeID)
	logger.L.Infof("agent api ws endpoint enabled on path %s", s.agentAPIPath)
	var err error
	if ln != nil {
		err = srv.Serve(ln)
	} else {
		err = srv.ListenAndServe()
	}
	s.cleanupRuntime()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(req *http.Request) bool {
			return security.IsAllowedWebOrigin(req, s.allowedWebOrigins)
		},
	}
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		origin := r.Header.Get("Origin")
		logger.L.Errorf("ws upgrade error: %v, origin=%s, host=%s, remote=%s", err, origin, r.Host, r.RemoteAddr)
		return
	}

	conn := NewConn(wsConn)

	// 3-second auth timeout
	go func() {
		time.Sleep(3 * time.Second)
		if !conn.authed {
			logger.L.Warnf("auth timeout, closing connection")
			wsConn.Close()
		}
	}()

	go conn.WritePump()
	conn.ReadPump(s.hub)
}

func (s *Server) pprofAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// token 只从头读取（Authorization: Bearer / X-Pprof-Token），不走 URL
		// query——query 会落进访问日志与代理日志。比较用常量时间，防时序侧信道。
		token := strings.TrimSpace(r.Header.Get("X-Pprof-Token"))
		if token == "" {
			if auth := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(auth, "Bearer ") {
				token = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			}
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.pprofSecret)) != 1 {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

func (s *Server) Shutdown() {
	if s.adapterLogMgr != nil {
		s.adapterLogMgr.Close()
	}
	s.srvMu.Lock()
	srv := s.srv
	s.srvMu.Unlock()

	if srv == nil {
		s.cleanupRuntime()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	s.cleanupRuntime()
}

// NotifyUser 向指定用户的所有在线连接推送 WS 包，供 Call Controller 回调使用。
func (s *Server) NotifyUser(userID int64, cmd string, payload any) {
	handler.BroadcastCallToUser(s.hub, context.Background(), userID, cmd, payload)
}

func (s *Server) cleanupRuntime() {
	s.cleanupOnce.Do(func() {
		// 关停顺序三步，次序是有讲究的：
		// 1) 停 Redis 订阅——它是跨节点指令的入口，不先掐断，关停期间到来的广播
		//    还会继续往正在关停的 Manager 里灌新活。
		if s.stopRedisSub != nil {
			s.stopRedisSub()
		}
		// 2) 关 agent API：断开在服务的 agent 连接、停 Manager 名下的定时器、
		//    等后台协程退出。否则它们会活过 http.Server.Shutdown（劫持的 WS 连接
		//    不在其等待范围内），在进程收尾后继续读写 DB。
		if s.agentAPIMgr != nil {
			s.agentAPIMgr.Shutdown()
		}
		// 3) 最后停流式收尾定时器。必须在 Shutdown 之后：这些定时器由 WS 读循环
		//    处理最后一个 stream_chunk 时现场挂上，只有等读循环全部退出，才不会
		//    出现「刚清空又被挂上一个、之后无人停」的漏网。
		//    放在 Shutdown 之前反而不安全；而 Shutdown 等待期间 DB 仍然活着，
		//    这期间定时器触发去写库没有问题。
		s.stopAgentAPIStreamFinishTimers()
		// Stop 只拦得住还没触发的；已经触发、正在跑的收尾回调要等它写完，
		// 否则它会活过关停，去写已经关掉的库（流式消息也就永远收不了尾）。
		s.waitAgentAPIStreamFinalizers()
		if s.agentAPIMgr != nil && wsagentapi.GetGlobal() == s.agentAPIMgr {
			wsagentapi.SetGlobal(nil)
		}
		agenttoolbar.SetGlobal(nil)
		service.SetAgentChannelBridge(nil)
	})
}

func (s *Server) notifyAgentDeliveryStatus(payload protocol.AgentDeliveryStatusPayload) {
	if payload.OwnerID <= 0 {
		return
	}
	ctx := context.Background()
	if isAgentDeliveryTerminalStatus(payload.Status) &&
		strings.TrimSpace(payload.SessionID) != "" &&
		strings.TrimSpace(payload.EventID) != "" {
		if err := handler.ClearSessionActivityByRef(ctx, s.hub, payload.SessionID, "", payload.EventID); err != nil {
			logger.L.Warnf(
				"clear session activity by event failed session=%s owner=%d event=%s status=%s: %v",
				payload.SessionID,
				payload.OwnerID,
				payload.EventID,
				payload.Status,
				err,
			)
		}
	}
	handler.RecordAgentDeliveryStatus(ctx, payload)

	for _, conn := range s.hub.GetUserConns(payload.OwnerID) {
		conn.SendPayload(protocol.CmdAgentDeliveryStatus, conn.NextSeq(), payload)
	}

	data, _ := json.Marshal(map[string]any{
		"user_id": payload.OwnerID,
		"cmd":     protocol.CmdAgentDeliveryStatus,
		"payload": payload,
	})

	routeKey := fmt.Sprintf("im:ws:route:%d", payload.OwnerID)
	devices, err := store.RDB.HGetAll(ctx, routeKey).Result()
	if err != nil {
		return
	}

	publishedNodes := make(map[string]bool)
	for _, nodeID := range devices {
		if nodeID == s.nodeID || publishedNodes[nodeID] {
			continue
		}
		publishedNodes[nodeID] = true
		if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", nodeID), string(data)).Err(); err != nil {
			logger.L.Warnf("publish cross-node event to %s failed: %v", nodeID, err)
		}
	}
}

// deliverMcpFrameToOwner 把 MCP 下行帧投递给 owner 的唯一一个设备连接。
//
// MCP 是点对点语义：同一时刻只能有一个 APP MCP Server 实例应答，否则多设备
// 会对同一 JSON-RPC id 重复回帧、破坏协议。多 WS 副本部署下，agent 连接与
// owner 的 APP 连接可能落在不同节点，因此不能只查本地连接。这里以 owner 路由表
// 中字典序最小的 deviceID 作为全局唯一目标——所有 ws 节点都会算出同一个设备，
// 从而保证唯一性；目标设备在本节点则直投，在它节点则带 target_device_id 跨节点
// 转发。APP 内置 MCP Server 是无状态请求/应答，目标设备上下线导致的目标漂移
// 不影响单帧正确性。
func (s *Server) deliverMcpFrameToOwner(ownerID int64, pkt *protocol.Packet) {
	if ownerID <= 0 {
		return
	}
	targetDevice, targetNode := s.pickMcpTargetDevice(ownerID)
	if targetDevice != "" {
		if targetNode == s.nodeID {
			for _, conn := range s.hub.GetUserConns(ownerID) {
				if conn.GetDeviceID() == targetDevice {
					conn.SendPacket(pkt)
					return
				}
			}
			logger.L.Warnf("[mcp-frame] target device %s routed to this node but no live conn owner=%d, drop", targetDevice, ownerID)
			return
		}
		envelope, err := json.Marshal(map[string]any{
			"user_id":          ownerID,
			"cmd":              pkt.Cmd,
			"payload":          pkt.Payload,
			"target_device_id": targetDevice,
		})
		if err != nil {
			return
		}
		if err := store.RDB.Publish(context.Background(), fmt.Sprintf("chan:%s", targetNode), envelope).Err(); err != nil {
			logger.L.Warnf("[mcp-frame] publish cross-node to %s failed owner=%d: %v", targetNode, ownerID, err)
		}
		return
	}
	// 路由不可用/为空/无存活设备：兜底本地任一连接（单节点或路由尚未建立场景）
	conns := s.hub.GetUserConns(ownerID)
	if len(conns) == 0 {
		logger.L.Warnf("[mcp-frame] owner=%d has no online device, drop downstream frame", ownerID)
		return
	}
	conns[0].SendPacket(pkt)
}

// pickMcpTargetDevice 选 owner 的全局唯一 MCP 目标设备：路由表中字典序最小的
// “存活”设备。route hash field 无 TTL，节点崩溃会残留僵尸条目，因此用 alive key
// （心跳续期、90s TTL）过滤，避免把帧投向已死节点而真实在线的设备收不到。所有
// ws 节点对同一 owner 都会算出同一台设备，从而保证点对点唯一性。返回 ("","")
// 表示无可用路由。
func (s *Server) pickMcpTargetDevice(ownerID int64) (string, string) {
	if store.RDB == nil {
		return "", ""
	}
	ctx := context.Background()
	devices, err := store.RDB.HGetAll(ctx, fmt.Sprintf("im:ws:route:%d", ownerID)).Result()
	if err != nil || len(devices) == 0 {
		return "", ""
	}
	deviceIDs := make([]string, 0, len(devices))
	for dev := range devices {
		deviceIDs = append(deviceIDs, dev)
	}
	sort.Strings(deviceIDs)
	// 优先选「前台」存活设备：用户当前正盯着的那台，避免把 open_page 这类 UI 指令
	// 投到用户没在看的设备上。无前台标记时回退字典序最小的存活设备（保持原行为）。
	// 字典序与前台判定都基于 Redis 共享数据，多 ws 节点对同一 owner 算出同一台，
	// 维持点对点唯一性。
	var fallbackDev, fallbackNode string
	for _, dev := range deviceIDs {
		aliveKey := fmt.Sprintf("im:ws:alive:%d:%s", ownerID, dev)
		if n, aErr := store.RDB.Exists(ctx, aliveKey).Result(); aErr != nil || n == 0 {
			continue
		}
		if fallbackDev == "" {
			fallbackDev, fallbackNode = dev, devices[dev]
		}
		stateKey := fmt.Sprintf("im:ws:appstate:%d:%s", ownerID, dev)
		if state, sErr := store.RDB.Get(ctx, stateKey).Result(); sErr == nil && state == appStateForegroundValue {
			return dev, devices[dev]
		}
	}
	return fallbackDev, fallbackNode
}

// appStateForegroundValue 是设备前台状态在 Redis 中的标记值（与 conn.appStateString 对齐）。
const appStateForegroundValue = "foreground"

// publishDeviceAppState 把设备前台/后台状态写入 Redis，供跨节点 pickMcpTargetDevice
// 选择「用户当前在用的设备」。TTL 10 分钟，用户切前台/后台或重连时刷新；过期则
// pickMcpTargetDevice 退回字典序选择，不影响在线判定。
func publishDeviceAppState(userID int64, deviceID, state string) {
	if store.RDB == nil || userID <= 0 || deviceID == "" {
		return
	}
	store.RDB.Set(context.Background(),
		fmt.Sprintf("im:ws:appstate:%d:%s", userID, deviceID), state, 10*time.Minute)
}

// deleteDeviceAppState 设备下线时清理其前台状态标记。
func deleteDeviceAppState(userID int64, deviceID string) {
	if store.RDB == nil || userID <= 0 || deviceID == "" {
		return
	}
	store.RDB.Del(context.Background(),
		fmt.Sprintf("im:ws:appstate:%d:%s", userID, deviceID))
}

func isAgentDeliveryTerminalStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case protocol.AgentDeliveryStatusResponded, protocol.AgentDeliveryStatusCanceled, protocol.AgentDeliveryStatusTimeout, protocol.AgentDeliveryStatusFailed:
		return true
	default:
		return false
	}
}

func (s *Server) notifyAgentOutputStatus(payload protocol.AgentOutputStatusPayload) {
	if payload.OwnerID <= 0 {
		return
	}

	ctx := context.Background()
	s.fanoutUserPacket(ctx, payload.OwnerID, protocol.CmdAgentOutputStatus, payload)

	// Terminal states: proactively clear composing activity so the frontend
	// indicator disappears in lockstep with the toolbar stop button.
	switch payload.State {
	case protocol.AgentOutputStateCompleted,
		protocol.AgentOutputStateFailed,
		protocol.AgentOutputStateStopped:
		if s.agentAPIMgr != nil && s.agentAPIMgr.ShouldClearComposingForTerminal(
			payload.RunID,
			payload.SessionID,
			payload.OwnerID,
			payload.AgentID,
		) {
			s.clearAgentAPIComposing(ctx, payload.AgentID, payload.OwnerID, strings.TrimSpace(payload.SessionID))
		}
	}

	if svc := agenttoolbar.GetGlobal(); svc != nil && payload.OwnerID > 0 && strings.TrimSpace(payload.SessionID) != "" {
		switch payload.State {
		case protocol.AgentOutputStateQueued,
			protocol.AgentOutputStateCompleted,
			protocol.AgentOutputStateFailed,
			protocol.AgentOutputStateStopped,
			protocol.AgentOutputStateStopping:
			svc.InvalidateSession(ctx, payload.OwnerID, payload.SessionID)
		}
		if err := svc.RefreshSessionForAgent(ctx, payload.OwnerID, payload.SessionID, payload.AgentID, "agent_output_status"); err != nil {
			logger.L.Warnf("refresh toolbar by output status failed owner=%d session=%s state=%s run_id=%s err=%v", payload.OwnerID, payload.SessionID, payload.State, payload.RunID, err)
		} else {
			logger.L.Infof(
				"[toolbar-stop] toolbar refreshed by output_status owner=%d session=%s state=%s run_id=%s agent=%d",
				payload.OwnerID, payload.SessionID, payload.State, payload.RunID, payload.AgentID,
			)
		}
	}
}

func (s *Server) notifyAgentEventLifecyclePacket(ownerID int64, cmd string, payload json.RawMessage) {
	if ownerID <= 0 || strings.TrimSpace(cmd) == "" {
		return
	}
	ctx := context.Background()
	s.fanoutUserPacket(ctx, ownerID, cmd, payload)
}

func (s *Server) notifyAgentStateSync(ownerID int64, payload protocol.AgentStateSyncPayload) {
	if ownerID <= 0 || payload.AgentID <= 0 {
		return
	}
	if !handler.RecordAgentState(context.Background(), ownerID, payload) {
		return
	}

	ctx := context.Background()
	s.fanoutUserPacket(ctx, ownerID, protocol.CmdAgentStateSync, payload)
}

func (s *Server) handleAgentAPISessionActivitySet(
	ctx context.Context,
	agentID int64,
	ownerID int64,
	payload protocol.SessionActivitySetPayload,
) error {
	if err := handler.SetSessionActivityFromAgentAPI(ctx, s.hub, agentID, ownerID, payload); err != nil {
		return err
	}
	// Refresh toolbar on every active composing event (start + tick) so the
	// stop button appears even when no active run is tracked.  RefreshSession
	// de-duplicates via snapshot comparison, so repeated ticks are cheap.
	// Also refresh when composing stops (empty-queue clear / explicit stop) so
	// the stop button and queue badge drop in lockstep with the indicator.
	if payload.Kind == protocol.SessionActivityKindComposing {
		if svc := agenttoolbar.GetGlobal(); svc != nil && ownerID > 0 && strings.TrimSpace(payload.SessionID) != "" {
			_ = svc.RefreshSessionForAgent(context.Background(), ownerID, payload.SessionID, agentID, "composing_activity")
		}
	}
	return nil
}

// AgentAPISend exposes the agent message send pipeline for internal use
// (e.g., call_summary system messages, call segment messages).
func (s *Server) AgentAPISend(ctx context.Context, req wsagentapi.SendMessageReq) (*wsagentapi.SendMessageResult, error) {
	return s.handleAgentAPISend(ctx, req)
}

// SetOnUserAllDevicesOffline 设置用户全设备离线时的回调（连接 call controller）。
// 同时自动清理该用户的排队条目（若存在）。
func (s *Server) SetOnUserAllDevicesOffline(fn func(userID int64)) {
	s.hub.OnUserAllDevicesOffline = func(userID int64) {
		fn(userID)
		handler.RemoveVisitorFromQueue(userID)
	}
}

// SetOnUserRegistered 设置用户设备注册后的回调（用于重推活跃通话通知）。
func (s *Server) SetOnUserRegistered(fn func(userID int64)) {
	s.hub.OnUserRegistered = fn
}

// GetHub 返回 hub 实例（实现 handler.HubInterface）。
func (s *Server) GetHub() handler.HubInterface {
	return s.hub
}

// stopAgentAPIStreamFinishTimers 封口并停掉所有还没触发的流式收尾定时器。
//
// ⛔ 计数是在排定定时器的那一刻就 +1 的（见 scheduleAgentAPIStreamFinalize），
// 不是等它触发才加，所以 Stop() 成功拦下的每一个都必须由这里代它 -1——
// 否则 waitAgentAPIStreamFinalizers 会盯着一个永远不会有人减掉的计数傻等到
// 超时：只要关停时有一条流式消息的宽限期还没到，就要白等满 10 秒才肯放行。
// Stop() 返回 false 说明定时器已经触发，回调自己的 defer 会减，这里不用管。
func (s *Server) stopAgentAPIStreamFinishTimers() {
	s.agentAPIStreamFinishMu.Lock()
	defer s.agentAPIStreamFinishMu.Unlock()
	s.agentAPIStreamFinishClosing = true
	for key, timer := range s.agentAPIStreamFinishTimers {
		if timer != nil && timer.Stop() {
			s.agentAPIStreamFinishActive--
		}
		delete(s.agentAPIStreamFinishTimers, key)
	}
}

// waitAgentAPIStreamFinalizers 等已经触发、正在跑的流式收尾回调退出。
// 超时只是不再干等——关停不能被一个卡住的回调拖死。
func (s *Server) waitAgentAPIStreamFinalizers() {
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.agentAPIStreamFinishMu.Lock()
		active := s.agentAPIStreamFinishActive
		s.agentAPIStreamFinishMu.Unlock()
		if active == 0 {
			return
		}
		if time.Now().After(deadline) {
			logger.L.Warnf("ws shutdown timed out: %d agent api stream finalizers still running", active)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

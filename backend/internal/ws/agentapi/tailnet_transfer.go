package agentapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/tailnet"
)

const (
	// tailnetFileCapability 是 Node 声明支持 Tailnet 直传的能力标识。
	tailnetFileCapability = "tailnet_file_v1"

	// tailnetTransferTimeout 是等待 tailnet_file_ready 的超时时间。
	tailnetTransferTimeout = 30 * time.Second

	// tailnetHTTPServerTimeout 是临时 HTTP server 的最大存活时间。
	tailnetHTTPServerTimeout = 5 * time.Minute

	// tailnetSmallFileThreshold 小于此大小走 base64（不值得建 HTTP 连接）。
	tailnetSmallFileThreshold = 1 * 1024 * 1024 // 1 MB

	// tailnetMaxBase64FileSize base64 模式的最大文件大小。
	tailnetMaxBase64FileSize = 16 * 1024 * 1024 // 16 MB
)

// TailnetFileServePayload 是 Gateway 发给 server 端的 tailnet_file_serve 命令 payload。
type TailnetFileServePayload struct {
	ActionID  string `json:"action_id"`
	FilePath  string `json:"file_path"`
	Direction string `json:"direction"` // "download" 或 "upload"
	Token     string `json:"token"`
	AllowedIP string `json:"allowed_ip"` // 允许访问本 server 的 client 端 Tailnet IP
	TimeoutMs int64  `json:"timeout_ms"`
}

// TailnetFileReadyPayload 是源端回复 Gateway 的 tailnet_file_ready 命令 payload。
type TailnetFileReadyPayload struct {
	ActionID    string `json:"action_id"`
	URL         string `json:"url"`
	FileSize    int64  `json:"file_size,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

// TailnetFileFetchPayload 是 Gateway 发给 client 端的 tailnet_file_fetch 命令 payload。
type TailnetFileFetchPayload struct {
	ActionID  string `json:"action_id"`
	Direction string `json:"direction"` // "download" → GET；"upload" → PUT
	URL       string `json:"url"`
	Token     string `json:"token"`
	FilePath  string `json:"file_path"`
	FileSize  int64  `json:"file_size,omitempty"`
}

// TailnetFileDonePayload 是任一端发给 Gateway 的 tailnet_file_done 命令 payload。
type TailnetFileDonePayload struct {
	ActionID         string `json:"action_id"`
	Status           string `json:"status"` // "ok" | "failed" | "timeout"
	BytesTransferred int64  `json:"bytes_transferred,omitempty"`
	ErrorMsg         string `json:"error_msg,omitempty"`
}

// pendingTransfer 记录一次进行中的 Tailnet 传输会话。
type pendingTransfer struct {
	actionID      string
	serverAgentID int64 // server 端（peer），唯一允许发 tailnet_file_ready 的来源
	clientAgentID int64 // client 端（initiator），唯一允许发 tailnet_file_done 的来源
	readyCh       chan TailnetFileReadyPayload
	doneCh        chan TailnetFileDonePayload
	createdAt     time.Time
}

// TailnetTransferCoordinator 负责协调 Tailnet 直传流程。
// 它持有对 Manager 的引用，用于查找连接和发送 WS 命令。
type TailnetTransferCoordinator struct {
	manager *Manager

	mu      sync.Mutex
	pending map[string]*pendingTransfer // actionID -> pendingTransfer
}

// newTailnetTransferCoordinator 创建协调器实例。
func newTailnetTransferCoordinator(m *Manager) *TailnetTransferCoordinator {
	return &TailnetTransferCoordinator{
		manager: m,
		pending: make(map[string]*pendingTransfer),
	}
}

// CanDirectTransfer 判断两个 agentID 是否满足 Tailnet 直传条件。
// ownerID 是发起请求的连接所属 owner：agent 共享下同一 agent 有多条连接，
// 直传是宿主机级、且入口已校验双方同 owner，故两端均按该 owner 精确选连接。
func (c *TailnetTransferCoordinator) CanDirectTransfer(srcAgentID, dstAgentID, ownerID int64) bool {
	c.manager.mu.RLock()
	srcConn := c.manager.connByOwnerLocked(srcAgentID, ownerID)
	dstConn := c.manager.connByOwnerLocked(dstAgentID, ownerID)
	c.manager.mu.RUnlock()

	if srcConn == nil || dstConn == nil {
		return false
	}
	if !hasDeclaredName(srcConn.capabilities, tailnetFileCapability) {
		return false
	}
	if !hasDeclaredName(dstConn.capabilities, tailnetFileCapability) {
		return false
	}
	return tailnet.SameTailnet(srcConn.tailnetIP, dstConn.tailnetIP)
}

// StartTransfer 发起一次 Tailnet 直传。
// initiator 是主动方（client 端，发起 HTTP 请求）；peer 是被动方（server 端，起 HTTP server）。
// direction（相对 initiator）："download"=从 peer 拉取到本地；"upload"=把本地推送到 peer。
// localPath 是 initiator 本地路径，remotePath 是 peer 路径。
// 两个方向下 server 端恒为 peer、client 端恒为 initiator，仅 HTTP 方法不同。
// 注意：本方法会阻塞等待 ready，应在独立协程中调用。
func (c *TailnetTransferCoordinator) StartTransfer(initiatorAgentID, peerAgentID, ownerID int64, direction, localPath, remotePath string) (string, error) {
	dir := strings.TrimSpace(direction)
	if dir != "download" && dir != "upload" {
		return "", fmt.Errorf("tailnet: invalid direction %q", direction)
	}

	// 按发起请求的 owner 精确选两端连接（agent 共享多连接物理隔离）。
	c.manager.mu.RLock()
	clientConn := c.manager.connByOwnerLocked(initiatorAgentID, ownerID) // client 端 = 发起方
	serverConn := c.manager.connByOwnerLocked(peerAgentID, ownerID)      // server 端 = 对端
	c.manager.mu.RUnlock()

	if clientConn == nil {
		return "", fmt.Errorf("tailnet: initiator agent %d not connected", initiatorAgentID)
	}
	if serverConn == nil {
		return "", fmt.Errorf("tailnet: peer agent %d not connected", peerAgentID)
	}

	actionID := buildTransferActionID(initiatorAgentID, peerAgentID)
	secret := []byte(config.C.JWT.Secret)

	// token: src_node=server 端（提供 URL）, dst_node=client 端；file 绑定 server 端路径
	token, err := tailnet.IssueTransferToken(secret, actionID, serverConn.clientID, clientConn.clientID, remotePath, dir)
	if err != nil {
		return "", fmt.Errorf("tailnet: issue token failed: %w", err)
	}

	// 注册 pending
	pt := &pendingTransfer{
		actionID:      actionID,
		serverAgentID: peerAgentID,
		clientAgentID: initiatorAgentID,
		readyCh:       make(chan TailnetFileReadyPayload, 1),
		doneCh:        make(chan TailnetFileDonePayload, 1),
		createdAt:     time.Now(),
	}
	c.mu.Lock()
	c.pending[actionID] = pt
	c.mu.Unlock()

	// 1. 通知 server 端（对端）启动 HTTP server
	servePayload := TailnetFileServePayload{
		ActionID:  actionID,
		FilePath:  remotePath,
		Direction: dir,
		Token:     token,
		AllowedIP: clientConn.tailnetIP, // 只允许发起方（client 端）访问
		TimeoutMs: tailnetHTTPServerTimeout.Milliseconds(),
	}
	if !serverConn.sendPayload("tailnet_file_serve", serverConn.nextSeq(), servePayload) {
		c.removePending(actionID)
		return "", fmt.Errorf("tailnet: send tailnet_file_serve to peer agent %d failed", peerAgentID)
	}

	// 2. 等待 server 端回复 tailnet_file_ready
	var readyPayload TailnetFileReadyPayload
	select {
	case readyPayload = <-pt.readyCh:
	case <-c.manager.stopping():
		// 本节点关停：对端连接已断，ready 不会来了，别把 Shutdown 拖住。
		// 同样先看 ready 是不是已经到了，避免与关停信号同时就绪时被随机丢掉。
		select {
		case readyPayload = <-pt.readyCh:
		default:
			c.removePending(actionID)
			return "", fmt.Errorf("tailnet: node shutting down action_id=%s", actionID)
		}
	case <-time.After(tailnetTransferTimeout):
		c.removePending(actionID)
		return "", fmt.Errorf("tailnet: timeout waiting for tailnet_file_ready from peer agent %d", peerAgentID)
	}

	// 3. 通知 client 端（发起方）发起 HTTP 请求
	fetchPayload := TailnetFileFetchPayload{
		ActionID:  actionID,
		Direction: dir,
		URL:       readyPayload.URL,
		Token:     token,
		FilePath:  localPath,
		FileSize:  readyPayload.FileSize,
	}
	if !clientConn.sendPayload("tailnet_file_fetch", clientConn.nextSeq(), fetchPayload) {
		c.removePending(actionID)
		return "", fmt.Errorf("tailnet: send tailnet_file_fetch to initiator agent %d failed", initiatorAgentID)
	}

	logger.L.Infof("tailnet transfer started action_id=%s initiator=%d peer=%d dir=%s", actionID, initiatorAgentID, peerAgentID, dir)
	return actionID, nil
}

// HandleFileReady 处理 server 端发来的 tailnet_file_ready。
// 校验来源必须是该会话的 server 端，防止他人伪造 ready 注入恶意 URL。
func (c *TailnetTransferCoordinator) HandleFileReady(fromAgentID int64, payload TailnetFileReadyPayload) {
	c.mu.Lock()
	pt := c.pending[payload.ActionID]
	c.mu.Unlock()
	if pt == nil {
		logger.L.Warnf("tailnet: received tailnet_file_ready for unknown action_id=%s", payload.ActionID)
		return
	}
	if fromAgentID != pt.serverAgentID {
		logger.L.Warnf("tailnet: tailnet_file_ready from unexpected agent=%d expected server=%d action_id=%s", fromAgentID, pt.serverAgentID, payload.ActionID)
		return
	}
	select {
	case pt.readyCh <- payload:
	default:
	}
}

// HandleFileDone 处理 client 端发来的 tailnet_file_done。
// 校验来源必须是该会话的 client 端，防止他人伪造 done 提前中断。
// 只投递结果，不在此清理 pending；清理统一交给 WaitDone（读到结果或超时），
// 避免 done 早于 WaitDone 到达时会话被提前删除导致 WaitDone 误报失败。
func (c *TailnetTransferCoordinator) HandleFileDone(fromAgentID int64, payload TailnetFileDonePayload) {
	c.mu.Lock()
	pt := c.pending[payload.ActionID]
	c.mu.Unlock()
	if pt == nil {
		return
	}
	if fromAgentID != pt.clientAgentID {
		logger.L.Warnf("tailnet: tailnet_file_done from unexpected agent=%d expected client=%d action_id=%s", fromAgentID, pt.clientAgentID, payload.ActionID)
		return
	}
	select {
	case pt.doneCh <- payload:
	default:
	}
	logger.L.Infof("tailnet transfer done action_id=%s status=%s bytes=%d", payload.ActionID, payload.Status, payload.BytesTransferred)
}

// WaitDone 等待传输完成，返回结果 payload；无论成功或超时都清理 pending。
func (c *TailnetTransferCoordinator) WaitDone(actionID string, timeout time.Duration) (TailnetFileDonePayload, error) {
	c.mu.Lock()
	pt := c.pending[actionID]
	c.mu.Unlock()
	if pt == nil {
		return TailnetFileDonePayload{}, fmt.Errorf("tailnet: no pending transfer for action_id=%s", actionID)
	}
	select {
	case done := <-pt.doneCh:
		c.removePending(actionID)
		return done, nil
	case <-c.manager.stopping():
		// 本节点关停：两端连接都已断开，传输不可能再完成；不能让这条最长 5 分钟的
		// 等待把 Shutdown 拖死（超时放弃它就成了活过关停的野协程）。
		// 先看 done 是不是已经到了——关停信号与它同时就绪时 select 会随机选，
		// 直接判失败就会把一次已经成功的传输误报成失败。
		select {
		case done := <-pt.doneCh:
			c.removePending(actionID)
			return done, nil
		default:
		}
		c.removePending(actionID)
		return TailnetFileDonePayload{Status: "failed", ErrorMsg: "node shutting down"},
			fmt.Errorf("tailnet: node shutting down action_id=%s", actionID)
	case <-time.After(timeout):
		c.removePending(actionID)
		return TailnetFileDonePayload{Status: "timeout"}, fmt.Errorf("tailnet: transfer timeout action_id=%s", actionID)
	}
}

func (c *TailnetTransferCoordinator) removePending(actionID string) {
	c.mu.Lock()
	delete(c.pending, actionID)
	c.mu.Unlock()
}

// tailnetActionSeq 为 action_id 提供进程内单调序号，避免同毫秒并发冲突。
var tailnetActionSeq atomic.Int64

// buildTransferActionID 生成传输会话 ID。
func buildTransferActionID(srcAgentID, dstAgentID int64) string {
	return fmt.Sprintf("tf:%d:%d:%d:%d", srcAgentID, dstAgentID, time.Now().UnixMilli(), tailnetActionSeq.Add(1))
}

// TransferMode 表示文件传输模式。
type TransferMode string

const (
	TransferModeTailnet     TransferMode = "tailnet_direct"
	TransferModeRelay       TransferMode = "gateway_relay"
	TransferModeUnavailable TransferMode = "unavailable"
)

// ResolveTransferMode 根据文件大小和双方能力决定传输模式。
// fileSize<=0 表示大小未知（download 时常见）：跳过小文件判断，可直传则优先直传，
// 因为未知大小可能超过 base64 的 16MB 上限，直传无此限制更安全。
func (c *TailnetTransferCoordinator) ResolveTransferMode(initiatorAgentID, peerAgentID, ownerID int64, fileSize int64) TransferMode {
	if fileSize > 0 && fileSize < tailnetSmallFileThreshold {
		// 小文件直接走 base64，不值得建 HTTP 连接
		return TransferModeRelay
	}
	canDirect := c.CanDirectTransfer(initiatorAgentID, peerAgentID, ownerID)
	if fileSize > tailnetMaxBase64FileSize {
		// 超过 16MB：base64 放不下，必须直传；不能直传则不可行
		if canDirect {
			return TransferModeTailnet
		}
		return TransferModeUnavailable
	}
	// 1MB ~ 16MB：优先直传，不可用则回退 base64
	if canDirect {
		return TransferModeTailnet
	}
	return TransferModeRelay
}

// handleTailnetFileReady 是 Manager 的 WS 命令处理入口（tailnet_file_ready）。
func (m *Manager) handleTailnetFileReady(conn *agentConn, payload json.RawMessage) {
	var p TailnetFileReadyPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		logger.L.Warnf("tailnet_file_ready invalid payload agent=%d err=%v", conn.agentID, err)
		return
	}
	if strings.TrimSpace(p.ActionID) == "" || strings.TrimSpace(p.URL) == "" {
		logger.L.Warnf("tailnet_file_ready missing fields agent=%d", conn.agentID)
		return
	}
	m.tailnetCoordinator.HandleFileReady(conn.agentID, p)
}

// handleTailnetFileDone 是 Manager 的 WS 命令处理入口（tailnet_file_done）。
func (m *Manager) handleTailnetFileDone(conn *agentConn, payload json.RawMessage) {
	var p TailnetFileDonePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		logger.L.Warnf("tailnet_file_done invalid payload agent=%d err=%v", conn.agentID, err)
		return
	}
	if strings.TrimSpace(p.ActionID) == "" {
		logger.L.Warnf("tailnet_file_done missing action_id agent=%d", conn.agentID)
		return
	}
	m.tailnetCoordinator.HandleFileDone(conn.agentID, p)
}

// TailnetTransferRequestPayload 是发起插件请求一次直传的入口命令 payload。
type TailnetTransferRequestPayload struct {
	PeerAgentID int64  `json:"peer_agent_id,string"`
	Direction   string `json:"direction"`
	LocalPath   string `json:"local_path"`
	RemotePath  string `json:"remote_path"`
	FileSize    int64  `json:"file_size,omitempty"`
}

// TailnetTransferResultPayload 是 Gateway 回发起插件的结果。
type TailnetTransferResultPayload struct {
	ActionID         string `json:"action_id,omitempty"`
	Mode             string `json:"mode"`
	Status           string `json:"status,omitempty"`
	BytesTransferred int64  `json:"bytes_transferred,omitempty"`
	ErrorMsg         string `json:"error_msg,omitempty"`
}

// handleTailnetTransferRequest 是入口命令 tailnet_transfer_request 的处理器。
// 解析角色、判定传输模式；可直传则在独立协程中编排并回发结果，否则即时告知模式。
func (m *Manager) handleTailnetTransferRequest(conn *agentConn, seq int64, payload json.RawMessage) {
	var p TailnetTransferRequestPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		conn.sendPayload("tailnet_transfer_result", seq, TailnetTransferResultPayload{
			Mode: string(TransferModeUnavailable), Status: "failed", ErrorMsg: "invalid payload",
		})
		return
	}
	dir := strings.TrimSpace(p.Direction)
	if dir != "download" && dir != "upload" {
		conn.sendPayload("tailnet_transfer_result", seq, TailnetTransferResultPayload{
			Mode: string(TransferModeUnavailable), Status: "failed", ErrorMsg: "invalid direction",
		})
		return
	}
	if p.PeerAgentID <= 0 || strings.TrimSpace(p.LocalPath) == "" || strings.TrimSpace(p.RemotePath) == "" {
		conn.sendPayload("tailnet_transfer_result", seq, TailnetTransferResultPayload{
			Mode: string(TransferModeUnavailable), Status: "failed", ErrorMsg: "missing fields",
		})
		return
	}

	// 权限校验：对端必须在线且与发起方属于同一 owner，禁止跨用户文件传输。
	// 按发起者 owner 精确取对端连接（agent 共享多连接物理隔离）；owner 缺失时回退主连接。
	peerConn := m.lookupConnForOwner(p.PeerAgentID, conn.ownerID)
	if peerConn == nil || peerConn.ownerID != conn.ownerID {
		conn.sendPayload("tailnet_transfer_result", seq, TailnetTransferResultPayload{
			Mode: string(TransferModeUnavailable), Status: "failed", ErrorMsg: "peer not accessible",
		})
		return
	}

	// 请求来自该 agent 连接本身，conn.ownerID 即发起者；按其精确选两端连接（agent 共享多连接物理隔离）。
	ownerID := conn.ownerID
	mode := m.tailnetCoordinator.ResolveTransferMode(conn.agentID, p.PeerAgentID, ownerID, p.FileSize)
	if mode != TransferModeTailnet {
		// 非直传：告知插件模式，由插件自行回退 base64 或报错
		conn.sendPayload("tailnet_transfer_result", seq, TailnetTransferResultPayload{Mode: string(mode)})
		return
	}

	initiator := conn.agentID
	// 编排阻塞等待 ready/done，放后台协程，避免阻塞 WS 读循环。
	// 走 Manager 的后台工作组：它会读写 DB，裸 goroutine 会活过关停。
	// 这条等待最长 5 分钟，超过 shutdownWait 时 Shutdown 会带告警放弃等待。
	m.goBackground(func() {
		actionID, err := m.tailnetCoordinator.StartTransfer(initiator, p.PeerAgentID, ownerID, dir, p.LocalPath, p.RemotePath)
		if err != nil {
			logger.L.Warnf("tailnet transfer start failed initiator=%d peer=%d err=%v", initiator, p.PeerAgentID, err)
			conn.sendPayload("tailnet_transfer_result", 0, TailnetTransferResultPayload{
				Mode: string(TransferModeTailnet), Status: "failed", ErrorMsg: err.Error(),
			})
			return
		}
		done, err := m.tailnetCoordinator.WaitDone(actionID, tailnetHTTPServerTimeout)
		if err != nil {
			conn.sendPayload("tailnet_transfer_result", 0, TailnetTransferResultPayload{
				ActionID: actionID, Mode: string(TransferModeTailnet), Status: "timeout", ErrorMsg: err.Error(),
			})
			return
		}
		conn.sendPayload("tailnet_transfer_result", 0, TailnetTransferResultPayload{
			ActionID:         actionID,
			Mode:             string(TransferModeTailnet),
			Status:           done.Status,
			BytesTransferred: done.BytesTransferred,
			ErrorMsg:         done.ErrorMsg,
		})
	})
}

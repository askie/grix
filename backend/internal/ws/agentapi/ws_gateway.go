package agentapi

import (
	"compress/flate"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/askie/grix/backend/internal/agentadapter"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/notification"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/clientip"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/tailnet"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/gorilla/websocket"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var codexRequiredLocalActions = []string{
	"get_context",
	"set_model",
	"set_mode",
}

func (m *Manager) ServeWS(w http.ResponseWriter, r *http.Request) {
	queryAgentID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("agent_id")), 10, 64)
	connClientIP := clientip.FromRequest(r)

	// 高频认证失败限流（升级前拦截）：老版本 connector 对已删除 agent 会无退避疯狂重连，
	// 处于封锁期的 (agent_id, ip) 直接拒绝升级握手，省掉 upgrade 与后续认证的全部开销。
	// agent_id 取自 URL query 自报值，仅用于限流分桶；不带 query 的连接由下方认证前兜底拦截。
	if agentAuthFailLimiter.Blocked(queryAgentID, connClientIP) {
		http.Error(w, "too many auth failures", http.StatusTooManyRequests)
		return
	}

	wsConn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.L.Warnf("agent api ws upgrade error: %v", err)
		return
	}
	defer wsConn.Close()

	// 若已协商 permessage-deflate，写侧压缩默认开启；这里把压缩级别降到最快档，
	// 用最小 CPU 换流量收益（未协商压缩时该调用无副作用）。
	_ = wsConn.SetCompressionLevel(flate.BestSpeed)

	// 纵深防御：限制单条消息的线上（压缩前）字节，避免开启解压后一个小压缩帧被放大成
	// 巨大内存分配。上限取业务单包上限 MaxPacketBytes——合法消息解压后本就不超过它，
	// 明文消息线上字节即消息大小，均不会误伤；同时这道限制在认证首包读之前就已生效。
	wsConn.SetReadLimit(protocol.MaxPacketBytes)

	// 登记到 Manager 的后台工作组：Shutdown 会关掉这条底层连接让读循环立刻返回，
	// 并等待本 ServeWS（及其派生协程）退出后才认为关停完成。
	if !m.trackServe(wsConn) {
		logger.L.Warnf("agent api ws rejected: manager is shutting down")
		return
	}
	defer m.untrackServe(wsConn)

	_ = wsConn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, firstPacketRaw, err := wsConn.ReadMessage()
	if err != nil {
		logger.L.Warnf("agent api auth first packet read failed: %v", err)
		return
	}

	var firstPacket protocol.Packet
	if err := json.Unmarshal(firstPacketRaw, &firstPacket); err != nil {
		sendPacketDirect(wsConn, protocol.Packet{
			Cmd: "auth_ack",
			Seq: 1,
			Payload: marshalPayload(AuthAckPayload{
				Code: 10003,
				Msg:  "invalid auth packet",
			}),
		})
		return
	}
	if firstPacket.Cmd != "auth" {
		sendPacketDirect(wsConn, protocol.Packet{
			Cmd: "auth_ack",
			Seq: firstPacket.Seq,
			Payload: marshalPayload(AuthAckPayload{
				Code: 10001,
				Msg:  "auth required as first packet",
			}),
		})
		return
	}

	var authPayload AuthPayload
	if err := json.Unmarshal(firstPacket.Payload, &authPayload); err != nil {
		sendPacketDirect(wsConn, protocol.Packet{
			Cmd: "auth_ack",
			Seq: firstPacket.Seq,
			Payload: marshalPayload(AuthAckPayload{
				Code: 10003,
				Msg:  "invalid auth payload",
			}),
		})
		return
	}
	if isHermesAuthPayload(authPayload) {
		if msg := validateHermesAuthPayload(authPayload); msg != "" {
			sendPacketDirect(wsConn, protocol.Packet{
				Cmd: "auth_ack",
				Seq: firstPacket.Seq,
				Payload: marshalPayload(AuthAckPayload{
					Code: 10003,
					Msg:  msg,
				}),
			})
			return
		}
	}
	clientType := model.NormalizeAgentClientType(authPayload.ClientType)
	if !model.IsValidAgentClientType(clientType) {
		sendPacketDirect(wsConn, protocol.Packet{
			Cmd: "auth_ack",
			Seq: firstPacket.Seq,
			Payload: marshalPayload(AuthAckPayload{
				Code: 10003,
				Msg:  "invalid client_type",
			}),
		})
		return
	}
	agentID := authPayload.AgentID
	if agentID == 0 {
		agentID = queryAgentID
	}
	// 认证前兜底限流：agent_id 只写在 auth payload、不带 URL query 的连接走这里拦截。
	// 处于封锁期直接断开，不再走 DB 认证流程。
	if agentID != queryAgentID && agentAuthFailLimiter.Blocked(agentID, connClientIP) {
		return
	}
	// 阶段0 安全：解析真实客户端 IP（经 LB 转发头），命中封禁名单直接拒绝握手。
	// 注意：此时 agentID 取自客户端自报的 auth payload，尚未经 api_key 验证；
	// 谎报 agentID 可规避"针对某 agent"的封禁（谎报后 auth 必然失败），
	// 因此单 agent 维度封禁不是硬保证，需要硬封禁时应下发 agent_id=0 的全局规则。
	if checkAgentIPBanned(agentID, connClientIP) {
		sendPacketDirect(wsConn, protocol.Packet{
			Cmd: "auth_ack",
			Seq: firstPacket.Seq,
			Payload: marshalPayload(AuthAckPayload{
				Code: 10001,
				Msg:  "forbidden",
			}),
		})
		return
	}
	ownerID, isPrimary, authErr := m.authenticateAgent(agentID, authPayload.APIKey, authPayload.SharedOwnerID)
	if authErr != nil {
		// 服务端内部错误不计入限流，避免 DB 抖动期把正常重连的 agent 封锁 10 分钟。
		if !errors.Is(authErr, errAuthInternal) {
			agentAuthFailLimiter.RecordFailure(agentID, connClientIP)
		}
		// 10008 是与 connector 约定的协议契约：agent 已删除（或不存在），客户端应永久
		// 停止重连并自清理；其余认证失败（密钥错误、禁用等可恢复状态）维持 10001。
		ackCode, ackMsg := 10001, "auth failed"
		if errors.Is(authErr, errAgentDeleted) {
			ackCode, ackMsg = 10008, "agent deleted"
		}
		sendPacketDirect(wsConn, protocol.Packet{
			Cmd: "auth_ack",
			Seq: firstPacket.Seq,
			Payload: marshalPayload(AuthAckPayload{
				Code: ackCode,
				Msg:  ackMsg,
			}),
		})
		return
	}
	connectionEpoch, epochErr := m.reserveConnectionEpoch(r.Context(), ownerID, agentID)
	if epochErr != nil {
		// Production configures a Redis-backed allocator. Never attach the
		// connection or publish online with a local/zero fallback when Redis
		// cannot establish cross-node ordering.
		logger.L.Errorf(
			"agent api connection epoch allocation failed: agent_id=%d owner_id=%d err=%v",
			agentID,
			ownerID,
			epochErr,
		)
		sendPacketDirect(wsConn, protocol.Packet{
			Cmd: "auth_ack",
			Seq: firstPacket.Seq,
			Payload: marshalPayload(AuthAckPayload{
				Code: 10001,
				Msg:  "auth temporarily unavailable",
			}),
		})
		return
	}
	resolvedClientType := clientType
	if resolvedClientType == "" {
		resolvedClientType = model.NormalizeAgentClientType(authPayload.HostType)
	}
	if err := m.persistAgentClientType(agentID, resolvedClientType); err != nil {
		logger.L.Errorf("agent api client_type persist failed: agent_id=%d client_type=%s err=%v", agentID, resolvedClientType, err)
		sendPacketDirect(wsConn, protocol.Packet{
			Cmd: "auth_ack",
			Seq: firstPacket.Seq,
			Payload: marshalPayload(AuthAckPayload{
				Code: 10001,
				Msg:  "auth failed",
			}),
		})
		return
	}
	if err := m.persistAgentHostMeta(agentID, authPayload.HostMeta); err != nil {
		logger.L.Errorf("agent api host_meta persist failed: agent_id=%d err=%v", agentID, err)
	}
	logger.L.Infof("Agent API plugin connected successfully: agent_id=%d client_type=%s", agentID, clientType)

	// Adapter selection
	var adapterResult *agentadapter.SelectResult
	var adapterIDStr string
	normalizedCapabilities := normalizeDeclaredNames(authPayload.Capabilities)
	normalizedLocalActions := normalizeDeclaredNames(authPayload.LocalActions)
	if clientType == model.AgentClientTypeCodex {
		if missing := missingDeclaredNames(normalizedLocalActions, codexRequiredLocalActions); len(missing) > 0 {
			logger.L.Warnf(
				"codex auth missing required local_actions: agent_id=%d host_version=%s missing=%v local_actions=%v",
				agentID,
				strings.TrimSpace(authPayload.HostVersion),
				missing,
				normalizedLocalActions,
			)
		}
	}
	if m.adapterRegistry != nil {
		meta := agentadapter.AgentClientMeta{
			AgentID:         agentID,
			Client:          authPayload.Client,
			ClientType:      clientType,
			ClientVersion:   authPayload.ClientVersion,
			HostType:        authPayload.HostType,
			HostVersion:     authPayload.HostVersion,
			ProtocolVersion: authPayload.ProtocolVersion,
			ContractVersion: authPayload.ContractVersion,
			Capabilities:    normalizedCapabilities,
			AdapterHint:     authPayload.AdapterHint,
		}
		adapterResult = agentadapter.SelectByMeta(m.adapterRegistry, meta)
		if adapterResult != nil {
			adapterIDStr = adapterResult.AdapterID
		}
	}

	var agentProfile model.Agent
	if err := store.DB.Select("agent_name", "introduction", "system_prompt").First(&agentProfile, agentID).Error; err != nil {
		logger.L.Errorf("agent api auth profile load failed: agent_id=%d err=%v", agentID, err)
		sendPacketDirect(wsConn, protocol.Packet{
			Cmd: "auth_ack",
			Seq: firstPacket.Seq,
			Payload: marshalPayload(AuthAckPayload{
				Code: 10001,
				Msg:  "auth temporarily unavailable",
			}),
		})
		return
	}

	conn := &agentConn{
		ws:              wsConn,
		agentID:         agentID,
		ownerID:         ownerID,
		isPrimary:       isPrimary,
		clientID:        strings.TrimSpace(authPayload.Client),
		clientType:      clientType,
		adapter:         adapterResultAdapter(adapterResult),
		adapterID:       adapterIDStr,
		hostVersion:     authPayload.HostVersion,
		capabilities:    normalizedCapabilities,
		localActions:    normalizedLocalActions,
		skills:          parseAuthSkills(authPayload.Skills),
		librarySkills:   parseLibrarySkills(authPayload.LibrarySkills),
		tailnetIP:       resolveTailnetIP(authPayload),
		clientIP:        connClientIP,
		connectedAt:     time.Now(),
		connectionEpoch: connectionEpoch,
		send:            make(chan []byte, 256),
		done:            make(chan struct{}),
	}
	// 把 agentConn 关联到后台工作组：关停时才能对它走完整的 close()
	// （置终止标志 + 关 done），生产者据此立刻知道连接已死。
	m.bindServeConn(wsConn, conn)

	// 记录连接来源（IP/归属地/异地标记/白名单观测）；失败不影响连接。
	recordAgentConnection(m, conn)
	authAck := AuthAckPayload{
		Code:         0,
		Msg:          "ok",
		AgentID:      conn.agentID,
		OwnerID:      conn.ownerID,
		Protocol:     protocol.AgentAPIProtocolVersion,
		HeartbeatSec: int(m.heartbeat.Seconds()),
		AgentName:    agentProfile.AgentName,
		Introduction: agentProfile.Introduction,
		SystemPrompt: agentProfile.SystemPrompt,
	}
	if adapterResult != nil && authPayload.ContractVersion > 0 {
		ext := agentadapter.FormatAuthAckExt(adapterResult, agentadapter.AgentClientMeta{
			ContractVersion: authPayload.ContractVersion,
			Capabilities:    normalizedCapabilities,
		})
		authAck.ContractVersion = ext["contract_version"].(int)
		authAck.AdapterID = ext["adapter_id"].(string)
		authAck.SupportedCapabilities = ext["supported_capabilities"].([]string)
		authAck.DegradedCapabilities = ext["degraded_capabilities"].([]string)
		authAck.UnsupportedCapabilities = ext["unsupported_capabilities"].([]string)
	}
	if hasDeclaredName(normalizedCapabilities, terminalCommitCapability) &&
		!hasDeclaredName(authAck.SupportedCapabilities, terminalCommitCapability) {
		authAck.SupportedCapabilities = append(
			authAck.SupportedCapabilities,
			terminalCommitCapability,
		)
	}

	// 先把连接同步登记进 manager,让 IsAgentChannelAvailable 立刻可见;
	// 必须放在 auth_ack 之前,否则客户端拿到 ack 就发 delegate_start,server 端还没看到
	// 这条连接,会被判 agent_api_channel_unavailable(CI 2 核机调度慢时偶发)。
	if !m.attachConn(conn) {
		logger.L.Warnf(
			"agent api auth rejected by connection authority: agent_id=%d owner_id=%d epoch=%d",
			conn.agentID,
			conn.ownerID,
			conn.connectionEpoch,
		)
		sendPacketDirect(wsConn, protocol.Packet{
			Cmd: "auth_ack",
			Seq: firstPacket.Seq,
			Payload: marshalPayload(AuthAckPayload{
				Code: 10001,
				Msg:  "connection superseded",
			}),
		})
		finalizeAgentConnection(conn, "connection_superseded")
		conn.close()
		return
	}
	defer m.unregister(conn)
	if !m.refreshAgentLease(conn) {
		// A higher epoch won on another node after attach but before auth_ack.
		// refreshAgentLease closes this stale websocket; never acknowledge it as
		// authenticated.
		return
	}

	// auth_ack 走 sendPacketDirect 直接写 ws,绕开 send chan / writePump,
	// 保证它是客户端收到的第一条消息;此时还没启动 writePump,不会与之并发写 ws。
	sendPacketDirect(wsConn, protocol.Packet{
		Cmd:     "auth_ack",
		Seq:     firstPacket.Seq,
		Payload: marshalPayload(authAck),
	})

	// 本连接派生的协程都挂到 conn.wg 上；ServeWS 返回前先断连接再等它们退出，
	// 这样「重放积压 / 处理 agent_invoke」不会跑到连接注销之后，更不会活过进程关停。
	conn.spawn(func() { conn.writePump(m.heartbeat) })

	// 积压重放放到 writePump 启动之后异步执行:三段 drain 的 batch 总和(384) 可能超过
	// send chan 容量(256),writePump 必须先开始消费才能避免阻塞 replay goroutine。
	conn.spawn(func() { m.replayPending(conn) })

	defer func() {
		conn.close()
		conn.wg.Wait()
	}()

	_ = wsConn.SetReadDeadline(time.Now().Add(m.heartbeat * 2))
	wsConn.SetPongHandler(func(_ string) error {
		m.refreshAgentLease(conn)
		return wsConn.SetReadDeadline(time.Now().Add(m.heartbeat * 2))
	})

	for {
		_, raw, err := wsConn.ReadMessage()
		if err != nil {
			return
		}

		// Phase 1.2: 单条 packet 大小硬上限。超过即关闭连接,不再尝试解析。
		if len(raw) > protocol.MaxPacketBytes {
			logger.L.Warnf(
				"agentapi packet too large agent=%d owner=%d bytes=%d limit=%d",
				conn.agentID,
				conn.ownerID,
				len(raw),
				protocol.MaxPacketBytes,
			)
			conn.sendPayload("error", 0, SendNackPayload{
				Code: protocol.CodePayloadTooLarge,
				Msg:  "packet too large",
			})
			conn.close()
			return
		}

		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			conn.recordViolation()
			conn.sendPayload("error", 0, SendNackPayload{
				Code: protocol.CodeInvalidPayload,
				Msg:  "invalid packet",
			})
			continue
		}
		// The socket may still be readable after a higher epoch connected on
		// another node. Fence every parsed inbound packet before activity
		// tracking or any command handler can mutate messages, streams, tools,
		// terminal state, or leases on behalf of that stale connection.
		if !m.ensureAgentConnectionAuthoritative(conn) {
			return
		}
		if pkt.Cmd != protocol.CmdPing {
			logger.L.Infof("Agent API plugin received reply cmd=%s agent_id=%d seq=%d bytes=%d", pkt.Cmd, conn.agentID, pkt.Seq, len(pkt.Payload))
		}
		if pkt.Cmd == protocol.CmdEventStopAck || pkt.Cmd == protocol.CmdEventStopResult {
			logger.L.Infof(
				"agent_output_stop raw inbound agent=%d cmd=%s seq=%d bytes=%d",
				conn.agentID,
				pkt.Cmd,
				pkt.Seq,
				len(raw),
			)
		}
		if isHermesConn(conn) && !isHermesAllowedClientCommand(pkt.Cmd) {
			conn.recordViolation()
			conn.sendPayload("error", pkt.Seq, SendNackPayload{
				Code: protocol.CodeUnsupportedCmd,
				Msg:  "unsupported cmd for hermes",
			})
			continue
		}

		// Update only the event explicitly referenced by this packet. Generic,
		// Codex and Pi outputs all use this path; unrelated ping/tool traffic
		// must not distort another event's state.
		m.observeInboundPacketActivity(conn, &pkt)

		switch pkt.Cmd {
		case "ping":
			conn.sendPayload("pong", pkt.Seq, map[string]int64{"ts": time.Now().UnixMilli()})
		case "pong":
			m.refreshAgentLease(conn)
		case protocol.CmdEventAck:
			m.handleEventAck(conn, &pkt)
		case protocol.CmdEventResult:
			m.handleEventResult(conn, &pkt)
		case protocol.CmdEventStopAck:
			m.handleEventStopAck(conn, &pkt)
		case protocol.CmdEventStopResult:
			m.handleEventStopResult(conn, &pkt)
		case protocol.CmdEventState, protocol.CmdEventCancelResult, protocol.CmdQueueClearResult, protocol.CmdQueueReorderResult, protocol.CmdEventHoldResult, protocol.CmdQueueEditResult, protocol.CmdQueueSnapshot:
			// Forward event lifecycle packets to APP-side WS clients of the owner.
			// Keep gateway forward-compatible for Hermes while preserving lease updates.
			m.refreshAgentLease(conn)
			// 队列快照是队列权威数据：转发前先落只读镜像，供停止目标/工具栏解析。
			if pkt.Cmd == protocol.CmdQueueSnapshot && conn.ownerID > 0 {
				if sessionID, empty := storeQueueSnapshotMirror(context.Background(), conn.ownerID, conn.agentID, pkt.Payload); empty {
					m.clearComposingForEmptyQueueSnapshot(conn, sessionID)
				}
			}
			m.mu.RLock()
			lifecycleFn := m.eventLifecycleFn
			m.mu.RUnlock()
			if lifecycleFn != nil && conn.ownerID > 0 {
				lifecycleFn(conn.ownerID, pkt.Cmd, pkt.Payload)
			}
		case protocol.CmdSessionActivitySet:
			m.handleSessionActivitySet(conn, &pkt)
		case "send_msg":
			m.handleSendMsg(conn, &pkt)
		case "client_stream_chunk":
			m.handleClientStreamChunk(conn, &pkt)
		case "delete_msg":
			m.handleDeleteMsg(conn, &pkt)
		case "edit_msg":
			m.handleEditMsg(conn, &pkt)
		case protocol.CmdUpdateBindingCard:
			m.handleUpdateBindingCard(conn, &pkt)
		case "react_msg":
			m.handleReactMsg(conn, &pkt)
		case "media_upload_init":
			m.handleMediaUploadInit(conn, &pkt)
		case cmdSessionRouteBind:
			m.handleSessionRouteBind(conn, &pkt)
		case cmdSessionRouteResolve:
			m.handleSessionRouteResolve(conn, &pkt)
		case protocol.CmdAgentInvoke:
			// 异步处理：dispatch_agent 等动作内部会同步等待目标 agent 连接回包
			// （如 session_bind 的 local_action_result）。目标是本连接自己时
			// （agent 自派自），同步处理会阻塞本读循环导致回包永远读不到而死锁，
			// 且任何慢动作都会卡住整条连接的入站处理。
			//
			// 挂在 Manager 而不是本连接上：invoke 内部可能等到几十秒，
			// 若让 ServeWS 等它，断线后的注销（路由/在线状态清理）就会被拖住。
			// 注销要即时，进程关停时由 Manager.Shutdown 兜底等它跑完。
			invokePkt := pkt
			m.goBackground(func() { m.handleAgentInvoke(conn, &invokePkt) })
		case protocol.CmdMcpFrame:
			m.handleMcpFrame(conn, &pkt)
		case protocol.CmdLocalActionResult:
			m.handleLocalActionResult(conn, &pkt)
		case protocol.CmdAuditState:
			m.handleAuditState(conn, &pkt)
		case protocol.CmdCodexEvent:
			m.handleCodexEvent(conn, &pkt)
		case protocol.CmdPiEvent:
			m.handlePiEvent(conn, &pkt)
		case protocol.CmdAgentSkillsUpdate:
			m.handleSkillsUpdate(conn, &pkt)
		case protocol.CmdRelayCredentialRequest:
			m.handleRelayCredentialRequest(conn, &pkt)
		case protocol.CmdRelayStateSyncRequest:
			m.handleRelayStateSyncRequest(conn, &pkt)
		case protocol.CmdRelayStateReport:
			m.handleRelayStateReport(conn, &pkt)
		case "tailnet_file_ready":
			m.handleTailnetFileReady(conn, pkt.Payload)
		case "tailnet_file_done":
			m.handleTailnetFileDone(conn, pkt.Payload)
		case "tailnet_transfer_request":
			m.handleTailnetTransferRequest(conn, pkt.Seq, pkt.Payload)
		case "error":
			m.refreshAgentLease(conn)
		default:
			conn.recordViolation()
			conn.sendPayload("error", pkt.Seq, SendNackPayload{
				Code: protocol.CodeUnsupportedCmd,
				Msg:  "unknown cmd",
			})
		}
	}
}

func (m *Manager) clearComposingForEmptyQueueSnapshot(conn *agentConn, sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if m == nil || conn == nil || conn.ownerID <= 0 || conn.agentID <= 0 || sessionID == "" {
		return
	}
	m.mu.RLock()
	activityFn := m.activityFn
	m.mu.RUnlock()
	if activityFn == nil {
		return
	}
	if err := activityFn(context.Background(), conn.agentID, conn.ownerID, protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    false,
	}); err != nil {
		logger.L.Warnf(
			"clear composing after empty queue_snapshot failed owner=%d session=%s agent=%d err=%v",
			conn.ownerID,
			sessionID,
			conn.agentID,
			err,
		)
	}
}

// errAgentDeleted 表示 agent 已删除或不存在，是不可恢复的终态。
// ServeWS 据此返回 auth_ack code=10008（协议契约），让 connector 永久停止重连并自清理。
var errAgentDeleted = errors.New("agent deleted")

// errAuthInternal 表示认证过程中的服务端内部错误（DB 抖动、连接池耗尽等）。
// 不计入认证失败限流：故障属于服务端自身，不该被放大成对客户端的 10 分钟封锁。
var errAuthInternal = errors.New("auth internal error")

// authenticateAgent 校验 agent 连接身份，返回该连接的有效 ownerID 与是否为主连接。
// 所有连接（含共享）都用主人 api_key（connector 持有）。带 sharedOwnerID（且非主人本人）时，
// 再校验该被共享者在 agent_shares 中有有效授权，连接身份认定为被共享者。
func (m *Manager) authenticateAgent(agentID int64, apiKey string, sharedOwnerID int64) (ownerID int64, isPrimary bool, err error) {
	if agentID <= 0 || strings.TrimSpace(apiKey) == "" {
		return 0, false, errors.New("invalid credentials")
	}

	var agent model.Agent
	if dbErr := store.DB.Select("id,owner_id,provider_type,status,api_key_hash").
		First(&agent, agentID).Error; dbErr != nil {
		if errors.Is(dbErr, gorm.ErrRecordNotFound) {
			return 0, false, errAgentDeleted
		}
		return 0, false, fmt.Errorf("%w: %v", errAuthInternal, dbErr)
	}
	// 已删除是终态 → errAgentDeleted(10008)；禁用(status=2)属可恢复状态，走通用失败(10001)。
	if agent.Status == model.AgentStatusDeleted {
		return 0, false, errAgentDeleted
	}
	if agent.Status != model.AgentStatusActive || agent.ProviderType != model.AgentProviderAPI {
		return 0, false, errors.New("agent unavailable")
	}
	if strings.TrimSpace(agent.APIKeyHash) == "" {
		return 0, false, errors.New("api key not configured")
	}
	incoming := pkgagentapi.HashAPIKey(apiKey)
	if subtle.ConstantTimeCompare([]byte(incoming), []byte(agent.APIKeyHash)) != 1 {
		return 0, false, errors.New("api key mismatch")
	}

	// 共享连接：身份认定为被共享者，需该被共享者有有效共享授权。
	if sharedOwnerID > 0 && sharedOwnerID != agent.OwnerID {
		ok, shareErr := agentShareActive(agentID, sharedOwnerID)
		if shareErr != nil {
			return 0, false, fmt.Errorf("%w: %v", errAuthInternal, shareErr)
		}
		if !ok {
			return 0, false, errors.New("share not authorized")
		}
		return sharedOwnerID, false, nil
	}

	return agent.OwnerID, true, nil
}

// AgentShareActive 是 agentShareActive 的导出包装，供 handler 层（访问门禁的被共享者豁免）复用。
func AgentShareActive(agentID, sharedTo int64) (bool, error) {
	return agentShareActive(agentID, sharedTo)
}

// agentShareActive 判断 (agentID, sharedTo) 是否存在有效（未撤销、未过期）共享，
// 且 sharedTo 是活跃账户。被共享者封号/注销时握手即被拒，无需 agent 主人手动撤销。
func agentShareActive(agentID, sharedTo int64) (bool, error) {
	var share model.AgentShare
	err := store.DB.Select("id", "expires_at").Where(
		"agent_id = ? AND shared_to = ? AND status = ?",
		agentID, sharedTo, model.AgentShareStatusActive,
	).First(&share).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if share.ExpiresAt != nil && share.ExpiresAt.Before(time.Now()) {
		return false, nil
	}
	var u model.User
	if err := store.DB.Select("id", "status").Where("id = ?", sharedTo).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return u.Status == model.UserStatusActive, nil
}

func (m *Manager) persistAgentClientType(agentID int64, clientType string) error {
	normalized := model.NormalizeAgentClientType(clientType)
	if normalized == "" {
		return nil
	}
	return store.DB.Model(&model.Agent{}).
		Where("id = ?", agentID).
		Update("agent_client_type", normalized).Error
}

func (m *Manager) persistAgentHostMeta(agentID int64, rawHostMeta json.RawMessage) error {
	if len(rawHostMeta) == 0 {
		return nil
	}
	var agent model.Agent
	if err := store.DB.Select("id", "config").First(&agent, agentID).Error; err != nil {
		return err
	}
	var config map[string]any
	if err := json.Unmarshal(agent.Config, &config); err != nil {
		config = make(map[string]any)
	}
	var hostMeta any
	if err := json.Unmarshal(rawHostMeta, &hostMeta); err != nil {
		return err
	}
	config["host_meta"] = hostMeta
	merged, _ := json.Marshal(config)
	return store.DB.Model(&model.Agent{}).
		Where("id = ?", agentID).
		Update("config", datatypes.JSON(merged)).Error
}

func (m *Manager) register(conn *agentConn) {
	if conn == nil || conn.agentID <= 0 {
		return
	}
	if !m.attachConn(conn) {
		conn.close()
		return
	}
	m.replayPending(conn)
}

// attachConn 把连接同步登记到 manager 的内存表与 Redis 路由，让 IsAgentChannelAvailable
// 立刻可见；不做任何「重放积压」之类可能阻塞 send chan 的工作。
// 必须在 auth_ack 投递之前调用，否则客户端收到 auth_ack 就发 delegate_start，
// 此时 manager 还没看到这条连接，会被判 agent_api_channel_unavailable。
func (m *Manager) attachConn(conn *agentConn) bool {
	if conn == nil || conn.agentID <= 0 {
		return false
	}
	m.attachMu.Lock()
	defer m.attachMu.Unlock()

	leaseTTL := m.heartbeat*2 + 5*time.Second
	claimed, err := m.claimAgentConnectionAuthority(conn, leaseTTL)
	if err != nil {
		logger.L.Warnf(
			"claim agent connection authority failed agent=%d owner=%d node=%s epoch=%d err=%v",
			conn.agentID,
			conn.ownerID,
			m.getNodeID(),
			conn.connectionEpoch,
			err,
		)
		return false
	}
	if !claimed {
		return false
	}

	m.mu.Lock()
	owners := m.conns[conn.agentID]
	if owners == nil {
		owners = make(map[int64]*agentConn)
		m.conns[conn.agentID] = owners
	}
	old := owners[conn.ownerID]
	if old != nil && old != conn {
		// Reservation and authentication are intentionally separate. A lower
		// epoch request can therefore finish later; never let it replace or
		// close the already-attached successor on this node.
		if old.connectionEpoch > 0 &&
			(conn.connectionEpoch <= 0 || old.connectionEpoch >= conn.connectionEpoch) {
			m.mu.Unlock()
			return false
		}
	}
	owners[conn.ownerID] = conn
	m.mu.Unlock()

	if old != nil {
		// Wait for an in-flight lease refresh from the replaced connection.
		// The map already points at conn, so any refresh starting after this
		// point will fail its active-connection check.
		old.stateMu.Lock()
		// 仅同一身份(同 ownerID)重连才踢旧连接；不同被共享者/主人的连接并存，互不影响。
		m.preservePendingForAgentDisconnect(old.agentID)
		sendShareRevokedNotice(old, "replaced_by_new_connection")
		finalizeAgentConnection(old, "replaced_by_new_connection")
		old.close()
		old.stateMu.Unlock()
	}

	// Agent 上线通知：只对 agent 主人本人的主连接触发（被共享者连接不算 agent 上线）。
	// 是否为真正的离线→上线转换由推送服务据 presence 状态判定，此处即时投递信号；
	// 抖动重连不会重复推（状态未曾翻成离线）。
	if conn.isPrimary {
		notification.SignalAgentOnline(conn.agentID, conn.ownerID)
	}
	return true
}

// replayPending 把队列里的积压事件喂给新连接。drain 总量(128+128+128)
// 可能超过 send chan 容量(256)，所以必须在 writePump 启动后才能调用，否则可能阻塞调用方。
func (m *Manager) replayPending(conn *agentConn) {
	if conn == nil || conn.agentID <= 0 {
		return
	}
	// 离线补发：每条连接只补发「自己这个 owner」的积压（队列按 (agentID, ownerID) 隔离），
	// 主人与被共享者各取各的，互不串。
	m.drainQueuedAgentEvents(conn, queuedAgentEventDrainBatch)
	m.drainDurablePendingDelegateAcks(conn, durablePendingDelegateDrainBatch)
	m.drainQueuedDelegateEvents(conn, queuedDelegateEventDrainBatch)

	// 主连接建立时，若该 agent 有被共享者则下发列表，connector 据此为各被共享者建独立连接。
	if conn.isPrimary {
		m.sendShareSet(conn, conn.agentID, false)
	}
}

func (m *Manager) unregister(conn *agentConn) {
	if conn == nil || conn.agentID <= 0 {
		return
	}
	conn.stateMu.Lock()
	// 断开回填连接日志 + 清理 Redis 在线信息（幂等，kick/顶号等已带原因先执行时此处为空操作）。
	finalizeAgentConnection(conn, disconnectReasonClosed)
	active := false
	agentGone := false
	var disconnectFn StreamDisconnectHandler
	m.mu.Lock()
	owners := m.conns[conn.agentID]
	if owners != nil {
		if current := owners[conn.ownerID]; current == conn {
			delete(owners, conn.ownerID)
			active = true
		}
		if len(owners) == 0 {
			delete(m.conns, conn.agentID)
			agentGone = true
		}
	}
	disconnectFn = m.disconnectFn
	m.mu.Unlock()
	if !active {
		conn.stateMu.Unlock()
		conn.close()
		return
	}
	// Cleanup is generation-owned just like refresh. If a newer websocket on
	// this or another node already claimed authority, this unregister belongs
	// to a stale TCP connection and must not emit offline, clear capabilities /
	// runtime data, abort successor streams, or delete successor MCP metadata.
	if !m.releaseAgentConnectionAuthority(conn) {
		conn.stateMu.Unlock()
		conn.close()
		return
	}
	// Agent 离线通知：主人本人的主连接断开时，安排一次延迟离线确认；推送服务在
	// 抖动阈值后复查仍不在线才推。被顶号替换的旧连接 active=false，已在上面提前返回，
	// 不会误排离线。
	if conn.isPrimary {
		notification.SignalAgentOffline(conn.agentID, conn.ownerID)
	}
	// State is owner-scoped: every active (agent, owner) connection must publish
	// its own offline transition even while another owner remains connected.
	// connection_epoch lets the persistence/fanout layer reject a late offline
	// emitted by an older connection on another node.
	m.emitAgentStateForConn(conn, protocol.AgentStateOffline, false, 0)
	// The connection has already been removed from m.conns. A refresh that was
	// waiting on stateMu will now fail isCurrentAgentConn and cannot resurrect
	// this generation as online.
	conn.stateMu.Unlock()
	// releaseAgentConnectionAuthority already atomically removed this exact
	// generation's route/capability/runtime/conninfo leases. MCP sessions are
	// owner-scoped; removing all agent sessions here would let a node whose last
	// local connection closed erase another owner's sessions on another node.
	m.CleanupMcpSessionsForAgentOwner(conn.agentID, conn.ownerID)
	if agentGone {
		m.preservePendingForAgentDisconnect(conn.agentID)
		// 中转开关 actual 生命周期（设计 §2.4）：该 agent 最后一条权威连接断开时
		// applied 置 false（实际态随离线不可知，保守标未生效）。本机连接表已空后
		// 再查跨节点 Redis 路由表，仍有其他权威连接在线则不动——实际态由在线连接
		// 的下一次 sync/report 负责。feature flag 关闭时 service 层停用。
		// 已知竞态（评审确认可接受）：断连前发出的 relay_state_report 若延迟到达，
		// 仍可能因 revision >= desired 被写回 applied=true；该窗口极短，以重连后
		// 的下一次 sync 对齐为准。
		if !agentHasAnyOnlineRoute(conn.agentID) {
			service.GatewayRelayStateDisconnected(conn.agentID)
		}
	}
	// 按 owner 维度通知上层该连接的流断开，始终执行。
	if disconnectFn != nil {
		disconnectFn(context.Background(), conn.agentID, conn.ownerID)
	}
	conn.close()
}

// KickAgent forcefully closes ALL of an agent's active WS connections (主连接 + 所有共享连接)。
// agent 共享场景下,被共享者 B/C/... 的连接是独立连接,必须一并踢掉,否则:
//   - 站长删 agent / 重置 api key 时,被共享者的连接仍存活,可继续读主人本机数据
//   - 强制下线场景下出现"主连接走了,共享连接还活着"的不一致
func (m *Manager) KickAgent(agentID int64, reason string) {
	if agentID <= 0 {
		return
	}
	m.mu.RLock()
	owners := m.conns[agentID]
	conns := make([]*agentConn, 0, len(owners))
	for _, c := range owners {
		if c != nil {
			conns = append(conns, c)
		}
	}
	m.mu.RUnlock()
	for _, c := range conns {
		sendShareRevokedNotice(c, reason)
		finalizeAgentConnection(c, reason)
		m.unregister(c)
	}
}

// KickAgentOwner 只断开某个 agent 的指定 owner 连接（主人或某个被共享者），
// 其余连接不受影响。用于管控 API 的"按连接踢线"。
func (m *Manager) KickAgentOwner(agentID, ownerID int64, reason string) {
	if agentID <= 0 || ownerID <= 0 {
		return
	}
	m.mu.RLock()
	var target *agentConn
	if owners := m.conns[agentID]; owners != nil {
		target = owners[ownerID]
	}
	m.mu.RUnlock()
	if target == nil {
		return
	}
	sendShareRevokedNotice(target, reason)
	finalizeAgentConnection(target, reason)
	m.unregister(target)
}

func (m *Manager) emitDeliveryStatus(payload protocol.AgentDeliveryStatusPayload) {
	if strings.TrimSpace(payload.EventID) != "" {
		if payload.DispatchGeneration <= 0 {
			m.acksMu.Lock()
			if entry := m.pending[payload.EventID]; entry != nil {
				payload.DispatchGeneration = entry.dispatchGeneration
			}
			m.acksMu.Unlock()
		}
		if payload.DispatchGeneration <= 0 {
			if ledger, err := store.LoadAgentEventTerminalLedger(payload.EventID); err == nil && ledger != nil {
				payload.DispatchGeneration = ledger.DispatchGeneration
			}
		}
		payload.Revision = nextAgentStatusRevision("delivery", payload.EventID)
	}
	m.mu.RLock()
	fn := m.statusFn
	m.mu.RUnlock()
	if fn != nil {
		fn(payload)
	}
}

func (m *Manager) emitAgentState(ownerID int64, payload protocol.AgentStateSyncPayload) {
	if ownerID <= 0 || payload.AgentID <= 0 {
		return
	}
	m.mu.RLock()
	fn := m.stateFn
	m.mu.RUnlock()
	if fn != nil {
		fn(ownerID, payload)
	}
}

func (m *Manager) reserveConnectionEpoch(ctx context.Context, ownerID, agentID int64) (int64, error) {
	m.mu.RLock()
	allocator := m.connectionEpochFn
	m.mu.RUnlock()
	if allocator == nil {
		// Standalone Manager users that do not install a state handler retain
		// legacy epoch-zero behavior. Server.Start always installs the
		// Redis-backed allocator before accepting connections.
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	epoch, err := allocator(ctx, ownerID, agentID)
	if err != nil {
		return 0, err
	}
	if epoch <= 0 {
		return 0, fmt.Errorf("connection epoch allocator returned invalid generation %d", epoch)
	}
	return epoch, nil
}

func (m *Manager) refreshAgentLease(conn *agentConn) bool {
	if conn == nil || conn.agentID <= 0 || conn.ownerID <= 0 {
		return false
	}
	conn.stateMu.Lock()
	defer conn.stateMu.Unlock()
	if !m.isCurrentAgentConn(conn) {
		return false
	}
	leaseTTL := m.heartbeat*2 + 5*time.Second
	leaseUntil := time.Now().Add(leaseTTL).UnixMilli()
	if conn.connectionEpoch > 0 {
		refreshed, err := m.refreshAgentConnectionAuthority(conn, leaseTTL)
		if err != nil || !refreshed {
			logger.L.Warnf(
				"agent lease authority rejected agent=%d owner=%d node=%s epoch=%d refreshed=%t err=%v",
				conn.agentID,
				conn.ownerID,
				m.getNodeID(),
				conn.connectionEpoch,
				refreshed,
				err,
			)
			conn.close()
			return false
		}
	} else {
		m.refreshAgentRoute(conn, leaseTTL)
	}
	m.refreshAgentCapabilities(conn, leaseTTL)
	m.refreshAgentRuntimeProfile(conn, leaseTTL)
	m.refreshConnInfo(conn, leaseTTL)
	if !m.ensureAgentConnectionAuthoritative(conn) {
		return false
	}
	m.emitAgentStateForConn(conn, protocol.AgentStateOnline, true, leaseUntil)
	m.drainQueuedAgentEvents(conn, queuedAgentEventDrainBatch)
	m.drainDurablePendingDelegateAcks(conn, durablePendingDelegateDrainBatch)
	m.drainQueuedDelegateEvents(conn, queuedDelegateEventDrainBatch)
	return true
}

type agentStateExtra struct {
	Source          string `json:"source,omitempty"`
	Connected       bool   `json:"connected"`
	LeaseUntil      int64  `json:"lease_until,omitempty"`
	ConnectionEpoch int64  `json:"connection_epoch,omitempty"`
}

func buildAgentStatePayload(
	agentID int64,
	state string,
	connected bool,
	leaseUntil int64,
	connectionEpoch int64,
) protocol.AgentStateSyncPayload {
	if state != protocol.AgentStateOnline {
		state = protocol.AgentStateOffline
		connected = false
		leaseUntil = 0
	}
	if connectionEpoch < 0 {
		connectionEpoch = 0
	}
	return protocol.AgentStateSyncPayload{
		AgentID: agentID,
		State:   state,
		Extra: marshalPayload(agentStateExtra{
			Source:          protocol.SessionActivitySourceAgentAPI,
			Connected:       connected,
			LeaseUntil:      leaseUntil,
			ConnectionEpoch: connectionEpoch,
		}),
	}
}

// emitAgentStateForConn builds a domain status event, routes it through the
// connection's adapter (if available), and then broadcasts to the app layer.
func (m *Manager) emitAgentStateForConn(conn *agentConn, state string, connected bool, leaseUntil int64) {
	if conn == nil || conn.ownerID <= 0 || conn.agentID <= 0 {
		return
	}
	payload := buildAgentStatePayload(
		conn.agentID,
		state,
		connected,
		leaseUntil,
		conn.connectionEpoch,
	)
	m.emitAgentState(conn.ownerID, payload)
}

func (m *Manager) isCurrentAgentConn(conn *agentConn) bool {
	if conn == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	owners := m.conns[conn.agentID]
	return owners != nil && owners[conn.ownerID] == conn
}

func (c *agentConn) nextSeq() int64 {
	return atomic.AddInt64(&c.seq, 1)
}

func (c *agentConn) sendPayload(cmd string, seq int64, payload any) bool {
	raw, _ := json.Marshal(payload)
	packet := protocol.Packet{
		Cmd:     cmd,
		Seq:     seq,
		Payload: raw,
	}
	data, err := json.Marshal(packet)
	if err != nil {
		return false
	}
	if c.closed.Load() {
		return false
	}
	// 生产者有多个（重放、事件下推、local_action…），与 close() 天然并发。
	// send 通道永不关闭——多生产者下关它，会让「先判活再写」的生产者
	// 踩到 send on closed channel 而 panic；连接是否终止一律看 done。
	//
	// 「判活」与「入队」必须在同一把锁里，且 close() 争同一把锁。否则两种坏结果二选一：
	//   · 判活与入队之间连接被关 → 包进了没人消费的缓冲区，却报「已送达」，
	//     调用方跳过离线入队（PushToAgent、event_edit 重放还会顺手删掉队列记录）→ 消息静默丢失；
	//   · 若改用「入队后再复查 done」来兜 → 已经写出网卡的包被误判为失败，
	//     调用方再排一次离线队列 → agent 重连后收到重复事件，同一条消息执行两遍。
	// 加锁之后两者都不会发生：close 之后必然报失败，入队成功就一定还没关。
	c.sendMu.Lock()
	select {
	case <-c.done:
		c.sendMu.Unlock()
		return false
	default:
	}
	select {
	case c.send <- data:
		c.sendMu.Unlock()
		return true
	default:
	}
	c.sendMu.Unlock()

	// 通道打满说明对端消费不动了，按既有语义直接断开这条连接。
	// close() 要争 sendMu，必须在锁外调用。
	c.close()
	return false
}

func (c *agentConn) writePump(heartbeat time.Duration) {
	ticker := time.NewTicker(heartbeat)
	defer func() {
		ticker.Stop()
		c.close()
		// 关停关闭时 close() 特意不关 TCP（让上面的 1001 关闭帧先出去），
		// 无论 writePump 从哪个分支退出，这里兜底关闭底层连接。
		if c.shutdownClose.Load() && c.ws != nil {
			_ = c.ws.Close()
		}
	}()

	for {
		select {
		case <-c.done:
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			closeFrame := []byte{}
			if c.shutdownClose.Load() {
				closeFrame = websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down")
			}
			_ = c.ws.WriteMessage(websocket.CloseMessage, closeFrame)
			return
		case data := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			var pkt protocol.Packet
			if err := json.Unmarshal(data, &pkt); err == nil && pkt.Cmd == protocol.CmdEventStop {
				logger.L.Infof(
					"agent_output_stop write event_stop agent=%d client=%s remote=%s seq=%d bytes=%d",
					c.agentID,
					c.clientID,
					c.ws.RemoteAddr().String(),
					pkt.Seq,
					len(data),
				)
			}
			if err := c.ws.WriteMessage(websocket.TextMessage, data); err != nil {
				if pkt.Cmd == protocol.CmdEventStop {
					logger.L.Warnf(
						"agent_output_stop write event_stop failed agent=%d client=%s remote=%s seq=%d err=%v",
						c.agentID,
						c.clientID,
						c.ws.RemoteAddr().String(),
						pkt.Seq,
						err,
					)
				}
				return
			}
		case <-ticker.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// spawn 起一个属于本连接的协程；ServeWS 返回前会等它退出。
func (c *agentConn) spawn(fn func()) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		fn()
	}()
}

func (c *agentConn) close() {
	c.closeOnce.Do(func() {
		// 与 sendPayload 的「判活 + 入队」互斥：拿到锁就说明没有生产者卡在
		// 「已判活、尚未入队」的中间态，因此 done 关闭之后不会再有包被塞进
		// 没人消费的缓冲区却被报成「已送达」。
		c.sendMu.Lock()
		c.closed.Store(true)
		// 只关 done，不关 send：send 有并发生产者，关它会 panic。
		// done 关闭后 writePump 立即退出，生产者也不再写入。
		if c.done != nil {
			close(c.done)
		}
		c.sendMu.Unlock()

		if c.ws != nil {
			if c.shutdownClose.Load() {
				// 关停关闭：这里直接关 TCP 会抢在 writePump 的 1001 关闭帧前面，
				// 对端只能看到 1006 异常断开。TCP 由 writePump 退出时负责关闭。
			} else {
				_ = c.ws.Close()
			}
		}
	})
}

// adapterResultAdapter extracts the AgentAdapter from a SelectResult, returning
// nil if no adapter was selected.
func adapterResultAdapter(result *agentadapter.SelectResult) agentadapter.AgentAdapter {
	if result == nil {
		return nil
	}
	return result.Adapter
}

func missingDeclaredNames(values []string, required []string) []string {
	if len(required) == 0 {
		return nil
	}
	declared := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		declared[normalized] = struct{}{}
	}
	missing := make([]string, 0, len(required))
	for _, expected := range required {
		normalized := strings.ToLower(strings.TrimSpace(expected))
		if normalized == "" {
			continue
		}
		if _, ok := declared[normalized]; ok {
			continue
		}
		missing = append(missing, normalized)
	}
	return missing
}

func parseAuthSkills(raw json.RawMessage) []toolruntime.SkillEntry {
	if len(raw) == 0 {
		return nil
	}
	var skills []toolruntime.SkillEntry
	if err := json.Unmarshal(raw, &skills); err != nil {
		return nil
	}
	return skills
}

// parseLibrarySkills 解析 connector 上报的技能库全集（技能库启用，方案 v2）。
func parseLibrarySkills(raw json.RawMessage) []toolruntime.LibrarySkillEntry {
	if len(raw) == 0 {
		return nil
	}
	var skills []toolruntime.LibrarySkillEntry
	if err := json.Unmarshal(raw, &skills); err != nil {
		return nil
	}
	return skills
}

// resolveTailnetIP 从 auth payload 中提取并验证 Tailnet IP。
// 只有当 IP 确实属于 Tailscale 网段时才返回，否则返回空字符串。
func resolveTailnetIP(payload AuthPayload) string {
	ip := strings.TrimSpace(payload.TailnetIP)
	if ip == "" {
		return ""
	}
	if !tailnet.IsTailnetIP(ip) {
		logger.L.Warnf("agent auth tailnet_ip rejected (not in tailnet range): %s", ip)
		return ""
	}
	return ip
}

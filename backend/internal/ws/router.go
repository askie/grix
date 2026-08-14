package ws

import (
	"encoding/json"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func RoutePacket(hub *Hub, c *Conn, pkt *protocol.Packet) {
	// Auth must be first packet
	if !c.authed && pkt.Cmd != protocol.CmdAuth {
		logger.L.Warnf("unauthenticated packet cmd=%s", pkt.Cmd)
		return
	}
	if c.authed && c.sessionID != "" && security.IsLoginSessionRevoked(c.userID, c.sessionID) {
		c.SendPayload(protocol.CmdKicked, c.NextSeq(), map[string]string{
			"reason": "session_revoked",
		})
		hub.Unregister(c)
		c.Close()
		c.closeWebsocket()
		return
	}

	switch pkt.Cmd {
	case protocol.CmdAuth:
		handler.HandleAuth(hub, c, pkt)
	case protocol.CmdPing:
		handler.HandlePing(hub, c, pkt)
	case protocol.CmdSendMsg:
		handler.HandleSendMsg(hub, c, pkt)
	case protocol.CmdRetryMsg:
		handler.HandleRetryMsg(hub, c, pkt)
	case protocol.CmdPushAck:
		handler.HandlePushAck(hub, c, pkt)
	case protocol.CmdAppStateSet:
		handleAppStateSet(c, pkt)
	case protocol.CmdPullSync:
		handler.HandlePullSync(hub, c, pkt)
	case protocol.CmdSessionRead:
		handler.HandleSessionRead(hub, c, pkt)
	case protocol.CmdSessionHistoryReset:
		handler.HandleSessionHistoryReset(hub, c, pkt)
	case protocol.CmdSessionHistoryResetsQuery:
		handler.HandleSessionHistoryResetsQuery(hub, c, pkt)
	case protocol.CmdFriendSync:
		handler.HandleFriendSync(hub, c, pkt)
	case protocol.CmdSessionActivitySet:
		handler.HandleSessionActivitySet(hub, c, pkt)
	case protocol.CmdSessionActivityList:
		handler.HandleSessionActivityList(hub, c, pkt)
	case protocol.CmdStreamStop:
		handler.HandleStreamStop(hub, c, pkt)
	case protocol.CmdOverrideStream:
		handler.HandleOverride(hub, c, pkt)
	case protocol.CmdReAuth:
		handler.HandleReAuth(hub, c, pkt)
	case protocol.CmdClientStreamChunk:
		handler.HandleClientStreamChunk(hub, c, pkt)
	case protocol.CmdDelegateStart:
		handler.HandleDelegateStart(hub, c, pkt)
	case protocol.CmdDelegateStop:
		handler.HandleDelegateStop(hub, c, pkt)
	case protocol.CmdDelegateList:
		handler.HandleDelegateList(hub, c, pkt)
	case protocol.CmdAgentOutputGet:
		handler.HandleAgentOutputGet(hub, c, pkt)
	case protocol.CmdAgentOutputStop:
		handler.HandleAgentOutputStop(hub, c, pkt)
	case protocol.CmdAgentToolbarGet:
		handler.HandleAgentToolbarGet(hub, c, pkt)
	case protocol.CmdAgentToolbarAction:
		handler.HandleAgentToolbarAction(hub, c, pkt)
	case protocol.CmdConversationAuditSet:
		handler.HandleConversationAuditSet(hub, c, pkt)
	case protocol.CmdEventCancel:
		handler.HandleEventCancel(hub, c, pkt)
	case protocol.CmdQueueClear:
		handler.HandleQueueClear(hub, c, pkt)
	case protocol.CmdQueueReorder:
		handler.HandleQueueReorder(hub, c, pkt)
	case protocol.CmdEventHold:
		handler.HandleEventHold(hub, c, pkt)
	case protocol.CmdQueueEdit:
		handler.HandleQueueEdit(hub, c, pkt)
	case protocol.CmdQueueSnapshotQuery:
		handler.HandleQueueSnapshotQuery(hub, c, pkt)
	case protocol.CmdRelayLocalStreamStart:
		handler.HandleRelayLocalStreamStart(hub, c, pkt)
	case protocol.CmdRelayLocalStreamChunk:
		handler.HandleRelayLocalStreamChunk(hub, c, pkt)
	case protocol.CmdRelayLocalStreamFinish:
		handler.HandleRelayLocalStreamFinish(hub, c, pkt)
	case protocol.CmdAgentFileList:
		handler.HandleAgentFileList(hub, c, pkt)
	case protocol.CmdAgentSkillUpload:
		handler.HandleAgentSkillUpload(hub, c, pkt)
	case protocol.CmdAgentSkillEnable:
		handler.HandleAgentSkillEnable(hub, c, pkt)
	case protocol.CmdAgentSkillDisable:
		handler.HandleAgentSkillDisable(hub, c, pkt)
	case protocol.CmdAgentSkillRefresh:
		handler.HandleAgentSkillRefresh(hub, c, pkt)
	case protocol.CmdAgentCreateFolder:
		handler.HandleAgentCreateFolder(hub, c, pkt)
	case protocol.CmdAgentSessionBindingsList:
		handler.HandleAgentSessionBindingsList(hub, c, pkt)
	case protocol.CmdAgentSessionBind:
		handler.HandleAgentSessionBind(hub, c, pkt)
	case protocol.CmdAuditGetManifest:
		handler.HandleAuditGetManifest(hub, c, pkt)
	case protocol.CmdAuditListSpans:
		handler.HandleAuditListSpans(hub, c, pkt)
	case protocol.CmdAuditGetContentChunk:
		handler.HandleAuditGetContentChunk(hub, c, pkt)
	case protocol.CmdMcpFrame:
		handler.HandleMcpFrame(hub, c, pkt)
	// 语音通话信令（Phase 1）
	case protocol.CmdCallInvite:
		handler.HandleCallInvite(hub, c, pkt)
	case protocol.CmdCallAnswer:
		handler.HandleCallAnswer(hub, c, pkt)
	case protocol.CmdCallReject:
		handler.HandleCallReject(hub, c, pkt)
	case protocol.CmdCallHangup:
		handler.HandleCallHangup(hub, c, pkt)
	case protocol.CmdCallClientDiag:
		handler.HandleCallClientDiag(hub, c, pkt)
	// 语音通话信令（Phase 2：AI 托管）
	case protocol.CmdCallAnswerWithAI:
		handler.HandleCallAnswerWithAI(hub, c, pkt)
	case protocol.CmdCallTakeover:
		handler.HandleCallTakeover(hub, c, pkt)
	case protocol.CmdCallHandBack:
		handler.HandleCallHandBack(hub, c, pkt)
	case protocol.CmdCallListen:
		handler.HandleCallListen(hub, c, pkt)
	case protocol.CmdCallLeave:
		handler.HandleCallLeave(hub, c, pkt)
	case protocol.CmdCallDirectAI:
		handler.HandleCallDirectAI(hub, c, pkt)
	case protocol.CmdCallVoiceBrain:
		handler.HandleCallVoiceBrain(hub, c, pkt)
	case protocol.CmdCallVoiceDelegateStart:
		handler.HandleCallVoiceDelegateStart(hub, c, pkt)
	case protocol.CmdCallVoiceDelegateStop:
		handler.HandleCallVoiceDelegateStop(hub, c, pkt)
	default:
		logger.L.Warnf("unknown cmd: %s", pkt.Cmd)
	}
}

func handleAppStateSet(c *Conn, pkt *protocol.Packet) {
	if c == nil || pkt == nil {
		return
	}

	var payload protocol.AppStateSetPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("app_state_set payload error user=%d device=%s err=%v", c.userID, c.deviceID, err)
		return
	}
	if !c.setAppState(payload.AppState) {
		logger.L.Warnf("app_state_set invalid user=%d device=%s value=%q", c.userID, c.deviceID, payload.AppState)
		return
	}
	// 前台/后台切换后刷新 Redis 标记，让 pickMcpTargetDevice 跟随用户当前在用的设备。
	publishDeviceAppState(c.userID, c.deviceID, c.appStateString())
	if err := c.refreshReadDeadline(); err != nil {
		logger.L.Warnf("app_state_set refresh read deadline failed user=%d device=%s state=%s err=%v", c.userID, c.deviceID, c.appStateString(), err)
		return
	}
	logger.L.Infof(
		"app_state_set user=%d device=%s platform=%s state=%s app_state_reported=%v route_mode=%s idle_ms=%d",
		c.userID,
		c.deviceID,
		c.platform,
		c.appStateString(),
		c.appStateReported.Load(),
		c.mobileConnRoutingMode(),
		c.inboundIdleDuration().Milliseconds(),
	)
}

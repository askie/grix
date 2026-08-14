package ws

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/clientip"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/gorilla/websocket"
)

const widgetPlatform = handler.WidgetPlatform

func (s *Server) handleWidgetWS(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(req *http.Request) bool {
			return true
		},
	}
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.L.Warnf("widget ws upgrade error: %v", err)
		return
	}

	conn := NewConn(wsConn)
	go conn.WritePump()
	defer func() {
		s.hub.Unregister(conn)
		conn.Close()
		conn.closeWebsocket()
	}()

	_ = wsConn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, firstRaw, err := wsConn.ReadMessage()
	if err != nil {
		logger.L.Warnf("widget ws auth first packet read failed: %v", err)
		return
	}

	var firstPkt protocol.Packet
	if err := json.Unmarshal(firstRaw, &firstPkt); err != nil {
		sendWidgetAuthFailure(conn, 1, "invalid auth packet")
		return
	}
	if firstPkt.Cmd != protocol.CmdAuth {
		sendWidgetAuthFailure(conn, firstPkt.Seq, "auth required as first packet")
		return
	}

	var authPayload protocol.AuthPayload
	if err := json.Unmarshal(firstPkt.Payload, &authPayload); err != nil {
		sendWidgetAuthFailure(conn, firstPkt.Seq, "invalid auth payload")
		return
	}
	claims, err := jwtpkg.ValidateWidgetAccessToken(authPayload.Token)
	if err != nil {
		logger.L.Warnf("call trace: widget ws auth_fail reason=token_invalid seq=%d", firstPkt.Seq)
		sendWidgetAuthFailure(conn, firstPkt.Seq, "token invalid or expired")
		return
	}
	if strings.TrimSpace(claims.SessionID) == "" || claims.WidgetSiteID <= 0 || claims.WidgetVisitorID <= 0 {
		sendWidgetAuthFailure(conn, firstPkt.Seq, "invalid widget token")
		return
	}

	var wsSession model.WidgetSession
	if err := store.DB.Where("session_id = ?", claims.SessionID).First(&wsSession).Error; err != nil {
		logger.L.Warnf("call trace: widget ws auth_fail reason=session_invalid seq=%d session=%s", firstPkt.Seq, claims.SessionID)
		sendWidgetAuthFailure(conn, firstPkt.Seq, "session invalid")
		return
	}
	if wsSession.Status != model.WidgetSessionStatusActive {
		logger.L.Warnf("call trace: widget ws auth_fail reason=not_active seq=%d session=%s status=%d", firstPkt.Seq, claims.SessionID, wsSession.Status)
		sendWidgetAuthFailure(conn, firstPkt.Seq, "session not active")
		return
	}
	if err := jwtpkg.ValidateWidgetSessionBinding(claims, wsSession.SiteID, wsSession.SessionID, wsSession.VisitorID); err != nil {
		logger.L.Warnf("call trace: widget ws auth_fail reason=binding_mismatch seq=%d", firstPkt.Seq)
		sendWidgetAuthFailure(conn, firstPkt.Seq, "session mismatch")
		return
	}

	// owner 维度的访客 IP 封禁：被封 IP 不允许接入 widget WS。
	clientIP := clientip.FromRequest(r)
	if security.IsWidgetIPBanned(wsSession.OwnerUserID, clientIP) {
		logger.L.Warnf("call trace: widget ws auth_fail reason=ip_banned seq=%d session=%s owner=%d", firstPkt.Seq, claims.SessionID, wsSession.OwnerUserID)
		sendWidgetAuthFailure(conn, firstPkt.Seq, "visitor ip is banned")
		return
	}

	deviceID := "widget_" + wsSession.VisitorKey
	conn.SetAuth(wsSession.VisitorID, "widget:"+wsSession.SessionID, deviceID, widgetPlatform)
	conn.SetWidgetContext(wsSession.OwnerUserID, clientIP)
	s.hub.Register(conn)
	conn.SendPayload(protocol.CmdAuthAck, firstPkt.Seq, protocol.AuthAckPayload{Code: 0, UserID: wsSession.VisitorID, Msg: "ok"})
	_ = wsConn.SetReadDeadline(time.Now().Add(90 * time.Second))
	logger.L.Infof("call trace: widget ws connected visitor=%d owner=%d session=%s device=%s", wsSession.VisitorID, wsSession.OwnerUserID, wsSession.SessionID, deviceID)

	for {
		_, raw, err := wsConn.ReadMessage()
		if err != nil {
			logger.L.Infof("call trace: widget ws disconnected visitor=%d err=%v", wsSession.VisitorID, err)
			return
		}
		conn.markInboundActivity()
		_ = conn.refreshReadDeadline()

		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			continue
		}
		if !isWidgetAllowedCmd(pkt.Cmd) {
			conn.SendPayload(protocol.CmdSendNack, pkt.Seq, protocol.SendNackPayload{Code: 4004, Msg: "unsupported cmd"})
			continue
		}
		switch pkt.Cmd {
		case protocol.CmdPing:
			handler.HandlePing(s.hub, conn, &pkt)
		case protocol.CmdPushAck:
			handler.HandlePushAck(s.hub, conn, &pkt)
		case protocol.CmdSendMsg:
			if widgetConnIPBanned(conn) {
				conn.SendPayload(protocol.CmdSendNack, pkt.Seq, protocol.SendNackPayload{Code: 4003, Msg: "visitor ip is banned"})
				continue
			}
			handler.HandleSendMsg(s.hub, conn, &pkt)
		case protocol.CmdPullSync:
			handler.HandlePullSync(s.hub, conn, &pkt)
		case protocol.CmdCallInvite:
			if widgetConnIPBanned(conn) {
				conn.SendPayload(protocol.CmdSendNack, pkt.Seq, protocol.SendNackPayload{Code: 4003, Msg: "visitor ip is banned"})
				continue
			}
			logger.L.Infof("call trace: widget ws cmd_call_invite visitor=%d seq=%d", wsSession.VisitorID, pkt.Seq)
			// 访客语音客服：被叫与会话由可信的 widget 会话决定，跳过好友校验
			handler.HandleWidgetCallInvite(s.hub, conn, &pkt, wsSession.OwnerUserID, wsSession.SessionID, wsSession.VisitorName)
		case protocol.CmdCallHangup:
			handler.HandleCallHangup(s.hub, conn, &pkt)
		case protocol.CmdCallClientDiag:
			handler.HandleCallClientDiag(s.hub, conn, &pkt)
		}
	}
}

func sendWidgetAuthFailure(conn *Conn, seq int64, msg string) {
	conn.SendPayload(protocol.CmdAuthAck, seq, protocol.AuthAckPayload{Code: 10001, Msg: msg})
}

// widgetConnIPBanned 判断该 widget 连接的来源 IP 当前是否被 owner 封禁。
// 连接期间 ban 的访客靠这条消息级检查即时生效（走 security 的 30s 缓存）。
func widgetConnIPBanned(conn *Conn) bool {
	ownerUserID, clientIP := conn.GetWidgetContext()
	if ownerUserID <= 0 {
		return false
	}
	return security.IsWidgetIPBanned(ownerUserID, clientIP)
}

func isWidgetAllowedCmd(cmd string) bool {
	switch cmd {
	case protocol.CmdPing, protocol.CmdPushAck, protocol.CmdSendMsg, protocol.CmdPullSync,
		protocol.CmdCallInvite, protocol.CmdCallHangup, protocol.CmdCallClientDiag:
		return true
	default:
		return false
	}
}

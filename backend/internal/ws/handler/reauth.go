package handler

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleReAuth(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.ReAuthPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("re_auth payload error: %v", err)
		return
	}

	claims, err := jwtpkg.ValidateAccessToken(payload.Token)
	if err != nil {
		conn.SendPayload(protocol.CmdReAuthAck, pkt.Seq, protocol.ReAuthAckPayload{
			Code: 10002, Msg: "凭证已失效，请重新登录",
		})
		return
	}

	if claims.UserID != conn.GetUserID() {
		conn.SendPayload(protocol.CmdReAuthAck, pkt.Seq, protocol.ReAuthAckPayload{
			Code: 10002, Msg: "用户身份不匹配",
		})
		return
	}
	if security.IsAccessTokenRevoked(claims.ID) {
		conn.SendPayload(protocol.CmdReAuthAck, pkt.Seq, protocol.ReAuthAckPayload{
			Code: 10002, Msg: "凭证已失效，请重新登录",
		})
		return
	}
	if security.IsLoginSessionRevoked(claims.UserID, claims.SessionID) {
		conn.SendPayload(protocol.CmdReAuthAck, pkt.Seq, protocol.ReAuthAckPayload{
			Code: 10002, Msg: "凭证已失效，请重新登录",
		})
		return
	}
	if err := security.EnsureUserActive(claims.UserID); err != nil {
		if errors.Is(err, security.ErrUserDisabled) || errors.Is(err, security.ErrUserNotFound) {
			conn.SendPayload(protocol.CmdReAuthAck, pkt.Seq, protocol.ReAuthAckPayload{
				Code: protocol.ReAuthCodeFatal, Msg: "用户已被禁用",
			})
			return
		}
		// 存储层故障。账号没问题，报可重试，客户端保留会话继续重连。
		logger.L.Warnf("re_auth user status check failed user=%d err=%v", claims.UserID, err)
		conn.SendPayload(protocol.CmdReAuthAck, pkt.Seq, protocol.ReAuthAckPayload{
			Code: protocol.AuthCodeRetryable, Msg: "服务暂时不可用，请稍后重试",
		})
		return
	}
	if err := service.TouchLoginDeviceSession(claims.UserID, claims.SessionID); err != nil {
		logger.L.Warnf("touch login device session failed user=%d sid=%s err=%v", claims.UserID, claims.SessionID, err)
	}

	expiresIn := int64(0)
	if claims.ExpiresAt != nil {
		remain := time.Until(claims.ExpiresAt.Time).Seconds()
		if remain > 0 {
			expiresIn = int64(remain)
		}
	}

	conn.SendPayload(protocol.CmdReAuthAck, pkt.Seq, protocol.ReAuthAckPayload{
		Code: 0, Msg: "令牌续期成功", ExpiresIn: expiresIn,
	})
	PushStoredAgentStates(conn)
	PushStoredAgentDeliveryStatuses(conn)
}

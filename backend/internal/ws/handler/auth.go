package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/customercoach"
	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleAuth(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.AuthPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("auth payload parse error: %v", err)
		return
	}

	claims, err := jwtpkg.ValidateAccessToken(payload.Token)
	if err != nil {
		sendAuthFailure(conn, pkt.Seq, "凭证已失效，请重新登录")
		return
	}
	if security.IsAccessTokenRevoked(claims.ID) {
		sendAuthFailure(conn, pkt.Seq, "凭证已失效，请重新登录")
		return
	}
	if err := security.EnsureUserActive(claims.UserID); err != nil {
		if errors.Is(err, security.ErrUserDisabled) || errors.Is(err, security.ErrUserNotFound) {
			sendAuthFailure(conn, pkt.Seq, "用户已被禁用")
			return
		}
		// 存储层故障。账号本身没问题，不能报成"被禁用"——那会让客户端清掉会话。
		logger.L.Warnf("auth user status check failed user=%d err=%v", claims.UserID, err)
		sendAuthRetryable(conn, pkt.Seq)
		return
	}
	if security.IsLoginSessionRevoked(claims.UserID, claims.SessionID) {
		sendAuthFailure(conn, pkt.Seq, "凭证已失效，请重新登录")
		return
	}

	deviceID := strings.TrimSpace(payload.DeviceID)
	platform := strings.TrimSpace(payload.Platform)
	if deviceID == "" || platform == "" {
		sendAuthFailure(conn, pkt.Seq, "用户身份不匹配")
		return
	}

	sid := strings.TrimSpace(claims.SessionID)
	if sid == "" {
		sendAuthFailure(conn, pkt.Seq, "凭证已失效，请重新登录")
		return
	}
	if err := service.EnsureLoginDeviceSessionReady(claims.UserID, sid, deviceID, platform); err != nil {
		msg := ""
		switch {
		case errors.Is(err, service.ErrLoginDeviceSessionNotFound):
			msg = "凭证已失效，请重新登录"
		case errors.Is(err, service.ErrLoginDeviceSessionMismatch):
			msg = "用户身份不匹配"
		case errors.Is(err, service.ErrLoginDeviceSessionIDRequired),
			errors.Is(err, service.ErrLoginDeviceSessionDeviceIDMiss),
			errors.Is(err, service.ErrLoginDeviceSessionPlatformMiss):
			msg = "用户身份不匹配"
		}
		if msg == "" {
			// 剩下的都是存储层故障。旧实现在这里兜底成"鉴权失败"，客户端无从分辨，
			// 数据库抖一下就会把在线用户当成凭证失效处理。
			logger.L.Warnf("login device session check failed user=%d sid=%s device=%s err=%v", claims.UserID, sid, deviceID, err)
			sendAuthRetryable(conn, pkt.Seq)
			return
		}
		logger.L.Warnf("login device session mismatch user=%d sid=%s device=%s err=%v", claims.UserID, sid, deviceID, err)
		sendAuthFailure(conn, pkt.Seq, msg)
		return
	}
	if err := service.TouchLoginDeviceSession(claims.UserID, sid); err != nil {
		logger.L.Warnf("touch login device session failed user=%d sid=%s err=%v", claims.UserID, sid, err)
	}

	conn.SetAuth(claims.UserID, sid, deviceID, platform)
	hub.Register(conn)

	latestInboxSeq := loadLatestInboxSeq(claims.UserID)
	conn.SendPayload(protocol.CmdAuthAck, pkt.Seq, protocol.AuthAckPayload{
		Code:           0,
		UserID:         claims.UserID,
		LatestInboxSeq: latestInboxSeq,
		Msg:            "鉴权成功",
	})
	PushStoredAgentStates(conn)
	PushStoredAgentDeliveryStatuses(conn)
	go func(userID int64) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := customercoach.TriggerOnUserOpen(ctx, userID, "ws_auth"); err != nil {
			logger.L.Warnf("customer coach trigger failed user=%d source=ws_auth err=%v", userID, err)
		}
	}(claims.UserID)
}

// sendAuthFailure 终态失败：凭证或账号本身的问题，重连不可能自愈。
func sendAuthFailure(conn ConnInterface, seq int64, msg string) {
	conn.SendPayload(protocol.CmdAuthAck, seq, protocol.AuthAckPayload{
		Code: protocol.AuthCodeFatal,
		Msg:  msg,
	})
}

// sendAuthRetryable 服务端自己暂时不可用，凭证没问题。客户端保留会话继续重连。
func sendAuthRetryable(conn ConnInterface, seq int64) {
	conn.SendPayload(protocol.CmdAuthAck, seq, protocol.AuthAckPayload{
		Code: protocol.AuthCodeRetryable,
		Msg:  "服务暂时不可用，请稍后重试",
	})
}

func loadLatestInboxSeq(userID int64) int64 {
	if userID <= 0 || store.DB == nil {
		return 0
	}

	var latestSeq int64
	if err := store.DB.Model(&model.UserInbox{}).
		Select("COALESCE(MAX(inbox_seq), 0)").
		Where("user_id = ?", userID).
		Scan(&latestSeq).Error; err != nil {
		logger.L.Warnf("load latest inbox seq failed user=%d err=%v", userID, err)
		return 0
	}
	if latestSeq < 0 {
		return 0
	}
	return latestSeq
}

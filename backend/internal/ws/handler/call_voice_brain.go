package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// HandleCallVoiceBrain 处理 owner 在"我↔文字 agent"私聊里发起的语音大脑通话。
//
// 与 direct_ai 的唯一区别：语音媒体用用户级"语音大脑"(type=4)，但通话锚定到
// owner↔文字 agent 的工作会话。语音转写以 owner 本人身份落入该会话，文字 agent 作为
// 会话成员被现有 direct-route 机制自动触发并以自身身份回复（接点A）；回复经
// MaybeInjectVoiceReply 注入语音侧念回（接点B）。摆渡链路全部复用 direct_ai 现成实现。
func HandleCallVoiceBrain(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	if callCtrl == nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("call service unavailable"))
		return
	}
	var payload protocol.CallVoiceBrainPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid payload"))
		return
	}
	textAgentID, err := strconv.ParseInt(payload.AgentID, 10, 64)
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid agent_id"))
		return
	}
	ownerID := conn.GetUserID()
	logger.L.Infof("call trace: voice_brain recv user=%d text_agent=%d seq=%d", ownerID, textAgentID, pkt.Seq)

	// 1. 校验文字 agent：owner 本人、可用、且非语音大模型（语音模型走 direct_ai）。
	if err := ensureTextBrainAgent(ownerID, textAgentID); err != nil {
		logger.L.Warnf("call voice_brain text agent check failed user=%d agent=%d err=%v", ownerID, textAgentID, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}

	// 2. 解析用户级语音大脑(type=4)，作为本通话的语音通道（媒体侧）。
	voiceAgentID, ok := loadVoiceBrainAgentID(ownerID)
	if !ok {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("请先在设置里选择语音大脑"))
		return
	}
	if err := ensureVoiceAgentOwner(ownerID, voiceAgentID); err != nil {
		logger.L.Warnf("call voice_brain voice agent check failed user=%d voice=%d err=%v", ownerID, voiceAgentID, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}

	// 3. 锚定会话：session_id 必传，校验文字 agent 是该会话成员，防止转写落入无关会话。
	sessionID := payload.SessionID
	if sessionID == "" {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("session_id is required"))
		return
	}
	if err := ensureAgentInSession(sessionID, textAgentID); err != nil {
		logger.L.Warnf("call voice_brain session member check failed user=%d agent=%d session=%s err=%v", ownerID, textAgentID, sessionID, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("文字 agent 不在指定会话中"))
		return
	}

	// 4. 媒体 spec 取自语音大脑。spec.AgentID=voiceAgentID → CalleeID/DelegatedAgentID=voiceAgentID，
	//    direct 判定成立（注入超时放宽/每轮多条），与 direct_ai 一致。
	spec, err := resolveAgentVoiceSpec(voiceAgentID, "")
	if err != nil {
		logger.L.Warnf("call voice_brain resolve spec failed user=%d voice=%d err=%v", ownerID, voiceAgentID, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}
	// 工作模式（用户级开关，仅作用于语音大脑，不影响客服）：
	//   实时互动(默认)：RelayMode=false → 豆包端到端实时听说，主人的话由豆包自答互动；
	//                   文字大脑回复经 502 external_rag 当背景资料注入，豆包参考后用自己口气接续。
	//   念稿兜底     ：RelayMode=true  → STT+TTS 管线，豆包只当嘴和耳，文字 agent 是唯一大脑、逐字念回。
	// 两条路径均已在 voicebridge 实现；此处只按开关下发 RelayMode，桥侧据此路由。
	spec.RelayMode = !loadVoiceBrainRealtime(ownerID)
	spec.TextAgentID = textAgentID
	if !reserveVoiceDailyQuota(voiceAgentID, spec.DailyLimit) {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("今日语音次数已达上限"))
		return
	}

	ctx := context.Background()
	callID := snowflake.GenID()
	busyGuard, err := reserveCallBusy(ctx, callID, ownerID)
	if err != nil {
		logger.L.Warnf("call voice_brain busy guard error user=%d call=%d err=%v", ownerID, callID, err)
		if errors.Is(err, call.ErrCallerBusy) {
			conn.SendPayload(protocol.CmdCallBusy, pkt.Seq, map[string]string{"reason": err.Error()})
			return
		}
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("internal error"))
		return
	}
	ownerCommitted := false
	defer func() {
		if !ownerCommitted {
			busyGuard.release(ctx)
		}
	}()

	_, token, roomURL, err := callCtrl.DirectAICallWithID(ctx, callID, ownerID, sessionID, spec)
	if err != nil {
		logger.L.Warnf("call voice_brain error user=%d voice=%d session=%s err=%v", ownerID, voiceAgentID, sessionID, err)
		if errors.Is(err, call.ErrCallerBusy) {
			conn.SendPayload(protocol.CmdCallBusy, pkt.Seq, map[string]string{"reason": err.Error()})
			return
		}
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}
	if err := rememberCallOwner(ctx, callID, hub.GetNodeID()); err != nil {
		logger.L.Errorf("call voice_brain owner guard error user=%d call=%d node=%s err=%v", ownerID, callID, hub.GetNodeID(), err)
		_ = callCtrl.Hangup(ctx, callID, ownerID)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("internal error"))
		return
	}
	ownerCommitted = true
	conn.SendPayload(protocol.CmdCallInviteAck, pkt.Seq, protocol.CallInviteAckPayload{
		CallID:     strconv.FormatInt(callID, 10),
		RoomToken:  token,
		RoomURL:    roomURL,
		ICEServers: callICEServers(),
	})
	logger.L.Infof("call trace: voice_brain invite_ack user=%d call=%d voice=%d text_agent=%d session=%s room_url=%s token_len=%d",
		ownerID, callID, voiceAgentID, textAgentID, sessionID, roomURL, len(token))
}

// loadVoiceBrainAgentID 读取用户级语音大脑 agent。可在测试中替换为 mock。
var loadVoiceBrainAgentID = func(userID int64) (int64, bool) {
	return apiservice.LoadUserVoiceBrainAgentID(userID)
}

// loadVoiceBrainRealtime 读取语音大脑工作模式：true=豆包实时互动(端到端+502背景注入)，
// false=STT+TTS 念稿兜底。可在测试中替换为 mock。
var loadVoiceBrainRealtime = func(userID int64) bool {
	return apiservice.LoadUserVoiceBrainRealtime(userID)
}

// ensureAgentInSession 校验 agentID 是指定会话的成员（member_type=2），防止转写落入无关会话。可在测试中替换。
var ensureAgentInSession = func(sessionID string, agentID int64) error {
	if store.DB == nil {
		return nil // 测试环境
	}
	var count int64
	store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 2", sessionID, agentID).
		Count(&count)
	if count == 0 {
		return fmt.Errorf("agent %d is not a member of session %s", agentID, sessionID)
	}
	return nil
}

// ensureTextBrainAgent 校验文字大脑 agent 存在、归属 owner、可用，且不是语音大模型。
// 语音大模型自身的语音通话走 direct_ai；语音大脑只对文字 agent 生效（互斥）。可在测试中替换。
var ensureTextBrainAgent = func(userID, agentID int64) error {
	if store.DB == nil {
		return nil // 测试环境
	}
	var ag struct {
		OwnerID      int64
		ProviderType int16
	}
	if err := store.DB.Model(&model.Agent{}).
		Select("owner_id, provider_type").
		Where("id = ? AND status = 1", agentID).
		Scan(&ag).Error; err != nil {
		return fmt.Errorf("query agent: %w", err)
	}
	if ag.OwnerID == 0 {
		return fmt.Errorf("agent %d not found or disabled", agentID)
	}
	if ag.OwnerID != userID {
		return fmt.Errorf("only the owner can use voice brain on this agent")
	}
	if ag.ProviderType == model.AgentProviderVoice {
		return fmt.Errorf("agent %d is a voice model; use direct call instead", agentID)
	}
	return nil
}

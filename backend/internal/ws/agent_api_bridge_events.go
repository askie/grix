package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/mention"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
)

var presignAgentAPIMediaUpload = service.OSSPresign

func (s *Server) handleAgentAPIDeleteMsg(
	_ context.Context,
	agentID, ownerID int64,
	payload agentapi.DeleteMsgPayload,
) error {
	err := service.DeleteMessage(context.Background(), payload.SessionID, payload.MsgID, service.MessageDeleteActor{
		UserID:  ownerID,
		AgentID: agentID,
	})
	if err != nil {
		if err.Error() == "20008" || err.Error() == "20008: 无权删除该消息" {
			return &agentapi.SendError{Code: 20008, Msg: "can only delete own messages"}
		}
		return &agentapi.SendError{Code: 5001, Msg: "delete failed"}
	}
	return nil
}

func (s *Server) handleAgentAPIEditMsg(
	_ context.Context,
	agentID, ownerID int64,
	payload agentapi.EditMsgPayload,
) error {
	err := service.EditMessage(
		context.Background(),
		payload.SessionID,
		payload.MsgID,
		service.MessageEditActor{
			UserID:  ownerID,
			AgentID: agentID,
		},
		payload.Content,
		payload.Extra,
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrMessageEditDenied):
			return &agentapi.SendError{Code: 20008, Msg: "can only edit own messages"}
		case errors.Is(err, service.ErrMessageNotFound):
			return &agentapi.SendError{Code: 4004, Msg: "message not found"}
		case errors.Is(err, service.ErrMessageContentEmpty):
			return &agentapi.SendError{Code: 4001, Msg: "message content required"}
		default:
			return &agentapi.SendError{Code: 5001, Msg: "edit failed"}
		}
	}
	return nil
}

func (s *Server) handleAgentAPIReactMsg(
	_ context.Context,
	agentID, ownerID int64,
	payload agentapi.ReactMsgPayload,
) error {
	ctx := context.Background()
	operation := strings.TrimSpace(payload.Op)
	if operation == "" {
		operation = "add"
	}

	var msg model.Message
	if err := store.DB.Where("msg_id = ? AND session_id = ?", payload.MsgID, payload.SessionID).
		First(&msg).Error; err != nil {
		return &agentapi.SendError{Code: 4004, Msg: "message not found"}
	}

	var memberCount int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", payload.SessionID, ownerID).
		Count(&memberCount).Error; err != nil || memberCount == 0 {
		return &agentapi.SendError{Code: 4003, Msg: "permission denied"}
	}

	switch operation {
	case "remove":
		if err := store.DB.Where(
			"msg_id = ? AND session_id = ? AND user_id = ? AND emoji = ?",
			payload.MsgID,
			payload.SessionID,
			ownerID,
			payload.Emoji,
		).Delete(&model.MessageReaction{}).Error; err != nil {
			return &agentapi.SendError{Code: 5001, Msg: "react failed"}
		}
	default:
		reaction := model.MessageReaction{
			MsgID:     payload.MsgID,
			SessionID: payload.SessionID,
			UserID:    ownerID,
			Emoji:     payload.Emoji,
		}
		if err := store.DB.Create(&reaction).Error; err != nil {
			logger.L.Debugf("react_msg duplicate or error user=%d msg=%d emoji=%s: %v", ownerID, payload.MsgID, payload.Emoji, err)
		}
	}

	reactEvent := map[string]interface{}{
		"msg_id":     fmt.Sprintf("%d", payload.MsgID),
		"session_id": payload.SessionID,
		"actor_id":   fmt.Sprintf("%d", ownerID),
		"emoji":      payload.Emoji,
		"op":         operation,
	}
	agentmsg.BroadcastToSession(ctx, payload.SessionID, "event_react", reactEvent)
	// 按 ownerID(=发起 react 的 agent 连接的 owner)精确路由,确保共享场景下
	// B 通过 X 发 react 时,事件回到 B 的 connector,而不是主人 A 的。
	ForwardEventToAgents(payload.SessionID, ownerID, "event_react", reactEvent)
	return nil
}

func (s *Server) handleAgentAPIMediaUploadInit(
	_ context.Context,
	agentID, ownerID int64,
	payload agentapi.MediaUploadInitPayload,
) (*agentapi.MediaUploadInitResult, error) {
	presignResp, err := presignAgentAPIMediaUpload(ownerID, payload.Name, payload.Mime)
	if err != nil {
		return nil, &agentapi.SendError{Code: 5001, Msg: "media upload init failed"}
	}
	return &agentapi.MediaUploadInitResult{
		UploadID:  payload.UploadID,
		UploadURL: presignResp.UploadURL,
		Method:    "PUT",
		MediaURL:  presignResp.MediaAccessURL,
	}, nil
}

// ForwardEventToAgents 把事件转发给会话内所有 agent 成员。ownerID 用于 agent 共享多连接物理隔离:
//   - >0: 按 (agentID, ownerID) 严格路由,共享场景下事件落到对应被共享者的 connector
//   - 0:  回退主连接(兼容旧路径)
func ForwardEventToAgents(sessionID string, ownerID int64, cmd string, payload interface{}) {
	mgr := agentapi.GetGlobal()
	if mgr == nil {
		return
	}

	var members []model.SessionMember
	store.DB.Where("session_id = ? AND member_type = 2", sessionID).Find(&members)
	for _, m := range members {
		mgr.PushToAgent(m.MemberID, ownerID, cmd, payload)
	}
}

func mergeAgentAPIExtraWithIdentity(raw json.RawMessage, content string, agentID int64, identity *agentmsg.SenderIdentity) json.RawMessage {
	extra := map[string]any{
		"agent_api_origin": true,
		"agent_id":         fmt.Sprintf("%d", agentID),
	}
	if identity.IsDelegated {
		extra["delegate_origin"] = true
	}
	if len(raw) == 0 {
		merged, _ := json.Marshal(extra)
		return mention.NormalizeExtra(merged, content)
	}

	var incoming map[string]any
	if err := json.Unmarshal(raw, &incoming); err == nil {
		for k, v := range incoming {
			extra[k] = v
		}
		merged, _ := json.Marshal(extra)
		return mention.NormalizeExtra(merged, content)
	}

	merged, _ := json.Marshal(extra)
	return mention.NormalizeExtra(merged, content)
}

func mergeAgentAPIExtra(raw json.RawMessage, content string, agentID int64, isDelegated bool) json.RawMessage {
	identity := &agentmsg.SenderIdentity{IsDelegated: isDelegated}
	return mergeAgentAPIExtraWithIdentity(raw, content, agentID, identity)
}

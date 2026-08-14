package ws

import (
	"github.com/askie/grix/backend/internal/api/service"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
)

type serviceAgentChannelBridge struct{}

func (serviceAgentChannelBridge) PushAgentEvent(agentID, ownerID int64, cmd string, payload interface{}) bool {
	return wsagentapi.PushAgentEvent(agentID, ownerID, cmd, payload)
}

func (serviceAgentChannelBridge) PushDelegateEvent(event service.AgentDelegateEvent) bool {
	return wsagentapi.PushDelegateEvent(wsagentapi.DelegateEventPayload{
		EventID:     event.EventID,
		EventType:   event.EventType,
		AgentID:     event.AgentID,
		OwnerID:     event.OwnerID,
		SessionID:   event.SessionID,
		ThreadID:    event.ThreadID,
		SessionType: event.SessionType,
		MsgID:       event.MsgID,
		SenderID:    event.SenderID,
		MsgType:     event.MsgType,
		Content:     event.Content,
		CreatedAt:   event.CreatedAt,
	})
}

func (serviceAgentChannelBridge) IsAgentChannelAvailable(agentID int64) bool {
	return wsagentapi.IsAgentChannelAvailable(agentID)
}

func (serviceAgentChannelBridge) GetAgentClientType(agentID int64) string {
	return wsagentapi.GetAgentClientType(agentID)
}

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

type localStreamRequestState struct {
	SessionID    string `json:"session_id"`
	AgentID      int64  `json:"agent_id"`
	TriggerMsgID int64  `json:"trigger_msg_id"`
	MsgID        int64  `json:"msg_id"`
}

var localStreamRequestRegistry sync.Map

func HandleInternalRedisDispatch(cmd string, payload json.RawMessage) bool {
	switch cmd {
	case protocol.InternalCmdSessionTypeInvalidate:
		return handleSessionTypeInvalidate(payload)
	case internalCmdCallRPCRequest:
		return handleCallRPCRequest(payload)
	case internalCmdCallRPCResponse:
		return handleCallRPCResponse(payload)
	}
	return false
}

func localStreamActiveKey(sessionID string) string {
	return fmt.Sprintf("relay:local:active:%s", sessionID)
}

func localStreamRequestKey(sessionID string, agentID int64, triggerMsgID int64) string {
	return fmt.Sprintf("relay:local:req:%s:%d:%d", sessionID, agentID, triggerMsgID)
}

func findLocalStreamStartState(
	_ context.Context,
	payload protocol.RelayLocalStreamStartPayload,
) (localStreamRequestState, error) {
	if payload.SessionID == "" || payload.AgentID <= 0 || payload.TriggerMsgID <= 0 {
		return localStreamRequestState{}, nil
	}
	return loadLocalStreamRequestState(payload.SessionID, payload.AgentID, payload.TriggerMsgID), nil
}

func storeLocalStreamStartState(
	_ context.Context,
	payload protocol.RelayLocalStreamStartPayload,
	msgID int64,
) error {
	if payload.SessionID == "" || payload.AgentID <= 0 || payload.TriggerMsgID <= 0 || msgID <= 0 {
		return nil
	}
	state := localStreamRequestState{
		SessionID:    payload.SessionID,
		AgentID:      payload.AgentID,
		TriggerMsgID: payload.TriggerMsgID,
		MsgID:        msgID,
	}
	localStreamRequestRegistry.Store(localStreamRequestKey(payload.SessionID, payload.AgentID, payload.TriggerMsgID), state)
	return nil
}

func loadLocalStreamRequestState(sessionID string, agentID int64, triggerMsgID int64) localStreamRequestState {
	if sessionID == "" || agentID <= 0 || triggerMsgID <= 0 {
		return localStreamRequestState{}
	}
	value, ok := localStreamRequestRegistry.Load(localStreamRequestKey(sessionID, agentID, triggerMsgID))
	if !ok {
		return localStreamRequestState{}
	}
	state, ok := value.(localStreamRequestState)
	if !ok || !state.valid() {
		localStreamRequestRegistry.Delete(localStreamRequestKey(sessionID, agentID, triggerMsgID))
		return localStreamRequestState{}
	}
	return state
}

func clearLocalStreamStartState(
	_ context.Context,
	sessionID string,
	agentID int64,
	triggerMsgID int64,
	_ int64,
) error {
	if sessionID == "" || agentID <= 0 || triggerMsgID <= 0 {
		return nil
	}
	localStreamRequestRegistry.Delete(localStreamRequestKey(sessionID, agentID, triggerMsgID))
	return nil
}

func buildLocalStreamActivity(sessionID string, agentID int64, triggerMsgID int64, msgID int64) protocol.SessionActivityPayload {
	return protocol.SessionActivityPayload{
		SessionID:    sessionID,
		Kind:         protocol.SessionActivityKindComposing,
		ActorID:      agentID,
		ActorType:    protocol.SessionActivityActorTypeAgent,
		ExecutorID:   agentID,
		ExecutorType: protocol.SessionActivityActorTypeAgent,
		Source:       protocol.SessionActivitySourceLocalAgent,
		RefMsgID:     fmt.Sprintf("%d", msgID),
		RefEventID:   fmt.Sprintf("%d", triggerMsgID),
	}
}

func (s localStreamRequestState) valid() bool {
	return s.SessionID != "" && s.AgentID > 0 && s.TriggerMsgID > 0 && s.MsgID > 0
}

package handler

import (
	"encoding/json"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

// ConnInterface abstracts ws.Conn to avoid import cycle.
type ConnInterface interface {
	SendPayload(cmd string, seq int64, payload interface{})
	SendPacket(pkt *protocol.Packet)
	AckPush(msgID int64)
	NextSeq() int64
	Close()
	GetUserID() int64
	GetDeviceID() string
	GetPlatform() string
	SetAuth(userID int64, sessionID, deviceID, platform string)
	IsAuthed() bool
}

// HubInterface abstracts ws.Hub to avoid import cycle.
type HubInterface interface {
	Register(c ConnInterface)
	Unregister(c ConnInterface)
	RefreshAlive(c ConnInterface)
	GetUserConns(userID int64) []ConnInterface
	GetNodeID() string
}

func marshalPayload(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

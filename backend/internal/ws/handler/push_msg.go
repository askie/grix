package handler

import (
	"encoding/json"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandlePushAck(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.PushAckPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("push_ack payload error: %v", err)
		return
	}
	conn.AckPush(payload.MsgID)
	// ACK received, cancel any pending retransmission for this msg_id
	// In a production system, this would remove the message from a retry queue
	logger.L.Debugf("push_ack received for msg_id=%d", payload.MsgID)
}

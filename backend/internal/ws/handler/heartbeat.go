package handler

import (
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandlePing(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	hub.RefreshAlive(conn)
	conn.SendPayload(protocol.CmdPong, pkt.Seq, map[string]int64{
		"server_time": time.Now().UnixMilli(),
	})
}

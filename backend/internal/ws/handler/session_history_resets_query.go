package handler

import (
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleSessionHistoryResetsQuery(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	userID := conn.GetUserID()
	if userID <= 0 {
		conn.SendPayload(protocol.CmdSessionHistoryResetsQueryAck, pkt.Seq, protocol.SessionHistoryResetsQueryAckPayload{})
		return
	}

	var resets []model.SessionHistoryReset
	if err := store.DB.Where("user_id = ?", userID).Find(&resets).Error; err != nil {
		logger.L.Warnf("session_history_resets_query error user=%d: %v", userID, err)
		conn.SendPayload(protocol.CmdSessionHistoryResetsQueryAck, pkt.Seq, protocol.SessionHistoryResetsQueryAckPayload{})
		return
	}

	items := make([]protocol.SessionHistoryResetPayload, 0, len(resets))
	for _, r := range resets {
		items = append(items, protocol.SessionHistoryResetPayload{
			SessionID: r.SessionID,
			DeletedAt: r.DeletedBefore.UnixMilli(),
		})
	}
	conn.SendPayload(protocol.CmdSessionHistoryResetsQueryAck, pkt.Seq, protocol.SessionHistoryResetsQueryAckPayload{
		Resets: items,
	})
}

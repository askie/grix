package agentmsg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// BroadcastToSession publishes a command to all human members of a session via Redis Pub/Sub.
func BroadcastToSession(ctx context.Context, sessionID, cmd string, payload interface{}) {
	BroadcastToSessionWithMembers(ctx, sessionID, cmd, payload, nil)
}

// BroadcastToSessionWithMembers is like BroadcastToSession but accepts pre-queried
// members to avoid repeated DB lookups during streaming. If members is nil, it falls
// back to querying the DB.
func BroadcastToSessionWithMembers(ctx context.Context, sessionID, cmd string, payload interface{}, members []model.SessionMember) {
	if members == nil {
		store.DB.Where("session_id = ? AND member_type = 1", sessionID).Find(&members)
	}

	payloadData, _ := json.Marshal(payload)

	for _, m := range members {
		routeKey := fmt.Sprintf("im:ws:route:%d", m.MemberID)
		devices, err := store.RDB.HGetAll(ctx, routeKey).Result()
		if err != nil || len(devices) == 0 {
			continue
		}
		publishedNodes := make(map[string]bool)
		for _, nodeID := range devices {
			if publishedNodes[nodeID] {
				continue
			}
			publishedNodes[nodeID] = true
			envelope, _ := json.Marshal(map[string]interface{}{
				"user_id": m.MemberID,
				"cmd":     cmd,
				"payload": json.RawMessage(payloadData),
			})
			if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", nodeID), string(envelope)).Err(); err != nil {
				logger.L.Warnf("broadcast to session %s node %s failed: %v", sessionID, nodeID, err)
			}
		}
	}
}

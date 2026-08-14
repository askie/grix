package dispatch

import (
	"encoding/json"
	"fmt"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// PublishAIRequest publishes an AI request to NATS for the LLM service.
func PublishAIRequest(sessionID string, data interface{}) {
	payload, err := json.Marshal(data)
	if err != nil {
		logger.L.Errorf("marshal ai request error: %v", err)
		return
	}
	subject := fmt.Sprintf("ai.request.%s", sessionID)
	if _, err := store.JS.Publish(subject, payload); err != nil {
		logger.L.Errorf("nats publish error: %v", err)
	}
}

// PublishOfflinePush publishes an offline push task to NATS.
func PublishOfflinePush(userID int64, data interface{}) {
	payload, err := json.Marshal(data)
	if err != nil {
		logger.L.Errorf("marshal push data error: %v", err)
		return
	}
	subject := fmt.Sprintf("im.push.offline.%d", userID)
	if _, err := store.JS.Publish(subject, payload); err != nil {
		logger.L.Errorf("nats publish offline push error: %v", err)
	}
}

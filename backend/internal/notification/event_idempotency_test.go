package notification

import (
	"encoding/json"
	"testing"
)

func TestAgentNotificationEventSerializesSinkIdempotencyKey(t *testing.T) {
	const key = "agent-terminal:event-1:completed:task_completed"
	raw, err := json.Marshal(AgentNotificationEvent{
		EventKey:      EventTaskCompleted,
		UserID:        1,
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	var decoded AgentNotificationEvent
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if decoded.IdempotencyKey != key {
		t.Fatalf("idempotency key=%q want=%q", decoded.IdempotencyKey, key)
	}
}

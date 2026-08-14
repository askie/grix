package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSendAckPayload_OmitsMsgIDWhenUnknown(t *testing.T) {
	raw, err := json.Marshal(SendAckPayload{
		SessionID:   "sess-1",
		ClientMsgID: "client-1",
		CreatedAt:   1704067200000,
	})
	if err != nil {
		t.Fatalf("marshal SendAckPayload: %v", err)
	}
	if strings.Contains(string(raw), "\"msg_id\"") {
		t.Fatalf("expected msg_id to be omitted when unknown, got=%s", string(raw))
	}
}

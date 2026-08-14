package agentmsg

import (
	"context"
	"testing"
)

func TestBroadcastToSessionQueriesMembersAndDedupesNodes(t *testing.T) {
	cleanup := setupAgentMsgTest(t)
	defer cleanup()

	const sessionID = "sess-agentmsg-broadcast-1"
	mustCreateSessionWithHumanMembers(t, sessionID, 7001, []int64{7101, 7102, 7103})

	seedRoute(t, 7101, map[string]string{
		"dev-a": "node-a",
		"dev-b": "node-a",
	})
	seedRoute(t, 7102, map[string]string{
		"dev-a": "node-a",
		"dev-b": "node-b",
		"dev-c": "node-b",
	})

	subA := subscribeChannel(t, "chan:node-a")
	defer subA.Close()
	subB := subscribeChannel(t, "chan:node-b")
	defer subB.Close()

	payload := map[string]any{"text": "hello"}
	BroadcastToSession(context.Background(), sessionID, "stream_chunk", payload)

	msgA1 := readEnvelopeMessage(t, subA)
	msgA2 := readEnvelopeMessage(t, subA)
	msgB := readEnvelopeMessage(t, subB)

	assertBroadcastEnvelope(t, msgA1, "stream_chunk", payload, 7101, 7102)
	assertBroadcastEnvelope(t, msgA2, "stream_chunk", payload, 7101, 7102)
	assertBroadcastEnvelope(t, msgB, "stream_chunk", payload, 7102)
}

func assertBroadcastEnvelope(t *testing.T, envelope map[string]any, cmd string, payload map[string]any, allowedUsers ...int64) {
	t.Helper()

	if envelope["cmd"] != cmd {
		t.Fatalf("cmd=%v want=%s", envelope["cmd"], cmd)
	}

	userID, ok := envelope["user_id"].(float64)
	if !ok {
		t.Fatalf("user_id type mismatch: %T", envelope["user_id"])
	}
	found := false
	for _, allowed := range allowedUsers {
		if int64(userID) == allowed {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("user_id=%d not in allowed set %v", int64(userID), allowedUsers)
	}

	rawPayload, ok := envelope["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type mismatch: %T", envelope["payload"])
	}
	if rawPayload["text"] != payload["text"] {
		t.Fatalf("payload mismatch got=%v want=%v", rawPayload["text"], payload["text"])
	}
}

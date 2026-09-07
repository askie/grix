package agentapi

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

// event_result 的 error_surfaced 必须原样带进投递状态：server 层据此决定
// 还要不要再写一条「智能体处理失败：…」会话消息。
func TestDeliveryStatusForEventResultCarriesErrorSurfaced(t *testing.T) {
	event := DelegateEventPayload{
		EventID:   "evt-1",
		SessionID: "sess-1",
		OwnerID:   101,
		AgentID:   201,
	}
	const reason = "You're out of usage credits. Run /usage-credits to keep using Fable 5.1 or /model to switch models."

	surfaced := deliveryStatusForEventResult(event, 0, EventResultPayload{
		EventID:       "evt-1",
		Status:        protocol.AgentEventResultFailed,
		Msg:           reason,
		ErrorSurfaced: true,
	})
	if surfaced.Status != protocol.AgentDeliveryStatusFailed {
		t.Fatalf("status=%q want failed", surfaced.Status)
	}
	if !surfaced.ErrorSurfaced {
		t.Fatalf("error_surfaced should propagate to the delivery status")
	}
	// 排障信息不能丢：reason 照常回传。
	if surfaced.Msg != reason {
		t.Fatalf("msg=%q want the connector reason verbatim", surfaced.Msg)
	}

	legacy := deliveryStatusForEventResult(event, 0, EventResultPayload{
		EventID: "evt-1",
		Status:  protocol.AgentEventResultFailed,
		Msg:     reason,
	})
	if legacy.ErrorSurfaced {
		t.Fatalf("event_result without error_surfaced should stay false")
	}
	if legacy.Msg != reason {
		t.Fatalf("msg=%q want the connector reason verbatim", legacy.Msg)
	}
}

// 老连接器发来的 event_result 不带该字段；新连接器带上时要能解出来。
func TestEventResultPayloadDecodesErrorSurfaced(t *testing.T) {
	var legacy EventResultPayload
	if err := json.Unmarshal([]byte(`{"event_id":"e","status":"failed","msg":"boom"}`), &legacy); err != nil {
		t.Fatalf("unmarshal legacy payload error: %v", err)
	}
	if legacy.ErrorSurfaced {
		t.Fatalf("legacy payload should decode error_surfaced=false")
	}

	var modern EventResultPayload
	if err := json.Unmarshal(
		[]byte(`{"event_id":"e","status":"failed","msg":"boom","error_surfaced":true}`), &modern,
	); err != nil {
		t.Fatalf("unmarshal payload error: %v", err)
	}
	if !modern.ErrorSurfaced {
		t.Fatalf("payload with error_surfaced=true should decode as true")
	}
}

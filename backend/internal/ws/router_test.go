package ws

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestHandleAppStateSet(t *testing.T) {
	ensureConnTestLogger()
	conn := NewConn(nil)
	conn.SetAuth(1001, "session-1", "device-1", "ios")

	payload := protocol.AppStateSetPayload{AppState: "background"}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	handleAppStateSet(conn, &protocol.Packet{
		Cmd:     protocol.CmdAppStateSet,
		Seq:     1,
		Payload: raw,
	})

	if got := conn.appStateString(); got != "background" {
		t.Fatalf("app state=%s want=background", got)
	}
}

func TestHandleAppStateSetInvalid(t *testing.T) {
	ensureConnTestLogger()
	conn := NewConn(nil)
	conn.SetAuth(1001, "session-1", "device-1", "ios")

	payload := protocol.AppStateSetPayload{AppState: "invalid"}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	handleAppStateSet(conn, &protocol.Packet{
		Cmd:     protocol.CmdAppStateSet,
		Seq:     2,
		Payload: raw,
	})

	if got := conn.appStateString(); got != "foreground" {
		t.Fatalf("app state=%s want=foreground", got)
	}
}

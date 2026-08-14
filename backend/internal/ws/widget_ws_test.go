package ws

import (
	"testing"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestIsWidgetAllowedCmd(t *testing.T) {
	allowed := []string{protocol.CmdPing, protocol.CmdPushAck, protocol.CmdSendMsg, protocol.CmdPullSync}
	for _, cmd := range allowed {
		if !isWidgetAllowedCmd(cmd) {
			t.Fatalf("cmd %s should be allowed", cmd)
		}
	}
	if isWidgetAllowedCmd(protocol.CmdSessionRead) {
		t.Fatalf("cmd %s should not be allowed", protocol.CmdSessionRead)
	}
}

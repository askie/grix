package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
)

func TestSanitizeCallClientDiagField(t *testing.T) {
	assert.Equal(t, "a b c", sanitizeCallClientDiagField("a\nb\rc"))
	assert.Len(t, sanitizeCallClientDiagField(strings.Repeat("x", 600)), maxCallClientDiagFieldLen)
}

func TestHandleCallClientDiag_InvalidPayload(t *testing.T) {
	conn := &callHandlerMockConn{userID: 1}
	HandleCallClientDiag(newCallHandlerMockHub(), conn, &protocol.Packet{Payload: json.RawMessage(`{`)})
	assert.Empty(t, conn.sent)
}

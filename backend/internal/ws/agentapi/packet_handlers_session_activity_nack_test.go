package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func readSendNack(t *testing.T, conn *agentConn) (protocol.Packet, SendNackPayload) {
	t.Helper()
	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("decode packet: %v", err)
		}
		var nack SendNackPayload
		if err := json.Unmarshal(pkt.Payload, &nack); err != nil {
			t.Fatalf("decode nack payload: %v", err)
		}
		return pkt, nack
	default:
		t.Fatal("expected a send_nack packet")
	}
	return protocol.Packet{}, SendNackPayload{}
}

func TestHandleSessionActivitySet_NackMapsPolicyRejections(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
		wantMsg  string
	}{
		{name: "muted member", err: sessionguard.ErrMemberSpeakMuted, wantCode: protocol.CodeUnauthorized, wantMsg: sessionguard.ErrMemberSpeakMuted.Error()},
		{name: "group muted", err: sessionguard.ErrGroupAllMembersMuted, wantCode: protocol.CodeUnauthorized, wantMsg: sessionguard.ErrGroupAllMembersMuted.Error()},
		{name: "not a member", err: agentmsg.ErrPermissionDenied, wantCode: protocol.CodeUnauthorized, wantMsg: "session not writable by agent"},
		{name: "session gone", err: gorm.ErrRecordNotFound, wantCode: protocol.CodeUnauthorized, wantMsg: "session unavailable"},
		{name: "explicit send error", err: &SendError{Code: 4029, Msg: "slow down"}, wantCode: 4029, wantMsg: "slow down"},
		{name: "internal error", err: errors.New("redis down"), wantCode: protocol.CodeServerInternal, wantMsg: "session activity update failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
			defer mgr.Shutdown()
			mgr.activityFn = func(_ context.Context, _ int64, _ int64, _ protocol.SessionActivitySetPayload) error {
				return tc.err
			}
			conn := &agentConn{agentID: 100, ownerID: 200, clientID: "test", send: make(chan []byte, 64)}
			pkt := makePacket(t, protocol.CmdSessionActivitySet, 11, protocol.SessionActivitySetPayload{
				SessionID: "sess-nack",
				Kind:      protocol.SessionActivityKindComposing,
				Active:    true,
			})

			mgr.handleSessionActivitySet(conn, pkt)

			got, nack := readSendNack(t, conn)
			if got.Cmd != "send_nack" || got.Seq != 11 {
				t.Fatalf("packet cmd=%q seq=%d want send_nack/11", got.Cmd, got.Seq)
			}
			if nack.Code != tc.wantCode || nack.Msg != tc.wantMsg {
				t.Fatalf("nack code=%d msg=%q want %d/%q", nack.Code, nack.Msg, tc.wantCode, tc.wantMsg)
			}
			if nack.Cmd != protocol.CmdSessionActivitySet || nack.SessionID != "sess-nack" {
				t.Fatalf("nack must echo cmd/session_id, got cmd=%q session=%q", nack.Cmd, nack.SessionID)
			}
		})
	}
}

func TestHandleSessionActivitySet_InvalidPayloadNackEchoesCmd(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.activityFn = func(_ context.Context, _ int64, _ int64, _ protocol.SessionActivitySetPayload) error {
		t.Fatal("handler must not run for an unsupported kind")
		return nil
	}
	conn := &agentConn{agentID: 100, ownerID: 200, clientID: "test", send: make(chan []byte, 64)}
	pkt := makePacket(t, protocol.CmdSessionActivitySet, 12, protocol.SessionActivitySetPayload{
		SessionID: "sess-kind",
		Kind:      "dancing",
		Active:    true,
	})

	mgr.handleSessionActivitySet(conn, pkt)

	_, nack := readSendNack(t, conn)
	if nack.Code != protocol.CodeInvalidPayload || nack.Cmd != protocol.CmdSessionActivitySet || nack.SessionID != "sess-kind" {
		t.Fatalf("unexpected nack %+v", nack)
	}
}

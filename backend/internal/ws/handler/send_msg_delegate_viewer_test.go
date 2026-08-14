package handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// 私聊投递降级：会话内无人查看时下发 single_message（整段回复），有人查看时保持流式（不注入）。
func TestSendMsgDelegatePrivateViewerGatesResponseDelivery(t *testing.T) {
	cases := []struct {
		name       string
		sessionID  string
		seedViewer bool
		wantSingle bool
	}{
		{name: "no_viewer_single_message", sessionID: "session-delegate-no-viewer", seedViewer: false, wantSingle: true},
		{name: "viewer_present_stream", sessionID: "session-delegate-with-viewer", seedViewer: true, wantSingle: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupSendMsgTest(t)
			defer cleanup()

			const (
				ownerID  = int64(8801)
				senderID = int64(8802)
				agentID  = int64(9971)
			)
			prevManager := wsagentapi.GetGlobal()
			wsagentapi.SetGlobal(nil)
			defer wsagentapi.SetGlobal(prevManager)

			if err := store.DB.Create(&model.Agent{
				ID:           agentID,
				AgentName:    "viewer-gate-agent",
				OwnerID:      ownerID,
				ProviderType: model.AgentProviderAPI,
				Status:       1,
			}).Error; err != nil {
				t.Fatalf("create agent error: %v", err)
			}
			if err := store.DB.Create(&model.Session{
				SessionID:   tc.sessionID,
				OwnerID:     ownerID,
				SessionType: 1,
			}).Error; err != nil {
				t.Fatalf("create session error: %v", err)
			}
			now := time.Now().UTC()
			for _, m := range []model.SessionMember{
				{SessionID: tc.sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
				{SessionID: tc.sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
			} {
				if err := store.DB.Create(&m).Error; err != nil {
					t.Fatalf("create member error: %v", err)
				}
			}
			seedSendMsgFriendRelation(t, senderID, ownerID)
			seedSendMsgFriendRelation(t, ownerID, senderID)

			ctx := context.Background()
			if err := store.RDB.HSet(ctx, "im:delegate:"+tc.sessionID+":8801",
				"agent_id", "9971",
				"max_consecutive_replies", "3",
			).Err(); err != nil {
				t.Fatalf("seed delegate key error: %v", err)
			}

			senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-device"}
			ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-device"}
			hub := &sendMsgMockHub{
				nodeID: "node-a",
				conns: map[int64][]ConnInterface{
					senderID: {senderConn},
					ownerID:  {ownerConn},
				},
			}

			if tc.seedViewer {
				if err := UpsertSessionActivity(ctx, hub, protocol.SessionActivityPayload{
					SessionID:    tc.sessionID,
					Kind:         protocol.SessionActivityKindViewing,
					ActorID:      senderID,
					ActorType:    protocol.SessionActivityActorTypeHuman,
					ExecutorID:   senderID,
					ExecutorType: protocol.SessionActivityActorTypeHuman,
					Source:       protocol.SessionActivitySourceHumanInput,
				}); err != nil {
					t.Fatalf("upsert viewing activity error: %v", err)
				}
				senderConn.sent = nil
			}

			pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
				SessionID:   tc.sessionID,
				ClientMsgID: "viewer-gate-cmsg",
				MsgType:     1,
				Content:     "trigger agent",
			})
			HandleSendMsg(hub, senderConn, pkt)

			queuedRaw, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:9971", 0, -1).Result()
			if err != nil {
				t.Fatalf("load queued delegate events error: %v", err)
			}
			if len(queuedRaw) != 1 {
				t.Fatalf("queued events count=%d want=1", len(queuedRaw))
			}
			var ev wsagentapi.DelegateEventPayload
			if err := json.Unmarshal([]byte(queuedRaw[0]), &ev); err != nil {
				t.Fatalf("unmarshal queued event error: %v", err)
			}

			mode, ok := connectorResponseDeliveryOf(t, ev.Extra)
			if tc.wantSingle {
				if !ok || mode != connectorResponseDeliverySingle {
					t.Fatalf("want single_message, got mode=%q ok=%v extra=%s", mode, ok, ev.Extra)
				}
			} else if ok {
				t.Fatalf("want stream (no response_delivery), got mode=%q extra=%s", mode, ev.Extra)
			}
		})
	}
}

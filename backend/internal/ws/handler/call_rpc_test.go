package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

func startCallRPCDispatchLoop(t *testing.T, nodeID string) func() {
	t.Helper()
	sub := store.RDB.Subscribe(context.Background(), fmt.Sprintf("chan:%s", nodeID))
	_, _ = sub.Receive(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for msg := range sub.Channel() {
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err == nil {
				HandleInternalRedisDispatch(envelope.Cmd, envelope.Payload)
			}
		}
	}()
	return func() {
		_ = sub.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}
}

func setupCallRPCHandlerTest(t *testing.T) (*mockBridgeManager, func()) {
	t.Helper()
	cleanupRedis := setupSendMsgTest(t)
	bridge, cleanupCall := setupCallAIHandlerTest(t)
	return bridge, func() {
		cleanupCall()
		cleanupRedis()
	}
}

func inviteCallOnOwnerNode(t *testing.T, hub *callHandlerMockHub, callerID, calleeID int64) (string, *callHandlerMockConn) {
	t.Helper()
	callerConn := &callHandlerMockConn{userID: callerID}
	calleeConn := &callHandlerMockConn{userID: calleeID}
	hub.addConn(calleeID, calleeConn)

	payload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: fmt.Sprintf("%d", calleeID), PeerType: "user", CallMode: 1})
	HandleCallInvite(hub, callerConn, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: payload})

	ring, ok := calleeConn.findCmd(protocol.CmdCallRing)
	require.True(t, ok)
	var ringPayload protocol.CallRingPayload
	data, _ := json.Marshal(ring.payload)
	require.NoError(t, json.Unmarshal(data, &ringPayload))
	return ringPayload.CallID, calleeConn
}

func TestCallRPC_RemoteAnswerRoutesToOwnerNode(t *testing.T) {
	_, cleanup := setupCallRPCHandlerTest(t)
	defer cleanup()
	stopOwner := startCallRPCDispatchLoop(t, "node-owner")
	defer stopOwner()
	stopOrigin := startCallRPCDispatchLoop(t, "node-origin")
	defer stopOrigin()

	ownerHub := newCallHandlerMockHub()
	ownerHub.nodeID = "node-owner"
	callID, _ := inviteCallOnOwnerNode(t, ownerHub, 1, 2)

	originHub := newCallHandlerMockHub()
	originHub.nodeID = "node-origin"
	remoteCalleeConn := &callHandlerMockConn{userID: 2}
	payload, _ := json.Marshal(protocol.CallAnswerPayload{CallID: callID})
	HandleCallAnswer(originHub, remoteCalleeConn, &protocol.Packet{Cmd: protocol.CmdCallAnswer, Seq: 9, Payload: payload})

	answered, ok := remoteCalleeConn.findCmd(protocol.CmdCallPeerAnswered)
	require.True(t, ok)
	require.Equal(t, int64(9), answered.seq)
	raw, ok := answered.payload.(json.RawMessage)
	require.True(t, ok)
	var got protocol.CallPeerAnsweredPayload
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, callID, got.CallID)
	require.Equal(t, "human", got.Mode)
	require.NotEmpty(t, got.RoomToken)
}

func TestCallRPC_RemoteTakeoverAndHandBackRouteToOwnerNode(t *testing.T) {
	bridge, cleanup := setupCallRPCHandlerTest(t)
	defer cleanup()
	stopOwner := startCallRPCDispatchLoop(t, "node-owner")
	defer stopOwner()
	stopOrigin := startCallRPCDispatchLoop(t, "node-origin")
	defer stopOrigin()

	ownerHub := newCallHandlerMockHub()
	ownerHub.nodeID = "node-owner"
	callID, ownerCalleeConn := inviteCallOnOwnerNode(t, ownerHub, 1, 2)

	aiPayload, _ := json.Marshal(protocol.CallAnswerWithAIPayload{CallID: callID, AgentID: "42"})
	HandleCallAnswerWithAI(ownerHub, ownerCalleeConn, &protocol.Packet{Cmd: protocol.CmdCallAnswerWithAI, Seq: 2, Payload: aiPayload})
	require.Len(t, bridge.started, 1)

	originHub := newCallHandlerMockHub()
	originHub.nodeID = "node-origin"
	remoteCalleeConn := &callHandlerMockConn{userID: 2}

	takeoverPayload, _ := json.Marshal(protocol.CallTakeoverPayload{CallID: callID})
	HandleCallTakeover(originHub, remoteCalleeConn, &protocol.Packet{Cmd: protocol.CmdCallTakeover, Seq: 10, Payload: takeoverPayload})
	takeoverState, ok := remoteCalleeConn.findCmd(protocol.CmdCallState)
	require.True(t, ok)
	var takeoverGot protocol.CallAIStatePayload
	raw, ok := takeoverState.payload.(json.RawMessage)
	require.True(t, ok)
	require.NoError(t, json.Unmarshal(raw, &takeoverGot))
	require.Equal(t, "human_active", takeoverGot.Mode)

	handBackPayload, _ := json.Marshal(protocol.CallHandBackPayload{CallID: callID})
	HandleCallHandBack(originHub, remoteCalleeConn, &protocol.Packet{Cmd: protocol.CmdCallHandBack, Seq: 11, Payload: handBackPayload})
	// 交回=恢复发声（unmute），不重建 session：started 仍 1，muted/unmuted 各 1
	require.Len(t, bridge.started, 1)
	require.Len(t, bridge.muted, 1)
	require.Len(t, bridge.unmuted, 1)
}

func TestCallRPC_RemoteRejectForgetsOwner(t *testing.T) {
	_, cleanup := setupCallRPCHandlerTest(t)
	defer cleanup()
	stopOwner := startCallRPCDispatchLoop(t, "node-owner")
	defer stopOwner()
	stopOrigin := startCallRPCDispatchLoop(t, "node-origin")
	defer stopOrigin()

	ownerHub := newCallHandlerMockHub()
	ownerHub.nodeID = "node-owner"
	callIDRaw, _ := inviteCallOnOwnerNode(t, ownerHub, 1, 2)
	callID, err := parseInt64ForTest(callIDRaw)
	require.NoError(t, err)

	originHub := newCallHandlerMockHub()
	originHub.nodeID = "node-origin"
	remoteCalleeConn := &callHandlerMockConn{userID: 2}
	payload, _ := json.Marshal(protocol.CallRejectPayload{CallID: callIDRaw, Reason: "busy"})
	HandleCallReject(originHub, remoteCalleeConn, &protocol.Packet{Cmd: protocol.CmdCallReject, Seq: 12, Payload: payload})

	_, ok := lookupCallOwner(context.Background(), callID)
	require.False(t, ok)
}

func TestCallBusyGuardBlocksSecondNodeAndReleasesOnEnd(t *testing.T) {
	_, cleanup := setupCallRPCHandlerTest(t)
	defer cleanup()

	ownerHub := newCallHandlerMockHub()
	ownerHub.nodeID = "node-owner"
	callIDRaw, ownerCalleeConn := inviteCallOnOwnerNode(t, ownerHub, 1, 2)
	callID, err := parseInt64ForTest(callIDRaw)
	require.NoError(t, err)

	ownerController := callCtrl
	room := &callHandlerMockRoom{}
	persist := &callHandlerMockPersist{}
	secondNodeController := call.New(room, persist, func(_ int64, _ string, _ any) {})
	secondNodeController.SetCleanupHook(cleanupCallGuards)
	callCtrl = secondNodeController

	secondHub := newCallHandlerMockHub()
	secondHub.nodeID = "node-second"
	secondCallerConn := &callHandlerMockConn{userID: 3}
	payload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	HandleCallInvite(secondHub, secondCallerConn, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 20, Payload: payload})
	_, ok := secondCallerConn.findCmd(protocol.CmdCallBusy)
	require.True(t, ok)

	callCtrl = ownerController
	rejectPayload, _ := json.Marshal(protocol.CallRejectPayload{CallID: callIDRaw, Reason: "busy"})
	HandleCallReject(ownerHub, ownerCalleeConn, &protocol.Packet{Cmd: protocol.CmdCallReject, Seq: 21, Payload: rejectPayload})

	_, ownerOK := lookupCallOwner(context.Background(), callID)
	require.False(t, ownerOK)
	for _, userID := range []int64{1, 2} {
		exists, err := store.RDB.Exists(context.Background(), callBusyKey(userID)).Result()
		require.NoError(t, err)
		require.Equal(t, int64(0), exists)
	}
}

func TestCallRPCRequestReturnsErrorWhenStateInvalid(t *testing.T) {
	_, cleanup := setupCallRPCHandlerTest(t)
	defer cleanup()

	resp := executeCallRPCRequest(callRPCRequest{
		Action: callRPCActionTakeover,
		CallID: 999,
		UserID: 2,
	})
	require.False(t, resp.OK)
	require.Contains(t, resp.Error, "call not found")
}

func parseInt64ForTest(raw string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(raw, "%d", &id)
	return id, err
}

type callRPCPersistWithHandover struct {
	callHandlerMockPersist
	events []model.CallHandoverEvent
}

func (p *callRPCPersistWithHandover) UpdateHandover(_ context.Context, _ int64, event model.CallHandoverEvent, _ int16, _ string) error {
	p.events = append(p.events, event)
	return nil
}

func newCallRPCControllerForTest(bridge *mockBridgeManager) {
	room := &callHandlerMockRoom{}
	persist := &callRPCPersistWithHandover{}
	callCtrl = call.NewWithBridge(room, persist, func(_ int64, _ string, _ any) {}, bridge)
}

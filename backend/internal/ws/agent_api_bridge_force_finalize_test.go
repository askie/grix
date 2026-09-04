package ws

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/agentstream"
	"github.com/askie/grix/backend/internal/ws/protocol"
	redis "github.com/redis/go-redis/v9"
)

const forceFinalizeTestNodeID = "node-force-finalize"

// seedForceFinalizeSession 建一个会话，并只给 observerID 登记在线路由，
// 使 agentmsg 的每次广播在 chan:<node> 上恰好落一条，便于计数。
func seedForceFinalizeSession(t *testing.T, sessionID string, observerID int64, memberIDs ...int64) {
	t.Helper()
	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     memberIDs[0],
		SessionType: 2,
		GroupName:   "force-finalize-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range memberIDs {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	if err := store.RDB.HSet(
		context.Background(),
		"im:ws:route:"+strconv.FormatInt(observerID, 10),
		"device-1",
		forceFinalizeTestNodeID,
	).Err(); err != nil {
		t.Fatalf("seed route error: %v", err)
	}
}

// collectStreamFinishes 读干净订阅通道里已到达的 stream_finish 广播。
func collectStreamFinishes(t *testing.T, sub *redis.PubSub) []protocol.StreamFinishPayload {
	t.Helper()
	var finishes []protocol.StreamFinishPayload
	ch := sub.Channel()
	for {
		select {
		case msg := <-ch:
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("decode broadcast envelope error: %v", err)
			}
			if envelope.Cmd != protocol.CmdStreamFinish {
				continue
			}
			var finish protocol.StreamFinishPayload
			if err := json.Unmarshal(envelope.Payload, &finish); err != nil {
				t.Fatalf("decode stream_finish payload error: %v", err)
			}
			finishes = append(finishes, finish)
		case <-time.After(300 * time.Millisecond):
			return finishes
		}
	}
}

func requireStreamRegistryEmpty(t *testing.T, agentID int64, clientMsgID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		exists, err := store.RDB.HExists(
			context.Background(),
			agentAPIStreamRegistryKey(agentID),
			clientMsgID,
		).Result()
		if err != nil {
			t.Fatalf("check stream registry error: %v", err)
		}
		if !exists {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("stream registry entry %s should be cleaned up", clientMsgID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// 连接器只发了 is_finish=false 的分片就打终态时，强制收尾必须广播一条带累计正文的
// stream_finish，并清掉流状态，客户端才不会一直停在"正在输出"。
func TestHandleForceFinalizeSessionStreams_AbortsUnfinishedStream(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID   = "g_force_finalize_unfinished"
		ownerID     = int64(1001)
		peerID      = int64(2003)
		agentID     = int64(9971)
		clientMsgID = "force-finalize-unfinished-1"
	)
	seedForceFinalizeSession(t, sessionID, peerID, ownerID, peerID)

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9971").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}

	s := &Server{}
	defer s.cleanupRuntime()
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  clientMsgID,
		DeltaContent: "codex failed: partial error text",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("stream chunk error: %v", err)
	}

	sub := store.RDB.Subscribe(ctx, "chan:"+forceFinalizeTestNodeID)
	defer sub.Close()

	s.handleForceFinalizeSessionStreams(ctx, agentID, ownerID, sessionID)

	finishes := collectStreamFinishes(t, sub)
	if len(finishes) != 1 {
		t.Fatalf("stream_finish broadcast count=%d want=1", len(finishes))
	}
	if finishes[0].FinalContent != "codex failed: partial error text" {
		t.Fatalf("final content=%q want accumulated body", finishes[0].FinalContent)
	}
	if finishes[0].SessionID != sessionID || !finishes[0].IsFinish {
		t.Fatalf("unexpected stream_finish payload: %+v", finishes[0])
	}
	requireStreamRegistryEmpty(t, agentID, clientMsgID)
}

// 已经收到 is_finish=true 收尾块的流由宽限收尾器负责，强制收尾必须让位，
// 全程只能有一条 stream_finish。
func TestHandleForceFinalizeSessionStreams_SkipsAlreadyFinishedStream(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID   = "g_force_finalize_finished"
		ownerID     = int64(1001)
		peerID      = int64(2003)
		agentID     = int64(9972)
		clientMsgID = "force-finalize-finished-1"
	)
	seedForceFinalizeSession(t, sessionID, peerID, ownerID, peerID)

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9972").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}

	s := &Server{}
	defer s.cleanupRuntime()
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  clientMsgID,
		DeltaContent: "answer body",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("first stream chunk error: %v", err)
	}

	sub := store.RDB.Subscribe(ctx, "chan:"+forceFinalizeTestNodeID)
	defer sub.Close()

	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: clientMsgID,
		ChunkSeq:    2,
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}

	// 终态紧跟收尾块到达，此时宽限收尾器还在排队。
	s.handleForceFinalizeSessionStreams(ctx, agentID, ownerID, sessionID)
	requireStreamRegistryEmpty(t, agentID, clientMsgID)

	finishes := collectStreamFinishes(t, sub)
	if len(finishes) != 1 {
		t.Fatalf("stream_finish broadcast count=%d want=1", len(finishes))
	}
	if finishes[0].FinalContent != "answer body" {
		t.Fatalf("final content=%q want %q", finishes[0].FinalContent, "answer body")
	}
	// 停止围栏只有正常宽限收尾器会写：强制收尾若抢跑，这条流就会走 Abort 分支，
	// 围栏缺失即说明让位逻辑失效。
	if !agentstream.HasStoppedFence(ctx, agentID, clientMsgID) {
		t.Fatal("finished stream should be closed by the grace finalizer, not by force finalize")
	}
}

// 会话里没有未收尾的流时，强制收尾零副作用。
func TestHandleForceFinalizeSessionStreams_NoOpWithoutOpenStreams(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "g_force_finalize_empty"
		ownerID   = int64(1001)
		peerID    = int64(2003)
		agentID   = int64(9973)
	)
	seedForceFinalizeSession(t, sessionID, peerID, ownerID, peerID)

	ctx := context.Background()
	sub := store.RDB.Subscribe(ctx, "chan:"+forceFinalizeTestNodeID)
	defer sub.Close()

	s := &Server{}
	defer s.cleanupRuntime()
	s.handleForceFinalizeSessionStreams(ctx, agentID, ownerID, sessionID)

	if finishes := collectStreamFinishes(t, sub); len(finishes) != 0 {
		t.Fatalf("stream_finish broadcast count=%d want=0", len(finishes))
	}
}

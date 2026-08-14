package agentapi

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestDispatchChatStateQueryIncludesOneResultOnlyForExactCompletedSession(t *testing.T) {
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		testDB.Close()
	})

	previousManager := globalManager
	globalManager = nil
	t.Cleanup(func() { globalManager = previousManager })

	const (
		ownerID          = int64(88101)
		agentID          = int64(88201)
		completedSession = "task-result-completed"
		runningSession   = "task-result-running"
		noResultSession  = "task-result-no-text"
	)
	startedAt := time.Now().UTC().Add(-time.Minute)
	completedAt := startedAt.Add(30 * time.Second)
	states := []model.SessionAgentState{
		{
			SessionID: completedSession, OwnerID: ownerID, AgentID: agentID,
			State: model.SessionAgentStateCompleted, LastRunID: "run-completed",
			StartedAt: &startedAt, CompletedAt: &completedAt, UpdatedAt: completedAt,
		},
		{
			SessionID: runningSession, OwnerID: ownerID, AgentID: agentID,
			State: model.SessionAgentStateRunning, LastRunID: "run-running",
			StartedAt: &startedAt, UpdatedAt: completedAt.Add(time.Second),
		},
		{
			SessionID: noResultSession, OwnerID: ownerID, AgentID: agentID,
			State: model.SessionAgentStateCompleted, LastRunID: "run-no-text",
			StartedAt: &startedAt, CompletedAt: &completedAt, UpdatedAt: completedAt.Add(2 * time.Second),
		},
	}
	if err := testDB.DB.Create(&states).Error; err != nil {
		t.Fatalf("seed chat states error: %v", err)
	}
	messages := []model.Message{
		{
			MsgID: 901, SessionID: completedSession, SenderID: agentID, SenderType: 2,
			MsgType: model.MsgTypeText, Content: "最终结果正文", CreatedAt: startedAt.Add(10 * time.Second),
		},
		{
			MsgID: 902, SessionID: completedSession, SenderID: agentID, SenderType: 2,
			MsgType:   model.MsgTypeText,
			Content:   "[Tool](grix://card/tool_execution?d=%7B%7D)",
			CreatedAt: startedAt.Add(20 * time.Second),
		},
		{
			MsgID: 903, SessionID: runningSession, SenderID: agentID, SenderType: 2,
			MsgType: model.MsgTypeText, Content: "尚未完成的文本", CreatedAt: startedAt.Add(10 * time.Second),
		},
	}
	if err := testDB.DB.Create(&messages).Error; err != nil {
		t.Fatalf("seed task messages error: %v", err)
	}

	data, code, msg := dispatchChatStateQuery(ownerID, agentID, map[string]interface{}{
		"session_id": completedSession,
	})
	if code != 0 || msg != "" {
		t.Fatalf("exact completed query code=%d msg=%q", code, msg)
	}
	completed := data.(protocol.AgentTaskQueryRespPayload)
	if len(completed.Tasks) != 1 {
		t.Fatalf("completed task count=%d want=1", len(completed.Tasks))
	}
	result := completed.Tasks[0].FinalResult
	if result == nil || !result.Found || result.MsgID != 901 || result.Content != "最终结果正文" {
		t.Fatalf("final_result=%#v want msg 901", result)
	}

	data, code, msg = dispatchChatStateQuery(ownerID, agentID, map[string]interface{}{
		"session_id": runningSession,
	})
	if code != 0 || msg != "" {
		t.Fatalf("exact running query code=%d msg=%q", code, msg)
	}
	running := data.(protocol.AgentTaskQueryRespPayload)
	if len(running.Tasks) != 1 || running.Tasks[0].FinalResult != nil {
		t.Fatalf("running tasks=%#v should not include final_result", running.Tasks)
	}

	data, code, msg = dispatchChatStateQuery(ownerID, agentID, map[string]interface{}{
		"session_id": noResultSession,
	})
	if code != 0 || msg != "" {
		t.Fatalf("no-result completed query code=%d msg=%q", code, msg)
	}
	noResult := data.(protocol.AgentTaskQueryRespPayload)
	if len(noResult.Tasks) != 1 || noResult.Tasks[0].FinalResult == nil || noResult.Tasks[0].FinalResult.Found {
		t.Fatalf("no-result task=%#v want found=false", noResult.Tasks)
	}

	data, code, msg = dispatchChatStateQuery(ownerID, agentID, map[string]interface{}{})
	if code != 0 || msg != "" {
		t.Fatalf("list query code=%d msg=%q", code, msg)
	}
	list := data.(protocol.AgentTaskQueryRespPayload)
	for _, task := range list.Tasks {
		if task.FinalResult != nil {
			t.Fatalf("list task %s unexpectedly included final_result", task.SessionID)
		}
	}
}

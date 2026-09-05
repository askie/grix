package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type chatStateListResp struct {
	Code int `json:"code"`
	Data struct {
		List []struct {
			SessionID         string `json:"session_id"`
			AgentID           string `json:"agent_id"`
			AgentName         string `json:"agent_name"`
			AgentOnline       bool   `json:"agent_online"`
			AgentProviderType int16  `json:"agent_provider_type"`
			State             string `json:"state"`
			TaskTitle         string `json:"task_title"`
			LastRunID         string `json:"last_run_id"`
			UpdatedAt         int64  `json:"updated_at"`
		} `json:"list"`
	} `json:"data"`
}

// chatStateRouter mounts the handler with a stubbed auth that pins the caller.
func chatStateRouter(callerID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/chat_states/list", func(c *gin.Context) {
		c.Set("user_id", callerID)
		ChatStateList(c)
	})
	return r
}

func setupChatStateTest(t *testing.T) func() {
	t.Helper()
	prevDB, prevRDB := store.DB, store.RDB
	tdb := testutil.NewTestDB()
	rdb := testutil.NewMockRedis()
	store.DB, store.RDB = tdb.DB, rdb
	return func() {
		_ = rdb.Close()
		tdb.Close()
		store.DB, store.RDB = prevDB, prevRDB
	}
}

func seedAgent(t *testing.T, agentID, ownerID int64, name string, providerType int16) {
	t.Helper()
	require.NoError(t, store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    name,
		OwnerID:      ownerID,
		ProviderType: providerType,
		Status:       1,
	}).Error)
}

func seedChatState(t *testing.T, sessionID string, ownerID, agentID int64, state string, ageSeconds int) {
	t.Helper()
	require.NoError(t, store.DB.Create(&model.SessionAgentState{
		SessionID: sessionID,
		OwnerID:   ownerID,
		AgentID:   agentID,
		State:     state,
		TaskTitle: "task " + sessionID,
		LastRunID: "run-" + sessionID,
		UpdatedAt: time.Now().UTC().Add(-time.Duration(ageSeconds) * time.Second),
	}).Error)
}

func getChatStates(t *testing.T, callerID int64, query string) chatStateListResp {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, "/v1/chat_states/list"+query, nil)
	w := httptest.NewRecorder()
	chatStateRouter(callerID).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp chatStateListResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func TestChatStateListReturnsOnlyCallerRows(t *testing.T) {
	defer setupChatStateTest(t)()
	seedAgent(t, 900, 500, "mine", 3)
	seedAgent(t, 901, 501, "theirs", 3)
	seedChatState(t, "sess-mine", 500, 900, model.SessionAgentStateRunning, 10)
	seedChatState(t, "sess-theirs", 501, 901, model.SessionAgentStateRunning, 5)

	resp := getChatStates(t, 500, "")
	require.Len(t, resp.Data.List, 1)
	require.Equal(t, "sess-mine", resp.Data.List[0].SessionID)
	require.Equal(t, "900", resp.Data.List[0].AgentID, "agent_id must be a string; int64 does not survive a JS number")
	require.Equal(t, "mine", resp.Data.List[0].AgentName)
	require.Equal(t, "task sess-mine", resp.Data.List[0].TaskTitle)
	require.Equal(t, "run-sess-mine", resp.Data.List[0].LastRunID)
	require.False(t, resp.Data.List[0].AgentOnline, "no connector presence was reported")
}

func TestChatStateListWaitingFilter(t *testing.T) {
	defer setupChatStateTest(t)()
	seedAgent(t, 900, 500, "mine", 3)
	seedChatState(t, "sess-running", 500, 900, model.SessionAgentStateRunning, 40)
	seedChatState(t, "sess-approval", 500, 900, model.SessionAgentStateWaitingApproval, 30)
	seedChatState(t, "sess-question", 500, 900, model.SessionAgentStateWaitingQuestion, 20)
	seedChatState(t, "sess-done", 500, 900, model.SessionAgentStateCompleted, 10)

	all := getChatStates(t, 500, "")
	require.Len(t, all.Data.List, 4)
	require.Equal(t, "sess-done", all.Data.List[0].SessionID, "newest first")

	waiting := getChatStates(t, 500, "?state=waiting")
	require.Len(t, waiting.Data.List, 2)
	got := map[string]string{}
	for _, item := range waiting.Data.List {
		got[item.SessionID] = item.State
	}
	require.Equal(t, map[string]string{
		"sess-approval": model.SessionAgentStateWaitingApproval,
		"sess-question": model.SessionAgentStateWaitingQuestion,
	}, got)
}

func TestChatStateListRejectsUnknownStateFilter(t *testing.T) {
	defer setupChatStateTest(t)()
	req, _ := http.NewRequest(http.MethodGet, "/v1/chat_states/list?state=running", nil)
	w := httptest.NewRecorder()
	chatStateRouter(500).ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// A row whose agent was deleted cannot be acted on, so it is not listed.
func TestChatStateListSkipsDeletedAgents(t *testing.T) {
	defer setupChatStateTest(t)()
	seedAgent(t, 900, 500, "alive", 3)
	require.NoError(t, store.DB.Create(&model.Agent{
		ID: 902, AgentName: "gone", OwnerID: 500, ProviderType: 3, Status: 3,
	}).Error)
	seedChatState(t, "sess-alive", 500, 900, model.SessionAgentStateRunning, 10)
	seedChatState(t, "sess-gone", 500, 902, model.SessionAgentStateRunning, 5)

	resp := getChatStates(t, 500, "")
	require.Len(t, resp.Data.List, 1)
	require.Equal(t, "sess-alive", resp.Data.List[0].SessionID)
}

// Remote-model agents never report presence. The client needs provider_type to
// tell "offline" from "does not have a connector at all".
func TestChatStateListReportsProviderType(t *testing.T) {
	defer setupChatStateTest(t)()
	seedAgent(t, 903, 500, "remote", 1)
	seedChatState(t, "sess-remote", 500, 903, model.SessionAgentStateIdle, 1)

	resp := getChatStates(t, 500, "")
	require.Len(t, resp.Data.List, 1)
	require.Equal(t, int16(1), resp.Data.List[0].AgentProviderType)
	require.False(t, resp.Data.List[0].AgentOnline)
}

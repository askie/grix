package agentapi

import (
	"strconv"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/store"
)

func TestDispatchAgentIntroductionUpdate(t *testing.T) {
	const (
		ownerID  = int64(43101)
		actorID  = int64(43102)
		targetID = int64(43103)
	)

	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	seedAgentInvokeDispatchActor(t, testDB, ownerID, actorID, "ak_intro_update")
	seedAgentInvokeDispatchScope(t, actorID, agentscope.ScopeAgentIntroUpdate)

	if err := store.DB.Create(&model.Agent{
		ID:           targetID,
		AgentName:    "target_intro_agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed target agent: %v", err)
	}

	t.Run("updates introduction of owner's agent", func(t *testing.T) {
		_, code, msg := dispatchAgentInvoke(actorID, ownerID, "agent_introduction_update", map[string]interface{}{
			"agent_id":     strconv.FormatInt(targetID, 10),
			"introduction": "updated by tool",
		})
		if code != 0 {
			t.Fatalf("code=%d msg=%q", code, msg)
		}
		var got model.Agent
		if err := store.DB.First(&got, targetID).Error; err != nil {
			t.Fatalf("reload target: %v", err)
		}
		if got.Introduction != "updated by tool" {
			t.Fatalf("introduction=%q want %q", got.Introduction, "updated by tool")
		}
	})

	t.Run("updates agent_name of owner's agent", func(t *testing.T) {
		data, code, msg := dispatchAgentInvoke(actorID, ownerID, "agent_introduction_update", map[string]interface{}{
			"agent_id":   strconv.FormatInt(targetID, 10),
			"agent_name": "renamed_by_tool",
		})
		if code != 0 {
			t.Fatalf("code=%d msg=%q", code, msg)
		}
		var got model.Agent
		if err := store.DB.First(&got, targetID).Error; err != nil {
			t.Fatalf("reload target: %v", err)
		}
		if got.AgentName != "renamed_by_tool" {
			t.Fatalf("agent_name=%q want %q", got.AgentName, "renamed_by_tool")
		}
		// 只改名不应动简介
		if got.Introduction != "updated by tool" {
			t.Fatalf("introduction=%q want unchanged %q", got.Introduction, "updated by tool")
		}
		resp, ok := data.(map[string]interface{})
		if !ok || resp["agent_name"] != "renamed_by_tool" {
			t.Fatalf("resp=%v want agent_name echoed", data)
		}
	})

	t.Run("updates name and introduction together", func(t *testing.T) {
		_, code, msg := dispatchAgentInvoke(actorID, ownerID, "agent_introduction_update", map[string]interface{}{
			"agent_id":     strconv.FormatInt(targetID, 10),
			"agent_name":   "renamed_again",
			"introduction": "intro v2",
		})
		if code != 0 {
			t.Fatalf("code=%d msg=%q", code, msg)
		}
		var got model.Agent
		if err := store.DB.First(&got, targetID).Error; err != nil {
			t.Fatalf("reload target: %v", err)
		}
		if got.AgentName != "renamed_again" || got.Introduction != "intro v2" {
			t.Fatalf("agent=%q/%q want renamed_again/intro v2", got.AgentName, got.Introduction)
		}
	})

	t.Run("rejects duplicate agent_name under same owner", func(t *testing.T) {
		const siblingAgent = int64(43104)
		if err := store.DB.Create(&model.Agent{
			ID:           siblingAgent,
			AgentName:    "sibling_agent",
			OwnerID:      ownerID,
			ProviderType: model.AgentProviderAPI,
			Status:       model.AgentStatusActive,
		}).Error; err != nil {
			t.Fatalf("seed sibling agent: %v", err)
		}
		_, code, _ := dispatchAgentInvoke(actorID, ownerID, "agent_introduction_update", map[string]interface{}{
			"agent_id":   strconv.FormatInt(targetID, 10),
			"agent_name": "sibling_agent",
		})
		if code == 0 {
			t.Fatalf("expected non-zero code for duplicate name")
		}
	})

	t.Run("rejects missing agent_id", func(t *testing.T) {
		_, code, _ := dispatchAgentInvoke(actorID, ownerID, "agent_introduction_update", map[string]interface{}{
			"introduction": "x",
		})
		if code != 4001 {
			t.Fatalf("code=%d want 4001", code)
		}
	})

	t.Run("rejects when neither agent_name nor introduction provided", func(t *testing.T) {
		_, code, _ := dispatchAgentInvoke(actorID, ownerID, "agent_introduction_update", map[string]interface{}{
			"agent_id": strconv.FormatInt(targetID, 10),
		})
		if code != 4001 {
			t.Fatalf("code=%d want 4001", code)
		}
	})

	t.Run("rejects agent owned by another owner", func(t *testing.T) {
		const foreignAgent = int64(43150)
		if err := store.DB.Create(&model.Agent{
			ID:           foreignAgent,
			AgentName:    "foreign_agent",
			OwnerID:      ownerID + 999,
			ProviderType: model.AgentProviderAPI,
			Status:       model.AgentStatusActive,
		}).Error; err != nil {
			t.Fatalf("seed foreign agent: %v", err)
		}
		_, code, _ := dispatchAgentInvoke(actorID, ownerID, "agent_introduction_update", map[string]interface{}{
			"agent_id":     strconv.FormatInt(foreignAgent, 10),
			"introduction": "hack",
		})
		if code == 0 {
			t.Fatalf("expected non-zero code for foreign agent")
		}
	})
}

func TestDispatchCallOwner(t *testing.T) {
	const (
		ownerID = int64(43201)
		actorID = int64(43202)
		voiceID = int64(43203)
	)

	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	seedAgentInvokeDispatchActor(t, testDB, ownerID, actorID, "ak_call_owner")
	seedAgentInvokeDispatchScope(t, actorID, agentscope.ScopeOwnerCall)

	var captured []SendMessageReq
	hooks := agentInvokeHooks{
		sendMessage: func(req SendMessageReq) (*SendMessageResult, error) {
			captured = append(captured, req)
			return &SendMessageResult{MsgID: 9001}, nil
		},
	}

	t.Run("rejects missing session_id", func(t *testing.T) {
		_, code, _ := dispatchAgentInvokeWithHooks(actorID, ownerID, "call_owner", map[string]interface{}{}, hooks)
		if code != 4001 {
			t.Fatalf("code=%d want 4001", code)
		}
	})

	t.Run("rejects when owner has no voice brain configured", func(t *testing.T) {
		_, code, _ := dispatchAgentInvokeWithHooks(actorID, ownerID, "call_owner", map[string]interface{}{
			"session_id": "sess-no-brain",
		}, hooks)
		if code != 4002 {
			t.Fatalf("code=%d want 4002", code)
		}
	})

	// Configure the owner's voice brain so calls can go through.
	vid := voiceID
	if err := store.DB.Create(&model.UserSetting{UserID: ownerID, VoiceBrainAgentID: &vid}).Error; err != nil {
		t.Fatalf("seed voice brain setting: %v", err)
	}

	t.Run("sends call card then enforces cooldown", func(t *testing.T) {
		_, code, msg := dispatchAgentInvokeWithHooks(actorID, ownerID, "call_owner", map[string]interface{}{
			"session_id": "sess-call",
		}, hooks)
		if code != 0 {
			t.Fatalf("code=%d msg=%q", code, msg)
		}
		if len(captured) == 0 {
			t.Fatalf("expected a card message to be sent")
		}
		last := captured[len(captured)-1]
		if !strings.Contains(last.Content, "grix://card/call_owner") {
			t.Fatalf("card content missing marker: %q", last.Content)
		}
		if last.SessionID != "sess-call" || last.AgentID != actorID {
			t.Fatalf("unexpected send req: %+v", last)
		}

		// Immediate second call to the same session is rate-limited.
		_, code2, _ := dispatchAgentInvokeWithHooks(actorID, ownerID, "call_owner", map[string]interface{}{
			"session_id": "sess-call",
		}, hooks)
		if code2 != 4290 {
			t.Fatalf("code=%d want 4290 (cooldown)", code2)
		}
	})
}

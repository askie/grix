package agentapi

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func seedDispatchTargetAgent(t *testing.T, ownerID, agentID int64, clientType string) {
	t.Helper()
	if err := store.DB.Create(&model.Agent{
		ID:              agentID,
		AgentName:       "target_" + strconv.FormatInt(agentID, 10),
		OwnerID:         ownerID,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: clientType,
		Status:          model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed target agent: %v", err)
	}
}

func TestDispatchSessionSend(t *testing.T) {
	const (
		ownerID = int64(44101)
		agentID = int64(44102)
	)
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	seedAgentInvokeDispatchActor(t, testDB, ownerID, agentID, "ak_session_send")
	seedAgentInvokeDispatchScope(t, agentID, agentscope.ScopeSessionSend)

	const sid = "sess-owner-member"
	if err := store.DB.Create(&model.SessionMember{SessionID: sid, MemberID: ownerID, MemberType: 1}).Error; err != nil {
		t.Fatalf("seed member: %v", err)
	}

	var captured []SendMessageReq
	hooks := agentInvokeHooks{
		sendMessage: func(req SendMessageReq) (*SendMessageResult, error) {
			captured = append(captured, req)
			return &SendMessageResult{MsgID: 7001}, nil
		},
	}

	t.Run("rejects missing content", func(t *testing.T) {
		_, code, _ := dispatchAgentInvokeWithHooks(agentID, ownerID, "session_send", map[string]interface{}{
			"session_id": sid,
		}, hooks)
		if code != 4001 {
			t.Fatalf("code=%d want 4001", code)
		}
	})

	t.Run("rejects when owner not a member", func(t *testing.T) {
		_, code, _ := dispatchAgentInvokeWithHooks(agentID, ownerID, "session_send", map[string]interface{}{
			"session_id": "sess-not-member",
			"content":    "hi",
		}, hooks)
		if code != 4003 {
			t.Fatalf("code=%d want 4003", code)
		}
	})

	t.Run("sends as owner", func(t *testing.T) {
		_, code, msg := dispatchAgentInvokeWithHooks(agentID, ownerID, "session_send", map[string]interface{}{
			"session_id": sid,
			"content":    "hello from owner",
		}, hooks)
		if code != 0 {
			t.Fatalf("code=%d msg=%q", code, msg)
		}
		if len(captured) == 0 {
			t.Fatalf("no message captured")
		}
		last := captured[len(captured)-1]
		if last.IdentityMode != agentmsg.ModeCaller || last.CallerID != ownerID {
			t.Fatalf("identity not owner: mode=%q caller=%d", last.IdentityMode, last.CallerID)
		}
		if last.SessionID != sid || last.Content != "hello from owner" {
			t.Fatalf("unexpected send: %+v", last)
		}
		if last.QuotedMessageID != 0 {
			t.Fatalf("quoted_message_id=%d want 0", last.QuotedMessageID)
		}
	})

	t.Run("forwards quoted_message_id when target exists in session", func(t *testing.T) {
		const quoteID = int64(7001001)
		if err := store.DB.Create(&model.Message{
			MsgID:      quoteID,
			SessionID:  sid,
			SenderID:   ownerID,
			SenderType: 1,
			MsgType:    1,
			Content:    "anchor",
		}).Error; err != nil {
			t.Fatalf("seed quoted message: %v", err)
		}
		before := len(captured)
		_, code, msg := dispatchAgentInvokeWithHooks(agentID, ownerID, "session_send", map[string]interface{}{
			"session_id":        sid,
			"content":           "callback",
			"quoted_message_id": quoteID,
		}, hooks)
		if code != 0 {
			t.Fatalf("code=%d msg=%q", code, msg)
		}
		if len(captured) != before+1 {
			t.Fatalf("captured=%d want %d", len(captured), before+1)
		}
		last := captured[len(captured)-1]
		if last.QuotedMessageID != quoteID {
			t.Fatalf("quoted_message_id=%d want %d", last.QuotedMessageID, quoteID)
		}
	})

	t.Run("rejects quoted_message_id outside session", func(t *testing.T) {
		before := len(captured)
		_, code, _ := dispatchAgentInvokeWithHooks(agentID, ownerID, "session_send", map[string]interface{}{
			"session_id":        sid,
			"content":           "callback",
			"quoted_message_id": int64(999999001),
		}, hooks)
		if code != 4001 {
			t.Fatalf("code=%d want 4001", code)
		}
		if len(captured) != before {
			t.Fatalf("should not send when quote missing, captured grew %d->%d", before, len(captured))
		}
	})

	t.Run("rejects provided invalid quoted_message_id without sending", func(t *testing.T) {
		for _, invalid := range []interface{}{"", "abc", "0", "1.5", float64(1.5), int64(0), int64(-1)} {
			before := len(captured)
			_, code, msg := dispatchAgentInvokeWithHooks(agentID, ownerID, "session_send", map[string]interface{}{
				"session_id":        sid,
				"content":           "callback",
				"quoted_message_id": invalid,
			}, hooks)
			if code != 4001 || msg != "quoted_message_id invalid" {
				t.Fatalf("quoted_message_id=%#v: code=%d msg=%q want 4001 invalid", invalid, code, msg)
			}
			if len(captured) != before {
				t.Fatalf("quoted_message_id=%#v should not send, captured grew %d->%d", invalid, before, len(captured))
			}
		}
	})

	t.Run("sends as owner even when agent is a member of the target session", func(t *testing.T) {
		const ownSid = "sess-agent-own"
		if err := store.DB.Create(&model.SessionMember{SessionID: ownSid, MemberID: ownerID, MemberType: 1}).Error; err != nil {
			t.Fatalf("seed owner member: %v", err)
		}
		if err := store.DB.Create(&model.SessionMember{SessionID: ownSid, MemberID: agentID, MemberType: 2}).Error; err != nil {
			t.Fatalf("seed agent member: %v", err)
		}
		before := len(captured)
		_, code, msg := dispatchAgentInvokeWithHooks(agentID, ownerID, "session_send", map[string]interface{}{
			"session_id": ownSid,
			"content":    "dispatch callback as owner",
		}, hooks)
		if code != 0 {
			t.Fatalf("code=%d msg=%q want 0", code, msg)
		}
		if len(captured) != before+1 {
			t.Fatalf("message should be sent when agent is in session, captured grew %d->%d", before, len(captured))
		}
		last := captured[len(captured)-1]
		if last.IdentityMode != agentmsg.ModeCaller || last.CallerID != ownerID {
			t.Fatalf("identity not owner: mode=%q caller=%d", last.IdentityMode, last.CallerID)
		}
	})
}

func TestDispatchDispatchAgentPromptPath(t *testing.T) {
	const (
		ownerID  = int64(44201)
		actorID  = int64(44202)
		targetID = int64(44203)
	)
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	seedAgentInvokeDispatchActor(t, testDB, ownerID, actorID, "ak_dispatch_prompt")
	seedAgentInvokeDispatchScope(t, actorID, agentscope.ScopeAgentDispatch)
	seedDispatchTargetAgent(t, ownerID, targetID, model.AgentClientTypeOpenClaw)

	var captured []SendMessageReq
	hooks := agentInvokeHooks{
		sendMessage: func(req SendMessageReq) (*SendMessageResult, error) {
			captured = append(captured, req)
			return &SendMessageResult{MsgID: 8001}, nil
		},
		bindSession: func(agentID int64, sessionID, actorID, cwd, providerKey string) (*sessionBindResponse, error) {
			t.Fatalf("bindSession should not be called for openclaw")
			return nil, nil
		},
	}

	_, code, msg := dispatchAgentInvokeWithHooks(actorID, ownerID, "dispatch_agent", map[string]interface{}{
		"agent_id": strconv.FormatInt(targetID, 10),
		"cwd":      "/work/repo",
		"task":     "跑一下测试",
	}, hooks)
	if code != 0 {
		t.Fatalf("code=%d msg=%q", code, msg)
	}
	if len(captured) != 1 {
		t.Fatalf("expected 1 prompt message, got %d", len(captured))
	}
	got := captured[0]
	if got.IdentityMode != agentmsg.ModeCaller || got.CallerID != ownerID {
		t.Fatalf("prompt not sent as owner: %+v", got)
	}
	if !strings.Contains(got.Content, "工作目录：/work/repo") || !strings.Contains(got.Content, "跑一下测试") {
		t.Fatalf("prompt content wrong: %q", got.Content)
	}
	assertDispatchOriginAgent(t, got, actorID)
}

// 派发任务的 origin_agent_id 必须是调用方而不是目标：路由层按 origin 排除"发出者"，
// 标成目标会让目标被当成发出者跳过，任务永远不投递（2026-08-28 线上回归）。
func assertDispatchOriginAgent(t *testing.T, req SendMessageReq, wantOrigin int64) {
	t.Helper()
	var extra struct {
		OriginAgentID string `json:"origin_agent_id"`
	}
	if err := json.Unmarshal(req.Extra, &extra); err != nil {
		t.Fatalf("extra not json: %s", string(req.Extra))
	}
	if extra.OriginAgentID != strconv.FormatInt(wantOrigin, 10) {
		t.Fatalf("origin_agent_id=%q want %d (extra=%s)", extra.OriginAgentID, wantOrigin, string(req.Extra))
	}
	if extra.OriginAgentID == strconv.FormatInt(req.AgentID, 10) {
		t.Fatalf("origin_agent_id must not equal target agent %d", req.AgentID)
	}
}

func TestDispatchOriginAgentIDSelfDispatchOmitsOrigin(t *testing.T) {
	if got := dispatchOriginAgentID(7, 7); got != 0 {
		t.Fatalf("self dispatch should omit origin, got %d", got)
	}
	if got := dispatchOriginAgentID(7, 9); got != 7 {
		t.Fatalf("origin should be caller, got %d", got)
	}
}

func TestDispatchDispatchAgentRequiresTargetQuoteCapabilityForNewCallbackProtocol(t *testing.T) {
	const (
		ownerID  = int64(44211)
		actorID  = int64(44212)
		targetID = int64(44213)
	)
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	seedAgentInvokeDispatchActor(t, testDB, ownerID, actorID, "ak_dispatch_quote_capability")
	seedAgentInvokeDispatchScope(t, actorID, agentscope.ScopeAgentDispatch)
	seedDispatchTargetAgent(t, ownerID, targetID, model.AgentClientTypeOpenClaw)

	var captured []SendMessageReq
	hooks := agentInvokeHooks{
		sendMessage: func(req SendMessageReq) (*SendMessageResult, error) {
			captured = append(captured, req)
			return &SendMessageResult{MsgID: 8011}, nil
		},
	}
	params := map[string]interface{}{
		"agent_id": strconv.FormatInt(targetID, 10),
		"cwd":      "/work/repo",
		"task":     "完成后按 report_dispatch_result 回写，使用 quoted_message_id。",
	}

	_, code, msg := dispatchAgentInvokeWithHooks(actorID, ownerID, "dispatch_agent", params, hooks)
	if code != 4002 || msg != "target agent runtime does not support quoted dispatch callbacks" {
		t.Fatalf("missing capability: code=%d msg=%q", code, msg)
	}
	if len(captured) != 0 {
		t.Fatalf("task must not send without target quote capability")
	}

	if err := toolruntime.StoreProfile(context.Background(), toolruntime.Profile{
		AgentID:      targetID,
		OwnerID:      ownerID,
		Capabilities: []string{protocol.AgentAPISessionSendQuoteCapability},
		Online:       true,
		LeaseUntil:   time.Now().Add(time.Minute).UnixMilli(),
	}, time.Minute); err != nil {
		t.Fatalf("seed target runtime profile: %v", err)
	}

	_, code, msg = dispatchAgentInvokeWithHooks(actorID, ownerID, "dispatch_agent", params, hooks)
	if code != 0 {
		t.Fatalf("quote-capable target: code=%d msg=%q", code, msg)
	}
	if len(captured) != 1 {
		t.Fatalf("captured=%d want=1", len(captured))
	}
}

func TestDispatchDispatchAgentBindingPath(t *testing.T) {
	const (
		ownerID  = int64(44301)
		actorID  = int64(44302)
		targetID = int64(44303)
	)
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	seedAgentInvokeDispatchActor(t, testDB, ownerID, actorID, "ak_dispatch_bind")
	seedAgentInvokeDispatchScope(t, actorID, agentscope.ScopeAgentDispatch)
	seedDispatchTargetAgent(t, ownerID, targetID, model.AgentClientTypeClaude)

	t.Run("binds then sends task", func(t *testing.T) {
		var captured []SendMessageReq
		var boundCwd, boundProvider string
		hooks := agentInvokeHooks{
			sendMessage: func(req SendMessageReq) (*SendMessageResult, error) {
				captured = append(captured, req)
				return &SendMessageResult{MsgID: 9001}, nil
			},
			bindSession: func(agentID int64, sessionID, actorID, cwd, providerKey string) (*sessionBindResponse, error) {
				boundCwd = cwd
				boundProvider = providerKey
				return &sessionBindResponse{ProviderKey: providerKey, Cwd: cwd, BindingID: "bind-1", WorkerStatus: "ready"}, nil
			},
		}
		data, code, msg := dispatchAgentInvokeWithHooks(actorID, ownerID, "dispatch_agent", map[string]interface{}{
			"agent_id": strconv.FormatInt(targetID, 10),
			"cwd":      "/work/proj",
			"task":     "实现功能 X",
		}, hooks)
		if code != 0 {
			t.Fatalf("code=%d msg=%q", code, msg)
		}
		if boundProvider != "claude" || boundCwd != "/work/proj" {
			t.Fatalf("bind args wrong: provider=%q cwd=%q", boundProvider, boundCwd)
		}
		if len(captured) != 1 || captured[0].Content != "实现功能 X" {
			t.Fatalf("task message wrong: %+v", captured)
		}
		if captured[0].IdentityMode != agentmsg.ModeCaller {
			t.Fatalf("task not sent as owner: %+v", captured[0])
		}
		assertDispatchOriginAgent(t, captured[0], actorID)
		m, _ := data.(map[string]interface{})
		if m["mode"] != "binding" {
			t.Fatalf("expected mode=binding, got %v", m["mode"])
		}
	})

	t.Run("bind timeout returns error and does not send task", func(t *testing.T) {
		var captured []SendMessageReq
		hooks := agentInvokeHooks{
			sendMessage: func(req SendMessageReq) (*SendMessageResult, error) {
				captured = append(captured, req)
				return &SendMessageResult{MsgID: 9002}, nil
			},
			bindSession: func(agentID int64, sessionID, actorID, cwd, providerKey string) (*sessionBindResponse, error) {
				return nil, ErrSessionBindTimeout
			},
		}
		_, code, _ := dispatchAgentInvokeWithHooks(actorID, ownerID, "dispatch_agent", map[string]interface{}{
			"agent_id": strconv.FormatInt(targetID, 10),
			"cwd":      "/work/proj2",
			"task":     "别发出去",
		}, hooks)
		if code != 4290 {
			t.Fatalf("code=%d want 4290", code)
		}
		if len(captured) != 0 {
			t.Fatalf("task must not be sent on bind failure, got %d", len(captured))
		}
	})
}

// TestDispatchDispatchAgentAlwaysNewSessionWithTitle 验证两点：
//   - 每次派发都新建独立会话，不复用历史会话；
//   - title 入参写入会话标题，未传时从任务文本兜底截取。
func TestDispatchDispatchAgentAlwaysNewSessionWithTitle(t *testing.T) {
	const (
		ownerID  = int64(44501)
		actorID  = int64(44502)
		targetID = int64(44503)
	)
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	seedAgentInvokeDispatchActor(t, testDB, ownerID, actorID, "ak_dispatch_newsess")
	seedAgentInvokeDispatchScope(t, actorID, agentscope.ScopeAgentDispatch)
	seedDispatchTargetAgent(t, ownerID, targetID, model.AgentClientTypeOpenClaw)

	hooks := agentInvokeHooks{
		sendMessage: func(req SendMessageReq) (*SendMessageResult, error) {
			return &SendMessageResult{MsgID: 7001}, nil
		},
	}

	dispatch := func(params map[string]interface{}) string {
		params["agent_id"] = strconv.FormatInt(targetID, 10)
		params["cwd"] = "/work/repo"
		data, code, msg := dispatchAgentInvokeWithHooks(actorID, ownerID, "dispatch_agent", params, hooks)
		if code != 0 {
			t.Fatalf("code=%d msg=%q", code, msg)
		}
		m, _ := data.(map[string]interface{})
		sid, _ := m["session_id"].(string)
		if sid == "" {
			t.Fatalf("missing session_id in %+v", m)
		}
		return sid
	}

	titleOf := func(sessionID string) string {
		var s model.Session
		if err := store.DB.Select("last_msg_summary").
			Where("session_id = ?", sessionID).First(&s).Error; err != nil {
			t.Fatalf("load session %s: %v", sessionID, err)
		}
		return s.LastMsgSummary
	}

	// 两次相同 cwd 的派发必须落到不同会话（不复用）。
	sid1 := dispatch(map[string]interface{}{"task": "跑测试", "title": "回归测试任务"})
	sid2 := dispatch(map[string]interface{}{"task": "跑测试", "title": "回归测试任务"})
	if sid1 == sid2 {
		t.Fatalf("expected new session each dispatch, both got %s", sid1)
	}
	// 显式 title 写入会话标题。
	if got := titleOf(sid1); got != "回归测试任务" {
		t.Fatalf("title not applied: %q", got)
	}
	// 未传 title 时取任务首行非空文本兜底。
	sid3 := dispatch(map[string]interface{}{"task": "  \n实现登录功能\n第二行细节"})
	if got := titleOf(sid3); got != "实现登录功能" {
		t.Fatalf("fallback title wrong: %q", got)
	}
}

func TestDispatchDispatchAgentOwnershipAndParams(t *testing.T) {
	const (
		ownerID = int64(44401)
		actorID = int64(44402)
	)
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	seedAgentInvokeDispatchActor(t, testDB, ownerID, actorID, "ak_dispatch_perm")
	seedAgentInvokeDispatchScope(t, actorID, agentscope.ScopeAgentDispatch)

	hooks := agentInvokeHooks{
		sendMessage: func(req SendMessageReq) (*SendMessageResult, error) {
			return &SendMessageResult{MsgID: 1}, nil
		},
		bindSession: func(agentID int64, sessionID, actorID, cwd, providerKey string) (*sessionBindResponse, error) {
			return &sessionBindResponse{}, nil
		},
	}

	t.Run("rejects missing cwd", func(t *testing.T) {
		_, code, _ := dispatchAgentInvokeWithHooks(actorID, ownerID, "dispatch_agent", map[string]interface{}{
			"agent_id": "44402",
			"task":     "x",
		}, hooks)
		if code != 4001 {
			t.Fatalf("code=%d want 4001", code)
		}
	})

	t.Run("rejects missing task", func(t *testing.T) {
		_, code, msg := dispatchAgentInvokeWithHooks(actorID, ownerID, "dispatch_agent", map[string]interface{}{
			"agent_id": "44402",
			"cwd":      "/x",
		}, hooks)
		if code != 4001 {
			t.Fatalf("code=%d want 4001", code)
		}
		if !strings.Contains(msg, "task required") {
			t.Fatalf("msg=%q want task required", msg)
		}
	})

	t.Run("rejects whitespace-only task", func(t *testing.T) {
		_, code, _ := dispatchAgentInvokeWithHooks(actorID, ownerID, "dispatch_agent", map[string]interface{}{
			"agent_id": "44402",
			"cwd":      "/x",
			"task":     "  \n\t ",
		}, hooks)
		if code != 4001 {
			t.Fatalf("code=%d want 4001", code)
		}
	})

	t.Run("rejects overlong title", func(t *testing.T) {
		_, code, msg := dispatchAgentInvokeWithHooks(actorID, ownerID, "dispatch_agent", map[string]interface{}{
			"agent_id": "44402",
			"cwd":      "/x",
			"task":     "y",
			"title":    strings.Repeat("题", dispatchTitleMaxRunes+1),
		}, hooks)
		if code != 4001 {
			t.Fatalf("code=%d want 4001", code)
		}
		if !strings.Contains(msg, "title too long") {
			t.Fatalf("msg=%q want title too long", msg)
		}
	})

	t.Run("accepts title at max length", func(t *testing.T) {
		seedDispatchTargetAgent(t, ownerID, 44403, model.AgentClientTypeOpenClaw)
		_, code, msg := dispatchAgentInvokeWithHooks(actorID, ownerID, "dispatch_agent", map[string]interface{}{
			"agent_id": "44403",
			"cwd":      "/x",
			"task":     "y",
			"title":    strings.Repeat("题", dispatchTitleMaxRunes),
		}, hooks)
		if code != 0 {
			t.Fatalf("code=%d msg=%q", code, msg)
		}
	})

	t.Run("rejects agent of another owner", func(t *testing.T) {
		const foreign = int64(44490)
		seedDispatchTargetAgent(t, ownerID+999, foreign, model.AgentClientTypeClaude)
		_, code, _ := dispatchAgentInvokeWithHooks(actorID, ownerID, "dispatch_agent", map[string]interface{}{
			"agent_id": strconv.FormatInt(foreign, 10),
			"cwd":      "/x",
			"task":     "y",
		}, hooks)
		if code != 4003 {
			t.Fatalf("code=%d want 4003", code)
		}
	})
}

package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
)

type stopSlashCall struct {
	ownerID        int64
	sessionID      string
	agentID        int64
	clientActionID string
}

func newStopSlashRoute(agents ...model.Agent) *directSessionRoute {
	route := &directSessionRoute{}
	for _, agent := range agents {
		route.Targets = append(route.Targets, directDispatchTarget{Agent: agent})
	}
	return route
}

func apiAgent(id, ownerID int64) model.Agent {
	return model.Agent{ID: id, OwnerID: ownerID, ProviderType: model.AgentProviderAPI}
}

func TestIsStopSlashCommand(t *testing.T) {
	cases := []struct {
		name    string
		msgType int16
		content string
		want    bool
	}{
		{name: "exact", msgType: model.MsgTypeText, content: "/stop", want: true},
		{name: "surrounding spaces", msgType: model.MsgTypeText, content: "  /stop\n", want: true},
		{name: "upper case", msgType: model.MsgTypeText, content: "/STOP", want: true},
		{name: "with argument", msgType: model.MsgTypeText, content: "/stop xxx", want: false},
		{name: "embedded", msgType: model.MsgTypeText, content: "帮我 /stop 一下", want: false},
		{name: "prefix only", msgType: model.MsgTypeText, content: "/stopped", want: false},
		{name: "empty", msgType: model.MsgTypeText, content: "   ", want: false},
		{name: "non text msg", msgType: 2, content: "/stop", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStopSlashCommand(tc.msgType, tc.content); got != tc.want {
				t.Fatalf("isStopSlashCommand(%d, %q) = %v, want %v", tc.msgType, tc.content, got, tc.want)
			}
		})
	}
}

// 有活动 run：/stop 被消费（不再作为 user_chat 事件下发），走工具栏同一入口，
// 并把工具栏 ack 文案写回会话。
func TestMaybeHandleStopSlashCommandStopsOwnerAgent(t *testing.T) {
	var stops []stopSlashCall
	var notices []wsagentapi.SendMessageReq
	stop := func(_ context.Context, ownerID int64, sessionID string, agentID int64, clientActionID string) (bool, string, error) {
		stops = append(stops, stopSlashCall{ownerID, sessionID, agentID, clientActionID})
		return true, "已提交停止请求", nil
	}
	sendNotice := func(_ context.Context, req wsagentapi.SendMessageReq) error {
		notices = append(notices, req)
		return nil
	}

	handled := maybeHandleStopSlashCommand(
		context.Background(),
		"sess-1",
		1001,
		1,
		9001,
		model.MsgTypeText,
		" /stop ",
		newStopSlashRoute(apiAgent(2001, 1001)),
		stop,
		sendNotice,
	)
	if !handled {
		t.Fatalf("maybeHandleStopSlashCommand() = false, want true (message must not be dispatched)")
	}
	if len(stops) != 1 {
		t.Fatalf("stop calls = %d, want 1", len(stops))
	}
	want := stopSlashCall{ownerID: 1001, sessionID: "sess-1", agentID: 2001, clientActionID: "slash_stop:2001:9001"}
	if stops[0] != want {
		t.Fatalf("stop call = %+v, want %+v", stops[0], want)
	}
	if len(notices) != 1 {
		t.Fatalf("notice count = %d, want 1", len(notices))
	}
	if notices[0].Content != "已提交停止请求" {
		t.Fatalf("notice content = %q, want toolbar ack wording", notices[0].Content)
	}
	if notices[0].SessionID != "sess-1" || notices[0].AgentID != 2001 || notices[0].OwnerID != 1001 {
		t.Fatalf("notice target = %+v, want session sess-1 agent 2001 owner 1001", notices[0])
	}
}

// 无活动 run：仍然被消费（不入队、不报错），提示沿用工具栏拒绝文案。
func TestMaybeHandleStopSlashCommandWithoutActiveRun(t *testing.T) {
	var notices []wsagentapi.SendMessageReq
	stop := func(context.Context, int64, string, int64, string) (bool, string, error) {
		return false, "当前没有可停止的输出", nil
	}
	sendNotice := func(_ context.Context, req wsagentapi.SendMessageReq) error {
		notices = append(notices, req)
		return nil
	}

	handled := maybeHandleStopSlashCommand(
		context.Background(), "sess-1", 1001, 1, 9001, model.MsgTypeText, "/stop",
		newStopSlashRoute(apiAgent(2001, 1001)), stop, sendNotice,
	)
	if !handled {
		t.Fatalf("maybeHandleStopSlashCommand() = false, want true (must not enqueue a user_chat event)")
	}
	if len(notices) != 1 || notices[0].Content != "当前没有可停止的输出" {
		t.Fatalf("notices = %+v, want single toolbar rejection wording", notices)
	}
}

func TestMaybeHandleStopSlashCommandSkipsNonExactCommand(t *testing.T) {
	called := false
	stop := func(context.Context, int64, string, int64, string) (bool, string, error) {
		called = true
		return true, "", nil
	}
	for _, content := range []string{"/stop xxx", "帮我 /stop", "/stopped"} {
		if maybeHandleStopSlashCommand(
			context.Background(), "sess-1", 1001, 1, 9001, model.MsgTypeText, content,
			newStopSlashRoute(apiAgent(2001, 1001)), stop, nil,
		) {
			t.Fatalf("content %q was intercepted, want normal dispatch", content)
		}
	}
	if called {
		t.Fatalf("stop executed for a non-exact /stop command")
	}
}

func TestMaybeHandleStopSlashCommandSkipsNonOwnerTargets(t *testing.T) {
	called := false
	stop := func(context.Context, int64, string, int64, string) (bool, string, error) {
		called = true
		return true, "", nil
	}
	cases := []struct {
		name       string
		senderID   int64
		senderType int16
		route      *directSessionRoute
	}{
		{
			name:     "other owner agent in group",
			senderID: 1001, senderType: 1,
			route: newStopSlashRoute(apiAgent(2001, 1002)),
		},
		{
			name:     "sender is an agent",
			senderID: 2001, senderType: 2,
			route: newStopSlashRoute(apiAgent(2001, 2001)),
		},
		{
			name:     "non agent-api provider",
			senderID: 1001, senderType: 1,
			route: newStopSlashRoute(model.Agent{ID: 2001, OwnerID: 1001, ProviderType: model.AgentProviderLocal}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if maybeHandleStopSlashCommand(
				context.Background(), "sess-1", tc.senderID, tc.senderType, 9001, model.MsgTypeText, "/stop",
				tc.route, stop, nil,
			) {
				t.Fatalf("maybeHandleStopSlashCommand() = true, want false")
			}
		})
	}
	if called {
		t.Fatalf("stop executed for a target the sender does not own")
	}
}

// 停止执行失败时不吞消息：退回普通文本链路，由 agent 自己处理。
func TestMaybeHandleStopSlashCommandFallsBackOnStopError(t *testing.T) {
	stop := func(context.Context, int64, string, int64, string) (bool, string, error) {
		return false, "", errors.New("agent toolbar service unavailable")
	}
	if maybeHandleStopSlashCommand(
		context.Background(), "sess-1", 1001, 1, 9001, model.MsgTypeText, "/stop",
		newStopSlashRoute(apiAgent(2001, 1001)), stop, nil,
	) {
		t.Fatalf("maybeHandleStopSlashCommand() = true, want false so the message still reaches the agent")
	}
}

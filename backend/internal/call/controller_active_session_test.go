package call

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

// 接点B 反查防串线规范测试（架构文档 29 §4.2.1）。
func TestGetActiveCallBySession(t *testing.T) {
	mk := func() *Controller {
		c := New(nil, nil, nil)
		return c
	}
	aiEntry := func(sessionID string, provider string) *callEntry {
		return &callEntry{
			record: model.CallRecord{SessionID: sessionID, State: model.CallStateAIDelegated},
			aiSpec: VoiceBridgeSpec{Provider: provider},
		}
	}

	t.Run("active AI call returns id+provider", func(t *testing.T) {
		c := mk()
		c.calls[100] = aiEntry("s1", "doubao_realtime")
		id, provider, direct, ok := c.GetActiveCallBySession("s1")
		if !ok || id != 100 || provider != "doubao_realtime" {
			t.Fatalf("want (100,doubao_realtime,true) got (%d,%q,%v)", id, provider, ok)
		}
		if direct {
			t.Fatal("访客代接通话不应判为直拨")
		}
	})

	t.Run("direct AI call detected", func(t *testing.T) {
		c := mk()
		agentID := int64(77)
		c.calls[100] = &callEntry{
			record: model.CallRecord{
				SessionID:        "s1",
				State:            model.CallStateAIDelegated,
				CalleeID:         agentID,
				DelegatedAgentID: &agentID,
			},
			aiSpec: VoiceBridgeSpec{Provider: "doubao_realtime"},
		}
		_, _, direct, ok := c.GetActiveCallBySession("s1")
		if !ok || !direct {
			t.Fatalf("DelegatedAgentID==CalleeID 应判为直拨 got (direct=%v, ok=%v)", direct, ok)
		}
	})

	t.Run("delegated answer not direct", func(t *testing.T) {
		c := mk()
		agentID := int64(77)
		c.calls[100] = &callEntry{
			record: model.CallRecord{
				SessionID:        "s1",
				State:            model.CallStateAIDelegated,
				CalleeID:         88, // 被叫是人类 owner，非 agent 本身
				DelegatedAgentID: &agentID,
			},
			aiSpec: VoiceBridgeSpec{Provider: "doubao_realtime"},
		}
		_, _, direct, ok := c.GetActiveCallBySession("s1")
		if !ok || direct {
			t.Fatalf("AI 代接（DelegatedAgentID!=CalleeID）不应判为直拨 got (direct=%v, ok=%v)", direct, ok)
		}
	})

	t.Run("non-delegated state not matched", func(t *testing.T) {
		c := mk()
		c.calls[101] = &callEntry{record: model.CallRecord{SessionID: "s1", State: model.CallStateRinging}}
		c.calls[102] = &callEntry{record: model.CallRecord{SessionID: "s1", State: model.CallStateHumanActive}}
		if _, _, _, ok := c.GetActiveCallBySession("s1"); ok {
			t.Fatal("非 AI_DELEGATED 状态不应匹配")
		}
	})

	t.Run("session mismatch returns false", func(t *testing.T) {
		c := mk()
		c.calls[103] = aiEntry("s1", "doubao_realtime")
		if _, _, _, ok := c.GetActiveCallBySession("other"); ok {
			t.Fatal("不同 session 不应匹配")
		}
	})

	t.Run("empty session returns false", func(t *testing.T) {
		c := mk()
		c.calls[104] = aiEntry("s1", "doubao_realtime")
		if _, _, _, ok := c.GetActiveCallBySession(""); ok {
			t.Fatal("空 session 应返回 false")
		}
	})

	t.Run("ambiguous multi-active same session returns false", func(t *testing.T) {
		c := mk()
		c.calls[105] = aiEntry("s1", "doubao_realtime")
		c.calls[106] = aiEntry("s1", "doubao_realtime")
		if _, _, _, ok := c.GetActiveCallBySession("s1"); ok {
			t.Fatal("同 session 多通活跃 AI 应视为歧义返回 false（防串线）")
		}
	})

	t.Run("other sessions do not interfere", func(t *testing.T) {
		c := mk()
		c.calls[107] = aiEntry("s1", "doubao_realtime")
		c.calls[108] = aiEntry("s2", "openai_realtime")
		c.calls[109] = &callEntry{record: model.CallRecord{SessionID: "s3", State: model.CallStateRinging}}
		id, provider, _, ok := c.GetActiveCallBySession("s2")
		if !ok || id != 108 || provider != "openai_realtime" {
			t.Fatalf("want (108,openai_realtime,true) got (%d,%q,%v)", id, provider, ok)
		}
	})
}

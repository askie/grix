package agentapi

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

// TestHandleSkillLibraryActionPendingResult_Success 覆盖 skill_enable/skill_disable
// 成功路径：connector 在 result 里回 enable_state + uninstallable，需原样透传给等待方。
func TestHandleSkillLibraryActionPendingResult_Success(t *testing.T) {
	ch := make(chan *skillLibraryActionResponse, 1)
	pending := &pendingLocalAction{
		actionID:             "skill_enable:1:1",
		kind:                 "skill_enable",
		actionType:           "skill_enable",
		skillLibraryResultCh: ch,
	}

	var m *Manager
	m.handleSkillLibraryActionPendingResult(pending, protocol.LocalActionResultPayload{
		ActionID: pending.actionID,
		Status:   "ok",
		Result: map[string]any{
			"enable_state":  "link",
			"uninstallable": true,
		},
	})

	select {
	case resp := <-ch:
		if resp.Error != "" {
			t.Fatalf("unexpected error: %q", resp.Error)
		}
		if resp.EnableState != "link" {
			t.Fatalf("enable_state=%q want=%q", resp.EnableState, "link")
		}
		if !resp.Uninstallable {
			t.Fatalf("uninstallable=%v want=true", resp.Uninstallable)
		}
	default:
		t.Fatalf("expected a result on skillLibraryResultCh")
	}
}

// TestHandleSkillLibraryActionPendingResult_FailedWithConflict 覆盖失败路径：
// connector 报 failed + error_msg/error_code，并在 result.conflict_kind 里带冲突详情，
// 供客户端渲染"覆盖为链接/替换为普通文件"二次确认。
func TestHandleSkillLibraryActionPendingResult_FailedWithConflict(t *testing.T) {
	ch := make(chan *skillLibraryActionResponse, 1)
	pending := &pendingLocalAction{
		actionID:             "skill_enable:1:2",
		kind:                 "skill_enable",
		actionType:           "skill_enable",
		skillLibraryResultCh: ch,
	}

	var m *Manager
	m.handleSkillLibraryActionPendingResult(pending, protocol.LocalActionResultPayload{
		ActionID:  pending.actionID,
		Status:    "failed",
		ErrorCode: "skill_conflict",
		ErrorMsg:  "同名文件已存在且不是本平台管理的链接",
		Result: map[string]any{
			"conflict_kind": "unmanaged_file",
		},
	})

	select {
	case resp := <-ch:
		if resp.Error != "同名文件已存在且不是本平台管理的链接" {
			t.Fatalf("error=%q", resp.Error)
		}
		if resp.ConflictKind != "unmanaged_file" {
			t.Fatalf("conflict_kind=%q want=%q", resp.ConflictKind, "unmanaged_file")
		}
	default:
		t.Fatalf("expected a result on skillLibraryResultCh")
	}
}

// TestHandleSkillLibraryActionPendingResult_FailedFallsBackToErrorCode 当 connector
// 没给 error_msg 时，用 error_code 兜底；两者都没有时用状态兜底文案。
func TestHandleSkillLibraryActionPendingResult_FailedFallsBackToErrorCode(t *testing.T) {
	ch := make(chan *skillLibraryActionResponse, 1)
	pending := &pendingLocalAction{
		actionID:             "skill_disable:1:3",
		kind:                 "skill_disable",
		actionType:           "skill_disable",
		skillLibraryResultCh: ch,
	}

	var m *Manager
	m.handleSkillLibraryActionPendingResult(pending, protocol.LocalActionResultPayload{
		ActionID:  pending.actionID,
		Status:    "failed",
		ErrorCode: "not_found",
	})

	resp := <-ch
	if resp.Error != "not_found" {
		t.Fatalf("error=%q want=%q", resp.Error, "not_found")
	}
}

func TestHandleSkillLibraryActionPendingResult_NilChannelNoop(t *testing.T) {
	var m *Manager
	// pending 为 nil 或 result channel 为 nil 时应直接返回，不能 panic。
	m.handleSkillLibraryActionPendingResult(nil, protocol.LocalActionResultPayload{Status: "ok"})
	m.handleSkillLibraryActionPendingResult(&pendingLocalAction{}, protocol.LocalActionResultPayload{Status: "ok"})
}

func TestSendSkillEnableActionAndWait_RequiresNameAndScope(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	if _, err := mgr.SendSkillEnableActionAndWait(1, 1, "sess-1", "", "global", "1", ""); err == nil {
		t.Fatalf("expected error for empty name")
	}
	if _, err := mgr.SendSkillEnableActionAndWait(1, 1, "sess-1", "grix-log-locator", "", "1", ""); err == nil {
		t.Fatalf("expected error for empty scope")
	}
}

func TestSendSkillEnableActionAndWait_NotSupportedWhenOffline(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	_, err := mgr.SendSkillEnableActionAndWait(9701, 9701, "sess-1", "grix-log-locator", "global", "9701", "")
	if err != ErrSkillEnableNotSupported {
		t.Fatalf("err=%v want=%v", err, ErrSkillEnableNotSupported)
	}
}

func TestSendSkillDisableActionAndWait_RequiresNameAndScope(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	if _, err := mgr.SendSkillDisableActionAndWait(1, 1, "sess-1", "", "global", "1"); err == nil {
		t.Fatalf("expected error for empty name")
	}
	if _, err := mgr.SendSkillDisableActionAndWait(1, 1, "sess-1", "grix-log-locator", "", "1"); err == nil {
		t.Fatalf("expected error for empty scope")
	}
}

func TestSendSkillDisableActionAndWait_NotSupportedWhenOffline(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	_, err := mgr.SendSkillDisableActionAndWait(9702, 9702, "sess-1", "grix-log-locator", "global", "9702")
	if err != ErrSkillDisableNotSupported {
		t.Fatalf("err=%v want=%v", err, ErrSkillDisableNotSupported)
	}
}

// TestSendSkillRefreshActionAndWait_NotSupportedWhenOffline 覆盖 agent 离线/未声明
// skill_refresh 时的快速失败路径：dispatch 不出去必须立即返回 NotSupported，不能挂到超时。
func TestSendSkillRefreshActionAndWait_NotSupportedWhenOffline(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	err := mgr.SendSkillRefreshActionAndWait(9703, 9703, "sess-1", "9703")
	if err != ErrSkillRefreshNotSupported {
		t.Fatalf("err=%v want=%v", err, ErrSkillRefreshNotSupported)
	}
}

// TestSkillRefreshPendingResultRouting 覆盖 local_action_result 按 kind=skill_refresh
// 路由到等待方：插件回 ok 时等待方拿到无错误的响应。
func TestSkillRefreshPendingResultRouting(t *testing.T) {
	ch := make(chan *skillLibraryActionResponse, 1)
	pending := &pendingLocalAction{
		actionID:             "skill_refresh:1:1",
		kind:                 "skill_refresh",
		actionType:           "skill_refresh",
		skillLibraryResultCh: ch,
	}

	var m *Manager
	m.handleSkillLibraryActionPendingResult(pending, protocol.LocalActionResultPayload{
		ActionID: pending.actionID,
		Status:   "ok",
	})

	select {
	case resp := <-ch:
		if resp.Error != "" {
			t.Fatalf("unexpected error: %q", resp.Error)
		}
	default:
		t.Fatalf("expected a result on skillLibraryResultCh")
	}
}

package quotaremind

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/pkg/userpref"
)

// MessageSender 定义向会话以主人身份投递消息的接口。
type MessageSender interface {
	SendOwnerMessage(ctx context.Context, ownerID int64, sessionID string, agentID int64, content string) error
}

// LanguageResolver 获取用户偏好语言的抽象。
type LanguageResolver func(ctx context.Context, userID int64) string

type sentRecord struct {
	cycleID string
	sentAt  time.Time
}

// Service 管理限额低余量统一提醒与防抖发送。
type Service struct {
	sender   MessageSender
	loadLang LanguageResolver

	mu     sync.Mutex
	cycles map[string]sentRecord
}

// NewService 创建限额低余量提醒服务。
func NewService(sender MessageSender, loadLang LanguageResolver) *Service {
	if loadLang == nil {
		loadLang = userpref.LanguageIfSet
	}
	return &Service{
		sender:   sender,
		loadLang: loadLang,
		cycles:   make(map[string]sentRecord),
	}
}

// CheckSnapshot 检查工具栏快照中的限额项，并在剩余比例 <= 10% 时以主人身份发起交接提醒。
func (s *Service) CheckSnapshot(ctx context.Context, ownerID int64, snapshot toolprotocol.Snapshot) error {
	if s == nil || s.sender == nil || ownerID <= 0 || strings.TrimSpace(snapshot.SessionID) == "" || snapshot.AgentID <= 0 {
		return nil
	}

	alert, triggered := EvaluateQuotaAlert(snapshot.Items)
	if !triggered {
		// 余量已恢复或无低额度告警，清理该会话和智能体的防抖状态以便未来再度达到阈值时能正常告警
		s.clearAlert(ownerID, snapshot.SessionID, snapshot.AgentID)
		return nil
	}

	dedupKey := s.buildDedupKey(ownerID, snapshot.SessionID, snapshot.AgentID, alert.WindowType)

	// 幂等防重：同一限额周期内只提醒一次
	cycleID := alert.ResetsAt
	if cycleID == "" {
		cycleID = "active"
	}

	s.mu.Lock()
	if prev, ok := s.cycles[dedupKey]; ok {
		if cycleID != "active" && prev.cycleID == cycleID {
			s.mu.Unlock()
			return nil
		}
		// 若无 reset 时间戳，24 小时内不重复发送
		if cycleID == "active" && time.Since(prev.sentAt) < 24*time.Hour {
			s.mu.Unlock()
			return nil
		}
	}
	s.cleanupOldRecordsLocked()
	s.mu.Unlock()

	// 语言版本解析：未设置语言或非中文一律默认走英文版
	rawLang := s.loadLang(ctx, ownerID)
	lang := resolveLanguage(rawLang)

	// 格式化本地化文案
	content := FormatReminderMessage(lang, alert.WindowType, alert.RemainingPercent)
	if content == "" {
		return nil
	}

	if err := s.sender.SendOwnerMessage(ctx, ownerID, snapshot.SessionID, snapshot.AgentID, content); err != nil {
		return err
	}

	s.mu.Lock()
	s.cycles[dedupKey] = sentRecord{
		cycleID: cycleID,
		sentAt:  time.Now(),
	}
	s.mu.Unlock()

	return nil
}

// FormatReminderMessage 按照语言生成主人口吻的提醒文本。
func FormatReminderMessage(lang, windowType string, remainingPercent float64) string {
	windowKey := "quota_window_five_hour"
	if windowType == WindowWeekly {
		windowKey = "quota_window_weekly"
	}
	windowLabel := tooli18n.T(lang, windowKey)
	return tooli18n.Tf(lang, "quota_remind_handover", windowLabel, remainingPercent)
}

// resolveLanguage 判定文案语言：未设置语言（空值）或非中文一律默认走英文版。
func resolveLanguage(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if strings.HasPrefix(v, "zh") {
		return "zh"
	}
	return "en"
}

func (s *Service) buildDedupKey(ownerID int64, sessionID string, agentID int64, windowType string) string {
	return fmt.Sprintf("%d:%s:%d:%s", ownerID, strings.TrimSpace(sessionID), agentID, windowType)
}

func (s *Service) clearAlert(ownerID int64, sessionID string, agentID int64) {
	prefix := fmt.Sprintf("%d:%s:%d:", ownerID, strings.TrimSpace(sessionID), agentID)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.cycles {
		if strings.HasPrefix(k, prefix) {
			delete(s.cycles, k)
		}
	}
}

func (s *Service) cleanupOldRecordsLocked() {
	if len(s.cycles) < 2000 {
		return
	}
	now := time.Now()
	for k, v := range s.cycles {
		if now.Sub(v.sentAt) > 30*24*time.Hour {
			delete(s.cycles, k)
		}
	}
}

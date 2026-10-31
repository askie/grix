package quotaremind

import (
	"context"
	"strings"
	"time"

	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/pkg/userpref"
	appstore "github.com/askie/grix/backend/internal/store"
)

// MessageSender 定义向会话以主人身份投递消息的接口。
type MessageSender interface {
	SendOwnerMessage(ctx context.Context, ownerID int64, sessionID string, agentID int64, content string) error
}

// LanguageResolver 获取用户偏好语言的抽象。
type LanguageResolver func(ctx context.Context, userID int64) string

// Service 管理限额低余量统一提醒、周期防重与节流发送。
type Service struct {
	sender      MessageSender
	loadLang    LanguageResolver
	limiter     Limiter
	throttleTTL time.Duration
}

// NewService 创建限额低余量提醒服务。
func NewService(sender MessageSender, loadLang LanguageResolver, limiter Limiter) *Service {
	if loadLang == nil {
		loadLang = userpref.LanguageIfSet
	}
	if limiter == nil {
		limiter = NewRedisLimiter(appstore.RDB, defaultThrottleTTL)
	}
	return &Service{
		sender:      sender,
		loadLang:    loadLang,
		limiter:     limiter,
		throttleTTL: defaultThrottleTTL,
	}
}

// SetThrottleTTL 设置自定义节流冷却窗口（默认 2 分钟）。
func (s *Service) SetThrottleTTL(d time.Duration) {
	if s != nil && d > 0 {
		s.throttleTTL = d
	}
}

// CheckSnapshot 检查工具栏快照中的限额项，并在剩余比例 <= 10%（及 <= 5%）时以主人身份发起交接提醒。
// 规则：
// 1. 当剩余比例 <= 10% 时，一个会话在同一限额周期内只能发一次。
// 2. 当剩余比例继续降至 <= 5% 时，再发一次，之后彻底不再发送，单个周期内最多发送两次。
func (s *Service) CheckSnapshot(ctx context.Context, ownerID int64, snapshot toolprotocol.Snapshot) error {
	if s == nil || s.sender == nil || ownerID <= 0 || strings.TrimSpace(snapshot.SessionID) == "" || snapshot.AgentID <= 0 {
		return nil
	}

	alert, triggered := EvaluateQuotaAlert(snapshot.Items)
	if !triggered {
		return nil
	}

	cycleID := alert.ResetsAt
	if cycleID == "" {
		cycleID = "active"
	}

	// 原子抢占该周期对应阶梯的提醒资格与节流窗口
	acquired, release, err := s.limiter.TryAcquire(
		ctx,
		ownerID,
		snapshot.SessionID,
		snapshot.AgentID,
		alert.WindowType,
		cycleID,
		alert.Tier,
		s.throttleTTL,
	)
	if err != nil {
		return err
	}
	if !acquired {
		return nil
	}

	// 语言版本解析：未设置语言或非中文一律默认走英文版
	rawLang := s.loadLang(ctx, ownerID)
	lang := resolveLanguage(rawLang)

	// 格式化本地化文案
	content := FormatReminderMessage(lang, alert.WindowType, alert.ThresholdInt, alert.RemainingPercent)
	if content == "" {
		if release != nil {
			release()
		}
		return nil
	}

	if err := s.sender.SendOwnerMessage(ctx, ownerID, snapshot.SessionID, snapshot.AgentID, content); err != nil {
		if release != nil {
			release()
		}
		return err
	}

	return nil
}

// FormatReminderMessage 按照语言生成主人口吻的提醒文本。
func FormatReminderMessage(lang, windowType string, thresholdInt int, remainingPercent float64) string {
	windowKey := "quota_window_five_hour"
	if windowType == WindowWeekly {
		windowKey = "quota_window_weekly"
	}
	windowLabel := tooli18n.T(lang, windowKey)
	return tooli18n.Tf(lang, "quota_remind_handover", windowLabel, thresholdInt, remainingPercent)
}

// resolveLanguage 判定文案语言：未设置语言（空值）或非中文一律默认走英文版。
func resolveLanguage(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if strings.HasPrefix(v, "zh") {
		return "zh"
	}
	return "en"
}

package quotaremind

import (
	"context"
	"strings"
	"time"

	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/userpref"
	appstore "github.com/askie/grix/backend/internal/store"
)

// SendOwnerMessagesEnabled controls whether evaluated quota alerts are delivered
// as owner-identity session messages. Temporarily false to pause reminders
// without removing evaluation/limiter code; set true to re-enable delivery.
var SendOwnerMessagesEnabled = false

// MessageSender 定义向会话以主人身份投递消息的接口。
type MessageSender interface {
	SendOwnerMessage(ctx context.Context, ownerID int64, sessionID string, agentID int64, content string) error
}

// LanguageResolver 获取用户偏好语言的抽象。
type LanguageResolver func(ctx context.Context, userID int64) string

// Service 管理限额低余量统一提醒、周期防重与节流发送。
type Service struct {
	sender       MessageSender
	loadLang     LanguageResolver
	limiter      Limiter
	activity     SessionActivity
	throttleTTL  time.Duration
	activeWithin time.Duration
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
		sender:       sender,
		loadLang:     loadLang,
		limiter:      limiter,
		throttleTTL:  defaultThrottleTTL,
		activeWithin: defaultActiveWithin,
	}
}

// SetThrottleTTL 设置自定义节流冷却窗口（默认 2 分钟）。
func (s *Service) SetThrottleTTL(d time.Duration) {
	if s != nil && d > 0 {
		s.throttleTTL = d
	}
}

// SetSessionActivity 设置会话活跃度检查器；为 nil 时不检查（单测可注入 mock）。
func (s *Service) SetSessionActivity(activity SessionActivity) {
	if s != nil {
		s.activity = activity
	}
}

// SetActiveWithin 设置「近期有真实对话」的时间窗（默认 24 小时）。
func (s *Service) SetActiveWithin(d time.Duration) {
	if s != nil && d > 0 {
		s.activeWithin = d
	}
}

// CheckSnapshot 检查工具栏快照中的限额项，并在剩余比例 <= 10%（及 <= 5%）时以主人身份发起交接提醒。
// 另：若存在 account_credits 且余额 < 1000，额外发送一次 Credit 低余额提醒（周期内最多一次）。
// 规则：
// 1. 当剩余比例 <= 10% 时，一个会话在同一限额周期内只能发一次。
// 2. 当剩余比例继续降至 <= 5% 时，再发一次，之后彻底不再发送，单个周期内最多发送两次。
// 3. 会话在 activeWithin 内必须有非提醒类真实消息，否则视为冷会话，不发送、不占周期名额。
// 4. Credit 余额提醒与用量百分比提醒相互独立，各自去重。
func (s *Service) CheckSnapshot(ctx context.Context, ownerID int64, snapshot toolprotocol.Snapshot) error {
	if s == nil || s.sender == nil || ownerID <= 0 || strings.TrimSpace(snapshot.SessionID) == "" || snapshot.AgentID <= 0 {
		return nil
	}

	rateAlert, rateTriggered := EvaluateQuotaAlert(snapshot.Items)
	creditAlert, creditTriggered := EvaluateCreditAlert(snapshot.Items)
	if logger.L != nil {
		logger.L.Infow("quota reminder evaluated",
			"owner_id", ownerID,
			"session_id", snapshot.SessionID,
			"agent_id", snapshot.AgentID,
			"rate_triggered", rateTriggered,
			"rate_window", rateAlert.WindowType,
			"rate_tier", rateAlert.Tier,
			"rate_remaining_percent", rateAlert.RemainingPercent,
			"credit_triggered", creditTriggered,
			"credit_balance", creditAlert.RemainingPercent,
		)
	}
	if !rateTriggered && !creditTriggered {
		return nil
	}

	// 冷会话门槛必须在抢占周期名额之前：否则冷会话会「占坑」导致稍后真正活跃时发不出去。
	if s.activity != nil {
		active, err := s.activity.HasRecentConversation(ctx, snapshot.SessionID, s.activeWithin)
		if err != nil {
			return err
		}
		if !active {
			if logger.L != nil {
				logger.L.Infow("quota reminder skipped cold session",
					"owner_id", ownerID,
					"session_id", snapshot.SessionID,
					"agent_id", snapshot.AgentID,
					"active_within", s.activeWithin.String(),
				)
			}
			return nil
		}
	}

	rawLang := s.loadLang(ctx, ownerID)
	lang := resolveLanguage(rawLang)

	if rateTriggered {
		if err := s.sendAlert(ctx, ownerID, snapshot, rateAlert, FormatReminderMessage(lang, rateAlert.WindowType, rateAlert.ThresholdInt, rateAlert.RemainingPercent)); err != nil {
			return err
		}
	}
	if creditTriggered {
		if err := s.sendAlert(ctx, ownerID, snapshot, creditAlert, FormatCreditReminderMessage(lang, creditAlert.RemainingPercent)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) sendAlert(
	ctx context.Context,
	ownerID int64,
	snapshot toolprotocol.Snapshot,
	alert QuotaAlert,
	content string,
) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	if !SendOwnerMessagesEnabled {
		if logger.L != nil {
			logger.L.Infow("quota reminder send skipped (temporarily disabled)",
				"owner_id", ownerID,
				"session_id", snapshot.SessionID,
				"agent_id", snapshot.AgentID,
				"window_type", alert.WindowType,
				"tier", alert.Tier,
			)
		}
		return nil
	}

	cycleID := alert.ResetsAt
	if cycleID == "" {
		cycleID = "active"
	}

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
		if logger.L != nil {
			logger.L.Infow("quota reminder rate limited",
				"owner_id", ownerID,
				"session_id", snapshot.SessionID,
				"agent_id", snapshot.AgentID,
				"window_type", alert.WindowType,
				"cycle_id", cycleID,
				"tier", alert.Tier,
			)
		}
		return nil
	}

	if err := s.sender.SendOwnerMessage(ctx, ownerID, snapshot.SessionID, snapshot.AgentID, content); err != nil {
		if release != nil {
			release()
		}
		if logger.L != nil {
			logger.L.Errorw("quota reminder send failed",
				"owner_id", ownerID,
				"session_id", snapshot.SessionID,
				"agent_id", snapshot.AgentID,
				"window_type", alert.WindowType,
				"cycle_id", cycleID,
				"tier", alert.Tier,
				"error", err,
			)
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

// FormatCreditReminderMessage 生成 Credit 低余额交接提醒。
func FormatCreditReminderMessage(lang string, balance float64) string {
	return tooli18n.Tf(lang, "quota_remind_credits", balance, int(CreditAlertBalanceThreshold))
}

// resolveLanguage 判定文案语言：未设置语言或非中文一律默认走英文版。
func resolveLanguage(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if strings.HasPrefix(v, "zh") {
		return "zh"
	}
	return "en"
}

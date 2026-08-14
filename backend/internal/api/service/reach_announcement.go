package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
)

// ReachAnnouncementLocale is the editable copy of one announcement in one
// language. Title+Body compose the in-app system message; EmailSubject and
// EmailIntro feed the branded email frame with Body as the detail section.
type ReachAnnouncementLocale struct {
	Title        string `json:"title"`
	Body         string `json:"body"`
	EmailSubject string `json:"email_subject"`
	EmailIntro   string `json:"email_intro"`
}

// ReachAnnouncementContent is the per-task copy snapshot stored in
// reach_tasks.content. Each recipient gets the locale matching their
// preferred language (zh default, en for English).
type ReachAnnouncementContent struct {
	ZH ReachAnnouncementLocale `json:"zh"`
	EN ReachAnnouncementLocale `json:"en"`
}

// Locale picks the copy for a user's preferred language.
func (c ReachAnnouncementContent) Locale(language string) ReachAnnouncementLocale {
	if language == preferredLanguageEN {
		return c.EN
	}
	return c.ZH
}

// InAppText renders the in-app system message for this locale.
func (l ReachAnnouncementLocale) InAppText() string {
	title := strings.TrimSpace(l.Title)
	body := strings.TrimSpace(l.Body)
	if body == "" {
		return title
	}
	return title + "\n" + body
}

// EmailSubjectOrTitle falls back to the title when the subject was left empty.
func (l ReachAnnouncementLocale) EmailSubjectOrTitle() string {
	if s := strings.TrimSpace(l.EmailSubject); s != "" {
		return s
	}
	return strings.TrimSpace(l.Title)
}

// IsZero reports whether the content carries no copy at all (legacy tasks
// created before the content column existed).
func (c ReachAnnouncementContent) IsZero() bool {
	return c.ZH == (ReachAnnouncementLocale{}) && c.EN == (ReachAnnouncementLocale{})
}

// DefaultAppReleaseAnnouncementContent builds the bilingual default copy for
// an app release, matching the wording previously hardcoded in the send path.
func DefaultAppReleaseAnnouncementContent(version, changelog string) ReachAnnouncementContent {
	version = strings.TrimSpace(version)
	changelog = strings.TrimSpace(changelog)
	return ReachAnnouncementContent{
		ZH: ReachAnnouncementLocale{
			Title:        "Grix " + version + " 新版本已发布 🎉",
			Body:         changelog,
			EmailSubject: "Grix " + version + " 新版本已发布",
			EmailIntro:   "Grix 发布了新版本，立即更新体验最新功能和改进。",
		},
		EN: ReachAnnouncementLocale{
			Title:        "Grix " + version + " is now available 🎉",
			Body:         changelog,
			EmailSubject: "Grix " + version + " is now available",
			EmailIntro:   "A new version of Grix is available. Update now to get the latest features and improvements.",
		},
	}
}

func parseReachAnnouncementContent(task *model.ReachTask) ReachAnnouncementContent {
	var content ReachAnnouncementContent
	if len(task.Content) > 0 {
		_ = json.Unmarshal(task.Content, &content)
	}
	return content
}

// announcementContentForTask returns the task's copy snapshot, rebuilding the
// default copy from the release row for legacy tasks without one.
func announcementContentForTask(task *model.ReachTask) ReachAnnouncementContent {
	content := parseReachAnnouncementContent(task)
	if !content.IsZero() {
		return content
	}
	evt := reachEventFromTask(task)
	return DefaultAppReleaseAnnouncementContent(evt.Version, evt.Changelog)
}

// UpdateReachAnnouncementContent replaces the editable copy of a draft
// announcement task. Only system-event drafts can be edited — once sending
// starts the snapshot must stay stable.
func UpdateReachAnnouncementContent(taskID int64, content ReachAnnouncementContent) error {
	if strings.TrimSpace(content.ZH.Title) == "" || strings.TrimSpace(content.EN.Title) == "" {
		return errors.New("中英文标题均不能为空")
	}
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return err
	}
	res := store.DB.Model(&model.ReachTask{}).
		Where("id = ? AND kind = ? AND status = ?", taskID, model.ReachKindSystemEvent, model.ReachStatusDraft).
		Updates(map[string]any{
			// JSONB 列需要 string，[]byte 会被 pgx 简单协议当 bytea 处理（SQLSTATE 22P02）
			"content":    string(contentJSON),
			"updated_at": time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("任务不存在或不是待发送状态")
	}
	return nil
}

// SendReachAnnouncement fires the fan-out of a draft announcement task. The
// draft→sending flip is guarded by the status predicate so a double click (or
// two admins) starts the fan-out exactly once.
func SendReachAnnouncement(ctx context.Context, taskID int64) error {
	var task model.ReachTask
	if err := store.DB.Where("id = ? AND kind = ?", taskID, model.ReachKindSystemEvent).
		First(&task).Error; err != nil {
		return fmt.Errorf("任务不存在: %w", err)
	}
	if task.Status != model.ReachStatusDraft {
		return errors.New("任务不是待发送状态")
	}

	settings, err := systemsetting.GetAuthSettings()
	if err != nil {
		return err
	}
	customerUserID := settings.AutoAddCustomerUserID
	if customerUserID <= 0 {
		return errors.New("未配置客服账号，无法发送")
	}

	// 与编辑接口同一口径：任一语言标题为空都拒发，否则该语言用户的站内信
	// 会全部落 failed 且 resume 无法补发（send_log 唯一键幂等）。
	content := announcementContentForTask(&task)
	if strings.TrimSpace(content.ZH.Title) == "" || strings.TrimSpace(content.EN.Title) == "" {
		return errors.New("公告文案不完整（中英文标题均不能为空），请先编辑文案")
	}

	res := store.DB.Model(&model.ReachTask{}).
		Where("id = ? AND status = ?", taskID, model.ReachStatusDraft).
		Updates(map[string]any{"status": model.ReachStatusSending, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("任务不是待发送状态")
	}

	logger.L.Infof("reach: announcement send triggered task=%d", taskID)
	go executeReachTask(ctx, taskID, customerUserID, content)
	return nil
}

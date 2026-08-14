package service

import (
	"fmt"
	"html"
	"strings"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/dm"
)

// SendReachEmail sends a general-purpose HTML email for reach (non-verification).
// It reuses the same AliCloud DirectMail infrastructure as verification codes but
// with caller-provided subject and body. Returns an error when the mail config is
// missing or sending fails.
func SendReachEmail(to, subject, htmlBody string) error {
	if err := validateAliEmailConfig(); err != nil {
		return err
	}
	client, err := dm.NewClientWithAccessKey(
		config.C.AliEmail.RegionID,
		config.C.AliEmail.AccessKeyID,
		config.C.AliEmail.AccessKeySecret,
	)
	if err != nil {
		return fmt.Errorf("init email client: %w", err)
	}
	req := dm.CreateSingleSendMailRequest()
	req.Scheme = "https"
	req.AccountName = config.C.AliEmail.FromAddress
	req.AddressType = "1"
	req.ReplyToAddress = "false"
	req.ToAddress = to
	req.Subject = subject
	req.HtmlBody = htmlBody
	if _, err := client.SingleSendMail(req); err != nil {
		logger.L.Warnf("reach email send failed to=%s subject=%q err=%v", to, subject, err)
		return fmt.Errorf("send email: %w", err)
	}
	return nil
}

// ReachAnnouncementEmailContent builds the localized email subject and HTML
// body from a task's announcement copy snapshot. language should be "en" or
// "zh" (default zh).
func ReachAnnouncementEmailContent(content ReachAnnouncementContent, language string) (subject, body string) {
	locale := content.Locale(language)
	lang := "zh"
	if language == preferredLanguageEN {
		lang = "en"
	}
	// 主题剥掉换行（防 header 注入类脏数据），正文各字段进 HTML 前统一转义：
	// 文案是纯文本语义，管理员可编辑也不该被解析成标签。
	subject = strings.NewReplacer("\r", " ", "\n", " ").Replace(locale.EmailSubjectOrTitle())
	body = appReleaseEmailHTML(
		strings.TrimSpace(locale.Title),
		strings.TrimSpace(locale.EmailIntro),
		strings.TrimSpace(locale.Body),
		lang,
	)
	return subject, body
}

func appReleaseEmailHTML(title, intro, changelog, lang string) string {
	changelogSection := ""
	if changelog != "" {
		changelogLabel := "更新内容"
		if lang == "en" {
			changelogLabel = "What's new"
		}
		changelogSection = fmt.Sprintf(
			`<tr><td style="padding:0 24px 24px"><h3 style="margin:0 0 8px;font-size:14px;color:#555">%s</h3><p style="margin:0;font-size:14px;color:#333;white-space:pre-wrap">%s</p></td></tr>`,
			changelogLabel,
			html.EscapeString(changelog),
		)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;padding:0;background:#f5f5f5;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif">
<table width="100%%" cellpadding="0" cellspacing="0" style="max-width:600px;margin:24px auto;background:#fff;border-radius:8px;overflow:hidden">
<tr><td style="background:#4A90D9;padding:24px;text-align:center"><h1 style="margin:0;font-size:20px;color:#fff">%s</h1></td></tr>
<tr><td style="padding:24px"><p style="margin:0;font-size:14px;color:#333;line-height:1.6">%s</p></td></tr>
%s
<tr><td style="padding:12px 24px 24px;text-align:center;border-top:1px solid #eee"><p style="margin:0;font-size:12px;color:#999">Grix — AI-First Communication</p></td></tr>
</table>
</body></html>`, html.EscapeString(title), html.EscapeString(intro), changelogSection)
}

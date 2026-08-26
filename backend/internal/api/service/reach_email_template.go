package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/dm"
	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/pkg/logger"
)

// 阿里云邮件推送的 SingleSendMail 不接受 TemplateId（只有 BatchSendMail 支持模板，
// 但它要求预先建好收件人列表，无法按单地址即时发）。所以这里走 DescTemplate 把模板
// 正文拉回本地，由服务端替换变量后再用 SingleSendMail 逐个发出。
// 模板正文很少变动，拉取结果缓存 5 分钟，避免每封信一次 OpenAPI 调用。
const aliEmailTemplateCacheTTL = 5 * time.Minute

// ErrAliEmailTemplateNotConfigured 表示未指定可用的模板 ID。
var ErrAliEmailTemplateNotConfigured = errors.New("ali email template not configured")

// AliEmailTemplate 是 DescTemplate 返回里我们真正会用到的字段。
type AliEmailTemplate struct {
	Name    string
	Subject string
	Text    string
}

type cachedAliEmailTemplate struct {
	tpl       AliEmailTemplate
	expiresAt time.Time
}

var (
	aliEmailTemplateMu    sync.Mutex
	aliEmailTemplateCache = map[int]cachedAliEmailTemplate{}
	aliEmailTemplateNow   = time.Now
	// 单测替换点：真实实现走阿里云 DescTemplate。
	descAliEmailTemplate = descAliEmailTemplateAPI
)

// ReachEmailTemplateID 返回当前生效的通知邮件模板 ID（配置项，默认 440876）。
func ReachEmailTemplateID() int { return config.C.AliEmail.ReachTemplateID }

// GetAliEmailTemplate 读取模板正文，命中缓存时不发起 OpenAPI 调用。
func GetAliEmailTemplate(templateID int) (AliEmailTemplate, error) {
	if templateID <= 0 {
		return AliEmailTemplate{}, ErrAliEmailTemplateNotConfigured
	}
	now := aliEmailTemplateNow()

	aliEmailTemplateMu.Lock()
	if hit, ok := aliEmailTemplateCache[templateID]; ok && now.Before(hit.expiresAt) {
		aliEmailTemplateMu.Unlock()
		return hit.tpl, nil
	}
	aliEmailTemplateMu.Unlock()

	tpl, err := descAliEmailTemplate(templateID)
	if err != nil {
		return AliEmailTemplate{}, err
	}

	aliEmailTemplateMu.Lock()
	aliEmailTemplateCache[templateID] = cachedAliEmailTemplate{tpl: tpl, expiresAt: now.Add(aliEmailTemplateCacheTTL)}
	aliEmailTemplateMu.Unlock()
	return tpl, nil
}

// InvalidateAliEmailTemplateCache 清掉模板缓存；塘主在阿里云改完模板后可立即生效。
func InvalidateAliEmailTemplateCache() {
	aliEmailTemplateMu.Lock()
	aliEmailTemplateCache = map[int]cachedAliEmailTemplate{}
	aliEmailTemplateMu.Unlock()
}

func descAliEmailTemplateAPI(templateID int) (AliEmailTemplate, error) {
	if err := validateAliEmailConfig(); err != nil {
		return AliEmailTemplate{}, err
	}
	client, err := dm.NewClientWithAccessKey(
		config.C.AliEmail.RegionID,
		config.C.AliEmail.AccessKeyID,
		config.C.AliEmail.AccessKeySecret,
	)
	if err != nil {
		return AliEmailTemplate{}, fmt.Errorf("init email client: %w", err)
	}
	req := dm.CreateDescTemplateRequest()
	req.Scheme = "https"
	req.TemplateId = requests.NewInteger(templateID)
	resp, err := client.DescTemplate(req)
	if err != nil {
		logger.L.Warnf("desc ali email template failed id=%d err=%v", templateID, err)
		return AliEmailTemplate{}, fmt.Errorf("desc email template %d: %w", templateID, err)
	}
	if resp == nil || strings.TrimSpace(resp.TemplateText) == "" {
		return AliEmailTemplate{}, fmt.Errorf("email template %d has empty body", templateID)
	}
	return AliEmailTemplate{
		Name:    resp.TemplateName,
		Subject: resp.TemplateSubject,
		Text:    resp.TemplateText,
	}, nil
}

// RenderReachEmailTemplate 拉取模板并替换 {key} 变量，返回渲染后的主题与正文。
// 只做字面替换：调用方负责把变量值处理成安全的 HTML 片段。
func RenderReachEmailTemplate(templateID int, vars map[string]string) (subject, body string, err error) {
	tpl, err := GetAliEmailTemplate(templateID)
	if err != nil {
		return "", "", err
	}
	return applyEmailTemplateVars(tpl.Subject, vars), applyEmailTemplateVars(tpl.Text, vars), nil
}

// ResolveReachEmailSubject 决定最终主题：vars["subject"] 非空时覆盖模板主题，
// 并统一剥掉换行（防 header 注入类脏数据）。预览与实发共用，避免两处各写一份。
func ResolveReachEmailSubject(templateSubject string, vars map[string]string) string {
	subject := templateSubject
	if s := strings.TrimSpace(vars["subject"]); s != "" {
		subject = s
	}
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(subject))
}

func applyEmailTemplateVars(text string, vars map[string]string) string {
	if text == "" || len(vars) == 0 {
		return text
	}
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		pairs = append(pairs, "{"+k+"}", v)
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

// SendReachEmailByTemplate 用阿里云已报备模板的正文发一封通知邮件。
// vars 里的 key 对应模板中的 {key} 占位符（当前模板用到 {name} 与 {body}）。
// 主题默认取模板的 TemplateSubject（同样做变量替换）；vars["subject"] 非空时覆盖它，
// 供后台按次编辑标题。
func SendReachEmailByTemplate(templateID int, vars map[string]string, to string) error {
	templateSubject, body, err := RenderReachEmailTemplate(templateID, vars)
	if err != nil {
		return err
	}
	subject := ResolveReachEmailSubject(templateSubject, vars)
	if subject == "" {
		return fmt.Errorf("email template %d has empty subject", templateID)
	}
	// 走与 direct reach 相同的发送入口（可在单测中替换），最终仍是 SingleSendMail。
	return sendDirectReachEmail(to, subject, body)
}

package i18n

import (
	"strings"

	"github.com/gin-gonic/gin"
)

type messagePair struct {
	zh string
	en string
}

var messageCatalog = []messagePair{
	{zh: "参数错误", en: "Invalid parameters"},
	{zh: "生成验证码失败", en: "Failed to generate captcha"},
	{zh: "图形验证码错误或已过期", en: "Captcha is invalid or expired"},
	{zh: "邮箱验证码错误或已过期", en: "Email verification code is invalid or expired"},
	{zh: "邮箱验证码不能为空", en: "Email verification code is required"},
	{zh: "初始化邮件客户端失败", en: "Failed to initialize email client"},
	{zh: "邮件发送失败", en: "Failed to send email"},
	{zh: "该邮箱已被注册", en: "This email is already registered"},
	{zh: "用户不存在或密码错误", en: "User does not exist or password is incorrect"},
	{zh: "密码错误", en: "Password is incorrect"},
	{zh: "用户不存在", en: "User does not exist"},
	{zh: "用户邮箱不存在", en: "User email does not exist"},
	{zh: "用户已被禁用", en: "User is disabled"},
	{zh: "数据库未初始化", en: "Database is not initialized"},
	{zh: "密码加密失败", en: "Failed to encrypt password"},
	{zh: "新密码不能为空", en: "New password is required"},
	{zh: "登录态初始化失败", en: "Failed to initialize login session"},
	{zh: "refresh token 无效或已过期", en: "Refresh token is invalid or expired"},
	{zh: "refresh token 已失效，请重新登录", en: "Refresh token is invalidated, please log in again"},
	{zh: "无法从 Google 获取邮箱", en: "Failed to get email from Google"},
	{zh: "对应用户不存在", en: "Linked user does not exist"},
	{zh: "系统已关闭注册", en: "Registration is disabled"},
	{zh: "系统已关闭密码登录", en: "Password login is disabled"},
	{zh: "系统已关闭密码重置", en: "Password reset is disabled"},
	{zh: "系统已关闭 Google 登录", en: "Google login is disabled"},
	{zh: "头像文件不能为空", en: "Avatar file is required"},
	{zh: "头像文件过大（最大10MB）", en: "Avatar file is too large (max 10MB)"},
	{zh: "头像文件格式无效", en: "Avatar image format is invalid"},
	{zh: "OSS 未初始化", en: "OSS is not initialized"},

	{zh: "请求过于频繁，请稍后再试", en: "Too many requests, please try again later"},
	{zh: "验证码发送过于频繁，请5分钟后再试", en: "Verification code was sent too frequently, please try again in 5 minutes"},
	{zh: "验证码服务暂时不可用，请稍后再试", en: "Verification code service is temporarily unavailable, please try again later"},
	{zh: "无权删除该消息", en: "No permission to delete this message"},

	{zh: "缺少或无效的认证请求头", en: "Missing or invalid authorization header"},
	{zh: "Token 无效或已过期", en: "Token is invalid or expired"},
	{zh: "API Key 为空", en: "API key is empty"},
	{zh: "API Key 无效", en: "API key is invalid"},
	{zh: "Agent 不是 API 提供方", en: "Agent is not an API provider"},
	{zh: "Agent 已禁用", en: "Agent is disabled"},

	// errcode.go 中文消息对照
	{zh: "未授权或 Access Token 已过期", en: "Unauthorized or access token expired"},
	{zh: "Refresh Token 已过期或被吊销", en: "Refresh token expired or revoked"},
	{zh: "请求参数验证错误", en: "Request parameter validation error"},
	{zh: "资源不存在", en: "Resource not found"},
	{zh: "内容违规", en: "Content violation"},
	{zh: "服务端内部异常", en: "Internal server error"},
	{zh: "Agent 不存在", en: "Agent not found"},
	{zh: "同名 Agent 已存在", en: "Agent with the same name already exists"},
	{zh: "无权操作该 Agent", en: "No permission to operate this agent"},
	{zh: "Agent 数量超出限制", en: "Agent count exceeds limit"},
	{zh: "无效的 Provider 类型", en: "Invalid provider type"},
	{zh: "本地端点地址不合法", en: "Local endpoint address is invalid"},
	{zh: "Context 文件超过 64KB 限制", en: "Context file exceeds 64KB limit"},

	// 自定义技能同步 errors（errcode.go 27001-27009）
	{zh: "技能名称不能为空", en: "Skill name is required"},
	{zh: "技能名称过长（上限 100 字符）", en: "Skill name is too long (max 100 characters)"},
	{zh: "同名技能已存在", en: "A skill with the same name already exists"},
	{zh: "技能不存在", en: "Skill not found"},
	{zh: "技能内容不能为空", en: "Skill content is required"},
	{zh: "技能内容超过上限", en: "Skill content exceeds the size limit"},
	{zh: "系统内置技能只读", en: "Built-in skills are read-only"},
	{zh: "技能名称含非法字符（不能包含路径分隔符、..、前导点或控制字符）", en: "Skill name contains invalid characters (path separators, .., leading dots, or control characters are not allowed)"},
	{zh: "grix- 前缀技能为平台保留，不可上传", en: "Skills with the grix- prefix are reserved by the platform and cannot be uploaded"},

	// Agent 自定义斜杠命令 errors（errcode.go 20015-20019）
	{zh: "命令名不合法（需以 / 开头，只能用小写字母、数字和 _ : -，最长 32 位）", en: "Invalid command name (must start with /, use lowercase letters, digits, _ : -, max 32 characters)"},
	{zh: "命令说明超过 200 字", en: "Command description exceeds 200 characters"},
	{zh: "同名命令已存在", en: "A command with the same name already exists"},
	{zh: "自定义命令数量超出上限（最多 50 条）", en: "Custom command limit reached (max 50)"},
	{zh: "自定义命令不存在", en: "Custom command not found"},
}

var messageLookup = buildLookup()

func buildLookup() map[string]messagePair {
	lookup := make(map[string]messagePair, len(messageCatalog)*2)
	for _, item := range messageCatalog {
		zh := strings.TrimSpace(item.zh)
		en := strings.TrimSpace(item.en)
		if zh != "" {
			lookup[zh] = item
		}
		if en != "" {
			lookup[en] = item
		}
	}
	return lookup
}

// RequestLanguage 从请求头解析语言，优先 X-App-Locale，其次 Accept-Language。
// 返回 "zh" 或 "en"（其他语言 fallback 到 "en"）。
func RequestLanguage(c *gin.Context) string {
	if c == nil {
		return "zh"
	}
	locale := strings.TrimSpace(c.GetHeader("X-App-Locale"))
	if locale == "" {
		locale = strings.TrimSpace(c.GetHeader("Accept-Language"))
	}
	return normalizeLanguage(locale)
}

// appLanguages are the languages the Flutter app ships translations for.
// Keep in sync with frontend/assets/i18n/*.json.
var appLanguages = map[string]bool{
	"zh": true, "en": true, "ja": true, "ko": true, "de": true,
	"fr": true, "es": true, "pt": true, "ru": true, "ar": true, "hi": true,
}

// RequestAppLanguage 从请求头解析 App 级语言（X-App-Locale 优先，其次
// Accept-Language），返回 App 支持的 11 种语言代码之一；未匹配 fallback "en"。
// 与 RequestLanguage 的区别：后者面向后端 zh/en 双语报错文案，这里面向
// 需要跟随 App 界面语言的富文本内容（如 agent 接入向导目录）。
func RequestAppLanguage(c *gin.Context) string {
	if c == nil {
		return "zh"
	}
	locale := strings.TrimSpace(c.GetHeader("X-App-Locale"))
	if locale == "" {
		locale = strings.TrimSpace(c.GetHeader("Accept-Language"))
	}
	return normalizeAppLanguage(locale)
}

func normalizeAppLanguage(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	if lower == "" {
		return "zh"
	}
	first := strings.FieldsFunc(lower, func(r rune) bool {
		return r == ',' || r == ';'
	})
	if len(first) > 0 {
		lower = strings.TrimSpace(first[0])
	}
	// zh-CN / ja-JP / pt_BR 等取语言子标签
	lang := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '-' || r == '_'
	})
	if len(lang) > 0 {
		lower = strings.TrimSpace(lang[0])
	}
	if appLanguages[lower] {
		return lower
	}
	return "en"
}

func LocalizeMessage(msg, lang string) string {
	normalizedMsg := strings.TrimSpace(msg)
	if normalizedMsg == "" {
		return msg
	}

	normalizedLang := normalizeLanguage(lang)
	if pair, ok := messageLookup[normalizedMsg]; ok {
		if normalizedLang == "en" {
			return pair.en
		}
		return pair.zh
	}

	if strings.HasPrefix(normalizedMsg, "注册失败:") {
		if normalizedLang == "en" {
			suffix := strings.TrimSpace(strings.TrimPrefix(normalizedMsg, "注册失败:"))
			if suffix == "" {
				return "Registration failed"
			}
			return "Registration failed: " + suffix
		}
		return normalizedMsg
	}

	if strings.HasPrefix(strings.ToLower(normalizedMsg), "registration failed:") {
		if normalizedLang == "zh" {
			suffix := strings.TrimSpace(normalizedMsg[len("registration failed:"):])
			if suffix == "" {
				return "注册失败"
			}
			return "注册失败: " + suffix
		}
		return normalizedMsg
	}

	return normalizedMsg
}

// normalizeLanguage 将任意 locale 字符串归一化为 "zh" 或 "en"。
// 仅中文返回 "zh"，其余所有语言（ja/ko/de/fr/es/pt/ru/ar/hi 等）均 fallback 到 "en"。
func normalizeLanguage(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	if lower == "" {
		return "zh"
	}
	// Accept-Language 可能包含多个值，取第一个
	first := strings.FieldsFunc(lower, func(r rune) bool {
		return r == ',' || r == ';'
	})
	if len(first) > 0 {
		lower = strings.TrimSpace(first[0])
	}
	// 中文：zh, zh-CN, zh-TW, zh_CN 等
	if strings.HasPrefix(lower, "zh") {
		return "zh"
	}
	// 其余所有语言 fallback 到英文
	return "en"
}

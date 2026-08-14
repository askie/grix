// Package locale 提供访客/通话场景下的语言归一化与多语言文案选取。
// 语言集合与前端 Flutter i18n（lib/app/locale/locale_service.dart）保持一致。
package locale

import "strings"

// Default 是集合内所有归一化/选取逻辑的兜底语言。
const Default = "en_US"

// Supported 是当前支持的语言集合，顺序与前端设置页展示顺序一致。
var Supported = []string{
	"en_US", "zh_CN", "ja_JP", "ko_KR", "de_DE", "fr_FR", "es_ES", "pt_BR", "ru_RU", "ar", "hi_IN",
}

// Normalize 把任意 BCP-47 风格的语言字符串（如 "zh-CN"、"zh"、"en-GB"）
// 归一化为 Supported 中的一员；无法识别时回退 Default。
func Normalize(raw string) string {
	loc, _ := Match(raw)
	return loc
}

// Match 归一化 raw 并报告是否为「真命中」：exact/前缀匹配到某个 Supported
// 语言时 ok=true；raw 为空或语言不在集合内、只能兜底到 Default 时 ok=false。
// 用于需要区分"用户确实选了这门语言"与"识别不了只好回退"的场景（如按 key 落库时丢弃未知 key）。
func Match(raw string) (loc string, ok bool) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return Default, false
	}
	v = strings.ReplaceAll(v, "-", "_")
	for _, s := range Supported {
		if strings.EqualFold(s, v) {
			return s, true
		}
	}
	lang := strings.ToLower(strings.SplitN(v, "_", 2)[0])
	for _, s := range Supported {
		sLang := strings.ToLower(strings.SplitN(s, "_", 2)[0])
		if sLang == lang {
			return s, true
		}
	}
	return Default, false
}

// Pick 按 loc 从多语言文案 map 中选取文案：精确命中 > Default 兜底 > 集合内任意非空值 > 空串。
func Pick(m map[string]string, loc string) string {
	if v := strings.TrimSpace(m[loc]); v != "" {
		return v
	}
	if v := strings.TrimSpace(m[Default]); v != "" {
		return v
	}
	for _, s := range Supported {
		if v := strings.TrimSpace(m[s]); v != "" {
			return v
		}
	}
	return ""
}

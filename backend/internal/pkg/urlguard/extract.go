package urlguard

import (
	"regexp"
	"strings"
	"unicode"
)

// urlPattern 匹配 http/https 显式链接，以及裸域名形式（如 evil.com/path）。
// 故意收得保守：仅 ASCII letters/digits/hyphen 组成的域名段；IDN 经规范化后再校验。
var urlPattern = regexp.MustCompile(
	`(?i)\b(?:https?://)?` + // 可选 scheme
		`(?:[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?\.)+` + // 至少一个二级标签
		`[a-z]{2,24}` + // TLD
		`(?::\d{1,5})?` + // 可选端口
		// 可选路径：严格 RFC 3986 字符集，避免吞掉中文字符 / markdown 括号
		`(?:/[A-Za-z0-9\-._~%!$&'*+,;=:@/?#\[\]]*)?`)

// Extract 提取文本中所有候选 URL（去重，保持出现顺序）。
func Extract(text string) []string {
	if text == "" {
		return nil
	}
	raw := urlPattern.FindAllString(text, -1)
	if len(raw) == 0 {
		return nil
	}
	var out []string
	seen := make(map[string]struct{}, len(raw))
	for _, m := range raw {
		m = trimURLBoundary(m)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

// trimURLBoundary 去掉 URL 末尾的标点（含 ASCII 与中日韩全角标点）。
func trimURLBoundary(s string) string {
	return strings.TrimRightFunc(s, func(r rune) bool {
		switch r {
		case '.', ',', ';', ':', '!', '?', ')', ']', '}', '>', '\'', '"', '`':
			return true
		}
		return unicode.Is(unicode.P, r) || unicode.Is(unicode.S, r)
	})
}

// Package phonemask 提供手机号脱敏的单一真源，供模型 JSON 出站与日志统一调用。
package phonemask

import (
	"strings"
	"unicode/utf8"
)

// Mask 脱敏手机号。
// 规则：保留前 6 后 4（含 + 号与国家码前缀），中间被遮区用 4 个星号替换；空串原样返回。
// 必须保证前缀终点(6)与后缀起点(n-4)之间至少留 1 个被遮字符，即 n ≥ 11；否则前后缀区间
// 重叠/相接，星号没有真正替换掉任何字符，真实号会原样泄露——此时整段星号化，绝不下发真实号。
// 合法手机号最短 8 字符（+ 加 7 位，见 identity e164 校验），故 8~10 位会走整段星号化分支。
//
//	+8613800138000 → +86138****8000
//	+15551234567   → +15551****4567
//	+44712345678   → +44712****5678
//	+1234567       → ********（短号无法留出被遮间隙，整段星号化）
func Mask(phone string) string {
	s := strings.TrimSpace(phone)
	if s == "" {
		return ""
	}
	n := utf8.RuneCountInString(s)
	if n < 11 {
		return strings.Repeat("*", n)
	}
	runes := []rune(s)
	return string(runes[:6]) + "****" + string(runes[n-4:])
}

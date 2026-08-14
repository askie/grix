package urlguard

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatcher_EvasionResistant(t *testing.T) {
	// 只有一条规则 evil.com / malicious，下列变形应全部命中。
	m := Compile([]Rule{{Kind: RuleExactDomain, Value: "evil.com", Severity: SeverityMalicious}})
	mustHit := []string{
		"http://evil.com/path",
		"http://EVIL.COM/",
		"http://sub.evil.com/",
		"http://a.b.evil.com/x",
		"http://evil.com./",
		"http://evil.com/%2e%2e/x",
		"evil.com/x",
		"http://good.com.evil.com/", // 子域伪装
	}
	for _, u := range mustHit {
		v := m.Match(u)
		assert.True(t, v.Hit, "应命中: %s", u)
		assert.Equal(t, SeverityMalicious, v.Severity, u)
	}
	// 不应误杀 —— goodevil.com 与 evil.com 是无关域名
	assert.False(t, m.Match("http://goodevil.com/").Hit, "不应误杀 goodevil.com")
	assert.False(t, m.Match("http://notevil.org/evil.com").Hit, "不应误杀 path 中的 evil.com")
}

func TestMatcher_IPRule(t *testing.T) {
	m := Compile([]Rule{{Kind: RuleExactDomain, Value: "1.2.3.4", Severity: SeverityMalicious}})
	// IP 多进制写法都应命中
	for _, u := range []string{
		"http://1.2.3.4/x",
		"http://16909060/x",   // 1.2.3.4 = 0x01020304 = 16909060
		"http://0x01020304/x",
	} {
		assert.True(t, m.Match(u).Hit, u)
	}
	// 不同 IP 不应命中
	assert.False(t, m.Match("http://5.6.7.8/x").Hit)
}

func TestMatcher_Wildcard(t *testing.T) {
	m := Compile([]Rule{{Kind: RuleWildcard, Value: "*.evil.com", Severity: SeverityMalicious}})
	assert.True(t, m.Match("http://sub.evil.com/").Hit)
	assert.True(t, m.Match("http://evil.com/").Hit, "wildcard 应等价于精确域，根域也命中")
}

func TestMatcher_Regex(t *testing.T) {
	m := Compile([]Rule{{Kind: RuleRegex, Value: `^bad-site\.com/.*phish.*`, Severity: SeverityMalicious}})
	assert.True(t, m.Match("http://bad-site.com/login/phishpage").Hit)
	assert.False(t, m.Match("http://bad-site.com/normal").Hit)
}

func TestMatcher_Keyword(t *testing.T) {
	m := Compile([]Rule{{Kind: RuleKeyword, Value: "phish-kit", Severity: SeveritySuspicious}})
	assert.True(t, m.Match("http://x.com/phish-kit/login").Hit)
}

func TestMatcher_PrefersMaliciousOverSuspicious(t *testing.T) {
	m := Compile([]Rule{
		{Kind: RuleKeyword, Value: "promo", Severity: SeveritySuspicious},
		{Kind: RuleExactDomain, Value: "evil.com", Severity: SeverityMalicious},
	})
	v := m.MatchText("看 http://evil.com/promo 和 http://good.com/promo")
	assert.True(t, v.Hit)
	assert.Equal(t, SeverityMalicious, v.Severity)
}

func TestMatcher_MatchTextNoHit(t *testing.T) {
	m := Compile([]Rule{{Kind: RuleExactDomain, Value: "evil.com", Severity: SeverityMalicious}})
	v := m.MatchText("看 https://good.com/path 没毛病")
	assert.False(t, v.Hit)
}

func TestMatcher_NilSafe(t *testing.T) {
	var m *Matcher
	assert.False(t, m.Match("http://evil.com/").Hit)
}

func TestMatcher_InvalidRegexSkipped(t *testing.T) {
	m := Compile([]Rule{{Kind: RuleRegex, Value: "[invalid", Severity: SeverityMalicious}})
	// 不应 panic；regex 编译失败被跳过
	assert.False(t, m.Match("http://x.com/").Hit)
}

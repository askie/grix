package service

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/pkg/urlguard"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/stretchr/testify/assert"
)

// 自家域名白名单：精确等于 host 或是 own 的真子域才直通；
// 不会被 `<anything>.<own>` 以外的伪造形态误判。
func TestCheckOneLink_OwnDomainWhitelist(t *testing.T) {
	settings := systemsetting.LinkSafetySettings{
		Enabled:            true,
		OwnDomainWhitelist: []string{"grix.dhf.pub"},
	}
	// 空 matcher：白名单未命中时一律 clean，所以只需要白名单逻辑本身的对错。
	matcher := urlguard.Compile(nil)

	cases := []struct {
		name string
		url  string
		want string // expected verdict
	}{
		{"exact host", "http://grix.dhf.pub/x", "clean"},
		{"real sub", "http://sub.grix.dhf.pub/x", "clean"},
		{"deeper sub", "http://a.b.grix.dhf.pub/x", "clean"},

		// 关键反例：必须不命中白名单（否则就是高优安全漏洞）
		{"trailing fake", "http://grix.dhf.pub.attacker.com/x", "clean"},
		// ↑ 该 URL 不会命中白名单（host 不以 .grix.dhf.pub 结尾，host 也不等于）；
		//   matcher 也未配置规则 → 自然 clean。
		//   关键在于："其 verdict 是 clean 不是因为白名单"。
		{"unrelated host", "http://example.com/x", "clean"},
		{"contains as substring", "http://grixxdhfxpub.attacker.com/x", "clean"},
	}
	for _, c := range cases {
		v := checkOneLink(context.Background(), c.url, settings, matcher, "test")
		assert.Equal(t, c.want, v.Verdict, c.name)
	}
}

// 关键：白名单 + 黑名单同时存在时，攻击者用伪造形态不应通过白名单旁路黑名单。
func TestCheckOneLink_WhitelistDoesNotBypassBlacklist(t *testing.T) {
	settings := systemsetting.LinkSafetySettings{
		Enabled:            true,
		OwnDomainWhitelist: []string{"grix.dhf.pub"},
	}
	matcher := urlguard.Compile([]urlguard.Rule{
		{Kind: urlguard.RuleExactDomain, Value: "attacker.com", Severity: urlguard.SeverityMalicious},
	})

	// host 为 grix.dhf.pub.attacker.com：白名单不应命中（修复前会命中并旁路黑名单）
	// 黑名单 attacker.com 应命中（host 后缀展开 attacker.com）
	v := checkOneLink(context.Background(), "http://grix.dhf.pub.attacker.com/path", settings, matcher, "test")
	assert.Equal(t, "malicious", v.Verdict,
		"白名单不应放行 *.attacker.com 这类伪造形态；必须由黑名单接管")
}

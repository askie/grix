package systemsetting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// normalizeOwnDomains 必须：
// - 拒绝少于 2 段的条目（防 com / org 被当全 TLD 直通）
// - 去重、小写化、去前缀 *. 与首尾点
// - IDN 转 punycode
func TestNormalizeOwnDomains_RejectsSingleSegment(t *testing.T) {
	in := []string{
		"grix.dhf.pub",
		"  Example.COM ", // 大小写 + 空白
		"*.sub.example.org",
		"com",     // 必须被丢
		"org",     // 必须被丢
		"localhost", // 单段，必须被丢
		"",
		"example.com", // 重复（小写后）
		".trim..dots..", // 多余点
	}
	out := normalizeOwnDomains(in)

	assert.Contains(t, out, "grix.dhf.pub")
	assert.Contains(t, out, "example.com")
	assert.Contains(t, out, "sub.example.org")
	assert.Contains(t, out, "trim.dots")

	for _, bad := range []string{"com", "org", "localhost", ""} {
		assert.NotContains(t, out, bad, "应拒绝单段条目: %q", bad)
	}

	// 去重：example.com 不应出现两次
	count := 0
	for _, v := range out {
		if v == "example.com" {
			count++
		}
	}
	assert.Equal(t, 1, count, "重复条目应被去重")
}

func TestNormalizeOwnDomains_IDNToPunycode(t *testing.T) {
	// 西里尔 а（U+0430）应转 punycode
	out := normalizeOwnDomains([]string{"аpple.com"})
	if assert.Len(t, out, 1) {
		assert.True(t, len(out[0]) > 4 && out[0][:4] == "xn--",
			"IDN 应转 punycode, got: %s", out[0])
	}
}

func TestNormalizeOwnDomains_EmptyInputReturnsNil(t *testing.T) {
	assert.Nil(t, normalizeOwnDomains(nil))
	assert.Nil(t, normalizeOwnDomains([]string{"", " ", "com", "org"}))
}

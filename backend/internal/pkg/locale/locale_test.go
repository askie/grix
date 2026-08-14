package locale

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":      Default,
		"en-US": "en_US",
		"en_US": "en_US",
		"zh-CN": "zh_CN",
		"zh":    "zh_CN",
		"zh-TW": "zh_CN", // 无繁体资源，语言前缀命中简体
		"de":    "de_DE",
		"ar":    "ar",
		"ar-SA": "ar",
		"xx-Yy": Default,
		"ja":    "ja_JP",
		"pt-BR": "pt_BR",
		"pt":    "pt_BR",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatch(t *testing.T) {
	// 真命中：exact / 前缀命中集合内语言，ok=true。
	hits := map[string]string{
		"en-US": "en_US",
		"en":    "en_US",
		"zh-CN": "zh_CN",
		"zh":    "zh_CN",
		"pt":    "pt_BR",
	}
	for in, want := range hits {
		if got, ok := Match(in); !ok || got != want {
			t.Errorf("Match(%q) = (%q,%v), want (%q,true)", in, got, ok, want)
		}
	}
	// 兜底：空串 / 集合外语言，ok=false 且返回 Default。
	for _, in := range []string{"", "xx", "xx-YY", "zzz"} {
		if got, ok := Match(in); ok || got != Default {
			t.Errorf("Match(%q) = (%q,%v), want (%q,false)", in, got, ok, Default)
		}
	}
}

func TestPick(t *testing.T) {
	m := map[string]string{
		"en_US": "Hello",
		"zh_CN": "你好",
	}
	if got := Pick(m, "zh_CN"); got != "你好" {
		t.Errorf("exact match: got %q", got)
	}
	if got := Pick(m, "ja_JP"); got != "Hello" {
		t.Errorf("fallback to default: got %q", got)
	}
	if got := Pick(map[string]string{"fr_FR": "Bonjour"}, "ja_JP"); got != "Bonjour" {
		t.Errorf("fallback to any non-empty: got %q", got)
	}
	if got := Pick(nil, "en_US"); got != "" {
		t.Errorf("empty map: got %q", got)
	}
}

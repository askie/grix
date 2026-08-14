package phonemask

import "testing"

func TestMask(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"only-spaces-trim-to-empty", "   ", ""},
		{"under-8-runes-all-star", "+861", "****"},
		{"exact-7-runes-all-star", "+123456", "*******"},
		// 8~10 位：前缀(6)与后缀(4)区间重叠，无法留出被遮间隙，必须整段星号化，
		// 否则真实号会原样泄露（曾经的漏洞区间）。
		{"len-8-all-star", "+1234567", "********"},
		{"len-9-all-star", "+12345678", "*********"},
		{"len-10-all-star", "+123456789", "**********"},
		// 11 位起：中间至少留出 1 位被遮，开始走前 6 后 4。
		{"len-11-first-masked", "+1234567890", "+12345****7890"},
		{"cn-13", "+8613800138000", "+86138****8000"},
		{"us-12", "+15551234567", "+15551****4567"},
		{"uk-13", "+447123456789", "+44712****6789"},
		{"with-surrounding-space", "  +8613800138000  ", "+86138****8000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Mask(c.in)
			if got != c.want {
				t.Fatalf("Mask(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

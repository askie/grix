package identity

import "testing"

func TestPhoneMask(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"+8613800138000", "+86138****8000"},
		{"+15551234567", "+15551****4567"},
		{"", ""},
		{"123", "***"},
		{"  +86138001  ", "*********"}, // trim 后 9 字符：短号无法留出被遮间隙，整段星号化防泄露
	}
	for _, c := range cases {
		got := PhoneMask(c.in)
		if got != c.want {
			t.Errorf("PhoneMask(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

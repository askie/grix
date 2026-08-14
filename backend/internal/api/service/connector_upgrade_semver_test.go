package service

import "testing"

func TestIsNewer_Semver(t *testing.T) {
	cases := []struct {
		name     string
		a, b     string
		expected bool
	}{
		{"patch_greater", "2.9.1", "2.9.0", true},
		{"minor_greater", "2.10.0", "2.9.99", true},          // 关键：跨进位字符串比较错，semver 对
		{"major_greater", "3.0.0", "2.99.99", true},
		{"equal", "2.9.0", "2.9.0", false},
		{"older_minor", "2.9.0", "2.10.0", false},            // 老路径下这里返回 true（错），新路径必须为 false
		{"older_patch", "2.10.0", "2.10.1", false},
		{"v_prefix_greater", "v2.10.0", "2.9.5", true},
		{"prerelease_treated_as_base", "2.10.0-beta", "2.9.0", true},
		{"short_form_filled_zero", "2.10", "2.9.99", true},
		{"non_numeric_falls_back_string", "abc", "abz", false}, // 退化为字符串比较 abc<abz
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isNewer(tc.a, tc.b)
			if got != tc.expected {
				t.Fatalf("isNewer(%q,%q)=%v want %v", tc.a, tc.b, got, tc.expected)
			}
		})
	}
}

package identity

import (
	"testing"
)

func TestSanitizePhoneE164(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"+8613800138000", "+8613800138000", false},
		{"+86 138 0013 8000", "+8613800138000", false},
		{"+86-138-0013-8000", "+8613800138000", false},
		{"+86(0)13800138000", "+8613800138000", false},
		{"+1 (555) 123-4567", "+15551234567", false},
		{"+44.7712.345.678", "+447712345678", false},
		{"＋8613800138000", "+8613800138000", false},
		{"", "", true},
		{"13800138000", "", true},     // no plus
		{"+86abc138", "", true},       // letters
		{"+8612", "", true},            // too short
		{"+1234567890123456", "", true}, // too long
	}
	for _, c := range cases {
		got, err := SanitizePhoneE164(c.in)
		if c.err {
			if err == nil {
				t.Errorf("SanitizePhoneE164(%q) expected err, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("SanitizePhoneE164(%q) unexpected err %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("SanitizePhoneE164(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseCountryCode(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"+8613800138000", "+86", false},
		{"+15551234567", "+1", false},
		{"+447712345678", "+44", false},
		{"+85291234567", "+852", false},
		{"+9999999999", "", true}, // unknown prefix
	}
	for _, c := range cases {
		got, err := ParseCountryCode(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseCountryCode(%q) expected err, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseCountryCode(%q) unexpected err %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseCountryCode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRegionForCountry(t *testing.T) {
	if RegionForCountry("+86") != "cn" {
		t.Errorf("+86 should be cn")
	}
	if RegionForCountry("+1") != "global" {
		t.Errorf("+1 should be global")
	}
	if RegionForCountry("+44") != "global" {
		t.Errorf("+44 should be global")
	}
}

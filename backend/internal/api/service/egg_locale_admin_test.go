package service

import "testing"

func TestNormalizeAdminEggLocale(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		expected string
		ok       bool
	}{
		{name: "zh short", input: "zh", expected: "zh-CN", ok: true},
		{name: "zh full", input: "zh-CN", expected: "zh-CN", ok: true},
		{name: "zh underscore", input: "zh_CN", expected: "zh-CN", ok: true},
		{name: "en short", input: "en", expected: "en-US", ok: true},
		{name: "en full", input: "en-US", expected: "en-US", ok: true},
		{name: "en mixed case", input: "EN-us", expected: "en-US", ok: true},
		{name: "unsupported", input: "fr-FR", expected: "", ok: false},
		{name: "empty", input: "", expected: "", ok: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			normalized, ok := NormalizeAdminEggLocale(tc.input)
			if ok != tc.ok {
				t.Fatalf("ok=%v, want %v", ok, tc.ok)
			}
			if normalized != tc.expected {
				t.Fatalf("normalized=%q, want %q", normalized, tc.expected)
			}
		})
	}
}

func TestBuildEggLocaleChain(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "zh locale keeps default fallback",
			input:    "zh-CN",
			expected: []string{"zh-CN", "zh", "en-US", "en"},
		},
		{
			name:     "en locale no zh fallback",
			input:    "en-US",
			expected: []string{"en-US", "en"},
		},
		{
			name:     "unknown locale keeps explicit locale and english fallback",
			input:    "fr-FR",
			expected: []string{"fr-FR", "fr", "en-US", "en"},
		},
		{
			name:     "empty locale falls back to en-US",
			input:    "",
			expected: []string{"en-US", "en"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildEggLocaleChain(tc.input)
			if len(got) != len(tc.expected) {
				t.Fatalf("len(chain)=%d, want %d, chain=%v", len(got), len(tc.expected), got)
			}
			for idx := range tc.expected {
				if got[idx] != tc.expected[idx] {
					t.Fatalf("chain[%d]=%q, want %q, chain=%v", idx, got[idx], tc.expected[idx], got)
				}
			}
		})
	}
}

func TestNormalizeCategoryI18nInputs(t *testing.T) {
	t.Parallel()

	t.Run("normalize locale and trim text", func(t *testing.T) {
		t.Parallel()

		inputs := []EggCategoryI18nInput{
			{Locale: "zh", Name: " 人格 ", Description: " 介绍 "},
			{Locale: "en-us", Name: "Persona", Description: " Profile "},
		}

		normalized, ec := normalizeCategoryI18nInputs(inputs)
		if ec != nil {
			t.Fatalf("unexpected errcode: %+v", ec)
		}
		if len(normalized) != 2 {
			t.Fatalf("len(normalized)=%d, want 2", len(normalized))
		}
		if normalized[0].Locale != "zh-CN" || normalized[0].Name != "人格" || normalized[0].Description != "介绍" {
			t.Fatalf("normalized[0]=%+v", normalized[0])
		}
		if normalized[1].Locale != "en-US" || normalized[1].Name != "Persona" || normalized[1].Description != "Profile" {
			t.Fatalf("normalized[1]=%+v", normalized[1])
		}
	})

	t.Run("reject unsupported locale", func(t *testing.T) {
		t.Parallel()

		_, ec := normalizeCategoryI18nInputs([]EggCategoryI18nInput{
			{Locale: "fr-FR", Name: "Voyage"},
		})
		if ec == nil {
			t.Fatal("expected errcode, got nil")
		}
		if ec.Msg != "分类 i18n 语言仅支持 zh-CN / en-US" {
			t.Fatalf("msg=%q", ec.Msg)
		}
	})

	t.Run("reject duplicate locale", func(t *testing.T) {
		t.Parallel()

		_, ec := normalizeCategoryI18nInputs([]EggCategoryI18nInput{
			{Locale: "zh-CN", Name: "人格"},
			{Locale: "zh", Name: "人设"},
		})
		if ec == nil {
			t.Fatal("expected errcode, got nil")
		}
		if ec.Msg != "分类 i18n 语言重复" {
			t.Fatalf("msg=%q", ec.Msg)
		}
	})

	t.Run("reject empty name", func(t *testing.T) {
		t.Parallel()

		_, ec := normalizeCategoryI18nInputs([]EggCategoryI18nInput{
			{Locale: "en-US", Name: "  "},
		})
		if ec == nil {
			t.Fatal("expected errcode, got nil")
		}
		if ec.Msg != "分类 i18n 需要 locale 和 name" {
			t.Fatalf("msg=%q", ec.Msg)
		}
	})
}

func TestNormalizeVersionI18nInputs(t *testing.T) {
	t.Parallel()

	t.Run("normalize locale and trim desc", func(t *testing.T) {
		t.Parallel()

		normalized, ec := normalizeVersionI18nInputs([]EggVersionI18nInput{
			{Locale: "zh", VersionDesc: " 描述 "},
			{Locale: "en-US", VersionDesc: " Description "},
		})
		if ec != nil {
			t.Fatalf("unexpected errcode: %+v", ec)
		}
		if len(normalized) != 2 {
			t.Fatalf("len(normalized)=%d, want 2", len(normalized))
		}
		if normalized[0].Locale != "zh-CN" || normalized[0].VersionDesc != "描述" {
			t.Fatalf("normalized[0]=%+v", normalized[0])
		}
		if normalized[1].Locale != "en-US" || normalized[1].VersionDesc != "Description" {
			t.Fatalf("normalized[1]=%+v", normalized[1])
		}
	})

	t.Run("reject unsupported locale", func(t *testing.T) {
		t.Parallel()

		_, ec := normalizeVersionI18nInputs([]EggVersionI18nInput{
			{Locale: "fr-FR", VersionDesc: "desc"},
		})
		if ec == nil {
			t.Fatal("expected errcode, got nil")
		}
		if ec.Msg != "版本 i18n 语言仅支持 zh-CN / en-US" {
			t.Fatalf("msg=%q", ec.Msg)
		}
	})

	t.Run("reject duplicate locale", func(t *testing.T) {
		t.Parallel()

		_, ec := normalizeVersionI18nInputs([]EggVersionI18nInput{
			{Locale: "zh-CN", VersionDesc: "a"},
			{Locale: "zh", VersionDesc: "b"},
		})
		if ec == nil {
			t.Fatal("expected errcode, got nil")
		}
		if ec.Msg != "版本 i18n 语言重复" {
			t.Fatalf("msg=%q", ec.Msg)
		}
	})
}

func TestNormalizeEggI18nInputs(t *testing.T) {
	t.Parallel()

	t.Run("normalize locale and trim text", func(t *testing.T) {
		t.Parallel()

		normalized, ec := normalizeEggI18nInputs([]EggI18nInput{
			{Locale: "zh", Name: " 人格助手 ", Description: " 简介 ", Vibe: " 友好 "},
			{Locale: "en-US", Name: "Assistant", Description: " Intro ", Vibe: " Friendly "},
		})
		if ec != nil {
			t.Fatalf("unexpected errcode: %+v", ec)
		}
		if len(normalized) != 2 {
			t.Fatalf("len(normalized)=%d, want 2", len(normalized))
		}
		if normalized[0].Locale != "zh-CN" || normalized[0].Name != "人格助手" || normalized[0].Description != "简介" || normalized[0].Vibe != "友好" {
			t.Fatalf("normalized[0]=%+v", normalized[0])
		}
		if normalized[1].Locale != "en-US" || normalized[1].Name != "Assistant" || normalized[1].Description != "Intro" || normalized[1].Vibe != "Friendly" {
			t.Fatalf("normalized[1]=%+v", normalized[1])
		}
	})

	t.Run("reject unsupported locale", func(t *testing.T) {
		t.Parallel()

		_, ec := normalizeEggI18nInputs([]EggI18nInput{
			{Locale: "fr-FR", Name: "助手"},
		})
		if ec == nil {
			t.Fatal("expected errcode, got nil")
		}
		if ec.Msg != "egg i18n 语言仅支持 zh-CN / en-US" {
			t.Fatalf("msg=%q", ec.Msg)
		}
	})

	t.Run("reject duplicate locale", func(t *testing.T) {
		t.Parallel()

		_, ec := normalizeEggI18nInputs([]EggI18nInput{
			{Locale: "zh-CN", Name: "助手"},
			{Locale: "zh", Name: "助理"},
		})
		if ec == nil {
			t.Fatal("expected errcode, got nil")
		}
		if ec.Msg != "egg i18n 语言重复" {
			t.Fatalf("msg=%q", ec.Msg)
		}
	})
}

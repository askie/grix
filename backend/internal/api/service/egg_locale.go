package service

import "strings"

var adminEggSupportedLocales = []string{"zh-CN", "en-US"}

func AdminEggSupportedLocales() []string {
	return append([]string(nil), adminEggSupportedLocales...)
}

func NormalizeAdminEggLocale(raw string) (string, bool) {
	locale := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "_", "-"))
	switch locale {
	case "zh", "zh-cn":
		return "zh-CN", true
	case "en", "en-us":
		return "en-US", true
	default:
		return "", false
	}
}

func normalizeEggLocale(raw string) string {
	locale, ok := normalizeEggLocaleToken(raw)
	if !ok {
		return "en-US"
	}
	return locale
}

func buildEggLocaleChain(raw string) []string {
	locale := normalizeEggLocale(raw)
	chain := make([]string, 0, 4)
	appendUnique := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range chain {
			if strings.EqualFold(existing, value) {
				return
			}
		}
		chain = append(chain, value)
	}

	appendUnique(locale)
	appendUnique(eggLocaleBase(locale))
	appendUnique("en-US")
	appendUnique("en")
	return chain
}

func normalizeEggLocaleToken(raw string) (string, bool) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", false
	}
	if idx := strings.IndexAny(token, ",;"); idx >= 0 {
		token = strings.TrimSpace(token[:idx])
	}
	token = strings.ReplaceAll(token, "_", "-")
	if token == "" {
		return "", false
	}

	parts := strings.Split(token, "-")
	normalized := make([]string, 0, len(parts))
	for idx, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", false
		}
		switch {
		case idx == 0:
			normalized = append(normalized, strings.ToLower(part))
		case len(part) == 2 || len(part) == 3:
			normalized = append(normalized, strings.ToUpper(part))
		case len(part) == 4:
			normalized = append(normalized, strings.ToUpper(part[:1])+strings.ToLower(part[1:]))
		default:
			normalized = append(normalized, strings.ToLower(part))
		}
	}
	return strings.Join(normalized, "-"), true
}

func eggLocaleBase(raw string) string {
	locale, ok := normalizeEggLocaleToken(raw)
	if !ok {
		return ""
	}
	if idx := strings.Index(locale, "-"); idx > 0 {
		return locale[:idx]
	}
	return locale
}

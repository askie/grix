package service

import (
	"sort"
	"strings"
	"unicode"
)

func sessionSearchMatchesKeyword(sessionID, title string, keyword sessionSearchKeyword) bool {
	return scoreSessionSearchTitle(title, keyword, 40) > 0 || scoreSessionSearchTitle(sessionID, keyword, 10) > 0
}

func normalizeSessionSearchText(raw string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(raw)), " "))
}

func buildSessionSearchKeyword(keyword string) sessionSearchKeyword {
	lowered := normalizeSessionSearchText(keyword)
	if lowered == "" {
		return sessionSearchKeyword{}
	}

	return sessionSearchKeyword{
		lowered: lowered,
		compact: compactSessionSearchText(lowered),
		tokens:  splitSessionSearchTokens(lowered),
	}
}

func splitSessionSearchTokens(text string) []string {
	rawTokens := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(rawTokens) == 0 {
		return nil
	}

	tokens := make([]string, 0, len(rawTokens))
	seen := make(map[string]struct{}, len(rawTokens))
	for _, token := range rawTokens {
		normalized := strings.TrimSpace(token)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		tokens = append(tokens, normalized)
	}
	return tokens
}

func compactSessionSearchText(text string) string {
	if text == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(text))
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func rankSessionSearchItems(items []SessionSearchItem, keyword sessionSearchKeyword) []SessionSearchItem {
	if len(items) == 0 {
		return items
	}

	ranked := make([]rankedSessionSearchItem, 0, len(items))
	for idx, item := range items {
		score := scoreSessionSearchTitle(item.Title, keyword, 40)
		score = maxInt(score, scoreSessionSearchTitle(item.SessionID, keyword, 10))
		ranked = append(ranked, rankedSessionSearchItem{
			item:        item,
			score:       score,
			originIndex: idx,
		})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].originIndex < ranked[j].originIndex
	})

	result := make([]SessionSearchItem, 0, len(ranked))
	for _, entry := range ranked {
		result = append(result, entry.item)
	}
	return result
}

func scoreSessionSearchTitle(value string, keyword sessionSearchKeyword, fieldBonus int) int {
	normalized := normalizeSessionSearchText(value)
	if normalized == "" || keyword.lowered == "" {
		return 0
	}

	score := 0
	switch {
	case normalized == keyword.lowered:
		score = maxInt(score, 3000+fieldBonus)
	case strings.HasPrefix(normalized, keyword.lowered):
		score = maxInt(score, 2400+fieldBonus)
	case strings.Contains(normalized, keyword.lowered):
		score = maxInt(score, 1800+fieldBonus)
	}

	compact := compactSessionSearchText(normalized)
	if compact != "" && keyword.compact != "" {
		switch {
		case compact == keyword.compact:
			score = maxInt(score, 2900+fieldBonus)
		case strings.HasPrefix(compact, keyword.compact):
			score = maxInt(score, 2300+fieldBonus)
		case strings.Contains(compact, keyword.compact):
			score = maxInt(score, 1700+fieldBonus)
		}
	}

	if len(keyword.tokens) > 1 {
		if containsAllTokens(normalized, keyword.tokens) {
			score = maxInt(score, 2100+fieldBonus)
		}
		if compact != "" && containsAllTokens(compact, keyword.tokens) {
			score = maxInt(score, 2200+fieldBonus)
		}
	}

	return score
}

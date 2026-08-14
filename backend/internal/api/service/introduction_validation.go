package service

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	errIntroductionTooLong        = errors.New("introduction too long")
	errIntroductionInvalidControl = errors.New("introduction contains invalid control chars")
)

func normalizeIntroductionText(raw string, maxRunes int) (string, error) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = strings.TrimSpace(normalized)

	if utf8.RuneCountInString(normalized) > maxRunes {
		return "", errIntroductionTooLong
	}
	for _, r := range normalized {
		if !unicode.IsControl(r) {
			continue
		}
		if r == '\n' || r == '\t' {
			continue
		}
		return "", errIntroductionInvalidControl
	}
	return normalized, nil
}

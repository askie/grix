package textutil

// TruncateRunes truncates a string to at most maxRunes Unicode code points.
// It never returns invalid UTF-8.
func TruncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}

	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}

	return string(runes[:maxRunes])
}

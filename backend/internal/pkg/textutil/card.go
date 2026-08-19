package textutil

import (
	"regexp"
	"strings"
)

// Matches a message whose entire body is one markdown grix://card link.
// Mixed replies that mention a card after real text must remain previewable.
var standaloneGrixCardPattern = regexp.MustCompile(`(?s)^\s*\[.*\]\(grix://card/[^)\s]+\)\s*$`)

// IsStandaloneCardMessage returns true if content is a standalone grix://card
// Markdown link (i.e. the entire message is a single card). These messages
// carry tool/status UI and are not suitable for session preview text.
func IsStandaloneCardMessage(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	return standaloneGrixCardPattern.MatchString(trimmed)
}

// StandaloneCardExcludeSQL is a SQL predicate that is true for rows that are
// NOT a standalone card. [column] must be a trusted identifier, not user input.
func StandaloneCardExcludeSQL(column string, postgres bool) string {
	if postgres {
		return "NOT (trim(" + column + ") ~ '^\\[[\\s\\S]*\\]\\(grix://card/[^)[:space:]]+\\)\\s*$')"
	}
	// SQLite GLOB: starts with '[' and contains '](grix://card/'.
	return "NOT (trim(" + column + ") GLOB '[[]*](grix://card/*)')"
}

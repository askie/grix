package textutil

import "strings"

// IsStandaloneCardMessage returns true if content is a standalone grix://card
// Markdown link (i.e. the entire message is a single card). These messages
// carry tool/status UI and are not suitable for session preview text.
func IsStandaloneCardMessage(content string) bool {
	return strings.Contains(content, "](grix://card/")
}

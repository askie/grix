package textutil

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRunes(t *testing.T) {
	t.Run("no truncation", func(t *testing.T) {
		in := "hello"
		out := TruncateRunes(in, 10)
		if out != in {
			t.Fatalf("unexpected output: got=%q want=%q", out, in)
		}
	})

	t.Run("truncate multibyte safely", func(t *testing.T) {
		in := strings.Repeat("a", 59) + "你"
		out := TruncateRunes(in, 60)

		if !utf8.ValidString(out) {
			t.Fatalf("output must be valid UTF-8, got invalid bytes: %q", out)
		}
		if out != in {
			t.Fatalf("unexpected output: got=%q want=%q", out, in)
		}
	})

	t.Run("max runes <= 0", func(t *testing.T) {
		out := TruncateRunes("abc", 0)
		if out != "" {
			t.Fatalf("unexpected output: got=%q want empty", out)
		}
	})
}

package ws

import "testing"

func TestBuildThinkingCardMarkdown_EncodesContent(t *testing.T) {
	got := buildThinkingCardMarkdown("line 1\nline 2")
	want := "[Thinking](grix://card/thinking?content=line+1%0Aline+2)"
	if got != want {
		t.Fatalf("thinking card markdown mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildThinkingCardMarkdown_EmptyAfterTrim(t *testing.T) {
	got := buildThinkingCardMarkdown("   \n\t  ")
	if got != "" {
		t.Fatalf("expected empty markdown for blank content, got: %s", got)
	}
}

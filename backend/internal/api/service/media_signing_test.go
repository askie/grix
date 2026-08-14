package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const testMediaURL = "https://aibot-1252145388.cos.accelerate.myqcloud.com/media/user/2030840865701756928/1781921062109_image_picker_edited.png"

// fakeSigner appends a per-call counter so the same URL signed twice yields
// different strings — proving the memo (not luck) keeps content and extra equal.
func newCountingSigner() mediaURLSignFunc {
	n := 0
	return func(u string) string {
		n++
		return fmt.Sprintf("%s?sig=%d", u, n)
	}
}

func TestSignMediaURLsInContent_SignsInlineImage(t *testing.T) {
	content := fmt.Sprintf("看这张图\n![image](<%s>)", testMediaURL)
	signer := func(u string) string { return u + "?sig=X" }

	got := signMediaURLsInContent(content, signer)

	want := fmt.Sprintf("看这张图\n![image](<%s?sig=X>)", testMediaURL)
	if got != want {
		t.Fatalf("inline image not signed correctly\n got=%q\nwant=%q", got, want)
	}
	// Angle brackets / parens must survive the replace.
	if !strings.Contains(got, "(<") || !strings.Contains(got, ">)") {
		t.Fatalf("markdown delimiters were clobbered: %q", got)
	}
}

func TestSignMediaURLsInContent_LeavesNonMediaUntouched(t *testing.T) {
	content := "纯文本 https://example.com/page 没有图"
	// Signer that would only ever change media URLs (here: identity, mimicking a
	// non-media URL that resolveMediaObjectKey can't map).
	got := signMediaURLsInContent(content, func(u string) string { return u })
	if got != content {
		t.Fatalf("non-media text was modified\n got=%q\nwant=%q", got, content)
	}
}

func TestSignMediaURLsInContent_Empty(t *testing.T) {
	if got := signMediaURLsInContent("", func(u string) string { return u + "?x" }); got != "" {
		t.Fatalf("empty content should stay empty, got %q", got)
	}
}

// The core guarantee: the same source URL becomes the SAME signed URL in both
// content and extra, so the client can de-dup the inline image against the
// attachment grid even though the signer is non-deterministic.
func TestSignMessageMedia_ContentAndExtraMatch(t *testing.T) {
	content := fmt.Sprintf("![image](<%s>)", testMediaURL)
	extra, _ := json.Marshal(map[string]any{
		"attachments": []any{
			map[string]any{
				"media_url":       testMediaURL,
				"attachment_type": "image",
			},
		},
	})

	signedContent, signedExtra := signMessageMedia(content, json.RawMessage(extra), newCountingSigner())

	var env map[string]any
	if err := json.Unmarshal(signedExtra, &env); err != nil {
		t.Fatalf("signed extra not valid json: %v", err)
	}
	list := env["attachments"].([]any)
	extraURL := list[0].(map[string]any)["media_url"].(string)

	wantContent := fmt.Sprintf("![image](<%s>)", extraURL)
	if signedContent != wantContent {
		t.Fatalf("content and extra signed URLs diverged\n content=%q\n   extra media_url=%q", signedContent, extraURL)
	}
	// And it must actually be signed (changed from the bare URL).
	if extraURL == testMediaURL {
		t.Fatalf("extra media_url was not signed")
	}
}

func TestSignMessageMedia_MultipleImagesEachConsistent(t *testing.T) {
	urlA := testMediaURL
	urlB := "https://aibot-1252145388.cos.accelerate.myqcloud.com/media/user/2030840865701756928/another.png"
	content := fmt.Sprintf("![image](<%s>)\n![image](<%s>)", urlA, urlB)
	extra, _ := json.Marshal(map[string]any{
		"attachments": []any{
			map[string]any{"media_url": urlA, "attachment_type": "image"},
			map[string]any{"media_url": urlB, "attachment_type": "image"},
		},
	})

	signedContent, signedExtra := signMessageMedia(content, json.RawMessage(extra), newCountingSigner())

	var env map[string]any
	_ = json.Unmarshal(signedExtra, &env)
	list := env["attachments"].([]any)
	signedA := list[0].(map[string]any)["media_url"].(string)
	signedB := list[1].(map[string]any)["media_url"].(string)

	if !strings.Contains(signedContent, "(<"+signedA+">)") {
		t.Fatalf("image A inline copy != extra copy\n content=%q\n extraA=%q", signedContent, signedA)
	}
	if !strings.Contains(signedContent, "(<"+signedB+">)") {
		t.Fatalf("image B inline copy != extra copy\n content=%q\n extraB=%q", signedContent, signedB)
	}
	if signedA == signedB {
		t.Fatalf("distinct images must get distinct signed URLs")
	}
}

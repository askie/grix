package service

import (
	"strings"
	"testing"

	"github.com/askie/grix/backend/config"
)

func TestGenerateFriendQRCodeLengthAndAlphabet(t *testing.T) {
	code, err := generateFriendQRCode(24)
	if err != nil {
		t.Fatalf("generateFriendQRCode error = %v", err)
	}
	if len(code) != 24 {
		t.Fatalf("expected length 24, got %d", len(code))
	}

	alphabetSet := make(map[rune]struct{}, len(friendQRCodeAlphabet))
	for _, ch := range friendQRCodeAlphabet {
		alphabetSet[ch] = struct{}{}
	}
	for _, ch := range code {
		if _, ok := alphabetSet[ch]; !ok {
			t.Fatalf("unexpected char %q in code %q", ch, code)
		}
	}
}

func TestBuildFriendQRShareURL(t *testing.T) {
	original := config.C.Server.FriendQRBaseURL
	t.Cleanup(func() {
		config.C.Server.FriendQRBaseURL = original
	})

	config.C.Server.FriendQRBaseURL = "https://release.example.com/u"
	url := buildFriendQRShareURL("abc123")
	if url != "https://release.example.com/u/abc123" {
		t.Fatalf("unexpected url: %s", url)
	}

	config.C.Server.FriendQRBaseURL = ""
	fallback := buildFriendQRShareURL("xyz")
	if !strings.HasPrefix(fallback, defaultFriendQRLinkBase+"/") {
		t.Fatalf("unexpected fallback url: %s", fallback)
	}
}

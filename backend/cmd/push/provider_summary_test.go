package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLogPathReturnsAbsolutePath(t *testing.T) {
	relativePath := filepath.Join("testdata", "firebase.json")

	resolved := resolveLogPath(relativePath)

	if !filepath.IsAbs(resolved) {
		t.Fatalf("expected absolute path, got %q", resolved)
	}
	if !strings.HasSuffix(resolved, filepath.Join("testdata", "firebase.json")) {
		t.Fatalf("unexpected resolved path: %q", resolved)
	}
}

func TestRedactKeyHint(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty",
			input: "   ",
			want:  "",
		},
		{
			name:  "short",
			input: "abc123",
			want:  "abc123",
		},
		{
			name:  "medium",
			input: "abc123456",
			want:  "abc...456",
		},
		{
			name:  "long",
			input: "1234567890abcdef",
			want:  "1234...cdef",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactKeyHint(tc.input); got != tc.want {
				t.Fatalf("redactKeyHint(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

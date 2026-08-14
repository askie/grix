package service

import (
	"testing"

	"github.com/askie/grix/backend/config"
)

func TestResolveMediaObjectKey_OnlyOwnStorage(t *testing.T) {
	prev := config.C.OSS.Media
	t.Cleanup(func() { config.C.OSS.Media = prev })

	config.C.OSS.Media = config.OSSConfig{
		Endpoint:   "s3.ap-southeast-1.amazonaws.com",
		Bucket:     "grix-prod-media",
		PublicURL:  "https://cdn.grix.im",
		StorageDir: "aibot/media",
	}

	cases := []struct {
		name    string
		url     string
		wantKey string
	}{
		{
			name:    "tailnet video link is foreign",
			url:     "http://100.64.0.1:8099/cb_demo.mp4",
			wantKey: "",
		},
		{
			name:    "arbitrary external https link is foreign",
			url:     "https://example.com/aibot/clip.mp4",
			wantKey: "",
		},
		{
			name:    "external host that happens to expose /aibot/media path is foreign",
			url:     "https://evil.example.com/random/file.png",
			wantKey: "",
		},
		{
			name:    "virtual-hosted media bucket URL is ours",
			url:     "https://grix-prod-media.s3.ap-southeast-1.amazonaws.com/aibot/media/media/sess1/x.png",
			wantKey: "aibot/media/media/sess1/x.png",
		},
		{
			name:    "path-style media endpoint URL is ours",
			url:     "https://s3.ap-southeast-1.amazonaws.com/grix-prod-media/aibot/media/user/42/y.png",
			wantKey: "aibot/media/user/42/y.png",
		},
		{
			name:    "configured public CDN URL is ours",
			url:     "https://cdn.grix.im/aibot/media/media/sess2/z.png",
			wantKey: "aibot/media/media/sess2/z.png",
		},
		{
			name:    "bare relative object key stays resolvable",
			url:     "aibot/media/media/sess3/w.png",
			wantKey: "aibot/media/media/sess3/w.png",
		},
		{
			name:    "different bucket sharing the same region endpoint is still foreign (virtual-hosted)",
			url:     "https://other-bucket.s3.ap-southeast-1.amazonaws.com/aibot/media/media/sess1/x.png",
			wantKey: "",
		},
		{
			name:    "different bucket sharing the same region endpoint is still foreign (path-style)",
			url:     "https://s3.ap-southeast-1.amazonaws.com/other-bucket/aibot/media/media/sess1/x.png",
			wantKey: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveMediaObjectKey(tc.url); got != tc.wantKey {
				t.Fatalf("resolveMediaObjectKey(%q) = %q, want %q", tc.url, got, tc.wantKey)
			}
		})
	}
}

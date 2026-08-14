package e2e

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

func seedRelease(t *testing.T, id int64, platform, version string, build int) {
	t.Helper()
	r := model.AppRelease{
		ID:           id,
		Version:      version,
		BuildNumber:  build,
		Platform:     platform,
		Channel:      "stable",
		UpdateMethod: "download",
		DownloadURL:  "https://release.dhf.pub/latest/Grix-" + platform + ".zip",
		Status:       model.ReleaseStatusPublished,
	}
	if err := store.DB.Create(&r).Error; err != nil {
		t.Fatalf("seed release: %v", err)
	}
}

func fetchAppcast(t *testing.T, ctx *e2eContext, platform string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/app/appcast.xml?platform="+platform, nil)
	w := httptest.NewRecorder()
	ctx.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("appcast %s: status %d, body %s", platform, w.Code, w.Body.String())
	}
	return w.Body.String()
}

// A Windows client running the newest published build must not be told an update
// exists. WinSparkle compares the appcast version against the exe's FileVersion
// string, which Flutter sets to the full pubspec version ("3.1.4+693"); the feed
// has to speak the same format for an equal comparison to come out equal.
func TestAppcastXML_WindowsVersionMatchesExeFileVersion(t *testing.T) {
	ctx := setupE2E(t)
	seedRelease(t, 9001, "windows", "3.1.4", 693)

	xml := fetchAppcast(t, ctx, "windows")

	if !strings.Contains(xml, "<sparkle:version>3.1.4+693</sparkle:version>") {
		t.Fatalf("windows appcast must advertise 3.1.4+693, got:\n%s", xml)
	}
	if strings.Contains(xml, "<sparkle:version>693</sparkle:version>") {
		t.Fatalf("windows appcast must not advertise a bare build number, got:\n%s", xml)
	}
	if !strings.Contains(xml, "<sparkle:shortVersionString>3.1.4</sparkle:shortVersionString>") {
		t.Fatalf("windows appcast lost its short version string:\n%s", xml)
	}
}

// macOS Sparkle compares CFBundleVersion, which Flutter sets to the bare build
// number, so that platform keeps its existing format.
func TestAppcastXML_MacosKeepsBuildNumber(t *testing.T) {
	ctx := setupE2E(t)
	seedRelease(t, 9002, "macos", "3.1.4", 690)

	xml := fetchAppcast(t, ctx, "macos")

	if !strings.Contains(xml, "<sparkle:version>690</sparkle:version>") {
		t.Fatalf("macos appcast must advertise build 690, got:\n%s", xml)
	}
}

// A genuinely newer Windows build must still be advertised.
func TestAppcastXML_WindowsAdvertisesNewerBuild(t *testing.T) {
	ctx := setupE2E(t)
	seedRelease(t, 9003, "windows", "3.1.4", 693)
	seedRelease(t, 9004, "windows", "3.1.5", 700)

	xml := fetchAppcast(t, ctx, "windows")

	if n := strings.Count(xml, "<item>"); n != 1 {
		t.Fatalf("expected exactly 1 item, got %d:\n%s", n, xml)
	}
	if !strings.Contains(xml, "<sparkle:version>3.1.5+700</sparkle:version>") {
		t.Fatalf("expected newest 3.1.5+700 advertised, got:\n%s", xml)
	}
}

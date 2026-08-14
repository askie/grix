package service

import (
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.5.0", "2.4.0", 1},
		{"2.4.0", "2.5.0", -1},
		{"2.5.0", "2.5.0", 0},
		{"2.5.1", "2.5.0", 1},
		{"2.10.0", "2.9.0", 1}, // numeric, not lexicographic
		{"2.5.0+490", "2.4.0+474", 1},
		{"2.5", "2.5.0", 0},
	}
	for _, c := range cases {
		if got := compareSemver(c.a, c.b); got != c.want {
			t.Errorf("compareSemver(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestIsUpgrade(t *testing.T) {
	// Older semver is never an upgrade, even with a higher build number — this is
	// the exact bug we are fixing (offering 2.4.0 to a 2.5.0 user).
	if isUpgrade("2.4.0", 999, "2.5.0", 490) {
		t.Fatal("2.4.0 must never be an upgrade over 2.5.0 regardless of build number")
	}
	// Newer semver is an upgrade even if build number is lower.
	if !isUpgrade("2.5.0", 100, "2.4.0", 999) {
		t.Fatal("2.5.0 should be an upgrade over 2.4.0")
	}
	// Same semver: build number breaks the tie.
	if !isUpgrade("2.5.0", 491, "2.5.0", 490) {
		t.Fatal("higher build of same version should be an upgrade")
	}
	if isUpgrade("2.5.0", 490, "2.5.0", 490) {
		t.Fatal("identical version+build is not an upgrade")
	}
}

func withTestDB(t *testing.T) {
	t.Helper()
	testDB := testutil.NewTestDB()
	t.Cleanup(testDB.Close)
	original := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() { store.DB = original })
}

func seedAppRelease(t *testing.T, id int64, platform, version string, build int) {
	t.Helper()
	r := model.AppRelease{
		ID:           id,
		Version:      version,
		BuildNumber:  build,
		Platform:     platform,
		Channel:      "stable",
		UpdateMethod: "download",
		DownloadURL:  "https://example.com/grix.zip",
		Status:       model.ReleaseStatusPublished,
	}
	if err := store.DB.Create(&r).Error; err != nil {
		t.Fatalf("seed release: %v", err)
	}
}

// Reproduces老郭's report: a macOS user on 2.5.0+490 must NOT be told 2.4.0 is
// available, even though the latest published desktop release is 2.4.0+474.
func TestCheckAppUpdate_NeverOffersOlderVersion(t *testing.T) {
	withTestDB(t)
	seedAppRelease(t, 1, "macos", "2.4.0", 474)

	resp, ec := CheckAppUpdate(CheckAppUpdateReq{
		Platform:    "macos",
		Version:     "2.5.0",
		BuildNumber: 490,
	})
	if ec != nil {
		t.Fatalf("unexpected errcode: %+v", ec)
	}
	if resp.HasUpdate {
		t.Fatalf("expected no update offered to 2.5.0+490 when latest is 2.4.0+474, got %+v", resp.Latest)
	}
}

func TestCheckAppUpdate_OffersGenuineNewerVersion(t *testing.T) {
	withTestDB(t)
	seedAppRelease(t, 1, "android", "2.4.0", 474)

	resp, ec := CheckAppUpdate(CheckAppUpdateReq{
		Platform:    "android",
		Version:     "2.3.0",
		BuildNumber: 445,
	})
	if ec != nil {
		t.Fatalf("unexpected errcode: %+v", ec)
	}
	if !resp.HasUpdate || resp.Latest == nil || resp.Latest.Version != "2.4.0" {
		t.Fatalf("expected update to 2.4.0, got %+v", resp)
	}
}

// Even if a stale lower-semver release somehow carries the highest build number,
// it must not be selected as the latest.
func TestCheckAppUpdate_PicksNewestBySemverNotBuild(t *testing.T) {
	withTestDB(t)
	seedAppRelease(t, 1, "android", "2.5.0", 480) // genuine newest by semver
	seedAppRelease(t, 2, "android", "2.4.0", 999) // bogus high build, older semver

	resp, ec := CheckAppUpdate(CheckAppUpdateReq{
		Platform:    "android",
		Version:     "2.3.0",
		BuildNumber: 445,
	})
	if ec != nil {
		t.Fatalf("unexpected errcode: %+v", ec)
	}
	if !resp.HasUpdate || resp.Latest == nil || resp.Latest.Version != "2.5.0" {
		t.Fatalf("expected latest 2.5.0, got %+v", resp)
	}
}

func TestGenerateAppcast_AdvertisesOnlyNewest(t *testing.T) {
	withTestDB(t)
	seedAppRelease(t, 1, "macos", "2.3.0", 445)
	seedAppRelease(t, 2, "macos", "2.4.0", 474)
	seedAppRelease(t, 3, "macos", "2.4.0", 454)

	xml, ec := GenerateAppcast("macos", "stable")
	if ec != nil {
		t.Fatalf("unexpected errcode: %+v", ec)
	}
	if n := strings.Count(xml, "<item>"); n != 1 {
		t.Fatalf("expected exactly 1 item in appcast, got %d\n%s", n, xml)
	}
	if !strings.Contains(xml, "<sparkle:version>474</sparkle:version>") {
		t.Fatalf("expected newest build 474 advertised, got:\n%s", xml)
	}
	if strings.Contains(xml, "<sparkle:version>445</sparkle:version>") ||
		strings.Contains(xml, "<sparkle:version>454</sparkle:version>") {
		t.Fatalf("older releases must not be advertised:\n%s", xml)
	}
}

// WinSparkle reads the exe's FileVersion string, which Flutter sets to the full
// pubspec version ("3.1.4+693"). The appcast must advertise the same format, or
// WinSparkle compares a bare build number against it and offers the installed
// build as an update forever.
func TestGenerateAppcast_WindowsAdvertisesFullVersion(t *testing.T) {
	withTestDB(t)
	seedAppRelease(t, 1, "windows", "3.1.4", 693)

	xml, ec := GenerateAppcast("windows", "stable")
	if ec != nil {
		t.Fatalf("unexpected errcode: %+v", ec)
	}
	if !strings.Contains(xml, "<sparkle:version>3.1.4+693</sparkle:version>") {
		t.Fatalf("expected full version 3.1.4+693 advertised, got:\n%s", xml)
	}
	if strings.Contains(xml, "<sparkle:version>693</sparkle:version>") {
		t.Fatalf("bare build number must not be advertised to WinSparkle:\n%s", xml)
	}
}

// macOS Sparkle compares CFBundleVersion, which Flutter sets to the bare build
// number, so that platform must keep advertising the build number alone.
func TestGenerateAppcast_MacosAdvertisesBuildNumber(t *testing.T) {
	withTestDB(t)
	seedAppRelease(t, 1, "macos", "3.1.4", 690)

	xml, ec := GenerateAppcast("macos", "stable")
	if ec != nil {
		t.Fatalf("unexpected errcode: %+v", ec)
	}
	if !strings.Contains(xml, "<sparkle:version>690</sparkle:version>") {
		t.Fatalf("expected bare build 690 advertised, got:\n%s", xml)
	}
}

// Windows updates ship an Inno Setup installer. The appcast must tell WinSparkle
// to run it silently, otherwise the "auto" update still forces the user through
// the install wizard. macOS installs a zip itself and must NOT carry the element.
func TestGenerateAppcast_WindowsCarriesSilentInstallerArguments(t *testing.T) {
	withTestDB(t)
	seedAppRelease(t, 1, "windows", "3.1.4", 693)

	xml, ec := GenerateAppcast("windows", "stable")
	if ec != nil {
		t.Fatalf("unexpected errcode: %+v", ec)
	}
	if !strings.Contains(xml, "<sparkle:installerArguments>/SILENT</sparkle:installerArguments>") {
		t.Fatalf("windows appcast must instruct WinSparkle to install silently, got:\n%s", xml)
	}
}

func TestGenerateAppcast_MacosHasNoInstallerArguments(t *testing.T) {
	withTestDB(t)
	seedAppRelease(t, 1, "macos", "3.1.4", 690)

	xml, ec := GenerateAppcast("macos", "stable")
	if ec != nil {
		t.Fatalf("unexpected errcode: %+v", ec)
	}
	if strings.Contains(xml, "sparkle:installerArguments") {
		t.Fatalf("macOS appcast must not carry installer arguments, got:\n%s", xml)
	}
}

func TestSparkleInstallerArguments(t *testing.T) {
	if got := sparkleInstallerArguments("windows"); got != "/SILENT" {
		t.Errorf("windows: got %q, want /SILENT", got)
	}
	for _, p := range []string{"macos", "linux", "android", "ios"} {
		if got := sparkleInstallerArguments(p); got != "" {
			t.Errorf("%s: got %q, want empty", p, got)
		}
	}
}

func TestSparkleVersion(t *testing.T) {
	if got := sparkleVersion("windows", "3.1.4", 693); got != "3.1.4+693" {
		t.Errorf("windows: got %q, want 3.1.4+693", got)
	}
	if got := sparkleVersion("macos", "3.1.4", 690); got != "690" {
		t.Errorf("macos: got %q, want 690", got)
	}
	// A pubspec with no build suffix produces an exe reporting a bare "3.1.4";
	// appending "+0" would make WinSparkle see a newer version than installed.
	if got := sparkleVersion("windows", "3.1.4", 0); got != "3.1.4" {
		t.Errorf("windows build 0: got %q, want 3.1.4", got)
	}
}

func TestValidSemver(t *testing.T) {
	valid := []string{"1.0", "2.5.0", "10.20.30", "0.0.1"}
	for _, v := range valid {
		if !validSemver(v) {
			t.Errorf("validSemver(%q) should be true", v)
		}
	}
	invalid := []string{"", "abc", "1", "1.2.3.4", "1..2", ".1.2", "1.2.", "v1.2.3"}
	for _, v := range invalid {
		if validSemver(v) {
			t.Errorf("validSemver(%q) should be false", v)
		}
	}
}

func TestCreateAppRelease_RejectsInvalidVersion(t *testing.T) {
	withTestDB(t)
	_, ec := CreateAppRelease(CreateAppReleaseReq{
		Version:     "abc",
		BuildNumber: 1,
		Platform:    "android",
	})
	if ec == nil {
		t.Fatal("expected error for invalid version")
	}
}

func TestCreateAppRelease_RejectsDuplicate(t *testing.T) {
	withTestDB(t)
	_, ec := CreateAppRelease(CreateAppReleaseReq{
		Version: "1.0.0", BuildNumber: 100, Platform: "android",
		DownloadURL: "https://example.com/a.apk",
	})
	if ec != nil {
		t.Fatalf("first create should succeed: %+v", ec)
	}
	_, ec = CreateAppRelease(CreateAppReleaseReq{
		Version: "1.0.0", BuildNumber: 100, Platform: "android",
		DownloadURL: "https://example.com/b.apk",
	})
	if ec == nil {
		t.Fatal("expected error for duplicate release")
	}
	if ec.HTTPStatus != 409 {
		t.Fatalf("expected 409 Conflict, got %d", ec.HTTPStatus)
	}
}

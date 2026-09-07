package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// The Android APK used to be built with --split-per-abi, which offsets the
// versionCode by 2000 for arm64 (build 920 -> 2920). Universal APKs drop that
// offset and the build number jumps to 3000, so an old 2920 client must still
// be offered the 3000 release even though the semantic version is identical.
func TestIsUpgrade_AndroidBuildNumberJumpTo3000(t *testing.T) {
	if !isUpgrade("3.2.7", 3000, "3.2.7", 2920) {
		t.Fatal("3.2.7+3000 must be an upgrade over the split-per-abi 3.2.7+2920")
	}
	// The pre-jump numbering must not suddenly look newer than the new scheme.
	if isUpgrade("3.2.7", 2920, "3.2.7", 3000) {
		t.Fatal("3.2.7+2920 must never be an upgrade over 3.2.7+3000")
	}
	// A user still on the real (unoffset) 921 also gets it.
	if !isUpgrade("3.2.7", 3000, "3.2.7", 921) {
		t.Fatal("3.2.7+3000 must be an upgrade over 3.2.7+921")
	}
}

func TestCheckAppUpdate_OffersBuild3000ToSplitPerAbiClient(t *testing.T) {
	withTestDB(t)
	seedAppRelease(t, 1, "android", "3.2.7", 3000)

	resp, ec := CheckAppUpdate(CheckAppUpdateReq{
		Platform:    "android",
		Version:     "3.2.7",
		BuildNumber: 2920,
	})
	if ec != nil {
		t.Fatalf("unexpected errcode: %+v", ec)
	}
	if !resp.HasUpdate || resp.Latest == nil || resp.Latest.BuildNumber != 3000 {
		t.Fatalf("expected 3.2.7+3000 offered to a 2920 client, got %+v", resp)
	}
}

func TestNormalizeDownloadErrorCode(t *testing.T) {
	cases := map[string]string{
		"":                      "",
		"permission_blocked":    "permission_blocked",
		"download_timeout":      "download_timeout",
		"sha256_mismatch":       "sha256_mismatch",
		"installer_not_found":   "installer_not_found",
		"low_storage":           "low_storage",
		"install_not_completed": "install_not_completed",
		// Free-form exception text from older clients collapses into one bucket
		// instead of polluting the enum (see the legacy-text test for the two
		// causes that are recovered before this fallback).
		"DioException [connection error]: ...": "download_failed",
	}
	for in, want := range cases {
		if got := normalizeDownloadErrorCode(in); got != want {
			t.Errorf("normalizeDownloadErrorCode(%q)=%q want %q", in, got, want)
		}
	}
}

// Clients older than 3.2.7+3000 keep sending free-form exception text. The two
// causes still recoverable from that text must survive normalisation, otherwise
// a timeout wave is indistinguishable from a generic failure until every user
// has updated.
func TestNormalizeDownloadErrorCode_RecoversLegacyFreeText(t *testing.T) {
	timeouts := []string{
		"DioException [receive timeout]: The request took longer than 0:03:00.000000",
		"connection timeout",
		"Receive Timeout",
	}
	for _, in := range timeouts {
		if got := normalizeDownloadErrorCode(in); got != "download_timeout" {
			t.Errorf("normalizeDownloadErrorCode(%q)=%q want download_timeout", in, got)
		}
	}

	mismatches := []string{
		"APK SHA256 mismatch: expected=abc, got=def",
		"sha256 校验失败",
	}
	for _, in := range mismatches {
		if got := normalizeDownloadErrorCode(in); got != "sha256_mismatch" {
			t.Errorf("normalizeDownloadErrorCode(%q)=%q want sha256_mismatch", in, got)
		}
	}

	// Text carrying neither clue still collapses into the generic bucket.
	for _, in := range []string{"SocketException: Connection reset by peer", "unknown"} {
		if got := normalizeDownloadErrorCode(in); got != "download_failed" {
			t.Errorf("normalizeDownloadErrorCode(%q)=%q want download_failed", in, got)
		}
	}
}

func TestNormalizeDownloadStage(t *testing.T) {
	if got := normalizeDownloadStage(""); got != DownloadStageDownload {
		t.Errorf("empty stage should default to download, got %q", got)
	}
	if got := normalizeDownloadStage("install"); got != DownloadStageInstall {
		t.Errorf("install stage lost, got %q", got)
	}
	if got := normalizeDownloadStage("bogus"); got != DownloadStageDownload {
		t.Errorf("unknown stage should default to download, got %q", got)
	}
}

func TestReportAppDownload_PersistsDeviceFieldsAndStage(t *testing.T) {
	withTestDB(t)
	seedAppRelease(t, 1, "android", "3.2.7", 3000)

	fromBuild := 2920
	if ec := ReportAppDownload(ReportAppDownloadReq{
		UserID:      42,
		BuildNumber: 3000,
		Platform:    "android",
		FromBuild:   &fromBuild,
		Stage:       "install",
		DeviceModel: "Xiaomi 2211133C",
		OsVersion:   "Android 14 (34)",
		Abi:         "arm64-v8a",
	}); ec != nil {
		t.Fatalf("report failed: %+v", ec)
	}

	var got model.AppDownloadReport
	if err := store.DB.First(&got).Error; err != nil {
		t.Fatalf("read back report: %v", err)
	}
	if got.Stage != "install" || got.DeviceModel != "Xiaomi 2211133C" ||
		got.OsVersion != "Android 14 (34)" || got.Abi != "arm64-v8a" {
		t.Fatalf("new fields not stored: %+v", got)
	}
	if got.ErrorMsg != "" {
		t.Fatalf("successful install must have empty error_msg, got %q", got.ErrorMsg)
	}
}

// Old clients send neither stage nor device fields; those rows must still land
// and be counted as downloads.
func TestReportAppDownload_LegacyClientDefaultsToDownloadStage(t *testing.T) {
	withTestDB(t)
	seedAppRelease(t, 1, "android", "3.2.7", 3000)

	if ec := ReportAppDownload(ReportAppDownloadReq{
		UserID:      7,
		BuildNumber: 3000,
		Platform:    "android",
	}); ec != nil {
		t.Fatalf("report failed: %+v", ec)
	}

	var got model.AppDownloadReport
	if err := store.DB.First(&got).Error; err != nil {
		t.Fatalf("read back report: %v", err)
	}
	if got.Stage != DownloadStageDownload {
		t.Fatalf("legacy report should default to download stage, got %q", got.Stage)
	}
}

func TestGetAppDownloadStats_InstallSuccessAndDeviceBreakdown(t *testing.T) {
	withTestDB(t)
	seedAppRelease(t, 1, "android", "3.2.7", 3000)

	report := func(stage, errMsg, device string) {
		t.Helper()
		if ec := ReportAppDownload(ReportAppDownloadReq{
			UserID:      1,
			BuildNumber: 3000,
			Platform:    "android",
			Stage:       stage,
			ErrorMsg:    errMsg,
			DeviceModel: device,
			DurationMs:  1000,
		}); ec != nil {
			t.Fatalf("report failed: %+v", ec)
		}
	}

	report("download", "", "Xiaomi 2211133C")
	report("install", "", "Xiaomi 2211133C")
	report("install", "install_not_completed", "HUAWEI ALN-AL00")
	report("install", "install_not_completed", "HUAWEI ALN-AL00")
	report("download", "permission_blocked", "vivo V2227A")

	stats, ec := GetAppDownloadStats(1)
	if ec != nil {
		t.Fatalf("stats failed: %+v", ec)
	}
	// 下载口径只算 stage=download：上面 1 条 download 成功 + 1 条 install 成功，
	// success 必须还是 1，不能被安装记录顶成 2。
	if stats.Total != 2 {
		t.Fatalf("total=%d want 2 (只数 download 阶段的两条)", stats.Total)
	}
	if stats.Success != 1 {
		t.Fatalf("success=%d want 1，安装成功的记录不该算进下载成功", stats.Success)
	}
	if stats.Failed != 1 {
		t.Fatalf("failed=%d want 1，安装失败的记录不该算进下载失败", stats.Failed)
	}
	if stats.InstallSuccess != 1 {
		t.Fatalf("install_success=%d want 1", stats.InstallSuccess)
	}
	if stats.InstallFailed != 2 {
		t.Fatalf("install_failed=%d want 2", stats.InstallFailed)
	}
	if len(stats.FailedByDevice) != 2 {
		t.Fatalf("failed_by_device=%+v want 2 rows", stats.FailedByDevice)
	}
	// Most failures first.
	top := stats.FailedByDevice[0]
	if top.DeviceModel != "HUAWEI ALN-AL00" || top.Count != 2 ||
		top.ErrorMsg != "install_not_completed" {
		t.Fatalf("unexpected top failing device: %+v", top)
	}
	second := stats.FailedByDevice[1]
	if second.DeviceModel != "vivo V2227A" || second.Count != 1 ||
		second.ErrorMsg != "permission_blocked" {
		t.Fatalf("unexpected second failing device: %+v", second)
	}
}

// 一次成功的更新会留下 download 和 install 两条记录。下载口径若不按 stage 过滤，
// 安装记录就会把"下载成功"翻倍，并把平均下载耗时拉低——这是发布后看数据的人
// 最容易被误导的地方。
func TestGetAppDownloadStats_InstallReportsDoNotPolluteDownloadStats(t *testing.T) {
	withTestDB(t)
	seedAppRelease(t, 1, "android", "3.2.7", 3000)

	// 一条下载成功（耗时 8s）
	if ec := ReportAppDownload(ReportAppDownloadReq{
		UserID: 1, BuildNumber: 3000, Platform: "android",
		Stage: "download", DurationMs: 8000,
	}); ec != nil {
		t.Fatalf("download report failed: %+v", ec)
	}
	// 一条安装成功（安装上报不带耗时）
	if ec := ReportAppDownload(ReportAppDownloadReq{
		UserID: 1, BuildNumber: 3000, Platform: "android",
		Stage: "install", DurationMs: 10,
	}); ec != nil {
		t.Fatalf("install report failed: %+v", ec)
	}

	stats, ec := GetAppDownloadStats(1)
	if ec != nil {
		t.Fatalf("stats failed: %+v", ec)
	}
	if stats.Total != 1 {
		t.Fatalf("total=%d want 1", stats.Total)
	}
	if stats.Success != 1 {
		t.Fatalf("success=%d want 1", stats.Success)
	}
	if stats.InstallSuccess != 1 {
		t.Fatalf("install_success=%d want 1", stats.InstallSuccess)
	}
	// 平均耗时只看下载那条，不能被 install 的 10ms 拉到 4005。
	if stats.AvgDurationMs != 8000 {
		t.Fatalf("avg_duration_ms=%v want 8000（安装记录不该进平均耗时）", stats.AvgDurationMs)
	}
}

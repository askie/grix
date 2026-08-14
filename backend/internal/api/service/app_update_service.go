package service

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/reach"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

// --- Check update ---

type CheckAppUpdateReq struct {
	Platform    string // ios | android | macos | windows
	Version     string // semver, e.g. "2.3.0"
	BuildNumber int    // Flutter build number, e.g. 382
	Channel     string // stable | beta
	UserID      int64  // authenticated user, used for gray rollout
	OsVersion   string // OS version reported by the client, e.g. "18.5" (iOS) or "14" (Android)
}

type CheckAppUpdateResp struct {
	HasUpdate  bool               `json:"has_update"`
	Force      bool               `json:"force"`
	Latest     *AppReleaseInfo    `json:"latest,omitempty"`
}

type AppReleaseInfo struct {
	Version      string `json:"version"`
	BuildNumber  int    `json:"build_number"`
	Changelog    string `json:"changelog,omitempty"`
	UpdateMethod string `json:"update_method"` // download | app_store | google_play
	DownloadURL  string `json:"download_url,omitempty"`
	AppStoreURL  string `json:"app_store_url,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
	Sha256       string `json:"sha256,omitempty"`
}

// validPlatforms is the whitelist of allowed platform values.
var validPlatforms = map[string]bool{
	"ios":     true,
	"android": true,
	"macos":   true,
	"windows": true,
	"linux":   true,
}

// validChannels 允许的通道值白名单
var validChannels = map[string]bool{
	"stable": true,
	"beta":   true,
}

// validateDownloadURL 校验下载链接的安全性（必须 https，可选域名白名单）
func validateDownloadURL(rawURL string) bool {
	if rawURL == "" {
		return true // 允许为空（如 app_store 方式不需要 download_url）
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return false
	}
	allowed := strings.TrimSpace(config.C.AppUpdate.AllowedDownloadDomains)
	if allowed == "" {
		return true // 未配置白名单则仅校验 https
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range strings.Split(allowed, ",") {
		d := strings.TrimSpace(strings.ToLower(domain))
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// validSemver checks that v is a well-formed dotted-numeric version with 2 or 3
// components (e.g. "2.5", "2.10.3"). Each component must be a non-negative integer.
func validSemver(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}

// parseSemver splits a dotted numeric version like "2.5.0" into its components,
// dropping any build-metadata / pre-release suffix after the first '+' or '-'.
// Non-numeric components are treated as 0.
func parseSemver(v string) []int {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, "+-"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}

// compareSemver returns -1 if a<b, 0 if equal, 1 if a>b, comparing component by
// component (missing components count as 0).
func compareSemver(a, b string) int {
	pa, pb := parseSemver(a), parseSemver(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		va, vb := 0, 0
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va != vb {
			if va < vb {
				return -1
			}
			return 1
		}
	}
	return 0
}

// isUpgrade reports whether candidate (version, build) is strictly newer than the
// current (version, build). Semantic version dominates; build number breaks ties.
// A candidate whose semantic version is lower is NEVER an upgrade — even if its
// build number is higher — so an older release can never be offered as an update.
func isUpgrade(candVer string, candBuild int, curVer string, curBuild int) bool {
	if c := compareSemver(candVer, curVer); c != 0 {
		return c > 0
	}
	return candBuild > curBuild
}

// newestRelease returns the release that is the maximum by (semver, build) among
// the given slice, or nil if it is empty.
func newestRelease(releases []model.AppRelease) *model.AppRelease {
	var best *model.AppRelease
	for i := range releases {
		r := &releases[i]
		if best == nil || isUpgrade(r.Version, r.BuildNumber, best.Version, best.BuildNumber) {
			best = r
		}
	}
	return best
}

// CheckAppUpdate checks if a newer version is available for the client.
func CheckAppUpdate(req CheckAppUpdateReq) (*CheckAppUpdateResp, *errcode.ErrCode) {
	if !validPlatforms[req.Platform] {
		return nil, &errcode.ErrBadRequest
	}
	if req.Channel == "" {
		req.Channel = "stable"
	}
	if !validChannels[req.Channel] {
		return nil, &errcode.ErrBadRequest
	}

	// Pick the newest published release for this platform + channel by
	// (semver, build). Selecting in Go rather than via ORDER BY build_number
	// guarantees we never treat a semver-older release as the latest even if it
	// somehow carries a higher build number.
	var releases []model.AppRelease
	if err := store.DB.
		Where("platform = ? AND channel = ? AND status = ?",
			req.Platform, req.Channel, model.ReleaseStatusPublished).
		Find(&releases).Error; err != nil {
		return &CheckAppUpdateResp{HasUpdate: false}, nil
	}
	release := newestRelease(releases)
	if release == nil {
		// No published release for this platform
		return &CheckAppUpdateResp{HasUpdate: false}, nil
	}

	// Only offer a release that is strictly newer than the client's current
	// version. Semantic version dominates; an older version is never presented
	// as an update even if its build number is higher.
	if !isUpgrade(release.Version, release.BuildNumber, req.Version, req.BuildNumber) {
		return &CheckAppUpdateResp{HasUpdate: false}, nil
	}

	// Gray rollout matching
	if !matchAppRolloutRules(release.ID, req.UserID) {
		return &CheckAppUpdateResp{HasUpdate: false}, nil
	}

	// Determine force: client build < min_build threshold
	force := false
	if release.MinBuild != nil && req.BuildNumber < *release.MinBuild {
		force = true
	}

	resp := &CheckAppUpdateResp{
		HasUpdate: true,
		Force:     force,
		Latest: &AppReleaseInfo{
			Version:      release.Version,
			BuildNumber:  release.BuildNumber,
			Changelog:    release.Changelog,
			UpdateMethod: release.UpdateMethod,
			DownloadURL:  release.DownloadURL,
			AppStoreURL:  release.AppStoreURL,
			FileSize:     release.FileSize,
			Sha256:       release.Sha256,
		},
	}
	return resp, nil
}

// --- Gray rollout matching ---

func matchAppRolloutRules(releaseID, userID int64) bool {
	// 查询该 release 的 active 规则；无 active 规则则全员可见
	var rules []model.AppRolloutRule
	if err := store.DB.
		Where("release_id = ? AND status = ?", releaseID, model.RolloutRuleActive).
		Order("priority DESC").
		Find(&rules).Error; err != nil {
		return false
	}
	if len(rules) == 0 {
		return true
	}

	for _, rule := range rules {
		switch rule.RuleType {
		case "user_list":
			if matchUserList(rule.RuleValue, userID) {
				return true
			}
		case "percentage":
			if matchUserPercentage(rule.RuleValue, userID) {
				return true
			}
		}
	}
	return false
}

func matchUserList(ruleValue []byte, userID int64) bool {
	var data struct {
		UserIDs []int64 `json:"user_ids"`
	}
	if err := json.Unmarshal(ruleValue, &data); err != nil {
		return false
	}
	for _, id := range data.UserIDs {
		if id == userID {
			return true
		}
	}
	return false
}

func matchUserPercentage(ruleValue []byte, userID int64) bool {
	var data struct {
		Percent int `json:"percent"`
	}
	if err := json.Unmarshal(ruleValue, &data); err != nil {
		return false
	}
	if data.Percent >= 100 {
		return true
	}
	if data.Percent <= 0 {
		return false
	}
	return appHashMod(userID, 100) < data.Percent
}

// --- Admin: App release management ---

type CreateAppReleaseReq struct {
	Version        string
	BuildNumber    int
	Platform       string
	Channel        string
	Changelog      string
	MinVersion     *string
	MinBuild       *int
	UpdateMethod   string
	DownloadURL    string
	AppStoreURL    string
	FileSize       int64
	Sha256         string
	EddsaSignature string
}

type AppReleaseResp struct {
	ID             int64   `json:"id,string"`
	Version        string  `json:"version"`
	BuildNumber    int     `json:"build_number"`
	Platform       string  `json:"platform"`
	Channel        string  `json:"channel"`
	Changelog      string  `json:"changelog"`
	MinVersion     *string `json:"min_version"`
	MinBuild       *int    `json:"min_build"`
	UpdateMethod   string  `json:"update_method"`
	DownloadURL    string  `json:"download_url"`
	AppStoreURL    string  `json:"app_store_url"`
	FileSize       int64   `json:"file_size"`
	Sha256         string  `json:"sha256"`
	EddsaSignature string  `json:"eddsa_signature"`
	Status         int16   `json:"status"`
	StatusLabel    string  `json:"status_label"`
	PublishedAt    *string `json:"published_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
}

func appReleaseToResp(r *model.AppRelease) AppReleaseResp {
	resp := AppReleaseResp{
		ID:             r.ID,
		Version:        r.Version,
		BuildNumber:    r.BuildNumber,
		Platform:       r.Platform,
		Channel:        r.Channel,
		Changelog:      r.Changelog,
		MinVersion:     r.MinVersion,
		MinBuild:       r.MinBuild,
		UpdateMethod:   r.UpdateMethod,
		DownloadURL:    r.DownloadURL,
		AppStoreURL:    r.AppStoreURL,
		FileSize:       r.FileSize,
		Sha256:         r.Sha256,
		EddsaSignature: r.EddsaSignature,
		Status:         r.Status,
		StatusLabel:    releaseStatusLabels[r.Status],
		CreatedAt:      r.CreatedAt.Format(time.RFC3339),
	}
	if r.PublishedAt != nil {
		s := r.PublishedAt.Format(time.RFC3339)
		resp.PublishedAt = &s
	}
	return resp
}

func CreateAppRelease(req CreateAppReleaseReq) (*AppReleaseResp, *errcode.ErrCode) {
	if !validSemver(req.Version) {
		return nil, &errcode.ErrCode{HTTPStatus: 400, BizCode: 10003, Msg: "version must be dotted-numeric (e.g. 1.2.3)"}
	}
	if req.Channel == "" {
		req.Channel = "stable"
	}
	if !validChannels[req.Channel] {
		return nil, &errcode.ErrBadRequest
	}
	if !validPlatforms[req.Platform] {
		return nil, &errcode.ErrBadRequest
	}
	if req.UpdateMethod == "" {
		req.UpdateMethod = "download"
	}
	if !validateDownloadURL(req.DownloadURL) {
		return nil, &errcode.ErrBadRequest
	}
	if !validateDownloadURL(req.AppStoreURL) {
		return nil, &errcode.ErrBadRequest
	}

	// Reject duplicate (platform, channel, version, build_number)
	var count int64
	store.DB.Model(&model.AppRelease{}).
		Where("platform = ? AND channel = ? AND version = ? AND build_number = ?",
			req.Platform, req.Channel, req.Version, req.BuildNumber).
		Count(&count)
	if count > 0 {
		return nil, &errcode.ErrCode{HTTPStatus: 409, BizCode: 10003, Msg: "duplicate release: same platform/channel/version/build already exists"}
	}

	release := model.AppRelease{
		ID:             snowflake.GenID(),
		Version:        req.Version,
		BuildNumber:    req.BuildNumber,
		Platform:       req.Platform,
		Channel:        req.Channel,
		Changelog:      req.Changelog,
		MinVersion:     req.MinVersion,
		MinBuild:       req.MinBuild,
		UpdateMethod:   req.UpdateMethod,
		DownloadURL:    req.DownloadURL,
		AppStoreURL:    req.AppStoreURL,
		FileSize:       req.FileSize,
		Sha256:         req.Sha256,
		EddsaSignature: req.EddsaSignature,
		Status:         model.ReleaseStatusDraft,
	}
	if err := store.DB.Create(&release).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	resp := appReleaseToResp(&release)
	return &resp, nil
}

func PublishAppRelease(id int64) (*AppReleaseResp, *errcode.ErrCode) {
	var release model.AppRelease
	if err := store.DB.First(&release, id).Error; err != nil {
		return nil, &errcode.ErrNotFound
	}
	if release.Status != model.ReleaseStatusDraft && release.Status != model.ReleaseStatusPaused {
		return nil, &errcode.ErrBadRequest
	}

	now := time.Now()
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		// 撤销同平台同通道已发布的旧版本
		tx.Model(&model.AppRelease{}).
			Where("platform = ? AND channel = ? AND status = ? AND id != ?",
				release.Platform, release.Channel, model.ReleaseStatusPublished, id).
			Update("status", model.ReleaseStatusRevoked)

		return tx.Model(&release).Updates(map[string]any{
			"status":       model.ReleaseStatusPublished,
			"published_at": now,
		}).Error
	})
	if err != nil {
		return nil, &errcode.ErrInternal
	}
	release.Status = model.ReleaseStatusPublished
	release.PublishedAt = &now

	// Reach hub: on a full (全量) publish, fan out an in-app notice to every
	// active user. Best-effort — a reach failure must not fail the publish.
	reach.PublishAppReleaseEvent(&release)

	resp := appReleaseToResp(&release)
	return &resp, nil
}

func RevokeAppRelease(id int64) (*AppReleaseResp, *errcode.ErrCode) {
	var release model.AppRelease
	if err := store.DB.First(&release, id).Error; err != nil {
		return nil, &errcode.ErrNotFound
	}
	if release.Status != model.ReleaseStatusPublished && release.Status != model.ReleaseStatusPaused {
		return nil, &errcode.ErrBadRequest
	}
	if err := store.DB.Model(&release).Update("status", model.ReleaseStatusRevoked).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	release.Status = model.ReleaseStatusRevoked
	resp := appReleaseToResp(&release)
	return &resp, nil
}

func PauseAppRelease(id int64) (*AppReleaseResp, *errcode.ErrCode) {
	var release model.AppRelease
	if err := store.DB.First(&release, id).Error; err != nil {
		return nil, &errcode.ErrNotFound
	}
	if release.Status != model.ReleaseStatusPublished {
		return nil, &errcode.ErrBadRequest
	}
	if err := store.DB.Model(&release).Update("status", model.ReleaseStatusPaused).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	release.Status = model.ReleaseStatusPaused
	resp := appReleaseToResp(&release)
	return &resp, nil
}

func ResumeAppRelease(id int64) (*AppReleaseResp, *errcode.ErrCode) {
	var release model.AppRelease
	if err := store.DB.First(&release, id).Error; err != nil {
		return nil, &errcode.ErrNotFound
	}
	if release.Status != model.ReleaseStatusPaused {
		return nil, &errcode.ErrBadRequest
	}

	err := store.DB.Transaction(func(tx *gorm.DB) error {
		// 撤销同平台同通道已发布的旧版本，防止同时存在两个 published
		tx.Model(&model.AppRelease{}).
			Where("platform = ? AND channel = ? AND status = ? AND id != ?",
				release.Platform, release.Channel, model.ReleaseStatusPublished, id).
			Update("status", model.ReleaseStatusRevoked)

		return tx.Model(&release).Update("status", model.ReleaseStatusPublished).Error
	})
	if err != nil {
		return nil, &errcode.ErrInternal
	}
	release.Status = model.ReleaseStatusPublished
	resp := appReleaseToResp(&release)
	return &resp, nil
}

func DeleteAppRelease(id int64) *errcode.ErrCode {
	var release model.AppRelease
	if err := store.DB.First(&release, id).Error; err != nil {
		return &errcode.ErrNotFound
	}
	// Only allow deleting draft releases
	if release.Status != model.ReleaseStatusDraft {
		return &errcode.ErrBadRequest
	}
	// Delete associated rollout rules first
	store.DB.Where("release_id = ?", id).Delete(&model.AppRolloutRule{})
	if err := store.DB.Delete(&release).Error; err != nil {
		return &errcode.ErrInternal
	}
	return nil
}

type ListAppReleasesReq struct {
	Platform string
	Channel  string
	Status   *int16
	Page     int
	PageSize int
}

type ListAppReleasesResult struct {
	Total    int64            `json:"total"`
	Releases []AppReleaseResp `json:"releases"`
}

func ListAppReleases(req ListAppReleasesReq) (*ListAppReleasesResult, *errcode.ErrCode) {
	db := store.DB.Model(&model.AppRelease{})
	if req.Platform != "" {
		db = db.Where("platform = ?", req.Platform)
	}
	if req.Channel != "" {
		db = db.Where("channel = ?", req.Channel)
	}
	if req.Status != nil {
		db = db.Where("status = ?", *req.Status)
	}

	var total int64
	db.Count(&total)

	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}
	offset := (req.Page - 1) * req.PageSize

	var releases []model.AppRelease
	db.Order("created_at DESC").Offset(offset).Limit(req.PageSize).Find(&releases)

	result := &ListAppReleasesResult{
		Total:    total,
		Releases: make([]AppReleaseResp, len(releases)),
	}
	for i := range releases {
		result.Releases[i] = appReleaseToResp(&releases[i])
	}
	return result, nil
}

// --- Admin: App rollout rule management ---

type CreateAppRolloutRuleReq struct {
	ReleaseID int64
	RuleType  string // user_list | percentage
	RuleValue []byte // JSON
	Priority  int
}

type AppRolloutRuleResp struct {
	ID        int64  `json:"id,string"`
	ReleaseID int64  `json:"release_id,string"`
	RuleType  string `json:"rule_type"`
	RuleValue string `json:"rule_value"`
	Priority  int    `json:"priority"`
	Status    int16  `json:"status"`
	StatusLabel string `json:"status_label"`
}

var appRolloutRuleStatusLabels = map[int16]string{
	model.RolloutRuleActive: "active",
	model.RolloutRulePaused: "paused",
}

func appRolloutRuleToResp(r *model.AppRolloutRule) AppRolloutRuleResp {
	return AppRolloutRuleResp{
		ID:          r.ID,
		ReleaseID:   r.ReleaseID,
		RuleType:    r.RuleType,
		RuleValue:   string(r.RuleValue),
		Priority:    r.Priority,
		Status:      r.Status,
		StatusLabel: appRolloutRuleStatusLabels[r.Status],
	}
}

func CreateAppRolloutRule(req CreateAppRolloutRuleReq) (*AppRolloutRuleResp, *errcode.ErrCode) {
	// Verify release exists
	var release model.AppRelease
	if err := store.DB.First(&release, req.ReleaseID).Error; err != nil {
		return nil, &errcode.ErrNotFound
	}

	rule := model.AppRolloutRule{
		ID:        snowflake.GenID(),
		ReleaseID: req.ReleaseID,
		RuleType:  req.RuleType,
		RuleValue: req.RuleValue,
		Priority:  req.Priority,
		Status:    model.RolloutRuleActive,
	}
	if err := store.DB.Create(&rule).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	resp := appRolloutRuleToResp(&rule)
	return &resp, nil
}

func ListAppRolloutRules(releaseID int64) ([]AppRolloutRuleResp, *errcode.ErrCode) {
	var rules []model.AppRolloutRule
	if err := store.DB.Where("release_id = ?", releaseID).
		Order("priority DESC").
		Find(&rules).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	result := make([]AppRolloutRuleResp, len(rules))
	for i := range rules {
		result[i] = appRolloutRuleToResp(&rules[i])
	}
	return result, nil
}

func UpdateAppRolloutRuleStatus(id int64, status int16) (*AppRolloutRuleResp, *errcode.ErrCode) {
	if status != model.RolloutRuleActive && status != model.RolloutRulePaused {
		return nil, &errcode.ErrBadRequest
	}
	var rule model.AppRolloutRule
	if err := store.DB.First(&rule, id).Error; err != nil {
		return nil, &errcode.ErrNotFound
	}
	if err := store.DB.Model(&rule).Update("status", status).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	rule.Status = status
	resp := appRolloutRuleToResp(&rule)
	return &resp, nil
}

func DeleteAppRolloutRule(id int64) *errcode.ErrCode {
	result := store.DB.Delete(&model.AppRolloutRule{}, id)
	if result.Error != nil {
		return &errcode.ErrInternal
	}
	if result.RowsAffected == 0 {
		return &errcode.ErrNotFound
	}
	return nil
}

// appHashMod computes a deterministic hash mod for user-based rollout.
func appHashMod(id int64, mod int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(fmt.Sprintf("%d", id)))
	return int(h.Sum32() % uint32(mod))
}

// --- Appcast XML feed for Sparkle / WinSparkle ---

// sparkleVersion renders the version identifier that the desktop Sparkle client
// compares against the version its own binary reports.
//
// macOS Sparkle compares CFBundleVersion, which Flutter sets to the bare build
// number ("693").
//
// WinSparkle has no separate build field: it reads the executable's FileVersion
// string, which Flutter sets to the full pubspec version including build
// metadata ("3.1.4+693"). Advertising the bare build number there makes
// WinSparkle compare "693" against "3.1.4+693" component by component, find
// 693 > 3, and offer the already-installed build as an update forever.
// A release carrying no build number matches a pubspec with no build suffix, so
// the executable reports a bare "3.1.4" and the feed must not append "+0".
func sparkleVersion(platform, version string, build int) string {
	if platform == "windows" {
		if build <= 0 {
			return version
		}
		return fmt.Sprintf("%s+%d", version, build)
	}
	return strconv.Itoa(build)
}

// sparkleInstallerArguments returns the command-line arguments WinSparkle passes
// to the downloaded installer, or "" when none apply.
//
// On Windows the update payload is an Inno Setup installer. Without arguments it
// runs its interactive wizard, so the "auto" update still forces the user to
// click through Next/Install/Finish. Passing "/SILENT" makes Inno run without any
// wizard pages — it shows only a non-interactive progress window, then the
// installer's [Run] step (guarded by WizardSilent in grix.iss) relaunches the
// app. The result is a genuine one-confirmation update: the user approves once in
// WinSparkle's dialog, everything after installs unattended.
//
// "/SILENT" (progress window) is chosen over "/VERYSILENT" (fully invisible)
// deliberately: the overwrite step kills and restarts the running app, so a
// visible progress indicator is worth keeping until silent updates are proven on
// real Windows machines.
//
// macOS/Sparkle installs a .app from a zip itself and takes no installer
// arguments, so this only applies to Windows. WinSparkle reads it from the
// <sparkle:installerArguments> item element (verified present in the bundled
// WinSparkle 0.8.1).
func sparkleInstallerArguments(platform string) string {
	if platform == "windows" {
		return "/SILENT"
	}
	return ""
}

// GenerateAppcast generates a Sparkle-compatible appcast.xml for the given platform and channel.
func GenerateAppcast(platform, channel string) (string, *errcode.ErrCode) {
	if !validPlatforms[platform] {
		return "", &errcode.ErrBadRequest
	}
	if channel == "" {
		channel = "stable"
	}

	var releases []model.AppRelease
	err := store.DB.
		Where("platform = ? AND channel = ? AND status = ?", platform, channel, model.ReleaseStatusPublished).
		Find(&releases).Error
	if err != nil {
		return "", &errcode.ErrInternal
	}

	xmlStr := `<?xml version="1.0" encoding="utf-8"?>` + "\n"
	xmlStr += `<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle" xmlns:dc="http://purl.org/dc/elements/1.1/">` + "\n"
	xmlStr += "  <channel>\n"
	xmlStr += "    <title>Grix App Updates</title>\n"

	// Advertise only the genuine newest release (by semver, then build). Listing
	// only the true latest prevents the Sparkle client from ever picking an
	// older version as the available update.
	var feedItems []model.AppRelease
	if newest := newestRelease(releases); newest != nil {
		feedItems = append(feedItems, *newest)
	}

	for _, r := range feedItems {
		xmlStr += "    <item>\n"
		xmlStr += fmt.Sprintf("      <title>Version %s (%d)</title>\n", xmlEscape(r.Version), r.BuildNumber)
		xmlStr += fmt.Sprintf("      <sparkle:shortVersionString>%s</sparkle:shortVersionString>\n", xmlEscape(r.Version))
		xmlStr += fmt.Sprintf("      <sparkle:version>%s</sparkle:version>\n", xmlEscape(sparkleVersion(platform, r.Version, r.BuildNumber)))
		if r.Changelog != "" {
			// Escape CDATA end marker within changelog
			safeChangelog := cdataSafe(r.Changelog)
			xmlStr += fmt.Sprintf("      <description><![CDATA[%s]]></description>\n", safeChangelog)
		}
		if r.DownloadURL != "" {
			// Installer arguments must precede the enclosure: WinSparkle reads this
			// item-level element to decide how to launch the downloaded installer.
			if args := sparkleInstallerArguments(platform); args != "" {
				xmlStr += fmt.Sprintf("      <sparkle:installerArguments>%s</sparkle:installerArguments>\n", xmlEscape(args))
			}
			xmlStr += fmt.Sprintf("      <enclosure url=\"%s\"", xmlAttrEscape(r.DownloadURL))
			if r.FileSize > 0 {
				xmlStr += fmt.Sprintf(" length=\"%d\"", r.FileSize)
			}
			if r.EddsaSignature != "" {
				xmlStr += fmt.Sprintf(" sparkle:edSignature=\"%s\"", xmlAttrEscape(r.EddsaSignature))
			}
			xmlStr += " type=\"application/octet-stream\"/>\n"
		}
		xmlStr += "    </item>\n"
	}

	xmlStr += "  </channel>\n"
	xmlStr += "</rss>\n"
	return xmlStr, nil
}

// xmlEscape escapes text for safe inclusion in XML element content.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// xmlAttrEscape escapes text for safe inclusion in XML attribute values (double-quoted).
func xmlAttrEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// cdataSafe replaces "]]>" with "]]&gt;" to prevent CDATA termination.
func cdataSafe(s string) string {
	return strings.ReplaceAll(s, "]]>", "]]&gt;")
}

// --- Download statistics ---

type ReportAppDownloadReq struct {
	UserID      int64
	BuildNumber int
	Platform    string
	FromBuild   *int
	ErrorMsg    string
	DurationMs  int
}

func ReportAppDownload(req ReportAppDownloadReq) *errcode.ErrCode {
	// Resolve release_id from build_number + platform
	var release model.AppRelease
	if err := store.DB.
		Where("build_number = ? AND platform = ? AND status = ?",
			req.BuildNumber, req.Platform, model.ReleaseStatusPublished).
		First(&release).Error; err != nil {
		return &errcode.ErrNotFound
	}

	report := model.AppDownloadReport{
		ID:         snowflake.GenID(),
		UserID:     req.UserID,
		ReleaseID:  release.ID,
		FromBuild:  req.FromBuild,
		Platform:   req.Platform,
		ErrorMsg:   req.ErrorMsg,
		DurationMs: req.DurationMs,
	}
	if err := store.DB.Create(&report).Error; err != nil {
		return &errcode.ErrInternal
	}
	return nil
}

type AppDownloadStatsResp struct {
	ReleaseID int64 `json:"release_id"`
	Version   string `json:"version"`
	BuildNumber int `json:"build_number"`
	Platform  string `json:"platform"`
	Total     int64 `json:"total"`
	Success   int64 `json:"success"`
	Failed    int64 `json:"failed"`
	AvgDurationMs float64 `json:"avg_duration_ms"`
}

func GetAppDownloadStats(releaseID int64) (*AppDownloadStatsResp, *errcode.ErrCode) {
	var release model.AppRelease
	if err := store.DB.First(&release, releaseID).Error; err != nil {
		return nil, &errcode.ErrNotFound
	}

	var total int64
	store.DB.Model(&model.AppDownloadReport{}).Where("release_id = ?", releaseID).Count(&total)

	var success int64
	store.DB.Model(&model.AppDownloadReport{}).Where("release_id = ? AND error_msg = ''", releaseID).Count(&success)

	var failed int64
	store.DB.Model(&model.AppDownloadReport{}).Where("release_id = ? AND error_msg != ''", releaseID).Count(&failed)

	var avgDuration float64
	store.DB.Model(&model.AppDownloadReport{}).
		Where("release_id = ? AND duration_ms > 0", releaseID).
		Select("COALESCE(AVG(duration_ms), 0)").
		Row().Scan(&avgDuration)

	return &AppDownloadStatsResp{
		ReleaseID:   releaseID,
		Version:     release.Version,
		BuildNumber: release.BuildNumber,
		Platform:    release.Platform,
		Total:       total,
		Success:     success,
		Failed:      failed,
		AvgDurationMs: avgDuration,
	}, nil
}

type ListAppDownloadReportsReq struct {
	ReleaseID int64
	Platform  string
	Page      int
	PageSize  int
}

type AppDownloadReportResp struct {
	ID         int64  `json:"id,string"`
	UserID     int64  `json:"user_id,string"`
	ReleaseID  int64  `json:"release_id,string"`
	FromBuild  *int   `json:"from_build"`
	Platform   string `json:"platform"`
	ErrorMsg   string `json:"error_msg"`
	DurationMs int    `json:"duration_ms"`
	ReportedAt string `json:"reported_at"`
}

type ListAppDownloadReportsResult struct {
	Total   int64                   `json:"total"`
	Reports []AppDownloadReportResp `json:"reports"`
}

func ListAppDownloadReports(req ListAppDownloadReportsReq) (*ListAppDownloadReportsResult, *errcode.ErrCode) {
	db := store.DB.Model(&model.AppDownloadReport{})
	if req.ReleaseID > 0 {
		db = db.Where("release_id = ?", req.ReleaseID)
	}
	if req.Platform != "" {
		db = db.Where("platform = ?", req.Platform)
	}

	var total int64
	db.Count(&total)

	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}
	offset := (req.Page - 1) * req.PageSize

	var reports []model.AppDownloadReport
	db.Order("reported_at DESC").Offset(offset).Limit(req.PageSize).Find(&reports)

	result := &ListAppDownloadReportsResult{
		Total:   total,
		Reports: make([]AppDownloadReportResp, len(reports)),
	}
	for i, r := range reports {
		result.Reports[i] = AppDownloadReportResp{
			ID:         r.ID,
			UserID:     r.UserID,
			ReleaseID:  r.ReleaseID,
			FromBuild:  r.FromBuild,
			Platform:   r.Platform,
			ErrorMsg:   r.ErrorMsg,
			DurationMs: r.DurationMs,
			ReportedAt: r.ReportedAt.Format(time.RFC3339),
		}
	}
	return result, nil
}

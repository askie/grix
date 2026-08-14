package service

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
)

// --- Request / Response types ---

type CheckUpgradeReq struct {
	ClientType    string
	ClientVersion string
	Channel       string
	AgentID       int64
	Platform      string
	Arch          string
}

type CheckUpgradeResp struct {
	Available bool             `json:"available"`
	Release   *ReleaseInfoResp `json:"release,omitempty"`
}

type ReleaseInfoResp struct {
	Version    string `json:"version"`
	NpmPackage string `json:"npm_package"`
	NpmTag     string `json:"npm_tag"`
	Changelog  string `json:"changelog,omitempty"`
	Channel    string `json:"channel"`
	Force      bool   `json:"force"`
	MinVersion string `json:"min_version,omitempty"`
}

type ReportUpgradeReq struct {
	AgentID     int64
	ClientType  string
	FromVersion string
	ToVersion   string
	Status      string
	ErrorCode   *string
	ErrorMsg    *string
	UpgradeLog  *string
	CrashCount  int
	NpmVersion  *string
	NodeVersion *string
	DiskFreeMb  *int
	Platform    *string
	Arch        *string
	DurationMs  *int
	HostName    *string
	InstallID   *string
}

// --- Version comparison ---

// isNewer 判断 version 是否严格高于 baseline，按 semver 数字分量比较。
// 兼容 "v" 前缀、预发布尾巴（例如 "2.10.0-beta.1"）按主.次.补丁三段比，
// 多余段视为 0；非法/缺失则字符串退化比较，避免回归静默放过。
func isNewer(version, baseline string) bool {
	v, vOK := parseSemverTriple(version)
	b, bOK := parseSemverTriple(baseline)
	if !vOK || !bOK {
		return version > baseline
	}
	for i := 0; i < 3; i++ {
		if v[i] != b[i] {
			return v[i] > b[i]
		}
	}
	return false
}

func parseSemverTriple(s string) ([3]int, bool) {
	var out [3]int
	if s == "" {
		return out, false
	}
	raw := s
	if raw[0] == 'v' || raw[0] == 'V' {
		raw = raw[1:]
	}
	// 去掉 build metadata / 预发布尾巴
	if i := strings.IndexAny(raw, "-+"); i >= 0 {
		raw = raw[:i]
	}
	parts := strings.Split(raw, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		if p == "" {
			return out, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// --- CheckUpgrade ---

func CheckUpgrade(req CheckUpgradeReq) (*CheckUpgradeResp, *errcode.ErrCode) {
	clientType := req.ClientType
	if clientType == "" {
		clientType = "grix-connector"
	}
	// Find the latest published release for this client type and channel.
	// NOTE: select the highest *semver* version in Go — a SQL "version DESC"
	// sorts lexically, where "1.5.6" > "1.5.10" and the wrong release wins.
	var releases []model.ConnectorRelease
	err := store.DB.
		Where("client_type = ? AND channel = ? AND status = ?", clientType, req.Channel, model.ReleaseStatusPublished).
		Find(&releases).Error
	if err != nil || len(releases) == 0 {
		// No published release found
		return &CheckUpgradeResp{Available: false}, nil
	}
	release := releases[0]
	for _, r := range releases[1:] {
		if isNewer(r.Version, release.Version) {
			release = r
		}
	}

	// Version comparison
	if !isNewer(release.Version, req.ClientVersion) {
		return &CheckUpgradeResp{Available: false}, nil
	}

	// Min version check: client must be >= min_version
	if release.MinVersion != nil && isNewer(*release.MinVersion, req.ClientVersion) {
		return &CheckUpgradeResp{Available: false}, nil
	}

	// Gray release matching: check rollout rules
	rulesMatched, ec := matchRolloutRules(release.ID, req.AgentID)
	if ec != nil {
		return nil, ec
	}
	if !rulesMatched {
		return &CheckUpgradeResp{Available: false}, nil
	}

	resp := &CheckUpgradeResp{
		Available: true,
		Release: &ReleaseInfoResp{
			Version:    release.Version,
			NpmPackage: release.NpmPackage,
			NpmTag:     release.NpmTag,
			Changelog:  release.Changelog,
			Channel:    release.Channel,
			Force:      release.Force,
		},
	}
	if release.MinVersion != nil {
		resp.Release.MinVersion = *release.MinVersion
	}
	return resp, nil
}

func matchRolloutRules(releaseID, agentID int64) (bool, *errcode.ErrCode) {
	// Check if any rules exist at all (any status)
	var totalRules int64
	store.DB.Model(&model.ConnectorRolloutRule{}).Where("release_id = ?", releaseID).Count(&totalRules)
	if totalRules == 0 {
		// No rules at all → available to everyone
		return true, nil
	}

	// Check active rules only
	var rules []model.ConnectorRolloutRule
	if err := store.DB.
		Where("release_id = ? AND status = ?", releaseID, model.RolloutRuleActive).
		Order("priority DESC").
		Find(&rules).Error; err != nil {
		return false, &errcode.ErrInternal
	}

	for _, rule := range rules {
		switch rule.RuleType {
		case "agent_list":
			if matchAgentList(rule.RuleValue, agentID) {
				return true, nil
			}
		case "percentage":
			if matchPercentage(rule.RuleValue, agentID) {
				return true, nil
			}
		}
	}
	return false, nil
}

func matchAgentList(ruleValue []byte, agentID int64) bool {
	var data struct {
		AgentIDs []int64 `json:"agent_ids"`
	}
	if err := json.Unmarshal(ruleValue, &data); err != nil {
		return false
	}
	for _, id := range data.AgentIDs {
		if id == agentID {
			return true
		}
	}
	return false
}

func matchPercentage(ruleValue []byte, agentID int64) bool {
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
	return hashMod(agentID, 100) < data.Percent
}

func hashMod(id int64, mod int) int {
	h := fnv.New32a()
	fmt.Fprintf(h, "%d", id)
	return int(h.Sum32() % uint32(mod))
}

// Ensure sort is available
var _ = sort.Ints

// --- Admin: Release management ---

type CreateConnectorReleaseReq struct {
	ClientType string
	Version    string
	Channel    string
	Changelog  string
	MinVersion *string
	NpmPackage string
	NpmTag     string
	Force      bool
}

type ConnectorReleaseResp struct {
	ID          int64   `json:"id,string"`
	ClientType  string  `json:"client_type"`
	Version     string  `json:"version"`
	Channel     string  `json:"channel"`
	Changelog   string  `json:"changelog"`
	MinVersion  *string `json:"min_version"`
	NpmPackage  string  `json:"npm_package"`
	NpmTag      string  `json:"npm_tag"`
	Force       bool    `json:"force"`
	Status      int16   `json:"status"`
	StatusLabel string  `json:"status_label"`
}

var releaseStatusLabels = map[int16]string{
	model.ReleaseStatusDraft:     "draft",
	model.ReleaseStatusPublished: "published",
	model.ReleaseStatusRevoked:   "revoked",
	model.ReleaseStatusPaused:    "paused",
}

func releaseToResp(r *model.ConnectorRelease) ConnectorReleaseResp {
	label := releaseStatusLabels[r.Status]
	return ConnectorReleaseResp{
		ID:          r.ID,
		ClientType:  r.ClientType,
		Version:     r.Version,
		Channel:     r.Channel,
		Changelog:   r.Changelog,
		MinVersion:  r.MinVersion,
		NpmPackage:  r.NpmPackage,
		NpmTag:      r.NpmTag,
		Force:       r.Force,
		Status:      r.Status,
		StatusLabel: label,
	}
}

func CreateConnectorRelease(req CreateConnectorReleaseReq) (*ConnectorReleaseResp, *errcode.ErrCode) {
	release := model.ConnectorRelease{
		ID:         snowflake.GenID(),
		ClientType: req.ClientType,
		Version:    req.Version,
		Channel:    req.Channel,
		Changelog:  req.Changelog,
		MinVersion: req.MinVersion,
		NpmPackage: req.NpmPackage,
		NpmTag:     req.NpmTag,
		Force:      req.Force,
		Status:     model.ReleaseStatusDraft,
	}
	if err := store.DB.Create(&release).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	resp := releaseToResp(&release)
	return &resp, nil
}

func PublishConnectorRelease(id int64) (*ConnectorReleaseResp, *errcode.ErrCode) {
	var release model.ConnectorRelease
	if err := store.DB.First(&release, id).Error; err != nil {
		return nil, &errcode.ErrNotFound
	}
	if release.Status != model.ReleaseStatusDraft && release.Status != model.ReleaseStatusPaused {
		return nil, &errcode.ErrBadRequest
	}
	now := time.Now()
	if err := store.DB.Model(&release).Updates(map[string]any{
		"status":       model.ReleaseStatusPublished,
		"published_at": now,
	}).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	release.Status = model.ReleaseStatusPublished
	release.PublishedAt = &now
	resp := releaseToResp(&release)
	return &resp, nil
}

func RevokeConnectorRelease(id int64) (*ConnectorReleaseResp, *errcode.ErrCode) {
	var release model.ConnectorRelease
	if err := store.DB.First(&release, id).Error; err != nil {
		return nil, &errcode.ErrNotFound
	}
	if err := store.DB.Model(&release).Update("status", model.ReleaseStatusRevoked).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	release.Status = model.ReleaseStatusRevoked
	resp := releaseToResp(&release)
	return &resp, nil
}

func ListConnectorReleases(clientType string) ([]ConnectorReleaseResp, *errcode.ErrCode) {
	db := store.DB.Model(&model.ConnectorRelease{})
	if clientType != "" {
		db = db.Where("client_type = ?", clientType)
	}
	var releases []model.ConnectorRelease
	if err := db.Order("created_at DESC").Find(&releases).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	result := make([]ConnectorReleaseResp, len(releases))
	for i := range releases {
		result[i] = releaseToResp(&releases[i])
	}
	return result, nil
}

// --- ReportUpgrade ---

// clampInt32 钳制上报中的整型遥测字段到 PostgreSQL int4 范围 [0, MaxInt32]。
// connector 旧版本会用 Number.MAX_SAFE_INTEGER(9007199254740991) 作为"磁盘充足"
// 哨兵值上报 disk_free_mb，超出 int4 列上限导致整条 INSERT 失败(SQLSTATE 22003)。
// 这里做防御性钳制，确保任何客户端的异常大值都不会让上报落库失败。
func clampInt32(v *int) *int {
	if v == nil {
		return nil
	}
	c := *v
	if c < 0 {
		c = 0
	} else if c > math.MaxInt32 {
		c = math.MaxInt32
	}
	return &c
}

// autoPauseAsyncOnReport 控制失败上报后是否异步触发 auto-pause 检查。生产恒为 true。
// 测试会关闭它：该 goroutine 读全局 store.DB（fire-and-forget），而各测试会重置
// store.DB，A 用例派出的检查可能在 B 用例运行时执行、提前暂停 B 的规则，造成偶发失败。
var autoPauseAsyncOnReport = true

func ReportUpgrade(req ReportUpgradeReq) *errcode.ErrCode {
	report := model.ConnectorUpgradeReport{
		ID:          snowflake.GenID(),
		AgentID:     req.AgentID,
		ClientType:  req.ClientType,
		FromVersion: req.FromVersion,
		ToVersion:   req.ToVersion,
		Status:      req.Status,
		ErrorCode:   req.ErrorCode,
		ErrorMsg:    req.ErrorMsg,
		UpgradeLog:  req.UpgradeLog,
		CrashCount:  req.CrashCount,
		NpmVersion:  req.NpmVersion,
		NodeVersion: req.NodeVersion,
		DiskFreeMb:  clampInt32(req.DiskFreeMb),
		Platform:    req.Platform,
		Arch:        req.Arch,
		DurationMs:  clampInt32(req.DurationMs),
		HostName:    req.HostName,
		InstallID:   req.InstallID,
		ReportedAt:  time.Now(),
	}
	if err := store.DB.Create(&report).Error; err != nil {
		return &errcode.ErrInternal
	}

	// Trigger auto-pause check on failure reports
	if autoPauseAsyncOnReport && (req.Status == model.UpgradeReportFailed || req.Status == model.UpgradeReportRolledBack) {
		go func() {
			if paused, ec := AutoPauseCheck(); ec == nil && len(paused) > 0 {
				for _, r := range paused {
					fmt.Printf("auto-pause: rule %d (%s) paused: %s\n", r.ID, r.RuleType, r.Reason)
				}
			}
		}()
	}

	return nil
}

// --- Admin: Rollout rule management ---

type CreateRolloutRuleReq struct {
	ReleaseID int64
	RuleType  string
	RuleValue []byte
	Priority  int
}

type RolloutRuleResp struct {
	ID              int64  `json:"id,string"`
	ReleaseID       int64  `json:"release_id,string"`
	RuleType        string `json:"rule_type"`
	RuleValue       string `json:"rule_value"`
	Priority        int    `json:"priority"`
	Status          int16  `json:"status"`
	StatusLabel     string `json:"status_label"`
	AutoPauseConfig string `json:"auto_pause_config"`
}

var rolloutRuleStatusLabels = map[int16]string{
	model.RolloutRuleActive: "active",
	model.RolloutRulePaused: "paused",
}

func ruleToResp(r *model.ConnectorRolloutRule) RolloutRuleResp {
	label := rolloutRuleStatusLabels[r.Status]
	return RolloutRuleResp{
		ID:              r.ID,
		ReleaseID:       r.ReleaseID,
		RuleType:        r.RuleType,
		RuleValue:       string(r.RuleValue),
		Priority:        r.Priority,
		Status:          r.Status,
		StatusLabel:     label,
		AutoPauseConfig: string(r.AutoPauseConfig),
	}
}

func CreateRolloutRule(req CreateRolloutRuleReq) (*RolloutRuleResp, *errcode.ErrCode) {
	var release model.ConnectorRelease
	if err := store.DB.First(&release, req.ReleaseID).Error; err != nil {
		return nil, &errcode.ErrNotFound
	}

	rule := model.ConnectorRolloutRule{
		ID:              snowflake.GenID(),
		ReleaseID:       req.ReleaseID,
		RuleType:        req.RuleType,
		RuleValue:       req.RuleValue,
		Priority:        req.Priority,
		Status:          model.RolloutRuleActive,
		AutoPauseConfig: []byte("{}"),
	}
	if err := store.DB.Create(&rule).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	resp := ruleToResp(&rule)
	return &resp, nil
}

func ListRolloutRules(releaseID int64) ([]RolloutRuleResp, *errcode.ErrCode) {
	var rules []model.ConnectorRolloutRule
	if err := store.DB.Where("release_id = ?", releaseID).
		Order("priority DESC").
		Find(&rules).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	result := make([]RolloutRuleResp, len(rules))
	for i := range rules {
		result[i] = ruleToResp(&rules[i])
	}
	return result, nil
}

func UpdateRolloutRuleStatus(id int64, status int16) (*RolloutRuleResp, *errcode.ErrCode) {
	var rule model.ConnectorRolloutRule
	if err := store.DB.First(&rule, id).Error; err != nil {
		return nil, &errcode.ErrNotFound
	}
	if err := store.DB.Model(&rule).Update("status", status).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	rule.Status = status
	resp := ruleToResp(&rule)
	return &resp, nil
}

func DeleteRolloutRule(id int64) *errcode.ErrCode {
	result := store.DB.Delete(&model.ConnectorRolloutRule{}, id)
	if result.Error != nil {
		return &errcode.ErrInternal
	}
	if result.RowsAffected == 0 {
		return &errcode.ErrNotFound
	}
	return nil
}

// --- Upgrade stats ---

type UpgradeStatsResp struct {
	Total      int64 `json:"total"`
	Success    int64 `json:"success"`
	Failed     int64 `json:"failed"`
	RolledBack int64 `json:"rolled_back"`
}

// hostLatestReport 表示某台机器（按 install_id / host_name / agent_id 优先级
// 去重）在指定 to_version 的最新一次升级状态。
type hostLatestReport struct {
	Status     string
	ErrorCode  *string
	DurationMs *int
	ReportedAt time.Time
}

func hostKeyOf(r *model.ConnectorUpgradeReport) string {
	if r.InstallID != nil && *r.InstallID != "" {
		return "i:" + *r.InstallID
	}
	if r.HostName != nil && *r.HostName != "" {
		return "h:" + *r.HostName
	}
	return fmt.Sprintf("a:%d", r.AgentID)
}

// aggregateLatestByHost 按机器维度聚合给定版本/时间窗口内的最新一条 report。
// windowMinutes <= 0 表示不限时间窗口。
func aggregateLatestByHost(version, clientType string, windowMinutes int) (map[string]hostLatestReport, *errcode.ErrCode) {
	db := store.DB.Model(&model.ConnectorUpgradeReport{}).Where("to_version = ?", version)
	if clientType != "" {
		db = db.Where("client_type = ?", clientType)
	}
	if windowMinutes > 0 {
		cutoff := time.Now().Add(-time.Duration(windowMinutes) * time.Minute)
		db = db.Where("reported_at > ?", cutoff)
	}
	var reports []model.ConnectorUpgradeReport
	if err := db.Find(&reports).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	latest := make(map[string]hostLatestReport, len(reports))
	for i := range reports {
		r := &reports[i]
		key := hostKeyOf(r)
		existing, ok := latest[key]
		if !ok || r.ReportedAt.After(existing.ReportedAt) {
			latest[key] = hostLatestReport{
				Status:     r.Status,
				ErrorCode:  r.ErrorCode,
				DurationMs: r.DurationMs,
				ReportedAt: r.ReportedAt,
			}
		}
	}
	return latest, nil
}

// GetUpgradeStats 按机器维度统计指定版本的升级结果（同一台机器多个 agent
// 的多次上报只算一次，看最新状态）。
func GetUpgradeStats(version, clientType string) (*UpgradeStatsResp, *errcode.ErrCode) {
	hosts, ec := aggregateLatestByHost(version, clientType, 0)
	if ec != nil {
		return nil, ec
	}
	stats := &UpgradeStatsResp{Total: int64(len(hosts))}
	for _, h := range hosts {
		switch h.Status {
		case model.UpgradeReportSuccess:
			stats.Success++
		case model.UpgradeReportFailed:
			stats.Failed++
		case model.UpgradeReportRolledBack:
			stats.RolledBack++
		}
	}
	return stats, nil
}

// --- Auto-pause ---

type autoPauseConfig struct {
	FailureRateThreshold float64 `json:"failure_rate_threshold"`
	MinSampleSize        int     `json:"min_sample_size"`
	WindowMinutes        int     `json:"window_minutes"`
	MaxTotalFailures     int     `json:"max_total_failures"`
}

type AutoPauseResult struct {
	ID       int64  `json:"id,string"`
	RuleType string `json:"rule_type"`
	Reason   string `json:"reason"`
}

func AutoPauseCheck() ([]AutoPauseResult, *errcode.ErrCode) {
	var rules []model.ConnectorRolloutRule
	if err := store.DB.Where("status = ?", model.RolloutRuleActive).Find(&rules).Error; err != nil {
		return nil, &errcode.ErrInternal
	}

	var paused []AutoPauseResult
	for _, rule := range rules {
		var cfg autoPauseConfig
		if err := json.Unmarshal(rule.AutoPauseConfig, &cfg); err != nil || cfg.FailureRateThreshold <= 0 {
			continue
		}

		var release model.ConnectorRelease
		if err := store.DB.First(&release, rule.ReleaseID).Error; err != nil {
			continue
		}

		// 按机器维度聚合：同一 connector 装置上的多 agent 多次上报只算一次。
		hosts, hostsEc := aggregateLatestByHost(release.Version, release.ClientType, cfg.WindowMinutes)
		if hostsEc != nil {
			continue
		}
		total := len(hosts)
		if total < cfg.MinSampleSize {
			continue
		}
		failed := 0
		for _, h := range hosts {
			if h.Status == model.UpgradeReportFailed || h.Status == model.UpgradeReportRolledBack {
				failed++
			}
		}

		failureRate := float64(failed) / float64(total)
		reason := ""

		if failureRate > cfg.FailureRateThreshold {
			reason = "failure_rate_exceeded"
		} else if cfg.MaxTotalFailures > 0 && failed >= cfg.MaxTotalFailures {
			reason = "max_total_failures_reached"
		}

		if reason != "" {
			store.DB.Model(&model.ConnectorRolloutRule{}).Where("id = ?", rule.ID).Update("status", model.RolloutRulePaused)
			paused = append(paused, AutoPauseResult{
				ID:       rule.ID,
				RuleType: rule.RuleType,
				Reason:   reason,
			})
		}
	}
	return paused, nil
}

func PauseConnectorRelease(id int64) (*ConnectorReleaseResp, *errcode.ErrCode) {
	var release model.ConnectorRelease
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
	resp := releaseToResp(&release)
	return &resp, nil
}

func ResumeConnectorRelease(id int64) (*ConnectorReleaseResp, *errcode.ErrCode) {
	var release model.ConnectorRelease
	if err := store.DB.First(&release, id).Error; err != nil {
		return nil, &errcode.ErrNotFound
	}
	if release.Status != model.ReleaseStatusPaused {
		return nil, &errcode.ErrBadRequest
	}
	if err := store.DB.Model(&release).Update("status", model.ReleaseStatusPublished).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	release.Status = model.ReleaseStatusPublished
	resp := releaseToResp(&release)
	return &resp, nil
}

// --- Admin: Upgrade report queries ---

type ListUpgradeReportsReq struct {
	ClientType string
	ToVersion  string
	Status     string
	AgentID    *int64
	Page       int
	PageSize   int
}

type UpgradeReportResp struct {
	ID          int64   `json:"id,string"`
	AgentID     int64   `json:"agent_id,string"`
	FromVersion string  `json:"from_version"`
	ToVersion   string  `json:"to_version"`
	Status      string  `json:"status"`
	ErrorCode   *string `json:"error_code"`
	ErrorMsg    *string `json:"error_msg"`
	DurationMs  *int    `json:"duration_ms"`
	Platform    *string `json:"platform"`
	Arch        *string `json:"arch"`
	HostName    *string `json:"host_name"`
	InstallID   *string `json:"install_id"`
	ReportedAt  string  `json:"reported_at"`
}

type ListUpgradeReportsResult struct {
	Total   int64                 `json:"total"`
	Reports []UpgradeReportResp   `json:"reports"`
}

func ListUpgradeReports(req ListUpgradeReportsReq) (*ListUpgradeReportsResult, *errcode.ErrCode) {
	db := store.DB.Model(&model.ConnectorUpgradeReport{})
	if req.ClientType != "" {
		db = db.Where("client_type = ?", req.ClientType)
	}
	if req.ToVersion != "" {
		db = db.Where("to_version = ?", req.ToVersion)
	}
	if req.Status != "" {
		db = db.Where("status = ?", req.Status)
	}
	if req.AgentID != nil {
		db = db.Where("agent_id = ?", *req.AgentID)
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

	var reports []model.ConnectorUpgradeReport
	db.Order("reported_at DESC").Offset(offset).Limit(req.PageSize).Find(&reports)

	result := &ListUpgradeReportsResult{
		Total:   total,
		Reports: make([]UpgradeReportResp, len(reports)),
	}
	for i, r := range reports {
		result.Reports[i] = UpgradeReportResp{
			ID:          r.ID,
			AgentID:     r.AgentID,
			FromVersion: r.FromVersion,
			ToVersion:   r.ToVersion,
			Status:      r.Status,
			ErrorCode:   r.ErrorCode,
			ErrorMsg:    r.ErrorMsg,
			DurationMs:  r.DurationMs,
			Platform:    r.Platform,
			Arch:        r.Arch,
			HostName:    r.HostName,
			InstallID:   r.InstallID,
			ReportedAt:  r.ReportedAt.Format(time.RFC3339),
		}
	}
	return result, nil
}

func GetAgentUpgradeHistory(agentID int64) ([]UpgradeReportResp, *errcode.ErrCode) {
	var reports []model.ConnectorUpgradeReport
	if err := store.DB.Where("agent_id = ?", agentID).
		Order("reported_at DESC").
		Limit(50).
		Find(&reports).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	result := make([]UpgradeReportResp, len(reports))
	for i, r := range reports {
		result[i] = UpgradeReportResp{
			ID:          r.ID,
			AgentID:     r.AgentID,
			FromVersion: r.FromVersion,
			ToVersion:   r.ToVersion,
			Status:      r.Status,
			ErrorCode:   r.ErrorCode,
			ErrorMsg:    r.ErrorMsg,
			DurationMs:  r.DurationMs,
			Platform:    r.Platform,
			Arch:        r.Arch,
			HostName:    r.HostName,
			InstallID:   r.InstallID,
			ReportedAt:  r.ReportedAt.Format(time.RFC3339),
		}
	}
	return result, nil
}

// --- Enhanced stats with error distribution ---

type UpgradeStatsDetailResp struct {
	Total             int64             `json:"total"`
	Success           int64             `json:"success"`
	Failed            int64             `json:"failed"`
	RolledBack        int64             `json:"rolled_back"`
	ErrorDistribution map[string]int    `json:"error_distribution"`
	AvgDurationMs     int64             `json:"avg_duration_ms"`
}

func GetUpgradeStatsDetail(version, clientType string) (*UpgradeStatsDetailResp, *errcode.ErrCode) {
	hosts, ec := aggregateLatestByHost(version, clientType, 0)
	if ec != nil {
		return nil, ec
	}
	stats := &UpgradeStatsDetailResp{
		Total:             int64(len(hosts)),
		ErrorDistribution: make(map[string]int),
	}
	var durSum int64
	var durCnt int64
	for _, h := range hosts {
		switch h.Status {
		case model.UpgradeReportSuccess:
			stats.Success++
		case model.UpgradeReportFailed:
			stats.Failed++
		case model.UpgradeReportRolledBack:
			stats.RolledBack++
		}
		if h.ErrorCode != nil && *h.ErrorCode != "" {
			stats.ErrorDistribution[*h.ErrorCode]++
		}
		if h.DurationMs != nil {
			durSum += int64(*h.DurationMs)
			durCnt++
		}
	}
	if durCnt > 0 {
		stats.AvgDurationMs = durSum / durCnt
	}
	return stats, nil
}

package service

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/urlguard"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LinkBlocklistRulePayload 后台前端表单字段（创建 / 修改共用）。
type LinkBlocklistRulePayload struct {
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	Severity string `json:"severity"`
	Source   string `json:"source"`
	Enabled  *bool  `json:"enabled"`
	Note     string `json:"note"`
}

// LinkBlocklistListParams 列表筛选 + 分页。
type LinkBlocklistListParams struct {
	Query    string
	Kind     string
	Severity string
	Source   string
	Enabled  *bool
	Page     int
	PageSize int
}

type LinkBlocklistListResult struct {
	Items    []model.LinkBlocklistRule `json:"items"`
	Total    int64                     `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
}

const (
	linkBlocklistMaxPageSize     = 100
	linkBlocklistDefaultPageSize = 20
)

// ListLinkBlocklistRules 列表（多维筛选 + 分页）。
func ListLinkBlocklistRules(p LinkBlocklistListParams) (LinkBlocklistListResult, error) {
	if store.DB == nil {
		return LinkBlocklistListResult{}, errors.New("db unavailable")
	}
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize <= 0 {
		p.PageSize = linkBlocklistDefaultPageSize
	}
	if p.PageSize > linkBlocklistMaxPageSize {
		p.PageSize = linkBlocklistMaxPageSize
	}

	q := store.DB.Model(&model.LinkBlocklistRule{})
	if v := strings.TrimSpace(p.Query); v != "" {
		like := "%" + v + "%"
		q = q.Where("value ILIKE ? OR note ILIKE ?", like, like)
	}
	if v := strings.TrimSpace(p.Kind); v != "" {
		q = q.Where("kind = ?", v)
	}
	if v := strings.TrimSpace(p.Severity); v != "" {
		q = q.Where("severity = ?", v)
	}
	if v := strings.TrimSpace(p.Source); v != "" {
		q = q.Where("source = ?", v)
	}
	if p.Enabled != nil {
		q = q.Where("enabled = ?", *p.Enabled)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return LinkBlocklistListResult{}, err
	}

	var rows []model.LinkBlocklistRule
	if err := q.Order("id DESC").
		Offset((p.Page - 1) * p.PageSize).
		Limit(p.PageSize).
		Find(&rows).Error; err != nil {
		return LinkBlocklistListResult{}, err
	}

	return LinkBlocklistListResult{
		Items:    rows,
		Total:    total,
		Page:     p.Page,
		PageSize: p.PageSize,
	}, nil
}

// GetLinkBlocklistRule 单条详情。
func GetLinkBlocklistRule(id int64) (*model.LinkBlocklistRule, error) {
	if store.DB == nil {
		return nil, errors.New("db unavailable")
	}
	var r model.LinkBlocklistRule
	if err := store.DB.First(&r, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

// CreateLinkBlocklistRule 新建（带规则合法性校验 + 同 kind+value 唯一约束）。
func CreateLinkBlocklistRule(
	ctx context.Context,
	adminID int64,
	p LinkBlocklistRulePayload,
	clientIP, userAgent string,
) (*model.LinkBlocklistRule, error) {
	if store.DB == nil {
		return nil, errors.New("db unavailable")
	}
	norm, err := normalizeLinkBlocklistPayload(p)
	if err != nil {
		return nil, err
	}

	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	row := model.LinkBlocklistRule{
		ID:        snowflake.GenID(),
		Kind:      norm.Kind,
		Value:     norm.Value,
		Severity:  norm.Severity,
		Source:    norm.Source,
		Enabled:   enabled,
		Note:      norm.Note,
		CreatedBy: &adminID,
	}
	if err := store.DB.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, classifyLinkBlocklistDBError(err)
	}

	apiservice.InvalidateLinkMatcherCache()
	writeLinkBlocklistAudit(adminID, "link_rule_create", row.ID, nil, &row, clientIP, userAgent)
	return &row, nil
}

// UpdateLinkBlocklistRule 修改（含启停 / 编辑）。
func UpdateLinkBlocklistRule(
	ctx context.Context,
	adminID, id int64,
	p LinkBlocklistRulePayload,
	clientIP, userAgent string,
) (*model.LinkBlocklistRule, error) {
	if store.DB == nil {
		return nil, errors.New("db unavailable")
	}

	var before model.LinkBlocklistRule
	if err := store.DB.First(&before, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	norm, err := normalizeLinkBlocklistPayload(p)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{
		"kind":     norm.Kind,
		"value":    norm.Value,
		"severity": norm.Severity,
		"source":   norm.Source,
		"note":     norm.Note,
	}
	if p.Enabled != nil {
		updates["enabled"] = *p.Enabled
	}

	if err := store.DB.WithContext(ctx).
		Model(&model.LinkBlocklistRule{}).
		Where("id = ?", id).
		Updates(updates).Error; err != nil {
		return nil, classifyLinkBlocklistDBError(err)
	}

	var after model.LinkBlocklistRule
	_ = store.DB.First(&after, "id = ?", id).Error

	apiservice.InvalidateLinkMatcherCache()
	writeLinkBlocklistAudit(adminID, "link_rule_update", id, &before, &after, clientIP, userAgent)
	return &after, nil
}

// DeleteLinkBlocklistRule 删除单条。
func DeleteLinkBlocklistRule(
	ctx context.Context,
	adminID, id int64,
	clientIP, userAgent string,
) error {
	if store.DB == nil {
		return errors.New("db unavailable")
	}
	var before model.LinkBlocklistRule
	if err := store.DB.First(&before, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := store.DB.WithContext(ctx).Delete(&model.LinkBlocklistRule{}, "id = ?", id).Error; err != nil {
		return err
	}
	apiservice.InvalidateLinkMatcherCache()
	writeLinkBlocklistAudit(adminID, "link_rule_delete", id, &before, nil, clientIP, userAgent)
	return nil
}

// BatchLinkBlocklistAction 批量启用 / 禁用 / 删除。
type BatchLinkBlocklistAction string

const (
	BatchLinkBlocklistEnable  BatchLinkBlocklistAction = "enable"
	BatchLinkBlocklistDisable BatchLinkBlocklistAction = "disable"
	BatchLinkBlocklistDelete  BatchLinkBlocklistAction = "delete"
)

// BatchUpdateLinkBlocklistRules 批量操作。返回影响行数。
func BatchUpdateLinkBlocklistRules(
	ctx context.Context,
	adminID int64,
	ids []int64,
	action BatchLinkBlocklistAction,
	clientIP, userAgent string,
) (int64, error) {
	if store.DB == nil {
		return 0, errors.New("db unavailable")
	}
	if len(ids) == 0 {
		return 0, nil
	}
	var affected int64
	switch action {
	case BatchLinkBlocklistEnable:
		tx := store.DB.WithContext(ctx).
			Model(&model.LinkBlocklistRule{}).
			Where("id IN ?", ids).
			Update("enabled", true)
		if tx.Error != nil {
			return 0, tx.Error
		}
		affected = tx.RowsAffected
	case BatchLinkBlocklistDisable:
		tx := store.DB.WithContext(ctx).
			Model(&model.LinkBlocklistRule{}).
			Where("id IN ?", ids).
			Update("enabled", false)
		if tx.Error != nil {
			return 0, tx.Error
		}
		affected = tx.RowsAffected
	case BatchLinkBlocklistDelete:
		tx := store.DB.WithContext(ctx).
			Where("id IN ?", ids).
			Delete(&model.LinkBlocklistRule{})
		if tx.Error != nil {
			return 0, tx.Error
		}
		affected = tx.RowsAffected
	default:
		return 0, fmt.Errorf("unknown action: %s", action)
	}

	apiservice.InvalidateLinkMatcherCache()
	apiservice.WriteAuditLog(apiservice.WriteAuditLogReq{
		EventType: "link_rule_batch_" + string(action),
		UserID:    &adminID,
		Detail: map[string]any{
			"ids":      ids,
			"affected": affected,
		},
		ClientIP:  clientIP,
		UserAgent: userAgent,
	})
	return affected, nil
}

// TestLinkBlocklist 在线测试一个 URL，零副作用。
func TestLinkBlocklist(rawURL string) apiservice.LinkCheckVerdict {
	return apiservice.TestLinkSafetyRule(rawURL)
}

// ---------- 批量导入 ----------

// LinkBlocklistImportResult 一次导入的结果汇总。
type LinkBlocklistImportResult struct {
	Created  int                          `json:"created"`
	Skipped  int                          `json:"skipped"`
	Failures []LinkBlocklistImportFailure `json:"failures,omitempty"`
}

// LinkBlocklistImportFailure 单条失败明细，便于运营人员排错。
type LinkBlocklistImportFailure struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// ImportLinkBlocklistRulesCSV 批量导入 CSV 格式黑名单规则。
// 字段顺序固定：kind,value,severity,source,enabled,note
// kind/value 必填；其它字段缺省时使用默认值（severity=malicious, source=manual, enabled=true）。
// 跳过空行；以 `#` 开头视为注释跳过；首行含 "kind" 视为表头自动跳过。
// 重复条目（同 kind+value）不计失败，只计 skipped。
//
// 性能：所有 normalize 在 Go 端完成，DB 端走单事务 + CreateInBatches +
// ON CONFLICT (kind,value) DO NOTHING；缓存失效与审计在末尾一次性触发，
// 避免万级规模时单条 INSERT + 万次 Pub/Sub 撑爆 HTTP 超时。
func ImportLinkBlocklistRulesCSV(
	ctx context.Context,
	adminID int64,
	csvBody string,
	clientIP, userAgent string,
) (LinkBlocklistImportResult, error) {
	if store.DB == nil {
		return LinkBlocklistImportResult{}, errors.New("db unavailable")
	}
	r := csv.NewReader(strings.NewReader(csvBody))
	r.FieldsPerRecord = -1

	result := LinkBlocklistImportResult{}

	// Pass 1: 解析 + normalize，校验失败的进 failures。
	type pendingRow struct {
		line int
		row  model.LinkBlocklistRule
	}
	pending := make([]pendingRow, 0, 1024)

	line := 0
	for {
		row, err := r.Read()
		line++
		if err == io.EOF {
			break
		}
		if err != nil {
			result.Failures = append(result.Failures, LinkBlocklistImportFailure{
				Line: line, Reason: err.Error(),
			})
			continue
		}
		if len(row) == 0 {
			continue
		}
		first := strings.TrimSpace(row[0])
		if first == "" || strings.HasPrefix(first, "#") {
			continue
		}
		if line == 1 && strings.EqualFold(first, "kind") {
			continue
		}

		payload := parseCSVRow(row)
		norm, err := normalizeLinkBlocklistPayload(payload)
		if err != nil {
			result.Failures = append(result.Failures, LinkBlocklistImportFailure{
				Line: line, Reason: err.Error(),
			})
			continue
		}
		enabled := true
		if payload.Enabled != nil {
			enabled = *payload.Enabled
		}
		pending = append(pending, pendingRow{
			line: line,
			row: model.LinkBlocklistRule{
				ID:        snowflake.GenID(),
				Kind:      norm.Kind,
				Value:     norm.Value,
				Severity:  norm.Severity,
				Source:    norm.Source,
				Enabled:   enabled,
				Note:      norm.Note,
				CreatedBy: &adminID,
			},
		})
	}

	// Pass 2: 批内去重，避免同一批里多条相同 (kind,value) 互相 conflict 触发额外锁。
	seen := make(map[string]struct{}, len(pending))
	deduped := make([]model.LinkBlocklistRule, 0, len(pending))
	dupInBatch := 0
	for _, p := range pending {
		key := p.row.Kind + "\x00" + p.row.Value
		if _, ok := seen[key]; ok {
			dupInBatch++
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, p.row)
	}

	// Pass 3: 一次事务批量 INSERT，ON CONFLICT (kind,value) DO NOTHING。
	var inserted int64
	if len(deduped) > 0 {
		tx := store.DB.WithContext(ctx).
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "kind"}, {Name: "value"}},
				DoNothing: true,
			}).
			CreateInBatches(deduped, 500)
		if tx.Error != nil {
			return result, tx.Error
		}
		inserted = tx.RowsAffected
	}

	result.Created = int(inserted)
	result.Skipped = (len(deduped) - int(inserted)) + dupInBatch

	if inserted > 0 {
		apiservice.InvalidateLinkMatcherCache()
	}
	apiservice.WriteAuditLog(apiservice.WriteAuditLogReq{
		EventType: "link_rule_import",
		UserID:    &adminID,
		Detail: map[string]any{
			"created":  result.Created,
			"skipped":  result.Skipped,
			"failures": len(result.Failures),
			"input":    len(pending) + len(result.Failures),
		},
		ClientIP:  clientIP,
		UserAgent: userAgent,
	})
	return result, nil
}

func parseCSVRow(row []string) LinkBlocklistRulePayload {
	get := func(i int) string {
		if i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	enabled := true
	if v := get(4); v != "" {
		enabled = !strings.EqualFold(v, "false") && v != "0"
	}
	return LinkBlocklistRulePayload{
		Kind:     get(0),
		Value:    get(1),
		Severity: get(2),
		Source:   get(3),
		Enabled:  &enabled,
		Note:     get(5),
	}
}

// ---------- 拦截统计 ----------

// LinkBlocklistStats 后台看板数据。
type LinkBlocklistStats struct {
	BlockedToday       int64                  `json:"blocked_today"`
	Blocked7d          int64                  `json:"blocked_7d"`
	Blocked30d         int64                  `json:"blocked_30d"`
	WarnedToday        int64                  `json:"warned_today"`
	Warned7d           int64                  `json:"warned_7d"`
	TopRules           []LinkBlocklistTopItem `json:"top_rules"`
	TopHosts           []LinkBlocklistTopItem `json:"top_hosts"`
	ActiveRulesCount   int64                  `json:"active_rules_count"`
	DisabledRulesCount int64                  `json:"disabled_rules_count"`
}

type LinkBlocklistTopItem struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// LoadLinkBlocklistStats 汇总看板数据。
// blocked_* / warned_* 走 audit_log（按 event_type 聚合）；
// TopRules 直接从 link_blocklist_rules.hit_count 取（命中聚合已 flush 到主表）；
// TopHosts 从 audit_log.detail->>'canonical_host' 聚合。
func LoadLinkBlocklistStats(ctx context.Context) (LinkBlocklistStats, error) {
	if store.DB == nil {
		return LinkBlocklistStats{}, errors.New("db unavailable")
	}
	now := time.Now()
	t1 := now.Add(-24 * time.Hour)
	t7 := now.Add(-7 * 24 * time.Hour)
	t30 := now.Add(-30 * 24 * time.Hour)

	out := LinkBlocklistStats{}

	countAudit := func(event string, since time.Time) int64 {
		var n int64
		_ = store.DB.WithContext(ctx).
			Model(&model.AuditLog{}).
			Where("event_type = ? AND created_at >= ?", event, since).
			Count(&n).Error
		return n
	}

	out.BlockedToday = countAudit("link_blocked", t1)
	out.Blocked7d = countAudit("link_blocked", t7)
	out.Blocked30d = countAudit("link_blocked", t30)
	out.WarnedToday = countAudit("link_warned", t1)
	out.Warned7d = countAudit("link_warned", t7)

	_ = store.DB.WithContext(ctx).
		Model(&model.LinkBlocklistRule{}).
		Where("enabled = ?", true).
		Count(&out.ActiveRulesCount).Error
	_ = store.DB.WithContext(ctx).
		Model(&model.LinkBlocklistRule{}).
		Where("enabled = ?", false).
		Count(&out.DisabledRulesCount).Error

	// TopRules: 直接 hit_count 倒序取前 10
	var topRules []model.LinkBlocklistRule
	if err := store.DB.WithContext(ctx).
		Where("hit_count > 0").
		Order("hit_count DESC").
		Limit(10).
		Find(&topRules).Error; err == nil {
		for _, r := range topRules {
			out.TopRules = append(out.TopRules, LinkBlocklistTopItem{
				Key:   r.Value,
				Count: r.HitCount,
			})
		}
	}

	// TopHosts: 7 日内 audit_log 聚合（detail->>'canonical_host'）
	type hostRow struct {
		Host  string
		Count int64
	}
	var hosts []hostRow
	if err := store.DB.WithContext(ctx).
		Raw(`SELECT detail->>'canonical_host' AS host, COUNT(*) AS count
		     FROM audit_logs
		     WHERE event_type = 'link_blocked' AND created_at >= ?
		     GROUP BY host
		     ORDER BY count DESC
		     LIMIT 10`, t7).
		Scan(&hosts).Error; err == nil {
		for _, h := range hosts {
			if h.Host == "" {
				continue
			}
			out.TopHosts = append(out.TopHosts, LinkBlocklistTopItem{
				Key:   h.Host,
				Count: h.Count,
			})
		}
	}

	return out, nil
}

// LoadLinkBlocklistRecentEvents 最近 N 条命中事件，给后台"最近拦截"列表。
func LoadLinkBlocklistRecentEvents(ctx context.Context, limit int) ([]model.AuditLog, error) {
	if store.DB == nil {
		return nil, errors.New("db unavailable")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []model.AuditLog
	if err := store.DB.WithContext(ctx).
		Where("event_type IN ?", []string{"link_blocked", "link_warned"}).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ---------- 内部辅助 ----------

// ErrNotFound 通用未找到错误。
var ErrNotFound = errors.New("not found")

type normalizedLinkRulePayload struct {
	Kind     string
	Value    string
	Severity string
	Source   string
	Note     string
}

func normalizeLinkBlocklistPayload(p LinkBlocklistRulePayload) (normalizedLinkRulePayload, error) {
	kind := strings.ToLower(strings.TrimSpace(p.Kind))
	value := strings.TrimSpace(p.Value)
	severity := strings.ToLower(strings.TrimSpace(p.Severity))
	source := strings.ToLower(strings.TrimSpace(p.Source))
	note := strings.TrimSpace(p.Note)

	switch urlguard.RuleKind(kind) {
	case urlguard.RuleExactDomain, urlguard.RuleWildcard, urlguard.RuleRegex, urlguard.RuleKeyword:
	default:
		return normalizedLinkRulePayload{}, fmt.Errorf("kind 无效: %s（合法值 domain/wildcard/regex/keyword）", kind)
	}
	switch urlguard.Severity(severity) {
	case urlguard.SeverityMalicious, urlguard.SeveritySuspicious:
	case "":
		severity = string(urlguard.SeverityMalicious)
	default:
		return normalizedLinkRulePayload{}, fmt.Errorf("severity 无效: %s（合法值 malicious/suspicious）", severity)
	}
	if source == "" {
		source = "manual"
	}
	if value == "" {
		return normalizedLinkRulePayload{}, errors.New("value 不能为空")
	}

	// 规则值校验
	switch urlguard.RuleKind(kind) {
	case urlguard.RuleExactDomain, urlguard.RuleWildcard:
		v := strings.TrimPrefix(strings.ToLower(value), "*.")
		if strings.Contains(v, "/") || strings.Contains(v, "://") {
			return normalizedLinkRulePayload{}, errors.New("domain/wildcard 规则值不应含路径或 scheme")
		}
		value = v
	case urlguard.RuleRegex:
		// Go regexp 用 RE2 算法，时间复杂度 O(n*m)，不存在 catastrophic backtracking；
		// 但极长正则在匹配时仍会拖慢，加保守长度上限防止误操作。
		if len(value) > 256 {
			return normalizedLinkRulePayload{}, errors.New("regex 长度不应超过 256 字符")
		}
		if _, err := regexp.Compile(value); err != nil {
			return normalizedLinkRulePayload{}, fmt.Errorf("regex 编译失败: %v", err)
		}
	case urlguard.RuleKeyword:
		value = strings.ToLower(value)
	}

	return normalizedLinkRulePayload{
		Kind: kind, Value: value, Severity: severity, Source: source, Note: note,
	}, nil
}

func classifyLinkBlocklistDBError(err error) error {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique constraint") {
		return errors.New("规则已存在（同 kind+value 重复）")
	}
	return err
}

func writeLinkBlocklistAudit(
	adminID int64,
	event string,
	ruleID int64,
	before, after *model.LinkBlocklistRule,
	clientIP, userAgent string,
) {
	detail := map[string]any{
		"rule_id": ruleID,
		"at":      time.Now().UTC(),
	}
	if before != nil {
		detail["before"] = before
	}
	if after != nil {
		detail["after"] = after
	}
	apiservice.WriteAuditLog(apiservice.WriteAuditLogReq{
		EventType: event,
		UserID:    &adminID,
		Detail:    detail,
		ClientIP:  clientIP,
		UserAgent: userAgent,
	})
}

package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/api/service/identity"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"gorm.io/gorm"
)

// ConnectorNotifyEventKey 是升级失败告知走触达链路时的 event_key。
const ConnectorNotifyEventKey = "connector_upgrade_failed"

// connectorUnsupportedErrorCode 是"平台不支持"而非故障；默认不打扰这批用户。
const connectorUnsupportedErrorCode = "WINDOWS_UPGRADE_UNSUPPORTED"

// connectorNotifyMaxUsers 单次手动触发的收件人上限，避免后台误勾成群发。
const connectorNotifyMaxUsers = 200

// connectorProblemIDChunk 是 IN 查询的分片大小。
const connectorProblemIDChunk = 500

// 通知渠道。
const (
	ConnectorNotifyChannelEmail = "email"
	ConnectorNotifyChannelSMS   = "sms"
	ConnectorNotifyChannelAuto  = "auto"
)

// 逐人结果状态。sent/failed/skipped 与 reach_send_logs 对齐，另加两个后台专用状态。
const (
	ConnectorNotifyStatusDuplicate     = "duplicate"
	ConnectorNotifyStatusNotConfigured = "not_configured"
)

// defaultConnectorProblemStatuses 是"有问题"的默认口径：失败与回滚。
var defaultConnectorProblemStatuses = []string{model.UpgradeReportFailed, model.UpgradeReportRolledBack}

// connectorHealthyStatuses 是"已自愈"的判据：机器后来又报了成功。
var connectorHealthyStatuses = map[string]bool{
	model.UpgradeReportSuccess:   true,
	model.UpgradeReportInstalled: true,
}

type ListConnectorProblemUsersReq struct {
	Version            string
	ClientType         string
	Statuses           []string
	IncludeUnsupported bool
	Page               int
	PageSize           int
}

// ConnectorProblemUser 是一名用户在指定版本上的升级问题汇总。
// 手机号只给末四位脱敏串；真实号仅在发送时由服务端解密使用。
type ConnectorProblemUser struct {
	UserID         int64    `json:"user_id,string"`
	Nickname       string   `json:"nickname"`
	Email          string   `json:"email"`
	PhoneMasked    string   `json:"phone_masked"`
	AgentIDs       []string `json:"agent_ids"`
	FailedHosts    int      `json:"failed_hosts"`
	ErrorCodes     []string `json:"error_codes"`
	LastReportedAt string   `json:"last_reported_at"`
}

type ListConnectorProblemUsersResult struct {
	Total int64                  `json:"total"`
	Users []ConnectorProblemUser `json:"users"`
}

// problemHost 是一台仍处于问题态的机器。
type problemHost struct {
	agentID    int64
	errorCode  string
	reportedAt time.Time
}

// ListConnectorProblemUsers 汇总指定版本上仍未自愈的升级问题机器，并按 owner 归并到用户。
//
// 口径（与后台"问题用户"页一致）：
//   - 同一台机器（install_id 优先，其次 host_name，再次 agent_id）在该版本只看最新一条上报；
//   - 该机器在任意版本上更晚报过 success/installed 的，视为已自愈，剔除；
//   - 默认排除 WINDOWS_UPGRADE_UNSUPPORTED（平台不支持不是故障），可用参数包含进来。
func ListConnectorProblemUsers(req ListConnectorProblemUsersReq) (*ListConnectorProblemUsersResult, *errcode.ErrCode) {
	version := strings.TrimSpace(req.Version)
	if version == "" {
		return nil, &errcode.ErrBadRequest
	}
	statuses := normalizeProblemStatuses(req.Statuses)

	hosts, ec := collectConnectorProblemHosts(version, strings.TrimSpace(req.ClientType), statuses, req.IncludeUnsupported)
	if ec != nil {
		return nil, ec
	}
	users, ec := groupProblemHostsByOwner(hosts)
	if ec != nil {
		return nil, ec
	}

	total := int64(len(users))
	page, pageSize := req.Page, req.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start > len(users) {
		start = len(users)
	}
	end := start + pageSize
	if end > len(users) {
		end = len(users)
	}
	return &ListConnectorProblemUsersResult{Total: total, Users: users[start:end]}, nil
}

func normalizeProblemStatuses(raw []string) map[string]bool {
	out := map[string]bool{}
	for _, s := range raw {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			out[s] = true
		}
	}
	if len(out) == 0 {
		for _, s := range defaultConnectorProblemStatuses {
			out[s] = true
		}
	}
	return out
}

// collectConnectorProblemHosts 返回 hostKey -> 仍处于问题态的机器。
func collectConnectorProblemHosts(version, clientType string, statuses map[string]bool, includeUnsupported bool) (map[string]problemHost, *errcode.ErrCode) {
	db := store.DB.Model(&model.ConnectorUpgradeReport{}).Where("to_version = ?", version)
	if clientType != "" {
		db = db.Where("client_type = ?", clientType)
	}
	var reports []model.ConnectorUpgradeReport
	if err := db.Order("reported_at ASC").Find(&reports).Error; err != nil {
		return nil, &errcode.ErrInternal
	}

	// 同一台机器只保留该版本的最新一条：同版本内后来报成功的天然被覆盖掉。
	latest := make(map[string]model.ConnectorUpgradeReport, len(reports))
	for _, r := range reports {
		key := hostKeyOf(&r)
		if prev, ok := latest[key]; ok && prev.ReportedAt.After(r.ReportedAt) {
			continue
		}
		latest[key] = r
	}

	candidates := make(map[string]problemHost, len(latest))
	agentIDSet := map[int64]struct{}{}
	for key, r := range latest {
		if !statuses[r.Status] {
			continue
		}
		code := ""
		if r.ErrorCode != nil {
			code = strings.TrimSpace(*r.ErrorCode)
		}
		if !includeUnsupported && code == connectorUnsupportedErrorCode {
			continue
		}
		candidates[key] = problemHost{agentID: r.AgentID, errorCode: code, reportedAt: r.ReportedAt}
		agentIDSet[r.AgentID] = struct{}{}
	}
	if len(candidates) == 0 {
		return candidates, nil
	}

	if err := dropSelfHealedHosts(candidates, agentIDSet, clientType); err != nil {
		return nil, &errcode.ErrInternal
	}
	return candidates, nil
}

// dropSelfHealedHosts 剔除后来（可能在更高版本上）又报成功的机器：已经自愈的不打扰。
func dropSelfHealedHosts(candidates map[string]problemHost, agentIDSet map[int64]struct{}, clientType string) error {
	agentIDs := make([]int64, 0, len(agentIDSet))
	for id := range agentIDSet {
		agentIDs = append(agentIDs, id)
	}

	newest := make(map[string]model.ConnectorUpgradeReport, len(candidates))
	for _, chunk := range chunkInt64(agentIDs, connectorProblemIDChunk) {
		db := store.DB.Model(&model.ConnectorUpgradeReport{}).Where("agent_id IN ?", chunk)
		if clientType != "" {
			db = db.Where("client_type = ?", clientType)
		}
		var rows []model.ConnectorUpgradeReport
		if err := db.Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			key := hostKeyOf(&r)
			if _, watched := candidates[key]; !watched {
				continue
			}
			if prev, ok := newest[key]; ok && prev.ReportedAt.After(r.ReportedAt) {
				continue
			}
			newest[key] = r
		}
	}
	for key, r := range newest {
		if connectorHealthyStatuses[r.Status] {
			delete(candidates, key)
		}
	}
	return nil
}

// groupProblemHostsByOwner 把问题机器按 agent 的 owner 归并成用户列表，按最后上报时间倒序。
func groupProblemHostsByOwner(hosts map[string]problemHost) ([]ConnectorProblemUser, *errcode.ErrCode) {
	if len(hosts) == 0 {
		return []ConnectorProblemUser{}, nil
	}
	agentIDSet := map[int64]struct{}{}
	for _, h := range hosts {
		agentIDSet[h.agentID] = struct{}{}
	}
	agentIDs := make([]int64, 0, len(agentIDSet))
	for id := range agentIDSet {
		agentIDs = append(agentIDs, id)
	}

	owners := make(map[int64]int64, len(agentIDs))
	for _, chunk := range chunkInt64(agentIDs, connectorProblemIDChunk) {
		var rows []model.Agent
		if err := store.DB.Model(&model.Agent{}).Select("id, owner_id").Where("id IN ?", chunk).Find(&rows).Error; err != nil {
			return nil, &errcode.ErrInternal
		}
		for _, a := range rows {
			owners[a.ID] = a.OwnerID
		}
	}

	type accum struct {
		agents     map[int64]struct{}
		errorCodes map[string]struct{}
		hosts      int
		lastAt     time.Time
	}
	byUser := map[int64]*accum{}
	for _, h := range hosts {
		ownerID, ok := owners[h.agentID]
		if !ok || ownerID <= 0 {
			// agent 已删除或没有 owner：没有可触达的人，跳过。
			continue
		}
		a := byUser[ownerID]
		if a == nil {
			a = &accum{agents: map[int64]struct{}{}, errorCodes: map[string]struct{}{}}
			byUser[ownerID] = a
		}
		a.agents[h.agentID] = struct{}{}
		if h.errorCode != "" {
			a.errorCodes[h.errorCode] = struct{}{}
		}
		a.hosts++
		if h.reportedAt.After(a.lastAt) {
			a.lastAt = h.reportedAt
		}
	}
	if len(byUser) == 0 {
		return []ConnectorProblemUser{}, nil
	}

	userIDs := make([]int64, 0, len(byUser))
	for id := range byUser {
		userIDs = append(userIDs, id)
	}
	profiles := make(map[int64]model.User, len(userIDs))
	for _, chunk := range chunkInt64(userIDs, connectorProblemIDChunk) {
		var rows []model.User
		if err := store.DB.Model(&model.User{}).
			Select("id, nickname, email, phone_e164, phone_last4, status").
			Where("id IN ?", chunk).Find(&rows).Error; err != nil {
			return nil, &errcode.ErrInternal
		}
		for _, u := range rows {
			profiles[u.ID] = u
		}
	}

	out := make([]ConnectorProblemUser, 0, len(byUser))
	for userID, a := range byUser {
		u := profiles[userID]
		agentIDs := make([]string, 0, len(a.agents))
		for id := range a.agents {
			agentIDs = append(agentIDs, strconv.FormatInt(id, 10))
		}
		sort.Strings(agentIDs)
		codes := make([]string, 0, len(a.errorCodes))
		for c := range a.errorCodes {
			codes = append(codes, c)
		}
		sort.Strings(codes)
		out = append(out, ConnectorProblemUser{
			UserID:         userID,
			Nickname:       u.Nickname,
			Email:          u.Email,
			PhoneMasked:    MaskUserPhone(u),
			AgentIDs:       agentIDs,
			FailedHosts:    a.hosts,
			ErrorCodes:     codes,
			LastReportedAt: a.lastAt.Format(time.RFC3339),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastReportedAt != out[j].LastReportedAt {
			return out[i].LastReportedAt > out[j].LastReportedAt
		}
		return out[i].UserID < out[j].UserID
	})
	return out, nil
}

// MaskUserPhone 返回 ****8000 形式的脱敏号；没有手机号时返回空串。
func MaskUserPhone(u model.User) string {
	last4 := strings.TrimSpace(u.PhoneLast4)
	if last4 == "" {
		plain := strings.TrimSpace(u.PhoneE164)
		if plain == "" {
			return ""
		}
		r := []rune(plain)
		if len(r) > 4 {
			last4 = string(r[len(r)-4:])
		} else {
			last4 = plain
		}
	}
	return "****" + last4
}

func chunkInt64(ids []int64, size int) [][]int64 {
	if size <= 0 || len(ids) <= size {
		return [][]int64{ids}
	}
	out := make([][]int64, 0, (len(ids)+size-1)/size)
	for start := 0; start < len(ids); start += size {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		out = append(out, ids[start:end])
	}
	return out
}

// --- 手动触发通知 ---

type NotifyConnectorProblemUsersReq struct {
	Version   string
	UserIDs   []int64
	Channel   string
	Title     string
	Body      string
	CreatedBy int64
}

type ConnectorNotifyResult struct {
	UserID  int64  `json:"user_id,string"`
	Channel string `json:"channel"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	TaskID  int64  `json:"task_id,string,omitempty"`
}

// NotifyConnectorProblemUsers 按后台勾选逐个发送升级失败告知。
// 幂等键为 connector_upgrade:{version}:{user_id}:{channel}，重复触发只回读既有任务不重发。
func NotifyConnectorProblemUsers(ctx context.Context, req NotifyConnectorProblemUsersReq) ([]ConnectorNotifyResult, *errcode.ErrCode) {
	if ctx == nil {
		ctx = context.Background()
	}
	version := strings.TrimSpace(req.Version)
	body := strings.TrimSpace(req.Body)
	channel := strings.ToLower(strings.TrimSpace(req.Channel))
	if channel == "" {
		channel = ConnectorNotifyChannelAuto
	}
	switch channel {
	case ConnectorNotifyChannelEmail, ConnectorNotifyChannelSMS, ConnectorNotifyChannelAuto:
	default:
		return nil, &errcode.ErrBadRequest
	}
	if version == "" || body == "" || len(req.UserIDs) == 0 || len(req.UserIDs) > connectorNotifyMaxUsers {
		return nil, &errcode.ErrBadRequest
	}

	results := make([]ConnectorNotifyResult, 0, len(req.UserIDs))
	seen := map[int64]bool{}
	for _, userID := range req.UserIDs {
		if userID <= 0 || seen[userID] {
			continue
		}
		seen[userID] = true
		results = append(results, notifyOneConnectorProblemUser(ctx, req, version, channel, body, userID))
	}
	return results, nil
}

func notifyOneConnectorProblemUser(ctx context.Context, req NotifyConnectorProblemUsersReq, version, channel, body string, userID int64) ConnectorNotifyResult {
	out := ConnectorNotifyResult{UserID: userID, Channel: channel, Status: model.ReachSendStatusFailed}

	var user model.User
	if err := store.DB.WithContext(ctx).
		Select("id, nickname, email, status, region, phone_e164, phone_country, phone_cipher, phone_last4").
		Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			out.Error = "user not found"
		} else {
			out.Error = err.Error()
		}
		return out
	}
	if user.Status != model.UserStatusActive {
		out.Status = model.ReachSendStatusSkipped
		out.Error = "user not active"
		return out
	}

	directReq := normalizeDirectReachReq(SendDirectUserReachReq{
		UserID:    userID,
		Title:     strings.TrimSpace(req.Title),
		LongText:  body,
		EventKey:  ConnectorNotifyEventKey,
		DedupKey:  fmt.Sprintf("connector_upgrade:%s:%d:%s", version, userID, channel),
		CreatedBy: req.CreatedBy,
	})

	task, created, err := createDirectReachTask(ctx, directReq, user)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.TaskID = task.ID
	if !created {
		out.Status = ConnectorNotifyStatusDuplicate
		return out
	}

	order := []string{channel}
	if channel == ConnectorNotifyChannelAuto {
		order = []string{ConnectorNotifyChannelEmail, ConnectorNotifyChannelSMS}
	}

	attempts := make([]DirectUserReachAttempt, 0, len(order))
	sent := ""
	notConfigured := false
	lastErr := ""
	for _, ch := range order {
		attempt := attemptConnectorNotifyChannel(ctx, task.ID, user, directReq, ch)
		attempts = append(attempts, attempt)
		if attempt.Status == model.ReachSendStatusSent {
			sent = ch
			break
		}
		if attempt.Error != "" {
			lastErr = attempt.Error
		}
		if attempt.Status == ConnectorNotifyStatusNotConfigured {
			notConfigured = true
		}
	}

	result := &SendDirectUserReachResult{Task: task, Attempts: attempts, Status: model.ReachStatusFailed}
	if sent != "" {
		result.Channel = sent
		result.Status = model.ReachStatusSent
	}
	if _, err := finishDirectReachTask(ctx, task.ID, order, result); err != nil {
		lastErr = err.Error()
	}

	switch {
	case sent != "":
		out.Channel = sent
		out.Status = model.ReachSendStatusSent
	case notConfigured:
		out.Status = ConnectorNotifyStatusNotConfigured
		out.Error = lastErr
	default:
		out.Status = model.ReachSendStatusFailed
		out.Error = lastErr
	}
	return out
}

// attemptConnectorNotifyChannel 投递单个渠道并落 reach_send_logs。
// not_configured 也写成 failed 日志（带原因），保证后台能追溯到那次点击。
func attemptConnectorNotifyChannel(ctx context.Context, taskID int64, user model.User, req SendDirectUserReachReq, channel string) DirectUserReachAttempt {
	attempt := DirectUserReachAttempt{Channel: channel}

	var deliver func() error
	switch channel {
	case ConnectorNotifyChannelEmail:
		to := strings.TrimSpace(user.Email)
		if to == "" {
			attempt.Status = model.ReachSendStatusSkipped
			attempt.Error = "user has no email"
			return attempt
		}
		deliver = func() error {
			return SendReachEmailByTemplate(ReachEmailTemplateID(), connectorNotifyEmailVars(user, req), to)
		}
	case ConnectorNotifyChannelSMS:
		phone, countryCode, phoneErr := directReachPhone(user)
		if phoneErr != nil || phone == "" {
			attempt.Status = model.ReachSendStatusSkipped
			attempt.Error = "user has no usable phone"
			if phoneErr != nil {
				attempt.Error = phoneErr.Error()
			}
			return attempt
		}
		deliver = func() error {
			return sendDirectReachSMS(ctx, ReachSMSRequest{
				UserID:      user.ID,
				PhoneE164:   phone,
				CountryCode: countryCode,
				Region:      user.Region,
				Text:        req.ShortText,
				Kind:        identity.SmsTextKindNotify,
			})
		}
	default:
		attempt.Status = model.ReachSendStatusSkipped
		attempt.Error = "unsupported channel"
		return attempt
	}

	logRow, err := createDirectReachSendLog(ctx, taskID, user, channel)
	if err != nil {
		attempt.Status = model.ReachSendStatusFailed
		attempt.Error = err.Error()
		return attempt
	}
	attempt.LogID = logRow.ID

	if err := deliver(); err != nil {
		attempt.Status = model.ReachSendStatusFailed
		attempt.Error = err.Error()
		if isConnectorNotifyNotConfigured(err) {
			attempt.Status = ConnectorNotifyStatusNotConfigured
		}
		markReachSendLog(ctx, logRow.ID, model.ReachSendStatusFailed, err.Error())
		return attempt
	}
	attempt.Status = model.ReachSendStatusSent
	markReachSendLog(ctx, logRow.ID, model.ReachSendStatusSent, "")
	return attempt
}

func isConnectorNotifyNotConfigured(err error) bool {
	return errors.Is(err, ErrReachSMSNotConfigured) ||
		errors.Is(err, ErrAliEmailTemplateNotConfigured) ||
		errors.Is(err, identity.ErrProviderNotConfigured) ||
		errors.Is(err, identity.ErrSmsTemplateNotConfigured)
}

func connectorNotifyEmailVars(user model.User, req SendDirectUserReachReq) map[string]string {
	return map[string]string{
		"name":    html.EscapeString(connectorNotifyDisplayName(user)),
		"body":    directReachMarkdownHTML(req.LongText),
		"title":   html.EscapeString(req.Title),
		"subject": req.Title,
	}
}

func connectorNotifyDisplayName(user model.User) string {
	if n := strings.TrimSpace(user.Nickname); n != "" {
		return n
	}
	return "Grix 用户"
}

// --- 发送前预览 ---

type ConnectorNotifyPreview struct {
	EmailSubject string `json:"email_subject"`
	EmailHTML    string `json:"email_html"`
	EmailError   string `json:"email_error,omitempty"`
	SMSText      string `json:"sms_text"`
	SMSError     string `json:"sms_error,omitempty"`
}

// PreviewConnectorNotify 渲染后台勾选后将要发出的邮件正文与短信文案。
// sampleUserID 可选：给了就用该用户的昵称填 {name}，否则用占位名。
func PreviewConnectorNotify(title, body string, sampleUserID int64) (*ConnectorNotifyPreview, *errcode.ErrCode) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, &errcode.ErrBadRequest
	}
	req := normalizeDirectReachReq(SendDirectUserReachReq{
		UserID:   1, // 仅用于通过校验，预览不触碰投递链路
		Title:    strings.TrimSpace(title),
		LongText: body,
	})

	var user model.User
	if sampleUserID > 0 {
		store.DB.Model(&model.User{}).Select("id, nickname").Where("id = ?", sampleUserID).First(&user)
	}

	out := &ConnectorNotifyPreview{SMSText: req.ShortText}
	subject, emailHTML, err := RenderReachEmailTemplate(ReachEmailTemplateID(), connectorNotifyEmailVars(user, req))
	if err != nil {
		out.EmailError = err.Error()
	} else {
		if s := strings.TrimSpace(req.Title); s != "" {
			subject = s
		}
		out.EmailSubject = subject
		out.EmailHTML = emailHTML
	}
	if err := connectorNotifySMSConfigError(); err != nil {
		out.SMSError = err.Error()
	}
	return out, nil
}

// connectorNotifySMSConfigError 只检查通知短信模板号是否已配，不发真短信。
func connectorNotifySMSConfigError() error {
	settings, err := systemsetting.GetSmsSettings()
	if err != nil {
		return err
	}
	if strings.TrimSpace(settings.Aliyun.TemplateCodeNotify) == "" {
		return ErrReachSMSNotConfigured
	}
	return nil
}

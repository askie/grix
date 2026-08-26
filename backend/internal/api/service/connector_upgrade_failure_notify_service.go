package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/api/service/identity"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
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
	// Page / PageSize 是 clamp 之后真正生效的值，供接口如实回显（请求 page_size=500 实际只会返 20）。
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

// problemHost 是一台仍处于问题态的机器。
// install_id / host_name 都留着：判"是否已自愈"时要按任一标识去匹配后续的成功上报，
// 只按 agent_id 匹配会漏掉重新注册过 agent 的机器。
type problemHost struct {
	agentID    int64
	installID  string
	hostName   string
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

	clientType := strings.TrimSpace(req.ClientType)
	hosts, ec := collectConnectorProblemHosts(version, clientType, statuses, req.IncludeUnsupported)
	if ec != nil {
		return nil, ec
	}
	owners, ec := resolveProblemHostOwners(hosts)
	if ec != nil {
		return nil, ec
	}
	if err := dropSelfHealedHosts(hosts, owners, clientType); err != nil {
		return nil, &errcode.ErrInternal
	}
	users, ec := groupProblemHostsByOwner(hosts, owners.agentOwner)
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
	return &ListConnectorProblemUsersResult{
		Total:    total,
		Users:    users[start:end],
		Page:     page,
		PageSize: pageSize,
	}, nil
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
		key := problemHostKey(&r)
		if prev, ok := latest[key]; ok && prev.ReportedAt.After(r.ReportedAt) {
			continue
		}
		latest[key] = r
	}

	candidates := make(map[string]problemHost, len(latest))
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
		candidates[key] = problemHost{
			agentID:    r.AgentID,
			installID:  derefTrimmed(r.InstallID),
			hostName:   derefTrimmed(r.HostName),
			errorCode:  code,
			reportedAt: r.ReportedAt,
		}
	}
	return candidates, nil
}

// problemHostKey 是本功能用的机器键，和仓库既有的 hostKeyOf 有一处刻意的差别：
// host_name 兜底时带上 agent_id。主机名不全局唯一（"localhost"、"MacBook-Pro.local"），
// 纯 host_name 作键会把不同用户的同名机器并成一台 —— 在统计里只是多算一次，
// 在这里会让一个用户的成功上报直接吃掉另一个用户的失败记录，导致该用户收不到通知。
// 跨 agent 认同一台机器的场景由自愈那一趟按 install_id / host_name 匹配来覆盖。
func problemHostKey(r *model.ConnectorUpgradeReport) string {
	if id := derefTrimmed(r.InstallID); id != "" {
		return "i:" + id
	}
	if host := derefTrimmed(r.HostName); host != "" {
		return fmt.Sprintf("a:%d|h:%s", r.AgentID, host)
	}
	return fmt.Sprintf("a:%d", r.AgentID)
}

func derefTrimmed(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

// problemHostOwners 是候选机器的归属关系：agentOwner 覆盖候选 agent 以及这些 owner 名下
// 的其它 agent（机器换过 agent 时要认得出来），ownerAgentIDs 是这些 agent 的全集。
type problemHostOwners struct {
	agentOwner    map[int64]int64
	ownerAgentIDs []int64
}

// resolveProblemHostOwners 两跳查出归属：候选 agent -> owner -> 该 owner 的全部 agent。
func resolveProblemHostOwners(hosts map[string]problemHost) (*problemHostOwners, *errcode.ErrCode) {
	out := &problemHostOwners{agentOwner: map[int64]int64{}}
	if len(hosts) == 0 {
		return out, nil
	}
	candidateAgents := map[int64]struct{}{}
	for _, h := range hosts {
		candidateAgents[h.agentID] = struct{}{}
	}

	ownerIDs := map[int64]struct{}{}
	for _, chunk := range chunkInt64(mapKeysInt64Set(candidateAgents), connectorProblemIDChunk) {
		var rows []model.Agent
		if err := store.DB.Model(&model.Agent{}).Select("id, owner_id").Where("id IN ?", chunk).Find(&rows).Error; err != nil {
			return nil, &errcode.ErrInternal
		}
		for _, a := range rows {
			out.agentOwner[a.ID] = a.OwnerID
			if a.OwnerID > 0 {
				ownerIDs[a.OwnerID] = struct{}{}
			}
		}
	}
	if len(ownerIDs) == 0 {
		// agent 全被删光时也要留下候选 agent 自己，否则自愈的第二路整段跳过，
		// 已经修好的机器会被重复打扰。
		out.ownerAgentIDs = mapKeysInt64Set(candidateAgents)
		return out, nil
	}

	agentIDs := map[int64]struct{}{}
	for _, chunk := range chunkInt64(mapKeysInt64Set(ownerIDs), connectorProblemIDChunk) {
		var rows []model.Agent
		if err := store.DB.Model(&model.Agent{}).Select("id, owner_id").Where("owner_id IN ?", chunk).Find(&rows).Error; err != nil {
			return nil, &errcode.ErrInternal
		}
		for _, a := range rows {
			out.agentOwner[a.ID] = a.OwnerID
			agentIDs[a.ID] = struct{}{}
		}
	}
	for id := range candidateAgents {
		agentIDs[id] = struct{}{}
	}
	out.ownerAgentIDs = mapKeysInt64Set(agentIDs)
	return out, nil
}

// dropSelfHealedHosts 剔除后来（可能在更高版本、也可能换了 agent）又报成功的机器。
//
// 两路匹配：
//   - install_id 全库匹配：安装标识本身唯一，跨 owner 也是同一台机器。
//   - 同 owner 的 agent 范围内，再按 agent_id 或 host_name 匹配。host_name 必须限定在
//     owner 内：主机名不全局唯一（"localhost"、"MacBook-Pro.local"），拿全库同名机器的
//     成功上报去抵消候选，会让无关用户直接收不到通知。
//
// 只拉 success/installed 的行，比拉全量历史省得多。
func dropSelfHealedHosts(candidates map[string]problemHost, owners *problemHostOwners, clientType string) error {
	if len(candidates) == 0 {
		return nil
	}
	byInstall := map[string][]string{}
	byOwnerHost := map[string][]string{}
	byAgent := map[int64][]string{}
	for key, h := range candidates {
		if h.installID != "" {
			byInstall[h.installID] = append(byInstall[h.installID], key)
		}
		if h.hostName != "" {
			ownerHost := ownerHostKey(owners.agentOwner[h.agentID], h.agentID, h.hostName)
			byOwnerHost[ownerHost] = append(byOwnerHost[ownerHost], key)
		}
		byAgent[h.agentID] = append(byAgent[h.agentID], key)
	}

	healed := map[string]struct{}{}
	mark := func(keys []string, reportedAt time.Time) {
		for _, key := range keys {
			if h, ok := candidates[key]; ok && reportedAt.After(h.reportedAt) {
				healed[key] = struct{}{}
			}
		}
	}

	loadHealthy := func(apply func(*gorm.DB) *gorm.DB) ([]model.ConnectorUpgradeReport, error) {
		db := store.DB.Model(&model.ConnectorUpgradeReport{}).
			Where("status IN ?", []string{model.UpgradeReportSuccess, model.UpgradeReportInstalled})
		if clientType != "" {
			db = db.Where("client_type = ?", clientType)
		}
		var rows []model.ConnectorUpgradeReport
		if err := apply(db).Find(&rows).Error; err != nil {
			return nil, err
		}
		return rows, nil
	}

	for _, chunk := range chunkStrings(mapKeysString(byInstall), connectorProblemIDChunk) {
		rows, err := loadHealthy(func(db *gorm.DB) *gorm.DB { return db.Where("install_id IN ?", chunk) })
		if err != nil {
			return err
		}
		for _, r := range rows {
			mark(byInstall[derefTrimmed(r.InstallID)], r.ReportedAt)
		}
	}

	for _, chunk := range chunkInt64(owners.ownerAgentIDs, connectorProblemIDChunk) {
		rows, err := loadHealthy(func(db *gorm.DB) *gorm.DB { return db.Where("agent_id IN ?", chunk) })
		if err != nil {
			return err
		}
		for _, r := range rows {
			mark(byAgent[r.AgentID], r.ReportedAt)
			if host := derefTrimmed(r.HostName); host != "" {
				mark(byOwnerHost[ownerHostKey(owners.agentOwner[r.AgentID], r.AgentID, host)], r.ReportedAt)
			}
		}
	}

	for key := range healed {
		delete(candidates, key)
	}
	return nil
}

// ownerHostKey 把主机名限定在 owner 内。agent 已删除（查不到 owner）时退回按 agent 隔离：
// 否则所有无主机器会挤进同一个 0 号桶，同名机器之间又能互相抵消。
func ownerHostKey(ownerID, agentID int64, hostName string) string {
	if ownerID <= 0 {
		return "a:" + strconv.FormatInt(agentID, 10) + "|" + hostName
	}
	return "o:" + strconv.FormatInt(ownerID, 10) + "|" + hostName
}

func mapKeysInt64Set(m map[int64]struct{}) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func mapKeysString(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// groupProblemHostsByOwner 把问题机器按 agent 的 owner 归并成用户列表，按最后上报时间倒序。
func groupProblemHostsByOwner(hosts map[string]problemHost, owners map[int64]int64) ([]ConnectorProblemUser, *errcode.ErrCode) {
	if len(hosts) == 0 {
		return []ConnectorProblemUser{}, nil
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
		// 迁移前的存量明文行兜底取末四位；不足四位的脏数据直接不展示，
		// 否则 "****" + 全号 会把整串号码当脱敏串下发。
		r := []rune(strings.TrimSpace(u.PhoneE164))
		if len(r) <= 4 {
			return ""
		}
		last4 = string(r[len(r)-4:])
	}
	return "****" + last4
}

func chunkStrings(values []string, size int) [][]string {
	if len(values) == 0 {
		return nil
	}
	if size <= 0 || len(values) <= size {
		return [][]string{values}
	}
	out := make([][]string, 0, (len(values)+size-1)/size)
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		out = append(out, values[start:end])
	}
	return out
}

func chunkInt64(ids []int64, size int) [][]int64 {
	if len(ids) == 0 {
		return nil
	}
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
		// 默认只发邮件。短信通道是休眠能力（模板号尚未报备），必须由调用方显式
		// 指定 sms/auto 才会走到，避免漏传 channel 时意外把人短信轰一遍。
		channel = ConnectorNotifyChannelEmail
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
		// 幂等键在投递前就占住了，所以整单失败的任务必须能重来一次：
		// 常见场景是模板号还没配 → 第一次点全部 not_configured，配好后再点
		// 若一律判 duplicate，这批用户在这个版本上就永远发不出去了。
		if task.Status != model.ReachStatusFailed {
			out.Status = ConnectorNotifyStatusDuplicate
			return out
		}
		reopened, err := reopenFailedReachTask(ctx, task.ID, directReachContentJSON(directReq))
		if err != nil {
			out.Error = err.Error()
			return out
		}
		if !reopened {
			// 并发的另一次点击已经把这单领走了：认输，不跟着再投一遍。
			out.Status = ConnectorNotifyStatusDuplicate
			return out
		}
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
		attempt, chNotConfigured := attemptConnectorNotifyChannel(ctx, task.ID, user, directReq, ch)
		attempts = append(attempts, attempt)
		if attempt.Status == model.ReachSendStatusSent {
			sent = ch
			break
		}
		if attempt.Error != "" {
			lastErr = attempt.Error
		}
		if chNotConfigured {
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

// reopenFailedReachTask 把整单失败的任务放回 sending，供后台改好配置后重试同一幂等键。
// 条件更新是并发闸门："读到 failed" 与 "重开" 之间若不原子，两次并发点击会同时进投递、
// 复用同一条日志行各发一次。返回 false 表示已被别的请求领走。
// content 一并覆盖：重投的文案可能与首次不同，任务记录必须跟着这次真正发出去的内容走。
func reopenFailedReachTask(ctx context.Context, taskID int64, content []byte) (bool, error) {
	updates := map[string]any{"status": model.ReachStatusSending, "updated_at": time.Now().UTC()}
	if len(content) > 0 {
		updates["content"] = datatypes.JSON(content)
	}
	res := store.DB.WithContext(ctx).Model(&model.ReachTask{}).
		Where("id = ? AND status = ?", taskID, model.ReachStatusFailed).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// attemptConnectorNotifyChannel 投递单个渠道并落 reach_send_logs。
// attempt.Status 只用 reach_send_logs 的取值，"配置缺失"通过第二个返回值单独上报，
// 这样任务 stats 与日志表口径一致，不会出现日志记 failed 而 stats 一个都不计的情况。
func attemptConnectorNotifyChannel(ctx context.Context, taskID int64, user model.User, req SendDirectUserReachReq, channel string) (DirectUserReachAttempt, bool) {
	attempt := DirectUserReachAttempt{Channel: channel}

	var deliver func() error
	switch channel {
	case ConnectorNotifyChannelEmail:
		to := strings.TrimSpace(user.Email)
		if to == "" {
			attempt.Status = model.ReachSendStatusSkipped
			attempt.Error = "user has no email"
			return attempt, false
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
			return attempt, false
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
		return attempt, false
	}

	logRow, err := ensureReachSendLog(ctx, taskID, user, channel)
	if err != nil {
		attempt.Status = model.ReachSendStatusFailed
		attempt.Error = err.Error()
		return attempt, false
	}
	attempt.LogID = logRow.ID

	if err := deliver(); err != nil {
		attempt.Status = model.ReachSendStatusFailed
		attempt.Error = err.Error()
		markReachSendLog(ctx, logRow.ID, model.ReachSendStatusFailed, err.Error())
		return attempt, isConnectorNotifyNotConfigured(err)
	}
	attempt.Status = model.ReachSendStatusSent
	markReachSendLog(ctx, logRow.ID, model.ReachSendStatusSent, "")
	return attempt, false
}

func isConnectorNotifyNotConfigured(err error) bool {
	return errors.Is(err, ErrReachSMSNotConfigured) ||
		errors.Is(err, ErrAliEmailTemplateNotConfigured) ||
		errors.Is(err, identity.ErrProviderNotConfigured) ||
		errors.Is(err, identity.ErrSmsTemplateNotConfigured)
}

func connectorNotifyEmailVars(user model.User, req SendDirectUserReachReq) map[string]string {
	return reachEmailTemplateVars(connectorNotifyDisplayName(user), req)
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
		store.DB.Model(&model.User{}).Select("id, nickname, region, phone_country").
			Where("id = ?", sampleUserID).First(&user)
	}

	out := &ConnectorNotifyPreview{SMSText: req.ShortText}
	vars := connectorNotifyEmailVars(user, req)
	templateSubject, emailHTML, err := RenderReachEmailTemplate(ReachEmailTemplateID(), vars)
	if err != nil {
		out.EmailError = err.Error()
	} else {
		out.EmailSubject = ResolveReachEmailSubject(templateSubject, vars)
		out.EmailHTML = emailHTML
	}
	if err := connectorNotifySMSConfigError(user); err != nil {
		out.SMSError = err.Error()
	}
	return out, nil
}

// connectorNotifySMSConfigError 走与实发完全相同的 provider 解析和配置判定，只是不真发。
// 只看模板号非空是不够的：ak/sk 缺失时预览会显示可用、一发就 not_configured。
func connectorNotifySMSConfigError(user model.User) error {
	return CheckReachSMS(user.Region, strings.TrimSpace(user.PhoneCountry), identity.SmsTextKindNotify)
}

package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// ConnectorUpgradeNoticeEventKey 是升级提醒 reach 任务的 event_key，DedupKey 按用户 + 目标版本去重。
const ConnectorUpgradeNoticeEventKey = "connector_upgrade_notice"

const (
	defaultConnectorUpgradeSeenWithinDays = 30
	maxConnectorUpgradeSeenWithinDays     = 365
)

// sendConnectorUpgradeReach 可在测试中替换。
var sendConnectorUpgradeReach = SendDirectUserReach

// ConnectorClientGrixConnector 是 Node 版 grix-connector 在 WS 鉴权 client 字段里的标识；
// Hermes（hermes-agent）与 OpenClaw 插件（openclaw-grix）各有自己的版本线，不参与 connector 升级提醒。
const ConnectorClientGrixConnector = "grix-connector"

// RecordAgentConnectorVersion 在 agent-api WS 鉴权成功后落库连接端标识与版本；version 为空时不写。
// 写库失败只记日志，绝不影响鉴权。
func RecordAgentConnectorVersion(agentID int64, client, version string) {
	client = strings.TrimSpace(client)
	version = strings.TrimSpace(version)
	if agentID <= 0 || version == "" || len(version) > 32 || len(client) > 32 {
		return
	}
	err := store.DB.Model(&model.Agent{}).Where("id = ?", agentID).Updates(map[string]any{
		"connector_client":          client,
		"connector_version":         version,
		"connector_version_seen_at": time.Now(),
	}).Error
	if err != nil {
		logger.L.Warnf("record connector version failed: agent_id=%d version=%s err=%v", agentID, version, err)
	}
}

type ConnectorUpgradeNotifyReq struct {
	// BelowVersion：connector_version 低于它的 agent 视为需要升级。
	BelowVersion string `json:"below_version"`
	// TargetVersion：邮件里建议升级到的版本，也参与 DedupKey。
	TargetVersion string `json:"target_version"`
	// SeenWithinDays：只看最近 N 天内连过线的 agent，默认 30。
	SeenWithinDays int `json:"seen_within_days"`
	// DryRun 为 true 时只返回命中的用户，不发送。
	DryRun bool `json:"dry_run"`
}

type ConnectorUpgradeNotifyAgent struct {
	AgentID          int64     `json:"agent_id,string"`
	AgentName        string    `json:"agent_name"`
	ConnectorVersion string    `json:"connector_version"`
	SeenAt           time.Time `json:"seen_at"`
}

type ConnectorUpgradeNotifyUser struct {
	UserID  int64                         `json:"user_id,string"`
	Agents  []ConnectorUpgradeNotifyAgent `json:"agents"`
	Status  string                        `json:"status,omitempty"`
	Channel string                        `json:"channel,omitempty"`
	Error   string                        `json:"error,omitempty"`
}

type ConnectorUpgradeNotifyResult struct {
	DryRun        bool                         `json:"dry_run"`
	BelowVersion  string                       `json:"below_version"`
	TargetVersion string                       `json:"target_version"`
	Users         []ConnectorUpgradeNotifyUser `json:"users"`
	SentCount     int                          `json:"sent_count"`
	FailedCount   int                          `json:"failed_count"`
}

// NotifyConnectorUpgrade 找出仍在跑旧版本 connector 的 agent，按 owner 聚合后各发一封升级提醒。
func NotifyConnectorUpgrade(ctx context.Context, req ConnectorUpgradeNotifyReq) (*ConnectorUpgradeNotifyResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req.BelowVersion = strings.TrimSpace(req.BelowVersion)
	req.TargetVersion = strings.TrimSpace(req.TargetVersion)
	if _, ok := parseSemverTriple(req.BelowVersion); !ok {
		return nil, errors.New("below_version must be semver")
	}
	if _, ok := parseSemverTriple(req.TargetVersion); !ok {
		return nil, errors.New("target_version must be semver")
	}
	if req.SeenWithinDays <= 0 {
		req.SeenWithinDays = defaultConnectorUpgradeSeenWithinDays
	}
	if req.SeenWithinDays > maxConnectorUpgradeSeenWithinDays {
		req.SeenWithinDays = maxConnectorUpgradeSeenWithinDays
	}

	users, err := listOutdatedConnectorUsers(ctx, req.BelowVersion, req.SeenWithinDays)
	if err != nil {
		return nil, err
	}
	result := &ConnectorUpgradeNotifyResult{
		DryRun:        req.DryRun,
		BelowVersion:  req.BelowVersion,
		TargetVersion: req.TargetVersion,
		Users:         users,
	}
	if req.DryRun {
		return result, nil
	}

	for i := range result.Users {
		u := &result.Users[i]
		title, body := connectorUpgradeNoticeContent(u.Agents, req.TargetVersion)
		res, err := sendConnectorUpgradeReach(ctx, SendDirectUserReachReq{
			UserID:    u.UserID,
			Title:     title,
			LongText:  body,
			ShortText: connectorUpgradeNoticeShortText(req.TargetVersion),
			EventKey:  ConnectorUpgradeNoticeEventKey,
			DedupKey:  fmt.Sprintf("connector_upgrade:%d:%s", u.UserID, req.TargetVersion),
		})
		if err != nil {
			u.Status = model.ReachStatusFailed
			u.Error = err.Error()
			result.FailedCount++
			continue
		}
		u.Status = res.Status
		u.Channel = res.Channel
		if res.Status == model.ReachStatusSent {
			result.SentCount++
		} else {
			result.FailedCount++
		}
	}
	return result, nil
}

// listOutdatedConnectorUsers 只看 grix-connector 客户端；版本比较在 Go 里做（semver 不能直接用 SQL 字符串比较），
// 候选集合只按 seen_at 窗口和非空版本在 SQL 层裁剪。
func listOutdatedConnectorUsers(ctx context.Context, belowVersion string, seenWithinDays int) ([]ConnectorUpgradeNotifyUser, error) {
	since := time.Now().AddDate(0, 0, -seenWithinDays)
	var agents []model.Agent
	err := store.DB.WithContext(ctx).
		Select("id, agent_name, owner_id, connector_version, connector_version_seen_at").
		Where("status = ? AND connector_client = ? AND connector_version <> '' AND connector_version_seen_at >= ?",
			1, ConnectorClientGrixConnector, since).
		Order("owner_id, id").
		Find(&agents).Error
	if err != nil {
		return nil, err
	}

	byOwner := make(map[int64]*ConnectorUpgradeNotifyUser)
	order := make([]int64, 0)
	for _, a := range agents {
		if !isNewer(belowVersion, a.ConnectorVersion) {
			continue
		}
		u, ok := byOwner[a.OwnerID]
		if !ok {
			u = &ConnectorUpgradeNotifyUser{UserID: a.OwnerID}
			byOwner[a.OwnerID] = u
			order = append(order, a.OwnerID)
		}
		seenAt := time.Time{}
		if a.ConnectorVersionSeenAt != nil {
			seenAt = *a.ConnectorVersionSeenAt
		}
		u.Agents = append(u.Agents, ConnectorUpgradeNotifyAgent{
			AgentID:          a.ID,
			AgentName:        a.AgentName,
			ConnectorVersion: a.ConnectorVersion,
			SeenAt:           seenAt,
		})
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]ConnectorUpgradeNotifyUser, 0, len(order))
	for _, id := range order {
		out = append(out, *byOwner[id])
	}
	return out, nil
}

func connectorUpgradeNoticeShortText(target string) string {
	return fmt.Sprintf("Your Grix Connector is outdated. Please upgrade to %s: npm install -g grix-connector@latest && grix-connector restart", target)
}

// connectorUpgradeNoticeContent 生成中英双语 Markdown 正文（直达 reach 会渲染成邮件 HTML / 站内消息）。
func connectorUpgradeNoticeContent(agents []ConnectorUpgradeNotifyAgent, target string) (title, body string) {
	title = "请升级你的 Grix Connector / Please upgrade your Grix Connector"

	var list strings.Builder
	for _, a := range agents {
		fmt.Fprintf(&list, "- %s：当前版本 / current version `%s`\n", a.AgentName, a.ConnectorVersion)
	}

	body = fmt.Sprintf(`我们检测到你有 Agent 仍在运行旧版本的 Grix Connector：

%s
旧版本存在异常错误重复上报的问题，会影响连接稳定性。请在运行 Connector 的电脑上执行以下命令升级到 **%s**：

`+"```"+`
npm install -g grix-connector@latest
grix-connector restart
grix-connector status
`+"```"+`

第 3 步输出的版本号应为 %s。升级不会影响已绑定的 Agent、会话和配置。

---

We detected that some of your Agents are still running an outdated Grix Connector:

%s
Outdated versions repeatedly report spurious errors and hurt connection stability. Please run the following on the machine hosting the Connector to upgrade to **%s**:

`+"```"+`
npm install -g grix-connector@latest
grix-connector restart
grix-connector status
`+"```"+`

Step 3 should print version %s. Upgrading keeps your bound Agents, sessions and configuration intact.
`, list.String(), target, target, list.String(), target, target)
	return title, body
}

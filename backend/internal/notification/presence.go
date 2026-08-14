package notification

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/push/provider"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
)

// presence.go implements agent 上线/离线 推送。设计要点（与 owner 约定）：
//
//   - 上线走即时：agent 从离线（或首次）恢复上线立刻推一条，短窗口内扎堆上线
//     的合并成一条。
//   - 离线走抖动阈值：主连接掉线后延迟 presenceFlapThreshold 再确认，仍未回来
//     才推；期间重连则不推，天然过滤网络抖动。
//   - 首条即时 + 尾随合并（leading + trailing）：同一 owner 首条立即发，随后
//     presenceMergeWindow 窗口内继续上/下线的合并成一条尾随消息。
//
// 单一状态权威放在推送服务（PresenceNotifier）：ws 侧只发原始信号
// （上线发 NATS 事件、下线写 Redis 待确认队列），是否构成真正的上线/离线转换
// 一律由推送服务据 Redis 里的 presence 状态原子判定，避免多节点竞争。
const (
	// presenceFlapThreshold 主连接掉线后延迟这么久再确认是否真离线。
	presenceFlapThreshold = 5 * time.Minute
	// presenceMergeWindow 首条推送后开启的合并窗口：窗口内继续上/下线的
	// 合并成一条尾随消息，兼顾时效与防滥发。
	presenceMergeWindow = 60 * time.Second
	// presenceTickInterval 巡检周期：确认到期的离线、冲刷尾随合并批。
	presenceTickInterval = 30 * time.Second
	// presenceLeaderTTL 巡检 leader 锁的租约，须大于一个巡检周期以容忍抖动。
	presenceLeaderTTL = 70 * time.Second
	// presenceStateTTLSec presence 状态键的过期秒数（每次转换刷新），避免长期
	// 不再连接的 agent 状态键无限堆积。
	presenceStateTTLSec = 30 * 24 * 60 * 60
)

const (
	presenceKindOnline  = "on"
	presenceKindOffline = "off"

	presenceLeaderKey         = "presence:ticker:leader"
	presenceOfflinePendingKey = "presence:offpend"
	// presenceOnlineSetKey tracks agents currently notified-online so the ticker
	// can reconcile against live conn info and catch a ws-node crash that skipped
	// the graceful unregister (no offline scheduled + state stuck online).
	presenceOnlineSetKey = "presence:online"
)

func presenceStateKey(agentID int64) string { return fmt.Sprintf("presence:st:%d", agentID) }
func presenceWindowKey(kind string, ownerID int64) string {
	return fmt.Sprintf("presence:win:%s:%d", kind, ownerID)
}
func presenceBatchKey(kind string, ownerID int64) string {
	return fmt.Sprintf("presence:batch:%s:%d", kind, ownerID)
}
func presenceBatchOwnersKey(kind string) string { return "presence:batchowners:" + kind }

func presenceMember(agentID, ownerID int64) string {
	return strconv.FormatInt(agentID, 10) + ":" + strconv.FormatInt(ownerID, 10)
}

func parsePresenceMember(m string) (agentID, ownerID int64, ok bool) {
	i := strings.IndexByte(m, ':')
	if i <= 0 || i >= len(m)-1 {
		return 0, 0, false
	}
	a, err1 := strconv.ParseInt(m[:i], 10, 64)
	o, err2 := strconv.ParseInt(m[i+1:], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return a, o, true
}

// presenceOnlineTransition sets state=online and returns 1 only if it was not
// already online — i.e. this connect is a real offline→online (or first-ever)
// transition worth notifying. Atomic so concurrent connects/ticker can't double
// notify.
var presenceOnlineTransition = redis.NewScript(`
local s = redis.call('GET', KEYS[1])
if s == 'online' then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
  return 0
end
redis.call('SET', KEYS[1], 'online', 'EX', ARGV[1])
return 1
`)

// presenceOfflineTransition sets state=offline and returns 1 only if it was
// online — so a still-offline / never-online agent never re-notifies.
var presenceOfflineTransition = redis.NewScript(`
local s = redis.call('GET', KEYS[1])
if s == 'online' then
  redis.call('SET', KEYS[1], 'offline', 'EX', ARGV[1])
  return 1
end
return 0
`)

// --- ws-side signals (run in the ws service) ---

// SignalAgentOnline is called by the ws service when an agent's primary
// connection is (re)established. It cancels any pending offline confirmation and
// publishes an online signal; the push service decides whether it is a real
// transition worth a push.
func SignalAgentOnline(agentID, ownerID int64) {
	if agentID <= 0 || ownerID <= 0 {
		return
	}
	if store.RDB != nil {
		_ = store.RDB.ZRem(context.Background(), presenceOfflinePendingKey, presenceMember(agentID, ownerID)).Err()
	}
	Publish(AgentNotificationEvent{
		EventKey: EventAgentOnline,
		UserID:   ownerID,
		AgentID:  agentID,
	})
}

// SignalAgentOffline is called by the ws service when an agent's primary
// connection drops. It schedules a confirmation at now+presenceFlapThreshold;
// the push-service ticker re-checks liveness before notifying, so a brief blip
// that reconnects within the window never produces a push.
func SignalAgentOffline(agentID, ownerID int64) {
	if agentID <= 0 || ownerID <= 0 || store.RDB == nil {
		return
	}
	dueMs := time.Now().Add(presenceFlapThreshold).UnixMilli()
	_ = store.RDB.ZAdd(context.Background(), presenceOfflinePendingKey, redis.Z{
		Score:  float64(dueMs),
		Member: presenceMember(agentID, ownerID),
	}).Err()
}

// --- push-side notifier (runs in the push service) ---

// PresenceNotifier owns the single presence-state authority and the periodic
// ticker. It is created and driven by the notification Dispatcher.
type PresenceNotifier struct {
	push          PushFunc
	instanceToken string
}

// NewPresenceNotifier builds a notifier delivering through the given push func.
func NewPresenceNotifier(push PushFunc) *PresenceNotifier {
	host, _ := os.Hostname()
	return &PresenceNotifier{
		push:          push,
		instanceToken: fmt.Sprintf("%s-%d-%d", host, os.Getpid(), time.Now().UnixNano()),
	}
}

// StartTicker runs the presence ticker until ctx is cancelled. Only the Redis
// leader among push replicas runs each tick, so notifications never double-fire.
func (p *PresenceNotifier) StartTicker(ctx context.Context) {
	if p == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(presenceTickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if p.acquireLeader(ctx) {
					p.tick(ctx)
				}
			}
		}
	}()
	logger.L.Infof("presence notifier ticker started interval=%s", presenceTickInterval)
}

func (p *PresenceNotifier) acquireLeader(ctx context.Context) bool {
	if store.RDB == nil {
		return false
	}
	ok, err := store.RDB.SetNX(ctx, presenceLeaderKey, p.instanceToken, presenceLeaderTTL).Result()
	if err != nil {
		return false
	}
	if ok {
		return true
	}
	// Still ours from a prior tick? refresh the lease and keep leading.
	if v, err := store.RDB.Get(ctx, presenceLeaderKey).Result(); err == nil && v == p.instanceToken {
		store.RDB.Expire(ctx, presenceLeaderKey, presenceLeaderTTL)
		return true
	}
	return false
}

func (p *PresenceNotifier) tick(ctx context.Context) {
	p.reconcileOnlineSet(ctx)
	p.processOfflinePending(ctx)
	p.flushTrailing(ctx, presenceKindOnline)
	p.flushTrailing(ctx, presenceKindOffline)
}

// OnOnlineSignal handles an agent_online signal off the NATS subject: it decides
// (atomically) whether this is a real offline→online transition and, if so,
// sends through the leading+trailing throttle.
func (p *PresenceNotifier) OnOnlineSignal(ctx context.Context, ownerID, agentID int64) {
	if p == nil || store.RDB == nil || ownerID <= 0 || agentID <= 0 {
		return
	}
	res, err := presenceOnlineTransition.Run(ctx, store.RDB,
		[]string{presenceStateKey(agentID)}, presenceStateTTLSec).Int()
	if err != nil {
		logger.L.Warnf("presence online transition agent=%d owner=%d err=%v", agentID, ownerID, err)
		return
	}
	// Register in the online set (idempotent) so the reconciliation sweep can
	// later detect a crash-skipped offline for this agent.
	store.RDB.ZAdd(ctx, presenceOnlineSetKey, redis.Z{
		Score:  float64(time.Now().UnixMilli()),
		Member: presenceMember(agentID, ownerID),
	})
	if res == 1 {
		p.send(ctx, presenceKindOnline, ownerID, agentID)
	}
}

// reconcileOnlineSet catches offline transitions that skipped the graceful
// unregister path — chiefly a ws-node process crash. For each notified-online
// agent whose live conn-info key has vanished, it schedules an offline
// confirmation (first-absence wins via ZADD NX); agents still live have any
// stale pending confirmation cancelled. Without this a crash would leave the
// state stuck online, silently suppressing the agent's next online push too.
func (p *PresenceNotifier) reconcileOnlineSet(ctx context.Context) {
	if store.RDB == nil {
		return
	}
	members, err := store.RDB.ZRange(ctx, presenceOnlineSetKey, 0, -1).Result()
	if err != nil || len(members) == 0 {
		return
	}
	dueMs := time.Now().Add(presenceFlapThreshold).UnixMilli()
	for _, m := range members {
		agentID, ownerID, ok := parsePresenceMember(m)
		if !ok {
			store.RDB.ZRem(ctx, presenceOnlineSetKey, m)
			continue
		}
		n, err := store.RDB.Exists(ctx, pkgagentapi.ConnInfoKey(agentID, ownerID)).Result()
		if err != nil {
			continue
		}
		if n > 0 {
			continue // still live
		}
		// Absent: schedule an offline confirmation if none is pending yet.
		store.RDB.ZAddNX(ctx, presenceOfflinePendingKey, redis.Z{
			Score:  float64(dueMs),
			Member: m,
		})
	}
}

// processOfflinePending confirms offline events whose flap window has elapsed:
// an agent still absent (no live conn info) transitions offline→notify; one that
// reconnected is dropped silently.
func (p *PresenceNotifier) processOfflinePending(ctx context.Context) {
	if store.RDB == nil {
		return
	}
	nowMs := time.Now().UnixMilli()
	members, err := store.RDB.ZRangeByScore(ctx, presenceOfflinePendingKey, &redis.ZRangeBy{
		Min:   "0",
		Max:   strconv.FormatInt(nowMs, 10),
		Count: 500,
	}).Result()
	if err != nil || len(members) == 0 {
		return
	}
	for _, m := range members {
		store.RDB.ZRem(ctx, presenceOfflinePendingKey, m)
		agentID, ownerID, ok := parsePresenceMember(m)
		if !ok {
			continue
		}
		// Authoritative liveness: a live conn-info key means the agent is back
		// (or never really left) — never notify offline, keep it in the online set.
		if n, err := store.RDB.Exists(ctx, pkgagentapi.ConnInfoKey(agentID, ownerID)).Result(); err == nil && n > 0 {
			continue
		}
		// Confirmed absent — drop it from the online set regardless of whether we
		// notify (already-offline agents must not linger in the set).
		store.RDB.ZRem(ctx, presenceOnlineSetKey, m)
		res, err := presenceOfflineTransition.Run(ctx, store.RDB,
			[]string{presenceStateKey(agentID)}, presenceStateTTLSec).Int()
		if err != nil {
			logger.L.Warnf("presence offline transition agent=%d owner=%d err=%v", agentID, ownerID, err)
			continue
		}
		if res == 1 {
			p.send(ctx, presenceKindOffline, ownerID, agentID)
		}
	}
}

// send applies the per-owner leading+trailing throttle: the first event opens a
// merge window and is delivered immediately; further events during the window
// are batched and flushed as one trailing message.
func (p *PresenceNotifier) send(ctx context.Context, kind string, ownerID, agentID int64) {
	if store.RDB == nil {
		return
	}
	opened, err := store.RDB.SetNX(ctx, presenceWindowKey(kind, ownerID), "1", presenceMergeWindow).Result()
	if err == nil && opened {
		p.deliver(ctx, kind, ownerID, []int64{agentID})
		return
	}
	store.RDB.SAdd(ctx, presenceBatchKey(kind, ownerID), agentID)
	store.RDB.SAdd(ctx, presenceBatchOwnersKey(kind), ownerID)
}

// flushTrailing delivers merged trailing batches for owners whose merge window
// has expired, then reopens the window so a sustained storm keeps coalescing.
func (p *PresenceNotifier) flushTrailing(ctx context.Context, kind string) {
	if store.RDB == nil {
		return
	}
	owners, err := store.RDB.SMembers(ctx, presenceBatchOwnersKey(kind)).Result()
	if err != nil || len(owners) == 0 {
		return
	}
	for _, o := range owners {
		ownerID, err := strconv.ParseInt(o, 10, 64)
		if err != nil {
			store.RDB.SRem(ctx, presenceBatchOwnersKey(kind), o)
			continue
		}
		if n, err := store.RDB.Exists(ctx, presenceWindowKey(kind, ownerID)).Result(); err == nil && n > 0 {
			continue // still cooling down
		}
		raw, err := store.RDB.SMembers(ctx, presenceBatchKey(kind, ownerID)).Result()
		if err != nil {
			continue
		}
		store.RDB.Del(ctx, presenceBatchKey(kind, ownerID))
		store.RDB.SRem(ctx, presenceBatchOwnersKey(kind), o)
		agentIDs := make([]int64, 0, len(raw))
		for _, r := range raw {
			if id, err := strconv.ParseInt(r, 10, 64); err == nil {
				agentIDs = append(agentIDs, id)
			}
		}
		if len(agentIDs) == 0 {
			continue
		}
		// Reopen the window so more events keep coalescing into the next batch.
		store.RDB.Set(ctx, presenceWindowKey(kind, ownerID), "1", presenceMergeWindow)
		p.deliver(ctx, kind, ownerID, agentIDs)
	}
}

// deliver renders and pushes one presence notification (single or merged),
// honoring the owner's notification preference and language.
func (p *PresenceNotifier) deliver(ctx context.Context, kind string, ownerID int64, agentIDs []int64) {
	if p.push == nil || ownerID <= 0 || len(agentIDs) == 0 {
		return
	}
	eventKey := EventAgentOnline
	if kind == presenceKindOffline {
		eventKey = EventAgentOffline
	}
	pref, err := ResolvePref(ownerID, eventKey)
	if err != nil || !pref.Enabled || !pref.HasChannel(ChannelPush) {
		return
	}
	names := lookupAgentNames(agentIDs)
	if len(names) == 0 {
		return
	}
	lang := userPreferredLanguage(ownerID)
	sessionID := ""
	if len(agentIDs) == 1 {
		sessionID = lastAgentSessionID(ownerID, agentIDs[0])
	}
	payload := &provider.PushPayload{
		Title:       presenceTitle(kind, lang),
		Body:        presenceBody(kind, lang, names),
		SessionID:   sessionID,
		EventKey:    eventKey,
		RecipientID: ownerID,
		// Presence notifications are opt-in status updates the user wants even
		// when the app is open on another screen.
		ForcePush: true,
	}
	if err := p.push(ctx, ownerID, payload); err != nil {
		logger.L.Warnf("presence deliver push owner=%d event=%s err=%v", ownerID, eventKey, err)
	}
}

// lookupAgentNames returns display names for the given agents in input order,
// skipping any that no longer exist. Falls back to a generic name for an agent
// whose name is blank so a merged count stays accurate.
func lookupAgentNames(agentIDs []int64) []string {
	if store.DB == nil || len(agentIDs) == 0 {
		return nil
	}
	var rows []model.Agent
	if err := store.DB.Select("id", "agent_name").Where("id IN ?", agentIDs).Find(&rows).Error; err != nil {
		return nil
	}
	byID := make(map[int64]string, len(rows))
	for _, r := range rows {
		byID[r.ID] = strings.TrimSpace(r.AgentName)
	}
	out := make([]string, 0, len(agentIDs))
	for _, id := range agentIDs {
		name, ok := byID[id]
		if !ok {
			continue // agent deleted between signal and delivery
		}
		if name == "" {
			name = "Agent"
		}
		out = append(out, name)
	}
	return out
}

// lastAgentSessionID returns the owner's most recent direct-chat session with
// the agent, so tapping the push opens that conversation. Mirrors
// service.findAgentSessionID / buildDirectKey (agent peer type = 2).
func lastAgentSessionID(ownerID, agentID int64) string {
	if store.DB == nil || ownerID <= 0 || agentID <= 0 {
		return ""
	}
	var session model.Session
	if err := store.DB.
		Select("session_id").
		Where("direct_key = ? AND is_deleted = false", agentDirectKey(ownerID, agentID)).
		Order("updated_at DESC").
		First(&session).Error; err != nil {
		return ""
	}
	return session.SessionID
}

// agentDirectKey builds the canonical 1:1 dedupe key for an owner↔agent direct
// session (human type=1, agent peer type=2), matching service.buildDirectKey.
func agentDirectKey(ownerID, agentID int64) string {
	typeA, idA := int16(1), ownerID
	typeB, idB := int16(2), agentID
	if typeA > typeB || (typeA == typeB && idA > idB) {
		typeA, typeB = typeB, typeA
		idA, idB = idB, idA
	}
	return fmt.Sprintf("d:%d:%d|%d:%d", typeA, idA, typeB, idB)
}

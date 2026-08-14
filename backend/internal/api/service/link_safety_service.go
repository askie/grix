package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/urlguard"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// LinkCheckVerdict 单条 URL 的判定结果（对外接口语义）。
type LinkCheckVerdict struct {
	URL           string `json:"url"`
	Verdict       string `json:"verdict"` // clean / suspicious / malicious
	CanonicalHost string `json:"canonical_host,omitempty"`
	Reason        string `json:"reason,omitempty"`
	RuleSource    string `json:"rule_source,omitempty"`
	RuleID        int64  `json:"rule_id,omitempty,string"`
}

const (
	linkVerdictClean      = "clean"
	linkVerdictSuspicious = "suspicious"
	linkVerdictMalicious  = "malicious"
)

// CheckLinks 批量校验 URL；不会返回错误（失败也按 clean 安全降级，调用方决定是否再次走可疑提示）。
func CheckLinks(ctx context.Context, urls []string) []LinkCheckVerdict {
	if len(urls) == 0 {
		return nil
	}
	settings, err := systemsetting.GetLinkSafetySettings()
	if err != nil || !settings.Enabled {
		out := make([]LinkCheckVerdict, len(urls))
		for i, u := range urls {
			out[i] = LinkCheckVerdict{URL: u, Verdict: linkVerdictClean}
		}
		return out
	}

	matcher, matcherSig := loadLinkMatcher()
	out := make([]LinkCheckVerdict, len(urls))
	for i, raw := range urls {
		out[i] = checkOneLink(ctx, raw, settings, matcher, matcherSig)
	}
	return out
}

func checkOneLink(
	ctx context.Context,
	raw string,
	settings systemsetting.LinkSafetySettings,
	matcher *urlguard.Matcher,
	matcherSig string,
) LinkCheckVerdict {
	canon, err := urlguard.Canonicalize(raw)
	if err != nil {
		return LinkCheckVerdict{URL: raw, Verdict: linkVerdictClean}
	}

	// 1) 自家域名白名单直通：必须精确等于 host，或 host 是 own 的真子域。
	// 不能依赖 HostSuffixes 的通用匹配——白名单匹配条件比黑名单严格，
	// 避免 `evil.com.<own>` 这类构造意外被当成自家域（虽然要求攻击者
	// 控制 <own> 的子域才能命中，但白名单语义就是字面精确，应在代码层显式表达）。
	for _, own := range settings.OwnDomainWhitelist {
		if canon.Host == own || strings.HasSuffix(canon.Host, "."+own) {
			return LinkCheckVerdict{URL: raw, Verdict: linkVerdictClean, CanonicalHost: canon.Host}
		}
	}

	// 2) Redis 缓存（key 含规则签名前缀 + 完整规范化 URL，规则一改全网自动失效；
	//    缓存 key 包含 query，避免不同 query 串错共用同一 verdict）。
	cacheKey := linkVerdictCacheKey(matcherSig, canon)
	if hit, ok := loadLinkVerdictCache(ctx, cacheKey); ok {
		hit.URL = raw
		return hit
	}

	// 3) 走 Matcher
	v := matcher.Match(raw)
	out := LinkCheckVerdict{URL: raw, CanonicalHost: canon.Host}
	if !v.Hit {
		out.Verdict = linkVerdictClean
	} else {
		out.Verdict = string(v.Severity)
		out.Reason = string(v.Rule.Kind)
		out.RuleSource = v.Rule.Source
		out.RuleID = v.Rule.ID
		bumpLinkBlocklistHit(ctx, v.Rule.ID)
		recordLinkSafetyHit(ctx, raw, canon, v)
	}

	// 4) 回写缓存
	storeLinkVerdictCache(ctx, cacheKey, out, settings)
	return out
}

// ---------- Matcher 编译缓存（黑名单规则签名变更时重建） ----------

var linkMatcherCache struct {
	mu        sync.RWMutex
	matcher   *urlguard.Matcher
	signature string
	loadedAt  time.Time
}

const linkMatcherRefreshInterval = 30 * time.Second

// loadLinkMatcher 返回当前的匹配器及其规则签名（用于 Redis verdict cache key）。
// 签名变化即代表规则集合变化，旧 cache key 自然失活，无需 SCAN+DEL。
func loadLinkMatcher() (*urlguard.Matcher, string) {
	now := time.Now()
	linkMatcherCache.mu.RLock()
	cached := linkMatcherCache.matcher
	cachedSig := linkMatcherCache.signature
	loadedAt := linkMatcherCache.loadedAt
	linkMatcherCache.mu.RUnlock()

	if cached != nil && now.Sub(loadedAt) < linkMatcherRefreshInterval {
		return cached, cachedSig
	}

	rules, sig, err := loadActiveLinkBlocklistRules()
	if err != nil {
		// DB 失败时：有旧 cache 用旧 cache（安全降级）；否则空 matcher 全 clean（fail-open）。
		// 升级为 Errorf 以便运维告警——防护已处于降级状态。
		if logger.L != nil {
			if cached != nil {
				logger.L.Errorf("load link blocklist rules failed, using stale cache: %v", err)
			} else {
				logger.L.Errorf("load link blocklist rules failed, fail-open (no protection): %v", err)
			}
		}
		if cached != nil {
			return cached, cachedSig
		}
		return urlguard.Compile(nil), ""
	}

	if cached != nil && cachedSig == sig {
		linkMatcherCache.mu.Lock()
		linkMatcherCache.loadedAt = now
		linkMatcherCache.mu.Unlock()
		return cached, sig
	}

	m := urlguard.Compile(rules)
	linkMatcherCache.mu.Lock()
	linkMatcherCache.matcher = m
	linkMatcherCache.signature = sig
	linkMatcherCache.loadedAt = now
	linkMatcherCache.mu.Unlock()
	return m, sig
}

// linkBlocklistInvalidateChannel 跨节点失效广播频道。
// 任何 api 节点写规则后通过它通知所有兄弟节点丢弃内存 matcher 缓存。
const linkBlocklistInvalidateChannel = "link_blocklist:invalidate"

// InvalidateLinkMatcherCache 后台规则增删改后调用：
// 1) 立刻清掉本进程内存 matcher；
// 2) Pub/Sub 广播让其他 api 节点同步清；
// 3) 不再 SCAN+DEL 整个结果缓存前缀（在大量缓存下会阻塞 Redis）。
//    结果缓存随各自 TTL 自然过期；malicious TTL 默认 24h，可在塘主后台调短。
//    如果需要立即生效，先调短 TTL 再改规则即可。
func InvalidateLinkMatcherCache() {
	clearLocalLinkMatcherCache()
	if store.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := store.RDB.Publish(ctx, linkBlocklistInvalidateChannel, "1").Err(); err != nil && logger.L != nil {
			logger.L.Warnf("publish link blocklist invalidate failed: %v", err)
		}
	}
}

func clearLocalLinkMatcherCache() {
	linkMatcherCache.mu.Lock()
	linkMatcherCache.matcher = nil
	linkMatcherCache.signature = ""
	linkMatcherCache.loadedAt = time.Time{}
	linkMatcherCache.mu.Unlock()
}

// StartLinkBlocklistInvalidateListener 在每个 api 进程启动时调用一次。
// 订阅失效频道，收到广播即丢弃本进程 matcher 缓存。
// ctx 通常是进程级 cancel context；关闭时自动退出 goroutine。
func StartLinkBlocklistInvalidateListener(ctx context.Context) {
	if store.RDB == nil {
		return
	}
	go func() {
		sub := store.RDB.Subscribe(ctx, linkBlocklistInvalidateChannel)
		defer sub.Close()
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				_ = msg
				clearLocalLinkMatcherCache()
			}
		}
	}()
}

func loadActiveLinkBlocklistRules() ([]urlguard.Rule, string, error) {
	if store.DB == nil {
		return nil, "", errors.New("db unavailable")
	}
	var rows []model.LinkBlocklistRule
	if err := store.DB.
		Where("enabled = ?", true).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, "", err
	}

	out := make([]urlguard.Rule, 0, len(rows))
	h := sha256.New()
	for _, r := range rows {
		out = append(out, urlguard.Rule{
			ID:       r.ID,
			Kind:     urlguard.RuleKind(r.Kind),
			Value:    r.Value,
			Severity: urlguard.Severity(r.Severity),
			Source:   r.Source,
		})
		// 签名：id + updated_at 即可代表"是否有任何规则变化"
		h.Write([]byte(strconv.FormatInt(r.ID, 10)))
		h.Write([]byte(r.UpdatedAt.Format(time.RFC3339Nano)))
		h.Write([]byte{0})
	}
	return out, hex.EncodeToString(h.Sum(nil)), nil
}

// ---------- 命中计数（内存聚合 + 周期 flush） ----------
//
// 设计：高频命中场景下不能每次都 UPDATE 主表（行写竞争 + 写放大）。
// 改为：命中时只在本进程内存里累加；每 linkHitFlushInterval 把所有规则的
// 累加增量一次性 flush 回 DB。损失：进程崩溃时丢失最多一个 flush 周期的计数；
// 收益：写入次数从 O(命中) 降到 O(规则数/flush 周期)，几乎可忽略。

const linkHitFlushInterval = 30 * time.Second

var linkHitAggregator struct {
	mu       sync.Mutex
	counts   map[int64]int64 // ruleID -> 待 flush 的增量
	lastHit  map[int64]time.Time
	flushing bool
}

func init() {
	linkHitAggregator.counts = make(map[int64]int64)
	linkHitAggregator.lastHit = make(map[int64]time.Time)
}

// bumpLinkBlocklistHit 命中时只触内存计数器，立即返回。
func bumpLinkBlocklistHit(ctx context.Context, ruleID int64) {
	if ruleID == 0 {
		return
	}
	linkHitAggregator.mu.Lock()
	linkHitAggregator.counts[ruleID]++
	linkHitAggregator.lastHit[ruleID] = time.Now()
	linkHitAggregator.mu.Unlock()
}

// StartLinkBlocklistHitFlusher 在 cmd/api 启动时调用一次，周期 flush 内存计数到 DB。
// 进程退出时由 ctx.Done() 触发最终 flush，最大化保留计数。
func StartLinkBlocklistHitFlusher(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(linkHitFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				flushLinkBlocklistHits()
				return
			case <-ticker.C:
				flushLinkBlocklistHits()
			}
		}
	}()
}

func flushLinkBlocklistHits() {
	if store.DB == nil {
		return
	}
	linkHitAggregator.mu.Lock()
	if linkHitAggregator.flushing || len(linkHitAggregator.counts) == 0 {
		linkHitAggregator.mu.Unlock()
		return
	}
	linkHitAggregator.flushing = true
	pending := linkHitAggregator.counts
	pendingLast := linkHitAggregator.lastHit
	linkHitAggregator.counts = make(map[int64]int64, len(pending))
	linkHitAggregator.lastHit = make(map[int64]time.Time, len(pending))
	linkHitAggregator.mu.Unlock()

	defer func() {
		linkHitAggregator.mu.Lock()
		linkHitAggregator.flushing = false
		linkHitAggregator.mu.Unlock()
	}()

	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for ruleID, delta := range pending {
		if err := store.DB.WithContext(bgCtx).
			Model(&model.LinkBlocklistRule{}).
			Where("id = ?", ruleID).
			Updates(map[string]any{
				"hit_count":   gorm.Expr("hit_count + ?", delta),
				"last_hit_at": pendingLast[ruleID],
			}).Error; err != nil && logger.L != nil {
			logger.L.Warnf("flush link blocklist hit failed rule=%d delta=%d err=%v",
				ruleID, delta, err)
			// 失败的增量重新合回内存，下次再试
			linkHitAggregator.mu.Lock()
			linkHitAggregator.counts[ruleID] += delta
			if pendingLast[ruleID].After(linkHitAggregator.lastHit[ruleID]) {
				linkHitAggregator.lastHit[ruleID] = pendingLast[ruleID]
			}
			linkHitAggregator.mu.Unlock()
		}
	}
}

// ---------- 命中拦截审计 ----------

// linkAuditDedupTTL 同 (event, user, canonical_host) 在该窗口内只写一条审计，
// 防止恶意链接群发场景下 audit_log 洪流。
const linkAuditDedupTTL = time.Minute

// recordLinkSafetyHit 命中黑名单时写审计：
// - malicious -> link_blocked（"已拦截"）
// - suspicious -> link_warned（"已提示"，区别于真正拦死的事件，避免审计语义错乱）
// 异步写、Redis SET NX 去重、不阻塞主调用路径；失败只 warn 不影响业务。
func recordLinkSafetyHit(ctx context.Context, rawURL string, canon urlguard.CanonURL, v urlguard.Verdict) {
	uid := userIDFromContext(ctx)
	meta := clientMetaFromContext(ctx)

	eventType := "link_blocked"
	if v.Severity == urlguard.SeveritySuspicious {
		eventType = "link_warned"
	}

	// 去重：1 分钟内同 event+user+host 只写一条
	if store.RDB != nil {
		dedupCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		dedupKey := "link:audit-dedup:" + eventType + ":" +
			strconv.FormatInt(uid, 10) + ":" + canon.Host
		ok, err := store.RDB.SetNX(dedupCtx, dedupKey, "1", linkAuditDedupTTL).Result()
		cancel()
		// Redis 失败时仍允许写一条审计（fail-open 审计），但跳过 dedup
		if err == nil && !ok {
			return
		}
	}

	// 异步写，不阻塞 /v1/link/check 响应链路
	go func() {
		var userIDPtr *int64
		if uid > 0 {
			userIDPtr = &uid
		}
		WriteAuditLog(WriteAuditLogReq{
			EventType: eventType,
			UserID:    userIDPtr,
			Detail: map[string]any{
				"url":            rawURL,
				"canonical_host": canon.Host,
				"severity":       string(v.Severity),
				"rule_id":        v.Rule.ID,
				"rule_kind":      string(v.Rule.Kind),
				"rule_value":     v.Rule.Value,
				"rule_source":    v.Rule.Source,
			},
			ClientIP:  meta.ClientIP,
			UserAgent: meta.UserAgent,
		})
	}()
}

// linkSafetyUserIDKey / linkSafetyClientMetaKey 跨包传递触发上下文的私有 key。
// 不用裸 string key 以避免与其他包的同名 key 相撞。
type linkSafetyUserIDKey struct{}
type linkSafetyClientMetaKey struct{}

// LinkClientMeta 调用方（如 handler）拿到的请求元数据，注入 ctx 供审计使用。
type LinkClientMeta struct {
	ClientIP  string
	UserAgent string
}

// ContextWithUserID 在 ctx 上注入触发链接校验的用户 ID。
// handler 在调 CheckLinks 前包一层，service 写审计时取出。
func ContextWithUserID(ctx context.Context, uid int64) context.Context {
	if uid <= 0 || ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, linkSafetyUserIDKey{}, uid)
}

// ContextWithClientMeta 在 ctx 上注入 IP / UA，写审计时落到 audit_log 的对应列。
func ContextWithClientMeta(ctx context.Context, meta LinkClientMeta) context.Context {
	if ctx == nil {
		return ctx
	}
	if meta.ClientIP == "" && meta.UserAgent == "" {
		return ctx
	}
	return context.WithValue(ctx, linkSafetyClientMetaKey{}, meta)
}

func userIDFromContext(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	if v, ok := ctx.Value(linkSafetyUserIDKey{}).(int64); ok {
		return v
	}
	return 0
}

func clientMetaFromContext(ctx context.Context) LinkClientMeta {
	if ctx == nil {
		return LinkClientMeta{}
	}
	if v, ok := ctx.Value(linkSafetyClientMetaKey{}).(LinkClientMeta); ok {
		return v
	}
	return LinkClientMeta{}
}

// ---------- Redis 结果缓存 ----------

const linkVerdictCachePrefix = "link:verdict:"

// linkVerdictCacheKey 缓存 key 含规则签名 + 完整规范化 URL（host+path+query）。
// 设计要点：
// 1. 含 query：避免不同 query 串错共用同一 verdict（regex 规则可命中 query）。
// 2. 前缀含 matcherSig：规则一变签名就变，旧 key 自然失活；
//    比 SCAN+DEL 优雅且对 Redis 零额外负担。
// 3. matcherSig 为空（DB 异常无规则可用）时退化为不区分版本，保证可用性。
func linkVerdictCacheKey(matcherSig string, c urlguard.CanonURL) string {
	sigTag := matcherSig
	if len(sigTag) > 8 {
		sigTag = sigTag[:8]
	}
	full := c.Host + c.Path
	if c.Query != "" {
		full += "?" + c.Query
	}
	return linkVerdictCachePrefix + sigTag + ":" + full
}

func loadLinkVerdictCache(ctx context.Context, key string) (LinkCheckVerdict, bool) {
	if store.RDB == nil {
		return LinkCheckVerdict{}, false
	}
	raw, err := store.RDB.Get(ctx, key).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) && logger.L != nil {
			logger.L.Warnf("load link verdict cache failed: %v", err)
		}
		return LinkCheckVerdict{}, false
	}
	var v LinkCheckVerdict
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return LinkCheckVerdict{}, false
	}
	return v, true
}

func storeLinkVerdictCache(
	ctx context.Context,
	key string,
	v LinkCheckVerdict,
	settings systemsetting.LinkSafetySettings,
) {
	if store.RDB == nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	var ttl time.Duration
	switch v.Verdict {
	case linkVerdictMalicious, linkVerdictSuspicious:
		ttl = time.Duration(settings.MaliciousCacheTTLMS) * time.Millisecond
	default:
		ttl = time.Duration(settings.CleanCacheTTLMS) * time.Millisecond
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	store.RDB.Set(ctx, key, string(raw), ttl)
}

// ---------- 在线测试（不写命中计数 / 不写缓存） ----------

// TestLinkSafetyRule 给塘主后台"在线测试"用：零副作用，仅判定。
func TestLinkSafetyRule(rawURL string) LinkCheckVerdict {
	matcher, _ := loadLinkMatcher()
	canon, err := urlguard.Canonicalize(rawURL)
	if err != nil {
		return LinkCheckVerdict{URL: rawURL, Verdict: linkVerdictClean, Reason: "unparseable"}
	}
	v := matcher.Match(rawURL)
	out := LinkCheckVerdict{URL: rawURL, CanonicalHost: canon.Host}
	if !v.Hit {
		out.Verdict = linkVerdictClean
		return out
	}
	out.Verdict = string(v.Severity)
	out.Reason = string(v.Rule.Kind)
	out.RuleSource = v.Rule.Source
	out.RuleID = v.Rule.ID
	return out
}


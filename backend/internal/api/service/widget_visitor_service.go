package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/locale"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrWidgetInitInvalidInput = errors.New("invalid widget init payload")
	ErrWidgetSiteNotFound     = errors.New("widget site not found")
	ErrWidgetSiteDisabled     = errors.New("widget site disabled")
	ErrWidgetOriginNotAllowed = errors.New("origin not allowed")
	ErrWidgetVisitorBanned    = errors.New("visitor is banned")
	ErrWidgetIPBanned         = errors.New("visitor ip is banned")
	ErrWidgetVisitorRateLimit = errors.New("too many visitor init requests")
)

type WidgetVisitorInitInput struct {
	SiteKey      string
	VisitorKey   string
	VisitorName  string
	VisitorEmail string
	PageURL      string
	Locale       string
	Origin       string
	WSURL        string
	ClientIP     string
	UserAgent    string
}

type WidgetVisitorInitResp struct {
	SessionID      string                     `json:"session_id"`
	VisitorID      int64                      `json:"visitor_id,string"`
	VisitorKey     string                     `json:"visitor_key"`
	WSURL          string                     `json:"ws_url"`
	WidgetToken    string                     `json:"widget_token"`
	ExpiresIn      int64                      `json:"expires_in"`
	VoiceEnabled   bool                       `json:"voice_enabled"` // owner 已配语音托管且 agent 开放访客通话
	DisplayConfig  WidgetVisitorDisplayConfig `json:"display_config"`
	ResolvedLocale string                     `json:"resolved_locale"` // 访客 locale 归一化结果，供弹窗自身 UI 文案多语言使用
}

func WidgetVisitorInit(in WidgetVisitorInitInput) (*WidgetVisitorInitResp, error) {
	siteKey := strings.TrimSpace(in.SiteKey)
	if siteKey == "" {
		return nil, ErrWidgetInitInvalidInput
	}
	origin := strings.TrimSpace(in.Origin)
	if origin == "" {
		return nil, ErrWidgetOriginNotAllowed
	}
	wsURL := strings.TrimSpace(in.WSURL)
	if wsURL == "" {
		return nil, ErrWidgetInitInvalidInput
	}

	visitorKey := strings.TrimSpace(in.VisitorKey)
	visitorName := strings.TrimSpace(in.VisitorName)
	visitorEmail := strings.TrimSpace(in.VisitorEmail)
	pageURL := strings.TrimSpace(in.PageURL)
	resolvedLocale := locale.Normalize(in.Locale)

	site, allowedOrigins, err := loadWidgetSiteByKey(siteKey)
	if err != nil {
		return nil, err
	}
	if !isWidgetOriginAllowed(origin, allowedOrigins) {
		return nil, ErrWidgetOriginNotAllowed
	}
	// owner 维度的访客 IP 封禁：对该 owner 名下所有 widget 站点生效，
	// 与 visitor_key 会话封禁相互独立。
	if security.IsWidgetIPBanned(site.OwnerUserID, in.ClientIP) {
		return nil, ErrWidgetIPBanned
	}
	initIPPrefix := normalizeClientIPPrefix(in.ClientIP)
	initIP := normalizeClientIP(in.ClientIP)
	// 只有显式携带且非指纹派生（vkf_ 前缀）的 visitor_key 才可作为会话凭证：
	// 指纹只由 IP/24、UA 等公开特征算出，同网段陌生人可重算，不能凭它恢复会话。
	providedKey := normalizeProvidedVisitorKey(visitorKey)
	credentialKey := providedKey != "" && !strings.HasPrefix(providedKey, widgetVisitorFingerprintPrefix)
	visitorKey = resolveWidgetVisitorKey(visitorKey, site, widgetVisitorFingerprintInput{
		Origin:       origin,
		ClientIP:     in.ClientIP,
		UserAgent:    in.UserAgent,
		VisitorEmail: visitorEmail,
		VisitorName:  visitorName,
		PageURL:      pageURL,
	})
	if !widgetAllowVisitorInitRate(site.ID, in.ClientIP, visitorKey) {
		return nil, ErrWidgetVisitorRateLimit
	}

	var bannedCount int64
	if err := store.DB.Model(&model.WidgetSession{}).
		Where("site_id = ? AND visitor_key = ? AND status = ?", site.ID, visitorKey, model.WidgetSessionStatusBanned).
		Count(&bannedCount).Error; err != nil {
		return nil, err
	}
	if bannedCount > 0 {
		return nil, ErrWidgetVisitorBanned
	}

	now := time.Now().UTC()

	var widgetSession model.WidgetSession
	reuseSession := false
	if err := store.DB.Where(
		"site_id = ? AND visitor_key = ? AND status = ?",
		site.ID,
		visitorKey,
		model.WidgetSessionStatusActive,
	).Order("updated_at DESC").
		First(&widgetSession).Error; err == nil {
		switch {
		case credentialKey:
			reuseSession = true
		case widgetSessionHasMessages(widgetSession.SessionID):
			// 指纹命中已有消息历史的活跃会话：不复用，落入下方新建分支，
			// 防止陌生人重算指纹恢复他人客服会话（读历史、冒名发言）。
		default:
			// 空会话（尚无消息历史）可以续用，但把 visitor_key 轮换为高熵随机值再下发，
			// 避免前端把可重算的指纹 key 当作长期凭证存储。
			reuseSession = true
			visitorKey = newWidgetVisitorKey()
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if !reuseSession && !credentialKey {
		// 指纹派生身份只用于新会话：一律下发高熵随机 visitor_key 供前端存储。
		visitorKey = newWidgetVisitorKey()
	}

	if reuseSession {
		updates := map[string]interface{}{
			"last_page_url":       pageURL,
			"last_active_at":      now,
			"updated_at":          now,
			"last_init_ip_prefix": initIPPrefix,
			"last_init_ip":        initIP,
			"last_init_at":        now,
			"locale":              resolvedLocale,
		}
		if !credentialKey {
			updates["visitor_key"] = visitorKey
		}
		if visitorName != "" {
			updates["visitor_name"] = visitorName
		}
		if visitorEmail != "" {
			updates["visitor_email"] = visitorEmail
		}
		if err := store.DB.Model(&model.WidgetSession{}).
			Where("id = ?", widgetSession.ID).
			Updates(updates).Error; err != nil {
			return nil, err
		}
		if visitorName != "" {
			widgetSession.VisitorName = visitorName
		}
		if visitorEmail != "" {
			widgetSession.VisitorEmail = visitorEmail
		}
		widgetSession.LastPageURL = pageURL
		widgetSession.LastActiveAt = now
		widgetSession.UpdatedAt = now
		widgetSession.Locale = resolvedLocale

		// 复用旧 session 时，清除 owner 之前的"删除"标记，
		// 否则 conversations 列表查询会永久排除该 session。
		store.DB.Where("session_id = ? AND user_id = ?", widgetSession.SessionID, site.OwnerUserID).
			Delete(&model.SessionHistoryReset{})

		ensureAutoDelegateForSessionMember(widgetSession.SessionID, widgetSession.VisitorID, site.OwnerUserID)
	} else {
		visitorID := snowflake.GenID()
		sessionID := newSessionID()
		title := buildWidgetVisitorTitle(visitorName, visitorID)

		err := store.DB.Transaction(func(tx *gorm.DB) error {
			session := model.Session{
				SessionID:   sessionID,
				OwnerID:     site.OwnerUserID,
				SessionType: model.SessionTypeDirect,
				UpdatedAt:   now,
				CreatedAt:   now,
			}
			if err := tx.Create(&session).Error; err != nil {
				return err
			}

			ownerMember := model.SessionMember{
				SessionID:    sessionID,
				MemberID:     site.OwnerUserID,
				MemberType:   1,
				CustomTitle:  title,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			}
			visitorMember := model.SessionMember{
				SessionID:    sessionID,
				MemberID:     visitorID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			}
			if err := tx.Create(&ownerMember).Error; err != nil {
				return err
			}
			if err := tx.Create(&visitorMember).Error; err != nil {
				return err
			}

			widgetSession = model.WidgetSession{
				ID:               snowflake.GenID(),
				SiteID:           site.ID,
				OwnerUserID:      site.OwnerUserID,
				VisitorID:        visitorID,
				VisitorKey:       visitorKey,
				SessionID:        sessionID,
				VisitorName:      visitorName,
				VisitorEmail:     visitorEmail,
				Locale:           resolvedLocale,
				LastPageURL:      pageURL,
				LastInitIPPrefix: initIPPrefix,
				LastInitIP:       initIP,
				Status:           model.WidgetSessionStatusActive,
				CreatedAt:        now,
				UpdatedAt:        now,
				LastActiveAt:     now,
				LastInitAt:       now,
			}
			return tx.Create(&widgetSession).Error
		})
		if err != nil {
			return nil, err
		}

		// 与普通会话创建保持一致：为 owner 登记文字自动托管。
		// 发起方=访客，被托管方=owner，使 owner 配置的文字托管 agent 接管访客消息。
		ensureAutoDelegateForSessionMember(sessionID, visitorID, site.OwnerUserID)
	}

	token, expiresIn, err := jwtpkg.GenerateWidgetAccessToken(
		widgetSession.SiteID,
		widgetSession.SessionID,
		widgetSession.VisitorID,
		widgetSession.OwnerUserID,
		nil,
	)
	if err != nil {
		return nil, err
	}

	displayCfg, _ := decodeDisplayConfig(site.DisplayConfig)

	return &WidgetVisitorInitResp{
		SessionID:      widgetSession.SessionID,
		VisitorID:      widgetSession.VisitorID,
		VisitorKey:     visitorKey,
		WSURL:          wsURL,
		WidgetToken:    token,
		ExpiresIn:      expiresIn,
		VoiceEnabled:   widgetVoiceServiceAvailable(widgetSession.OwnerUserID, widgetSession.SessionID),
		DisplayConfig:  resolveVisitorDisplayConfig(displayCfg, resolvedLocale),
		ResolvedLocale: resolvedLocale,
	}, nil
}

// widgetVoiceServiceAvailable 判断该 widget 会话是否可发起语音客服：
// owner 已配会话级/用户级语音托管，且该 agent 为 type=4、可用、开放访客通话。
func widgetVoiceServiceAvailable(ownerID int64, sessionID string) bool {
	agentID := int64(0)
	if store.RDB != nil {
		if v, err := store.RDB.HGet(context.Background(), fmt.Sprintf("im:voice_delegate:%s:%d", sessionID, ownerID), "agent_id").Int64(); err == nil && v > 0 {
			agentID = v
		}
	}
	if agentID == 0 {
		if id, ok := LoadUserVoiceAutoDelegateAgentID(ownerID); ok {
			agentID = id
		}
	}
	if agentID == 0 || store.DB == nil {
		return false
	}
	var ag model.Agent
	if err := store.DB.Select("provider_type", "status", "voice_provider", "voice_model", "voice_api_key_cipher", "voice_allow_visitor").
		First(&ag, agentID).Error; err != nil {
		return false
	}
	return ag.Status == 1 && ag.ProviderType == model.AgentProviderVoice && ag.VoiceAllowVisitor &&
		ag.VoiceProvider != "" && ag.VoiceModel != "" && ag.VoiceAPIKeyCipher != ""
}

type widgetVisitorFingerprintInput struct {
	Origin       string
	ClientIP     string
	UserAgent    string
	VisitorEmail string
	VisitorName  string
	PageURL      string
}

const widgetVisitorFingerprintPrefix = "vkf_"

// newWidgetVisitorKey 生成高熵随机会话凭证 key（uuid），陌生人无法重算。
func newWidgetVisitorKey() string {
	return "vk_" + uuid.NewString()
}

// widgetSessionHasMessages 判断会话是否已有消息历史。
// 查询失败时按"有消息"处理：宁可新开会话，也不冒指纹撞库复用的风险。
func widgetSessionHasMessages(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	var count int64
	if err := store.DB.Model(&model.Message{}).
		Where("session_id = ? AND is_deleted = ?", sessionID, false).
		Limit(1).Count(&count).Error; err != nil {
		return true
	}
	return count > 0
}

func resolveWidgetVisitorKey(raw string, site *model.WidgetSite, in widgetVisitorFingerprintInput) string {
	normalized := normalizeProvidedVisitorKey(raw)
	if normalized != "" {
		return normalized
	}
	derived := deriveWidgetVisitorFingerprint(site, in)
	if derived == "" {
		return newWidgetVisitorKey()
	}
	return derived
}

func normalizeProvidedVisitorKey(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	if len(v) > 120 {
		v = v[:120]
	}
	var b strings.Builder
	b.Grow(len(v))
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == ':' || r == '.' {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func deriveWidgetVisitorFingerprint(site *model.WidgetSite, in widgetVisitorFingerprintInput) string {
	if site == nil || site.ID <= 0 {
		return ""
	}
	origin, _ := normalizeOrigin(in.Origin)
	pageHost := ""
	if u, err := url.Parse(strings.TrimSpace(in.PageURL)); err == nil {
		pageHost = strings.ToLower(strings.TrimSpace(u.Host))
	}
	ipPrefix := normalizeClientIPPrefix(in.ClientIP)
	ua := strings.ToLower(strings.TrimSpace(in.UserAgent))
	if len(ua) > 200 {
		ua = ua[:200]
	}
	email := strings.ToLower(strings.TrimSpace(in.VisitorEmail))
	name := strings.ToLower(strings.TrimSpace(in.VisitorName))
	seed := fmt.Sprintf(
		"site=%d|origin=%s|page=%s|ip=%s|ua=%s|email=%s|name=%s|salt=%s",
		site.ID, origin, pageHost, ipPrefix, ua, email, name, strings.TrimSpace(site.SiteSecretHash),
	)
	sum := sha256.Sum256([]byte(seed))
	return widgetVisitorFingerprintPrefix + fmt.Sprintf("%x", sum[:16])
}

func normalizeClientIP(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	// 与 matchIPRule / clientip.FromRequest 保持一致用 net.ParseIP，
	// IPv4-mapped-IPv6（::ffff:1.2.3.4）会归一化为 1.2.3.4。
	ip := net.ParseIP(v)
	if ip == nil {
		return ""
	}
	return ip.String()
}

func normalizeClientIPPrefix(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	ip, err := netip.ParseAddr(v)
	if err != nil {
		return ""
	}
	if ip.Is4() {
		b := ip.As4()
		return fmt.Sprintf("%d.%d.%d.0/24", b[0], b[1], b[2])
	}
	if ip.Is6() {
		b := ip.As16()
		return fmt.Sprintf("%x:%x:%x:%x::/64", uint16(b[0])<<8|uint16(b[1]), uint16(b[2])<<8|uint16(b[3]), uint16(b[4])<<8|uint16(b[5]), uint16(b[6])<<8|uint16(b[7]))
	}
	return ""
}

func widgetAllowVisitorInitRate(siteID int64, clientIP, visitorKey string) bool {
	if store.RDB == nil || siteID <= 0 {
		return true
	}
	ctx := context.Background()
	window := 60 * time.Second
	ipLimit := int64(30)
	fpLimit := int64(12)
	ipPart := normalizeClientIPPrefix(clientIP)
	if ipPart != "" {
		ipKey := fmt.Sprintf("widget:init:site:%d:ip:%s", siteID, ipPart)
		if !consumeSimpleRate(ctx, ipKey, window, ipLimit) {
			return false
		}
	}
	keyPart := strings.TrimSpace(visitorKey)
	if keyPart == "" {
		return true
	}
	fpKey := fmt.Sprintf("widget:init:site:%d:fp:%s", siteID, keyPart)
	return consumeSimpleRate(ctx, fpKey, window, fpLimit)
}

func consumeSimpleRate(ctx context.Context, key string, window time.Duration, limit int64) bool {
	if store.RDB == nil || key == "" || limit <= 0 {
		return true
	}
	n, err := store.RDB.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if n == 1 {
		_ = store.RDB.Expire(ctx, key, window).Err()
	}
	return n <= limit
}

// WidgetVisitorConfig returns the public display config for an active site,
// without creating a visitor session. Used by the loader to apply appearance
// and decide auto-expand before any session is created.
func WidgetVisitorConfig(siteKey, origin, rawLocale string) (*WidgetVisitorDisplayConfig, error) {
	siteKey = strings.TrimSpace(siteKey)
	if siteKey == "" {
		return nil, ErrWidgetInitInvalidInput
	}
	site, allowedOrigins, err := loadWidgetSiteByKey(siteKey)
	if err != nil {
		return nil, err
	}
	if !isWidgetOriginAllowed(strings.TrimSpace(origin), allowedOrigins) {
		return nil, ErrWidgetOriginNotAllowed
	}
	cfg, err := decodeDisplayConfig(site.DisplayConfig)
	if err != nil {
		return nil, err
	}
	resolved := resolveVisitorDisplayConfig(cfg, locale.Normalize(rawLocale))
	return &resolved, nil
}

func loadWidgetSiteByKey(siteKey string) (*model.WidgetSite, []string, error) {
	var site model.WidgetSite
	if err := store.DB.Where("site_key = ?", siteKey).First(&site).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrWidgetSiteNotFound
		}
		return nil, nil, err
	}
	if site.Status != model.WidgetSiteStatusActive {
		return nil, nil, ErrWidgetSiteDisabled
	}
	var allowedOrigins []string
	if len(site.AllowedOrigins) > 0 {
		if err := json.Unmarshal(site.AllowedOrigins, &allowedOrigins); err != nil {
			return nil, nil, ErrWidgetOriginNotAllowed
		}
	}
	return &site, allowedOrigins, nil
}

func isWidgetOriginAllowed(origin string, allowedOrigins []string) bool {
	if len(allowedOrigins) == 0 {
		return false
	}
	normalizedOrigin, ok := normalizeOrigin(origin)
	if !ok {
		return false
	}
	for _, item := range allowedOrigins {
		normalizedAllowed, ok := normalizeOrigin(item)
		if !ok {
			continue
		}
		if normalizedAllowed == normalizedOrigin {
			return true
		}
	}
	return false
}

func normalizeOrigin(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	host := strings.ToLower(strings.TrimSpace(u.Host))
	if (scheme != "http" && scheme != "https") || host == "" {
		return "", false
	}
	return scheme + "://" + host, true
}

func buildWidgetVisitorTitle(visitorName string, visitorID int64) string {
	name := strings.TrimSpace(visitorName)
	if name != "" {
		return "访客 " + name
	}
	if visitorID > 0 {
		id := strconv.FormatInt(visitorID, 10)
		if len(id) > 4 {
			id = id[len(id)-4:]
		}
		return "访客 #" + id
	}
	return "访客"
}

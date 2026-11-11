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

// WidgetVisitorClientInfo 描述移动端 App SDK（iOS/Android/Flutter）的宿主客户端身份。
// 网页 widget 不传该结构（nil），完全沿用现有 Origin 校验路径。
type WidgetVisitorClientInfo struct {
	Platform   string `json:"platform"`
	AppID      string `json:"app_id"`
	SDKVersion string `json:"sdk_version"`
}

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
	Client       *WidgetVisitorClientInfo
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
	wsURL := strings.TrimSpace(in.WSURL)
	if wsURL == "" {
		return nil, ErrWidgetInitInvalidInput
	}

	var appPlatform, appID, sdkVersion string
	if in.Client != nil {
		var ok bool
		appPlatform, appID, sdkVersion, ok = normalizeWidgetVisitorClient(in.Client)
		if !ok {
			return nil, ErrWidgetOriginNotAllowed
		}
	} else if origin == "" {
		return nil, ErrWidgetOriginNotAllowed
	}

	visitorKey := strings.TrimSpace(in.VisitorKey)
	visitorName := strings.TrimSpace(in.VisitorName)
	visitorEmail := strings.TrimSpace(in.VisitorEmail)
	pageURL := strings.TrimSpace(in.PageURL)
	resolvedLocale := locale.Normalize(in.Locale)

	site, allowedOrigins, allowedApps, err := loadWidgetSiteByKey(siteKey)
	if err != nil {
		return nil, err
	}
	if in.Client != nil {
		if !isWidgetAppAllowed(appID, allowedApps) {
			return nil, ErrWidgetOriginNotAllowed
		}
		if pageURL == "" {
			pageURL = fmt.Sprintf("app://%s/%s", appPlatform, appID)
		}
	} else {
		if !isWidgetOriginAllowed(origin, allowedOrigins) {
			return nil, ErrWidgetOriginNotAllowed
		}
	}
	// 只记录该站点第一次被访客接入的域名或 App（"app:<app_id>"）：WHERE first_origin = ''
	// 让并发的初始化请求最多写入一次，之后换域名/换 App 不会覆盖（last_page_url 不能做这个
	// 判断，它每次复用会话都会被覆盖）。
	firstOriginValue := ""
	if in.Client != nil {
		firstOriginValue = widgetAppFirstOriginPrefix + appID
	} else if originHost, ok := normalizeOriginHost(origin); ok {
		firstOriginValue = originHost
	}
	if firstOriginValue != "" {
		if err := store.DB.Model(&model.WidgetSite{}).
			Where("id = ? AND first_origin = ''", site.ID).
			Update("first_origin", firstOriginValue).Error; err != nil {
			return nil, err
		}
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
		Platform:     appPlatform,
		AppID:        appID,
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
			if err := tx.Create(&widgetSession).Error; err != nil {
				return err
			}
			return appendMembershipEventsTx(tx, sessionID, "add", visitorID, nil, now, sessionMemberChangedNotifyMeta{Title: title})
		})
		if err != nil {
			return nil, err
		}
		notifySyncV2Dirty(site.OwnerUserID)

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

	if appPlatform != "" && appID != "" {
		ownerID := widgetSession.OwnerUserID
		sessID := widgetSession.SessionID
		detail := map[string]any{
			"site_id":     site.ID,
			"site_key":    site.SiteKey,
			"visitor_id":  widgetSession.VisitorID,
			"visitor_key": visitorKey,
			"platform":    appPlatform,
			"app_id":      appID,
		}
		if sdkVersion != "" {
			detail["sdk_version"] = sdkVersion
		}
		WriteAuditLog(WriteAuditLogReq{
			EventType: "widget_visitor_init",
			UserID:    &ownerID,
			SessionID: &sessID,
			Detail:    detail,
			ClientIP:  in.ClientIP,
			UserAgent: in.UserAgent,
		})
	}

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
	Platform     string
	AppID        string
}

const (
	widgetVisitorFingerprintPrefix = "vkf_"
	widgetAppFirstOriginPrefix     = "app:"
)

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
	if in.Platform != "" && in.AppID != "" {
		origin = fmt.Sprintf("app:%s:%s", in.Platform, in.AppID)
	}
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
	return WidgetVisitorConfigWithClient(siteKey, origin, rawLocale, nil)
}

func WidgetVisitorConfigWithClient(siteKey, origin, rawLocale string, client *WidgetVisitorClientInfo) (*WidgetVisitorDisplayConfig, error) {
	siteKey = strings.TrimSpace(siteKey)
	if siteKey == "" {
		return nil, ErrWidgetInitInvalidInput
	}
	var appID string
	if client != nil {
		var ok bool
		_, appID, _, ok = normalizeWidgetVisitorClient(client)
		if !ok {
			return nil, ErrWidgetOriginNotAllowed
		}
	}
	site, allowedOrigins, allowedApps, err := loadWidgetSiteByKey(siteKey)
	if err != nil {
		return nil, err
	}
	if client != nil {
		if !isWidgetAppAllowed(appID, allowedApps) {
			return nil, ErrWidgetOriginNotAllowed
		}
	} else {
		if !isWidgetOriginAllowed(strings.TrimSpace(origin), allowedOrigins) {
			return nil, ErrWidgetOriginNotAllowed
		}
	}
	cfg, err := decodeDisplayConfig(site.DisplayConfig)
	if err != nil {
		return nil, err
	}
	resolved := resolveVisitorDisplayConfig(cfg, locale.Normalize(rawLocale))
	return &resolved, nil
}

func loadWidgetSiteByKey(siteKey string) (*model.WidgetSite, []string, []string, error) {
	var site model.WidgetSite
	if err := store.DB.Where("site_key = ?", siteKey).First(&site).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, ErrWidgetSiteNotFound
		}
		return nil, nil, nil, err
	}
	if site.Status != model.WidgetSiteStatusActive {
		return nil, nil, nil, ErrWidgetSiteDisabled
	}
	var allowedOrigins []string
	if len(site.AllowedOrigins) > 0 {
		if err := json.Unmarshal(site.AllowedOrigins, &allowedOrigins); err != nil {
			return nil, nil, nil, ErrWidgetOriginNotAllowed
		}
	}
	var allowedApps []string
	if len(site.AllowedApps) > 0 {
		if err := json.Unmarshal(site.AllowedApps, &allowedApps); err != nil {
			return nil, nil, nil, ErrWidgetOriginNotAllowed
		}
	}
	return &site, allowedOrigins, allowedApps, nil
}

func normalizeWidgetVisitorClient(c *WidgetVisitorClientInfo) (platform, appID, sdkVersion string, ok bool) {
	if c == nil {
		return "", "", "", false
	}
	platform = strings.ToLower(strings.TrimSpace(c.Platform))
	switch platform {
	case "ios", "android", "flutter":
	default:
		return "", "", "", false
	}
	appID, ok = normalizeAppID(c.AppID)
	if !ok {
		return "", "", "", false
	}
	sdkVersion = strings.TrimSpace(c.SDKVersion)
	if len(sdkVersion) > 64 {
		sdkVersion = sdkVersion[:64]
	}
	return platform, appID, sdkVersion, true
}

// normalizeAppID 校验并规范化移动端 App 标识（iOS bundle id / Android 包名）：
// 反向域名风格，至少两段点分标识符，每段以字母开头，仅含字母、数字、下划线或连字符，
// 统一转小写比较。
func normalizeAppID(raw string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if len(v) < 3 || len(v) > 200 {
		return "", false
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return "", false
	}
	for _, p := range parts {
		if len(p) == 0 {
			return "", false
		}
		first := p[0]
		if first < 'a' || first > 'z' {
			return "", false
		}
		for i := 0; i < len(p); i++ {
			ch := p[i]
			if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-') {
				return "", false
			}
		}
	}
	return v, true
}

func isWidgetAppAllowed(appID string, allowedApps []string) bool {
	if len(allowedApps) == 0 {
		return false
	}
	normalizedValue, ok := normalizeAppID(appID)
	if !ok {
		return false
	}
	for _, item := range allowedApps {
		normalizedAllowed, ok := normalizeAppID(item)
		if !ok {
			continue
		}
		if normalizedAllowed == normalizedValue {
			return true
		}
	}
	return false
}

// isWidgetFirstOriginAllowed 判断站点已记录的 first_origin（可能是网页 host，也可能是
// "app:<app_id>"）是否仍在最新的 allowed_origins / allowed_apps 白名单内。
func isWidgetFirstOriginAllowed(firstOrigin string, allowedOrigins, allowedApps []string) bool {
	normalized, ok := normalizeOriginHost(firstOrigin)
	if !ok {
		return false
	}
	if strings.HasPrefix(normalized, widgetAppFirstOriginPrefix) {
		appID := strings.TrimPrefix(normalized, widgetAppFirstOriginPrefix)
		return isWidgetAppAllowed(appID, allowedApps)
	}
	return isWidgetOriginHostAllowed(normalized, allowedOrigins)
}

func isWidgetOriginAllowed(origin string, allowedOrigins []string) bool {
	return widgetOriginAllowedBy(origin, allowedOrigins, false)
}

// isWidgetOriginHostAllowed 判断 first_origin 记录的 host 是否仍被
// allowed_origins 允许。first_origin 只存规范化 host（无协议、无端口），所以走
// 同一条校验路径、只按 host 比较；站点更新据此决定是否清空 first_origin。
func isWidgetOriginHostAllowed(host string, allowedOrigins []string) bool {
	return widgetOriginAllowedBy(host, allowedOrigins, true)
}

// widgetOriginAllowedBy 是访客 Origin 校验和 first_origin 校验共用的实现：
// 候选值先归一化，再逐项与 allowed_origins 的归一化结果全等比较。hostOnly
// 时两侧都只比较 host（协议、端口不参与），通配符条目在两种模式下都解析失败、
// 因此永远不匹配，与访客接入时的行为一致。
func widgetOriginAllowedBy(value string, allowedOrigins []string, hostOnly bool) bool {
	if len(allowedOrigins) == 0 {
		return false
	}
	normalize := normalizeOrigin
	if hostOnly {
		normalize = normalizeOriginHost
	}
	normalizedValue, ok := normalize(value)
	if !ok {
		return false
	}
	for _, item := range allowedOrigins {
		normalizedAllowed, ok := normalize(item)
		if !ok {
			continue
		}
		if normalizedAllowed == normalizedValue {
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

// normalizeOriginHost 把 Origin（或裸 host，例如 allowed_origins 里的条目，以及
// App 通道写入的 "app:<app_id>"）归一化为可展示、可比较的标识：小写，去掉协议、
// userinfo、端口、路径、query、fragment 和 IPv6 方括号。`*` 通配和解析不出的值
// 视为无效，与迁移 131 的 pg_temp.first_origin_host 规则保持一致。
func normalizeOriginHost(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.Contains(value, "*") {
		return "", false
	}
	if len(value) > len(widgetAppFirstOriginPrefix) && strings.EqualFold(value[:len(widgetAppFirstOriginPrefix)], widgetAppFirstOriginPrefix) {
		appID, ok := normalizeAppID(value[len(widgetAppFirstOriginPrefix):])
		if !ok {
			return "", false
		}
		return widgetAppFirstOriginPrefix + appID, true
	}
	// 没有协议时按 authority 解析，让 "example.com:8443/path?q=1" 也能只取 host。
	if !strings.Contains(value, "://") {
		value = "//" + strings.TrimLeft(value, "/")
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		return "", false
	}
	if u.Scheme != "" {
		scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
		if scheme != "http" && scheme != "https" {
			return "", false
		}
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" || strings.ContainsAny(host, " \t\r\n*") {
		return "", false
	}
	// url.Hostname 只去方括号，不校验内容：非数字端口、未加方括号的 IPv6
	// 等残值会带着冒号落到这里，必须按无效处理。
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", false
	}
	return host, true
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

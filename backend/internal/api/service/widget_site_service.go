package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/locale"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	widgetSiteStatusAll    int16 = 0
	widgetSiteDefaultLimit       = 20
	widgetSiteMaxLimit           = 100
)

var (
	ErrWidgetSiteInvalidInput = errors.New("invalid widget site payload")
	ErrWidgetSiteNotOwned     = errors.New("widget site not found or forbidden")
)

// WidgetDisplayConfig holds the visual/behavioral settings for a widget site.
// All fields are optional; omitted fields fall back to widget.js built-in defaults.
type WidgetDisplayConfig struct {
	ThemeColor  string `json:"theme_color,omitempty"`
	ButtonLabel string `json:"button_label,omitempty"`
	// Welcome 按语言存文案（key 为 locale.Supported 中的语言代码），
	// 访客侧接口据访客 locale 选取其中一份文案下发，见 resolveVisitorDisplayConfig。
	Welcome    map[string]string `json:"welcome,omitempty"`
	Position   string            `json:"position,omitempty"`
	AutoExpand bool              `json:"auto_expand,omitempty"`
	Title      string            `json:"title,omitempty"`
}

// WidgetVisitorDisplayConfig 是下发给访客端（widget.js/frame.html）的展示配置：
// Welcome 已按访客 locale 解析为单一文案，不透传完整多语言 map。
type WidgetVisitorDisplayConfig struct {
	ThemeColor  string `json:"theme_color,omitempty"`
	ButtonLabel string `json:"button_label,omitempty"`
	Welcome     string `json:"welcome,omitempty"`
	Position    string `json:"position,omitempty"`
	AutoExpand  bool   `json:"auto_expand,omitempty"`
	Title       string `json:"title,omitempty"`
}

// resolveVisitorDisplayConfig 把 owner 配置的多语言 WidgetDisplayConfig
// 解析为某一访客 locale 对应的单语言展示配置。
func resolveVisitorDisplayConfig(cfg WidgetDisplayConfig, loc string) WidgetVisitorDisplayConfig {
	return WidgetVisitorDisplayConfig{
		ThemeColor:  cfg.ThemeColor,
		ButtonLabel: cfg.ButtonLabel,
		Welcome:     locale.Pick(cfg.Welcome, loc),
		Position:    cfg.Position,
		AutoExpand:  cfg.AutoExpand,
		Title:       cfg.Title,
	}
}

type WidgetSiteDTO struct {
	ID             int64               `json:"id,string"`
	SiteKey        string              `json:"site_key"`
	SiteName       string              `json:"site_name"`
	AllowedOrigins []string            `json:"allowed_origins"`
	AllowedApps    []string            `json:"allowed_apps"`
	DisplayConfig  WidgetDisplayConfig `json:"display_config"`
	Status         int16               `json:"status"`
	CreatedAt      int64               `json:"created_at"`
	UpdatedAt      int64               `json:"updated_at"`
}

type WidgetSiteCreateInput struct {
	OwnerUserID    int64
	SiteName       string
	AllowedOrigins []string
	AllowedApps    []string
	DisplayConfig  WidgetDisplayConfig
}

type WidgetSiteCreateResp struct {
	Site       WidgetSiteDTO `json:"site"`
	SiteSecret string        `json:"site_secret"`
}

type WidgetSiteUpdateInput struct {
	OwnerUserID    int64
	SiteID         int64
	SiteName       string
	AllowedOrigins []string
	AllowedApps    []string
	DisplayConfig  WidgetDisplayConfig
	Status         int16
}

type WidgetSiteListInput struct {
	OwnerUserID int64
	Status      int16
	Limit       int
	Offset      int
}

type WidgetSiteListResp struct {
	Items  []WidgetSiteDTO `json:"items"`
	Total  int64           `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

func WidgetSiteCreate(in WidgetSiteCreateInput) (*WidgetSiteCreateResp, error) {
	if in.OwnerUserID <= 0 {
		return nil, ErrWidgetSiteInvalidInput
	}
	siteName := strings.TrimSpace(in.SiteName)
	if siteName == "" {
		return nil, ErrWidgetSiteInvalidInput
	}
	origins, apps, err := normalizeSiteAllowlists(in.AllowedOrigins, in.AllowedApps)
	if err != nil {
		return nil, err
	}

	siteKey, siteSecret, siteSecretHash, err := generateWidgetSiteCredentials()
	if err != nil {
		return nil, err
	}

	displayCfgRaw := mustMarshalDisplayConfig(in.DisplayConfig)

	now := time.Now().UTC()
	site := model.WidgetSite{
		ID:             snowflake.GenID(),
		OwnerUserID:    in.OwnerUserID,
		SiteKey:        siteKey,
		SiteSecretHash: siteSecretHash,
		SiteName:       siteName,
		AllowedOrigins: mustMarshalOrigins(origins),
		AllowedApps:    mustMarshalOrigins(apps),
		DisplayConfig:  displayCfgRaw,
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := store.DB.Create(&site).Error; err != nil {
		return nil, err
	}

	return &WidgetSiteCreateResp{Site: buildWidgetSiteDTO(site, origins, apps, in.DisplayConfig), SiteSecret: siteSecret}, nil
}

func WidgetSiteUpdate(in WidgetSiteUpdateInput) (*WidgetSiteDTO, error) {
	if in.OwnerUserID <= 0 || in.SiteID <= 0 {
		return nil, ErrWidgetSiteInvalidInput
	}
	siteName := strings.TrimSpace(in.SiteName)
	if siteName == "" || !isWidgetSiteStatusValid(in.Status) {
		return nil, ErrWidgetSiteInvalidInput
	}

	var site model.WidgetSite
	if err := store.DB.Where("id = ? AND owner_user_id = ?", in.SiteID, in.OwnerUserID).First(&site).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWidgetSiteNotOwned
		}
		return nil, err
	}

	// 字段缺失（nil）时保留库里原值，只有显式传了 []（non-nil 空切片）才清空，
	// 防止老版本客户端未携带 allowed_apps（或单改某一项）时误清空已有白名单。
	rawOrigins := in.AllowedOrigins
	if rawOrigins == nil {
		existingOrigins, err := decodeOrigins(site.AllowedOrigins)
		if err != nil {
			return nil, err
		}
		rawOrigins = existingOrigins
	}
	rawApps := in.AllowedApps
	if rawApps == nil {
		existingApps, err := decodeOrigins(site.AllowedApps)
		if err != nil {
			return nil, err
		}
		rawApps = existingApps
	}
	origins, apps, err := normalizeSiteAllowlists(rawOrigins, rawApps)
	if err != nil {
		return nil, err
	}

	displayCfgRaw := mustMarshalDisplayConfig(in.DisplayConfig)

	now := time.Now().UTC()
	updates := map[string]interface{}{
		"site_name":       siteName,
		"allowed_origins": mustMarshalOrigins(origins),
		"allowed_apps":    mustMarshalOrigins(apps),
		"display_config":  displayCfgRaw,
		"status":          in.Status,
		"updated_at":      now,
	}
	// 站长换域名或移除 App 后，first_origin 记录的老标识可能已不在新的白名单
	// 里：此时清空它，让第一个从新域名/新 App 接入的访客按 WidgetVisitorInit 的原有
	// 逻辑写入新值；在那之前客户端标题按 VisitorDomain() 退回站点名。只调换
	// 顺序、新增条目、改站点名或状态时仍被允许，first_origin 不变。
	if strings.TrimSpace(site.FirstOrigin) != "" && !isWidgetFirstOriginAllowed(site.FirstOrigin, origins, apps) {
		updates["first_origin"] = ""
		site.FirstOrigin = ""
	}
	if err := store.DB.Model(&model.WidgetSite{}).Where("id = ?", site.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	site.SiteName = siteName
	site.AllowedOrigins = mustMarshalOrigins(origins)
	site.AllowedApps = mustMarshalOrigins(apps)
	site.DisplayConfig = displayCfgRaw
	site.Status = in.Status
	site.UpdatedAt = now
	dto := buildWidgetSiteDTO(site, origins, apps, in.DisplayConfig)
	return &dto, nil
}

func WidgetSiteList(in WidgetSiteListInput) (*WidgetSiteListResp, error) {
	if in.OwnerUserID <= 0 {
		return nil, ErrWidgetSiteInvalidInput
	}
	if in.Status != widgetSiteStatusAll && !isWidgetSiteStatusValid(in.Status) {
		return nil, ErrWidgetSiteInvalidInput
	}
	limit := in.Limit
	if limit <= 0 {
		limit = widgetSiteDefaultLimit
	}
	if limit > widgetSiteMaxLimit {
		limit = widgetSiteMaxLimit
	}
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}

	query := store.DB.Model(&model.WidgetSite{}).Where("owner_user_id = ?", in.OwnerUserID)
	if in.Status != widgetSiteStatusAll {
		query = query.Where("status = ?", in.Status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var sites []model.WidgetSite
	if err := query.Order("updated_at DESC").Limit(limit).Offset(offset).Find(&sites).Error; err != nil {
		return nil, err
	}

	items := make([]WidgetSiteDTO, 0, len(sites))
	for _, site := range sites {
		origins, err := decodeOrigins(site.AllowedOrigins)
		if err != nil {
			return nil, err
		}
		apps, err := decodeOrigins(site.AllowedApps)
		if err != nil {
			return nil, err
		}
		dcfg, err := decodeDisplayConfig(site.DisplayConfig)
		if err != nil {
			return nil, err
		}
		items = append(items, buildWidgetSiteDTO(site, origins, apps, dcfg))
	}

	return &WidgetSiteListResp{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func WidgetSiteDetail(ownerUserID, siteID int64) (*WidgetSiteDTO, error) {
	if ownerUserID <= 0 || siteID <= 0 {
		return nil, ErrWidgetSiteInvalidInput
	}
	var site model.WidgetSite
	if err := store.DB.Where("id = ? AND owner_user_id = ?", siteID, ownerUserID).First(&site).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWidgetSiteNotOwned
		}
		return nil, err
	}
	origins, err := decodeOrigins(site.AllowedOrigins)
	if err != nil {
		return nil, err
	}
	apps, err := decodeOrigins(site.AllowedApps)
	if err != nil {
		return nil, err
	}
	dcfg, err := decodeDisplayConfig(site.DisplayConfig)
	if err != nil {
		return nil, err
	}
	dto := buildWidgetSiteDTO(site, origins, apps, dcfg)
	return &dto, nil
}

type WidgetSiteRotateSecretResp struct {
	SiteID     int64  `json:"site_id,string"`
	SiteKey    string `json:"site_key"`
	SiteSecret string `json:"site_secret"`
}

func WidgetSiteRotateSecret(ownerUserID, siteID int64) (*WidgetSiteRotateSecretResp, error) {
	if ownerUserID <= 0 || siteID <= 0 {
		return nil, ErrWidgetSiteInvalidInput
	}
	var site model.WidgetSite
	if err := store.DB.Where("id = ? AND owner_user_id = ?", siteID, ownerUserID).First(&site).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWidgetSiteNotOwned
		}
		return nil, err
	}
	_, siteSecret, siteSecretHash, err := generateWidgetSiteCredentials()
	if err != nil {
		return nil, err
	}
	if err := store.DB.Model(&model.WidgetSite{}).Where("id = ?", site.ID).Updates(map[string]interface{}{
		"site_secret_hash": siteSecretHash,
		"updated_at":       time.Now().UTC(),
	}).Error; err != nil {
		return nil, err
	}
	return &WidgetSiteRotateSecretResp{SiteID: site.ID, SiteKey: site.SiteKey, SiteSecret: siteSecret}, nil
}

func WidgetSiteDelete(ownerUserID, siteID int64) error {
	if ownerUserID <= 0 || siteID <= 0 {
		return ErrWidgetSiteInvalidInput
	}
	result := store.DB.Delete(&model.WidgetSite{}, "id = ? AND owner_user_id = ?", siteID, ownerUserID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrWidgetSiteNotOwned
	}
	return nil
}

func normalizeSiteAllowlists(rawOrigins, rawApps []string) ([]string, []string, error) {
	origins, err := normalizeAllowedOrigins(rawOrigins)
	if err != nil {
		return nil, nil, err
	}
	apps, err := normalizeAllowedApps(rawApps)
	if err != nil {
		return nil, nil, err
	}
	if len(origins) == 0 && len(apps) == 0 {
		return nil, nil, ErrWidgetSiteInvalidInput
	}
	return origins, apps, nil
}

func normalizeAllowedOrigins(origins []string) ([]string, error) {
	if len(origins) == 0 {
		return []string{}, nil
	}
	uniq := make(map[string]struct{}, len(origins))
	for _, item := range origins {
		if strings.TrimSpace(item) == "" {
			continue
		}
		normalized, ok := normalizeOrigin(item)
		if !ok {
			return nil, ErrWidgetSiteInvalidInput
		}
		uniq[normalized] = struct{}{}
	}
	if len(uniq) == 0 {
		return []string{}, nil
	}
	result := make([]string, 0, len(uniq))
	for origin := range uniq {
		result = append(result, origin)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeAllowedApps(apps []string) ([]string, error) {
	if len(apps) == 0 {
		return []string{}, nil
	}
	uniq := make(map[string]struct{}, len(apps))
	for _, item := range apps {
		if strings.TrimSpace(item) == "" {
			continue
		}
		normalized, ok := normalizeAppID(item)
		if !ok {
			return nil, ErrWidgetSiteInvalidInput
		}
		uniq[normalized] = struct{}{}
	}
	if len(uniq) == 0 {
		return []string{}, nil
	}
	result := make([]string, 0, len(uniq))
	for appID := range uniq {
		result = append(result, appID)
	}
	sort.Strings(result)
	return result, nil
}

func decodeOrigins(raw datatypes.JSON) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	var origins []string
	if err := json.Unmarshal(raw, &origins); err != nil {
		return nil, err
	}
	if origins == nil {
		return []string{}, nil
	}
	return origins, nil
}

func buildWidgetSiteDTO(site model.WidgetSite, origins, apps []string, dcfg WidgetDisplayConfig) WidgetSiteDTO {
	if origins == nil {
		origins = []string{}
	}
	if apps == nil {
		apps = []string{}
	}
	return WidgetSiteDTO{
		ID:             site.ID,
		SiteKey:        site.SiteKey,
		SiteName:       site.SiteName,
		AllowedOrigins: origins,
		AllowedApps:    apps,
		DisplayConfig:  dcfg,
		Status:         site.Status,
		CreatedAt:      site.CreatedAt.Unix(),
		UpdatedAt:      site.UpdatedAt.Unix(),
	}
}

func mustMarshalOrigins(origins []string) datatypes.JSON {
	raw, _ := json.Marshal(origins)
	return datatypes.JSON(raw)
}

func mustMarshalDisplayConfig(cfg WidgetDisplayConfig) datatypes.JSON {
	raw, _ := json.Marshal(cfg)
	return datatypes.JSON(raw)
}

func decodeDisplayConfig(raw datatypes.JSON) (WidgetDisplayConfig, error) {
	if len(raw) == 0 {
		return WidgetDisplayConfig{}, nil
	}
	var cfg WidgetDisplayConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return WidgetDisplayConfig{}, err
	}
	return cfg, nil
}

func isWidgetSiteStatusValid(status int16) bool {
	return status == model.WidgetSiteStatusActive || status == model.WidgetSiteStatusDisabled
}

func generateWidgetSiteCredentials() (siteKey, siteSecret, siteSecretHash string, err error) {
	siteKeyToken, err := randomToken(18)
	if err != nil {
		return "", "", "", err
	}
	siteSecretToken, err := randomToken(24)
	if err != nil {
		return "", "", "", err
	}
	siteKey = "wk_" + siteKeyToken
	siteSecret = "ws_" + siteSecretToken
	siteSecretHash = hashWidgetSiteSecret(siteSecret)
	return
}

func randomToken(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashWidgetSiteSecret(secret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

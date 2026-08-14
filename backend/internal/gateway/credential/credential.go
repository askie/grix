// Package credential 管理网关上游厂商的官方凭据（推理转发Key、对账AK/SK）。
//
// 凭据由塘主后台动态增删，密文落库（AES-GCM，复用 internal/pkg/secretcrypto），
// 网关运行时按厂商解密取用，支持每厂商挂多把推理Key轮询分流/热切换。
// 带短TTL内存缓存：后台改动最长 cacheTTL 后自动在各网关副本生效，无需重启。
package credential

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
)

const (
	PurposeInference = model.GatewayCredentialPurposeInference
	PurposeReconcile = model.GatewayCredentialPurposeReconcile

	// cacheTTL 是解密后凭据的内存缓存有效期。后台增删/启停后，最长这么久各网关副本自动生效。
	cacheTTL = 15 * time.Second
)

// ErrNoCredential 表示该厂商当前没有任何启用中的凭据。
var ErrNoCredential = errors.New("credential: no enabled credential for provider")

// Resolved 是解密后的可直接使用的凭据。
type Resolved struct {
	ID        int64
	Provider  string
	APIKey    string // 推理Key，或对账的 AccessKey
	APISecret string // 对账的 SecretKey（推理场景为空）
	BaseURL   string // 端点覆盖，空则用适配器默认
	Region    string // 火山对账区域，空则默认
}

// Service 提供凭据的读取（带缓存+轮询）与后台增删。goroutine 安全。
type Service struct {
	db *gorm.DB

	mu    sync.Mutex
	cache map[string]*cacheEntry // key: provider|purpose
	rr    map[string]*uint64     // 轮询计数器 key: provider|purpose
}

type cacheEntry struct {
	items    []Resolved
	expireAt time.Time
}

func New(db *gorm.DB) *Service {
	return &Service{db: db, cache: map[string]*cacheEntry{}, rr: map[string]*uint64{}}
}

func cacheKey(provider, purpose string) string { return provider + "|" + purpose }

// enabledResolved 返回某厂商+用途下所有启用凭据的解密结果（带TTL缓存）。
func (s *Service) enabledResolved(provider, purpose string) ([]Resolved, error) {
	key := cacheKey(provider, purpose)
	s.mu.Lock()
	if ent := s.cache[key]; ent != nil && time.Now().Before(ent.expireAt) {
		items := ent.items
		s.mu.Unlock()
		return items, nil
	}
	s.mu.Unlock()

	var rows []model.GatewayUpstreamCredential
	if err := s.db.Where("provider = ? AND purpose = ? AND enabled = ?", provider, purpose, true).
		Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]Resolved, 0, len(rows))
	for _, r := range rows {
		// 单条解密失败（密文损坏 / 加密主密钥被轮换过）只跳过并记错误日志，不能让一把坏Key
		// 拖垮同厂商其余所有好Key——多把Key本就是为了冗余分流。
		apiKey, err := secretcrypto.Decrypt(r.APIKeyEnc)
		if err != nil {
			logger.L.Errorf("credential: skip credential %d (provider=%s purpose=%s): decrypt api_key failed: %v", r.ID, r.Provider, r.Purpose, err)
			continue
		}
		apiSecret, err := secretcrypto.Decrypt(r.APISecretEnc)
		if err != nil {
			logger.L.Errorf("credential: skip credential %d (provider=%s purpose=%s): decrypt api_secret failed: %v", r.ID, r.Provider, r.Purpose, err)
			continue
		}
		items = append(items, Resolved{
			ID: r.ID, Provider: r.Provider, APIKey: apiKey, APISecret: apiSecret,
			BaseURL: r.BaseURL, Region: r.Region,
		})
	}
	s.mu.Lock()
	s.cache[key] = &cacheEntry{items: items, expireAt: time.Now().Add(cacheTTL)}
	s.mu.Unlock()
	return items, nil
}

// NextInference 轮询取一把启用中的推理转发凭据；一把都没有返回 ErrNoCredential。
func (s *Service) NextInference(provider string) (Resolved, error) {
	items, err := s.enabledResolved(provider, PurposeInference)
	if err != nil {
		return Resolved{}, err
	}
	if len(items) == 0 {
		return Resolved{}, ErrNoCredential
	}
	idx := s.nextIndex(cacheKey(provider, PurposeInference), len(items))
	return items[idx], nil
}

// FirstReconcile 取第一把启用中的对账凭据（对账用单把稳定即可）。
func (s *Service) FirstReconcile(provider string) (Resolved, bool) {
	items, err := s.enabledResolved(provider, PurposeReconcile)
	if err != nil || len(items) == 0 {
		return Resolved{}, false
	}
	return items[0], true
}

func (s *Service) nextIndex(key string, n int) int {
	s.mu.Lock()
	ctr := s.rr[key]
	if ctr == nil {
		var c uint64
		ctr = &c
		s.rr[key] = ctr
	}
	s.mu.Unlock()
	v := atomic.AddUint64(ctr, 1) - 1
	return int(v % uint64(n))
}

// --- 后台增删（塘主后台 / gatewayadmin 用） ---

type CreateInput struct {
	Provider  string
	Purpose   string
	APIKey    string
	APISecret string
	BaseURL   string
	Region    string
	Label     string
}

// Create 加密后写入一把新凭据，返回记录（api_key 只在此处收明文，之后库里只存密文）。
func (s *Service) Create(in CreateInput) (*model.GatewayUpstreamCredential, error) {
	if in.Provider == "" || in.APIKey == "" {
		return nil, errors.New("credential: provider 和 api_key 必填")
	}
	purpose := in.Purpose
	if purpose == "" {
		purpose = PurposeInference
	}
	if purpose != PurposeInference && purpose != PurposeReconcile {
		return nil, fmt.Errorf("credential: 非法 purpose %q", purpose)
	}
	encKey, err := secretcrypto.Encrypt(in.APIKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt api_key: %w", err)
	}
	encSecret, err := secretcrypto.Encrypt(in.APISecret)
	if err != nil {
		return nil, fmt.Errorf("encrypt api_secret: %w", err)
	}
	row := model.GatewayUpstreamCredential{
		ID:           snowflake.GenID(),
		Provider:     in.Provider,
		Purpose:      purpose,
		APIKeyEnc:    encKey,
		APISecretEnc: encSecret,
		KeyHint:      secretcrypto.Hint(in.APIKey),
		BaseURL:      in.BaseURL,
		Region:       in.Region,
		Label:        in.Label,
		Enabled:      true,
	}
	if err := s.db.Create(&row).Error; err != nil {
		return nil, err
	}
	s.invalidate(in.Provider, purpose)
	return &row, nil
}

// SetEnabled 启用/停用一把凭据。
func (s *Service) SetEnabled(id int64, enabled bool) error {
	res := s.db.Model(&model.GatewayUpstreamCredential{}).Where("id = ?", id).Update("enabled", enabled)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	s.invalidateAll()
	return nil
}

// Delete 删除一把凭据。
func (s *Service) Delete(id int64) error {
	res := s.db.Delete(&model.GatewayUpstreamCredential{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	s.invalidateAll()
	return nil
}

// List 列出凭据（可选按厂商过滤），永远不含明文/密文，只带 KeyHint。
func (s *Service) List(provider string) ([]model.GatewayUpstreamCredential, error) {
	q := s.db.Order("provider ASC, purpose ASC, id DESC")
	if provider != "" {
		q = q.Where("provider = ?", provider)
	}
	var rows []model.GatewayUpstreamCredential
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// invalidate 清掉本进程内某 provider+purpose 的缓存（跨进程副本仍靠 cacheTTL 过期）。
func (s *Service) invalidate(provider, purpose string) {
	s.mu.Lock()
	delete(s.cache, cacheKey(provider, purpose))
	s.mu.Unlock()
}

func (s *Service) invalidateAll() {
	s.mu.Lock()
	s.cache = map[string]*cacheEntry{}
	s.mu.Unlock()
}

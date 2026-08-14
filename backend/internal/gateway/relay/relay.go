// Package relay 管"Grix中转"的用户级模型设置：兜底模型 + 模型映射表。
//
// 设计要点：映射与兜底只存在于后端，由网关在每次请求时解析，不下发给 connector。
// 这样用户改了设置下一次请求就生效（不用广播、不用发插件版本），
// 且任何来源的请求——托管Agent、裸虚拟Key直连、将来任何客户端——都同样享有映射和兜底，
// 未知模型永远不会打挂链路。
package relay

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
)

const (
	// SystemDefaultModelKey 是"系统默认兜底模型"在 system_settings 里的键。
	// 塘主后台可改，改完立即生效、不用发版。
	SystemDefaultModelKey = "gateway.default_model"

	// builtinDefaultModel 是连系统设置都没配时的最后兜底。
	// 取便宜那档：兜底模型要接住所有未配映射的请求和未来所有新模型，
	// 默认便宜比默认贵安全——用户不该因为"什么都没配"就被扣得肉疼。
	builtinDefaultModel = "deepseek-v4-flash"

	cacheKeyPrefix = "gateway:relay:settings:"
	// cacheTTL 刻意压到 30 秒：cache-aside 有经典竞态——gateway 缓存未命中正在读库（读到旧值）
	// 的几毫秒里，api 侧保存了新设置并删缓存（删了个空），随后 gateway 把旧值写回缓存。
	// 用户"边跑 Claude Code 边在网页改映射"恰好是请求最密集的时刻，这个窗口并不罕见。
	// 这张表是主键单查、读库近乎免费，长 TTL 收益极小；30 秒把"保存了却没生效"的最坏时长压进可接受范围。
	cacheTTL = 30 * time.Second
)

// Settings 是某钱包的中转模型设置。
type Settings struct {
	// DefaultModel 兜底模型：没被映射命中、本身又不是后端支持模型的请求都落到它。
	DefaultModel string `json:"default_model"`
	// ModelMap 是 {客户端侧模型名: 后端支持的模型名}。
	ModelMap map[string]string `json:"model_map"`
}

type Service struct {
	db  *gorm.DB
	rdb *redis.Client // 可为 nil：缓存只是加速，全部读写都能降级为直接查库
}

func New(db *gorm.DB, rdb *redis.Client) *Service {
	return &Service{db: db, rdb: rdb}
}

// Get 取某钱包的中转设置。没有设置行时回落到"系统默认模型 + 空映射"，
// 保证任何钱包（哪怕从没进过设置页）都有兜底模型可用。
//
// 缓存只在命中时抄近路；Redis 不可用或读写失败一律降级为直接查库——
// 缓存问题绝不能挡住计费主链路。
func (s *Service) Get(walletID int64) (Settings, error) {
	if cached, ok := s.readCache(walletID); ok {
		return cached, nil
	}

	var row model.GatewayRelaySetting
	err := s.db.Where("wallet_id = ?", walletID).First(&row).Error
	var out Settings
	switch {
	case err == nil:
		out = Settings{DefaultModel: row.DefaultModel, ModelMap: toStringMap(row.ModelMap)}
	case errors.Is(err, gorm.ErrRecordNotFound):
		out = Settings{DefaultModel: s.SystemDefaultModel(), ModelMap: map[string]string{}}
	default:
		return Settings{}, err
	}

	s.writeCache(walletID, out)
	return out, nil
}

// Put 覆盖保存某钱包的中转设置。合法性（默认模型与映射目标必须是后端支持的模型）
// 由调用方在 API 层校验后再进来，这里只负责落库与失效缓存。
func (s *Service) Put(walletID int64, in Settings) error {
	if in.ModelMap == nil {
		in.ModelMap = map[string]string{}
	}
	row := model.GatewayRelaySetting{
		WalletID:     walletID,
		DefaultModel: in.DefaultModel,
		ModelMap:     toJSONMap(in.ModelMap),
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "wallet_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"default_model", "model_map", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}
	s.invalidate(walletID)
	return nil
}

// SystemDefaultModel 读塘主后台配的系统默认兜底模型；没配或配了空值时用内置常量。
func (s *Service) SystemDefaultModel() string {
	var row model.SystemSetting
	if err := s.db.Where("key = ?", SystemDefaultModelKey).First(&row).Error; err != nil {
		return builtinDefaultModel
	}
	var v string
	if err := json.Unmarshal(row.Value, &v); err != nil {
		return builtinDefaultModel
	}
	if v = strings.TrimSpace(v); v == "" {
		return builtinDefaultModel
	}
	return v
}

func cacheKey(walletID int64) string {
	return cacheKeyPrefix + strconv.FormatInt(walletID, 10)
}

func (s *Service) readCache(walletID int64) (Settings, bool) {
	if s.rdb == nil {
		return Settings{}, false
	}
	raw, err := s.rdb.Get(context.Background(), cacheKey(walletID)).Bytes()
	if err != nil {
		return Settings{}, false
	}
	var out Settings
	if err := json.Unmarshal(raw, &out); err != nil {
		return Settings{}, false
	}
	if out.ModelMap == nil {
		out.ModelMap = map[string]string{}
	}
	return out, true
}

func (s *Service) writeCache(walletID int64, in Settings) {
	if s.rdb == nil {
		return
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return
	}
	if err := s.rdb.Set(context.Background(), cacheKey(walletID), raw, cacheTTL).Err(); err != nil {
		logger.L.Warnf("relay: write settings cache failed, wallet=%d err=%v", walletID, err)
	}
}

// invalidate 在设置变更后删缓存。删失败要吼出来：缓存最长 cacheTTL 才自然过期，
// 这段时间里用户改的映射不会生效，属于"用户点了保存但没反应"的可感知故障。
func (s *Service) invalidate(walletID int64) {
	if s.rdb == nil {
		return
	}
	if err := s.rdb.Del(context.Background(), cacheKey(walletID)).Err(); err != nil {
		logger.L.Errorf("relay: invalidate settings cache failed, wallet=%d err=%v (stale mapping may serve up to %s)", walletID, err, cacheTTL)
	}
}

// toStringMap 把库里的 JSONB 收敛成 map[string]string：非字符串值和空值直接丢掉，
// 不让脏数据把请求路由到一个不存在的模型上。
func toStringMap(in datatypes.JSONMap) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		sv, ok := v.(string)
		if !ok {
			continue
		}
		if k = strings.TrimSpace(k); k == "" {
			continue
		}
		if sv = strings.TrimSpace(sv); sv == "" {
			continue
		}
		out[k] = sv
	}
	return out
}

func toJSONMap(in map[string]string) datatypes.JSONMap {
	out := make(datatypes.JSONMap, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

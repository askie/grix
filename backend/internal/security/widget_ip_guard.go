package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

// widget 访客 IP 封禁守卫：为 widget 访客 init / WS 握手 / 消息收发提供
// owner 维度的 IP 封禁判断，以及 ban 访客时的规则写入（upsert）。
//
// 防篡改模型与 agent_ip_guard 一致：每条规则入库时用服务端密钥对
// (owner_user_id, ip_cidr, expires_at) 算 HMAC 存入 signature 列；加载时逐条验签，
// 签名不符（例如有人直改数据库）的规则拒绝生效并打告警日志。
// 签名防"改"，不防"删"——删除由管理接口的属主校验约束。

// WidgetIPBanDefaultTTL 是 IP 封禁的默认有效期：7 天。到期规则不再生效；
// 再次封禁同一 IP 走 upsert，把过期时间刷新为 now+7天。
const WidgetIPBanDefaultTTL = 7 * 24 * time.Hour

// widgetIPBanCacheTTL 是封禁规则的内存缓存有效期。WS 消息级检查每条消息都会
// 触发判定，必须走缓存避免每消息一次 DB 查询；BanWidgetIP / 删除接口会主动失效缓存。
const widgetIPBanCacheTTL = 30 * time.Second

type widgetIPBanCacheEntry struct {
	rules    []model.WidgetIPBan
	loadedAt time.Time
}

// widgetIPBanCache key=ownerUserID(int64)，value=widgetIPBanCacheEntry。
var widgetIPBanCache sync.Map

// widgetIPBanSigningKey 派生 HMAC 密钥：回退 JWT secret，
// 域分隔避免同一 JWT secret 被跨用途复用（同 agent-ip-rule 模式；
// config 中没有 widget 专用密钥配置项，故只用 JWT secret）。
func widgetIPBanSigningKey() []byte {
	secret := strings.TrimSpace(config.C.JWT.Secret)
	sum := sha256.Sum256([]byte("widget-ip-ban:" + secret))
	return sum[:]
}

// SignWidgetIPBan 计算规则签名（base64url，无填充）。
// expires_at 以 Unix 秒参与签名，规避数据库时间精度/时区往返差异。
func SignWidgetIPBan(ownerUserID int64, ipCIDR string, expiresAt *time.Time) string {
	expiresUnix := int64(0)
	if expiresAt != nil {
		expiresUnix = expiresAt.UTC().Unix()
	}
	mac := hmac.New(sha256.New, widgetIPBanSigningKey())
	fmt.Fprintf(mac, "%d\n%s\n%d", ownerUserID, strings.TrimSpace(ipCIDR), expiresUnix)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyWidgetIPBan 验证规则签名是否有效。
func VerifyWidgetIPBan(rule *model.WidgetIPBan) bool {
	if rule == nil {
		return false
	}
	expected := SignWidgetIPBan(rule.OwnerUserID, rule.IPCIDR, rule.ExpiresAt)
	return hmac.Equal([]byte(expected), []byte(rule.Signature))
}

// BanWidgetIP 把 ip 加入该 owner 的访客 IP 封禁，有效期 ttl（调用方传
// WidgetIPBanDefaultTTL）。同一 (owner, ip) 已存在时刷新 expires_at/reason/
// source_session_id 并重新签名（upsert 去重）。ip 为空或非法时静默跳过（返回 nil），
// 因为这只是 ban 会话的附带动作，不应阻断主流程。
func BanWidgetIP(ownerUserID int64, ip, reason, sourceSessionID string, ttl time.Duration) error {
	normalized, err := NormalizeIPCIDR(ip)
	if err != nil {
		return nil
	}
	if store.DB == nil || ownerUserID <= 0 {
		return nil
	}
	now := time.Now().UTC()
	// 截断到秒：签名按 Unix 秒计算，避免 DB 时间精度差异导致验签失败。
	expiresAt := now.Add(ttl).UTC().Truncate(time.Second)
	signature := SignWidgetIPBan(ownerUserID, normalized, &expiresAt)

	var existing model.WidgetIPBan
	err = store.DB.Where("owner_user_id = ? AND ip_cidr = ?", ownerUserID, normalized).First(&existing).Error
	switch {
	case err == nil:
		if err := store.DB.Model(&model.WidgetIPBan{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{
			"reason":            strings.TrimSpace(reason),
			"source_session_id": strings.TrimSpace(sourceSessionID),
			"expires_at":        expiresAt,
			"signature":         signature,
			"updated_at":        now,
		}).Error; err != nil {
			return err
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		rule := model.WidgetIPBan{
			ID:              snowflake.GenID(),
			OwnerUserID:     ownerUserID,
			IPCIDR:          normalized,
			Reason:          strings.TrimSpace(reason),
			SourceSessionID: strings.TrimSpace(sourceSessionID),
			ExpiresAt:       &expiresAt,
			Signature:       signature,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := store.DB.Create(&rule).Error; err != nil {
			return err
		}
	default:
		return err
	}
	InvalidateWidgetIPBanCache(ownerUserID)
	return nil
}

// InvalidateWidgetIPBanCache 失效该 owner 的封禁规则缓存；
// BanWidgetIP 与删除接口都会调用。
func InvalidateWidgetIPBanCache(ownerUserID int64) {
	widgetIPBanCache.Delete(ownerUserID)
}

// loadValidWidgetIPBans 加载该 owner 未过期的封禁规则（带 30s 内存缓存），
// 验签失败的规则剔除并告警。
func loadValidWidgetIPBans(ownerUserID int64) []model.WidgetIPBan {
	if store.DB == nil {
		return nil
	}
	if cached, ok := widgetIPBanCache.Load(ownerUserID); ok {
		entry := cached.(widgetIPBanCacheEntry)
		if time.Since(entry.loadedAt) < widgetIPBanCacheTTL {
			return entry.rules
		}
	}
	now := time.Now().UTC()
	var rules []model.WidgetIPBan
	if err := store.DB.Where(
		"owner_user_id = ? AND (expires_at IS NULL OR expires_at > ?)",
		ownerUserID, now,
	).Find(&rules).Error; err != nil {
		logger.L.Errorf("widget ip bans load failed owner=%d err=%v", ownerUserID, err)
		return nil
	}
	valid := rules[:0]
	for i := range rules {
		if !VerifyWidgetIPBan(&rules[i]) {
			logger.L.Errorf(
				"widget ip ban signature mismatch (possible tampering), rule ignored: id=%d owner=%d cidr=%s",
				rules[i].ID, rules[i].OwnerUserID, rules[i].IPCIDR,
			)
			continue
		}
		valid = append(valid, rules[i])
	}
	widgetIPBanCache.Store(ownerUserID, widgetIPBanCacheEntry{rules: valid, loadedAt: now})
	return valid
}

// IsWidgetIPBanned 判断 ip 是否被该 owner 封禁（对其名下所有 widget 站点生效）。
// 空 IP / 非法 owner 一律放行。
func IsWidgetIPBanned(ownerUserID int64, ip string) bool {
	if ownerUserID <= 0 || strings.TrimSpace(ip) == "" {
		return false
	}
	for _, rule := range loadValidWidgetIPBans(ownerUserID) {
		if matchIPRule(rule.IPCIDR, ip) {
			return true
		}
	}
	return false
}

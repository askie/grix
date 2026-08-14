package service

import (
	"errors"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
)

// widget 访客 IP 封禁的管理接口（owner 鉴权）：列表 + 删除。
// 与 visitor_key 会话封禁相互独立，解除 IP 封禁不影响已被封的会话。

var ErrWidgetIPBanNotOwned = errors.New("widget ip ban not found or forbidden")

type WidgetIPBanDTO struct {
	ID              int64  `json:"id,string"`
	IPCIDR          string `json:"ip_cidr"`
	Reason          string `json:"reason"`
	SourceSessionID string `json:"source_session_id"`
	ExpiresAt       int64  `json:"expires_at"` // unix 秒，0 表示永不过期
	Expired         bool   `json:"expired"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

type WidgetIPBanListResp struct {
	Items []WidgetIPBanDTO `json:"items"`
	Total int64            `json:"total"`
}

// WidgetIPBanList 返回当前 owner 的全部 IP 封禁规则（含已过期的，前端据此展示状态）。
func WidgetIPBanList(ownerUserID int64) (*WidgetIPBanListResp, error) {
	if ownerUserID <= 0 {
		return nil, ErrWidgetSiteInvalidInput
	}
	var rules []model.WidgetIPBan
	if err := store.DB.Where("owner_user_id = ?", ownerUserID).
		Order("updated_at DESC").Find(&rules).Error; err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	items := make([]WidgetIPBanDTO, 0, len(rules))
	for _, rule := range rules {
		items = append(items, toWidgetIPBanDTO(rule, now))
	}
	return &WidgetIPBanListResp{Items: items, Total: int64(len(items))}, nil
}

// WidgetIPBanDelete 删除属于该 owner 的一条 IP 封禁规则，并失效判定缓存。
func WidgetIPBanDelete(ownerUserID, id int64) error {
	if ownerUserID <= 0 || id <= 0 {
		return ErrWidgetSiteInvalidInput
	}
	res := store.DB.Where("id = ? AND owner_user_id = ?", id, ownerUserID).Delete(&model.WidgetIPBan{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrWidgetIPBanNotOwned
	}
	security.InvalidateWidgetIPBanCache(ownerUserID)
	return nil
}

func toWidgetIPBanDTO(rule model.WidgetIPBan, now time.Time) WidgetIPBanDTO {
	dto := WidgetIPBanDTO{
		ID:              rule.ID,
		IPCIDR:          rule.IPCIDR,
		Reason:          rule.Reason,
		SourceSessionID: rule.SourceSessionID,
		CreatedAt:       rule.CreatedAt.Unix(),
		UpdatedAt:       rule.UpdatedAt.Unix(),
	}
	if rule.ExpiresAt != nil {
		dto.ExpiresAt = rule.ExpiresAt.Unix()
		dto.Expired = !rule.ExpiresAt.After(now)
	}
	return dto
}

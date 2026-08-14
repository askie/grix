package service

import (
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// CallRecordItem 通话历史列表项
type CallRecordItem struct {
	ID              int64  `json:"id,string"`
	SessionID       string `json:"session_id"`
	CallerID        int64  `json:"caller_id,string"`
	CalleeID        int64  `json:"callee_id,string"`
	CallMode        int16  `json:"call_mode"`
	State           int16  `json:"state"`
	DelegationMode  string `json:"delegation_mode"`
	StartedAt       *int64 `json:"started_at,omitempty"`
	AnsweredAt      *int64 `json:"answered_at,omitempty"`
	EndedAt         *int64 `json:"ended_at,omitempty"`
	DurationSeconds *int   `json:"duration_seconds,omitempty"`
	EndReason       string `json:"end_reason,omitempty"`
}

// CallRecordListResp 通话历史分页响应
type CallRecordListResp struct {
	Items    []CallRecordItem `json:"items"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

// ListCallRecords 查询当前用户的通话历史（主叫或被叫），按 started_at 倒序。
func ListCallRecords(userID int64, page, pageSize int) (*CallRecordListResp, error) {
	offset := (page - 1) * pageSize

	var records []model.CallRecord
	var total int64

	db := store.DB.Model(&model.CallRecord{}).
		Where("caller_id = ? OR callee_id = ?", userID, userID)

	if err := db.Count(&total).Error; err != nil {
		return nil, err
	}

	if err := db.Order("started_at DESC").
		Offset(offset).Limit(pageSize).
		Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]CallRecordItem, 0, len(records))
	for _, r := range records {
		item := CallRecordItem{
			ID:             r.ID,
			SessionID:      r.SessionID,
			CallerID:       r.CallerID,
			CalleeID:       r.CalleeID,
			CallMode:       r.CallMode,
			State:          r.State,
			DelegationMode: r.DelegationMode,
			EndReason:      r.EndReason,
			DurationSeconds: r.DurationSeconds,
		}
		if r.StartedAt != nil {
			ms := r.StartedAt.UnixMilli()
			item.StartedAt = &ms
		}
		if r.AnsweredAt != nil {
			ms := r.AnsweredAt.UnixMilli()
			item.AnsweredAt = &ms
		}
		if r.EndedAt != nil {
			ms := r.EndedAt.UnixMilli()
			item.EndedAt = &ms
		}
		items = append(items, item)
	}

	return &CallRecordListResp{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

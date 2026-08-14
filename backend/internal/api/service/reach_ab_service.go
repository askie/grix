package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
)

type ABVariantReq struct {
	Variant    string `json:"variant"`
	TemplateID int64  `json:"template_id,string"`
}

type CreateABTestReq struct {
	Variants    []ABVariantReq `json:"variants"`
	Channels    []string       `json:"channels"`
	Region      string         `json:"region"`
	Audience    *ReachAudience `json:"audience,omitempty"`
	ScheduledAt *time.Time     `json:"scheduled_at,omitempty"`
}

type ABTestResult struct {
	ABGroupID string            `json:"ab_group_id"`
	Tasks     []model.ReachTask `json:"tasks"`
}

func CreateABTest(ctx context.Context, req CreateABTestReq, adminID int64) (*ABTestResult, error) {
	if len(req.Variants) < 2 {
		return nil, fmt.Errorf("A/B test requires at least 2 variants")
	}
	if _, err := parseReachTestUserIDs(req.Audience); err != nil {
		return nil, err
	}

	groupID := fmt.Sprintf("ab_%d", snowflake.GenID())
	channelsJSON, _ := json.Marshal(req.Channels)
	audienceJSON, _ := json.Marshal(req.Audience)

	status := model.ReachStatusSending
	if req.ScheduledAt != nil && req.ScheduledAt.After(time.Now().UTC()) {
		status = model.ReachStatusScheduled
	}

	var tasks []model.ReachTask
	for _, v := range req.Variants {
		var tpl model.ReachTemplate
		if err := store.DB.Where("id = ?", v.TemplateID).First(&tpl).Error; err != nil {
			return nil, fmt.Errorf("template %d not found for variant %s", v.TemplateID, v.Variant)
		}

		task := model.ReachTask{
			ID:          snowflake.GenID(),
			Kind:        model.ReachKindMarketing,
			TemplateID:  v.TemplateID,
			Channels:    channelsJSON,
			Audience:    audienceJSON,
			Status:      status,
			ScheduledAt: req.ScheduledAt,
			Region:      req.Region,
			ABGroupID:   groupID,
			ABVariant:   v.Variant,
			CreatedBy:   adminID,
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		if err := store.DB.Create(&task).Error; err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	if status == model.ReachStatusSending {
		for _, task := range tasks {
			var tpl model.ReachTemplate
			store.DB.Where("id = ?", task.TemplateID).First(&tpl)
			go executeMarketingTask(ctx, task.ID, tpl, req.Channels, req.Audience)
		}
	}

	return &ABTestResult{ABGroupID: groupID, Tasks: tasks}, nil
}

type ABVariantStats struct {
	Variant   string           `json:"variant"`
	TaskID    int64            `json:"task_id,string"`
	Status    string           `json:"status"`
	Sent      int64            `json:"sent"`
	Opened    int64            `json:"opened"`
	Clicked   int64            `json:"clicked"`
	OpenRate  float64          `json:"open_rate"`
	ClickRate float64          `json:"click_rate"`
	Channels  map[string]int64 `json:"channel_breakdown"`
}

type ABTestStats struct {
	ABGroupID string           `json:"ab_group_id"`
	Variants  []ABVariantStats `json:"variants"`
}

func GetABTestStats(groupID string) (*ABTestStats, error) {
	var tasks []model.ReachTask
	if err := store.DB.Where("ab_group_id = ?", groupID).Find(&tasks).Error; err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks found for A/B group %s", groupID)
	}

	result := &ABTestStats{ABGroupID: groupID}
	for _, task := range tasks {
		var sent, opened, clicked int64
		store.DB.Model(&model.ReachSendLog{}).
			Where("task_id = ? AND status = ?", task.ID, model.ReachSendStatusSent).
			Count(&sent)
		store.DB.Model(&model.ReachSendLog{}).
			Where("task_id = ? AND opened_at IS NOT NULL", task.ID).
			Count(&opened)
		store.DB.Model(&model.ReachSendLog{}).
			Where("task_id = ? AND clicked_at IS NOT NULL", task.ID).
			Count(&clicked)

		type channelCount struct {
			Channel string
			Count   int64
		}
		var counts []channelCount
		store.DB.Model(&model.ReachSendLog{}).
			Select("channel, count(*) as count").
			Where("task_id = ? AND status = ?", task.ID, model.ReachSendStatusSent).
			Group("channel").
			Scan(&counts)
		breakdown := make(map[string]int64)
		for _, c := range counts {
			breakdown[c.Channel] = c.Count
		}

		var openRate, clickRate float64
		if sent > 0 {
			openRate = float64(opened) / float64(sent)
			clickRate = float64(clicked) / float64(sent)
		}

		result.Variants = append(result.Variants, ABVariantStats{
			Variant:   task.ABVariant,
			TaskID:    task.ID,
			Status:    task.Status,
			Sent:      sent,
			Opened:    opened,
			Clicked:   clicked,
			OpenRate:  openRate,
			ClickRate: clickRate,
			Channels:  breakdown,
		})
	}
	return result, nil
}

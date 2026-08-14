package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

func StartReachScheduler(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fireDueReachTasks(ctx)
			}
		}
	}()
	logger.L.Info("reach scheduler started (30s interval)")
}

func fireDueReachTasks(ctx context.Context) {
	var tasks []model.ReachTask
	now := time.Now().UTC()
	store.DB.
		Where("status = ? AND scheduled_at <= ?", model.ReachStatusScheduled, now).
		Limit(10).
		Find(&tasks)

	for _, task := range tasks {
		res := store.DB.Model(&model.ReachTask{}).
			Where("id = ? AND status = ?", task.ID, model.ReachStatusScheduled).
			Updates(map[string]any{"status": model.ReachStatusSending, "updated_at": now})
		if res.RowsAffected == 0 {
			continue
		}

		var tpl model.ReachTemplate
		if err := store.DB.Where("id = ?", task.TemplateID).First(&tpl).Error; err != nil {
			logger.L.Warnf("reach scheduler: template %d not found for task %d, marking failed", task.TemplateID, task.ID)
			store.DB.Model(&model.ReachTask{}).Where("id = ?", task.ID).
				Updates(map[string]any{"status": model.ReachStatusFailed, "updated_at": now})
			continue
		}

		var channels []string
		json.Unmarshal(task.Channels, &channels)

		var audience *ReachAudience
		if len(task.Audience) > 0 {
			var a ReachAudience
			if json.Unmarshal(task.Audience, &a) == nil {
				audience = &a
			}
		}

		logger.L.Infof("reach scheduler: firing task=%d template=%d", task.ID, task.TemplateID)
		go executeMarketingTask(ctx, task.ID, tpl, channels, audience)
	}
}

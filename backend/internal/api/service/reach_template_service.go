package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

type CreateReachTemplateReq struct {
	Name      string `json:"name"`
	Title     string `json:"title"`
	InAppBody string `json:"in_app_body"`
	PushBody  string `json:"push_body"`
	EmailHTML string `json:"email_html"`
	SmsBody   string `json:"sms_body"`
}

func CreateReachTemplate(req CreateReachTemplateReq) (*model.ReachTemplate, error) {
	if req.Name == "" || req.Title == "" {
		return nil, errors.New("name and title required")
	}
	tpl := model.ReachTemplate{
		ID:        snowflake.GenID(),
		Name:      req.Name,
		Title:     req.Title,
		InAppBody: req.InAppBody,
		PushBody:  req.PushBody,
		EmailHTML: req.EmailHTML,
		SmsBody:   req.SmsBody,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.DB.Create(&tpl).Error; err != nil {
		return nil, err
	}
	return &tpl, nil
}

type UpdateReachTemplateReq struct {
	Name      *string `json:"name"`
	Title     *string `json:"title"`
	InAppBody *string `json:"in_app_body"`
	PushBody  *string `json:"push_body"`
	EmailHTML *string `json:"email_html"`
	SmsBody   *string `json:"sms_body"`
}

func UpdateReachTemplate(id int64, req UpdateReachTemplateReq) (*model.ReachTemplate, error) {
	updates := map[string]any{"updated_at": time.Now().UTC()}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.InAppBody != nil {
		updates["in_app_body"] = *req.InAppBody
	}
	if req.PushBody != nil {
		updates["push_body"] = *req.PushBody
	}
	if req.EmailHTML != nil {
		updates["email_html"] = *req.EmailHTML
	}
	if req.SmsBody != nil {
		updates["sms_body"] = *req.SmsBody
	}
	res := store.DB.Model(&model.ReachTemplate{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var tpl model.ReachTemplate
	store.DB.Where("id = ?", id).First(&tpl)
	return &tpl, nil
}

func GetReachTemplate(id int64) (*model.ReachTemplate, error) {
	var tpl model.ReachTemplate
	if err := store.DB.Where("id = ?", id).First(&tpl).Error; err != nil {
		return nil, err
	}
	return &tpl, nil
}

func DeleteReachTemplate(id int64) error {
	var inUse int64
	store.DB.Model(&model.ReachTask{}).
		Where("template_id = ? AND status IN ?", id, []string{
			model.ReachStatusDraft, model.ReachStatusScheduled,
			model.ReachStatusSending, model.ReachStatusPaused,
		}).
		Count(&inUse)
	if inUse > 0 {
		return fmt.Errorf("template is referenced by %d active task(s), cancel them first", inUse)
	}
	res := store.DB.Where("id = ?", id).Delete(&model.ReachTemplate{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

type ListReachTemplatesReq struct {
	Page     int
	PageSize int
}

type ListReachTemplatesResult struct {
	Total     int64                 `json:"total"`
	Templates []model.ReachTemplate `json:"templates"`
}

func ListReachTemplates(req ListReachTemplatesReq) (*ListReachTemplatesResult, error) {
	db := store.DB.Model(&model.ReachTemplate{})
	var total int64
	db.Count(&total)

	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}
	var tpls []model.ReachTemplate
	db.Order("created_at DESC").
		Offset((req.Page - 1) * req.PageSize).
		Limit(req.PageSize).
		Find(&tpls)
	return &ListReachTemplatesResult{Total: total, Templates: tpls}, nil
}

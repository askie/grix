package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
)

type WriteAuditLogReq struct {
	EventType string
	UserID    *int64
	SessionID *string
	MsgID     *int64
	Detail    map[string]any
	ClientIP  string
	UserAgent string
}

// WriteAuditLog writes audit log in best-effort mode.
// It must not break user-facing API flow.
func WriteAuditLog(req WriteAuditLogReq) {
	eventType := strings.TrimSpace(req.EventType)
	if eventType == "" || store.DB == nil {
		return
	}

	detailJSON := datatypes.JSON([]byte("{}"))
	if len(req.Detail) > 0 {
		raw, err := json.Marshal(req.Detail)
		if err != nil {
			if logger.L != nil {
				logger.L.Warnf("marshal audit detail failed event=%s err=%v", eventType, err)
			}
		} else {
			detailJSON = datatypes.JSON(raw)
		}
	}

	clientIP := strings.TrimSpace(req.ClientIP)
	if clientIP == "" {
		clientIP = "0.0.0.0"
	}

	entry := model.AuditLog{
		EventType: eventType,
		UserID:    req.UserID,
		SessionID: req.SessionID,
		MsgID:     req.MsgID,
		Detail:    detailJSON,
		ClientIP:  clientIP,
		UserAgent: trimAuditUserAgent(req.UserAgent),
		CreatedAt: time.Now(),
	}
	if err := store.DB.Create(&entry).Error; err != nil {
		if logger.L != nil {
			logger.L.Warnf("write audit log failed event=%s err=%v", eventType, err)
		}
	}
}

func trimAuditUserAgent(raw string) string {
	v := strings.TrimSpace(raw)
	if len(v) <= 255 {
		return v
	}
	return v[:255]
}

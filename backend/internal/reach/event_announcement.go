package reach

import (
	"encoding/json"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

type AnnouncementEvent struct {
	EventKey string `json:"event_key"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Region   string `json:"region"`
}

func PublishAnnouncementEvent(title, body, region string) {
	if store.JS == nil {
		return
	}
	evt := AnnouncementEvent{
		EventKey: EventAnnouncement,
		Title:    title,
		Body:     body,
		Region:   region,
	}
	data, err := json.Marshal(evt)
	if err != nil {
		logger.L.Warnf("reach: marshal announcement event err=%v", err)
		return
	}
	if _, err := store.JS.Publish(NATSSubjectReachEvent, data); err != nil {
		logger.L.Warnf("reach: publish announcement event err=%v", err)
		return
	}
	logger.L.Infof("reach: published announcement event title=%q region=%s", title, region)
}

type AccountSecurityEvent struct {
	EventKey   string `json:"event_key"`
	UserID     int64  `json:"user_id"`
	ActionType string `json:"action_type"`
	Detail     string `json:"detail"`
	IP         string `json:"ip"`
}

func PublishAccountSecurityEvent(userID int64, actionType, detail, ip string) {
	if store.JS == nil {
		return
	}
	evt := AccountSecurityEvent{
		EventKey:   EventAccountSecurity,
		UserID:     userID,
		ActionType: actionType,
		Detail:     detail,
		IP:         ip,
	}
	data, err := json.Marshal(evt)
	if err != nil {
		logger.L.Warnf("reach: marshal account security event err=%v", err)
		return
	}
	if _, err := store.JS.Publish(NATSSubjectReachEvent, data); err != nil {
		logger.L.Warnf("reach: publish account security event user=%d err=%v", userID, err)
		return
	}
	logger.L.Infof("reach: published account security event user=%d action=%s", userID, actionType)
}

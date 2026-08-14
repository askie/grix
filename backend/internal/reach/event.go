// Package reach implements the unified reach hub: it fans platform events
// (currently app releases) out to users through their preferred channels. P1
// only covers the in-app (IM system message) channel for a fully-public app
// release; offline users still receive a push via the normal message-delivery
// fallback.
package reach

import (
	"encoding/json"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

const NATSSubjectReachEvent = "im.reach.event"

const (
	EventAppReleasePublished = "app_release_published"
	EventAnnouncement        = "announcement"
	EventAccountSecurity     = "account_security"
)

// AppReleaseEvent is the payload published to NATSSubjectReachEvent when an app
// release goes fully public.
type AppReleaseEvent struct {
	EventKey    string `json:"event_key"`
	ReleaseID   int64  `json:"release_id"`
	Version     string `json:"version"`
	Channel     string `json:"channel"`
	Platform    string `json:"platform"`
	Changelog   string `json:"changelog"`
	PublishedAt int64  `json:"published_at"` // unix millis
}

// PublishAppReleaseEvent best-effort emits a reach event for a freshly published
// app release. It only fires for a full (全量) release — see isFullRelease — and
// never blocks or fails the publish hot path: errors are logged and swallowed,
// matching the offline-push publisher's fire-and-forget style.
func PublishAppReleaseEvent(release *model.AppRelease) {
	if release == nil || store.JS == nil {
		return
	}
	if !isFullRelease(release.ID) {
		logger.L.Infof("reach: skip non-full app release id=%d version=%s", release.ID, release.Version)
		return
	}

	publishedAt := int64(0)
	if release.PublishedAt != nil {
		publishedAt = release.PublishedAt.UnixMilli()
	}
	evt := AppReleaseEvent{
		EventKey:    EventAppReleasePublished,
		ReleaseID:   release.ID,
		Version:     strings.TrimSpace(release.Version),
		Channel:     strings.TrimSpace(release.Channel),
		Platform:    release.Platform,
		Changelog:   release.Changelog,
		PublishedAt: publishedAt,
	}
	data, err := json.Marshal(evt)
	if err != nil {
		logger.L.Warnf("reach: marshal app release event id=%d err=%v", release.ID, err)
		return
	}
	if _, err := store.JS.Publish(NATSSubjectReachEvent, data); err != nil {
		logger.L.Warnf("reach: publish app release event id=%d err=%v", release.ID, err)
		return
	}
	logger.L.Infof("reach: published app release event id=%d version=%s platform=%s", release.ID, release.Version, release.Platform)
}

// isFullRelease reports whether the release is a full (全量) rollout: it has no
// active rollout rule, or an active percentage rule that covers 100%. Any other
// active rule (user_list, percentage<100) is a gray rollout and must not trigger
// the platform-wide reach.
func isFullRelease(releaseID int64) bool {
	var rules []model.AppRolloutRule
	if err := store.DB.
		Where("release_id = ? AND status = ?", releaseID, model.RolloutRuleActive).
		Find(&rules).Error; err != nil {
		logger.L.Warnf("reach: load rollout rules release=%d err=%v", releaseID, err)
		return false
	}
	if len(rules) == 0 {
		return true
	}
	for _, rule := range rules {
		if rule.RuleType != "percentage" {
			continue
		}
		var data struct {
			Percent int `json:"percent"`
		}
		if err := json.Unmarshal(rule.RuleValue, &data); err != nil {
			continue
		}
		if data.Percent >= 100 {
			return true
		}
	}
	return false
}

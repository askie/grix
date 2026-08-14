package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

func TrackReachOpen(logID int64) error {
	now := time.Now().UTC()
	return store.DB.Model(&model.ReachSendLog{}).
		Where("id = ? AND opened_at IS NULL", logID).
		Update("opened_at", now).Error
}

func TrackReachClick(logID int64) error {
	now := time.Now().UTC()
	return store.DB.Model(&model.ReachSendLog{}).
		Where("id = ? AND clicked_at IS NULL", logID).
		Update("clicked_at", now).Error
}

func reachTrackingBaseURL() string {
	origins := config.C.Server.AllowedWebOrigins
	if origins != "" {
		parts := strings.Split(origins, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(p, "https://") {
				return strings.TrimRight(p, "/")
			}
		}
		if p := strings.TrimSpace(parts[0]); p != "" {
			return strings.TrimRight(p, "/")
		}
	}
	return fmt.Sprintf("http://127.0.0.1:%d", config.C.Server.APIPort)
}

func ReachOpenTrackingURL(logID int64) string {
	return fmt.Sprintf("%s/v1/reach/t/o/%d", reachTrackingBaseURL(), logID)
}

func ReachClickTrackingURL(logID int64, targetURL string) string {
	return fmt.Sprintf("%s/v1/reach/t/c/%d?url=%s", reachTrackingBaseURL(), logID, targetURL)
}

func InjectEmailTracking(html string, logID int64) string {
	pixelTag := fmt.Sprintf(`<img src="%s" width="1" height="1" style="display:none" alt="">`, ReachOpenTrackingURL(logID))
	if idx := strings.LastIndex(html, "</body>"); idx >= 0 {
		return html[:idx] + pixelTag + html[idx:]
	}
	return html + pixelTag
}

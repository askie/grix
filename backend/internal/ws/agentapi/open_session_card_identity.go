package agentapi

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

func buildOpenSessionCardInstanceID(agentID int64, sessionID, source string, msgID int64) string {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if agentID <= 0 || normalizedSessionID == "" {
		return ""
	}
	normalizedSource := strings.TrimSpace(source)
	if normalizedSource == "" && msgID > 0 {
		normalizedSource = fmt.Sprintf("%d", msgID)
	}
	if normalizedSource == "" {
		return ""
	}
	return fmt.Sprintf("open_session:%d:%s:%s", agentID, normalizedSessionID, normalizedSource)
}

func loadOpenSessionCardInstanceID(ctx context.Context, sessionID string, msgID int64) string {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" || msgID <= 0 {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var msg model.Message
	if err := store.DB.
		Select("content").
		Where("session_id = ? AND msg_id = ?", normalizedSessionID, msgID).
		Where("is_deleted = ? AND is_revoked = ?", false, false).
		First(&msg).Error; err != nil {
		return ""
	}
	return extractOpenSessionCardInstanceID(msg.Content)
}

func extractOpenSessionCardInstanceID(content string) string {
	normalized := strings.TrimSpace(content)
	idx := strings.Index(normalized, "grix://card/agent_open_session")
	if idx < 0 {
		return ""
	}
	href := normalized[idx:]
	if end := strings.IndexByte(href, ')'); end >= 0 {
		href = href[:end]
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get("card_instance_id"))
}

func ensureOpenSessionCardInstanceID(content, cardInstanceID string) (string, string) {
	normalizedCardInstanceID := strings.TrimSpace(cardInstanceID)
	if normalizedCardInstanceID == "" {
		return content, ""
	}
	if existing := extractOpenSessionCardInstanceID(content); existing != "" {
		return content, existing
	}

	normalized := strings.TrimSpace(content)
	idx := strings.Index(normalized, "grix://card/agent_open_session")
	if idx < 0 {
		return content, ""
	}
	href := normalized[idx:]
	end := strings.IndexByte(href, ')')
	if end < 0 {
		return content, ""
	}
	href = href[:end]
	parsed, err := url.Parse(href)
	if err != nil {
		return content, ""
	}
	query := parsed.Query()
	query.Set("card_instance_id", normalizedCardInstanceID)
	parsed.RawQuery = query.Encode()
	updatedHref := parsed.String()
	return strings.Replace(content, href, updatedHref, 1), normalizedCardInstanceID
}

package reach

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnnouncementEvent_Fields(t *testing.T) {
	evt := AnnouncementEvent{
		EventKey: EventAnnouncement,
		Title:    "Maintenance",
		Body:     "System will be down",
		Region:   "cn",
	}
	assert.Equal(t, "announcement", evt.EventKey)
	assert.Equal(t, "Maintenance", evt.Title)
	assert.Equal(t, "cn", evt.Region)
}

func TestAccountSecurityEvent_Fields(t *testing.T) {
	evt := AccountSecurityEvent{
		EventKey:   EventAccountSecurity,
		UserID:     12345,
		ActionType: "login_new_device",
		Detail:     "New login from Chrome on macOS",
		IP:         "1.2.3.4",
	}
	assert.Equal(t, "account_security", evt.EventKey)
	assert.Equal(t, int64(12345), evt.UserID)
	assert.Equal(t, "login_new_device", evt.ActionType)
}

func TestPublishAnnouncementEvent_NoJetStream(t *testing.T) {
	PublishAnnouncementEvent("Test", "Body", "cn")
}

func TestPublishAccountSecurityEvent_NoJetStream(t *testing.T) {
	PublishAccountSecurityEvent(1, "login", "detail", "127.0.0.1")
}

package notification

import (
	"encoding/json"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
)

// defaultPref returns the seeded default for an event key per design §2.1.
// approval_requested is force-enabled.
func defaultPref(userID int64, eventKey string) model.NotificationPref {
	return model.NotificationPref{
		UserID:   userID,
		EventKey: eventKey,
		Enabled:  true,
		Channels: datatypes.JSON([]byte(`["push"]`)),
	}
}

// PrefView is the API/dispatcher-facing shape of a single preference.
type PrefView struct {
	EventKey string   `json:"event_key"`
	Enabled  bool     `json:"enabled"`
	Channels []string `json:"channels"`
}

// GetPrefs returns the user's preferences for all known event keys, seeding any
// missing rows with defaults on first read.
func GetPrefs(userID int64) ([]PrefView, error) {
	var rows []model.NotificationPref
	if err := store.DB.Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return nil, err
	}
	existing := make(map[string]model.NotificationPref, len(rows))
	for _, r := range rows {
		existing[r.EventKey] = r
	}

	var toCreate []model.NotificationPref
	out := make([]PrefView, 0, len(AllEventKeys))
	for _, key := range AllEventKeys {
		row, ok := existing[key]
		if !ok {
			row = defaultPref(userID, key)
			toCreate = append(toCreate, row)
		}
		out = append(out, toView(row))
	}
	if len(toCreate) > 0 {
		// Best-effort seed; ignore conflicts from a concurrent first read.
		_ = store.DB.Create(&toCreate).Error
	}
	return out, nil
}

// UpdatePrefs upserts the given preferences for the user. approval_requested is
// force-enabled regardless of the incoming value (design §3.1).
func UpdatePrefs(userID int64, prefs []PrefView) error {
	for _, p := range prefs {
		if !isKnownEventKey(p.EventKey) {
			continue
		}
		enabled := p.Enabled
		if p.EventKey == EventApprovalRequested {
			enabled = true
		}
		channels := normalizeChannels(p.Channels)
		raw, err := json.Marshal(channels)
		if err != nil {
			return err
		}
		row := model.NotificationPref{
			UserID:   userID,
			EventKey: p.EventKey,
			Enabled:  enabled,
			Channels: datatypes.JSON(raw),
		}
		if err := store.DB.Save(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

// ResolvePref returns the effective preference for a single event key, seeding
// the default if absent. Used by the dispatcher.
func ResolvePref(userID int64, eventKey string) (PrefView, error) {
	var row model.NotificationPref
	err := store.DB.Where("user_id = ? AND event_key = ?", userID, eventKey).First(&row).Error
	if err != nil {
		// Not found (or error) → fall back to default without persisting; the
		// dispatcher must not block on a write.
		return toView(defaultPref(userID, eventKey)), nil
	}
	return toView(row), nil
}

func toView(row model.NotificationPref) PrefView {
	var channels []string
	if len(row.Channels) > 0 {
		_ = json.Unmarshal(row.Channels, &channels)
	}
	if len(channels) == 0 {
		channels = []string{ChannelPush}
	}
	return PrefView{EventKey: row.EventKey, Enabled: row.Enabled, Channels: channels}
}

func (v PrefView) HasChannel(ch string) bool {
	for _, c := range v.Channels {
		if c == ch {
			return true
		}
	}
	return false
}

func normalizeChannels(in []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(in))
	for _, c := range in {
		switch c {
		case ChannelPush, ChannelTTS, ChannelVoiceCall:
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	if len(out) == 0 {
		out = []string{ChannelPush}
	}
	return out
}

func isKnownEventKey(key string) bool {
	for _, k := range AllEventKeys {
		if k == key {
			return true
		}
	}
	return false
}

package agentapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStoreQueueSnapshotMirrorDetailedItems(t *testing.T) {
	setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-detailed-items"
	)
	longPreview := strings.Repeat("界", 201)
	raw, err := json.Marshal(map[string]any{
		"session_id": session,
		"running":    []string{"evt-run"},
		"running_items": []map[string]any{
			{"event_id": "evt-run", "title": "running title"},
		},
		"queued": []map[string]any{
			{
				"event_id":        "evt-q",
				"position":        7,
				"content_preview": longPreview,
				"held":            true,
				"held_reason":     "manual",
			},
		},
	})
	require.NoError(t, err)
	storeQueueSnapshotMirror(context.Background(), owner, agent, raw)

	mirror := loadQueueSnapshotMirror(context.Background(), owner, session, agent)
	require.NotNil(t, mirror)
	require.Equal(t, []string{"evt-run"}, mirror.Running)
	require.Equal(t, []string{"evt-q"}, mirror.Queued)
	require.Len(t, mirror.Items, 2)
	require.Equal(t, queueSnapshotMirrorItem{
		EventID:        "evt-run",
		State:          "running",
		ContentPreview: "running title",
	}, mirror.Items[0])
	require.Equal(t, "evt-q", mirror.Items[1].EventID)
	require.Equal(t, "queued", mirror.Items[1].State)
	require.Equal(t, 7, mirror.Items[1].Position)
	require.Equal(t, strings.Repeat("界", 200), mirror.Items[1].ContentPreview)
	require.True(t, mirror.Items[1].Held)
	require.Equal(t, "manual", mirror.Items[1].HeldReason)
}

func TestStoreQueueSnapshotMirrorGenericItemDetails(t *testing.T) {
	setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-generic-item-details"
	)
	raw, err := json.Marshal(map[string]any{
		"session_id": session,
		"items": []map[string]any{
			{
				"event_id":        "evt-generic",
				"state":           "queued",
				"queue_position":  3,
				"content_preview": "generic preview",
				"held":            true,
				"held_reason":     "editing",
			},
		},
	})
	require.NoError(t, err)
	storeQueueSnapshotMirror(context.Background(), owner, agent, raw)

	mirror := loadQueueSnapshotMirror(context.Background(), owner, session, agent)
	require.NotNil(t, mirror)
	require.Equal(t, []string{"evt-generic"}, mirror.Queued)
	require.Equal(t, []queueSnapshotMirrorItem{
		{
			EventID:        "evt-generic",
			State:          "queued",
			Position:       3,
			ContentPreview: "generic preview",
			Held:           true,
			HeldReason:     "editing",
		},
	}, mirror.Items)
}

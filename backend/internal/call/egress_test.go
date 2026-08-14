package call

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSummaryExtra(t *testing.T) {
	extra := BuildSummaryExtra(123, 65, "https://oss.example.com/calls/123/mixed.ogg")

	require.NotNil(t, extra)
	assert.JSONEq(t, `{
		"kind": "call_summary",
		"call_id": "123",
		"duration_seconds": 65,
		"recording_url": "https://oss.example.com/calls/123/mixed.ogg"
	}`, string(extra))
}

func TestBuildSummaryExtra_NoRecording(t *testing.T) {
	extra := BuildSummaryExtra(456, 0, "")
	require.NotNil(t, extra)
	assert.JSONEq(t, `{
		"kind": "call_summary",
		"call_id": "456",
		"duration_seconds": 0
	}`, string(extra))
}

func TestEgressRecorder_Disabled(t *testing.T) {
	r := NewEgressRecorder(nil)
	assert.False(t, r.Enabled())

	// Should be no-ops
	ctx := t.Context()
	egressID, err := r.StartRecording(ctx, 123)
	assert.NoError(t, err)
	assert.Empty(t, egressID)

	url, err := r.StopRecording(ctx, "some-id")
	assert.NoError(t, err)
	assert.Empty(t, url)
}

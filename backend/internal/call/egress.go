package call

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

// EgressRecorder manages LiveKit Egress recording for calls.
type EgressRecorder struct {
	egressClient *lksdk.EgressClient
}

// NewEgressRecorder creates a recorder. egressClient can be nil (recording disabled).
func NewEgressRecorder(egressClient *lksdk.EgressClient) *EgressRecorder {
	return &EgressRecorder{egressClient: egressClient}
}

// Enabled returns true if egress client is configured.
func (r *EgressRecorder) Enabled() bool { return r.egressClient != nil }

// StartRecording starts a mixed audio recording for the room.
// Uses a 3-second timeout to avoid blocking call setup when Egress is unavailable.
func (r *EgressRecorder) StartRecording(ctx context.Context, callID int64) (egressID string, err error) {
	if r.egressClient == nil {
		return "", nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	roomName := fmt.Sprintf("call-%d", callID)
	resp, err := r.egressClient.StartRoomCompositeEgress(timeoutCtx, &livekit.RoomCompositeEgressRequest{
		RoomName:  roomName,
		AudioOnly: true,
		FileOutputs: []*livekit.EncodedFileOutput{
			{
				FileType: livekit.EncodedFileType_OGG,
				Filepath: fmt.Sprintf("calls/%d/mixed.ogg", callID),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("start egress: %w", err)
	}
	return resp.EgressId, nil
}

// StopRecording stops the recording and returns the recording URL.
func (r *EgressRecorder) StopRecording(ctx context.Context, egressID string) (recordingURL string, err error) {
	if r.egressClient == nil || egressID == "" {
		return "", nil
	}
	resp, err := r.egressClient.StopEgress(ctx, &livekit.StopEgressRequest{EgressId: egressID})
	if err != nil {
		return "", fmt.Errorf("stop egress: %w", err)
	}
	for _, fi := range resp.GetFileResults() {
		if fi.GetLocation() != "" {
			return fi.GetLocation(), nil
		}
	}
	return "", nil
}

// BuildSummaryExtra constructs the extra JSON for a call_summary system message.
func BuildSummaryExtra(callID int64, durationSec int, recordingURL string) json.RawMessage {
	extra := map[string]any{
		"kind":             "call_summary",
		"call_id":          fmt.Sprintf("%d", callID),
		"duration_seconds": durationSec,
	}
	if recordingURL != "" {
		extra["recording_url"] = recordingURL
	}
	b, _ := json.Marshal(extra)
	return b
}

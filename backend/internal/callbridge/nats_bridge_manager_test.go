package callbridge_test

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/callbridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNATSBridgeManager_NilConn_NoError(t *testing.T) {
	// nc=nil 时所有操作应静默成功（voicebridge 未部署时不影响主流程）
	m := callbridge.NewNATSBridgeManager(nil)
	ctx := context.Background()
	require.NoError(t, m.StartBridge(ctx, 1, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"}))
	require.NoError(t, m.StopBridge(ctx, 1))
	require.NoError(t, m.InterruptBridge(ctx, 1))
}

func TestNATSBridgeManager_ImplementsInterface(t *testing.T) {
	m := callbridge.NewNATSBridgeManager(nil)
	assert.NotNil(t, m)
}

package voicebridge_test

import (
	"testing"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/voicebridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	logger.Init()
}

func TestWorker_StartWithoutNATS_NoError(t *testing.T) {
	w := voicebridge.NewWorker(voicebridge.WorkerConfig{})
	err := w.Start()
	require.NoError(t, err)
	w.Stop()
}

func TestWorker_StopIdempotent(t *testing.T) {
	w := voicebridge.NewWorker(voicebridge.WorkerConfig{})
	_ = w.Start()
	w.Stop()
	w.Stop()
}

func TestWorker_SubjectConstants(t *testing.T) {
	assert.Equal(t, "voicebridge.control.start", voicebridge.SubjectStartBridge)
	assert.Equal(t, "voicebridge.control.stop", voicebridge.SubjectStopBridge)
	assert.Equal(t, "voicebridge.control.interrupt", voicebridge.SubjectInterruptBridge)
}

package call_test

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/call"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoopRoomManager 验证 NoopRoomManager 不 panic，返回空 token。
func TestNoopRoomManager(t *testing.T) {
	noop := &call.NoopRoomManager{}
	tokenCaller, tokenCallee, roomURL, err := noop.CreateRoom(context.Background(), 12345, 1001, 1002)
	require.NoError(t, err)
	assert.Empty(t, tokenCaller)
	assert.Empty(t, tokenCallee)
	assert.Empty(t, roomURL)

	err = noop.CloseRoom(context.Background(), 12345)
	require.NoError(t, err)
}

// TestControllerWithNoopRoom 验证 Controller 在 NoopRoomManager 下正常工作。
func TestControllerWithNoopRoom(t *testing.T) {
	noop := &call.NoopRoomManager{}
	persist := &mockPersist{}
	ctrl := call.New(noop, persist, func(_ int64, _ string, _ any) {})

	callID, tokenCaller, roomURL, err := ctrl.Invite(context.Background(), 10, 20, "sess-noop")
	require.NoError(t, err)
	assert.NotZero(t, callID)
	assert.Empty(t, tokenCaller) // noop 返回空 token
	assert.Empty(t, roomURL)
}

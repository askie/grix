package call

import "context"

// NoopRoomManager 空实现，用于 LiveKit 未配置时的占位。
type NoopRoomManager struct{}

func (n *NoopRoomManager) CreateRoom(_ context.Context, callID int64, _, _ int64) (string, string, string, error) {
	return "", "", "", nil
}

func (n *NoopRoomManager) CloseRoom(_ context.Context, _ int64) error { return nil }

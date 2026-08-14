package agentapi

import (
	"context"
	"errors"
)

func (m *Manager) SendMessage(ctx context.Context, req SendMessageReq) (*SendMessageResult, error) {
	if m == nil || m.sendFn == nil {
		return nil, errors.New("agent api send message unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return m.sendFn(ctx, req)
}

func SendMessage(ctx context.Context, req SendMessageReq) (*SendMessageResult, error) {
	globalMu.RLock()
	manager := globalManager
	globalMu.RUnlock()
	if manager == nil {
		return nil, errors.New("agent api manager unavailable")
	}
	return manager.SendMessage(ctx, req)
}

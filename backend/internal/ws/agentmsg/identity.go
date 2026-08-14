package agentmsg

import (
	"context"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// SenderIdentity holds the resolved sender information for an agent message.
type SenderIdentity struct {
	SenderID    int64
	SenderType  int16 // 1=human, 2=agent
	IsDelegated bool
	ExtraFields map[string]any // pre-built extra fields
}

const (
	ModeAgentAPI = "agent_api"
	ModeAIDirect = "ai_direct"
	ModeDelegate = "delegate"
	// ModeCaller 用于语音通话中访客(caller)说话的场景，
	// sender_id 直接使用 CallerID，不走 delegate 代理。
	ModeCaller = "caller"
)

// IdentityParams specifies the parameters for resolving a sender identity.
type IdentityParams struct {
	SessionID string
	OwnerID   int64
	AgentID   int64
	CallerID  int64  // 仅 ModeCaller 使用
	Mode      string // ModeAgentAPI | ModeAIDirect | ModeDelegate | ModeCaller
}

// ResolveIdentity determines the effective sender identity for an agent message.
func ResolveIdentity(ctx context.Context, p IdentityParams) (*SenderIdentity, error) {
	switch p.Mode {
	case ModeAIDirect:
		return &SenderIdentity{
			SenderID:    p.AgentID,
			SenderType:  2,
			ExtraFields: map[string]any{},
		}, nil

	case ModeDelegate:
		return &SenderIdentity{
			SenderID:    p.OwnerID,
			SenderType:  1,
			IsDelegated: true,
			ExtraFields: map[string]any{"delegate_origin": true},
		}, nil

	case ModeCaller:
		return &SenderIdentity{
			SenderID:    p.CallerID,
			SenderType:  1,
			ExtraFields: map[string]any{},
		}, nil

	case ModeAgentAPI:
		return resolveAgentAPIIdentity(ctx, p)

	default:
		return nil, fmt.Errorf("unknown identity mode: %s", p.Mode)
	}
}

func resolveAgentAPIIdentity(ctx context.Context, p IdentityParams) (*SenderIdentity, error) {
	if p.SessionID == "" || p.OwnerID <= 0 || p.AgentID <= 0 {
		return nil, ErrPermissionDenied
	}

	if IsDelegateActiveForAgent(ctx, p.SessionID, p.OwnerID, p.AgentID) {
		return &SenderIdentity{
			SenderID:    p.OwnerID,
			SenderType:  1,
			IsDelegated: true,
			ExtraFields: map[string]any{
				"delegate_origin":  true,
				"agent_api_origin": true,
				"agent_id":         fmt.Sprintf("%d", p.AgentID),
			},
		}, nil
	}

	var memberCount int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = ?", p.SessionID, p.AgentID, 2).
		Count(&memberCount).Error; err != nil {
		return nil, err
	}
	if memberCount == 0 {
		return nil, ErrPermissionDenied
	}

	return &SenderIdentity{
		SenderID:   p.AgentID,
		SenderType: 2,
		ExtraFields: map[string]any{
			"agent_api_origin": true,
			"agent_id":         fmt.Sprintf("%d", p.AgentID),
		},
	}, nil
}

// IsDelegateActiveForAgent checks whether the given agent currently holds a delegation
// for the specified owner in the session.
func IsDelegateActiveForAgent(ctx context.Context, sessionID string, ownerID, agentID int64) bool {
	if sessionID == "" || ownerID <= 0 || agentID <= 0 {
		return false
	}

	delegateKey := fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID)
	rawAgentID, err := store.RDB.HGet(ctx, delegateKey, "agent_id").Result()
	if err != nil {
		return false
	}

	rawAgentID = strings.TrimSpace(rawAgentID)
	if rawAgentID == "" {
		return false
	}
	return rawAgentID == fmt.Sprintf("%d", agentID)
}

var ErrPermissionDenied = fmt.Errorf("agent api send permission denied")

package agentmsg

import (
	"context"
	"fmt"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

func TestResolveIdentityModes(t *testing.T) {
	cleanup := setupAgentMsgTest(t)
	defer cleanup()

	t.Run("ai direct", func(t *testing.T) {
		identity, err := ResolveIdentity(context.Background(), IdentityParams{
			Mode:      ModeAIDirect,
			AgentID:   8011,
			OwnerID:   0,
			SessionID: "",
		})
		if err != nil {
			t.Fatalf("resolve ai direct error: %v", err)
		}
		if identity.SenderID != 8011 || identity.SenderType != 2 {
			t.Fatalf("identity=%#v", identity)
		}
		if identity.IsDelegated {
			t.Fatal("ai direct should not be delegated")
		}
		if identity.ExtraFields == nil {
			t.Fatal("ai direct should initialize extra fields")
		}
	})

	t.Run("delegate", func(t *testing.T) {
		identity, err := ResolveIdentity(context.Background(), IdentityParams{
			Mode:      ModeDelegate,
			SessionID: "sess-identity",
			OwnerID:   8012,
			AgentID:   8013,
		})
		if err != nil {
			t.Fatalf("resolve delegate error: %v", err)
		}
		if identity.SenderID != 8012 || identity.SenderType != 1 {
			t.Fatalf("identity=%#v", identity)
		}
		if !identity.IsDelegated {
			t.Fatal("delegate identity should be marked delegated")
		}
		if got := identity.ExtraFields["delegate_origin"]; got != true {
			t.Fatalf("delegate_origin=%v want=true", got)
		}
	})

	t.Run("unknown mode", func(t *testing.T) {
		_, err := ResolveIdentity(context.Background(), IdentityParams{Mode: "unknown"})
		if err == nil {
			t.Fatal("expected error for unknown mode")
		}
	})
}

func TestResolveAgentAPIIdentityUsesDelegateOrMembership(t *testing.T) {
	cleanup := setupAgentMsgTest(t)
	defer cleanup()

	ctx := context.Background()
	const (
		sessionID = "sess-agentapi-identity"
		ownerID   = int64(8111)
		agentID   = int64(8112)
	)

	if err := store.RDB.HSet(ctx, fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID), "agent_id", fmt.Sprintf("%d", agentID)).Err(); err != nil {
		t.Fatalf("seed delegate error: %v", err)
	}

	identity, err := ResolveIdentity(ctx, IdentityParams{
		Mode:      ModeAgentAPI,
		SessionID: sessionID,
		OwnerID:   ownerID,
		AgentID:   agentID,
	})
	if err != nil {
		t.Fatalf("resolve delegated agent api identity error: %v", err)
	}
	if identity.SenderID != ownerID || identity.SenderType != 1 || !identity.IsDelegated {
		t.Fatalf("identity=%#v", identity)
	}
	if got := identity.ExtraFields["agent_api_origin"]; got != true {
		t.Fatalf("agent_api_origin=%v want=true", got)
	}
	if got := identity.ExtraFields["agent_id"]; got != fmt.Sprintf("%d", agentID) {
		t.Fatalf("agent_id=%v want=%d", got, agentID)
	}

	if err := store.RDB.Del(ctx, fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID)).Err(); err != nil {
		t.Fatalf("clear delegate error: %v", err)
	}
	createSessionMember(t, model.SessionMember{
		SessionID:  sessionID,
		MemberID:   agentID,
		MemberType: 2,
	})

	identity, err = ResolveIdentity(ctx, IdentityParams{
		Mode:      ModeAgentAPI,
		SessionID: sessionID,
		OwnerID:   ownerID,
		AgentID:   agentID,
	})
	if err != nil {
		t.Fatalf("resolve agent membership identity error: %v", err)
	}
	if identity.SenderID != agentID || identity.SenderType != 2 || identity.IsDelegated {
		t.Fatalf("identity=%#v", identity)
	}
	if got := identity.ExtraFields["agent_api_origin"]; got != true {
		t.Fatalf("agent_api_origin=%v want=true", got)
	}
}

func TestResolveAgentAPIIdentityDeniedWithoutMembership(t *testing.T) {
	cleanup := setupAgentMsgTest(t)
	defer cleanup()

	_, err := ResolveIdentity(context.Background(), IdentityParams{
		Mode:      ModeAgentAPI,
		SessionID: "sess-agentapi-denied",
		OwnerID:   8211,
		AgentID:   8212,
	})
	if err != ErrPermissionDenied {
		t.Fatalf("err=%v want=%v", err, ErrPermissionDenied)
	}
}

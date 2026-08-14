package notification

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setTestSecret(t *testing.T) {
	t.Helper()
	config.C.Server.NotificationHmacSecret = "test-test-test-test-test-test-test-test"
}

func sampleClaims() ActionTokenClaims {
	return ActionTokenClaims{
		UserID:           2030840865701756928,
		EventKey:         EventApprovalRequested,
		AvailableActions: []string{ActionApprove, ActionDeny, ActionStop},
		Target: ActionTarget{
			ApprovalCommandID: "exec_ctx_123",
			SessionID:         "sess-1",
			RunEventID:        "evt-1",
			RunID:             "run-1",
			AgentID:           12345,
		},
		Nonce: "abc123def456",
	}
}

func TestGenerateAndParseRoundtrip(t *testing.T) {
	setTestSecret(t)
	tok, err := GenerateToken(sampleClaims())
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	claims, err := ParseToken(tok)
	require.NoError(t, err)
	assert.Equal(t, int64(2030840865701756928), claims.UserID)
	assert.Equal(t, EventApprovalRequested, claims.EventKey)
	assert.Equal(t, "exec_ctx_123", claims.Target.ApprovalCommandID)
	assert.Equal(t, "run-1", claims.Target.RunID)
	assert.True(t, claims.Allows(ActionApprove))
	assert.True(t, claims.Allows(ActionStop))
	assert.False(t, claims.Allows(ActionReply))
	assert.Greater(t, claims.Exp, time.Now().Unix())
}

func TestParseRejectsTamperedBody(t *testing.T) {
	setTestSecret(t)
	tok, err := GenerateToken(sampleClaims())
	require.NoError(t, err)

	// Flip a character in the body segment.
	b := []byte(tok)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	_, err = ParseToken(string(b))
	assert.ErrorIs(t, err, ErrTokenSignature)
}

func TestParseRejectsWrongSecret(t *testing.T) {
	setTestSecret(t)
	tok, err := GenerateToken(sampleClaims())
	require.NoError(t, err)

	config.C.Server.NotificationHmacSecret = "a-completely-different-secret-value"
	_, err = ParseToken(tok)
	assert.ErrorIs(t, err, ErrTokenSignature)
}

func TestParseRejectsExpired(t *testing.T) {
	setTestSecret(t)
	claims := sampleClaims()
	claims.Exp = time.Now().Add(-time.Minute).Unix()
	tok, err := GenerateToken(claims)
	require.NoError(t, err)

	_, err = ParseToken(tok)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestParseRejectsMalformed(t *testing.T) {
	setTestSecret(t)
	for _, bad := range []string{"", "nodot", ".", "a.", ".b", "a.b.c.d"} {
		_, err := ParseToken(bad)
		assert.Error(t, err, "input %q should fail", bad)
	}
}

func TestFallsBackToJWTSecret(t *testing.T) {
	config.C.Server.NotificationHmacSecret = ""
	config.C.JWT.Secret = "jwt-fallback-secret-value-here-ok"
	tok, err := GenerateToken(sampleClaims())
	require.NoError(t, err)
	_, err = ParseToken(tok)
	assert.NoError(t, err)
}

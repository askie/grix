package notification

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
)

// TokenTTL is how long an action token stays valid. It also drives the APNs
// apns-expiration header so the OS drops stale notifications whose buttons would
// no longer work.
const TokenTTL = 10 * time.Minute

// ActionTarget identifies the concrete operation a token authorizes.
type ActionTarget struct {
	ApprovalCommandID string `json:"approval_command_id,omitempty"`
	QuestionID        string `json:"question_id,omitempty"`
	QuestionMessageID int64  `json:"question_message_id,omitempty"`
	SessionID         string `json:"session_id"`
	RunEventID        string `json:"event_id,omitempty"`
	RunID             string `json:"run_id,omitempty"`
	AgentID           int64  `json:"agent_id"`
}

// ActionTokenClaims is the signed body of an action token.
type ActionTokenClaims struct {
	UserID           int64        `json:"user_id"`
	EventKey         string       `json:"event_key"`
	AvailableActions []string     `json:"available_actions"`
	Target           ActionTarget `json:"target"`
	Nonce            string       `json:"nonce"`
	Exp              int64        `json:"exp"`
}

// Allows reports whether the token authorizes the given action.
func (c *ActionTokenClaims) Allows(action string) bool {
	for _, a := range c.AvailableActions {
		if a == action {
			return true
		}
	}
	return false
}

var (
	// ErrTokenMalformed means the token could not be decoded/parsed.
	ErrTokenMalformed = errors.New("notification: malformed action token")
	// ErrTokenSignature means the HMAC did not verify.
	ErrTokenSignature = errors.New("notification: bad action token signature")
	// ErrTokenExpired means exp is in the past.
	ErrTokenExpired = errors.New("notification: action token expired")
)

// signingKey derives the HMAC key, mirroring the webhook-token precedent:
// dedicated secret if set, else fall back to the JWT secret. Domain-separated
// so the same JWT secret can't be cross-used.
func signingKey() []byte {
	secret := strings.TrimSpace(config.C.Server.NotificationHmacSecret)
	if secret == "" {
		secret = strings.TrimSpace(config.C.JWT.Secret)
	}
	sum := sha256.Sum256([]byte("notification-action-token:" + secret))
	return sum[:]
}

// GenerateToken signs the claims and returns a compact "<body>.<sig>" string,
// both segments base64url (no padding). exp is set from now if unset.
func GenerateToken(claims ActionTokenClaims) (string, error) {
	if claims.Exp == 0 {
		claims.Exp = time.Now().Add(TokenTTL).Unix()
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("notification: marshal token: %w", err)
	}
	bodySeg := base64.RawURLEncoding.EncodeToString(body)
	sig := sign(bodySeg)
	return bodySeg + "." + sig, nil
}

// ParseToken verifies signature and expiry, returning the claims. It does NOT
// check nonce replay — that is the caller's job (Redis) so the token can be
// validated in any service while replay state lives where the action executes.
func ParseToken(token string) (*ActionTokenClaims, error) {
	parts := strings.SplitN(strings.TrimSpace(token), ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrTokenMalformed
	}
	expected := sign(parts[0])
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return nil, ErrTokenSignature
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrTokenMalformed
	}
	var claims ActionTokenClaims
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, ErrTokenMalformed
	}
	if time.Now().Unix() > claims.Exp {
		return nil, ErrTokenExpired
	}
	return &claims, nil
}

func sign(bodySeg string) string {
	mac := hmac.New(sha256.New, signingKey())
	mac.Write([]byte(bodySeg))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

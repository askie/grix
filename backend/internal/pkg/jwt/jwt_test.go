package jwt

import (
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Initialize JWT with test secret
	Init("test-secret-key-for-testing", 3600, 86400)
	m.Run()
}

// TestGenerateAndValidateAccessToken tests access token generation and validation
func TestGenerateAndValidateAccessToken(t *testing.T) {
	tests := []struct {
		name    string
		userID  int64
		wantErr bool
	}{
		{"valid user", 12345, false},
		{"zero user id", 0, false}, // technically valid, but might want to reject
		{"negative user id", -1, false},
		{"large user id", 9999999999999, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, expiresIn, err := GenerateAccessToken(tt.userID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateAccessToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if token == "" {
					t.Error("token is empty")
				}
				if expiresIn <= 0 {
					t.Error("expiresIn should be positive")
				}

				// Validate the token
				claims, err := ValidateAccessToken(token)
				if err != nil {
					t.Errorf("ValidateAccessToken() error = %v", err)
					return
				}
				if claims.UserID != tt.userID {
					t.Errorf("claims.UserID = %v, want %v", claims.UserID, tt.userID)
				}
				if claims.Type != "access" {
					t.Errorf("claims.Type = %v, want access", claims.Type)
				}
			}
		})
	}
}

// TestGenerateAndValidateRefreshToken tests refresh token generation and validation
func TestGenerateAndValidateRefreshToken(t *testing.T) {
	userID := int64(12345)

	token, err := GenerateRefreshToken(userID)
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	claims, err := ValidateRefreshToken(token)
	if err != nil {
		t.Fatalf("ValidateRefreshToken() error = %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("claims.UserID = %v, want %v", claims.UserID, userID)
	}
	if claims.Type != "refresh" {
		t.Errorf("claims.Type = %v, want refresh", claims.Type)
	}
}

// TestValidateAccessTokenWithRefreshToken tests that refresh token is rejected for access
func TestValidateAccessTokenWithRefreshToken(t *testing.T) {
	userID := int64(12345)

	refreshToken, _ := GenerateRefreshToken(userID)

	_, err := ValidateAccessToken(refreshToken)
	if err == nil {
		t.Error("ValidateAccessToken should fail with refresh token")
	}
	if err.Error() != "not an access token" {
		t.Errorf("error = %v, want 'not an access token'", err)
	}
}

// TestValidateRefreshTokenWithAccessToken tests that access token is rejected for refresh
func TestValidateRefreshTokenWithAccessToken(t *testing.T) {
	userID := int64(12345)

	accessToken, _, _ := GenerateAccessToken(userID)

	_, err := ValidateRefreshToken(accessToken)
	if err == nil {
		t.Error("ValidateRefreshToken should fail with access token")
	}
	if err.Error() != "not a refresh token" {
		t.Errorf("error = %v, want 'not a refresh token'", err)
	}
}

func TestGenerateAndValidateWidgetAccessToken(t *testing.T) {
	token, expiresIn, err := GenerateWidgetAccessToken(
		11,
		"w_session_001",
		22,
		33,
		[]string{"chat:sync", "chat:send", "chat:send"},
	)
	if err != nil {
		t.Fatalf("GenerateWidgetAccessToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("widget token is empty")
	}
	if expiresIn <= 0 {
		t.Fatalf("expiresIn should be positive, got %d", expiresIn)
	}

	claims, err := ValidateWidgetAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateWidgetAccessToken() error = %v", err)
	}
	if claims.Type != TokenTypeWidgetAccess {
		t.Fatalf("claims.Type = %q, want %q", claims.Type, TokenTypeWidgetAccess)
	}
	if claims.WidgetSiteID != 11 || claims.WidgetVisitorID != 22 || claims.WidgetOwnerUserID != 33 {
		t.Fatalf("unexpected widget claims: site=%d visitor=%d owner=%d",
			claims.WidgetSiteID, claims.WidgetVisitorID, claims.WidgetOwnerUserID)
	}
	if claims.SessionID != "w_session_001" {
		t.Fatalf("claims.SessionID = %q, want %q", claims.SessionID, "w_session_001")
	}
	if !WidgetScopeAllowed(claims, "chat:send") || !WidgetScopeAllowed(claims, "chat:sync") {
		t.Fatalf("expected widget scopes to contain send+sync, got %v", claims.WidgetScopes)
	}
	if WidgetScopeAllowed(claims, "delegate:start") {
		t.Fatalf("unexpected allowed scope in %v", claims.WidgetScopes)
	}
}

func TestGenerateWidgetAccessTokenRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name      string
		siteID    int64
		sessionID string
		visitorID int64
		ownerUser int64
	}{
		{"invalid site", 0, "s1", 1, 1},
		{"empty session", 1, " ", 1, 1},
		{"invalid visitor", 1, "s1", 0, 1},
		{"invalid owner", 1, "s1", 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := GenerateWidgetAccessToken(tc.siteID, tc.sessionID, tc.visitorID, tc.ownerUser, nil)
			if err == nil {
				t.Fatalf("expected error for case=%s", tc.name)
			}
		})
	}
}

func TestValidateWidgetAccessTokenRejectsWrongType(t *testing.T) {
	accessToken, _, err := GenerateAccessToken(123)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	if _, err := ValidateWidgetAccessToken(accessToken); err == nil {
		t.Fatal("expected widget validator to reject access token")
	}
}

func TestWidgetAccessTokenExpiration(t *testing.T) {
	originalTTL := widgetAccessTTL
	widgetAccessTTL = 1 * time.Second
	defer func() { widgetAccessTTL = originalTTL }()

	token, _, err := GenerateWidgetAccessToken(1, "session-exp", 2, 3, nil)
	if err != nil {
		t.Fatalf("GenerateWidgetAccessToken() error = %v", err)
	}
	if _, err := ValidateWidgetAccessToken(token); err != nil {
		t.Fatalf("token should be valid immediately, got err=%v", err)
	}

	time.Sleep(1100 * time.Millisecond)
	if _, err := ValidateWidgetAccessToken(token); err == nil {
		t.Fatal("expected expired widget token to fail validation")
	}
}

func TestValidateWidgetSessionBinding(t *testing.T) {
	token, _, err := GenerateWidgetAccessToken(100, "session-100", 200, 300, []string{"chat:ack"})
	if err != nil {
		t.Fatalf("GenerateWidgetAccessToken() error = %v", err)
	}
	claims, err := ValidateWidgetAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateWidgetAccessToken() error = %v", err)
	}
	if err := ValidateWidgetSessionBinding(claims, 100, "session-100", 200); err != nil {
		t.Fatalf("binding should match, got err=%v", err)
	}
	if err := ValidateWidgetSessionBinding(claims, 101, "session-100", 200); err == nil {
		t.Fatal("expected site mismatch error")
	}
	if err := ValidateWidgetSessionBinding(claims, 100, "session-101", 200); err == nil {
		t.Fatal("expected session mismatch error")
	}
	if err := ValidateWidgetSessionBinding(claims, 100, "session-100", 201); err == nil {
		t.Fatal("expected visitor mismatch error")
	}
}

// TestParseTokenInvalid tests parsing invalid tokens
func TestParseTokenInvalid(t *testing.T) {
	tests := []struct {
		name      string
		token     string
		wantError bool
	}{
		{"empty token", "", true},
		{"invalid format", "not.a.valid.token", true},
		{"wrong signature", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoxMjMsInR5cGUiOiJhY2Nlc3MifQ.wrong", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseToken(tt.token)
			if (err != nil) != tt.wantError {
				t.Errorf("ParseToken() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// TestTokenExpiration tests that expired tokens are rejected
func TestTokenExpiration(t *testing.T) {
	// Create a token that expires in 1 second
	originalTTL := accessTTL
	accessTTL = 1 * time.Second
	defer func() { accessTTL = originalTTL }()

	token, _, err := GenerateAccessToken(12345)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Should be valid immediately
	_, err = ValidateAccessToken(token)
	if err != nil {
		t.Errorf("Token should be valid immediately, error = %v", err)
	}

	// Wait for expiration
	time.Sleep(1100 * time.Millisecond)

	// Should be expired now
	_, err = ValidateAccessToken(token)
	if err == nil {
		t.Error("Token should be expired")
	}
}

// TestDifferentSecrets tests that tokens signed with different secrets are rejected
func TestDifferentSecrets(t *testing.T) {
	// Generate token with current secret
	token, _, _ := GenerateAccessToken(12345)

	// Change secret
	originalSecret := secret
	secret = []byte("different-secret")
	defer func() { secret = originalSecret }()

	// Token should fail validation
	_, err := ValidateAccessToken(token)
	if err == nil {
		t.Error("Token signed with different secret should be rejected")
	}
}

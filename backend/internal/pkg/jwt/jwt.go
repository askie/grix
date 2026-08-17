package jwt

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	secret          []byte
	accessTTL       time.Duration
	refreshTTL      time.Duration
	widgetAccessTTL = 30 * time.Minute
)

const (
	TokenTypeAccess       = "access"
	TokenTypeRefresh      = "refresh"
	TokenTypeWidgetAccess = "widget_access"
)

var DefaultWidgetScopes = []string{
	"chat:send",
	"chat:sync",
	"chat:ack",
}

type Claims struct {
	UserID int64  `json:"user_id"`
	Type   string `json:"type"` // "access", "refresh", or "widget_access"
	// SessionID binds access/refresh tokens to one logical login session.
	// For widget_access, it stores the target chat session_id.
	SessionID string `json:"sid,omitempty"`
	// FamilyID is used by refresh token rotation and replay detection.
	FamilyID string `json:"family_id,omitempty"`
	// Widget claims (only set for widget_access tokens).
	WidgetSiteID      int64    `json:"wid,omitempty"`
	WidgetVisitorID   int64    `json:"vid,omitempty"`
	WidgetOwnerUserID int64    `json:"owner_uid,omitempty"`
	WidgetScopes      []string `json:"scope,omitempty"`
	jwt.RegisteredClaims
}

func Init(secretStr string, accessSec, refreshSec int) {
	secret = []byte(secretStr)
	accessTTL = time.Duration(accessSec) * time.Second
	refreshTTL = time.Duration(refreshSec) * time.Second
}

func GenerateAccessToken(userID int64) (string, int64, error) {
	return GenerateAccessTokenWithSession(userID, "")
}

func GenerateAccessTokenWithSession(userID int64, sessionID string) (string, int64, error) {
	exp := time.Now().Add(accessTTL)
	claims := Claims{
		UserID:    userID,
		Type:      TokenTypeAccess,
		SessionID: strings.TrimSpace(sessionID),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        uuid.NewString(),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	return token, int64(accessTTL.Seconds()), err
}

func GenerateRefreshToken(userID int64) (string, error) {
	token, _, _, err := GenerateRefreshTokenWithFamily(userID, "", "")
	return token, err
}

func GenerateRefreshTokenWithFamily(userID int64, familyID, jti string) (string, string, time.Time, error) {
	fid := strings.TrimSpace(familyID)
	if fid == "" {
		fid = uuid.NewString()
	}
	id := strings.TrimSpace(jti)
	if id == "" {
		id = uuid.NewString()
	}

	expAt := time.Now().Add(refreshTTL)
	claims := Claims{
		UserID:    userID,
		Type:      TokenTypeRefresh,
		SessionID: fid,
		FamilyID:  fid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        id,
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	return token, id, expAt, err
}

func GenerateWidgetAccessToken(
	siteID int64,
	sessionID string,
	visitorID int64,
	ownerUserID int64,
	scopes []string,
) (string, int64, error) {
	sid := strings.TrimSpace(sessionID)
	if siteID <= 0 || sid == "" || visitorID <= 0 || ownerUserID <= 0 {
		return "", 0, errors.New("invalid widget token input")
	}
	normalizedScopes := normalizeWidgetScopes(scopes)
	if len(normalizedScopes) == 0 {
		normalizedScopes = append([]string(nil), DefaultWidgetScopes...)
	}
	expiresAt := time.Now().Add(widgetAccessTTL)
	claims := Claims{
		Type:              TokenTypeWidgetAccess,
		SessionID:         sid,
		WidgetSiteID:      siteID,
		WidgetVisitorID:   visitorID,
		WidgetOwnerUserID: ownerUserID,
		WidgetScopes:      normalizedScopes,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        uuid.NewString(),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		return "", 0, err
	}
	return token, int64(widgetAccessTTL.Seconds()), nil
}

func ParseToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		// 精确限定 HS256，而非整个 HMAC 家族，防止算法混淆/降级。
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func ValidateAccessToken(tokenStr string) (*Claims, error) {
	claims, err := ParseToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.Type != "access" {
		return nil, errors.New("not an access token")
	}
	return claims, nil
}

func ValidateRefreshToken(tokenStr string) (*Claims, error) {
	claims, err := ParseToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.Type != "refresh" {
		return nil, errors.New("not a refresh token")
	}
	return claims, nil
}

func ValidateWidgetAccessToken(tokenStr string) (*Claims, error) {
	claims, err := ParseToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.Type != TokenTypeWidgetAccess {
		return nil, errors.New("not a widget access token")
	}
	if claims.WidgetSiteID <= 0 || claims.WidgetVisitorID <= 0 || claims.WidgetOwnerUserID <= 0 {
		return nil, errors.New("invalid widget claims")
	}
	if strings.TrimSpace(claims.SessionID) == "" {
		return nil, errors.New("invalid widget claims")
	}
	if len(normalizeWidgetScopes(claims.WidgetScopes)) == 0 {
		return nil, errors.New("invalid widget claims")
	}
	return claims, nil
}

func WidgetScopeAllowed(claims *Claims, scope string) bool {
	if claims == nil || claims.Type != TokenTypeWidgetAccess {
		return false
	}
	want := strings.TrimSpace(scope)
	if want == "" {
		return false
	}
	normalized := normalizeWidgetScopes(claims.WidgetScopes)
	for _, item := range normalized {
		if item == want {
			return true
		}
	}
	return false
}

func ValidateWidgetSessionBinding(claims *Claims, siteID int64, sessionID string, visitorID int64) error {
	if claims == nil {
		return errors.New("nil widget claims")
	}
	if claims.Type != TokenTypeWidgetAccess {
		return errors.New("not a widget access token")
	}
	if claims.WidgetSiteID != siteID {
		return fmt.Errorf("widget site mismatch")
	}
	if claims.WidgetVisitorID != visitorID {
		return fmt.Errorf("widget visitor mismatch")
	}
	if strings.TrimSpace(claims.SessionID) != strings.TrimSpace(sessionID) {
		return fmt.Errorf("widget session mismatch")
	}
	return nil
}

func normalizeWidgetScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		item := strings.TrimSpace(scope)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	slices.Sort(normalized)
	return normalized
}

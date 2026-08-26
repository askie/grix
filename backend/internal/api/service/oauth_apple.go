package service

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type appleTokenProfile struct {
	Email   string
	Subject string
}

var (
	errAppleLoginNotConfigured = errors.New("Apple 登录服务暂未配置")
	errAppleLoginVerifyFailed  = errors.New("Apple 登录验证失败")
	errAppleEmailUnavailable   = errors.New("无法从 Apple 获取邮箱")
)

var (
	appleJWKSCache struct {
		mu        sync.RWMutex
		keys      map[string]crypto.PublicKey
		expiresAt time.Time
		loaded    bool
	}
	appleJWKSNow = time.Now
)

const (
	appleJWKSEndpoint = "https://appleid.apple.com/auth/keys"
	appleIssuer       = "https://appleid.apple.com"
	appleJWKSCacheTTL = 24 * time.Hour
)

var appleJWKSClient = &http.Client{Timeout: 10 * time.Second}

var validateAppleIDToken = defaultValidateAppleIDToken

func LoginWithApple(idToken, deviceID, platform, language string) (*LoginResp, error) {
	appleEnabled, err := featuregate.IsPublicFeatureEnabled("auth_apple_login")
	if err != nil {
		return nil, err
	}
	if !appleEnabled {
		return nil, errors.New("系统已关闭 Apple 登录")
	}

	profile, err := validateAppleIDToken(idToken)
	if err != nil {
		return nil, err
	}

	email := profile.Email
	subject := profile.Subject

	var oauthAccount model.OAuthAccount
	err = store.DB.Where("provider = ? AND provider_uid = ?", "apple", subject).First(&oauthAccount).Error
	if err == nil {
		return loginWithOAuthAccount(&oauthAccount, deviceID, platform, language)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("Apple 登录服务暂时不可用，请稍后再试")
	}

	// Apple account not bound yet — check if email exists
	if email == "" {
		return nil, errAppleEmailUnavailable
	}

	var user model.User
	// 按邮箱认领已有账号时忽略大小写：用户在别处绑的邮箱可能是 Foo@x.com，
	// 精确匹配会漏掉它，进而给同一个人再建一个账号。
	err = store.DB.Where("LOWER(email) = ?", strings.ToLower(email)).First(&user).Error
	if err == nil {
		if err := security.EnsureUserActive(user.ID); err != nil {
			if errors.Is(err, security.ErrUserDisabled) {
				return nil, err
			}
			if errors.Is(err, security.ErrUserNotFound) {
				return nil, errors.New("对应用户不存在")
			}
			return nil, ErrAuthServiceUnavailable
		}
		newAccount := model.OAuthAccount{
			ID:          snowflake.GenID(),
			UserID:      user.ID,
			Provider:    "apple",
			ProviderUID: subject,
		}
		if err := store.DB.Create(&newAccount).Error; err != nil {
			if isDuplicatedOAuthProviderUIDErr(err) {
				return loginWithAppleProviderUID(subject, deviceID, platform, language)
			}
			return nil, errors.New("绑定 Apple 账号失败")
		}
		return doIssueToken(user, deviceID, platform, language)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("Apple 登录服务暂时不可用，请稍后再试")
	}

	// Email not found — auto register
	registerEnabled, err := featuregate.IsPublicFeatureEnabled("auth_register")
	if err != nil {
		return nil, err
	}
	if !registerEnabled {
		return nil, errors.New("系统已关闭注册")
	}
	settings, err := systemsetting.GetAuthSettings()
	if err != nil {
		return nil, err
	}

	idStr := strconv.FormatInt(snowflake.GenID(), 10)
	importName := fmt.Sprintf("u_%s", idStr[:8])
	user = model.User{
		ID:           snowflake.GenID(),
		Email:        email,
		Username:     importName,
		PasswordHash: "",
		AuthProvider: "apple",
		Nickname:     email,
		Status:       model.UserStatusActive,
	}

	newAccount := model.OAuthAccount{
		ID:          snowflake.GenID(),
		UserID:      user.ID,
		Provider:    "apple",
		ProviderUID: subject,
	}

	var resp *LoginResp
	var staleDevices []model.Device
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if err := createDefaultUserSettingsTx(tx, user.ID, language); err != nil {
			return err
		}
		if err := tx.Create(&newAccount).Error; err != nil {
			return err
		}
		if err := addConfiguredCustomerFriendTx(tx, user.ID, settings.AutoAddCustomerUserID); err != nil {
			return err
		}

		issued, stale, err := issueTokenWithDB(tx, user, deviceID, platform)
		if err != nil {
			return err
		}
		resp = issued
		staleDevices = stale
		return nil
	}); err != nil {
		if isDuplicatedOAuthProviderUIDErr(err) {
			return loginWithAppleProviderUID(subject, deviceID, platform, language)
		}
		return nil, normalizeAppleAutoRegisterErr(err)
	}

	cleanStaleDeviceCacheAfterCommit(user.ID, deviceID, staleDevices)
	notifyConfiguredCustomerFriendAdded(user.ID, settings.AutoAddCustomerUserID)
	scheduleRegisterWelcomeCompensation(user.ID, settings.AutoAddCustomerUserID)
	return resp, nil
}

func loginWithAppleProviderUID(providerUID, deviceID, platform, language string) (*LoginResp, error) {
	var account model.OAuthAccount
	if err := store.DB.Where("provider = ? AND provider_uid = ?", "apple", providerUID).First(&account).Error; err != nil {
		return nil, errors.New("绑定 Apple 账号失败")
	}
	return loginWithOAuthAccount(&account, deviceID, platform, language)
}

func normalizeAppleAutoRegisterErr(err error) error {
	if err == nil {
		return nil
	}
	if isDuplicatedEmailErr(err) {
		return errors.New("该邮箱已被注册")
	}
	if errors.Is(err, ErrConfiguredCustomerInvalid) ||
		errors.Is(err, ErrConfiguredCustomerNotFound) ||
		errors.Is(err, ErrConfiguredCustomerDisabled) {
		return err
	}
	return errors.New("Apple 登录服务暂时不可用，请稍后再试")
}

func defaultValidateAppleIDToken(idToken string) (appleTokenProfile, error) {
	if strings.TrimSpace(idToken) == "" {
		return appleTokenProfile{}, errAppleLoginVerifyFailed
	}

	allowedClientIDs := configuredAppleBundleIDs()
	if len(allowedClientIDs) == 0 {
		return appleTokenProfile{}, errAppleLoginNotConfigured
	}

	// Parse unverified to get the key ID header
	unverifiedToken, _, err := jwt.NewParser().ParseUnverified(idToken, &jwt.MapClaims{})
	if err != nil {
		return appleTokenProfile{}, errAppleLoginVerifyFailed
	}
	kid, ok := unverifiedToken.Header["kid"].(string)
	if !ok || kid == "" {
		return appleTokenProfile{}, errAppleLoginVerifyFailed
	}

	pubKey, err := getApplePublicKey(kid)
	if err != nil {
		return appleTokenProfile{}, errAppleLoginVerifyFailed
	}

	token, err := jwt.Parse(idToken, func(token *jwt.Token) (interface{}, error) {
		switch token.Method.(type) {
		case *jwt.SigningMethodRSA:
		case *jwt.SigningMethodECDSA:
		default:
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return pubKey, nil
	})
	if err != nil || !token.Valid {
		return appleTokenProfile{}, errAppleLoginVerifyFailed
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return appleTokenProfile{}, errAppleLoginVerifyFailed
	}

	// Verify issuer
	iss, _ := claims["iss"].(string)
	if iss != appleIssuer {
		return appleTokenProfile{}, errAppleLoginVerifyFailed
	}

	// Verify audience matches one of our configured bundle IDs
	aud, _ := claims["aud"].(string)
	if !containsString(allowedClientIDs, aud) {
		return appleTokenProfile{}, errAppleLoginVerifyFailed
	}

	subject, _ := claims["sub"].(string)
	if subject == "" {
		return appleTokenProfile{}, errAppleLoginVerifyFailed
	}

	email, _ := claims["email"].(string)
	emailVerified, _ := claims["email_verified"].(bool)
	// Apple 也会在 token 中携带 email_verified，与 Google 保持一致要求邮箱已验证。
	if email != "" && !emailVerified {
		return appleTokenProfile{}, errAppleLoginVerifyFailed
	}

	return appleTokenProfile{
		Email:   email,
		Subject: subject,
	}, nil
}

func configuredAppleBundleIDs() []string {
	raw := strings.TrimSpace(config.C.OAuth.AppleBundleIDs)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized := strings.TrimSpace(part)
		if normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

func getApplePublicKey(kid string) (crypto.PublicKey, error) {
	appleJWKSCache.mu.RLock()
	if appleJWKSCache.loaded && appleJWKSNow().Before(appleJWKSCache.expiresAt) {
		if key, ok := appleJWKSCache.keys[kid]; ok {
			appleJWKSCache.mu.RUnlock()
			return key, nil
		}
	}
	appleJWKSCache.mu.RUnlock()

	return refreshAppleJWKS(kid)
}

func refreshAppleJWKS(targetKid string) (crypto.PublicKey, error) {
	appleJWKSCache.mu.Lock()
	defer appleJWKSCache.mu.Unlock()

	// Double-check after acquiring write lock
	if appleJWKSCache.loaded && appleJWKSNow().Before(appleJWKSCache.expiresAt) {
		if key, ok := appleJWKSCache.keys[targetKid]; ok {
			return key, nil
		}
	}

	keys, err := fetchAppleJWKS()
	if err != nil {
		return nil, err
	}

	appleJWKSCache.keys = keys
	appleJWKSCache.expiresAt = appleJWKSNow().Add(appleJWKSCacheTTL)
	appleJWKSCache.loaded = true

	if key, ok := keys[targetKid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("apple JWK kid %s not found", targetKid)
}

func fetchAppleJWKS() (map[string]crypto.PublicKey, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, appleJWKSEndpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := appleJWKSClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apple JWK endpoint returned %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kty string   `json:"kty"`
			Kid string   `json:"kid"`
			N   string   `json:"n"`
			E   string   `json:"e"`
			X5c []string `json:"x5c"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, err
	}

	result := make(map[string]crypto.PublicKey, len(jwks.Keys))
	for _, key := range jwks.Keys {
		if key.Kid == "" {
			continue
		}
		if key.Kty == "RSA" && key.N != "" && key.E != "" {
			pubKey, err := parseRSAJWK(key.N, key.E)
			if err != nil {
				continue
			}
			result[key.Kid] = pubKey
		} else if len(key.X5c) > 0 && key.X5c[0] != "" {
			pubKey, err := parseAppleX5cCert(key.X5c[0])
			if err != nil {
				continue
			}
			result[key.Kid] = pubKey
		}
	}
	return result, nil
}

func parseRSAJWK(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}
	e := new(big.Int).SetBytes(eBytes)
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(e.Int64()),
	}, nil
}

func parseAppleX5cCert(x5c string) (*ecdsa.PublicKey, error) {
	certDER, err := base64.StdEncoding.DecodeString(x5c)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}
	pubKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("unexpected public key type")
	}
	return pubKey, nil
}

func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}

package provider

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/golang-jwt/jwt/v5"
)

const (
	apnsProductionURL = "https://api.push.apple.com"
	apnsSandboxURL    = "https://api.sandbox.push.apple.com"
	apnsTokenTTL      = 50 * time.Minute
)

type APNsProvider struct {
	KeyPath      string
	KeyID        string
	TeamID       string
	Topic        string
	IsProduction bool

	client   *http.Client
	baseURL  string
	nowFunc  func() time.Time
	tokenMu  sync.Mutex
	token    string
	tokenExp time.Time
	key      *ecdsa.PrivateKey
}

func NewAPNs(keyPath, keyID, teamID, topic string, isProd bool) *APNsProvider {
	baseURL := apnsSandboxURL
	if isProd {
		baseURL = apnsProductionURL
	}

	return &APNsProvider{
		KeyPath:      keyPath,
		KeyID:        keyID,
		TeamID:       teamID,
		Topic:        topic,
		IsProduction: isProd,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		baseURL: baseURL,
		nowFunc: time.Now,
	}
}

func (p *APNsProvider) Name() string { return "apns" }

func (p *APNsProvider) Send(ctx context.Context, deviceToken string, payload *PushPayload) (*PushResult, error) {
	if strings.TrimSpace(deviceToken) == "" {
		return nil, fmt.Errorf("apns device token is empty")
	}
	if payload == nil {
		return nil, fmt.Errorf("apns payload is nil")
	}

	bearer, err := p.authorizationToken()
	if err != nil {
		return nil, err
	}

	var body []byte
	if payload.BadgeOnly {
		badgeOnly := map[string]any{
			"aps": map[string]any{
				"content-available": 1,
				"badge":             payload.Badge,
			},
			"session_id": payload.SessionID,
		}
		if payload.RecipientID > 0 {
			badgeOnly["recipient_id"] = strconv.FormatInt(payload.RecipientID, 10)
		}
		body, err = json.Marshal(badgeOnly)
	} else {
		custom := map[string]any{
			"session_id": payload.SessionID,
		}
		if payload.RecipientID > 0 {
			custom["recipient_id"] = strconv.FormatInt(payload.RecipientID, 10)
		}
		if payload.SenderAvatarURL != "" {
			custom["sender_avatar_url"] = payload.SenderAvatarURL
		}
		if payload.SenderInitial != "" {
			custom["sender_initial"] = payload.SenderInitial
		}
		aps := map[string]any{
			"alert": map[string]any{
				"title": payload.Title,
				"body":  payload.Body,
			},
			"sound":           "default",
			"badge":           payload.Badge,
			"mutable-content": 1,
		}
		if payload.TimeSensitive {
			aps["interruption-level"] = "time-sensitive"
		}
		if payload.Category != "" {
			aps["category"] = payload.Category
		}
		if payload.ImageURL != "" {
			custom["image_url"] = payload.ImageURL
		}
		if payload.EventKey != "" {
			custom["event_key"] = payload.EventKey
		}
		if payload.ActionToken != "" {
			custom["action_token"] = payload.ActionToken
		}
		if len(payload.AvailableActions) > 0 {
			custom["available_actions"] = payload.AvailableActions
		}
		msg := map[string]any{"aps": aps}
		for k, v := range custom {
			msg[k] = v
		}
		body, err = json.Marshal(msg)
	}
	if err != nil {
		return nil, fmt.Errorf("marshal apns payload: %w", err)
	}

	endpoint := strings.TrimRight(p.baseURL, "/") + "/3/device/" + url.PathEscape(deviceToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create apns request: %w", err)
	}
	req.Header.Set("authorization", "bearer "+bearer)
	req.Header.Set("apns-topic", p.Topic)
	if payload.BadgeOnly {
		req.Header.Set("apns-push-type", "background")
	} else {
		req.Header.Set("apns-push-type", "alert")
	}
	if !payload.BadgeOnly {
		// 分级投递：审批 / 语音拨号 / 来电等高优先级信号立即投递（apns-priority=10）；
		// 普通消息降级投递（5），由系统按节能策略择机送达，减少打扰。
		if payload.HighPriority || payload.ForcePush || payload.TimeSensitive || payload.Category != "" {
			req.Header.Set("apns-priority", "10")
		} else {
			req.Header.Set("apns-priority", "5")
		}
	}
	// apns-expiration: drop the push once the action token would be expired, so
	// stale approval/question buttons never surface.
	if payload.Expiration > 0 {
		req.Header.Set("apns-expiration", strconv.FormatInt(payload.Expiration, 10))
	}
	req.Header.Set("content-type", "application/json")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("send apns request: %w", err)
	}
	defer resp.Body.Close()

	var result PushResult
	result.StatusCode = resp.StatusCode
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300

	if !result.Success {
		var apnsErr struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&apnsErr); err == nil {
			result.Reason = apnsErr.Reason
		}
	}

	logger.L.Infof("APNs push to %s status=%d reason=%s", textutil.TruncateRunes(deviceToken, 8), result.StatusCode, result.Reason)
	return &result, nil
}

func (p *APNsProvider) IsTokenInvalid(result *PushResult) bool {
	return result != nil && (result.StatusCode == http.StatusGone || result.Reason == "Unregistered")
}

// SendVoIP 发送 PushKit VoIP 推送（iOS 来电触达）。
// topic 使用 <bundle-id>.voip，apns-push-type 为 voip，优先级 10。
func (p *APNsProvider) SendVoIP(ctx context.Context, deviceToken string, payload map[string]any) (*PushResult, error) {
	if strings.TrimSpace(deviceToken) == "" {
		return nil, fmt.Errorf("apns voip device token is empty")
	}

	bearer, err := p.authorizationToken()
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal voip payload: %w", err)
	}

	voipTopic := p.Topic + ".voip"
	endpoint := strings.TrimRight(p.baseURL, "/") + "/3/device/" + url.PathEscape(deviceToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create voip apns request: %w", err)
	}
	req.Header.Set("authorization", "bearer "+bearer)
	req.Header.Set("apns-topic", voipTopic)
	req.Header.Set("apns-push-type", "voip")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("content-type", "application/json")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("send voip apns request: %w", err)
	}
	defer resp.Body.Close()

	result := &PushResult{
		StatusCode: resp.StatusCode,
		Success:    resp.StatusCode >= 200 && resp.StatusCode < 300,
	}
	if !result.Success {
		var apnsErr struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&apnsErr); err == nil {
			result.Reason = apnsErr.Reason
		}
	}
	logger.L.Infof("APNs VoIP push to %s status=%d reason=%s", textutil.TruncateRunes(deviceToken, 8), result.StatusCode, result.Reason)
	return result, nil
}

func (p *APNsProvider) httpClient() *http.Client {
	if p.client != nil {
		return p.client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (p *APNsProvider) authorizationToken() (string, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()

	now := p.currentTime()
	if p.token != "" && now.Before(p.tokenExp) {
		return p.token, nil
	}

	privateKey, err := p.privateKey()
	if err != nil {
		return "", err
	}

	claims := jwt.MapClaims{
		"iss": p.TeamID,
		"iat": now.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = p.KeyID

	signed, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("sign apns jwt: %w", err)
	}

	p.token = signed
	p.tokenExp = now.Add(apnsTokenTTL)
	return signed, nil
}

func (p *APNsProvider) privateKey() (*ecdsa.PrivateKey, error) {
	if p.key != nil {
		return p.key, nil
	}

	raw, err := os.ReadFile(p.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("read apns key: %w", err)
	}

	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("decode apns key pem: invalid format")
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse apns private key: %w", err)
	}

	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("apns private key is not ecdsa")
	}

	p.key = key
	return key, nil
}

func (p *APNsProvider) currentTime() time.Time {
	if p.nowFunc != nil {
		return p.nowFunc()
	}
	return time.Now()
}

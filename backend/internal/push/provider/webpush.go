package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	webPushDefaultTTL  = 60
	webPushSendTimeout = 8 * time.Second
)

type WebPushProvider struct {
	vapidPublicKey  string
	vapidPrivateKey string
	subscriber      string
	httpClient      *http.Client
	sendFunc        func(context.Context, []byte, *webpush.Subscription, *webpush.Options) (*http.Response, error)
}

type webPushDeviceToken struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func NewWebPush(vapidPublicKey, vapidPrivateKey, subscriber string) *WebPushProvider {
	return &WebPushProvider{
		vapidPublicKey:  strings.TrimSpace(vapidPublicKey),
		vapidPrivateKey: strings.TrimSpace(vapidPrivateKey),
		subscriber:      strings.TrimSpace(subscriber),
		httpClient: &http.Client{
			Timeout: webPushSendTimeout,
		},
		sendFunc: webpush.SendNotificationWithContext,
	}
}

func (p *WebPushProvider) Name() string { return "web_push" }

func (p *WebPushProvider) Send(ctx context.Context, deviceToken string, payload *PushPayload) (*PushResult, error) {
	if payload == nil {
		return nil, fmt.Errorf("push payload is nil")
	}
	if p == nil || p.sendFunc == nil {
		return nil, fmt.Errorf("web push provider is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if p.httpClient == nil {
		p.httpClient = &http.Client{
			Timeout: webPushSendTimeout,
		}
	}

	subscription, err := parseWebPushSubscription(deviceToken)
	if err != nil {
		return nil, err
	}

	payloadMap := map[string]any{
		"title":      payload.Title,
		"body":       payload.Body,
		"session_id": payload.SessionID,
		"badge":      payload.Badge,
		"badge_only": payload.BadgeOnly,
	}
	if payload.RecipientID > 0 {
		payloadMap["recipient_id"] = strconv.FormatInt(payload.RecipientID, 10)
	}
	message, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, fmt.Errorf("marshal web push payload: %w", err)
	}

	opts := &webpush.Options{
		Subscriber:      p.subscriber,
		VAPIDPublicKey:  p.vapidPublicKey,
		VAPIDPrivateKey: p.vapidPrivateKey,
		TTL:             webPushDefaultTTL,
		HTTPClient:      p.httpClient,
	}
	resp, err := p.sendFunc(ctx, message, subscription, opts)
	if err != nil {
		return nil, fmt.Errorf("send web push notification: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("send web push notification: empty response")
	}
	defer resp.Body.Close()

	result := &PushResult{
		Success:    resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices,
		StatusCode: resp.StatusCode,
	}
	if result.Success {
		return result, nil
	}

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		result.Reason = "subscription-expired"
		return result, nil
	}

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if readErr != nil {
		result.Reason = strings.TrimSpace(resp.Status)
		return result, nil
	}

	result.Reason = strings.TrimSpace(string(body))
	if result.Reason == "" {
		result.Reason = strings.TrimSpace(resp.Status)
	}
	return result, nil
}

func (p *WebPushProvider) IsTokenInvalid(result *PushResult) bool {
	return result != nil && (result.StatusCode == http.StatusNotFound || result.StatusCode == http.StatusGone || result.Reason == "subscription-expired")
}

func parseWebPushSubscription(deviceToken string) (*webpush.Subscription, error) {
	trimmed := strings.TrimSpace(deviceToken)
	if trimmed == "" {
		return nil, fmt.Errorf("web push token is empty")
	}

	var token webPushDeviceToken
	if err := json.Unmarshal([]byte(trimmed), &token); err != nil {
		return nil, fmt.Errorf("invalid web push token json: %w", err)
	}

	endpoint := strings.TrimSpace(token.Endpoint)
	p256dh := strings.TrimSpace(token.Keys.P256DH)
	auth := strings.TrimSpace(token.Keys.Auth)
	if endpoint == "" || p256dh == "" || auth == "" {
		return nil, fmt.Errorf("invalid web push token payload")
	}

	return &webpush.Subscription{
		Endpoint: endpoint,
		Keys: webpush.Keys{
			P256dh: p256dh,
			Auth:   auth,
		},
	}, nil
}

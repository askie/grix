package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/textutil"
)

type JPushProvider struct {
	AppKey       string
	MasterSecret string

	client  *http.Client
	baseURL string
}

func NewJPush(appKey, masterSecret string) *JPushProvider {
	return &JPushProvider{
		AppKey:       appKey,
		MasterSecret: masterSecret,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		baseURL: "https://api.jpush.cn",
	}
}

func (p *JPushProvider) Name() string { return "jpush" }

func (p *JPushProvider) Send(ctx context.Context, deviceToken string, payload *PushPayload) (*PushResult, error) {
	if strings.TrimSpace(deviceToken) == "" {
		return nil, fmt.Errorf("jpush device token is empty")
	}
	if payload == nil {
		return nil, fmt.Errorf("jpush payload is nil")
	}

	extras := map[string]string{
		"session_id": payload.SessionID,
	}
	if payload.RecipientID > 0 {
		extras["recipient_id"] = strconv.FormatInt(payload.RecipientID, 10)
	}
	if payload.SenderAvatarURL != "" {
		extras["sender_avatar_url"] = payload.SenderAvatarURL
	}
	if payload.SenderInitial != "" {
		extras["sender_initial"] = payload.SenderInitial
	}

	android := map[string]any{
		"alert":  payload.Body,
		"title":  payload.Title,
		"extras": extras,
	}
	if payload.SenderAvatarURL != "" {
		android["large_icon"] = payload.SenderAvatarURL
	}

	reqBody, err := json.Marshal(map[string]any{
		"platform": "android",
		"audience": map[string]any{
			"registration_id": []string{deviceToken},
		},
		"notification": map[string]any{
			"android": android,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal jpush payload: %w", err)
	}

	endpoint := strings.TrimRight(p.baseURL, "/") + "/v3/push"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, fmt.Errorf("create jpush request: %w", err)
	}
	req.Header.Set("authorization", "Basic "+p.basicAuthToken())
	req.Header.Set("content-type", "application/json")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("send jpush request: %w", err)
	}
	defer resp.Body.Close()

	result := &PushResult{
		Success:    resp.StatusCode >= 200 && resp.StatusCode < 300,
		StatusCode: resp.StatusCode,
	}
	if !result.Success {
		var jpErr struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&jpErr); err == nil {
			if jpErr.Error.Code != 0 {
				result.Reason = fmt.Sprintf("%d", jpErr.Error.Code)
			}
			if result.Reason == "" {
				result.Reason = jpErr.Error.Message
			}
		}
	}

	logger.L.Infof("JPush push to %s status=%d reason=%s", textutil.TruncateRunes(deviceToken, 8), result.StatusCode, result.Reason)
	return result, nil
}

func (p *JPushProvider) IsTokenInvalid(result *PushResult) bool {
	return result != nil && result.Reason == "1011"
}

func (p *JPushProvider) httpClient() *http.Client {
	if p.client != nil {
		return p.client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (p *JPushProvider) basicAuthToken() string {
	raw := p.AppKey + ":" + p.MasterSecret
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

type FCMProvider struct {
	CredentialsFile string

	client      *http.Client
	baseURL     string
	projectID   string
	tokenSource oauth2.TokenSource
}

func NewFCM(credentialsFile string) *FCMProvider {
	return &FCMProvider{
		CredentialsFile: credentialsFile,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		baseURL: "https://fcm.googleapis.com",
	}
}

func (p *FCMProvider) Name() string { return "fcm" }

func (p *FCMProvider) Send(ctx context.Context, deviceToken string, payload *PushPayload) (*PushResult, error) {
	if strings.TrimSpace(deviceToken) == "" {
		return nil, fmt.Errorf("fcm device token is empty")
	}
	if payload == nil {
		return nil, fmt.Errorf("fcm payload is nil")
	}

	projectID, err := p.resolveProjectID()
	if err != nil {
		return nil, err
	}
	accessToken, err := p.accessToken(ctx)
	if err != nil {
		return nil, err
	}

	data := map[string]string{
		"session_id": payload.SessionID,
	}
	if payload.RecipientID > 0 {
		data["recipient_id"] = strconv.FormatInt(payload.RecipientID, 10)
	}
	if payload.SenderAvatarURL != "" {
		data["sender_avatar_url"] = payload.SenderAvatarURL
	}
	if payload.SenderInitial != "" {
		data["sender_initial"] = payload.SenderInitial
	}

	notification := map[string]string{
		"title": payload.Title,
		"body":  payload.Body,
	}
	if payload.SenderAvatarURL != "" {
		notification["image"] = payload.SenderAvatarURL
	}

	// 分级投递：审批 / 语音拨号 / 来电等高优先级信号用 high 立即投递；
	// 普通消息用 normal 降级投递，由系统按节能策略择机送达。
	androidPriority := "normal"
	if payload.HighPriority || payload.ForcePush || payload.TimeSensitive || payload.Category != "" {
		androidPriority = "high"
	}

	reqBody, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"token":        deviceToken,
			"notification": notification,
			"data":         data,
			"android": map[string]any{
				"priority": androidPriority,
				"notification": map[string]string{
					"click_action": "PUSH_TAP_ACTION",
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal fcm payload: %w", err)
	}

	endpoint := strings.TrimRight(p.baseURL, "/") + "/v1/projects/" + projectID + "/messages:send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, fmt.Errorf("create fcm request: %w", err)
	}
	req.Header.Set("authorization", "Bearer "+accessToken)
	req.Header.Set("content-type", "application/json; charset=utf-8")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("send fcm request: %w", err)
	}
	defer resp.Body.Close()

	result := &PushResult{
		Success:    resp.StatusCode >= 200 && resp.StatusCode < 300,
		StatusCode: resp.StatusCode,
	}

	if !result.Success {
		var fcmErr struct {
			Error struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Details []struct {
					ErrorCode string `json:"errorCode"`
				} `json:"details"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&fcmErr); err == nil {
			result.Reason = fcmErr.Error.Status
			for _, detail := range fcmErr.Error.Details {
				if detail.ErrorCode != "" {
					result.Reason = detail.ErrorCode
					break
				}
			}
			if result.Reason == "UNREGISTERED" {
				result.Reason = "messaging/registration-token-not-registered"
			}
			if result.Reason == "" {
				result.Reason = fcmErr.Error.Message
			}
		}
	}

	logger.L.Infof("FCM push to %s status=%d reason=%s", textutil.TruncateRunes(deviceToken, 8), result.StatusCode, result.Reason)
	return result, nil
}

func (p *FCMProvider) IsTokenInvalid(result *PushResult) bool {
	return result != nil && result.Reason == "messaging/registration-token-not-registered"
}

// SendCallNotification 发送高优先级来电通知（Android 来电触达）。
// 使用 data-only 消息 + priority=high + content_available=true。
func (p *FCMProvider) SendCallNotification(ctx context.Context, deviceToken string, data map[string]string) (*PushResult, error) {
	if strings.TrimSpace(deviceToken) == "" {
		return nil, fmt.Errorf("fcm device token is empty")
	}

	projectID, err := p.resolveProjectID()
	if err != nil {
		return nil, err
	}
	accessToken, err := p.accessToken(ctx)
	if err != nil {
		return nil, err
	}

	reqBody, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"token": deviceToken,
			"data":  data,
			"android": map[string]any{
				"priority": "high",
			},
			"apns": map[string]any{
				"headers": map[string]string{
					"apns-priority": "10",
				},
				"payload": map[string]any{
					"aps": map[string]any{
						"content-available": 1,
					},
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal fcm call payload: %w", err)
	}

	endpoint := strings.TrimRight(p.baseURL, "/") + "/v1/projects/" + projectID + "/messages:send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, fmt.Errorf("create fcm call request: %w", err)
	}
	req.Header.Set("authorization", "Bearer "+accessToken)
	req.Header.Set("content-type", "application/json; charset=utf-8")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("send fcm call request: %w", err)
	}
	defer resp.Body.Close()

	result := &PushResult{
		Success:    resp.StatusCode >= 200 && resp.StatusCode < 300,
		StatusCode: resp.StatusCode,
	}
	logger.L.Infof("FCM call push to %s status=%d", textutil.TruncateRunes(deviceToken, 8), result.StatusCode)
	return result, nil
}

func (p *FCMProvider) httpClient() *http.Client {
	if p.client != nil {
		return p.client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (p *FCMProvider) accessToken(ctx context.Context) (string, error) {
	source, err := p.resolveTokenSource(ctx)
	if err != nil {
		return "", err
	}
	token, err := source.Token()
	if err != nil {
		return "", fmt.Errorf("resolve fcm access token: %w", err)
	}
	return token.AccessToken, nil
}

func (p *FCMProvider) resolveTokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	if p.tokenSource != nil {
		return p.tokenSource, nil
	}

	raw, err := os.ReadFile(p.CredentialsFile)
	if err != nil {
		return nil, fmt.Errorf("read fcm credentials: %w", err)
	}
	creds, err := google.CredentialsFromJSON(ctx, raw, fcmScope)
	if err != nil {
		return nil, fmt.Errorf("parse fcm credentials: %w", err)
	}
	p.tokenSource = creds.TokenSource
	return p.tokenSource, nil
}

func (p *FCMProvider) resolveProjectID() (string, error) {
	if p.projectID != "" {
		return p.projectID, nil
	}

	raw, err := os.ReadFile(p.CredentialsFile)
	if err != nil {
		return "", fmt.Errorf("read fcm credentials: %w", err)
	}
	var decoded struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("parse fcm project id: %w", err)
	}
	if strings.TrimSpace(decoded.ProjectID) == "" {
		return "", fmt.Errorf("fcm project_id is empty")
	}
	p.projectID = decoded.ProjectID
	return p.projectID, nil
}

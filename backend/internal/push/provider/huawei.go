package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/textutil"
)

// 华为 Push Kit 应答码。
// 参见 https://developer.huawei.com/consumer/cn/doc/HMSCore-References/https-send-api-0000001050986197
const (
	huaweiCodeSuccess = "80000000"
	// huaweiCodeAllTokenInvalid：本次下发的 token 全部无效（单 token 下发即该 token 失效）。
	huaweiCodeAllTokenInvalid = "80300007"
	// 鉴权类失败，需丢弃缓存的 access token 后重试。
	huaweiCodeTokenExpired = "80200001"
	huaweiCodeTokenFailed  = "80200003"
)

// HuaweiProvider 通过华为 Push Kit REST 接口下发通知栏消息。
// 消息由 EMUI/HarmonyOS 系统通道投递，应用进程被杀后仍可送达。
type HuaweiProvider struct {
	AppID        string
	ClientID     string
	ClientSecret string

	client   *http.Client
	authURL  string
	pushURL  string
	tokenBox accessTokenCache
}

func NewHuawei(appID, clientID, clientSecret string) *HuaweiProvider {
	return &HuaweiProvider{
		AppID:        appID,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		client:       &http.Client{Timeout: 10 * time.Second},
		authURL:      "https://oauth-login.cloud.huawei.com/oauth2/v3/token",
		pushURL:      "https://push-api.cloud.huawei.com",
	}
}

func (p *HuaweiProvider) Name() string { return "huawei" }

func (p *HuaweiProvider) Send(ctx context.Context, deviceToken string, payload *PushPayload) (*PushResult, error) {
	if strings.TrimSpace(deviceToken) == "" {
		return nil, fmt.Errorf("huawei device token is empty")
	}
	if payload == nil {
		return nil, fmt.Errorf("huawei payload is nil")
	}

	accessToken, err := p.tokenBox.get(ctx, p.fetchAccessToken)
	if err != nil {
		return nil, fmt.Errorf("huawei access token: %w", err)
	}

	extras := vendorExtras(payload)
	reqBody, err := json.Marshal(map[string]any{
		"validate_only": false,
		"message": map[string]any{
			"token": []string{deviceToken},
			"android": map[string]any{
				"notification": map[string]any{
					"title": payload.Title,
					"body":  payload.Body,
					"click_action": map[string]any{
						// type=1：点击后拉起 intent 指定的页面，extras 随 intent 传给客户端。
						"type":   1,
						"intent": vendorIntentURI(extras),
					},
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal huawei payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1/%s/messages:send", strings.TrimRight(p.pushURL, "/"), p.AppID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create huawei request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := vendorHTTPClient(p.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("send huawei request: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode huawei response: %w", err)
	}

	// 鉴权失效时清掉缓存，下一次下发会重新换取 token。
	if body.Code == huaweiCodeTokenExpired || body.Code == huaweiCodeTokenFailed {
		p.tokenBox.invalidate()
	}

	result := &PushResult{
		Success:    resp.StatusCode == http.StatusOK && body.Code == huaweiCodeSuccess,
		StatusCode: resp.StatusCode,
	}
	if !result.Success {
		result.Reason = body.Code
		if result.Reason == "" {
			result.Reason = body.Msg
		}
	}

	logger.L.Infof("Huawei push to %s status=%d reason=%s", textutil.TruncateRunes(deviceToken, 8), result.StatusCode, result.Reason)
	return result, nil
}

func (p *HuaweiProvider) IsTokenInvalid(result *PushResult) bool {
	return result != nil && result.Reason == huaweiCodeAllTokenInvalid
}

// fetchAccessToken 以 client_credentials 换取 Push Kit access token。
func (p *HuaweiProvider) fetchAccessToken(ctx context.Context) (string, time.Duration, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", p.ClientID)
	form.Set("client_secret", p.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.authURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("create huawei auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := vendorHTTPClient(p.client).Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("send huawei auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("huawei auth failed with status %d", resp.StatusCode)
	}

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", 0, fmt.Errorf("decode huawei auth response: %w", err)
	}
	return body.AccessToken, time.Duration(body.ExpiresIn) * time.Second, nil
}

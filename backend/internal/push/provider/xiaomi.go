package provider

import (
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

// 小米推送应答码。参见 https://dev.mi.com/console/doc/detail?pId=1163
const (
	xiaomiResultOK = "ok"
	// xiaomiNotifyEffectIntent：点击通知后拉起 extra.intent_uri 指定的意图。
	xiaomiNotifyEffectIntent = "2"
	// xiaomiNotifyTypeDefault：使用系统默认提示方式（声音/震动/呼吸灯）。
	xiaomiNotifyTypeDefault = "-1"
)

// XiaomiProvider 通过小米推送（MiPush）REST 接口下发通知栏消息。
// 消息由 MIUI/HyperOS 系统通道投递，应用进程被杀后仍可送达。
type XiaomiProvider struct {
	AppSecret string
	// PackageName 为客户端应用包名，小米要求下发时限定目标包。
	PackageName string

	client  *http.Client
	baseURL string
}

func NewXiaomi(appSecret, packageName string) *XiaomiProvider {
	return &XiaomiProvider{
		AppSecret:   appSecret,
		PackageName: packageName,
		client:      &http.Client{Timeout: 10 * time.Second},
		baseURL:     "https://api.xmpush.xiaomi.com",
	}
}

func (p *XiaomiProvider) Name() string { return "xiaomi" }

func (p *XiaomiProvider) Send(ctx context.Context, deviceToken string, payload *PushPayload) (*PushResult, error) {
	if strings.TrimSpace(deviceToken) == "" {
		return nil, fmt.Errorf("xiaomi device token is empty")
	}
	if payload == nil {
		return nil, fmt.Errorf("xiaomi payload is nil")
	}

	extras := vendorExtras(payload)

	form := url.Values{}
	form.Set("registration_id", deviceToken)
	form.Set("title", payload.Title)
	form.Set("description", payload.Body)
	form.Set("restricted_package_name", p.PackageName)
	form.Set("notify_type", xiaomiNotifyTypeDefault)
	form.Set("extra.notify_effect", xiaomiNotifyEffectIntent)
	form.Set("extra.intent_uri", vendorIntentURI(extras))
	for key, value := range extras {
		form.Set("extra."+key, value)
	}

	endpoint := strings.TrimRight(p.baseURL, "/") + "/v3/message/regid"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create xiaomi request: %w", err)
	}
	req.Header.Set("Authorization", "key="+p.AppSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")

	resp, err := vendorHTTPClient(p.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("send xiaomi request: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		Result      string `json:"result"`
		Code        int    `json:"code"`
		Reason      string `json:"reason"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode xiaomi response: %w", err)
	}

	result := &PushResult{
		Success:    resp.StatusCode == http.StatusOK && body.Result == xiaomiResultOK,
		StatusCode: resp.StatusCode,
	}
	if !result.Success {
		result.Reason = fmt.Sprintf("%d", body.Code)
		if body.Reason != "" {
			result.Reason = body.Reason
		} else if body.Description != "" {
			result.Reason = body.Description
		}
	}

	logger.L.Infof("Xiaomi push to %s status=%d reason=%s", textutil.TruncateRunes(deviceToken, 8), result.StatusCode, result.Reason)
	return result, nil
}

// IsTokenInvalid 恒为 false：小米下发接口对失效 regid 仍返回成功，
// 投递结果只能经由异步回执接口获知。据此判定失效会误杀正常设备，
// 因此小米通道不做 token 自动解绑。
func (p *XiaomiProvider) IsTokenInvalid(result *PushResult) bool {
	return false
}

package fxsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

// DefaultBaseURL 是免 Key 的公开端点前缀，实际请求形如 {base}/CNY。
const DefaultBaseURL = "https://open.er-api.com/v6/latest"

// maxResponseBytes 限制读取的响应体大小。正常响应约 10KB；故障或恶意的源可能返回
// 无界响应，在 30s 超时窗口内吃掉几百 MB 内存。
const maxResponseBytes = 1 << 20 // 1MB

// RateProvider 取「1 单位 sourceCurrency 折合多少 USD」及该报价的发布时间。
// 方向必须是 source→USD：入账按 sourceAmount × rate = USD 计算，反了会资损。
type RateProvider interface {
	FetchToUSD(ctx context.Context, sourceCurrency string) (rate decimal.Decimal, quotedAt time.Time, err error)
}

// erAPI 对接 exchangerate-api：免 Key 走公开端点，配了 Key 走带鉴权的同结构端点。
// 两者响应体字段一致，故共用一套解析。
type erAPI struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewERAPIProvider 按配置构造 provider；baseURL 为空时用 DefaultBaseURL。
func NewERAPIProvider(baseURL, apiKey string, client *http.Client) RateProvider {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &erAPI{baseURL: baseURL, apiKey: apiKey, client: client}
}

// 直接以 sourceCurrency 为 base 拉取，取响应里的 rates.USD——即 source→USD。
// 不用 base=USD 再取倒数：少一步换算就少一个把方向写反的机会。
func (p *erAPI) endpoint(sourceCurrency string) string {
	if p.apiKey != "" {
		return fmt.Sprintf("https://v6.exchangerate-api.com/v6/%s/latest/%s", p.apiKey, sourceCurrency)
	}
	return fmt.Sprintf("%s/%s", p.baseURL, sourceCurrency)
}

type erAPIResponse struct {
	Result             string             `json:"result"`
	ErrorType          string             `json:"error-type"`
	BaseCode           string             `json:"base_code"`
	TimeLastUpdateUnix int64              `json:"time_last_update_unix"`
	Rates              map[string]float64 `json:"rates"`
	// 付费端点把汇率表放在 conversion_rates，免费端点放在 rates。
	ConversionRates map[string]float64 `json:"conversion_rates"`
}

func (p *erAPI) FetchToUSD(ctx context.Context, sourceCurrency string) (decimal.Decimal, time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint(sourceCurrency), nil)
	if err != nil {
		return decimal.Zero, time.Time{}, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return decimal.Zero, time.Time{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, time.Time{}, fmt.Errorf("fxsync: %s upstream http %d", sourceCurrency, resp.StatusCode)
	}

	var body erAPIResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&body); err != nil {
		return decimal.Zero, time.Time{}, fmt.Errorf("fxsync: %s decode response: %w", sourceCurrency, err)
	}
	if body.Result != "success" {
		return decimal.Zero, time.Time{}, fmt.Errorf("fxsync: %s upstream result=%q error=%q", sourceCurrency, body.Result, body.ErrorType)
	}
	// base 必须正是我们要的源币种，否则拿到的 rates.USD 是别的货币兑美元，方向对但主体错。
	if body.BaseCode != sourceCurrency {
		return decimal.Zero, time.Time{}, fmt.Errorf("fxsync: %s upstream base_code=%q mismatch", sourceCurrency, body.BaseCode)
	}

	rates := body.Rates
	if len(rates) == 0 {
		rates = body.ConversionRates
	}
	usd, ok := rates["USD"]
	if !ok {
		return decimal.Zero, time.Time{}, fmt.Errorf("fxsync: %s upstream has no USD rate", sourceCurrency)
	}
	if body.TimeLastUpdateUnix <= 0 {
		return decimal.Zero, time.Time{}, fmt.Errorf("fxsync: %s upstream has no quote timestamp", sourceCurrency)
	}
	return decimal.NewFromFloat(usd), time.Unix(body.TimeLastUpdateUnix, 0), nil
}

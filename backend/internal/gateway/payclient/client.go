// Package payclient 是业务侧调用独立支付系统(cmd/pay)的 HTTP 客户端。
// 支付系统是独立服务，业务→支付走同步 HTTP 下单（见 docs/payment 设计 §10）。
package payclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Client 指向支付系统的内部可达地址。
type Client struct {
	baseURL string
	http    *http.Client
}

// New 用支付系统内部基址构造客户端，如 http://pay:27185。
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// CreateOrderRequest 是下单入参，与支付系统 POST /v1/pay/orders 的 body 对齐。
type CreateOrderRequest struct {
	BizType    string `json:"biz_type"`
	BizOrderID string `json:"biz_order_id"`
	Channel    string `json:"channel"`
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	Subject    string `json:"subject,omitempty"`
	ReturnURL  string `json:"return_url,omitempty"`
}

// CreateOrderResult 是支付系统下单返回。
type CreateOrderResult struct {
	PayOrderID     string `json:"pay_order_id"`
	Status         string `json:"status"`
	PayURL         string `json:"pay_url"`
	ChannelTradeNo string `json:"channel_trade_no"`
}

// CreateOrder 调支付系统创建支付单，返回收银台跳转。
func (c *Client) CreateOrder(ctx context.Context, req CreateOrderRequest) (*CreateOrderResult, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("payclient: base url not configured")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/pay/orders", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("payclient: create order status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out CreateOrderResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("payclient: bad response: %w", err)
	}
	return &out, nil
}

// OrderStatus 是支付系统对某支付单的权威状态（供入账前回验）。
type OrderStatus struct {
	Status   string          `json:"status"`
	Amount   decimal.Decimal `json:"amount"`
	Currency string          `json:"currency"`
}

// QueryOrder 回查支付系统某支付单的权威状态。业务侧入账前用它确认「确实付成了」，
// 不盲信 NATS 事件（防伪造/重放凭空入账）。
func (c *Client) QueryOrder(ctx context.Context, payOrderID int64) (*OrderStatus, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("payclient: base url not configured")
	}
	url := c.baseURL + "/v1/pay/orders/" + strconv.FormatInt(payOrderID, 10)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("payclient: query order status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out OrderStatus
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("payclient: bad query response: %w", err)
	}
	return &out, nil
}

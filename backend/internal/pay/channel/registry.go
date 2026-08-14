package channel

import (
	"fmt"
	"sort"
	"sync"
)

// Registry 是支付通道适配器的注册表，线程安全。
//
// 复刻 internal/agentadapter 的显式注册模式：适配器不在 init() 里自注册，
// 而是在 cmd/pay 启动处集中 NewRegistry() 后显式 Register 各通道
// （CN 区只注册支付宝，全球区只注册 PayPal，由启动配置决定）。
type Registry struct {
	mu    sync.RWMutex
	byID  map[string]PaymentChannel
	order []string
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]PaymentChannel)}
}

// Register 按 Code() 注册通道。空 code 或重复注册直接 panic
// （与 agentadapter 一致，属启动期编程错误，应尽早暴露）。
func (r *Registry) Register(c PaymentChannel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	code := c.Code()
	if code == "" {
		panic("pay/channel: empty channel code")
	}
	if _, dup := r.byID[code]; dup {
		panic(fmt.Sprintf("pay/channel: duplicate channel code %q", code))
	}
	r.byID[code] = c
	r.order = append(r.order, code)
	sort.Strings(r.order)
}

// Lookup 按 code 返回通道，不存在返回 (nil, false)。
func (r *Registry) Lookup(code string) (PaymentChannel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.byID[code]
	return c, ok
}

// SupportsCurrency 校验指定通道是否支持某币种。
func (r *Registry) SupportsCurrency(code, currency string) bool {
	c, ok := r.Lookup(code)
	if !ok {
		return false
	}
	for _, cur := range c.SupportedCurrencies() {
		if cur == currency {
			return true
		}
	}
	return false
}

// Codes 返回已注册的通道 code 列表（有序副本）。
func (r *Registry) Codes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

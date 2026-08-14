package identity

import (
	"sync"
)

// Registry 进程级单例：启动时按塘主配置注入 provider，
// 业务侧通过 GetSms(name) 取实例，避免每次发码新建 client（性能补丁清单第 21 条）。
//
// 塘主改 ak/sk 后调用 Reload 重建对应 provider。
type Registry struct {
	mu    sync.RWMutex
	byKey map[string]SmsProvider
}

var globalRegistry = &Registry{byKey: map[string]SmsProvider{}}

// Default 返回进程级单例。
func Default() *Registry {
	return globalRegistry
}

// SetSms 注册或覆盖一个 SMS provider。Reload 时使用。
func (r *Registry) SetSms(p SmsProvider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byKey[p.Name()] = p
}

// GetSms 按名取 provider；未配置返 nil + ErrProviderNotConfigured。
func (r *Registry) GetSms(name string) (SmsProvider, error) {
	r.mu.RLock()
	p, ok := r.byKey[name]
	r.mu.RUnlock()
	if !ok {
		return nil, ErrProviderNotConfigured
	}
	return p, nil
}

// Remove 解除某 provider 的注册（塘主关闭某 provider 时调用）。
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byKey, name)
}

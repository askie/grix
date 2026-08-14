package voiceadapter

import (
	"fmt"
	"sync"
)

// Factory 是创建 VoiceAgentBridge 实例的工厂函数。
// 每次通话调用一次，返回新实例。
type Factory func() VoiceAgentBridge

var (
	mu        sync.RWMutex
	factories = make(map[string]Factory) // family → Factory
)

// Register 注册一个 Provider 工厂。family 必须唯一，重复注册 panic。
func Register(family string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := factories[family]; exists {
		panic(fmt.Sprintf("voiceadapter: family %q already registered", family))
	}
	factories[family] = f
}

// New 根据 family 创建一个新的 VoiceAgentBridge 实例。
func New(family string) (VoiceAgentBridge, error) {
	mu.RLock()
	f, ok := factories[family]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("voiceadapter: unknown family %q", family)
	}
	return f(), nil
}

// Families 返回所有已注册的 family 列表（测试用）。
func Families() []string {
	mu.RLock()
	defer mu.RUnlock()
	result := make([]string, 0, len(factories))
	for k := range factories {
		result = append(result, k)
	}
	return result
}

// ResetForTest 清空注册表（仅测试用）。
func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	factories = make(map[string]Factory)
}

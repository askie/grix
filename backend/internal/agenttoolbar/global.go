package agenttoolbar

import "sync"

var (
	globalMu      sync.RWMutex
	globalService *Service
)

func SetGlobal(service *Service) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalService = service
}

func GetGlobal() *Service {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalService
}

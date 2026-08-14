package handler

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestInvalidateSessionTypeCacheRemovesEntry(t *testing.T) {
	const sid = "session-type-cache-invalidate-1"

	sessionTypeCacheMu.Lock()
	sessionTypeCache[sid] = 1
	sessionTypeCacheMu.Unlock()

	InvalidateSessionTypeCache(sid)

	sessionTypeCacheMu.RLock()
	_, ok := sessionTypeCache[sid]
	sessionTypeCacheMu.RUnlock()
	if ok {
		t.Fatalf("cache entry for %s should be removed", sid)
	}
}

func TestHandleSessionTypeInvalidateClearsCache(t *testing.T) {
	const sid = "session-type-cache-invalidate-2"

	sessionTypeCacheMu.Lock()
	sessionTypeCache[sid] = 2
	sessionTypeCacheMu.Unlock()

	payload, err := json.Marshal(protocol.SessionTypeInvalidatePayload{SessionID: sid})
	if err != nil {
		t.Fatalf("marshal payload error: %v", err)
	}
	if !handleSessionTypeInvalidate(payload) {
		t.Fatal("handleSessionTypeInvalidate should return true")
	}

	sessionTypeCacheMu.RLock()
	_, ok := sessionTypeCache[sid]
	sessionTypeCacheMu.RUnlock()
	if ok {
		t.Fatalf("cache entry for %s should be cleared via dispatch", sid)
	}
}

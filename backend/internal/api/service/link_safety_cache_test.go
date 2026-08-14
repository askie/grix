package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/pkg/urlguard"
	"github.com/stretchr/testify/assert"
)

// 缓存 key 必须包含 query，否则不同 query 串错共用同一 verdict。
func TestLinkVerdictCacheKey_IncludesQuery(t *testing.T) {
	c1 := urlguard.CanonURL{Host: "x.com", Path: "/r", Query: "to=a"}
	c2 := urlguard.CanonURL{Host: "x.com", Path: "/r", Query: "to=b"}
	c3 := urlguard.CanonURL{Host: "x.com", Path: "/r"}

	assert.NotEqual(t, linkVerdictCacheKey("sig1", c1), linkVerdictCacheKey("sig1", c2),
		"不同 query 必须落到不同 cache key")
	assert.NotEqual(t, linkVerdictCacheKey("sig1", c1), linkVerdictCacheKey("sig1", c3),
		"有 query vs 无 query 必须落到不同 cache key")
}

// 缓存 key 必须包含规则签名前缀：规则一改签名变 → 旧 key 自然失活。
// 这是去 SCAN+DEL 后保证一致性的关键。
func TestLinkVerdictCacheKey_SignatureChangesKey(t *testing.T) {
	c := urlguard.CanonURL{Host: "x.com", Path: "/p"}
	k1 := linkVerdictCacheKey("aaaaaaaaaaaa", c)
	k2 := linkVerdictCacheKey("bbbbbbbbbbbb", c)
	assert.NotEqual(t, k1, k2, "规则签名变化时 cache key 必须不同")
}

// 空 signature 退化但不应崩溃，且必须仍带前缀（便于后续 SCAN 清理）。
func TestLinkVerdictCacheKey_EmptySignature(t *testing.T) {
	k := linkVerdictCacheKey("", urlguard.CanonURL{Host: "x.com", Path: "/"})
	assert.Contains(t, k, "link:verdict:")
	assert.Contains(t, k, "x.com/")
}

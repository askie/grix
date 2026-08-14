package urlguard

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtract_HttpsAndBareDomain(t *testing.T) {
	got := Extract("看这个 https://evil.com/promo 和 good.org/x。")
	assert.Equal(t, []string{"https://evil.com/promo", "good.org/x"}, got)
}

func TestExtract_DedupAndOrder(t *testing.T) {
	got := Extract("a https://x.com b https://x.com c https://y.com")
	assert.Equal(t, []string{"https://x.com", "https://y.com"}, got)
}

func TestExtract_TrimsTrailingPunct(t *testing.T) {
	got := Extract("访问 https://x.com/a，或 https://y.com/b。")
	assert.Equal(t, []string{"https://x.com/a", "https://y.com/b"}, got)
}

func TestExtract_Empty(t *testing.T) {
	assert.Nil(t, Extract(""))
	assert.Nil(t, Extract("没有链接"))
}

func TestExtract_IgnoresJustWordsWithDot(t *testing.T) {
	// "e.g." 这类应当被 TLD 至少 2 位过滤掉（"g" 只有 1 位）
	got := Extract("比如 e.g. 之类")
	assert.Nil(t, got)
}

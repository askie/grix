package urlguard

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpressions_DomainAndPath(t *testing.T) {
	c, err := Canonicalize("http://a.b.c/1/2.html?p=1")
	require.NoError(t, err)
	exprs := c.Expressions()
	// 期望包含 host: a.b.c, b.c × path: /1/2.html?p=1, /1/2.html, /, /1/
	assert.Contains(t, exprs, "a.b.c/1/2.html?p=1")
	assert.Contains(t, exprs, "a.b.c/1/2.html")
	assert.Contains(t, exprs, "a.b.c/")
	assert.Contains(t, exprs, "a.b.c/1/")
	assert.Contains(t, exprs, "b.c/1/2.html")
	assert.Contains(t, exprs, "b.c/")
}

func TestHostSuffixes_LongHost(t *testing.T) {
	c, _ := Canonicalize("http://a.b.c.d.e.f.g/x")
	hosts := c.HostSuffixes()
	// Safe Browsing 规则：精确 host + 至多 4 个父域后缀（合计 ≤ 5）
	assert.LessOrEqual(t, len(hosts), 5)
	assert.Contains(t, hosts, "a.b.c.d.e.f.g")
	// 至少应包含最短可信父域之一
	assert.Contains(t, hosts, "f.g")
}

func TestHostSuffixes_IPNoExpand(t *testing.T) {
	c, _ := Canonicalize("http://1.2.3.4/x/y")
	hosts := c.HostSuffixes()
	assert.Equal(t, []string{"1.2.3.4"}, hosts, "IP 不应展开为父域后缀")
}

func TestExpressions_IPOnlyExact(t *testing.T) {
	c, _ := Canonicalize("http://1.2.3.4/x/y")
	for _, e := range c.Expressions() {
		assert.True(t, strings.HasPrefix(e, "1.2.3.4"), "IP 不应展开: %s", e)
	}
}

func TestPathPrefixes_RootHasNoExtra(t *testing.T) {
	c, _ := Canonicalize("http://evil.com/")
	paths := c.PathPrefixes()
	// 只应有 "/"
	assert.Equal(t, []string{"/"}, paths)
}

func TestExpressions_Dedup(t *testing.T) {
	c, _ := Canonicalize("http://evil.com/")
	exprs := c.Expressions()
	seen := make(map[string]struct{})
	for _, e := range exprs {
		_, dup := seen[e]
		assert.False(t, dup, "duplicate: %s", e)
		seen[e] = struct{}{}
	}
}

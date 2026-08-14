package urlguard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalize_BasicAndCases(t *testing.T) {
	cases := []struct {
		in   string
		host string
		path string
		isIP bool
	}{
		{"http://EVIL.com/A/../B/", "evil.com", "/B/", false},
		{"http://evil.com./a//b", "evil.com", "/a/b", false},
		{"evil.com/x", "evil.com", "/x", false},
		{"http://evil.com/%2561%2562", "evil.com", "/ab", false}, // 多重 percent-encode
		{"http://evil.com/?#frag", "evil.com", "/", false},
		{"http://a.b.c/p?q=1#frag", "a.b.c", "/p", false},
	}
	for _, c := range cases {
		got, err := Canonicalize(c.in)
		require.NoError(t, err, c.in)
		assert.Equal(t, c.host, got.Host, "host: %s", c.in)
		assert.Equal(t, c.path, got.Path, "path: %s", c.in)
		assert.Equal(t, c.isIP, got.IsIP, "isIP: %s", c.in)
	}
}

func TestCanonicalize_IPNormalization(t *testing.T) {
	// 这些都应等价于 127.0.0.1
	for _, in := range []string{
		"http://127.0.0.1/",
		"http://2130706433/",     // 整数型
		"http://0x7f000001/",     // 十六进制
		"http://0177.0.0.01/",    // 八进制
		"http://0x7f.0.0.0x01/",  // 多段 hex
	} {
		got, err := Canonicalize(in)
		require.NoError(t, err, in)
		assert.True(t, got.IsIP, "isIP: %s", in)
		assert.Equal(t, "127.0.0.1", got.Host, "host: %s", in)
	}

	got, err := Canonicalize("http://0x10203040/")
	require.NoError(t, err)
	assert.Equal(t, "16.32.48.64", got.Host)
}

func TestCanonicalize_IDNToPunycode(t *testing.T) {
	// 西里尔 а（U+0430）应被转 punycode
	got, err := Canonicalize("http://аpple.com/")
	require.NoError(t, err)
	assert.True(t, len(got.Host) > 0 && got.Host[:4] == "xn--", "host: %s", got.Host)
}

func TestCanonicalize_PreservesTrailingSlash(t *testing.T) {
	got, _ := Canonicalize("http://evil.com/path/")
	assert.Equal(t, "/path/", got.Path)
	got, _ = Canonicalize("http://evil.com/path")
	assert.Equal(t, "/path", got.Path)
}

func TestCanonicalize_EmptyAndBad(t *testing.T) {
	_, err := Canonicalize("")
	require.Error(t, err)
	_, err = Canonicalize("   \n\t")
	require.Error(t, err)
}

func TestCanonicalize_StripsControlChars(t *testing.T) {
	got, err := Canonicalize("http://e\tv\ril.com\n/x")
	require.NoError(t, err)
	assert.Equal(t, "evil.com", got.Host)
	assert.Equal(t, "/x", got.Path)
}

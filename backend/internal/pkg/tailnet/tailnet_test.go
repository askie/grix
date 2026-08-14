package tailnet

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- IsTailnetIP 测试 ----

func TestIsTailnetIP_ValidIPv4(t *testing.T) {
	cases := []string{
		"100.64.0.1",
		"100.100.100.100",
		"100.127.255.255",
	}
	for _, ip := range cases {
		assert.True(t, IsTailnetIP(ip), "expected tailnet IP: %s", ip)
	}
}

func TestIsTailnetIP_ValidIPv6(t *testing.T) {
	assert.True(t, IsTailnetIP("fd7a:115c:a1e0::1"))
}

func TestIsTailnetIP_InvalidIPs(t *testing.T) {
	cases := []string{
		"",
		"192.168.1.1",
		"10.0.0.1",
		"8.8.8.8",
		"not-an-ip",
		"100.63.255.255", // 刚好在 100.64.0.0/10 之外
		"100.128.0.0",    // 超出 100.64.0.0/10 范围
	}
	for _, ip := range cases {
		assert.False(t, IsTailnetIP(ip), "expected non-tailnet IP: %s", ip)
	}
}

func TestSameTailnet(t *testing.T) {
	assert.True(t, SameTailnet("100.64.0.1", "100.64.0.2"))
	assert.False(t, SameTailnet("100.64.0.1", "192.168.1.1"))
	assert.False(t, SameTailnet("", "100.64.0.1"))
}

// ---- TransferToken 测试 ----

var testSecret = []byte("test-secret-for-tailnet-transfer")

func TestIssueAndParseTransferToken(t *testing.T) {
	token, err := IssueTransferToken(testSecret, "tf:1:2:123", "node-a", "node-b", "/tmp/file.log", "download")
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := ParseTransferToken(testSecret, token)
	require.NoError(t, err)
	assert.Equal(t, "tf:1:2:123", claims.ActionID)
	assert.Equal(t, "node-a", claims.SrcNode)
	assert.Equal(t, "node-b", claims.DstNode)
	assert.Equal(t, "download", claims.Direction)
	assert.Equal(t, tokenTypeTransfer, claims.Type)
	assert.True(t, claims.ExpiresAt.After(time.Now()))
}

func TestIssueTransferToken_InvalidDirection(t *testing.T) {
	_, err := IssueTransferToken(testSecret, "tf:1:2:123", "node-a", "node-b", "/tmp/file.log", "invalid")
	assert.Error(t, err)
}

func TestIssueTransferToken_EmptySecret(t *testing.T) {
	_, err := IssueTransferToken(nil, "tf:1:2:123", "node-a", "node-b", "/tmp/file.log", "download")
	assert.Error(t, err)
}

func TestIssueTransferToken_MissingFields(t *testing.T) {
	_, err := IssueTransferToken(testSecret, "", "node-a", "node-b", "/tmp/file.log", "download")
	assert.Error(t, err)
}

func TestParseTransferToken_WrongSecret(t *testing.T) {
	token, err := IssueTransferToken(testSecret, "tf:1:2:123", "node-a", "node-b", "/tmp/file.log", "download")
	require.NoError(t, err)

	_, err = ParseTransferToken([]byte("wrong-secret"), token)
	assert.Error(t, err)
}

func TestParseTransferToken_InvalidToken(t *testing.T) {
	_, err := ParseTransferToken(testSecret, "not.a.jwt")
	assert.Error(t, err)
}

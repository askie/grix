package tailnet

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	transferTokenTTL  = 5 * time.Minute
	tokenTypeTransfer = "tailnet_transfer"
)

// TransferClaims 是 Tailnet 文件传输 JWT 的 claims。
type TransferClaims struct {
	ActionID    string `json:"sub"`      // 传输会话 ID（tf:xxx:yyy）
	SrcNode     string `json:"src_node"` // 源端 Node 连接标识
	DstNode     string `json:"dst_node"` // 目标端 Node 连接标识
	FileSHA256  string `json:"file_sha"` // 文件路径的 SHA256（绑定路径，非内容）
	Direction   string `json:"dir"`      // "download" 或 "upload"
	Type        string `json:"type"`     // 固定为 "tailnet_transfer"
	jwt.RegisteredClaims
}

// IssueTransferToken 签发一个短期文件传输 JWT。
// secret 复用 AIBOT_JWT_SECRET。
func IssueTransferToken(secret []byte, actionID, srcNode, dstNode, filePath, direction string) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("tailnet: jwt secret not configured")
	}
	if actionID == "" || srcNode == "" || dstNode == "" || filePath == "" {
		return "", errors.New("tailnet: missing required token fields")
	}
	dir := strings.TrimSpace(direction)
	if dir != "download" && dir != "upload" {
		return "", fmt.Errorf("tailnet: invalid direction %q", direction)
	}

	now := time.Now()
	claims := TransferClaims{
		ActionID:   actionID,
		SrcNode:    srcNode,
		DstNode:    dstNode,
		FileSHA256: hashPath(filePath),
		Direction:  dir,
		Type:       tokenTypeTransfer,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   actionID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(transferTokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// ParseTransferToken 解析并验证传输 JWT，返回 claims。
func ParseTransferToken(secret []byte, tokenStr string) (*TransferClaims, error) {
	if len(secret) == 0 {
		return nil, errors.New("tailnet: jwt secret not configured")
	}
	token, err := jwt.ParseWithClaims(tokenStr, &TransferClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("tailnet: unexpected signing method")
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*TransferClaims)
	if !ok || !token.Valid {
		return nil, errors.New("tailnet: invalid transfer token")
	}
	if claims.Type != tokenTypeTransfer {
		return nil, errors.New("tailnet: not a transfer token")
	}
	return claims, nil
}

// hashPath 对文件路径做 SHA256，用于绑定路径（不是文件内容）。
func hashPath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", sum)
}

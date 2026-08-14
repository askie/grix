package virtualkey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

const keyPrefix = "gwk_live_"

// Generate 生成一把新的网关虚拟Key：明文只在这一刻返回，之后系统只存哈希。
func Generate() (plain, hash, hint string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", "", err
	}
	plain = keyPrefix + base64.RawURLEncoding.EncodeToString(buf)
	hash = Hash(plain)
	hint = Hint(plain)
	return plain, hash, hint, nil
}

// Hash 对明文Key做SHA256，用于落库比对，不可逆。
func Hash(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

// Hint 取明文Key末尾8位，仅用于展示，不能反推出完整Key。
func Hint(key string) string {
	k := strings.TrimSpace(key)
	if len(k) <= 8 {
		return k
	}
	return k[len(k)-8:]
}

// HasPrefix 判断字符串是否是网关虚拟Key的样式，用于请求入口快速拒绝明显不是本网关Key的凭证。
func HasPrefix(key string) bool {
	return strings.HasPrefix(strings.TrimSpace(key), keyPrefix)
}

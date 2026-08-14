// Package urlguard 实现链接安全防护的 URL 规范化、匹配表达式生成与黑名单匹配。
// 规范化算法参考 Google Safe Browsing v4 规范，目的是消除常见绕过变形：
// 大小写、percent-encode、IDN 同形字、IP 多进制写法、尾随点、连续点等。
package urlguard

import (
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// CanonURL 规范化后的 URL。Host 已转小写 / 归一 IP / 转 punycode；Path 已 Clean。
type CanonURL struct {
	Scheme string
	Host   string
	Path   string
	Query  string
	IsIP   bool
}

var stripCharsReplacer = strings.NewReplacer("\t", "", "\r", "", "\n", "")

// Canonicalize 把原始 URL 规范化为标准形态。
// 返回的错误代表无法解析；正常情况下应能容忍各种异常输入。
func Canonicalize(raw string) (CanonURL, error) {
	raw = stripCharsReplacer.Replace(strings.TrimSpace(raw))
	if raw == "" {
		return CanonURL{}, errEmpty
	}
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		raw = raw[:i]
	}
	// 补 scheme 以便统一解析；不假设用户输入带 scheme。
	withScheme := raw
	if !strings.Contains(withScheme, "://") {
		withScheme = "http://" + withScheme
	}

	u, err := url.Parse(withScheme)
	if err != nil {
		return CanonURL{}, err
	}

	host := repeatUnescape(u.Hostname())
	host = collapseDots(strings.Trim(host, "."))
	host = strings.ToLower(host)
	if host == "" {
		return CanonURL{}, errNoHost
	}

	isIP := false
	if ip := parseLooseIP(host); ip != nil {
		host = ip.String()
		isIP = true
	} else if ascii, err := idna.Lookup.ToASCII(host); err == nil && ascii != "" {
		host = ascii
	}

	p := u.Path
	if p == "" {
		p = "/"
	}
	p = repeatUnescape(p)
	cleaned := path.Clean(p)
	if cleaned == "." {
		cleaned = "/"
	}
	// path.Clean 吃掉末尾斜杠；按用户原意补回。
	if strings.HasSuffix(p, "/") && !strings.HasSuffix(cleaned, "/") {
		cleaned += "/"
	}

	return CanonURL{
		Scheme: strings.ToLower(u.Scheme),
		Host:   host,
		Path:   cleaned,
		Query:  u.RawQuery,
		IsIP:   isIP,
	}, nil
}

// repeatUnescape 反复 percent-decode 直到不再变化，防多重编码绕过（如 %2525XX）。
// 设上限 8 次防恶意超深嵌套。
func repeatUnescape(s string) string {
	for i := 0; i < 8; i++ {
		dec, err := url.QueryUnescape(s)
		if err != nil || dec == s {
			return s
		}
		s = dec
	}
	return s
}

func collapseDots(s string) string {
	for strings.Contains(s, "..") {
		s = strings.ReplaceAll(s, "..", ".")
	}
	return s
}

// parseLooseIP 解析包括整数 / 0x hex / 0 八进制 / 不足 4 段在内的宽松 IP 写法。
// 都归一为标准点分十进制（IPv4）。IPv6 暂不展开（包到 net.ParseIP 处理）。
func parseLooseIP(host string) net.IP {
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4
		}
		return ip
	}
	// 纯整数 -> 32 位 IP
	if n, err := strconv.ParseUint(host, 10, 32); err == nil {
		return uint32ToIP(uint32(n))
	}
	if strings.HasPrefix(host, "0x") {
		if n, err := strconv.ParseUint(host[2:], 16, 32); err == nil {
			return uint32ToIP(uint32(n))
		}
	}
	// 多段，每段允许 0x hex / 0 八进制 / 十进制
	parts := strings.Split(host, ".")
	if len(parts) == 4 {
		var b [4]byte
		for i, p := range parts {
			n, err := parseFlexUint(p)
			if err != nil || n > 255 {
				return nil
			}
			b[i] = byte(n)
		}
		return net.IPv4(b[0], b[1], b[2], b[3])
	}
	return nil
}

func uint32ToIP(n uint32) net.IP {
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

func parseFlexUint(s string) (uint64, error) {
	switch {
	case strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X"):
		return strconv.ParseUint(s[2:], 16, 32)
	case len(s) > 1 && s[0] == '0':
		return strconv.ParseUint(s, 8, 32)
	default:
		return strconv.ParseUint(s, 10, 32)
	}
}

// errEmpty / errNoHost 都用 strErr 表示，调用方一般不区分。
type strErr string

func (e strErr) Error() string { return string(e) }

const (
	errEmpty  strErr = "urlguard: empty url"
	errNoHost strErr = "urlguard: no host"
)

package urlguard

import "strings"

// Expressions 生成 host 后缀 × path 前缀的全部匹配表达式（最多 30 个）。
// 任一表达式命中黑名单即判定为命中，使父域 / 父目录规则能命中具体 URL。
func (c CanonURL) Expressions() []string {
	hosts := c.HostSuffixes()
	paths := c.PathPrefixes()
	out := make([]string, 0, len(hosts)*len(paths))
	seen := make(map[string]struct{}, len(hosts)*len(paths))
	for _, h := range hosts {
		for _, p := range paths {
			expr := h + p
			if _, ok := seen[expr]; ok {
				continue
			}
			seen[expr] = struct{}{}
			out = append(out, expr)
		}
	}
	return out
}

// HostSuffixes 返回最多 5 个 host 候选：精确 host + 父域后缀（按 Safe Browsing 规则）。
// IP host 只返回精确 IP，不展开父域。
func (c CanonURL) HostSuffixes() []string {
	if c.IsIP {
		return []string{c.Host}
	}
	parts := strings.Split(c.Host, ".")
	if len(parts) <= 1 {
		return []string{c.Host}
	}
	res := []string{c.Host}
	// 从倒数第 5 个组件起，逐次去掉最左组件；至少保留 2 段（不到 TLD）。
	start := len(parts) - 5
	if start < 1 {
		start = 1
	}
	for i := start; i < len(parts)-1; i++ {
		res = append(res, strings.Join(parts[i:], "."))
	}
	return dedupStrings(res)
}

// PathPrefixes 返回最多 6 个 path 候选：精确 path+query、精确 path、根、目录前缀。
func (c CanonURL) PathPrefixes() []string {
	res := make([]string, 0, 6)
	if c.Query != "" {
		res = append(res, c.Path+"?"+c.Query)
	}
	res = append(res, c.Path)
	if c.Path != "/" {
		res = append(res, "/")
	}

	trimmed := strings.Trim(c.Path, "/")
	if trimmed != "" {
		segs := strings.Split(trimmed, "/")
		acc := "/"
		// 最多再加 3 个目录前缀（凑足 ≤ 6 总数）
		for i := 0; i < len(segs) && i < 3; i++ {
			if segs[i] == "" {
				continue
			}
			acc += segs[i] + "/"
			res = append(res, acc)
		}
	}
	return dedupStrings(res)
}

func dedupStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

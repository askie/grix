// Package ipgeo 提供离线 IP 地理归属查询（基于 ip2region xdb，随二进制 go:embed，
// 不依赖外部服务，CN/全球区部署均可用）。
// 数据源：github.com/lionsoul2014/ip2region data/ip2region_v4.xdb（IPv4）。
// IPv6 与查不到的 IP 返回空串，调用方按"未知归属地"处理，不影响主流程。
package ipgeo

import (
	_ "embed"
	"net"
	"strings"
	"sync"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

//go:embed data/ip2region.xdb
var xdbData []byte

var (
	initOnce sync.Once
	searcher *xdb.Searcher
	mu       sync.Mutex
)

func getSearcher() *xdb.Searcher {
	initOnce.Do(func() {
		s, err := xdb.NewWithBuffer(xdb.IPv4, xdbData)
		if err != nil {
			return
		}
		searcher = s
	})
	return searcher
}

// Lookup 返回 IP 的地理归属描述，如 "中国 广东省 深圳市" 或 "美国"。
// 内网/回环/非法/IPv6 返回空串。
func Lookup(ip string) string {
	trimmed := strings.TrimSpace(ip)
	if trimmed == "" {
		return ""
	}
	parsed := net.ParseIP(trimmed)
	if parsed == nil || parsed.To4() == nil {
		return ""
	}
	if parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || parsed.IsUnspecified() {
		return ""
	}
	s := getSearcher()
	if s == nil {
		return ""
	}
	// xdb Searcher 非并发安全（内部有共享读缓冲），串行化查询；
	// 查询是内存二分，微秒级，握手频率下无性能压力。
	mu.Lock()
	region, err := s.Search(trimmed)
	mu.Unlock()
	if err != nil {
		return ""
	}
	return formatRegion(region)
}

// formatRegion 把 ip2region 原始结果 "国家|区域|省份|城市|ISP" 压成人读格式，
// 去掉 0/空 占位段与 ISP 段。
func formatRegion(raw string) string {
	parts := strings.Split(raw, "|")
	keep := make([]string, 0, 4)
	// 只取 国家/区域/省/市 四段
	for i, part := range parts {
		if i >= 4 {
			break
		}
		p := strings.TrimSpace(part)
		if p == "" || p == "0" {
			continue
		}
		// 去重相邻重复（如 国家=区域）
		if len(keep) > 0 && keep[len(keep)-1] == p {
			continue
		}
		keep = append(keep, p)
	}
	return strings.Join(keep, " ")
}

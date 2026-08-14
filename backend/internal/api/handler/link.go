package handler

import (
	"net/http"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// 单次校验最多 URL 数（前端兜底；避免被刷接口）
const linkCheckMaxBatch = 20

// linkCheckMaxBodyBytes 限制 /v1/link/check 请求体最大字节数。
// 正常 20 个 URL × 2KB/URL 远低于 16KB；超大 body 直接拒。
const linkCheckMaxBodyBytes = 16 * 1024

type linkCheckRequest struct {
	URLs []string `json:"urls"`
}

// linkCheckPublicVerdict 对外裁剪过的判定结果：只暴露 verdict / canonical_host。
// 不返回 rule_id / rule_source / reason（防止攻击者批量探测黑名单内部结构）。
type linkCheckPublicVerdict struct {
	URL           string `json:"url"`
	Verdict       string `json:"verdict"`
	CanonicalHost string `json:"canonical_host,omitempty"`
}

// LinkCheck POST /v1/link/check
// 接收一批原始 URL，返回每条的 verdict（clean / suspicious / malicious）。
// 失败不阻断业务（前端按"可疑"或"放行"兜底）。
func LinkCheck(c *gin.Context) {
	// 限制请求体大小，超过即拒（防超大 JSON 撑爆内存）。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, linkCheckMaxBodyBytes)
	var body linkCheckRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	urls := normalizeLinkCheckInput(body.URLs)
	if len(urls) == 0 {
		response.OK(c, gin.H{"results": []linkCheckPublicVerdict{}})
		return
	}
	if len(urls) > linkCheckMaxBatch {
		urls = urls[:linkCheckMaxBatch]
	}
	// 把当前用户 ID / IP / UA 注入 ctx，service 写审计时能拿到。
	ctx := c.Request.Context()
	if v, ok := c.Get("user_id"); ok {
		if uid, ok := v.(int64); ok && uid > 0 {
			ctx = service.ContextWithUserID(ctx, uid)
		}
	}
	ctx = service.ContextWithClientMeta(ctx, service.LinkClientMeta{
		ClientIP:  c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})

	full := service.CheckLinks(ctx, urls)
	public := make([]linkCheckPublicVerdict, len(full))
	for i, v := range full {
		public[i] = linkCheckPublicVerdict{
			URL:           v.URL,
			Verdict:       v.Verdict,
			CanonicalHost: v.CanonicalHost,
		}
	}
	response.OK(c, gin.H{"results": public})
}

func normalizeLinkCheckInput(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

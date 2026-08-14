// Package upstream 定义"透传请求给真实厂商、拿到归一化usage"的适配器接口。
// 与 internal/llm/provider 是两回事：那个是内部AI编排用的窄接口，这个要支持任意请求体透传+多种协议。
package upstream

import (
	"context"
	"net/http"

	"github.com/askie/grix/backend/internal/gateway/pricing"
)

// Upstream 是某个真实厂商的转发适配器。
type Upstream interface {
	Name() string
	// Forward 把 rawBody 原样转发给真实厂商，响应原样写入 w（支持流式/非流式），
	// 并在拿到完整 usage 后返回归一化结果。model 是这次请求实际用的模型名，供计价查询。
	Forward(ctx context.Context, rawBody []byte, stream bool, w http.ResponseWriter) (model string, usage *pricing.Usage, err error)
}

// ResponsesUpstream 是原生支持 OpenAI Responses API 的厂商适配器。
// 不实现该接口的厂商仍由 httpapi 层走 Responses↔Chat 桥接。
type ResponsesUpstream interface {
	ForwardResponses(ctx context.Context, rawBody []byte, stream bool, w http.ResponseWriter) (model string, usage *pricing.Usage, err error)
}

// CountTokensUpstream 是原生支持 Anthropic messages/count_tokens 的厂商适配器。
// count_tokens 不产生生成用量、不计费，响应体原样透传（含上游错误体）。
type CountTokensUpstream interface {
	CountTokens(ctx context.Context, rawBody []byte) (status int, body []byte, err error)
}

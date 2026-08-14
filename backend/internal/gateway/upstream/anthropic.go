package upstream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/gateway/pricing"
)

// anthropicVersion 是 Anthropic 原生协议要求的版本头；国内厂商的 Anthropic 兼容端点同样接受它。
const anthropicVersion = "2023-06-01"

// anthropicPassthroughHeaders 是允许从客户端透传到上游的原生协议头白名单
// （plan-direct-relay-first-refactor §6.4）。直连模式下 Claude Code 会带 anthropic-beta
// 声明它要用的 feature；其余头部一律不转发：客户端的虚拟 x-api-key / Authorization、
// hop-by-hop 头、代理/调试头都必须剥离，上游鉴权恒由服务端注入真实厂商 Key。
// 新增条目前必须先用真实厂商端点验证过该头的语义。
var anthropicPassthroughHeaders = []string{"anthropic-version", "anthropic-beta"}

// FilterAnthropicPassthroughHeaders 从入站请求头里筛出白名单允许透传的部分。
func FilterAnthropicPassthroughHeaders(inbound http.Header) http.Header {
	out := make(http.Header, len(anthropicPassthroughHeaders))
	for _, k := range anthropicPassthroughHeaders {
		if vs := inbound.Values(k); len(vs) > 0 {
			out[http.CanonicalHeaderKey(k)] = vs
		}
	}
	return out
}

// AnthropicAdapter 走 Anthropic 原生协议转发（POST {baseURL}/v1/messages）。
// 一期用于国内厂商的 Anthropic 兼容端点（如 DeepSeek 的 https://api.deepseek.com/anthropic），
// 用我们自己的官方Key（后台动态下发）。未来接真·Anthropic 官方时复用同一适配器，只换 baseURL/Key。
type AnthropicAdapter struct {
	apiKey     string
	baseURL    string // 到 /anthropic 根，Forward 时再拼 /v1/messages
	httpClient *http.Client
	// passthrough 是经 FilterAnthropicPassthroughHeaders 白名单过滤后的客户端协议头。
	passthrough http.Header
}

// NewAnthropic 构造 Anthropic 协议转发适配器。baseURL 必须是 messages 端点的根（不含 /v1/messages）。
func NewAnthropic(apiKey, baseURL string, client *http.Client) *AnthropicAdapter {
	baseURL = strings.TrimRight(baseURL, "/")
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	return &AnthropicAdapter{apiKey: apiKey, baseURL: baseURL, httpClient: client}
}

func (a *AnthropicAdapter) Name() string { return "anthropic" }

// WithPassthroughHeaders 挂载经白名单过滤的客户端协议头（见 FilterAnthropicPassthroughHeaders）。
// 只接受过滤后的结果；适配器不再二次校验白名单，但鉴权头恒由 applyHeaders 覆盖。
func (a *AnthropicAdapter) WithPassthroughHeaders(h http.Header) *AnthropicAdapter {
	a.passthrough = h
	return a
}

// applyHeaders 组装发往上游的头：白名单透传头在前（客户端的 anthropic-version 优先于默认值），
// 鉴权与内容头在最后恒由服务端权威覆写——即使白名单将来误加条目，客户端也注不进来 Key。
func (a *AnthropicAdapter) applyHeaders(req *http.Request) {
	for k, vs := range a.passthrough {
		req.Header.Del(k)
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	// 双保险：即使调用方绕过白名单过滤器，鉴权头也到不了上游。
	req.Header.Del("Authorization")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	if req.Header.Get("anthropic-version") == "" {
		req.Header.Set("anthropic-version", anthropicVersion)
	}
}

// anthropicUsage 是 Anthropic Messages API 的用量字段（国内兼容端点同构）。
// input_tokens 已经是"未命中缓存的新输入"，不含缓存读/写；缓存读、缓存写各自独立计。
type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// normalizeAnthropicUsage 把 Anthropic 用量归一到网关计价桶。
// CachedTokens=缓存读；UncachedTokens=新输入+缓存写（DeepSeek 无缓存写、恒为0，与其 OpenAI 路径命中/未命中口径一致）。
// 说明：真·Anthropic 的缓存写按 1.25x 计价，与未命中不同；接真 Anthropic 时需单列缓存写桶，此处按国内厂商口径先合入未命中。
func normalizeAnthropicUsage(u anthropicUsage) *pricing.Usage {
	return &pricing.Usage{
		CachedTokens:     u.CacheReadInputTokens,
		UncachedTokens:   u.InputTokens + u.CacheCreationInputTokens,
		CompletionTokens: u.OutputTokens,
		ReasoningTokens:  0,
	}
}

func (a *AnthropicAdapter) Forward(ctx context.Context, rawBody []byte, stream bool, w http.ResponseWriter) (string, *pricing.Usage, error) {
	// 只探测 model（用于失败留痕/日志），请求体原样透传给厂商。
	var probe struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(rawBody, &probe)
	modelName := probe.Model

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(rawBody))
	if err != nil {
		return modelName, nil, err
	}
	a.applyHeaders(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return modelName, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return modelName, nil, fmt.Errorf("anthropic upstream error %d: %s", resp.StatusCode, string(respBody))
	}

	if stream {
		return a.forwardStream(modelName, resp.Body, w)
	}
	return a.forwardNonStream(modelName, resp.Body, w)
}

// CountTokens 转发 Anthropic messages/count_tokens（POST {baseURL}/v1/messages/count_tokens）。
// 不产生生成用量、不计费；响应（含上游错误体）连同状态码原样返回给调用方透传。
// err 只保留传输层失败；上游业务错误走 status+body 由网关原样回给客户端。
func (a *AnthropicAdapter) CountTokens(ctx context.Context, rawBody []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages/count_tokens", bytes.NewReader(rawBody))
	if err != nil {
		return 0, nil, err
	}
	a.applyHeaders(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

func (a *AnthropicAdapter) forwardNonStream(modelName string, body io.Reader, w http.ResponseWriter) (string, *pricing.Usage, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return modelName, nil, err
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)

	var parsed struct {
		Model string         `json:"model"`
		Usage anthropicUsage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return modelName, nil, fmt.Errorf("parse anthropic response: %w", err)
	}
	if parsed.Model != "" {
		modelName = parsed.Model
	}
	// 与流式端 gotStart 门槛对称：响应缺 usage（零值）时不结算，走 no_usage 失败留痕，防止静默记 0 元账。
	if parsed.Usage == (anthropicUsage{}) {
		return modelName, nil, nil
	}
	return modelName, normalizeAnthropicUsage(parsed.Usage), nil
}

// forwardStream 透传 Anthropic SSE（event:/data: 行原样回写），同时解析用量。
// Anthropic 把用量拆两处：message_start.message.usage 带输入/缓存，message_delta.usage 带最终输出；
// 这里合并——输入/缓存取 message_start，输出取最后一个 message_delta。
func (a *AnthropicAdapter) forwardStream(modelName string, body io.Reader, w http.ResponseWriter) (string, *pricing.Usage, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)

	var (
		gotStart bool
		merged   anthropicUsage
	)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(w, "%s\n", line)
		if flusher != nil {
			flusher.Flush()
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var evt struct {
			Type    string `json:"type"`
			Message *struct {
				Model string         `json:"model"`
				Usage anthropicUsage `json:"usage"`
			} `json:"message"`
			Usage *anthropicUsage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}
		switch evt.Type {
		case "message_start":
			if evt.Message != nil {
				if evt.Message.Model != "" {
					modelName = evt.Message.Model
				}
				merged.InputTokens = evt.Message.Usage.InputTokens
				merged.CacheCreationInputTokens = evt.Message.Usage.CacheCreationInputTokens
				merged.CacheReadInputTokens = evt.Message.Usage.CacheReadInputTokens
				merged.OutputTokens = evt.Message.Usage.OutputTokens
				gotStart = true
			}
		case "message_delta":
			if evt.Usage != nil {
				merged.OutputTokens = evt.Usage.OutputTokens
				// 部分实现的 message_delta 也回带完整输入/缓存（如 DeepSeek），有则一并采信。
				if evt.Usage.InputTokens > 0 {
					merged.InputTokens = evt.Usage.InputTokens
				}
				if evt.Usage.CacheReadInputTokens > 0 {
					merged.CacheReadInputTokens = evt.Usage.CacheReadInputTokens
				}
				if evt.Usage.CacheCreationInputTokens > 0 {
					merged.CacheCreationInputTokens = evt.Usage.CacheCreationInputTokens
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return modelName, nil, err
	}
	if !gotStart {
		return modelName, nil, fmt.Errorf("anthropic stream ended without message_start usage")
	}
	return modelName, normalizeAnthropicUsage(merged), nil
}

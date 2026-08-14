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

const deepseekBaseURL = "https://api.deepseek.com"

// DeepSeekAdapter 转发到 DeepSeek 官方 API，用的是我们自己的官方Key（由后台动态下发），不是用户的虚拟Key。
type DeepSeekAdapter struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewDeepSeek 构造 DeepSeek 转发适配器。baseURL 为空用默认端点；client 为空自建（生产传共享 client 复用连接池）。
func NewDeepSeek(apiKey, baseURL string, client *http.Client) *DeepSeekAdapter {
	if baseURL == "" {
		baseURL = deepseekBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	return &DeepSeekAdapter{apiKey: apiKey, baseURL: baseURL, httpClient: client}
}

func (d *DeepSeekAdapter) Name() string { return "deepseek" }

type deepseekUsage struct {
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
	InputTokens           int `json:"input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	InputTokensDetails    *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
	OutputTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

func normalizeDeepSeekUsage(u deepseekUsage) *pricing.Usage {
	cached := u.PromptCacheHitTokens
	if u.InputTokensDetails != nil {
		cached = u.InputTokensDetails.CachedTokens
	}
	uncached := u.PromptCacheMissTokens
	if uncached == 0 {
		input := u.PromptTokens
		if input == 0 {
			input = u.InputTokens
		}
		uncached = input - cached
		if uncached < 0 {
			uncached = 0
		}
	}
	completion := u.CompletionTokens
	if completion == 0 {
		completion = u.OutputTokens
	}
	reasoning := 0
	if u.CompletionTokensDetails != nil {
		reasoning = u.CompletionTokensDetails.ReasoningTokens
	}
	if u.OutputTokensDetails != nil {
		reasoning = u.OutputTokensDetails.ReasoningTokens
	}
	return &pricing.Usage{
		CachedTokens:     cached,
		UncachedTokens:   uncached,
		CompletionTokens: completion,
		ReasoningTokens:  reasoning,
	}
}

func (d *DeepSeekAdapter) Forward(ctx context.Context, rawBody []byte, stream bool, w http.ResponseWriter) (string, *pricing.Usage, error) {
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return "", nil, fmt.Errorf("parse request body: %w", err)
	}
	modelName, _ := payload["model"].(string)

	if stream {
		// 需要显式要求 usage，否则流式响应最后一块不会带 usage，网关就没法计费。
		payload["stream_options"] = map[string]any{"include_usage": true}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, fmt.Errorf("re-marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url("/chat/completions"), bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return modelName, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return modelName, nil, fmt.Errorf("deepseek upstream error %d: %s", resp.StatusCode, string(respBody))
	}

	if stream {
		return d.forwardStream(modelName, resp.Body, w)
	}
	return d.forwardNonStream(modelName, resp.Body, w)
}

// ForwardResponses 直接转发到 DeepSeek 官方 Responses API。DeepSeek Codex 集成使用
// wire_api=responses，不能再经 Chat Completions 桥接，否则 custom tool 等 Codex 语义会失真。
func (d *DeepSeekAdapter) ForwardResponses(ctx context.Context, rawBody []byte, stream bool, w http.ResponseWriter) (string, *pricing.Usage, error) {
	body, modelName, err := forceResponsesStoreFalse(rawBody)
	if err != nil {
		return "", nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url("/responses"), bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return modelName, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return modelName, nil, fmt.Errorf("deepseek responses upstream error %d: %s", resp.StatusCode, string(respBody))
	}

	if stream {
		return d.forwardResponsesStream(modelName, resp.Body, w)
	}
	return d.forwardResponsesNonStream(modelName, resp.Body, w)
}

func (d *DeepSeekAdapter) forwardResponsesNonStream(modelName string, body io.Reader, w http.ResponseWriter) (string, *pricing.Usage, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return modelName, nil, err
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)

	var parsed struct {
		Model string         `json:"model"`
		Usage *deepseekUsage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return modelName, nil, fmt.Errorf("parse deepseek responses response: %w", err)
	}
	if parsed.Model != "" {
		modelName = parsed.Model
	}
	if parsed.Usage == nil {
		return modelName, nil, nil
	}
	return modelName, normalizeDeepSeekUsage(*parsed.Usage), nil
}

func (d *DeepSeekAdapter) forwardResponsesStream(modelName string, body io.Reader, w http.ResponseWriter) (string, *pricing.Usage, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)

	var usage *pricing.Usage
	reader := bufio.NewReader(body)
	for {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line != "" || readErr == nil {
			modelName, usage = writeAndParseResponsesSSELine(w, flusher, line, modelName, usage)
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return modelName, usage, readErr
		}
	}
	if usage == nil {
		return modelName, nil, fmt.Errorf("deepseek responses stream ended without usage")
	}
	return modelName, usage, nil
}

func writeAndParseResponsesSSELine(w http.ResponseWriter, flusher http.Flusher, line, modelName string, usage *pricing.Usage) (string, *pricing.Usage) {
	if line != "" {
		fmt.Fprintf(w, "%s\n", line)
	} else {
		fmt.Fprintln(w)
	}
	if flusher != nil {
		flusher.Flush()
	}

	if !strings.HasPrefix(line, "data: ") {
		return modelName, usage
	}
	data := strings.TrimPrefix(line, "data: ")
	var event deepseekResponseEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return modelName, usage
	}
	if event.Model != "" {
		modelName = event.Model
	}
	if event.Response != nil && event.Response.Model != "" {
		modelName = event.Response.Model
	}
	if event.Usage != nil {
		usage = normalizeDeepSeekUsage(*event.Usage)
	}
	if event.Response != nil && event.Response.Usage != nil {
		usage = normalizeDeepSeekUsage(*event.Response.Usage)
	}
	return modelName, usage
}

func forceResponsesStoreFalse(raw []byte) ([]byte, string, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, "", fmt.Errorf("parse responses request body: %w", err)
	}
	var modelName string
	_ = json.Unmarshal(payload["model"], &modelName)
	payload["store"] = json.RawMessage("false")
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("re-marshal responses request body: %w", err)
	}
	return body, modelName, nil
}

type deepseekResponseEvent struct {
	Model    string         `json:"model"`
	Usage    *deepseekUsage `json:"usage"`
	Response *struct {
		Model string         `json:"model"`
		Usage *deepseekUsage `json:"usage"`
	} `json:"response"`
}

func (d *DeepSeekAdapter) url(path string) string {
	return strings.TrimRight(d.baseURL, "/") + path
}

func (d *DeepSeekAdapter) forwardNonStream(modelName string, body io.Reader, w http.ResponseWriter) (string, *pricing.Usage, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return modelName, nil, err
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)

	var parsed struct {
		Model string        `json:"model"`
		Usage deepseekUsage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return modelName, nil, fmt.Errorf("parse deepseek response: %w", err)
	}
	if parsed.Model != "" {
		modelName = parsed.Model
	}
	return modelName, normalizeDeepSeekUsage(parsed.Usage), nil
}

func (d *DeepSeekAdapter) forwardStream(modelName string, body io.Reader, w http.ResponseWriter) (string, *pricing.Usage, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)

	var usage *pricing.Usage
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
		if data == "[DONE]" {
			continue
		}
		var chunk struct {
			Model string         `json:"model"`
			Usage *deepseekUsage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Model != "" {
			modelName = chunk.Model
		}
		if chunk.Usage != nil {
			usage = normalizeDeepSeekUsage(*chunk.Usage)
		}
	}
	if err := scanner.Err(); err != nil {
		return modelName, usage, err
	}
	if usage == nil {
		return modelName, nil, fmt.Errorf("deepseek stream ended without usage")
	}
	return modelName, usage, nil
}

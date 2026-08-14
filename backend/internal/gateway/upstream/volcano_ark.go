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

const volcanoArkBaseURL = "https://ark.cn-beijing.volces.com/api/v3"

// VolcanoArkAdapter 转发到火山方舟(Ark)真实API，用的是我们自己唯一的官方Key。
// 请求/响应格式跟OpenAI Chat Completions基本一致，但usage里的缓存token是以
// prompt_tokens_details.cached_tokens 表示"命中缓存的那部分"(是prompt_tokens的子集)，
// 跟DeepSeek直接给hit/miss两个独立字段的写法不一样，归一化时要按这个减法算未命中数。
type VolcanoArkAdapter struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewVolcanoArk 构造火山方舟转发适配器。baseURL 为空用默认端点；client 为空自建（生产传共享 client）。
func NewVolcanoArk(apiKey, baseURL string, client *http.Client) *VolcanoArkAdapter {
	if baseURL == "" {
		baseURL = volcanoArkBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	return &VolcanoArkAdapter{apiKey: apiKey, baseURL: baseURL, httpClient: client}
}

func (v *VolcanoArkAdapter) Name() string { return "volcano_ark" }

type volcanoArkUsage struct {
	PromptTokens       int `json:"prompt_tokens"`
	CompletionTokens   int `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func normalizeVolcanoArkUsage(u volcanoArkUsage) *pricing.Usage {
	cached := 0
	if u.PromptTokensDetails != nil {
		cached = u.PromptTokensDetails.CachedTokens
	}
	reasoning := 0
	if u.CompletionTokensDetails != nil {
		reasoning = u.CompletionTokensDetails.ReasoningTokens
	}
	uncached := u.PromptTokens - cached
	if uncached < 0 {
		uncached = 0
	}
	return &pricing.Usage{
		CachedTokens:     cached,
		UncachedTokens:   uncached,
		CompletionTokens: u.CompletionTokens,
		ReasoningTokens:  reasoning,
	}
}

func (v *VolcanoArkAdapter) Forward(ctx context.Context, rawBody []byte, stream bool, w http.ResponseWriter) (string, *pricing.Usage, error) {
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return "", nil, fmt.Errorf("parse request body: %w", err)
	}
	modelName, _ := payload["model"].(string)

	if stream {
		payload["stream_options"] = map[string]any{"include_usage": true}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, fmt.Errorf("re-marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.apiKey)

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return modelName, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return modelName, nil, fmt.Errorf("volcano ark upstream error %d: %s", resp.StatusCode, string(respBody))
	}

	if stream {
		return v.forwardStream(modelName, resp.Body, w)
	}
	return v.forwardNonStream(modelName, resp.Body, w)
}

func (v *VolcanoArkAdapter) forwardNonStream(modelName string, body io.Reader, w http.ResponseWriter) (string, *pricing.Usage, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return modelName, nil, err
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)

	var parsed struct {
		Model string          `json:"model"`
		Usage volcanoArkUsage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return modelName, nil, fmt.Errorf("parse volcano ark response: %w", err)
	}
	if parsed.Model != "" {
		modelName = parsed.Model
	}
	return modelName, normalizeVolcanoArkUsage(parsed.Usage), nil
}

func (v *VolcanoArkAdapter) forwardStream(modelName string, body io.Reader, w http.ResponseWriter) (string, *pricing.Usage, error) {
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
			Model string           `json:"model"`
			Usage *volcanoArkUsage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Model != "" {
			modelName = chunk.Model
		}
		if chunk.Usage != nil {
			usage = normalizeVolcanoArkUsage(*chunk.Usage)
		}
	}
	if err := scanner.Err(); err != nil {
		return modelName, usage, err
	}
	if usage == nil {
		return modelName, nil, fmt.Errorf("volcano ark stream ended without usage")
	}
	return modelName, usage, nil
}

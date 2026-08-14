package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type OpenAIProvider struct {
	APIKey  string
	BaseURL string
	Model   string
}

func NewOpenAI(apiKey, baseURL, model string) *OpenAIProvider {
	return &OpenAIProvider{APIKey: apiKey, BaseURL: baseURL, Model: model}
}

func (p *OpenAIProvider) Name() string { return "openai" }

type openaiStreamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (p *OpenAIProvider) StreamChat(ctx context.Context, req *Request, callback StreamCallback) error {
	model := req.Model
	if model == "" {
		model = p.Model
	}

	body := map[string]interface{}{
		"model":    model,
		"messages": req.Messages,
		"stream":   true,
		"stream_options": map[string]interface{}{
			"include_usage": true,
		},
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}

	data, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai API error %d: %s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			callback(StreamChunk{IsFinish: true})
			return nil
		}

		var chunk openaiStreamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		sc := StreamChunk{}
		if len(chunk.Choices) > 0 {
			sc.DeltaContent = chunk.Choices[0].Delta.Content
			if chunk.Choices[0].FinishReason != nil {
				sc.IsFinish = true
			}
		}
		if chunk.Usage != nil {
			sc.PromptTokens = chunk.Usage.PromptTokens
			sc.CompletionTokens = chunk.Usage.CompletionTokens
		}
		callback(sc)
	}
	return scanner.Err()
}

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

type ClaudeProvider struct {
	APIKey  string
	BaseURL string
	Model   string
}

func NewClaude(apiKey, baseURL, model string) *ClaudeProvider {
	return &ClaudeProvider{APIKey: apiKey, BaseURL: baseURL, Model: model}
}

func (p *ClaudeProvider) Name() string { return "claude" }

func (p *ClaudeProvider) StreamChat(ctx context.Context, req *Request, callback StreamCallback) error {
	model := req.Model
	if model == "" {
		model = p.Model
	}

	// Convert messages to Claude format
	var system string
	claudeMessages := make([]map[string]string, 0)
	for _, m := range req.Messages {
		if m.Role == "system" {
			system = m.Content
			continue
		}
		claudeMessages = append(claudeMessages, map[string]string{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	body := map[string]interface{}{
		"model":      model,
		"messages":   claudeMessages,
		"max_tokens": 4096,
		"stream":     true,
	}
	if system != "" {
		body["system"] = system
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}

	data, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/v1/messages", bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("claude API error %d: %s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	var promptTokens, completionTokens int
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		switch eventType {
		case "content_block_delta":
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				text, _ := delta["text"].(string)
				callback(StreamChunk{DeltaContent: text})
			}
		case "message_delta":
			if usage, ok := event["usage"].(map[string]interface{}); ok {
				if v, ok := usage["output_tokens"].(float64); ok {
					completionTokens = int(v)
				}
			}
		case "message_start":
			if msg, ok := event["message"].(map[string]interface{}); ok {
				if usage, ok := msg["usage"].(map[string]interface{}); ok {
					if v, ok := usage["input_tokens"].(float64); ok {
						promptTokens = int(v)
					}
				}
			}
		case "message_stop":
			callback(StreamChunk{
				IsFinish:         true,
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
			})
			return nil
		}
	}
	return scanner.Err()
}

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

// LocalProvider implements the Provider interface for local LLM servers (Ollama-compatible).
type LocalProvider struct {
	Endpoint  string // e.g. "http://localhost:11434"
	ModelName string // e.g. "llama3"
}

func NewLocalProvider(endpoint, modelName string) *LocalProvider {
	return &LocalProvider{Endpoint: endpoint, ModelName: modelName}
}

func (p *LocalProvider) Name() string { return "local" }

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done               bool `json:"done"`
	PromptEvalCount    int  `json:"prompt_eval_count"`
	EvalCount          int  `json:"eval_count"`
}

func (p *LocalProvider) StreamChat(ctx context.Context, req *Request, callback StreamCallback) error {
	modelName := req.Model
	if modelName == "" {
		modelName = p.ModelName
	}

	ollamaMsgs := make([]ollamaMessage, len(req.Messages))
	for i, m := range req.Messages {
		ollamaMsgs[i] = ollamaMessage{Role: m.Role, Content: m.Content}
	}

	body := ollamaChatRequest{
		Model:    modelName,
		Messages: ollamaMsgs,
		Stream:   req.Stream,
	}

	data, _ := json.Marshal(body)

	endpoint := strings.TrimRight(p.Endpoint, "/")
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("local LLM API error %d: %s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var chunk ollamaChatResponse
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}

		sc := StreamChunk{
			DeltaContent: chunk.Message.Content,
		}

		if chunk.Done {
			sc.IsFinish = true
			sc.PromptTokens = chunk.PromptEvalCount
			sc.CompletionTokens = chunk.EvalCount
		}

		callback(sc)

		if chunk.Done {
			return nil
		}
	}
	return scanner.Err()
}

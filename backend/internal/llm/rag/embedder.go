package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/askie/grix/backend/config"
)

// EmbeddingResult contains the embedding vector and token count.
type EmbeddingResult struct {
	Embedding []float32
	Tokens    int
}

// GenerateEmbedding calls the OpenAI Embedding API.
func GenerateEmbedding(ctx context.Context, text string) (*EmbeddingResult, error) {
	cfg := config.C.LLM.OpenAI

	body := map[string]interface{}{
		"model": config.C.LLM.EmbeddingModel,
		"input": text,
	}
	data, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.BaseURL+"/embeddings", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}

	return &EmbeddingResult{
		Embedding: result.Data[0].Embedding,
		Tokens:    result.Usage.TotalTokens,
	}, nil
}

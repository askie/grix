package provider

import "context"

// StreamCallback is called for each chunk from the LLM.
type StreamCallback func(chunk StreamChunk)

type StreamChunk struct {
	DeltaContent string
	IsFinish     bool
	PromptTokens int
	CompletionTokens int
	Error        error
}

// Message represents a chat message for the LLM.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request contains the LLM request parameters.
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream"`
}

// Provider is the interface for LLM providers.
type Provider interface {
	Name() string
	StreamChat(ctx context.Context, req *Request, callback StreamCallback) error
}

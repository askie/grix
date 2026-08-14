package llmclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
)

const (
	defaultRequestTimeout = 60 * time.Second
	apiStyleResponses     = "responses"
	apiStyleChat          = "chat_completions"
)

type Config struct {
	APIKey           string
	BaseURL          string
	ProxyURL         string
	APIStyle         string
	DefaultModel     string
	ReasoningEffort  string
	ExtraBodyJSON    string
	ExtraHeadersJSON string
	RequestTimeout   time.Duration
}

type JSONRequest struct {
	Model           string
	Instructions    string
	Input           string
	SchemaName      string
	Schema          map[string]any
	Temperature     *float64
	MaxOutputTokens int64
}

type JSONResponse struct {
	ResponseID     string
	Model          string
	OutputText     string
	InputTokens    int64
	OutputTokens   int64
	TotalTokens    int64
	ResponseStatus string
}

type Client struct {
	client          openai.Client
	apiStyle        string
	defaultModel    string
	reasoningEffort string
	extraBody       map[string]any
	extraHeaders    map[string]string
}

func New(cfg Config) (*Client, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("llm api key is required")
	}

	apiStyle, err := normalizeAPIStyle(cfg.APIStyle)
	if err != nil {
		return nil, err
	}

	extraBody, err := parseJSONObject(strings.TrimSpace(cfg.ExtraBodyJSON))
	if err != nil {
		return nil, fmt.Errorf("parse llm extra body json: %w", err)
	}

	extraHeaders, err := parseHeaderJSONObject(strings.TrimSpace(cfg.ExtraHeadersJSON))
	if err != nil {
		return nil, fmt.Errorf("parse llm extra headers json: %w", err)
	}

	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}

	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL := strings.TrimSpace(cfg.BaseURL); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	httpClient := &http.Client{Timeout: timeout}
	if proxyURL := strings.TrimSpace(cfg.ProxyURL); proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse proxy url: %w", err)
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyURL(parsed)
		httpClient.Transport = transport
	}
	opts = append(opts, option.WithHTTPClient(httpClient))

	return &Client{
		client:          openai.NewClient(opts...),
		apiStyle:        apiStyle,
		defaultModel:    strings.TrimSpace(cfg.DefaultModel),
		reasoningEffort: strings.TrimSpace(cfg.ReasoningEffort),
		extraBody:       extraBody,
		extraHeaders:    extraHeaders,
	}, nil
}

func (c *Client) GenerateJSON(ctx context.Context, req JSONRequest, dest any) (*JSONResponse, error) {
	if c == nil {
		return nil, errors.New("llm client is nil")
	}
	if dest == nil {
		return nil, errors.New("llm json destination is nil")
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = c.defaultModel
	}
	if model == "" {
		return nil, errors.New("llm model is required")
	}

	instructions := strings.TrimSpace(req.Instructions)
	if instructions == "" {
		return nil, errors.New("llm instructions are required")
	}
	input := strings.TrimSpace(req.Input)
	if input == "" {
		return nil, errors.New("llm input is required")
	}
	schemaName := strings.TrimSpace(req.SchemaName)
	if schemaName == "" {
		return nil, errors.New("llm schema name is required")
	}
	if len(req.Schema) == 0 {
		return nil, errors.New("llm schema is required")
	}

	switch c.apiStyle {
	case apiStyleChat:
		return c.generateJSONWithChatCompletions(ctx, model, instructions, input, schemaName, req, dest)
	default:
		return c.generateJSONWithResponses(ctx, model, instructions, input, schemaName, req, dest)
	}
}

func (c *Client) generateJSONWithResponses(
	ctx context.Context,
	model string,
	instructions string,
	input string,
	schemaName string,
	req JSONRequest,
	dest any,
) (*JSONResponse, error) {
	params := responses.ResponseNewParams{
		Model:        openai.ResponsesModel(model),
		Instructions: openai.String(instructions),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(input),
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigParamOfJSONSchema(schemaName, req.Schema),
		},
	}
	if req.MaxOutputTokens > 0 {
		params.MaxOutputTokens = openai.Int(req.MaxOutputTokens)
	}
	if req.Temperature != nil {
		params.Temperature = openai.Float(*req.Temperature)
	}
	if effort := strings.TrimSpace(c.reasoningEffort); effort != "" {
		params.Reasoning = shared.ReasoningParam{
			Effort: shared.ReasoningEffort(effort),
		}
	}

	resp, err := c.client.Responses.New(ctx, params, c.requestOptions()...)
	if err != nil {
		return nil, err
	}

	outputText := strings.TrimSpace(resp.OutputText())
	if outputText == "" {
		return nil, fmt.Errorf("llm returned empty output with status=%s", resp.Status)
	}
	if err := json.Unmarshal([]byte(outputText), dest); err != nil {
		return nil, fmt.Errorf("decode llm json output: %w", err)
	}

	return &JSONResponse{
		ResponseID:     resp.ID,
		Model:          string(resp.Model),
		OutputText:     outputText,
		InputTokens:    resp.Usage.InputTokens,
		OutputTokens:   resp.Usage.OutputTokens,
		TotalTokens:    resp.Usage.TotalTokens,
		ResponseStatus: string(resp.Status),
	}, nil
}

func (c *Client) generateJSONWithChatCompletions(
	ctx context.Context,
	model string,
	instructions string,
	input string,
	schemaName string,
	req JSONRequest,
	dest any,
) (*JSONResponse, error) {
	params := openai.ChatCompletionNewParams{
		Model: shared.ChatModel(model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(buildChatCompletionInstructions(instructions, schemaName, req.Schema)),
			openai.UserMessage(input),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{
				Type: "json_object",
			},
		},
	}
	if req.MaxOutputTokens > 0 {
		params.MaxTokens = openai.Int(req.MaxOutputTokens)
	}
	if req.Temperature != nil {
		params.Temperature = openai.Float(*req.Temperature)
	}
	if effort := strings.TrimSpace(c.reasoningEffort); effort != "" {
		params.ReasoningEffort = shared.ReasoningEffort(effort)
	}

	resp, err := c.client.Chat.Completions.New(ctx, params, c.requestOptions()...)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("llm returned no chat completion choices")
	}

	outputText := strings.TrimSpace(resp.Choices[0].Message.Content)
	if outputText == "" {
		return nil, fmt.Errorf("llm returned empty chat completion content with finish_reason=%s", resp.Choices[0].FinishReason)
	}
	if err := json.Unmarshal([]byte(outputText), dest); err != nil {
		return nil, fmt.Errorf("decode llm json output: %w", err)
	}

	return &JSONResponse{
		ResponseID:     resp.ID,
		Model:          resp.Model,
		OutputText:     outputText,
		InputTokens:    resp.Usage.PromptTokens,
		OutputTokens:   resp.Usage.CompletionTokens,
		TotalTokens:    resp.Usage.TotalTokens,
		ResponseStatus: strings.TrimSpace(resp.Choices[0].FinishReason),
	}, nil
}

func (c *Client) requestOptions() []option.RequestOption {
	opts := make([]option.RequestOption, 0, len(c.extraHeaders)+len(c.extraBody))
	for key, value := range c.extraHeaders {
		opts = append(opts, option.WithHeader(key, value))
	}
	for key, value := range c.extraBody {
		opts = append(opts, option.WithJSONSet(key, value))
	}
	return opts
}

func buildChatCompletionInstructions(instructions string, schemaName string, schema map[string]any) string {
	return strings.TrimSpace(fmt.Sprintf(
		"%s\n\nReturn only a JSON object that matches the schema named %q. Schema:\n%s",
		instructions,
		schemaName,
		mustMarshalJSON(schema),
	))
}

func normalizeAPIStyle(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", apiStyleResponses:
		return apiStyleResponses, nil
	case apiStyleChat, "chat-completions", "chat/completions":
		return apiStyleChat, nil
	default:
		return "", fmt.Errorf("unsupported llm api style: %s", value)
	}
}

func parseJSONObject(raw string) (map[string]any, error) {
	if raw == "" {
		return nil, nil
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, err
	}
	if decoded == nil {
		return map[string]any{}, nil
	}
	return decoded, nil
}

func parseHeaderJSONObject(raw string) (map[string]string, error) {
	if raw == "" {
		return nil, nil
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, err
	}

	headers := make(map[string]string, len(decoded))
	for key, value := range decoded {
		strValue, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("header %s must be a string", key)
		}
		headers[key] = strValue
	}
	return headers, nil
}

func mustMarshalJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

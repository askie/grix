package responsesbridge

import (
	"encoding/json"
	"fmt"
	"time"
)

// ConvertResponse 把 Chat Completions 非流式响应转成 Responses API 响应。
func ConvertResponse(chatRaw []byte, responseID, model string) ([]byte, error) {
	var chat map[string]json.RawMessage
	if err := json.Unmarshal(chatRaw, &chat); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}

	// 上游错误体透传成 Responses error 形态。
	if errRaw, ok := chat["error"]; ok && string(errRaw) != "null" {
		var errObj map[string]any
		_ = json.Unmarshal(errRaw, &errObj)
		out := map[string]any{
			"id":         responseID,
			"object":     "response",
			"created_at": time.Now().Unix(),
			"status":     "failed",
			"model":      model,
			"error":      errObj,
			"output":     []any{},
		}
		return json.Marshal(out)
	}

	var choices []map[string]json.RawMessage
	_ = json.Unmarshal(chat["choices"], &choices)

	output := make([]any, 0)
	status := "completed"
	var incomplete any

	for _, choice := range choices {
		var message map[string]json.RawMessage
		_ = json.Unmarshal(choice["message"], &message)

		finish := decodeString(choice["finish_reason"])
		switch finish {
		case "length":
			status = "incomplete"
			incomplete = map[string]any{"reason": "max_output_tokens"}
		case "content_filter":
			status = "incomplete"
			incomplete = map[string]any{"reason": "content_filter"}
		}

		if reasoning := decodeString(message["reasoning_content"]); reasoning != "" {
			output = append(output, map[string]any{
				"type":   "reasoning",
				"id":     "rs_" + responseID,
				"summary": []any{},
				"content": []map[string]any{
					{"type": "reasoning_text", "text": reasoning},
				},
			})
		}

		if toolCallsRaw, ok := message["tool_calls"]; ok && string(toolCallsRaw) != "null" {
			var toolCalls []map[string]json.RawMessage
			_ = json.Unmarshal(toolCallsRaw, &toolCalls)
			for _, tc := range toolCalls {
				var fn map[string]json.RawMessage
				_ = json.Unmarshal(tc["function"], &fn)
				callID := decodeString(tc["id"])
				output = append(output, map[string]any{
					"type":      "function_call",
					"id":        "fc_" + callID,
					"call_id":   callID,
					"name":      decodeString(fn["name"]),
					"arguments": decodeString(fn["arguments"]),
					"status":    "completed",
				})
			}
		}

		content := decodeString(message["content"])
		refusal := decodeString(message["refusal"])
		if content != "" || refusal != "" {
			var blocks []map[string]any
			if refusal != "" {
				blocks = append(blocks, map[string]any{"type": "refusal", "refusal": refusal})
			}
			if content != "" {
				blocks = append(blocks, map[string]any{
					"type":        "output_text",
					"text":        content,
					"annotations": []any{},
				})
			}
			itemStatus := "completed"
			if status == "incomplete" {
				itemStatus = "incomplete"
			}
			output = append(output, map[string]any{
				"type":    "message",
				"id":      "msg_" + responseID,
				"role":    "assistant",
				"status":  itemStatus,
				"content": blocks,
			})
		}
	}

	created := time.Now().Unix()
	if c, ok := chat["created"]; ok {
		var n int64
		if json.Unmarshal(c, &n) == nil && n > 0 {
			created = n
		}
	}
	if echoed := decodeString(chat["model"]); echoed != "" {
		model = echoed
	}

	out := map[string]any{
		"id":         responseID,
		"object":     "response",
		"created_at": created,
		"status":     status,
		"model":      model,
		"output":     output,
		"usage":      convertUsage(chat["usage"]),
	}
	if incomplete != nil {
		out["incomplete_details"] = incomplete
	}
	return json.Marshal(out)
}

func convertUsage(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var u map[string]json.RawMessage
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil
	}
	prompt := decodeInt(u["prompt_tokens"])
	completion := decodeInt(u["completion_tokens"])
	total := decodeInt(u["total_tokens"])
	if total == 0 {
		total = prompt + completion
	}

	cached := 0
	if details, ok := u["prompt_tokens_details"]; ok {
		var d map[string]json.RawMessage
		if json.Unmarshal(details, &d) == nil {
			cached = decodeInt(d["cached_tokens"])
		}
	}
	reasoning := 0
	if details, ok := u["completion_tokens_details"]; ok {
		var d map[string]json.RawMessage
		if json.Unmarshal(details, &d) == nil {
			reasoning = decodeInt(d["reasoning_tokens"])
		}
	}

	return map[string]any{
		"input_tokens":  prompt,
		"output_tokens": completion,
		"total_tokens":  total,
		"input_tokens_details": map[string]any{
			"cached_tokens": cached,
		},
		"output_tokens_details": map[string]any{
			"reasoning_tokens": reasoning,
		},
	}
}

func decodeInt(raw json.RawMessage) int {
	var n int
	_ = json.Unmarshal(raw, &n)
	return n
}

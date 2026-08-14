// Package responsesbridge 把 OpenAI Responses API 请求/响应转成 Chat Completions，
// 再转回 Responses——上游厂商（DeepSeek / 豆包）只认 chat/completions，
// 而 Codex 等新客户端只打 /v1/responses。
package responsesbridge

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ConvertRequest 把 Responses API 请求体转成 Chat Completions 请求体。
// 不支持 previous_response_id / conversation / prompt 模板（需服务端状态）；
// Codex 默认 store=false 且每次带全量 input，足够覆盖主路径。
func ConvertRequest(raw []byte) (chatBody []byte, model string, stream bool, err error) {
	var req map[string]json.RawMessage
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, "", false, fmt.Errorf("decode responses request: %w", err)
	}

	out := map[string]any{}

	model = decodeString(req["model"])
	if model == "" {
		return nil, "", false, fmt.Errorf("model is required")
	}
	out["model"] = model

	stream = decodeBool(req["stream"])
	out["stream"] = stream
	if stream {
		out["stream_options"] = map[string]any{"include_usage": true}
	}

	if v, ok := req["temperature"]; ok {
		out["temperature"] = rawJSON(v)
	}
	if v, ok := req["top_p"]; ok {
		out["top_p"] = rawJSON(v)
	}
	if v, ok := req["max_output_tokens"]; ok {
		out["max_tokens"] = rawJSON(v)
	}
	if v, ok := req["parallel_tool_calls"]; ok {
		out["parallel_tool_calls"] = rawJSON(v)
	}
	if v, ok := req["user"]; ok {
		out["user"] = rawJSON(v)
	}

	if effort := reasoningEffort(req["reasoning"]); effort != "" {
		out["reasoning_effort"] = effort
	}

	if rf := responseFormat(req["text"]); rf != nil {
		out["response_format"] = rf
	}

	messages, err := buildMessages(req)
	if err != nil {
		return nil, "", false, err
	}
	out["messages"] = messages

	if tools, ok := req["tools"]; ok && string(tools) != "null" {
		converted, err := convertTools(tools)
		if err != nil {
			return nil, "", false, err
		}
		if len(converted) > 0 {
			out["tools"] = converted
		}
	}
	if tc, ok := req["tool_choice"]; ok && string(tc) != "null" {
		out["tool_choice"] = convertToolChoice(tc)
	}

	body, err := json.Marshal(out)
	if err != nil {
		return nil, "", false, err
	}
	return body, model, stream, nil
}

func buildMessages(req map[string]json.RawMessage) ([]map[string]any, error) {
	var messages []map[string]any

	if instructions := decodeString(req["instructions"]); instructions != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": instructions,
		})
	}

	inputRaw, ok := req["input"]
	if !ok || string(inputRaw) == "null" {
		if len(messages) == 0 {
			return nil, fmt.Errorf("input is required")
		}
		return messages, nil
	}

	// input 可以是 string 或 array
	var asString string
	if err := json.Unmarshal(inputRaw, &asString); err == nil {
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": asString,
		})
		return messages, nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(inputRaw, &items); err != nil {
		return nil, fmt.Errorf("decode input: %w", err)
	}

	var pendingToolCalls []map[string]any
	var deferred []map[string]any

	flushTools := func() {
		if len(pendingToolCalls) > 0 {
			messages = append(messages, map[string]any{
				"role":       "assistant",
				"content":    nil,
				"tool_calls": pendingToolCalls,
			})
			pendingToolCalls = nil
		}
		if len(deferred) > 0 {
			messages = append(messages, deferred...)
			deferred = nil
		}
	}

	for _, rawItem := range items {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(rawItem, &item); err != nil {
			continue
		}
		typ := decodeString(item["type"])
		if typ == "" {
			// 裸 message：有 role 就当 message
			if _, hasRole := item["role"]; hasRole {
				typ = "message"
			}
		}

		switch typ {
		case "message":
			flushTools()
			msg, ok := convertInputMessage(item)
			if ok {
				messages = append(messages, msg)
			}
		case "function_call", "custom_tool_call":
			tc := map[string]any{
				"id":   firstNonEmpty(decodeString(item["call_id"]), decodeString(item["id"])),
				"type": "function",
				"function": map[string]any{
					"name":      decodeString(item["name"]),
					"arguments": firstNonEmpty(decodeString(item["arguments"]), decodeString(item["input"])),
				},
			}
			pendingToolCalls = append(pendingToolCalls, tc)
		case "function_call_output", "custom_tool_call_output":
			deferred = append(deferred, map[string]any{
				"role":         "tool",
				"tool_call_id": decodeString(item["call_id"]),
				"content":      extractOutputText(item["output"]),
			})
		case "reasoning", "compaction", "compaction_trigger":
			// 上游 chat 用不了这些 Responses 专有项；跳过以免污染 messages。
			continue
		default:
			// 未知类型尽量忽略，保持主链路可用。
			continue
		}
	}
	flushTools()
	return messages, nil
}

func convertInputMessage(item map[string]json.RawMessage) (map[string]any, bool) {
	role := decodeString(item["role"])
	switch role {
	case "developer":
		role = "system"
	case "system", "user", "assistant", "tool":
	default:
		if role == "" {
			role = "user"
		}
	}
	content := normalizeContent(item["content"], role)
	msg := map[string]any{"role": role, "content": content}
	if id := decodeString(item["tool_call_id"]); id != "" {
		msg["tool_call_id"] = id
	}
	return msg, true
}

func normalizeContent(raw json.RawMessage, role string) any {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return jsonText(raw)
	}
	var texts []string
	var chatParts []map[string]any
	for _, p := range parts {
		typ := decodeString(p["type"])
		switch typ {
		case "input_text", "output_text", "text":
			texts = append(texts, decodeString(p["text"]))
			chatParts = append(chatParts, map[string]any{
				"type": "text",
				"text": decodeString(p["text"]),
			})
		case "input_image", "image_url":
			url := ""
			if img, ok := p["image_url"]; ok {
				var obj map[string]any
				if json.Unmarshal(img, &obj) == nil {
					url, _ = obj["url"].(string)
				}
				if url == "" {
					url = decodeString(img)
				}
			}
			if url == "" {
				url = decodeString(p["image_url"])
			}
			chatParts = append(chatParts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": url},
			})
		case "refusal":
			texts = append(texts, decodeString(p["refusal"]))
		}
	}
	// 纯文本多段合并成 string，兼容性最好；含图片时保留多模态 parts。
	hasImage := false
	for _, p := range chatParts {
		if p["type"] == "image_url" {
			hasImage = true
			break
		}
	}
	if !hasImage {
		return joinTextParts(texts)
	}
	_ = role
	return chatParts
}

// joinTextParts 合并完整文本块：块之间补一个换行，避免多块文本塌成一行；
// 已以换行收尾或开头的块保持原样，不重复加空行。
func joinTextParts(parts []string) string {
	var b strings.Builder
	lastByte := byte(0)
	for _, p := range parts {
		if p == "" {
			continue
		}
		if b.Len() > 0 &&
			lastByte != '\n' && lastByte != '\r' &&
			p[0] != '\n' && p[0] != '\r' {
			b.WriteByte('\n')
		}
		b.WriteString(p)
		lastByte = p[len(p)-1]
	}
	return b.String()
}

func convertTools(raw json.RawMessage) ([]map[string]any, error) {
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, fmt.Errorf("decode tools: %w", err)
	}
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		typ := decodeString(t["type"])
		switch typ {
		case "function":
			// Responses: {type,name,description,parameters}
			// Chat:      {type,function:{name,description,parameters}}
			if _, hasFn := t["function"]; hasFn {
				var passthrough map[string]any
				_ = json.Unmarshal(mustMarshal(t), &passthrough)
				out = append(out, passthrough)
				continue
			}
			fn := map[string]any{
				"name": decodeString(t["name"]),
			}
			if d := decodeString(t["description"]); d != "" {
				fn["description"] = d
			}
			if p, ok := t["parameters"]; ok {
				fn["parameters"] = rawJSON(p)
			}
			out = append(out, map[string]any{"type": "function", "function": fn})
		case "custom":
			// 降级成 function，尽量让上游能看到名字。
			fn := map[string]any{
				"name":       decodeString(t["name"]),
				"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
			}
			if d := decodeString(t["description"]); d != "" {
				fn["description"] = d
			}
			out = append(out, map[string]any{"type": "function", "function": fn})
		default:
			// web_search 等内置工具上游不认，跳过。
			continue
		}
	}
	return out, nil
}

func convertToolChoice(raw json.RawMessage) any {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return rawJSON(raw)
	}
	typ := decodeString(obj["type"])
	if typ == "function" {
		name := decodeString(obj["name"])
		if name == "" {
			if fn, ok := obj["function"]; ok {
				var f map[string]json.RawMessage
				_ = json.Unmarshal(fn, &f)
				name = decodeString(f["name"])
			}
		}
		return map[string]any{
			"type":     "function",
			"function": map[string]any{"name": name},
		}
	}
	return rawJSON(raw)
}

func responseFormat(textRaw json.RawMessage) any {
	if len(textRaw) == 0 || string(textRaw) == "null" {
		return nil
	}
	var text map[string]json.RawMessage
	if err := json.Unmarshal(textRaw, &text); err != nil {
		return nil
	}
	formatRaw, ok := text["format"]
	if !ok {
		return nil
	}
	var format map[string]json.RawMessage
	if err := json.Unmarshal(formatRaw, &format); err != nil {
		return nil
	}
	typ := decodeString(format["type"])
	switch typ {
	case "json_schema":
		schema := map[string]any{
			"name":   decodeString(format["name"]),
			"schema": rawJSON(format["schema"]),
		}
		if d := decodeString(format["description"]); d != "" {
			schema["description"] = d
		}
		if _, ok := format["strict"]; ok {
			schema["strict"] = rawJSON(format["strict"])
		}
		return map[string]any{"type": "json_schema", "json_schema": schema}
	case "json_object":
		return map[string]any{"type": "json_object"}
	case "text":
		return map[string]any{"type": "text"}
	default:
		return nil
	}
}

func reasoningEffort(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	return decodeString(obj["effort"])
}

func extractOutputText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return jsonText(raw)
	}
	var texts []string
	for _, p := range parts {
		typ := decodeString(p["type"])
		if typ == "" || typ == "input_text" || typ == "output_text" || typ == "text" {
			texts = append(texts, decodeString(p["text"]))
		}
	}
	return joinTextParts(texts)
}

func decodeString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

func decodeBool(raw json.RawMessage) bool {
	var b bool
	_ = json.Unmarshal(raw, &b)
	return b
}

func rawJSON(raw json.RawMessage) any {
	var v any
	_ = json.Unmarshal(raw, &v)
	return v
}

func jsonText(raw json.RawMessage) string {
	return string(raw)
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

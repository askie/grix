package upstream

import (
	"bytes"
	"encoding/json"
)

// rewriteToolDefinitionReferences 把 Claude Code 历史消息中的内联工具定义以 defer_loading
// 提升到 tools，并把原位置换成 DeepSeek Anthropic 兼容端点要求的 tool_reference。
// 这是有损兼容：会改变 tools 前缀/cache，同名历史定义只保留最新版本。
func rewriteToolDefinitionReferences(rawBody []byte) []byte {
	if !bytes.Contains(rawBody, []byte(`"tool_definition"`)) {
		return rawBody
	}

	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.UseNumber()
	var body map[string]any
	if err := decoder.Decode(&body); err != nil {
		return rawBody
	}
	messages, ok := body["messages"].([]any)
	if !ok {
		return rawBody
	}

	var tools []any
	if existing, exists := body["tools"]; exists {
		tools, ok = existing.([]any)
		if !ok {
			return rawBody
		}
	}
	existingNames := make(map[string]struct{}, len(tools))
	hasNonDeferredTool := false
	for _, item := range tools {
		if tool, ok := item.(map[string]any); ok {
			if tool["defer_loading"] != true {
				hasNonDeferredTool = true
			}
			if name, ok := tool["name"].(string); ok && name != "" {
				existingNames[name] = struct{}{}
			}
		}
	}
	if !hasNonDeferredTool {
		return rawBody
	}

	appendedIndexes := make(map[string]int)
	changed := false
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			continue
		}
		content, ok := message["content"].([]any)
		if !ok {
			continue
		}
		for i, rawBlock := range content {
			block, ok := rawBlock.(map[string]any)
			if !ok || block["type"] != "tool_addition" {
				continue
			}
			tool, ok := block["tool"].(map[string]any)
			if !ok || tool["type"] != "tool_definition" {
				continue
			}
			definition, ok := tool["definition"].(map[string]any)
			if !ok {
				continue
			}
			name, ok := definition["name"].(string)
			if !ok || name == "" {
				continue
			}

			if index, exists := appendedIndexes[name]; exists {
				definition["defer_loading"] = true
				tools[index] = definition
			} else if _, exists := existingNames[name]; !exists {
				definition["defer_loading"] = true
				appendedIndexes[name] = len(tools)
				tools = append(tools, definition)
			}
			block["tool"] = map[string]any{
				"type": "tool_reference",
				"name": name,
			}
			content[i] = block
			changed = true
		}
	}
	if !changed {
		return rawBody
	}
	body["tools"] = tools
	rewritten, err := json.Marshal(body)
	if err != nil {
		return rawBody
	}
	return rewritten
}

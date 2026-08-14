package agentapi

import (
	"fmt"
	"sort"
	"strings"
)

func buildClaudeRequestedSchemaQuestions(raw any) ([]map[string]any, bool) {
	schema, ok := raw.(map[string]interface{})
	if !ok || len(schema) == 0 {
		return nil, false
	}

	properties, ok := schema["properties"].(map[string]interface{})
	if !ok || len(properties) == 0 {
		return nil, false
	}

	requiredKeys := normalizeClaudeSchemaStringSlice(schema["required"])
	if len(requiredKeys) != len(properties) {
		return nil, false
	}
	requiredSet := make(map[string]struct{}, len(requiredKeys))
	for _, key := range requiredKeys {
		requiredSet[key] = struct{}{}
	}

	propertyKeys := make([]string, 0, len(properties))
	for key := range properties {
		if _, ok := requiredSet[key]; !ok {
			return nil, false
		}
		propertyKeys = append(propertyKeys, key)
	}
	sort.Strings(propertyKeys)

	questions := make([]map[string]any, 0, len(propertyKeys))
	for index, key := range propertyKeys {
		question, ok := buildClaudeRequestedSchemaQuestion(key, properties[key], index+1)
		if !ok {
			return nil, false
		}
		questions = append(questions, question)
	}
	return questions, len(questions) > 0
}

func buildClaudeRequestedSchemaQuestion(key string, raw any, index int) (map[string]any, bool) {
	property, ok := raw.(map[string]interface{})
	if !ok {
		return nil, false
	}

	header := normalizeClaudeQuestionText(property["title"])
	if header == "" {
		header = strings.TrimSpace(key)
	}
	if header == "" {
		return nil, false
	}

	itemType := normalizeClaudeSchemaType(property["type"])
	question := map[string]any{
		"index":     index,
		"header":    header,
		"field_key": strings.TrimSpace(key),
	}
	description := normalizeClaudeQuestionText(property["description"])

	if options, ok := normalizeClaudeSchemaEnumOptions(property["enum"]); ok {
		question["prompt"] = buildClaudeRequestedSchemaPrompt(header, description, "enum")
		question["options"] = options
		return question, true
	}

	switch itemType {
	case "", "string", "number", "integer":
		question["prompt"] = buildClaudeRequestedSchemaPrompt(header, description, itemType)
		return question, true
	case "boolean":
		question["prompt"] = buildClaudeRequestedSchemaPrompt(header, description, "boolean")
		question["options"] = []string{"yes", "no"}
		return question, true
	case "array":
		items, ok := property["items"].(map[string]interface{})
		if !ok {
			return nil, false
		}
		if options, ok := normalizeClaudeSchemaEnumOptions(items["enum"]); ok {
			question["prompt"] = buildClaudeRequestedSchemaPrompt(header, description, "enum_array")
			question["options"] = options
			question["multi_select"] = true
			return question, true
		}
		itemSchemaType := normalizeClaudeSchemaType(items["type"])
		if itemSchemaType == "" || itemSchemaType == "string" {
			question["prompt"] = buildClaudeRequestedSchemaPrompt(header, description, "string_array")
			question["multi_select"] = true
			return question, true
		}
		return nil, false
	default:
		return nil, false
	}
}

func normalizeClaudeSchemaType(raw any) string {
	return strings.ToLower(strings.TrimSpace(fmt.Sprint(raw)))
}

func normalizeClaudeSchemaStringSlice(raw any) []string {
	switch typed := raw.(type) {
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			value := strings.TrimSpace(fmt.Sprint(item))
			if value != "" {
				values = append(values, value)
			}
		}
		return values
	case []string:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			value := strings.TrimSpace(item)
			if value != "" {
				values = append(values, value)
			}
		}
		return values
	default:
		return nil
	}
}

func normalizeClaudeSchemaEnumOptions(raw any) ([]string, bool) {
	options := normalizeClaudeSchemaStringSlice(raw)
	if len(options) == 0 {
		return nil, false
	}
	return options, true
}

func buildClaudeRequestedSchemaPrompt(title, description, itemType string) string {
	lines := make([]string, 0, 2)
	if description = strings.TrimSpace(description); description != "" {
		lines = append(lines, description)
	}

	typeHint := map[string]string{
		"string":     "Enter text.",
		"number":     "Enter a number.",
		"integer":    "Enter an integer.",
		"boolean":    "Choose yes or no.",
		"enum":       "Choose one of the listed options.",
		"string_arr": "Enter one or more values, separated by commas.",
		"enum_arr":   "Choose one or more of the listed options.",
	}[normalizeClaudePromptType(itemType)]
	if typeHint != "" {
		lines = append(lines, typeHint)
	}

	prompt := strings.TrimSpace(strings.Join(lines, " "))
	if prompt != "" {
		return prompt
	}
	if strings.TrimSpace(title) == "" {
		return "Provide a value."
	}
	return fmt.Sprintf("Provide %s.", strings.TrimSpace(title))
}

func normalizeClaudePromptType(itemType string) string {
	switch itemType {
	case "", "string":
		return "string"
	case "number":
		return "number"
	case "integer":
		return "integer"
	case "boolean":
		return "boolean"
	case "enum":
		return "enum"
	case "string_array":
		return "string_arr"
	case "enum_array":
		return "enum_arr"
	default:
		return itemType
	}
}

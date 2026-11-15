package upstream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const toolDefinitionRequest = `{"model":"deepseek-chat","max_tokens":8,"tools":[{"name":"support_tool","description":"non-deferred support","input_schema":{"type":"object"},"defer_loading":false}],"messages":[{"role":"system","content":[{"type":"text","text":"tools changed"},{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"Artifact","description":"render artifacts","input_schema":{"type":"object"}}}},{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"Workflow","description":"run workflows","input_schema":{"type":"object","properties":{"input":{"type":"string"}}}}}}]}]}`

func decodeToolTestObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	return body
}

func toolTestMessage(t *testing.T, body map[string]any, index int) map[string]any {
	t.Helper()
	messages := body["messages"].([]any)
	return messages[index].(map[string]any)
}

func toolTestContent(t *testing.T, message map[string]any, index int) map[string]any {
	t.Helper()
	content := message["content"].([]any)
	return content[index].(map[string]any)
}

func TestAnthropicForwardRewritesToolDefinitions(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"model":"deepseek-flash","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", srv.URL, nil).WithToolDefinitionReferences()
	rec := httptest.NewRecorder()
	_, _, err := a.Forward(context.Background(), []byte(toolDefinitionRequest), false, rec)
	require.NoError(t, err)

	body := decodeToolTestObject(t, gotBody)
	message := toolTestMessage(t, body, 0)
	assert.Equal(t, map[string]any{"type": "tool_reference", "name": "Artifact"}, toolTestContent(t, message, 1)["tool"])
	assert.Equal(t, map[string]any{"type": "tool_reference", "name": "Workflow"}, toolTestContent(t, message, 2)["tool"])

	tools := body["tools"].([]any)
	require.Len(t, tools, 3)
	artifact := tools[1].(map[string]any)
	workflow := tools[2].(map[string]any)
	assert.Equal(t, "Artifact", artifact["name"])
	assert.Equal(t, "render artifacts", artifact["description"])
	assert.Equal(t, true, artifact["defer_loading"])
	assert.Equal(t, "Workflow", workflow["name"])
	assert.Equal(t, "run workflows", workflow["description"])
	assert.Equal(t, true, workflow["defer_loading"])
	assert.NotContains(t, string(gotBody), "tool_definition")
}

func TestAnthropicForwardWithoutCompatibilityKeepsToolDefinitions(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"model":"deepseek-flash","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", srv.URL, nil)
	rec := httptest.NewRecorder()
	_, _, err := a.Forward(context.Background(), []byte(toolDefinitionRequest), false, rec)
	require.NoError(t, err)
	assert.JSONEq(t, toolDefinitionRequest, string(gotBody))
}

func TestRewriteToolDefinitionReferencesDeduplicatesAndPreservesExistingTools(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"Workflow","description":"old"}}},{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"Workflow","description":"latest"}}},{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"Artifact","description":"new artifact"}}}]}],"tools":[{"name":"Artifact","description":"keep existing"}]}`)

	body := decodeToolTestObject(t, rewriteToolDefinitionReferences(raw))
	tools := body["tools"].([]any)
	require.Len(t, tools, 2)
	byName := make(map[string]map[string]any, len(tools))
	for _, item := range tools {
		tool := item.(map[string]any)
		byName[tool["name"].(string)] = tool
	}
	assert.Equal(t, "keep existing", byName["Artifact"]["description"])
	assert.Equal(t, "latest", byName["Workflow"]["description"])

	message := toolTestMessage(t, body, 0)
	for i := 0; i < 3; i++ {
		assert.Equal(t, "tool_reference", toolTestContent(t, message, i)["tool"].(map[string]any)["type"])
	}
}

func TestRewriteToolDefinitionReferencesReturnsOriginalOnNoMatchOrInvalidJSON(t *testing.T) {
	plain := []byte("{\n  \"model\": \"deepseek-chat\", \"messages\": []\n}")
	assert.Equal(t, plain, rewriteToolDefinitionReferences(plain))

	invalid := []byte(`{"messages":[{"type":"tool_addition","tool":{"type":"tool_definition"`)
	assert.Equal(t, invalid, rewriteToolDefinitionReferences(invalid))
}

func TestRewriteToolDefinitionReferencesLeavesAllDeferredRequestUnchanged(t *testing.T) {
	raw := []byte(`{"tools":[{"name":"support_tool","defer_loading":true}],"messages":[{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"Artifact","description":"render"}}}]}]}`)
	assert.Equal(t, raw, rewriteToolDefinitionReferences(raw))
}

func TestAnthropicCountTokensRewritesToolDefinitions(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"input_tokens":42}`))
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", srv.URL, nil).WithToolDefinitionReferences()
	status, _, err := a.CountTokens(context.Background(), []byte(toolDefinitionRequest))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)

	body := decodeToolTestObject(t, gotBody)
	message := toolTestMessage(t, body, 0)
	assert.Equal(t, "tool_reference", toolTestContent(t, message, 1)["tool"].(map[string]any)["type"])
	assert.Len(t, body["tools"], 3)
	assert.NotContains(t, string(gotBody), "tool_definition")
}

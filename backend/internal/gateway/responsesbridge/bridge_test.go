package responsesbridge

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertRequest_StringInputAndTools(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5",
		"instructions":"you are helpful",
		"input":"hello",
		"stream":true,
		"tools":[{"type":"function","name":"calc","description":"math","parameters":{"type":"object"}}],
		"reasoning":{"effort":"medium"},
		"max_output_tokens":128
	}`)
	chatBody, model, stream, err := ConvertRequest(raw)
	require.NoError(t, err)
	assert.Equal(t, "gpt-5", model)
	assert.True(t, stream)

	var out map[string]any
	require.NoError(t, json.Unmarshal(chatBody, &out))
	assert.Equal(t, "gpt-5", out["model"])
	assert.Equal(t, true, out["stream"])
	assert.Equal(t, float64(128), out["max_tokens"])
	assert.Equal(t, "medium", out["reasoning_effort"])

	msgs := out["messages"].([]any)
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0].(map[string]any)["role"])
	assert.Equal(t, "you are helpful", msgs[0].(map[string]any)["content"])
	assert.Equal(t, "user", msgs[1].(map[string]any)["role"])
	assert.Equal(t, "hello", msgs[1].(map[string]any)["content"])

	tools := out["tools"].([]any)
	require.Len(t, tools, 1)
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	assert.Equal(t, "calc", fn["name"])
}

func TestConvertRequest_DeveloperRoleAndFunctionRoundtrip(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5",
		"input":[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"sys"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"use tool"}]},
			{"type":"function_call","call_id":"call_1","name":"calc","arguments":"{\"x\":1}"},
			{"type":"function_call_output","call_id":"call_1","output":"2"}
		]
	}`)
	chatBody, _, _, err := ConvertRequest(raw)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(chatBody, &out))
	msgs := out["messages"].([]any)
	require.Len(t, msgs, 4)
	assert.Equal(t, "system", msgs[0].(map[string]any)["role"])
	assert.Equal(t, "sys", msgs[0].(map[string]any)["content"])
	assert.Equal(t, "user", msgs[1].(map[string]any)["role"])
	assert.Equal(t, "assistant", msgs[2].(map[string]any)["role"])
	assert.Equal(t, "tool", msgs[3].(map[string]any)["role"])
	assert.Equal(t, "call_1", msgs[3].(map[string]any)["tool_call_id"])
}

func TestConvertResponse_TextAndToolCalls(t *testing.T) {
	chat := []byte(`{
		"id":"chatcmpl-1",
		"model":"deepseek-v4-flash",
		"created":1700000000,
		"choices":[{
			"finish_reason":"tool_calls",
			"message":{
				"role":"assistant",
				"content":"thinking",
				"tool_calls":[{
					"id":"call_9",
					"type":"function",
					"function":{"name":"calc","arguments":"{\"n\":2}"}
				}]
			}
		}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,
			"prompt_tokens_details":{"cached_tokens":2},
			"completion_tokens_details":{"reasoning_tokens":1}}
	}`)
	out, err := ConvertResponse(chat, "resp_1", "gpt-5")
	require.NoError(t, err)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(out, &resp))
	assert.Equal(t, "resp_1", resp["id"])
	assert.Equal(t, "completed", resp["status"])
	assert.Equal(t, "deepseek-v4-flash", resp["model"])
	output := resp["output"].([]any)
	require.Len(t, output, 2)
	assert.Equal(t, "function_call", output[0].(map[string]any)["type"])
	assert.Equal(t, "message", output[1].(map[string]any)["type"])
	usage := resp["usage"].(map[string]any)
	assert.Equal(t, float64(10), usage["input_tokens"])
	assert.Equal(t, float64(5), usage["output_tokens"])
}

func TestStreamWriter_TextSSE(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewStreamWriter(rec, "resp_stream", "deepseek-v4-flash")
	rec.Header().Set("Content-Type", "text/event-stream")

	_, err := w.Write([]byte("data: {\"model\":\"deepseek-v4-flash\",\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n"))
	require.NoError(t, err)
	_, err = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1,\"total_tokens\":4,\"prompt_tokens_details\":{\"cached_tokens\":0},\"completion_tokens_details\":{\"reasoning_tokens\":0}}}\n\n"))
	require.NoError(t, err)
	_, err = w.Write([]byte("data: [DONE]\n\n"))
	require.NoError(t, err)
	require.NoError(t, w.Finish())

	body := rec.Body.String()
	assert.Contains(t, body, "response.created")
	assert.Contains(t, body, "response.output_text.delta")
	assert.Contains(t, body, `"delta":"Hi"`)
	assert.Contains(t, body, "response.completed")
	assert.True(t, strings.Contains(body, "event: response.completed"))
}

func TestConvertRequest_MultiTextPartsJoinedWithNewline(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5",
		"input":[
			{"type":"message","role":"user","content":[
				{"type":"input_text","text":"段落一"},
				{"type":"input_text","text":"段落二"}
			]},
			{"type":"function_call_output","call_id":"call_1","output":[
				{"type":"output_text","text":"第一块\n"},
				{"type":"output_text","text":"第二块"}
			]}
		]
	}`)
	chatBody, _, _, err := ConvertRequest(raw)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(chatBody, &out))
	msgs := out["messages"].([]any)
	require.Len(t, msgs, 2)
	assert.Equal(t, "段落一\n段落二", msgs[0].(map[string]any)["content"])
	// 已以换行收尾的块不重复加空行
	assert.Equal(t, "第一块\n第二块", msgs[1].(map[string]any)["content"])
}

func TestJoinTextParts(t *testing.T) {
	assert.Equal(t, "", joinTextParts(nil))
	assert.Equal(t, "a", joinTextParts([]string{"a"}))
	assert.Equal(t, "a\nb", joinTextParts([]string{"a", "b"}))
	assert.Equal(t, "a\nb", joinTextParts([]string{"a\n", "b"}))
	assert.Equal(t, "a\nb", joinTextParts([]string{"a", "\nb"}))
	assert.Equal(t, "a\nb", joinTextParts([]string{"a", "", "b"}))
}

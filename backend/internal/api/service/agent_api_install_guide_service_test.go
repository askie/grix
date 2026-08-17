package service

import (
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

func guidesByType(t *testing.T, lang string) map[string]AgentAPIInstallGuideResp {
	t.Helper()
	catalog := AgentAPIInstallGuideCatalog(lang)
	wantCount := len(agentAPIInstallGuideDefs)
	if len(catalog.List) != wantCount {
		t.Fatalf("[%s] len(list)=%d want=%d", lang, len(catalog.List), wantCount)
	}
	out := make(map[string]AgentAPIInstallGuideResp, len(catalog.List))
	for _, item := range catalog.List {
		out[item.Type] = item
	}
	return out
}

// Every client type the platform accepts must have a task — an agent whose type
// has no guide leaves its owner on the setup page with nothing to copy.
// OpenHuman is deliberately excluded: it stays a valid, working client type,
// just hidden from the creation list.
func TestAgentAPIInstallGuideCatalog_CoversEveryClientType(t *testing.T) {
	guides := guidesByType(t, "en")
	for _, clientType := range []string{
		model.AgentClientTypeClaude, model.AgentClientTypeCodex,
		model.AgentClientTypeKimi, model.AgentClientTypeQwen,
		model.AgentClientTypeOpenClaw, model.AgentClientTypeHermes,
		model.AgentClientTypeCursor, model.AgentClientTypeCopilot,
		model.AgentClientTypeKiro, model.AgentClientTypePi,
		model.AgentClientTypeOpenCode, model.AgentClientTypeReasonix,
		model.AgentClientTypeCodeWhale,
		model.AgentClientTypeAgy,
		model.AgentClientTypeDeepSeek,
	} {
		guide, ok := guides[clientType]
		if !ok {
			t.Fatalf("no guide for client type %q", clientType)
		}
		if guide.Label == "" || guide.Intro == "" {
			t.Fatalf("%s: label/intro must not be empty", clientType)
		}
		if guide.ContentMode != AgentAPIInstallGuideModeText {
			t.Fatalf("%s: content_mode=%q", clientType, guide.ContentMode)
		}
		if strings.TrimSpace(guide.ContentTemplate) == "" {
			t.Fatalf("%s: empty content_template", clientType)
		}
	}
	if got := AgentAPIInstallGuideCatalog("en").List[0].Type; got != model.AgentClientTypeDeepSeek {
		t.Fatalf("first guide=%q want=%q", got, model.AgentClientTypeDeepSeek)
	}
	if got := AgentAPIInstallGuideCatalog("en").List[3].Type; got != model.AgentClientTypeKimi {
		t.Fatalf("fourth guide=%q want=%q", got, model.AgentClientTypeKimi)
	}
	if _, ok := guides[model.AgentClientTypeGemini]; ok {
		t.Fatal("gemini must not appear in the agent creation guide list")
	}
	if _, ok := guides[model.AgentClientTypeOpenHuman]; ok {
		t.Fatal("openhuman must not appear in the agent creation guide list")
	}
	deepseek := guides[model.AgentClientTypeDeepSeek]
	if deepseek.Label != "DeepSeek Harness" {
		t.Fatalf("deepseek label=%q want=%q", deepseek.Label, "DeepSeek Harness")
	}
	if deepseek.ContentTemplate != "npm i -g @deepseek-ai/dsh" {
		t.Fatalf("deepseek content_template=%q", deepseek.ContentTemplate)
	}
	if !strings.Contains(deepseek.CopyTemplate, "npm i -g @deepseek-ai/dsh") {
		t.Fatal("deepseek task must install the official npm CLI")
	}
	if strings.Contains(deepseek.CopyTemplate, "dsh-jsonrpc-agent") {
		t.Fatal("deepseek task must not mention the compiled JSON-RPC binary")
	}
	if !strings.Contains(deepseek.CopyTemplate, `"client_type": "deepseek"`) {
		t.Fatal("deepseek task must configure client_type=deepseek")
	}
}

func TestAgentAPIInstallGuideCatalog_DefaultsToClaude(t *testing.T) {
	if got := AgentAPIInstallGuideCatalog("en").DefaultType; got != model.AgentClientTypeClaude {
		t.Fatalf("default_type=%q want=%q", got, model.AgentClientTypeClaude)
	}
}

func TestAgentAPIInstallGuideCatalog_LocalizesTasks(t *testing.T) {
	en := guidesByType(t, "en")
	zh := guidesByType(t, "zh")

	if !strings.Contains(en[model.AgentClientTypeQwen].CopyTemplate, "Connect this Grix Agent to grix-connector") {
		t.Fatalf("qwen en task not in english: %q", en[model.AgentClientTypeQwen].CopyTemplate)
	}
	if !strings.Contains(zh[model.AgentClientTypeQwen].CopyTemplate, "把这个 Grix Agent 接入本机的 grix-connector") {
		t.Fatalf("qwen zh task not in chinese: %q", zh[model.AgentClientTypeQwen].CopyTemplate)
	}
	// Labels are product names — they stay identical across locales.
	if en[model.AgentClientTypeReasonix].Label != zh[model.AgentClientTypeReasonix].Label {
		t.Fatal("reasonix label should not be localized")
	}
}

// The task is pasted into an agent verbatim, so a placeholder the client cannot
// resolve would ship a literal "{{api_key}}" to the target machine.
func TestAgentAPIInstallGuideCatalog_TasksCarryEveryPlaceholder(t *testing.T) {
	for _, lang := range []string{"en", "zh"} {
		for clientType, guide := range guidesByType(t, lang) {
			for _, placeholder := range []string{
				"{{agent_name}}", "{{agent_id}}", "{{api_key}}", "{{api_endpoint}}",
			} {
				if !strings.Contains(guide.CopyTemplate, placeholder) {
					t.Fatalf("[%s] %s: copy_template missing %s", lang, clientType, placeholder)
				}
			}
		}
	}
}

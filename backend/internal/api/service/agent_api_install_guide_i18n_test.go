package service

import (
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

var guideAppLanguages = []string{
	"zh", "en", "ja", "ko", "de", "fr", "es", "pt", "ru", "ar", "hi",
}

// Every app language must serve a complete catalog: all types present, every
// intro and task non-empty, no leftover fmt verbs, and the credential
// placeholders intact so the client can substitute them.
func TestAgentAPIInstallGuideCatalog_EveryAppLanguageComplete(t *testing.T) {
	for _, lang := range guideAppLanguages {
		catalog := AgentAPIInstallGuideCatalog(lang)
		if len(catalog.List) != len(agentAPIInstallGuideDefs) {
			t.Fatalf("lang=%s entries=%d want=%d", lang, len(catalog.List), len(agentAPIInstallGuideDefs))
		}
		for _, item := range catalog.List {
			if strings.TrimSpace(item.Intro) == "" {
				t.Fatalf("lang=%s type=%s intro empty", lang, item.Type)
			}
			task := item.CopyTemplate
			if strings.TrimSpace(task) == "" {
				t.Fatalf("lang=%s type=%s task empty", lang, item.Type)
			}
			if strings.Contains(task, "%s") || strings.Contains(task, "%!") {
				t.Fatalf("lang=%s type=%s task carries an unresolved fmt verb", lang, item.Type)
			}
			for _, placeholder := range []string{"{{agent_name}}", "{{agent_id}}", "{{api_key}}", "{{api_endpoint}}"} {
				if !strings.Contains(task, placeholder) {
					t.Fatalf("lang=%s type=%s task missing %s", lang, item.Type, placeholder)
				}
			}
		}
	}
}

// Languages with their own translation must not silently read English, and an
// unsupported language must fall back to English exactly.
func TestAgentAPIInstallGuideCatalog_LanguageSelectionAndFallback(t *testing.T) {
	en := AgentAPIInstallGuideCatalog("en")
	find := func(catalog AgentAPIInstallGuideCatalogResp, typ string) AgentAPIInstallGuideResp {
		for _, item := range catalog.List {
			if item.Type == typ {
				return item
			}
		}
		t.Fatalf("type %s not found", typ)
		return AgentAPIInstallGuideResp{}
	}

	for _, lang := range []string{"ja", "ko", "de", "fr", "es", "pt", "ru", "ar", "hi"} {
		got := find(AgentAPIInstallGuideCatalog(lang), model.AgentClientTypeClaude)
		want := find(en, model.AgentClientTypeClaude)
		if got.CopyTemplate == want.CopyTemplate {
			t.Fatalf("lang=%s claude task identical to en — translation not wired", lang)
		}
		if got.Intro == want.Intro {
			t.Fatalf("lang=%s claude intro identical to en — translation not wired", lang)
		}
	}

	// Unsupported language falls back to English wholesale.
	it := AgentAPIInstallGuideCatalog("it")
	for i, item := range it.List {
		if item.CopyTemplate != en.List[i].CopyTemplate {
			t.Fatalf("type=%s: unsupported lang should serve the en task", item.Type)
		}
	}

	// Labels and commands stay language-neutral.
	ja := AgentAPIInstallGuideCatalog("ja")
	for i, item := range ja.List {
		if item.Label != en.List[i].Label {
			t.Fatalf("type=%s label should not vary by language", item.Type)
		}
		if item.ContentTemplate != en.List[i].ContentTemplate {
			t.Fatalf("type=%s content template should not vary by language", item.Type)
		}
	}
}

// The Kimi task keeps its extra install step in every language, and the
// connector task embeds the per-language CLI phrase.
func TestAgentAPIInstallGuideCatalog_LanguageSpecificDetails(t *testing.T) {
	for _, lang := range guideAppLanguages {
		catalog := AgentAPIInstallGuideCatalog(lang)
		for _, item := range catalog.List {
			switch item.Type {
			case model.AgentClientTypeDeepSeek:
				if !strings.Contains(item.CopyTemplate, "npm i -g pnpm") {
					t.Fatalf("lang=%s deepseek task lost its pnpm install step", lang)
				}
				if !strings.Contains(item.CopyTemplate, "npm i -g @deepseek-ai/dsh") {
					t.Fatalf("lang=%s deepseek task lost its CLI install step", lang)
				}
				if strings.Contains(item.CopyTemplate, "dsh-jsonrpc-agent") {
					t.Fatalf("lang=%s deepseek task still mentions the compiled binary", lang)
				}
			case model.AgentClientTypeKimi:
				if !strings.Contains(item.CopyTemplate, "npm install -g @moonshot-ai/kimi-code") {
					t.Fatalf("lang=%s kimi task lost its CLI install step", lang)
				}
			case model.AgentClientTypeClaude:
				if !strings.Contains(item.CopyTemplate, "claude") {
					t.Fatalf("lang=%s claude task lost the CLI name", lang)
				}
			case model.AgentClientTypeOpenClaw:
				if !strings.Contains(item.CopyTemplate, "--strict-json") {
					t.Fatalf("lang=%s openclaw task lost the config command", lang)
				}
			case model.AgentClientTypeHermes:
				if !strings.Contains(item.CopyTemplate, "GRIX_ENDPOINT={{api_endpoint}}") {
					t.Fatalf("lang=%s hermes task lost the .env lines", lang)
				}
			}
		}
	}
}

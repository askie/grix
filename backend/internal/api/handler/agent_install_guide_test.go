package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/gin-gonic/gin"
)

type agentInstallGuideListResp struct {
	Code int `json:"code"`
	Data struct {
		DefaultType string `json:"default_type"`
		List        []struct {
			Type            string `json:"type"`
			Label           string `json:"label"`
			Intro           string `json:"intro"`
			ContentMode     string `json:"content_mode"`
			ContentTemplate string `json:"content_template"`
			LinkLabel       string `json:"link_label"`
			LinkURL         string `json:"link_url"`
			CopyTemplate    string `json:"copy_template"`
		} `json:"list"`
	} `json:"data"`
}

func fetchInstallGuides(t *testing.T, locale string) agentInstallGuideListResp {
	t.Helper()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", int64(1))
		c.Next()
	})
	r.GET("/agents/agent-api/install-guides", AgentAPIInstallGuideList)

	req := httptest.NewRequest(http.MethodGet, "/agents/agent-api/install-guides", nil)
	req.Header.Set("X-App-Locale", locale)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp agentInstallGuideListResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response error: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("code=%d want=0", resp.Code)
	}
	return resp
}

// Every guide is copied straight into an AI agent's prompt, so each one must
// carry all four placeholders — a missing one ships a task with a literal
// "{{...}}" or, worse, no credentials at all.
func TestAgentAPIInstallGuideList_EveryGuideCarriesPlaceholders(t *testing.T) {
	for _, locale := range []string{"en-US", "zh-CN"} {
		resp := fetchInstallGuides(t, locale)
		wantCount := len(service.AgentAPIInstallGuideCatalog(locale).List)
		if len(resp.Data.List) != wantCount {
			t.Fatalf("[%s] expected %d guide entries, got %d", locale, wantCount, len(resp.Data.List))
		}
		if resp.Data.DefaultType != "claude" {
			t.Fatalf("[%s] default_type=%q want=%q", locale, resp.Data.DefaultType, "claude")
		}
		for _, item := range resp.Data.List {
			if strings.TrimSpace(item.CopyTemplate) == "" {
				t.Fatalf("[%s] %s: empty copy_template", locale, item.Type)
			}
			for _, placeholder := range []string{"{{agent_name}}", "{{agent_id}}", "{{api_key}}", "{{api_endpoint}}"} {
				if !strings.Contains(item.CopyTemplate, placeholder) {
					t.Fatalf("[%s] %s: copy_template is missing %s", locale, item.Type, placeholder)
				}
			}
		}
	}
}

// The three install paths are genuinely different systems. A connector task that
// forgot "reload", an OpenClaw task that used snake_case fields, or a Hermes task
// that pointed at agents.json would each send the executing agent down the wrong
// road — these assertions pin each family to its own contract.
func TestAgentAPIInstallGuideList_TaskMatchesItsInstallPath(t *testing.T) {
	resp := fetchInstallGuides(t, "en-US")
	guides := map[string]string{}
	for _, item := range resp.Data.List {
		guides[item.Type] = item.CopyTemplate
	}

	for _, connectorType := range []string{
		"claude", "codex", "qwen", "cursor", "copilot", "kiro",
		"pi", "opencode", "reasonix", "codewhale", "agy", "kimi",
	} {
		task, ok := guides[connectorType]
		if !ok {
			t.Fatalf("guide for %q not found", connectorType)
		}
		for _, want := range []string{
			"npm install -g grix-connector",
			"~/.grix/config/agents.json",
			"grix-connector status",
			"grix-connector reload",
			`"client_type": "` + connectorType + `"`,
		} {
			if !strings.Contains(task, want) {
				t.Fatalf("%s task is missing %q", connectorType, want)
			}
		}
		// Merging, not clobbering: this is the one step that can destroy the
		// owner's other agents if the executing agent gets it wrong.
		if !strings.Contains(task, "Never overwrite the whole file") {
			t.Fatalf("%s task does not forbid overwriting agents.json", connectorType)
		}
	}
	deepseekTask, ok := guides["deepseek"]
	if !ok {
		t.Fatal("guide for deepseek not found")
	}
	for _, want := range []string{
		"npm i -g pnpm",
		"npm i -g @deepseek-ai/dsh",
		"npm install -g grix-connector",
		"~/.grix/config/agents.json",
		"grix-connector reload",
		`"client_type": "deepseek"`,
	} {
		if !strings.Contains(deepseekTask, want) {
			t.Fatalf("deepseek task is missing %q", want)
		}
	}
	if strings.Contains(deepseekTask, "dsh-jsonrpc-agent") {
		t.Fatal("deepseek task must not mention the compiled JSON-RPC binary")
	}
	if resp.Data.List[0].Type != "deepseek" {
		t.Fatalf("first guide should be deepseek, got %q", resp.Data.List[0].Type)
	}
	if resp.Data.List[3].Type != "kimi" {
		t.Fatalf("fourth guide should be kimi, got %q", resp.Data.List[3].Type)
	}
	for _, item := range resp.Data.List {
		if item.Type == "gemini" {
			t.Fatal("gemini must not appear in the agent creation guide list")
		}
	}

	openclaw := guides["openclaw"]
	for _, want := range []string{
		"openclaw plugins install grix-connector",
		"openclaw config set channels.grix.accounts.",
		"--strict-json",
		`"wsUrl"`, `"agentId"`, `"apiKey"`,
		"Never hand-edit openclaw.json",
	} {
		if !strings.Contains(openclaw, want) {
			t.Fatalf("openclaw task is missing %q", want)
		}
	}
	if strings.Contains(openclaw, "agents.json") {
		t.Fatal("openclaw task must not mention the connector's agents.json")
	}

	hermes := guides["hermes"]
	for _, want := range []string{
		"plugins install askie/grix-hermes-python --enable",
		"~/.hermes/.env",
		"~/.hermes/profiles/<profile>/.env",
		"GRIX_ENDPOINT={{api_endpoint}}",
		"GRIX_AGENT_ID={{agent_id}}",
		"GRIX_API_KEY={{api_key}}",
		"hermes --profile <profile> gateway restart",
	} {
		if !strings.Contains(hermes, want) {
			t.Fatalf("hermes task is missing %q", want)
		}
	}
	if strings.Contains(hermes, "agents.json") {
		t.Fatal("hermes task must not mention the connector's agents.json")
	}
}

func TestAgentAPIInstallGuideList_LocalizesByHeader(t *testing.T) {
	en := fetchInstallGuides(t, "en-US")
	zh := fetchInstallGuides(t, "zh-CN")

	if en.Data.List[0].Type != "deepseek" || zh.Data.List[0].Type != "deepseek" {
		t.Fatalf("first guide should be deepseek, got en=%q zh=%q", en.Data.List[0].Type, zh.Data.List[0].Type)
	}
	// DeepSeek 是 connector 类向导，但 CLI 走官方 npm 包，短命令不是 grix-connector。
	if !strings.Contains(en.Data.List[0].CopyTemplate, "Connect this Grix Agent to grix-connector") {
		t.Fatalf("english task not served for en-US: %q", en.Data.List[0].CopyTemplate)
	}
	if !strings.Contains(zh.Data.List[0].CopyTemplate, "把这个 Grix Agent 接入本机的 grix-connector") {
		t.Fatalf("chinese task not served for zh-CN: %q", zh.Data.List[0].CopyTemplate)
	}
	if got := en.Data.List[0].ContentTemplate; got != "npm i -g pnpm\nnpm i -g @deepseek-ai/dsh" {
		t.Fatalf("deepseek content_template=%q", got)
	}
}

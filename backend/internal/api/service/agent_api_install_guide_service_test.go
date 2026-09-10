package service

import (
	"strings"
	"testing"
	"unicode"

	"github.com/askie/grix/backend/internal/model"
)

// containsCJK reports whether s has any CJK-range rune — used to catch a
// Chinese loginInstruction string accidentally reused for the English guide
// text (customCliInstallGuide's loginZh/loginEn split exists to prevent
// exactly that regression).
func containsCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

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
		model.AgentClientTypeQoderCLI,
		model.AgentClientTypeQoderCLICN,
		model.AgentClientTypeMCode,
		model.AgentClientTypeDim,
		model.AgentClientTypeDeveco,
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
	if deepseek.ContentTemplate != "npm i -g pnpm\nnpm i -g @deepseek-ai/dsh" {
		t.Fatalf("deepseek content_template=%q", deepseek.ContentTemplate)
	}
	if !strings.Contains(deepseek.CopyTemplate, "npm i -g pnpm") {
		t.Fatal("deepseek task must install pnpm for profile plugins")
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

// Round 2a: qodercli/qoderclicn/mcode/dim each need their own install+login
// step 0 (like Kimi/DeepSeek), not the "assume it's already installed" shape
// connectorGuide() uses for CLIs owners typically already have.
func TestAgentAPIInstallGuideCatalog_Round2aCliGuides(t *testing.T) {
	en := guidesByType(t, "en")

	qodercli := en[model.AgentClientTypeQoderCLI]
	if qodercli.Label != "Qoder CLI" {
		t.Fatalf("qodercli label=%q", qodercli.Label)
	}
	if !strings.Contains(qodercli.CopyTemplate, "curl -fsSL https://qoder.com/install | bash") {
		t.Fatal("qodercli task must include the official install command")
	}
	if !strings.Contains(qodercli.CopyTemplate, "qodercli login") {
		t.Fatal("qodercli task must instruct running qodercli login")
	}
	if !strings.Contains(qodercli.CopyTemplate, `"client_type": "qodercli"`) {
		t.Fatal("qodercli task must configure client_type=qodercli")
	}
	if !strings.Contains(qodercli.CopyTemplate, "qodercli is not on PATH") {
		t.Fatal("qodercli task's troubleshooting line must name the qodercli binary")
	}

	qoderclicn := en[model.AgentClientTypeQoderCLICN]
	if !strings.Contains(qoderclicn.CopyTemplate, "static.qoder.com.cn/qoder-cli-cn/install.sh") {
		t.Fatal("qoderclicn task must include the CN-region install command, distinct from qodercli's")
	}
	if !strings.Contains(qoderclicn.CopyTemplate, "qoderclicn login") {
		t.Fatal("qoderclicn task must instruct running qoderclicn login")
	}

	mcode := en[model.AgentClientTypeMCode]
	if !strings.Contains(mcode.CopyTemplate, "npm install -g @minimax-ai/code") {
		t.Fatal("mcode task must include the official npm install command")
	}
	if !strings.Contains(mcode.CopyTemplate, "mcode login") {
		t.Fatal("mcode task must instruct running mcode login")
	}
	if !strings.Contains(mcode.CopyTemplate, "Node.js 22.19+") {
		t.Fatal("mcode task must reflect its higher Node.js floor (package.json engines >=22.19)")
	}

	dim := en[model.AgentClientTypeDim]
	if !strings.Contains(dim.CopyTemplate, "npm install -g dimcode") {
		t.Fatal("dim task must include the official npm install command")
	}
	if !strings.Contains(dim.CopyTemplate, "dim auth login") {
		t.Fatal("dim task must instruct running dim auth login (not a bare `dim login`, which does not exist)")
	}

	// customCliInstallGuide takes separate loginZh/loginEn strings specifically
	// so the en guide's login instructions are never the Chinese ones reused
	// verbatim — lock that in for all four round2a CLIs.
	for _, entry := range []struct {
		clientType string
		guide      AgentAPIInstallGuideResp
	}{
		{model.AgentClientTypeQoderCLI, qodercli},
		{model.AgentClientTypeQoderCLICN, qoderclicn},
		{model.AgentClientTypeMCode, mcode},
		{model.AgentClientTypeDim, dim},
	} {
		if containsCJK(entry.guide.CopyTemplate) {
			t.Fatalf("%s: en install guide task contains CJK text (Chinese leaking into the English guide): %q", entry.clientType, entry.guide.CopyTemplate)
		}
	}
}

func TestAgentAPIInstallGuideCatalog_Round3CliGuides(t *testing.T) {
	en := guidesByType(t, "en")

	omp := en[model.AgentClientTypeOmp]
	if omp.Label != "Oh-My-Pi" {
		t.Fatalf("omp label=%q", omp.Label)
	}
	if !strings.Contains(omp.CopyTemplate, "npm install -g @oh-my-pi/pi-coding-agent") {
		t.Fatal("omp task must include the official npm install command")
	}
	if !strings.Contains(omp.CopyTemplate, "curl -fsSL https://bun.sh/install | bash") {
		t.Fatal("omp task must install bun first (omp's npm shim needs it on PATH to run at all)")
	}
	if !strings.Contains(omp.CopyTemplate, `"client_type": "omp"`) {
		t.Fatal("omp task must configure client_type=omp")
	}
	if !strings.Contains(omp.CopyTemplate, "omp is not on PATH") {
		t.Fatal("omp task's troubleshooting line must name the omp binary")
	}

	codebuddy := en[model.AgentClientTypeCodeBuddy]
	if !strings.Contains(codebuddy.CopyTemplate, "npm install -g @tencent-ai/codebuddy-code") {
		t.Fatal("codebuddy task must include the official npm install command")
	}
	if !strings.Contains(codebuddy.CopyTemplate, "/login") {
		t.Fatal("codebuddy task must instruct using the in-app /login slash command")
	}
	if !strings.Contains(codebuddy.CopyTemplate, `"client_type": "codebuddy"`) {
		t.Fatal("codebuddy task must configure client_type=codebuddy")
	}

	for _, entry := range []struct {
		clientType string
		guide      AgentAPIInstallGuideResp
	}{
		{model.AgentClientTypeOmp, omp},
		{model.AgentClientTypeCodeBuddy, codebuddy},
	} {
		if containsCJK(entry.guide.CopyTemplate) {
			t.Fatalf("%s: en install guide task contains CJK text (Chinese leaking into the English guide): %q", entry.clientType, entry.guide.CopyTemplate)
		}
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

// TestAgentAPIInstallGuideCatalog_Round5NineLanguageCoverage guards against
// the round5 finding: qodercli/qoderclicn/mcode/dim/traecli/qwenpaw/zeroclaw/
// codebuddy/omp only had zh/en authored, so every other app language
// (ja ko de fr es pt ru ar hi) silently fell back to the English text. Each
// of these languages must now carry its own translation — distinct from the
// English fallback — and every placeholder the client substitutes.
func TestAgentAPIInstallGuideCatalog_Round5NineLanguageCoverage(t *testing.T) {
	round5Types := []string{
		model.AgentClientTypeQoderCLI,
		model.AgentClientTypeQoderCLICN,
		model.AgentClientTypeMCode,
		model.AgentClientTypeDim,
		model.AgentClientTypeTraeCli,
		model.AgentClientTypeQwenPaw,
		model.AgentClientTypeZeroClaw,
		model.AgentClientTypeCodeBuddy,
		model.AgentClientTypeOmp,
	}
	otherLangs := []string{"ja", "ko", "de", "fr", "es", "pt", "ru", "ar", "hi"}

	en := guidesByType(t, "en")
	for _, lang := range otherLangs {
		localized := guidesByType(t, lang)
		for _, clientType := range round5Types {
			got, ok := localized[clientType]
			if !ok {
				t.Fatalf("[%s] %s: missing from catalog", lang, clientType)
			}
			enTask := en[clientType].CopyTemplate
			if got.CopyTemplate == "" {
				t.Fatalf("[%s] %s: copy_template is empty", lang, clientType)
			}
			if got.CopyTemplate == enTask {
				t.Fatalf("[%s] %s: copy_template is identical to the English text — still falling back, not localized", lang, clientType)
			}
			for _, placeholder := range []string{
				"{{agent_name}}", "{{agent_id}}", "{{api_key}}", "{{api_endpoint}}",
			} {
				if !strings.Contains(got.CopyTemplate, placeholder) {
					t.Fatalf("[%s] %s: copy_template missing %s", lang, clientType, placeholder)
				}
			}
		}
	}
}

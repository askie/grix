package claude

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFormatToolSummary(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		toolInput map[string]any
		want      string
	}{
		{"Bash command", "Bash", map[string]any{"command": "ls -la"}, "Bash: ls -la"},
		{"Read file_path", "Read", map[string]any{"file_path": "/tmp/test.go"}, "Read: /tmp/test.go"},
		{"Edit file_path", "Edit", map[string]any{"file_path": "/tmp/test.go", "old_string": "foo"}, "Edit: /tmp/test.go"},
		{"Write file_path", "Write", map[string]any{"file_path": "/tmp/out.txt"}, "Write: /tmp/out.txt"},
		{"Grep pattern", "Grep", map[string]any{"pattern": "formatHook", "path": "/src"}, "Grep: formatHook"},
		{"Glob pattern", "Glob", map[string]any{"pattern": "**/*.go"}, "Glob: **/*.go"},
		{"WebSearch query", "WebSearch", map[string]any{"query": "golang generics"}, "WebSearch: golang generics"},
		{"NotebookEdit path", "NotebookEdit", map[string]any{"notebook_path": "/tmp/nb.ipynb"}, "NotebookEdit: /tmp/nb.ipynb"},
		{"TaskOutput task_id", "TaskOutput", map[string]any{"task_id": "abc-123"}, "TaskOutput: abc-123"},
		{"Agent description", "Agent", map[string]any{"description": "explore codebase"}, "Agent: explore codebase"},
		{"Skill name", "Skill", map[string]any{"skill": "review"}, "Skill: review"},
		{"Unknown tool no input", "CustomTool", nil, "CustomTool"},
		{"Empty input", "Read", map[string]any{}, "Read"},
		{"Long command truncated", "Bash", map[string]any{"command": strings.Repeat("x", 200)}, "Bash: " + strings.Repeat("x", 117) + "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var inputRaw json.RawMessage
			if tt.toolInput != nil {
				var err error
				inputRaw, err = json.Marshal(tt.toolInput)
				if err != nil {
					t.Fatalf("marshal input: %v", err)
				}
			}
			got := formatToolSummary(tt.toolName, inputRaw)
			if got != tt.want {
				t.Errorf("formatToolSummary(%q, %v) = %q, want %q", tt.toolName, tt.toolInput, got, tt.want)
			}
		})
	}
}

func TestShouldSuppressHookNotification_SuppressesGrixClaudeMCPTools(t *testing.T) {
	tests := []struct {
		name    string
		content string
		event   string
		want    bool
	}{
		{"reply suppressed", "mcp__grix-claude__reply", "PostToolUse", true},
		{"complete suppressed", "mcp__grix-claude__complete", "PostToolUse", true},
		{"delete_message suppressed", "mcp__grix-claude__delete_message", "PostToolUse", true},
		{"status suppressed", "mcp__grix-claude__status", "PostToolUse", true},
		{"Stop suppressed", "", "Stop", true},
		{"PreToolUse suppressed", "Bash", "PreToolUse", true},
		{"PreToolUse suppressed even with detail", "Read", "PreToolUse", true},
		{"Bash not suppressed", "Bash", "PostToolUse", false},
		{"Grep not suppressed", "Grep", "PostToolUse", false},
		{"TaskUpdate suppressed", "TaskUpdate", "PostToolUse", true},
		{"unknown suppressed", "unknown", "PostToolUse", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extra, _ := json.Marshal(map[string]any{
				"grix_hook_notification": true,
				"hook_event_name":       tt.event,
			})
			got := shouldSuppressHookNotification(tt.content, extra)
			if got != tt.want {
				t.Errorf("shouldSuppress(%q, event=%q) = %v, want %v", tt.content, tt.event, got, tt.want)
			}
		})
	}
}

func TestNormalizeHookNotificationCard_WithToolInput(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		hookEventName   string
		toolInput       map[string]any
		wantContains    string
	}{
		{
			"Grep with pattern",
			"Grep",
			"PostToolUse",
			map[string]any{"pattern": "formatHookEventText", "path": "/src/app.js"},
			"Grep: formatHookEventText",
		},
		{
			"Bash with command",
			"Bash",
			"PostToolUse",
			map[string]any{"command": "cat /etc/hosts"},
			"Bash: cat /etc/hosts",
		},
		{
			"PostToolUseFailure with file_path",
			"Read",
			"PostToolUseFailure",
			map[string]any{"file_path": "/nonexistent/file.go"},
			"Read: /nonexistent/file.go",
		},
		{
			"Stop event passthrough",
			"",
			"Stop",
			nil,
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extra := map[string]any{
				"grix_hook_notification": true,
				"hook_event_name":       tt.hookEventName,
			}
			if tt.toolInput != nil {
				extra["tool_input"] = tt.toolInput
			}
			extraRaw, _ := json.Marshal(extra)

			content, _, ok := normalizeHookNotificationCard(tt.content, extraRaw)
			if tt.wantContains == "" {
				if ok {
					t.Errorf("expected no card for %q, got content=%q", tt.name, content)
				}
				return
			}
			if !ok {
				t.Fatalf("expected card, got ok=false")
			}
			if !strings.Contains(content, tt.wantContains) {
				t.Errorf("content=%q should contain %q", content, tt.wantContains)
			}
		})
	}
}

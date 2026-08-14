package ws

import "testing"

func TestExtractToolbarReferenceIDEffortPrefersCanonicalParam(t *testing.T) {
	if got := extractToolbarReferenceID("set_reasoning_effort", map[string]any{
		"effort":           "auto",
		"reasoning_effort": "high",
	}); got != "auto" {
		t.Fatalf("canonical effort=%q want=auto", got)
	}
	if got := extractToolbarReferenceID("set_reasoning_effort", map[string]any{
		"reasoning_effort": "high",
	}); got != "high" {
		t.Fatalf("legacy reasoning_effort=%q want=high", got)
	}
}

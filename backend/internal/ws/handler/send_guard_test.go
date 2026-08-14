package handler

import (
	"context"
	"strings"
	"testing"
)

func TestInspectSendContentMetrics(t *testing.T) {
	metrics := inspectSendContentMetrics("  hello\nworld  ")
	if metrics.TrimmedLengthBytes == 0 || metrics.TrimmedLengthRunes == 0 {
		t.Fatalf("expected trimmed metrics to be recorded: %+v", metrics)
	}
}

func TestValidateSendContentUsesTransportChecksOnly(t *testing.T) {
	t.Run("allows markdown table content", func(t *testing.T) {
		content := strings.Join([]string{
			"**Grix-admin** plugins:",
			"```",
			"| Skill | Description | Trigger |",
			"| " + strings.Repeat("-", 94) + " | " + strings.Repeat("-", 68) + " | " + strings.Repeat("-", 57) + " |",
			"| grix_group | group management methods | create, get, update |",
			"| grix_user | user contact lookup | get, search |",
			"```",
			"Additional notes: this is a legitimate technical answer with a markdown table and should not be rejected by the send guard.",
		}, "\n")
		code, msg := validateSendContent(context.Background(), 0, "", content)
		if code != 0 || msg != "" {
			t.Fatalf("expected markdown table content to pass, got code=%d msg=%q", code, msg)
		}
	})

	t.Run("allows repeated rune content under size limit", func(t *testing.T) {
		code, msg := validateSendContent(context.Background(), 0, "", strings.Repeat("a", 300))
		if code != 0 || msg != "" {
			t.Fatalf("expected repeated content to pass transport checks, got code=%d msg=%q", code, msg)
		}
	})

	t.Run("allows repeated dash content under size limit", func(t *testing.T) {
		code, msg := validateSendContent(context.Background(), 0, "", strings.Repeat("-", 300))
		if code != 0 || msg != "" {
			t.Fatalf("expected repeated dash content to pass transport checks, got code=%d msg=%q", code, msg)
		}
	})

	t.Run("rejects oversize content", func(t *testing.T) {
		code, msg := validateSendContent(context.Background(), 0, "", strings.Repeat("a", sendMsgMaxRunes+1))
		if code != 4004 || msg != "message too large" {
			t.Fatalf("expected oversize content to be rejected, got code=%d msg=%q", code, msg)
		}
	})
}

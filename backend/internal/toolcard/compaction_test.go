package toolcard

import (
	"encoding/json"
	"testing"
)

func TestCompactForStorageIsIdempotentWhenFailureComesFromRawExtra(t *testing.T) {
	content := BuildExecutionCardURI(map[string]any{
		"summary_text": "Bash: deploy",
		"detail_text":  "permission denied by remote host",
	})
	extra := json.RawMessage(`{
		"channel_data":{
			"provider":{
				"raw_event":{
					"status":"failed",
					"tool_input":{"command":"deploy"},
					"raw_output":"permission denied by remote host"
				}
			}
		}
	}`)

	firstContent, firstExtra, firstMeta, ok := CompactForStorage(content, extra)
	if !ok || !firstMeta.Failed {
		t.Fatalf("first compaction metadata=%+v ok=%v", firstMeta, ok)
	}
	if firstMeta.DetailText != "permission denied by remote host" {
		t.Fatalf("first failure detail=%q", firstMeta.DetailText)
	}

	secondContent, secondExtra, secondMeta, ok := CompactForStorage(firstContent, firstExtra)
	if !ok || !secondMeta.Failed {
		t.Fatalf("second compaction metadata=%+v ok=%v", secondMeta, ok)
	}
	if secondMeta.DetailText != firstMeta.DetailText {
		t.Fatalf("second failure detail=%q want=%q", secondMeta.DetailText, firstMeta.DetailText)
	}
	if secondContent != firstContent || string(secondExtra) != string(firstExtra) {
		t.Fatalf(
			"compaction is not idempotent:\nfirst content=%q\nsecond content=%q\nfirst extra=%s\nsecond extra=%s",
			firstContent,
			secondContent,
			firstExtra,
			secondExtra,
		)
	}
}

package agentapi

import (
	"encoding/json"
	"fmt"
	"strings"
)

func buildExecApprovalCardMessage(payload map[string]any) pendingLocalActionReply {
	commandText := strings.TrimSpace(fmt.Sprintf("%v", payload["command"]))
	if commandText == "" {
		return pendingLocalActionReply{}
	}

	return pendingLocalActionReply{
		content: buildLocalGrixCardLink(
			fmt.Sprintf("[Exec Approval] %s", compactReplyText(commandText, 160)),
			"exec_approval",
			payload,
		),
		extra: buildExecApprovalCardExtra(payload),
	}
}

func buildExecStatusCardExtra(payload map[string]any) json.RawMessage {
	envelope := map[string]any{
		"biz_card": map[string]any{
			"version": 1,
			"type":    "exec_status",
			"payload": payload,
		},
		"channel_data": map[string]any{
			"grix": map[string]any{
				"execStatus": payload,
			},
		},
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil
	}
	return encoded
}

func buildExecApprovalCardExtra(payload map[string]any) json.RawMessage {
	envelope := map[string]any{
		"biz_card": map[string]any{
			"version": 1,
			"type":    "exec_approval",
			"payload": payload,
		},
		"channel_data": map[string]any{
			"execApproval": map[string]any{
				"approvalId":       strings.TrimSpace(fmt.Sprintf("%v", payload["approval_id"])),
				"approvalSlug":     strings.TrimSpace(fmt.Sprintf("%v", payload["approval_slug"])),
				"allowedDecisions": payload["allowed_decisions"],
			},
			"grix": map[string]any{
				"execApproval": map[string]any{
					"approval_command_id": strings.TrimSpace(fmt.Sprintf("%v", payload["approval_command_id"])),
					"command":             strings.TrimSpace(fmt.Sprintf("%v", payload["command"])),
					"host":                strings.TrimSpace(fmt.Sprintf("%v", payload["host"])),
					"warning_text":        strings.TrimSpace(fmt.Sprintf("%v", payload["warning_text"])),
				},
			},
		},
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil
	}
	return encoded
}

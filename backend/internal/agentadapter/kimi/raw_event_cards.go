package kimi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
)

// synthesizeKimiRawEventPayload rewrites an inbound send_msg whose
// `channel_data.acp.raw_event` envelope carries a Kimi ACP event into the
// same `channel_data.execApproval` + `channel_data.grix.execApproval` shape
// that `approvalcards.Normalize` already knows how to turn into an
// `exec_approval` card.
//
// Kimi flows its `session/request_permission` through the ACP raw-event
// bridge exactly the same way reasonix/gemini/cursor do; without this
// rewrite the payload lands as an opaque plain-text `Permission required:`
// message with no `execApproval` metadata, so
// `packet_handlers.go:extractApprovalIDFromCard` never records the card
// msg_id and the user's "allow" reply has no `approval_command_id` to
// resolve — the approval RPC waits forever on the ACP client side and the
// turn stays `running`.
//
// Only `permission_request` is handled here today; tool_use / tool_result /
// question / error events are still forwarded as plain content. Extend by
// mirroring `reasonix/inbound_cards.go` if a Kimi user surfaces a specific
// gap.
func synthesizeKimiRawEventPayload(p *agentadapter.InboundSendMsgPayload) (*agentadapter.InboundSendMsgPayload, bool) {
	channelData, eventType, rawPayload, ok := decodeKimiRawEventEnvelope(p)
	if !ok {
		return nil, false
	}
	if eventType != "permission_request" {
		return nil, false
	}
	return synthesizeKimiRawApprovalPayload(p, channelData, rawPayload)
}

// decodeKimiRawEventEnvelope extracts the `channel_data.acp.raw_event`
// envelope. The connector's ACP adapter (`bridge.ts` → `sendAcpRawEventEnvelope`)
// nests raw events under `acp.raw_event` (snake_case) or `acp.rawEvent`
// (camelCase) depending on the transport revision — tolerate both.
func decodeKimiRawEventEnvelope(p *agentadapter.InboundSendMsgPayload) (map[string]any, string, any, bool) {
	channelData := util.DecodeJSONObject(p.ChannelData)
	if len(channelData) == 0 {
		return nil, "", nil, false
	}
	acpEnvelope := util.NestedObject(channelData, "acp")
	if len(acpEnvelope) == 0 {
		return nil, "", nil, false
	}
	rawEvent := util.AsJSONObject(acpEnvelope["raw_event"])
	if len(rawEvent) == 0 {
		rawEvent = util.AsJSONObject(acpEnvelope["rawEvent"])
	}
	if len(rawEvent) == 0 {
		return nil, "", nil, false
	}
	eventType := strings.ToLower(util.FirstNonEmpty(
		util.NormalizeText(rawEvent["type"]),
		util.NormalizeText(rawEvent["event_type"]),
	))
	if eventType == "" {
		return nil, "", nil, false
	}
	return channelData, eventType, rawEvent["payload"], true
}

// synthesizeKimiRawApprovalPayload turns a permission_request payload into
// the exec_approval channel_data shape. Field-name mapping mirrors the ACP
// PermissionRequest structure the connector's acp-adapter emits:
//
//	{
//	  request_id, tool_call_id, tool_name, tool_title,
//	  tool_input, options: [ {kind:"allow_once"} , ... ]
//	}
//
// We derive:
//   - approval_command_id / approvalId — anchor for the user's later
//     `/approve <id> allow|deny` reply and for the pending-permission map
//     the connector uses to resolve `respondPermission(requestId, …)`.
//   - command — a single-line summary shown on the card.
//   - allowedDecisions — normalized to the exec-approval vocabulary
//     (allow-once / allow-always / deny) so the frontend surfaces the
//     right buttons.
func synthesizeKimiRawApprovalPayload(
	p *agentadapter.InboundSendMsgPayload,
	channelData map[string]any,
	rawPayload any,
) (*agentadapter.InboundSendMsgPayload, bool) {
	payload := util.AsJSONObject(rawPayload)
	if len(payload) == 0 {
		return nil, false
	}

	// The tool_call_id from ACP is the anchor for
	// pendingApprovals.get(toolCallId) inside `acp-adapter.ts`, so it must
	// round-trip verbatim back to the connector as `approval_command_id`.
	approvalCommandID := util.FirstNonEmpty(
		util.NormalizeText(payload["tool_call_id"]),
		util.NormalizeText(payload["toolCallId"]),
		util.NormalizeText(payload["approval_command_id"]),
		util.NormalizeText(payload["approvalCommandId"]),
		util.NormalizeText(payload["approval_id"]),
		util.NormalizeText(payload["approvalId"]),
		util.NormalizeText(payload["request_id"]),
		util.NormalizeText(payload["requestId"]),
	)
	if approvalCommandID == "" {
		return nil, false
	}

	approvalID := util.FirstNonEmpty(
		util.NormalizeText(payload["approval_id"]),
		util.NormalizeText(payload["approvalId"]),
	)
	if approvalID == "" {
		approvalID = approvalCommandID
	}
	approvalSlug := util.FirstNonEmpty(
		util.NormalizeText(payload["tool_name"]),
		util.NormalizeText(payload["toolName"]),
		util.NormalizeText(payload["tool_title"]),
		util.NormalizeText(payload["toolTitle"]),
	)
	if approvalSlug == "" {
		approvalSlug = approvalID
	}

	commandTitle := util.FirstNonEmpty(
		util.NormalizeText(payload["tool_title"]),
		util.NormalizeText(payload["toolTitle"]),
		util.NormalizeText(payload["tool_name"]),
		util.NormalizeText(payload["toolName"]),
	)
	commandInput := renderKimiRawInput(firstNonNil(
		payload["tool_input"],
		payload["toolInput"],
		payload["raw_input"],
		payload["rawInput"],
		payload["input"],
	))
	command := commandTitle
	switch {
	case commandTitle != "" && commandInput != "" && commandTitle != commandInput:
		command = truncateSingleLine(fmt.Sprintf("%s: %s", commandTitle, commandInput), 240)
	case command == "":
		command = truncateSingleLine(commandInput, 240)
	}
	if command == "" {
		command = "Kimi tool call"
	}

	replyMeta := map[string]any{
		"approvalId":       approvalID,
		"approvalSlug":     approvalSlug,
		"allowedDecisions": normalizeKimiAllowedDecisions(payload["options"]),
	}
	grixExecApproval := map[string]any{
		"approval_command_id": approvalCommandID,
		"command":             command,
		"host":                "Kimi ACP",
		"warning_text":        "Kimi requested approval before running this tool.",
	}

	normalizedChannelData := util.CloneJSONObject(channelData)
	if len(normalizedChannelData) == 0 {
		normalizedChannelData = map[string]any{}
	}
	normalizedChannelData["execApproval"] = replyMeta
	grixNamespace := util.CloneJSONObject(util.NestedObject(normalizedChannelData, "grix"))
	if len(grixNamespace) == 0 {
		grixNamespace = map[string]any{}
	}
	grixNamespace["execApproval"] = grixExecApproval
	normalizedChannelData["grix"] = grixNamespace

	channelDataRaw, _ := json.Marshal(normalizedChannelData)
	result := &agentadapter.InboundSendMsgPayload{
		Content:     p.Content,
		Extra:       p.Extra,
		ChannelData: channelDataRaw,
	}
	agentadapter.MergeCardsIntoExtra(result)
	return result, true
}

// normalizeKimiAllowedDecisions maps ACP `options[].kind` values into the
// exec-approval decision vocabulary the frontend and the backend
// `parseExecApprovalCommand` router share. Unknown / empty options fall
// back to the standard trio so a card is always actionable.
func normalizeKimiAllowedDecisions(value any) []string {
	seen := map[string]struct{}{}
	decisions := make([]string, 0, 3)

	rawItems, _ := value.([]any)
	for _, raw := range rawItems {
		var decision string
		switch typed := raw.(type) {
		case map[string]any:
			decision = mapKimiDecision(util.FirstNonEmpty(
				util.NormalizeText(typed["kind"]),
				util.NormalizeText(typed["name"]),
				util.NormalizeText(typed["optionId"]),
				util.NormalizeText(typed["option_id"]),
			))
		case string:
			decision = mapKimiDecision(typed)
		}
		if decision == "" {
			continue
		}
		if _, exists := seen[decision]; exists {
			continue
		}
		seen[decision] = struct{}{}
		decisions = append(decisions, decision)
	}
	if len(decisions) == 0 {
		return []string{"allow-once", "allow-always", "deny"}
	}
	return decisions
}

func mapKimiDecision(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "allow_once", "allow-once":
		return "allow-once"
	case "allow_always", "allow-always":
		return "allow-always"
	case "allow":
		return "allow-once"
	case "reject_once", "reject_always", "reject-once", "reject-always", "reject", "deny":
		return "deny"
	default:
		return ""
	}
}

// renderKimiRawInput squashes ACP `tool_input` into a compact single-line
// string suitable for the exec_approval card body. Kept small on purpose —
// the intent is a glanceable summary, not a full argument dump.
func renderKimiRawInput(rawInput any) string {
	switch value := rawInput.(type) {
	case string:
		return strings.TrimSpace(value)
	case map[string]any:
		if command := util.NormalizeText(value["command"]); command != "" {
			return command
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(encoded))
	default:
		return ""
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func truncateSingleLine(value string, limit int) string {
	compact := strings.Join(strings.Fields(value), " ")
	if len(compact) <= limit {
		return compact
	}
	end := limit - 3
	if end < 0 {
		end = 0
	}
	return strings.TrimSpace(compact[:end]) + "..."
}

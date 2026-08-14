package agentapi

// This file handles structured message payload parsing, including attachments,
// business cards (biz_card), and channel data (channel_data) extraction from
// the extra field.
//
// Note: structured fields still live on DelegateEventPayload because the queue
// and retry layer persists that shape. The dispatch path now converts them into
// agentadapter.DomainOutboundEvent before calling NormalizeOutbound, so adapters
// can own the final wire format without changing the persisted queue contract.

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/ws/threadmeta"
)

type AttachmentPayload struct {
	AttachmentType string `json:"attachment_type,omitempty"`
	MediaURL       string `json:"media_url,omitempty"`
	FileName       string `json:"file_name,omitempty"`
	ContentType    string `json:"content_type,omitempty"`
}

type messageExtraEnvelope struct {
	Attachments []AttachmentPayload `json:"attachments"`
	BizCard     json.RawMessage     `json:"biz_card"`
	ChannelData json.RawMessage     `json:"channel_data"`
}

func ApplyStructuredMessagePayload(event *DelegateEventPayload, msgType int16, extraRaw json.RawMessage) {
	if event == nil {
		return
	}
	if msgType > 0 {
		event.MsgType = msgType
	}

	extra := cloneStructuredRawMessage(extraRaw)
	if len(extra) == 0 {
		return
	}
	event.Extra = extra
	if threadID := threadmeta.Extract(extra); threadID != "" {
		event.ThreadID = threadID
	}

	var envelope messageExtraEnvelope
	if err := json.Unmarshal(extra, &envelope); err != nil {
		return
	}

	if attachments := normalizeAttachmentPayloads(envelope.Attachments); len(attachments) > 0 {
		event.Attachments = attachments
	}

	event.BizCard = cloneStructuredRawMessage(envelope.BizCard)
	event.ChannelData = cloneStructuredRawMessage(envelope.ChannelData)
}

func cloneStructuredRawMessage(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return nil
	}
	return append(json.RawMessage(nil), trimmed...)
}

func MergeMediaURLIntoExtra(extraRaw json.RawMessage, mediaURL string) json.RawMessage {
	normalizedMediaURL := strings.TrimSpace(mediaURL)
	if normalizedMediaURL == "" {
		return cloneStructuredRawMessage(extraRaw)
	}

	envelope := map[string]any{}
	if len(extraRaw) > 0 {
		if err := json.Unmarshal(extraRaw, &envelope); err != nil {
			envelope = map[string]any{}
		}
	}

	if currentMediaURL := strings.TrimSpace(normalizeStringValue(envelope["media_url"])); currentMediaURL == "" {
		envelope["media_url"] = normalizedMediaURL
	}

	attachments := normalizeAttachmentListValue(envelope["attachments"])
	if !attachmentListContainsMediaURL(attachments, normalizedMediaURL) {
		attachments = append(attachments, map[string]any{
			"media_url": normalizedMediaURL,
		})
	}
	envelope["attachments"] = attachments

	encoded, err := json.Marshal(envelope)
	if err != nil {
		return cloneStructuredRawMessage(extraRaw)
	}
	return encoded
}

func normalizeAttachmentPayloads(items []AttachmentPayload) []AttachmentPayload {
	if len(items) == 0 {
		return nil
	}
	normalized := make([]AttachmentPayload, 0, len(items))
	for _, item := range items {
		if attachment, ok := normalizeAttachmentPayload(item); ok {
			normalized = append(normalized, attachment)
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func normalizeAttachmentPayload(item AttachmentPayload) (AttachmentPayload, bool) {
	normalized := AttachmentPayload{
		AttachmentType: strings.TrimSpace(item.AttachmentType),
		MediaURL:       strings.TrimSpace(item.MediaURL),
		FileName:       strings.TrimSpace(item.FileName),
		ContentType:    strings.TrimSpace(item.ContentType),
	}
	if normalized.AttachmentType == "" &&
		normalized.MediaURL == "" &&
		normalized.FileName == "" &&
		normalized.ContentType == "" {
		return AttachmentPayload{}, false
	}
	return normalized, true
}

func normalizeAttachmentListValue(value any) []map[string]any {
	rawList, ok := value.([]any)
	if !ok {
		return nil
	}

	normalized := make([]map[string]any, 0, len(rawList))
	for _, item := range rawList {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		mediaURL := strings.TrimSpace(normalizeStringValue(object["media_url"]))
		if mediaURL == "" {
			continue
		}
		attachment := map[string]any{
			"media_url": mediaURL,
		}
		if attachmentType := strings.TrimSpace(normalizeStringValue(object["attachment_type"])); attachmentType != "" {
			attachment["attachment_type"] = attachmentType
		}
		if fileName := strings.TrimSpace(normalizeStringValue(object["file_name"])); fileName != "" {
			attachment["file_name"] = fileName
		}
		if contentType := strings.TrimSpace(normalizeStringValue(object["content_type"])); contentType != "" {
			attachment["content_type"] = contentType
		}
		normalized = append(normalized, attachment)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func attachmentListContainsMediaURL(items []map[string]any, mediaURL string) bool {
	for _, item := range items {
		if strings.TrimSpace(normalizeStringValue(item["media_url"])) == mediaURL {
			return true
		}
	}
	return false
}

func normalizeStringValue(value any) string {
	typed, ok := value.(string)
	if !ok {
		return ""
	}
	return typed
}

func buildDomainAttachmentPayloads(items []AttachmentPayload) []agentadapter.AttachmentPayload {
	if len(items) == 0 {
		return nil
	}
	normalized := make([]agentadapter.AttachmentPayload, 0, len(items))
	for _, item := range items {
		attachment, ok := normalizeAttachmentPayload(item)
		if !ok {
			continue
		}
		normalized = append(normalized, agentadapter.AttachmentPayload{
			AttachmentType: attachment.AttachmentType,
			MediaURL:       attachment.MediaURL,
			FileName:       attachment.FileName,
			ContentType:    attachment.ContentType,
		})
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

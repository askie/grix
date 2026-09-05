package push

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
)

// signPushMediaURL turns a stored media URL into a link the iOS notification
// service extension can fetch anonymously. Media objects live in a private
// bucket, so the bare stored URL is not downloadable; SignMediaURL leaves
// foreign URLs untouched. It is a variable so tests can run without OSS.
var signPushMediaURL = service.SignMediaURL

// markdownImageURLPattern matches the inline image an image message carries in
// its content, e.g. `![image](<https://host/key.jpg>)`.
var markdownImageURLPattern = regexp.MustCompile(`!\[[^\]]*\]\(\s*<?(https?://[^\s<>)]+)>?\s*\)`)

// pushImageExtra is the slice of a message's extra blob that can carry the
// image URL: the top-level media_url written by the agent API, and the
// attachments list written by the clients.
type pushImageExtra struct {
	MediaURL    string `json:"media_url"`
	Attachments []struct {
		MediaURL       string `json:"media_url"`
		AttachmentType string `json:"attachment_type"`
		ContentType    string `json:"content_type"`
	} `json:"attachments"`
}

// resolvePushImageURL picks the image an image message shows and returns a
// signed https URL for it, or "" when no usable URL exists. Callers must treat
// "" as "send the text-only push".
func resolvePushImageURL(extra json.RawMessage, content string) string {
	raw := rawImageURLFromExtra(extra)
	if raw == "" {
		raw = rawImageURLFromContent(content)
	}
	if raw == "" {
		return ""
	}

	signed := strings.TrimSpace(signPushMediaURL(raw))
	// Only a public https link is fetchable from the notification extension,
	// which runs without the app's credentials.
	if !strings.HasPrefix(signed, "https://") {
		return ""
	}
	return signed
}

func rawImageURLFromExtra(extra json.RawMessage) string {
	if len(strings.TrimSpace(string(extra))) == 0 {
		return ""
	}
	var envelope pushImageExtra
	if err := json.Unmarshal(extra, &envelope); err != nil {
		return ""
	}
	if url := strings.TrimSpace(envelope.MediaURL); url != "" {
		return url
	}
	for _, attachment := range envelope.Attachments {
		url := strings.TrimSpace(attachment.MediaURL)
		if url == "" {
			continue
		}
		if isImageAttachment(attachment.AttachmentType, attachment.ContentType) {
			return url
		}
	}
	return ""
}

func isImageAttachment(attachmentType, contentType string) bool {
	if strings.EqualFold(strings.TrimSpace(attachmentType), "image") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "image/")
}

func rawImageURLFromContent(content string) string {
	match := markdownImageURLPattern.FindStringSubmatch(content)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

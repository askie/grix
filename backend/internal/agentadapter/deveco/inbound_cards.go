package deveco

import (
	"net/url"

	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
)

// The connector's own internal channel/adapter machinery for deveco sessions
// stays tagged "opencode" (grix-connector reuses opencode-adapter.ts wholesale;
// see the package doc in adapter.go), so channel_data still nests this under
// "opencode", not "deveco" — mirrors opencode/inbound_cards.go on purpose.
func buildDevecoSessionBindingCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "opencode"), "sessionBinding")
	if len(record) == 0 {
		return "", false
	}

	status := util.NormalizeText(record["status"])
	reason := util.NormalizeText(record["reason"])
	errorCode := util.NormalizeText(record["error_code"])
	if status != "missing" && reason != "binding_missing" && errorCode != "session_binding_missing" {
		return "", false
	}

	payload := map[string]any{
		"summary_text": "当前对话还没有打开工作目录。",
		"detail_text":  "先提交一个工作目录。校验通过后，DevEco Code 会自动继续处理刚才那条消息。",
	}
	if initialCwd := util.FirstNonEmpty(
		util.NormalizeText(record["initial_cwd"]),
		util.NormalizeText(record["initialCwd"]),
	); initialCwd != "" {
		payload["initial_cwd"] = initialCwd
	}

	return buildDevecoCardLink(
		"[Open Workspace] 当前对话还没有打开工作目录。",
		"agent_open_session",
		payload,
	), true
}

func buildDevecoCardLink(fallbackText, cardType string, payload map[string]any) string {
	return "[" + fallbackText + "](" + buildDevecoCardURI(cardType, payload) + ")"
}

func buildDevecoCardURI(cardType string, payload map[string]any) string {
	u := url.URL{Scheme: "grix", Host: "card", Path: cardType}
	q := u.Query()
	for k, v := range payload {
		text := util.NormalizeText(v)
		if text == "" {
			continue
		}
		q.Set(k, text)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

package agentapi

import "strings"

const (
	MirrorModeRecordOnly       = "record_only"
	MirrorModeRecordAndProcess = "record_and_process"
)

func (evt DelegateEventPayload) IsRecordOnly() bool {
	return strings.EqualFold(strings.TrimSpace(evt.MirrorMode), MirrorModeRecordOnly)
}

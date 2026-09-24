package fold

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// A compound message row is a message.upsert whose payload also embeds the
// session projection and the unread state that the same write moved. Each
// embedded part reserves its own stream cursor, so the row always expands
// back into the classic rows at the cursors they would have had.
const (
	sessionKey = "session"
	unreadKey  = "unread"
)

var (
	sessionKeyToken = []byte(`"` + sessionKey + `"`)
	unreadKeyToken  = []byte(`"` + unreadKey + `"`)
	jsonNull        = []byte("null")
)

// CompoundPayload embeds session and unread (either may be empty) into a
// message.upsert payload. The writer and the backtest build compound rows
// through this one function.
func CompoundPayload(message, session, unread json.RawMessage) (json.RawMessage, error) {
	fields, err := objectFields(message)
	if err != nil {
		return nil, err
	}
	for _, embedded := range []json.RawMessage{session, unread} {
		if len(embedded) == 0 {
			continue
		}
		if _, _, err := embeddedEntity(embedded); err != nil {
			return nil, err
		}
	}
	if _, exists := fields[sessionKey]; exists {
		return nil, errors.New("fold: message payload already has a session field")
	}
	if _, exists := fields[unreadKey]; exists {
		return nil, errors.New("fold: message payload already has an unread field")
	}
	return joinCompound(fields, session, unread)
}

// Span is the number of cursors a message.upsert payload occupies: one for
// the message plus one per embedded part. It fails when an embedded part
// lacks the session_id or state_version of its classic row: clients apply
// every part behind its own version barrier.
func Span(payload json.RawMessage) (int, error) {
	split, ok := splitCompound(payload)
	if !ok {
		return 1, nil
	}
	for _, embedded := range []json.RawMessage{split.session, split.unread} {
		if embedded == nil {
			continue
		}
		if _, _, err := embeddedEntity(embedded); err != nil {
			return 0, err
		}
	}
	return split.partCount(), nil
}

type compound struct {
	// fields is the message object without the embedded parts.
	fields  map[string]json.RawMessage
	session json.RawMessage
	unread  json.RawMessage
}

func (c compound) partCount() int {
	count := 1
	if len(c.session) > 0 {
		count++
	}
	if len(c.unread) > 0 {
		count++
	}
	return count
}

// splitCompound separates the embedded parts of a message.upsert payload.
// Classic payloads are recognised by a byte scan, without decoding.
func splitCompound(payload json.RawMessage) (compound, bool) {
	if !bytes.Contains(payload, sessionKeyToken) && !bytes.Contains(payload, unreadKeyToken) {
		return compound{}, false
	}
	fields, err := objectFields(payload)
	if err != nil {
		return compound{}, false
	}
	session := present(fields[sessionKey])
	unread := present(fields[unreadKey])
	if session == nil && unread == nil {
		return compound{}, false
	}
	delete(fields, sessionKey)
	delete(fields, unreadKey)
	return compound{fields: fields, session: session, unread: unread}, true
}

func joinCompound(fields map[string]json.RawMessage, session, unread json.RawMessage) (json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(fields)+2)
	for key, value := range fields {
		out[key] = value
	}
	if len(session) > 0 {
		out[sessionKey] = session
	}
	if len(unread) > 0 {
		out[unreadKey] = unread
	}
	return json.Marshal(out)
}

func objectFields(payload json.RawMessage) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, fmt.Errorf("fold: message payload: %w", err)
	}
	if fields == nil {
		return nil, errors.New("fold: message payload is not an object")
	}
	return fields, nil
}

func present(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), jsonNull) {
		return nil
	}
	return raw
}

// embeddedEntity reads the entity id and version an embedded part had as a
// classic row: both payloads carry session_id and state_version.
func embeddedEntity(raw json.RawMessage) (string, int64, error) {
	var ref struct {
		SessionID    string          `json:"session_id"`
		StateVersion json.RawMessage `json:"state_version"`
	}
	if err := json.Unmarshal(raw, &ref); err != nil {
		return "", 0, fmt.Errorf("fold: embedded part: %w", err)
	}
	if strings.TrimSpace(ref.SessionID) == "" {
		return "", 0, errors.New("fold: embedded part has no session_id")
	}
	text := strings.TrimSpace(string(ref.StateVersion))
	if unquoted, err := strconv.Unquote(text); err == nil {
		text = unquoted
	}
	version, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("fold: embedded part state_version %q: %w", text, err)
	}
	return ref.SessionID, version, nil
}

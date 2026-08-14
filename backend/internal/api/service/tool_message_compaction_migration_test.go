package service

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/toolcard"
	"gorm.io/datatypes"
)

func TestRunToolMessageCompactionMigrationCompactsHistoricalRows(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := store.DB
	store.DB = testDB.DB
	defer func() { store.DB = originalDB }()

	successContent := toolcard.BuildExecutionCardURI(map[string]any{
		"summary_text": strings.Repeat("读取文件", 180),
		"detail_text":  strings.Repeat("successful raw output ", 1000),
	})
	failureContent := toolcard.BuildExecutionCardURI(map[string]any{
		"summary_text": "Bash failed",
		"detail_text":  "error: " + strings.Repeat("失败详情", 1000),
	})
	groupContent := buildHistoricalToolGroupCard(t, map[string]any{
		"count": 2,
		"children": []map[string]any{
			{
				"summary_text": "Read: file.go",
				"detail_text":  strings.Repeat("successful group raw output ", 1000),
			},
			{
				"summary_text": "Bash failed",
				"detail_text":  "error: " + strings.Repeat("group failure ", 1000),
			},
		},
	})
	rawExtra := datatypes.JSON(`{
		"channel_data":{
			"grix":{"toolExecution":{"summary_text":"duplicate","detail_text":"duplicate output"}},
			"hermes":{"raw_event":{"tool_input":{"command":"cat secret"},"tool_result":"raw result"}}
		},
		"biz_card":{"type":"tool_execution","payload":{"raw_output":"huge"}},
		"thread_id":"thread-1",
		"mention_user_ids":["1001"],
		"attachments":[{
			"media_url":"https://cdn.example.com/result.txt",
			"file_name":"result.txt",
			"content_type":"text/plain",
			"attachment_type":"file",
			"raw_output":"must be removed"
		}]
	}`)
	rows := []model.Message{
		{
			MsgID:      1001,
			SessionID:  "session-success",
			SenderID:   2001,
			SenderType: 2,
			Content:    successContent,
			Extra:      rawExtra,
		},
		{
			MsgID:      1002,
			SessionID:  "session-failure",
			SenderID:   2001,
			SenderType: 2,
			Content:    failureContent,
			Extra:      rawExtra,
		},
		{
			MsgID:      1003,
			SessionID:  "session-group",
			SenderID:   2001,
			SenderType: 2,
			Content:    groupContent,
			Extra:      rawExtra,
		},
		{
			MsgID:      1004,
			SessionID:  "session-normal",
			SenderID:   2001,
			SenderType: 2,
			Content:    "普通消息，不应修改",
			Extra:      datatypes.JSON(`{"raw_output":"belongs to a normal message"}`),
		},
	}
	if err := testDB.DB.Create(&rows).Error; err != nil {
		t.Fatalf("seed messages: %v", err)
	}

	if err := RunToolMessageCompactionMigration(context.Background()); err != nil {
		t.Fatalf("RunToolMessageCompactionMigration() error = %v", err)
	}

	var success model.Message
	if err := testDB.DB.Where("msg_id = ? AND session_id = ?", 1001, "session-success").
		First(&success).Error; err != nil {
		t.Fatalf("load compacted success message: %v", err)
	}
	successMeta, ok := toolcard.ExtractMetadata(success.Content, json.RawMessage(success.Extra))
	if !ok {
		t.Fatalf("success content is not a valid tool card: %q", success.Content)
	}
	if got := utf8.RuneCountInString(successMeta.SummaryText); got > toolcard.SummaryMaxRunes {
		t.Fatalf("success summary runes=%d want <=%d", got, toolcard.SummaryMaxRunes)
	}
	if successMeta.DetailText != "" {
		t.Fatalf("successful tool detail should be removed, got %q", successMeta.DetailText)
	}
	assertCompactedHistoricalExtra(t, success.Extra)

	var failure model.Message
	if err := testDB.DB.Where("msg_id = ? AND session_id = ?", 1002, "session-failure").
		First(&failure).Error; err != nil {
		t.Fatalf("load compacted failure message: %v", err)
	}
	failureMeta, ok := toolcard.ExtractMetadata(failure.Content, json.RawMessage(failure.Extra))
	if !ok || !failureMeta.Failed {
		t.Fatalf("failure metadata=%+v ok=%v", failureMeta, ok)
	}
	if len(failureMeta.DetailText) > toolcard.FailureDetailMaxBytes {
		t.Fatalf(
			"failure detail bytes=%d want <=%d",
			len(failureMeta.DetailText),
			toolcard.FailureDetailMaxBytes,
		)
	}
	assertCompactedHistoricalExtra(t, failure.Extra)

	var group model.Message
	if err := testDB.DB.Where("msg_id = ? AND session_id = ?", 1003, "session-group").
		First(&group).Error; err != nil {
		t.Fatalf("load compacted group message: %v", err)
	}
	if len(group.Content) > toolcard.GroupMaxBytes {
		t.Fatalf("group content bytes=%d want <=%d", len(group.Content), toolcard.GroupMaxBytes)
	}
	groupPayload := decodeHistoricalToolGroupCard(t, group.Content)
	groupChildren, _ := groupPayload["children"].([]any)
	if len(groupChildren) != 2 {
		t.Fatalf("group children=%d want=2 payload=%#v", len(groupChildren), groupPayload)
	}
	firstChild, _ := groupChildren[0].(map[string]any)
	if _, exists := firstChild["detail_text"]; exists {
		t.Fatalf("successful group child retained detail: %#v", firstChild)
	}
	secondChild, _ := groupChildren[1].(map[string]any)
	if detail, _ := secondChild["detail_text"].(string); len(detail) > toolcard.FailureDetailMaxBytes {
		t.Fatalf("group failure detail bytes=%d want <=%d", len(detail), toolcard.FailureDetailMaxBytes)
	}
	assertCompactedHistoricalExtra(t, group.Extra)

	var normal model.Message
	if err := testDB.DB.Where("msg_id = ? AND session_id = ?", 1004, "session-normal").
		First(&normal).Error; err != nil {
		t.Fatalf("load normal message: %v", err)
	}
	if normal.Content != rows[3].Content || string(normal.Extra) != string(rows[3].Extra) {
		t.Fatalf("normal message changed: content=%q extra=%s", normal.Content, normal.Extra)
	}

	var markerCount int64
	if err := testDB.DB.Table("data_migrations").
		Where("name = ?", toolMessageCompactionMigrationName).
		Count(&markerCount).Error; err != nil {
		t.Fatalf("count migration marker: %v", err)
	}
	if markerCount != 1 {
		t.Fatalf("migration marker count=%d want=1", markerCount)
	}

	beforeContent := success.Content
	stats, applied, err := runToolMessageCompactionMigration(context.Background(), testDB.DB)
	if err != nil {
		t.Fatalf("rerun migration: %v", err)
	}
	if applied || stats != (toolMessageCompactionStats{}) {
		t.Fatalf("rerun applied=%v stats=%+v want already-applied no-op", applied, stats)
	}
	var rerun model.Message
	if err := testDB.DB.Where("msg_id = ? AND session_id = ?", 1001, "session-success").
		First(&rerun).Error; err != nil {
		t.Fatalf("reload rerun message: %v", err)
	}
	if rerun.Content != beforeContent {
		t.Fatal("rerun changed already compacted content")
	}
}

func buildHistoricalToolGroupCard(t *testing.T, payload map[string]any) string {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal group payload: %v", err)
	}
	values := url.Values{}
	values.Set("d", string(data))
	cardURI := (&url.URL{
		Scheme:   "grix",
		Host:     "card",
		Path:     "/tool_execution_group",
		RawQuery: values.Encode(),
	}).String()
	return "[Tools] historical(" + cardURI + ")"
}

func decodeHistoricalToolGroupCard(t *testing.T, content string) map[string]any {
	t.Helper()
	const marker = "grix://card/tool_execution_group?"
	index := strings.Index(content, marker)
	if index < 0 {
		t.Fatalf("missing group card marker: %q", content)
	}
	end := strings.IndexByte(content[index:], ')')
	if end < 0 {
		end = len(content)
	} else {
		end += index
	}
	parsed, err := url.Parse(content[index:end])
	if err != nil {
		t.Fatalf("parse group URI: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(parsed.Query().Get("d")), &payload); err != nil {
		t.Fatalf("decode group payload: %v", err)
	}
	return payload
}

func assertCompactedHistoricalExtra(t *testing.T, extra datatypes.JSON) {
	t.Helper()
	text := string(extra)
	for _, removed := range []string{
		"raw_event",
		"tool_input",
		"tool_result",
		"raw_output",
		"biz_card",
		"duplicate output",
	} {
		if strings.Contains(text, removed) {
			t.Fatalf("compacted extra retained %q: %s", removed, text)
		}
	}
	for _, retained := range []string{
		`"compacted":true`,
		`"thread_id":"thread-1"`,
		`"mention_user_ids":["1001"]`,
		`"media_url":"https://cdn.example.com/result.txt"`,
	} {
		if !strings.Contains(text, retained) {
			t.Fatalf("compacted extra missing %s: %s", retained, text)
		}
	}
}

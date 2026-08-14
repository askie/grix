package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
)

func TestBuildMessageEditPayloadIncludesThreadID(t *testing.T) {
	msg := model.Message{
		MsgID:           9001,
		SessionID:       "edit-session",
		ThreadID:        "topic-edit-a",
		SenderID:        1001,
		SenderType:      2,
		MsgType:         1,
		Content:         "edited",
		QuotedMessageID: 18889990001,
		CreatedAt:       time.Unix(1700000000, 0).UTC(),
	}

	payload := buildMessageEditPayload(msg, 2, 88)
	if payload.ThreadID != "topic-edit-a" {
		t.Fatalf("thread_id=%q want=topic-edit-a", payload.ThreadID)
	}
}

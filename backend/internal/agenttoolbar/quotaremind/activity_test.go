package quotaremind_test

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agenttoolbar/quotaremind"
)

func TestDBSessionActivity_NilDBFailsClosed(t *testing.T) {
	activity := quotaremind.NewDBSessionActivity()
	ok, err := activity.HasRecentConversation(context.Background(), "sess-1", time.Hour)
	if err != nil {
		t.Fatalf("err=%v want nil", err)
	}
	if ok {
		t.Fatal("nil DB must fail closed (no recent conversation)")
	}
}

func TestDBSessionActivity_EmptySessionFailsClosed(t *testing.T) {
	activity := quotaremind.NewDBSessionActivity()
	ok, err := activity.HasRecentConversation(context.Background(), "  ", time.Hour)
	if err != nil {
		t.Fatalf("err=%v want nil", err)
	}
	if ok {
		t.Fatal("empty session id must fail closed")
	}
}

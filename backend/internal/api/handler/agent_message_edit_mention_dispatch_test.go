package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// TestAgentMessageEdit_NewAgentMentionDispatchesThroughFullStack exercises the
// real HTTP entry point (POST /agent-api/messages/edit) end to end: the
// editor is a session member agent, the edit adds a brand-new @mention of a
// second agent in the same group, and the edit must both (a) succeed as an
// ordinary edit and (b) claim exactly one mention-dispatch receipt for the
// newly mentioned agent — proving the HTTP handler is wired to
// ws/handler.DispatchMessageEditMentionAdditions, not just the WS bridge
// path. No wsagentapi manager is configured in this test (an HTTP request
// has no WS hub on hand), so delivery itself falls back to the offline
// delegate-event queue — this also demonstrates that a delivery-layer
// shortfall never blocks or fails the HTTP response (see
// message_edit_service.go's contract: EditMessage's own commit decides the
// response; mention dispatch runs strictly after and is best-effort).
func TestAgentMessageEdit_NewAgentMentionDispatchesThroughFullStack(t *testing.T) {
	logger.Init()
	r, testDB, cleanup := setupAgentMessageHandlerTest(t)
	defer cleanup()

	const (
		ownerID       = int64(21020)
		editorAgentID = int64(31020)
		targetAgentID = int64(31021)
		apiKey        = "ak_test_agent_message_edit_mention"
		sessionID     = "agent-edit-mention-group"
		msgID         = int64(90020001)
	)
	seedAgentAPIAuthData(t, testDB, ownerID, editorAgentID, apiKey)
	seedAgentMessageGroup(t, testDB, ownerID, sessionID, model.SessionModerationStatusActive)

	now := time.Now().UTC()
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     editorAgentID,
		MemberType:   2,
		JoinedAt:     now,
		LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("seed editor agent member error: %v", err)
	}
	if err := testDB.DB.Create(&model.Agent{
		ID:           targetAgentID,
		OwnerID:      ownerID,
		AgentName:    "target_agent",
		ProviderType: model.AgentProviderAPI,
		Status:       1,
	}).Error; err != nil {
		t.Fatalf("seed target agent error: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     targetAgentID,
		MemberType:   2,
		JoinedAt:     now,
		LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("seed target agent member error: %v", err)
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   editorAgentID,
		SenderType: 2,
		MsgType:    model.MsgTypeText,
		Content:    "handing this off",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("seed message error: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"session_id": sessionID,
		"msg_id":     fmt.Sprintf("%d", msgID),
		"content":    fmt.Sprintf("handing this off to @%d", targetAgentID),
	})

	req, _ := http.NewRequest(http.MethodPost, "/agent-api/messages/edit", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var msg model.Message
	if err := store.DB.Where("msg_id = ? AND session_id = ?", msgID, sessionID).First(&msg).Error; err != nil {
		t.Fatalf("reload message error: %v", err)
	}
	if msg.Content == "handing this off" {
		t.Fatalf("content was not updated")
	}

	var receiptCount int64
	if err := store.DB.Model(&model.MessageMentionDispatchReceipt{}).
		Where("msg_id = ? AND member_id = ?", msgID, targetAgentID).
		Count(&receiptCount).Error; err != nil {
		t.Fatalf("count receipt error: %v", err)
	}
	if receiptCount != 1 {
		t.Fatalf("receipt count=%d want=1 (edit response succeeded regardless of delivery outcome)", receiptCount)
	}
}

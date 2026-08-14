package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	geminiadapter "github.com/askie/grix/backend/internal/agentadapter/gemini"
	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
	"github.com/askie/grix/backend/internal/geminisession"
	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const geminiAuthGuideURL = "https://geminicli.com/docs/get-started/authentication/"

func (m *Manager) tryHandleGeminiOpenSessionSubmit(evt DelegateEventPayload) bool {
	submit, matched, err := grixactions.ParseOpenSessionSubmit(evt.Content)
	if !matched || !m.isGeminiAgent(evt.AgentID) {
		return false
	}
	lang := ownerCardLanguage(evt.OwnerID)
	if err != nil {
		return m.sendGeminiStatusCard(evt, map[string]any{
			"category":    "session",
			"status":      "warning",
			"summary":     tooli18n.T(lang, "gemini_cwd_required"),
			"detail_text": tooli18n.T(lang, "gemini_cwd_required_detail"),
		})
	}

	pending, ok := loadGeminiPendingWorkspace(context.Background(), evt.AgentID, evt.SessionID)
	if !ok || pending == nil {
		return m.sendGeminiStatusCard(evt, map[string]any{
			"category":    "session",
			"status":      "warning",
			"summary":     tooli18n.T(lang, "gemini_workspace_not_pending"),
			"detail_text": tooli18n.T(lang, "gemini_workspace_not_pending_detail"),
		})
	}
	// NOTE: Do NOT delete pending workspace here. It will be cleaned up
	// after the session_control local_action succeeds (in sendOrUpdateBindingCardReply).
	// This keeps the workspace alive for retry scenarios (session_invalid_cwd).

	cwd := strings.TrimSpace(submit.Cwd)
	if cwd == "" {
		return m.sendGeminiStatusCard(evt, map[string]any{
			"category":    "session",
			"status":      "warning",
			"summary":     tooli18n.T(lang, "gemini_cwd_required"),
			"detail_text": tooli18n.T(lang, "gemini_cwd_required_detail"),
		})
	}

	// Persist mode/model to DB (CWD is no longer stored here).
	config := geminiadapter.ExtractSessionConfig(pending.Event.Extra)
	if err := geminisession.Upsert(context.Background(), geminisession.Snapshot{
		AgentID:   pending.Event.AgentID,
		SessionID: strings.TrimSpace(pending.Event.SessionID),
		ModeID:    config.ModeID,
		ModelID:   config.ModelID,
	}); err != nil {
		logger.L.Warnf("persist gemini session context failed agent=%d session=%s err=%v", pending.Event.AgentID, pending.Event.SessionID, err)
	}

	// Send session_control local_action to the plugin to bind the CWD.
	// 按事件发起者 evt.OwnerID 精确路由：被共享者 B 提交工作区时，绑定必须落到
	// B 的 connector 实例，而不是主人 A 的（owner 缺失的回退已在路由层移除）。
	actionID := fmt.Sprintf("session_control:%s:%d", strings.TrimSpace(evt.EventID), evt.MsgID)
	if strings.TrimSpace(evt.EventID) == "" {
		actionID = fmt.Sprintf("session_control:%s:%d:%d", evt.SessionID, evt.AgentID, evt.MsgID)
	}
	cardInstanceID := strings.TrimSpace(submit.CardInstanceID)
	if cardInstanceID == "" {
		cardInstanceID = loadOpenSessionCardInstanceID(
			context.Background(),
			evt.SessionID,
			evt.QuotedMessageID,
		)
	}
	m.sendLocalActionWithPendingForOwner(evt.AgentID, evt.OwnerID, protocol.LocalActionPayload{
		ActionID:   actionID,
		EventID:    evt.EventID,
		ActionType: "session_control",
		Params: map[string]any{
			"session_id": strings.TrimSpace(evt.SessionID),
			"verb":       "open",
			"cwd":        cwd,
		},
		TimeoutMs: 15_000,
	}, &pendingLocalAction{
		actionID:         actionID,
		kind:             "session_control",
		agentID:          evt.AgentID,
		ownerID:          evt.OwnerID,
		sessionID:        evt.SessionID,
		threadID:         evt.ThreadID,
		quotedMessageID:  evt.MsgID,
		actionType:       "session_control",
		referenceID:      "open",
		cardInstanceID:   cardInstanceID,
		submittedPath:    cwd,
		bindingCardMsgID: loadBindingCardMsgID(context.Background(), evt.AgentID, evt.SessionID),
		geminiCleanup:    true,
	})

	// Replay the pending event with CWD merged into the payload.
	replay := pending.Event
	replay.EventID = strings.TrimSpace(evt.EventID)
	replay.Extra = geminiadapter.MergeSessionConfig(replay.Extra, geminiadapter.SessionConfig{
		Cwd:     cwd,
		ModeID:  config.ModeID,
		ModelID: config.ModelID,
	})
	if m.PushDelegateEvent(replay) {
		return true
	}

	return m.sendGeminiStatusCard(pending.Event, map[string]any{
		"category":    "session",
		"status":      "error",
		"summary":     tooli18n.T(lang, "gemini_retry_failed"),
		"detail_text": tooli18n.T(lang, "gemini_retry_failed_detail"),
	})
}

func (m *Manager) tryHandleGeminiQuestionReply(evt DelegateEventPayload) bool {
	reply, matched, err := grixactions.ParseQuestionReply(evt.Content)
	if !matched || !m.isGeminiAgent(evt.AgentID) {
		return false
	}
	lang := ownerCardLanguage(evt.OwnerID)
	if err != nil {
		return m.sendGeminiStatusCard(evt, map[string]any{
			"category":    "question",
			"status":      "warning",
			"summary":     tooli18n.T(lang, "gemini_reply_invalid"),
			"detail_text": tooli18n.T(lang, "gemini_reply_invalid_detail"),
		})
	}

	pending, ok := loadGeminiPendingRequest(context.Background(), reply.RequestID)
	if !ok || pending == nil || pending.Event.AgentID != evt.AgentID {
		return m.sendGeminiStatusCard(evt, map[string]any{
			"category":     "question",
			"status":       "warning",
			"summary":      tooli18n.T(lang, "gemini_question_not_pending"),
			"detail_text":  tooli18n.T(lang, "gemini_question_not_pending_detail"),
			"reference_id": strings.TrimSpace(reply.RequestID),
		})
	}
	deleteGeminiPendingRequest(context.Background(), reply.RequestID)

	switch pending.Kind {
	case geminiInteractionKindAuth:
		return m.handleGeminiAuthReply(pending, reply, evt.EventID)
	case geminiInteractionKindQuestion:
		return m.handleGeminiFormQuestionReply(pending, reply, evt.EventID)
	default:
		return m.sendGeminiStatusCard(pending.Event, map[string]any{
			"category":     "question",
			"status":       "warning",
			"summary":      tooli18n.T(lang, "gemini_reply_invalid"),
			"reference_id": strings.TrimSpace(reply.RequestID),
		})
	}
}

func (m *Manager) handleGeminiAuthReply(
	pending *geminiPendingInteraction,
	reply grixactions.QuestionReply,
	replayEventID string,
) bool {
	if pending == nil {
		return false
	}
	lang := ownerCardLanguage(pending.Event.OwnerID)
	if strings.TrimSpace(reply.Action) == "" {
		return m.sendGeminiStatusCard(pending.Event, map[string]any{
			"category":     "question",
			"status":       "warning",
			"summary":      tooli18n.T(lang, "gemini_auth_action_unsupported"),
			"reference_id": strings.TrimSpace(reply.RequestID),
		})
	}
	switch strings.TrimSpace(reply.Action) {
	case "cancel":
		return m.sendGeminiStatusCard(pending.Event, map[string]any{
			"category":     "question",
			"status":       "warning",
			"summary":      tooli18n.T(lang, "gemini_auth_cancelled"),
			"detail_text":  tooli18n.T(lang, "gemini_auth_cancelled_detail"),
			"reference_id": strings.TrimSpace(reply.RequestID),
		})
	case "accept":
		if !m.sendGeminiStatusCard(pending.Event, map[string]any{
			"category":     "question",
			"status":       "info",
			"summary":      tooli18n.T(lang, "gemini_auth_retrying"),
			"detail_text":  tooli18n.T(lang, "gemini_auth_retrying_detail"),
			"reference_id": strings.TrimSpace(reply.RequestID),
		}) {
			return false
		}
		replay := pending.Event
		replay.EventID = strings.TrimSpace(replayEventID)
		if m.PushDelegateEvent(replay) {
			return true
		}
		return m.sendGeminiStatusCard(pending.Event, map[string]any{
			"category":     "question",
			"status":       "error",
			"summary":      tooli18n.T(lang, "gemini_retry_not_scheduled"),
			"detail_text":  tooli18n.T(lang, "gemini_retry_not_scheduled_detail"),
			"reference_id": strings.TrimSpace(reply.RequestID),
		})
	default:
		return m.sendGeminiStatusCard(pending.Event, map[string]any{
			"category":     "question",
			"status":       "warning",
			"summary":      tooli18n.T(lang, "gemini_reply_invalid"),
			"reference_id": strings.TrimSpace(reply.RequestID),
		})
	}
}

func (m *Manager) handleGeminiFormQuestionReply(
	pending *geminiPendingInteraction,
	reply grixactions.QuestionReply,
	replayEventID string,
) bool {
	if pending == nil {
		return false
	}

	lang := ownerCardLanguage(pending.Event.OwnerID)
	requestID := strings.TrimSpace(reply.RequestID)
	if action := strings.TrimSpace(reply.Action); action != "" {
		if action == "cancel" {
			return m.sendGeminiStatusCard(pending.Event, map[string]any{
				"category":     "question",
				"status":       "warning",
				"summary":      tooli18n.T(lang, "gemini_question_cancelled"),
				"detail_text":  tooli18n.T(lang, "gemini_question_cancelled_detail"),
				"reference_id": requestID,
			})
		}
		return m.sendGeminiStatusCard(pending.Event, map[string]any{
			"category":     "question",
			"status":       "warning",
			"summary":      tooli18n.T(lang, "gemini_answer_invalid"),
			"detail_text":  tooli18n.T(lang, "gemini_answer_invalid_detail"),
			"reference_id": requestID,
		})
	}
	if len(reply.Response) == 0 {
		return m.sendGeminiStatusCard(pending.Event, map[string]any{
			"category":     "question",
			"status":       "warning",
			"summary":      tooli18n.T(lang, "gemini_answer_required"),
			"detail_text":  tooli18n.T(lang, "gemini_answer_required_detail"),
			"reference_id": requestID,
		})
	}

	answerText := buildGeminiQuestionAnswerText(pending.Event, reply.Response)
	if answerText == "" {
		return m.sendGeminiStatusCard(pending.Event, map[string]any{
			"category":     "question",
			"status":       "warning",
			"summary":      tooli18n.T(lang, "gemini_answer_apply_failed"),
			"detail_text":  tooli18n.T(lang, "gemini_answer_apply_failed_detail"),
			"reference_id": requestID,
		})
	}

	replay := stripGeminiQuestionCard(pending.Event)
	replay.EventID = strings.TrimSpace(replayEventID)
	replay.Extra = geminiadapter.AppendPromptText(replay.Extra, replay.Content, replay.ContextMessages, answerText)
	if !m.sendGeminiStatusCard(pending.Event, map[string]any{
		"category":     "question",
		"status":       "info",
		"summary":      tooli18n.T(lang, "gemini_answer_retrying"),
		"detail_text":  tooli18n.T(lang, "gemini_answer_retrying_detail"),
		"reference_id": requestID,
	}) {
		return false
	}
	if m.PushDelegateEvent(replay) {
		return true
	}
	return m.sendGeminiStatusCard(pending.Event, map[string]any{
		"category":     "question",
		"status":       "error",
		"summary":      tooli18n.T(lang, "gemini_retry_not_scheduled"),
		"detail_text":  tooli18n.T(lang, "gemini_retry_not_scheduled_detail"),
		"reference_id": requestID,
	})
}

func (m *Manager) tryHandleGeminiWorkspaceRequirement(evt DelegateEventPayload) bool {
	if evt.IsRecordOnly() || !m.isGeminiAgent(evt.AgentID) {
		return false
	}
	if strings.TrimSpace(evt.SessionID) == "" {
		return false
	}

	config := geminiadapter.ExtractSessionConfig(evt.Extra)
	if strings.TrimSpace(config.Cwd) != "" {
		return false
	}

	record := geminiPendingInteraction{
		Event: evt,
	}
	alreadyPending := false
	if existing, ok := loadGeminiPendingWorkspace(context.Background(), evt.AgentID, evt.SessionID); ok && existing != nil {
		record.CreatedAt = existing.CreatedAt
		alreadyPending = true
	}
	if !saveGeminiPendingWorkspace(context.Background(), record) {
		logger.L.Warnf("save gemini workspace interaction failed agent=%d session=%s", evt.AgentID, evt.SessionID)
		return false
	}
	if alreadyPending {
		return true
	}
	cardInstanceID := buildOpenSessionCardInstanceID(
		evt.AgentID,
		evt.SessionID,
		evt.EventID,
		evt.MsgID,
	)
	if m.sendGeminiCardMessage(evt, buildLocalGrixCardLink(
		"[Open Workspace] Gemini needs a workspace before it can continue.",
		"agent_open_session",
		map[string]any{
			"card_instance_id": cardInstanceID,
			"summary_text":     "Gemini needs a workspace before it can continue.",
			"detail_text":      "Choose the folder to open on the machine running grix-gemini, then Gemini will retry your last request automatically.",
			"initial_cwd":      strings.TrimSpace(config.Cwd),
		},
	), nil) {
		return true
	}
	deleteGeminiPendingWorkspace(context.Background(), evt.AgentID, evt.SessionID)
	return false
}

func (m *Manager) tryHandleGeminiQuestionRequirement(evt DelegateEventPayload) bool {
	if evt.IsRecordOnly() || !m.isGeminiAgent(evt.AgentID) {
		return false
	}

	cardPayload, question, ok := extractGeminiQuestionCard(evt)
	if !ok {
		return false
	}

	record := geminiPendingInteraction{
		Kind:      geminiInteractionKindQuestion,
		RequestID: question.RequestID,
		Event:     evt,
	}
	alreadyPending := false
	if existing, ok := loadGeminiPendingRequest(context.Background(), question.RequestID); ok && existing != nil && existing.Event.AgentID == evt.AgentID {
		record.CreatedAt = existing.CreatedAt
		alreadyPending = true
	}
	if !saveGeminiPendingRequest(context.Background(), record) {
		logger.L.Warnf("save gemini question interaction failed request_id=%s agent=%d", question.RequestID, evt.AgentID)
		return false
	}
	if alreadyPending {
		return true
	}

	if m.sendGeminiCardMessage(evt, buildLocalGrixCardLink(
		fmt.Sprintf("[Agent Question] %s", compactReplyText(question.RequestID, 120)),
		"agent_question",
		cardPayload,
	), nil) {
		return true
	}

	deleteGeminiPendingRequest(context.Background(), question.RequestID)
	return false
}

func (m *Manager) maybeHandleGeminiEventResult(conn *agentConn, payload EventResultPayload) {
	if !isGeminiConn(conn) || strings.TrimSpace(payload.Status) != protocol.AgentEventResultFailed {
		return
	}

	record, ok := loadDurablePendingDelegate(context.Background(), payload.EventID)
	if !ok || record == nil {
		return
	}
	if err := m.maybeHandleGeminiEventResultRecord(conn, payload, record); err != nil {
		logger.L.Warnf("handle gemini terminal interaction failed event=%s err=%v", payload.EventID, err)
	}
}

func (m *Manager) maybeHandleGeminiEventResultRecord(
	conn *agentConn,
	payload EventResultPayload,
	record *durablePendingDelegateRecord,
) error {
	if !isGeminiConn(conn) || strings.TrimSpace(payload.Status) != protocol.AgentEventResultFailed ||
		record == nil {
		return nil
	}
	switch strings.TrimSpace(payload.Code) {
	case "gemini_auth_missing":
		requestID := buildGeminiAuthRequestID(payload.EventID)
		if !saveGeminiPendingRequest(context.Background(), geminiPendingInteraction{
			Kind:      geminiInteractionKindAuth,
			RequestID: requestID,
			Event:     record.Event,
		}) {
			logger.L.Warnf("save gemini auth interaction failed event=%s agent=%d", payload.EventID, record.Event.AgentID)
			return fmt.Errorf("save gemini auth interaction")
		}
		if !m.sendGeminiCardMessageWithID(record.Event, buildLocalGrixCardLink(
			"[Agent Question] Gemini authentication required",
			"agent_question",
			map[string]any{
				"request_id":            requestID,
				"mode":                  "url",
				"message":               "Finish Gemini CLI authentication on the machine running grix-gemini, then come back and click Complete.",
				"url":                   geminiAuthGuideURL,
				"open_url_label":        "View Gemini authentication guide",
				"submitted_accept_text": "Gemini authentication completed",
				"submitted_cancel_text": "I'll set it up later",
			},
		), nil, "gemini_terminal_auth_"+record.Event.EventID) {
			deleteGeminiPendingRequest(context.Background(), requestID)
			return fmt.Errorf("send gemini auth interaction")
		}
	case "session_binding_missing":
		// Plugin detected missing workspace binding and already sent a send_msg card.
		// Backend stores the pending event for replay after the user submits a workspace.
		alreadyPending := false
		if existing, ok := loadGeminiPendingWorkspace(context.Background(), record.Event.AgentID, record.Event.SessionID); ok && existing != nil {
			alreadyPending = true
		}
		if !saveGeminiPendingWorkspace(context.Background(), geminiPendingInteraction{
			Kind:  geminiInteractionKindWorkspace,
			Event: record.Event,
		}) {
			logger.L.Warnf("save gemini workspace interaction failed event=%s agent=%d", payload.EventID, record.Event.AgentID)
			return fmt.Errorf("save gemini workspace interaction")
		}
		if alreadyPending {
			return nil
		}
		return nil
	}

	if statusCard := buildGeminiFailureStatusCard(ownerCardLanguage(record.Event.OwnerID), payload); len(statusCard) > 0 {
		if !m.sendGeminiStatusCard(record.Event, statusCard) {
			return fmt.Errorf("send gemini terminal status")
		}
	}
	return nil
}

func buildGeminiFailureStatusCard(lang string, payload EventResultPayload) map[string]any {
	code := strings.TrimSpace(payload.Code)
	msg := strings.TrimSpace(payload.Msg)

	card := map[string]any{
		"category":     "runtime",
		"reference_id": strings.TrimSpace(payload.EventID),
	}

	switch code {
	case "gemini_prompt_timeout":
		card["status"] = "warning"
		card["summary"] = tooli18n.T(lang, "gemini_prompt_timeout")
		card["detail_text"] = firstNonEmpty(msg, tooli18n.T(lang, "gemini_prompt_timeout_detail"))
	case "gemini_process_exit":
		card["status"] = "error"
		card["summary"] = tooli18n.T(lang, "gemini_process_exit")
		card["detail_text"] = firstNonEmpty(msg, tooli18n.T(lang, "gemini_process_exit_detail"))
	case "gemini_prompt_failed":
		card["status"] = "error"
		card["summary"] = tooli18n.T(lang, "gemini_prompt_failed")
		card["detail_text"] = firstNonEmpty(msg, tooli18n.T(lang, "gemini_prompt_failed_detail"))
	case "gemini_empty_output":
		card["status"] = "warning"
		card["summary"] = tooli18n.T(lang, "gemini_empty_output")
		card["detail_text"] = firstNonEmpty(msg, tooli18n.T(lang, "gemini_empty_output_detail"))
	case "gemini_invalid_payload":
		card["status"] = "error"
		card["summary"] = tooli18n.T(lang, "gemini_invalid_payload")
		card["detail_text"] = firstNonEmpty(msg, tooli18n.T(lang, "gemini_invalid_payload_detail"))
	default:
		return nil
	}

	return card
}

type geminiQuestionCard struct {
	RequestID  string
	Message    string
	FooterText string
	Questions  []geminiQuestionPrompt
}

type geminiQuestionPrompt struct {
	Index       int
	Header      string
	Prompt      string
	FieldKey    string
	Options     []string
	MultiSelect bool
}

func extractGeminiQuestionCard(evt DelegateEventPayload) (map[string]any, geminiQuestionCard, bool) {
	bizCard := decodeGeminiQuestionBizCard(evt)
	if len(bizCard) == 0 || !strings.EqualFold(strings.TrimSpace(fmt.Sprint(bizCard["type"])), "agent_question") {
		return nil, geminiQuestionCard{}, false
	}

	payload := geminiMapValue(bizCard["payload"])
	if len(payload) == 0 {
		return nil, geminiQuestionCard{}, false
	}
	if mode := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["mode"]))); mode == "url" {
		return nil, geminiQuestionCard{}, false
	}

	requestID := strings.TrimSpace(fmt.Sprint(payload["request_id"]))
	questions := normalizeGeminiQuestionPrompts(payload["questions"])
	if requestID == "" || len(questions) == 0 {
		return nil, geminiQuestionCard{}, false
	}

	card := geminiQuestionCard{
		RequestID:  requestID,
		Message:    strings.TrimSpace(fmt.Sprint(payload["message"])),
		FooterText: strings.TrimSpace(fmt.Sprint(payload["footer_text"])),
		Questions:  questions,
	}

	normalizedPayload := map[string]any{
		"request_id": requestID,
		"mode":       "form",
		"questions":  make([]map[string]any, 0, len(questions)),
	}
	if card.Message != "" {
		normalizedPayload["message"] = card.Message
	}
	if card.FooterText != "" {
		normalizedPayload["footer_text"] = card.FooterText
	}
	for _, question := range questions {
		item := map[string]any{
			"index":  question.Index,
			"header": question.Header,
			"prompt": question.Prompt,
		}
		if question.FieldKey != "" {
			item["field_key"] = question.FieldKey
		}
		if len(question.Options) > 0 {
			item["options"] = append([]string(nil), question.Options...)
		}
		if question.MultiSelect {
			item["multi_select"] = true
		}
		normalizedPayload["questions"] = append(normalizedPayload["questions"].([]map[string]any), item)
	}
	return normalizedPayload, card, true
}

func decodeGeminiQuestionBizCard(evt DelegateEventPayload) map[string]any {
	if len(evt.BizCard) > 0 {
		var decoded map[string]any
		if err := json.Unmarshal(evt.BizCard, &decoded); err == nil {
			return decoded
		}
	}

	base := decodeGeminiExtraObject(evt.Extra)
	if len(base) == 0 {
		return nil
	}
	return geminiMapValue(base["biz_card"])
}

func normalizeGeminiQuestionPrompts(raw any) []geminiQuestionPrompt {
	rawQuestions, ok := raw.([]any)
	if !ok {
		return nil
	}

	questions := make([]geminiQuestionPrompt, 0, len(rawQuestions))
	for index, rawQuestion := range rawQuestions {
		question := geminiMapValue(rawQuestion)
		header := strings.TrimSpace(fmt.Sprint(question["header"]))
		prompt := strings.TrimSpace(fmt.Sprint(question["prompt"]))
		if header == "" || prompt == "" {
			continue
		}

		item := geminiQuestionPrompt{
			Index:       geminiQuestionIndex(question["index"], index+1),
			Header:      header,
			Prompt:      prompt,
			FieldKey:    strings.TrimSpace(fmt.Sprint(question["field_key"])),
			Options:     normalizeGeminiQuestionOptions(question["options"]),
			MultiSelect: question["multi_select"] == true,
		}
		questions = append(questions, item)
	}
	return questions
}

func normalizeGeminiQuestionOptions(raw any) []string {
	switch typed := raw.(type) {
	case []any:
		options := make([]string, 0, len(typed))
		for _, item := range typed {
			value := strings.TrimSpace(fmt.Sprint(item))
			if value != "" {
				options = append(options, value)
			}
		}
		return options
	case []string:
		options := make([]string, 0, len(typed))
		for _, item := range typed {
			value := strings.TrimSpace(item)
			if value != "" {
				options = append(options, value)
			}
		}
		return options
	default:
		return nil
	}
}

func geminiQuestionIndex(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return typed
		}
	case int64:
		if typed > 0 {
			return int(typed)
		}
	case float64:
		if typed > 0 && typed == float64(int(typed)) {
			return int(typed)
		}
	}
	return fallback
}

func buildGeminiQuestionAnswerText(evt DelegateEventPayload, response map[string]any) string {
	_, card, ok := extractGeminiQuestionCard(evt)
	if !ok || len(response) == 0 {
		return ""
	}

	lines := []string{"User provided additional details:"}
	switch strings.TrimSpace(fmt.Sprint(response["type"])) {
	case "single":
		value := strings.TrimSpace(fmt.Sprint(response["value"]))
		if value == "" {
			return ""
		}
		label := "Answer"
		if len(card.Questions) == 1 {
			label = firstNonEmpty(card.Questions[0].Header, card.Questions[0].FieldKey, label)
		}
		lines = append(lines, fmt.Sprintf("%s: %s", label, value))
	case "map":
		rawEntries, ok := response["entries"].([]any)
		if !ok || len(rawEntries) == 0 {
			return ""
		}
		labels := make(map[string]string, len(card.Questions)*2)
		for _, question := range card.Questions {
			if question.FieldKey != "" {
				labels[question.FieldKey] = question.Header
			}
			labels[fmt.Sprintf("%d", question.Index)] = question.Header
		}
		for _, rawEntry := range rawEntries {
			entry := geminiMapValue(rawEntry)
			key := strings.TrimSpace(fmt.Sprint(entry["key"]))
			value := strings.TrimSpace(fmt.Sprint(entry["value"]))
			if key == "" || value == "" {
				continue
			}
			label := firstNonEmpty(labels[key], key)
			lines = append(lines, fmt.Sprintf("%s: %s", label, value))
		}
	default:
		return ""
	}

	if len(lines) <= 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func stripGeminiQuestionCard(evt DelegateEventPayload) DelegateEventPayload {
	evt.BizCard = nil
	base := decodeGeminiExtraObject(evt.Extra)
	if len(base) == 0 {
		return evt
	}

	if _, ok := base["biz_card"]; !ok {
		return evt
	}
	delete(base, "biz_card")
	if len(base) == 0 {
		evt.Extra = nil
		return evt
	}

	encoded, err := json.Marshal(base)
	if err != nil {
		return evt
	}
	evt.Extra = encoded
	return evt
}

func decodeGeminiExtraObject(raw json.RawMessage) map[string]any {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil
	}
	return decoded
}

func geminiMapValue(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return nil
	}
}

func (m *Manager) isGeminiAgent(agentID int64) bool {
	if agentID <= 0 {
		return false
	}
	if conn := m.lookupConn(agentID); isGeminiConn(conn) {
		return true
	}
	if store.DB == nil {
		return false
	}

	var agent model.Agent
	if err := store.DB.Select("id,agent_client_type").First(&agent, agentID).Error; err != nil {
		return false
	}
	return model.NormalizeAgentClientType(agent.AgentClientType) == model.AgentClientTypeGemini
}

func isGeminiConn(conn *agentConn) bool {
	if conn == nil {
		return false
	}
	return strings.TrimSpace(conn.adapterID) == geminiadapter.AdapterID ||
		model.NormalizeAgentClientType(conn.clientType) == model.AgentClientTypeGemini
}

func (m *Manager) sendGeminiStatusCard(evt DelegateEventPayload, payload map[string]any) bool {
	reply := buildAgentStatusCardReply(payload)
	if strings.TrimSpace(reply.content) == "" {
		return false
	}
	code := strings.TrimSpace(fmt.Sprint(payload["code"]))
	if code == "" {
		code = strings.TrimSpace(fmt.Sprint(payload["summary"]))
	}
	return m.sendGeminiCardMessageWithID(
		evt,
		reply.content,
		reply.extra,
		fmt.Sprintf("gemini_terminal_status_%s_%s", evt.EventID, code),
	)
}

func (m *Manager) sendGeminiCardMessage(evt DelegateEventPayload, content string, extra json.RawMessage) bool {
	return m.sendGeminiCardMessageWithID(evt, content, extra, "")
}

func (m *Manager) sendGeminiCardMessageWithID(
	evt DelegateEventPayload,
	content string,
	extra json.RawMessage,
	clientMsgID string,
) bool {
	if m == nil || m.sendFn == nil {
		return false
	}
	if evt.AgentID <= 0 || evt.OwnerID <= 0 || strings.TrimSpace(evt.SessionID) == "" || strings.TrimSpace(content) == "" {
		return false
	}

	clientMsgID = strings.TrimSpace(clientMsgID)
	if clientMsgID == "" {
		clientMsgID = fmt.Sprintf("gemini_interaction_%d", time.Now().UnixNano())
	}
	_, err := m.sendFn(context.Background(), SendMessageReq{
		AgentID:         evt.AgentID,
		OwnerID:         evt.OwnerID,
		SessionID:       evt.SessionID,
		ThreadID:        evt.ThreadID,
		ClientMsgID:     clientMsgID,
		MsgType:         1,
		Content:         content,
		Extra:           append(json.RawMessage(nil), extra...),
		VisibleTo:       ownerVisibleToForAdapterCard(geminiadapter.AdapterID, content, extra, evt.OwnerID),
		QuotedMessageID: evt.MsgID,
	})
	if err != nil {
		logger.L.Warnf("send gemini interaction card failed agent=%d owner=%d session=%s err=%v", evt.AgentID, evt.OwnerID, evt.SessionID, err)
		return false
	}
	return true
}

func buildGeminiAuthRequestID(eventID string) string {
	normalized := strings.TrimSpace(eventID)
	if normalized == "" {
		normalized = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return "gemini_auth_" + normalized
}

func geminiPayloadNeedsWorkspace(msg string) bool {
	normalized := strings.ToLower(strings.TrimSpace(msg))
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "missing cwd") ||
		strings.Contains(normalized, "cwd must be an absolute path") ||
		strings.Contains(normalized, "cwd")
}

func parseGeminiCardRequestID(content string) string {
	trimmed := strings.TrimSpace(content)
	if start := strings.Index(trimmed, "(grix://card/"); start >= 0 {
		trimmed = trimmed[start+1:]
		if end := strings.Index(trimmed, ")"); end >= 0 {
			trimmed = trimmed[:end]
		}
	}

	uri, err := url.Parse(trimmed)
	if err != nil || !strings.EqualFold(uri.Scheme, "grix") || !strings.EqualFold(uri.Host, "card") {
		return ""
	}
	if strings.Trim(strings.TrimSpace(uri.Path), "/") != "agent_question" {
		return ""
	}
	if requestID := strings.TrimSpace(uri.Query().Get("request_id")); requestID != "" {
		return requestID
	}
	raw := strings.TrimSpace(uri.Query().Get("d"))
	if raw == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(payload["request_id"]))
}

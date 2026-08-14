package responsesbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// StreamWriter 把上游 Chat Completions 的 SSE 转写成 Responses API 的 typed SSE。
// 实现 http.ResponseWriter，可直接塞给 upstream.Forward。
type StreamWriter struct {
	inner      http.ResponseWriter
	flusher    http.Flusher
	responseID string
	model      string

	mu      sync.Mutex
	lineBuf bytes.Buffer
	state   streamState
	err     error
}

type streamState struct {
	seq            int
	started        bool
	finished       bool
	msgID          string
	msgIndex       int
	textStarted    bool
	text           strings.Builder
	outputIndex    int
	toolCalls      map[int]*toolAcc
	completedItems []any
	usage          any
}

type toolAcc struct {
	id        string
	fcID      string
	name      string
	arguments strings.Builder
	outIndex  int
	started   bool
}

// NewStreamWriter 构造流式转接 writer。
func NewStreamWriter(w http.ResponseWriter, responseID, model string) *StreamWriter {
	flusher, _ := w.(http.Flusher)
	return &StreamWriter{
		inner:      w,
		flusher:    flusher,
		responseID: responseID,
		model:      model,
		state: streamState{
			msgID:     "msg_" + responseID,
			toolCalls: map[int]*toolAcc{},
		},
	}
}

func (s *StreamWriter) Header() http.Header         { return s.inner.Header() }
func (s *StreamWriter) WriteHeader(statusCode int) { s.inner.WriteHeader(statusCode) }

func (s *StreamWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}

	// 非 SSE（上游错误 JSON）原样透传。
	ct := s.inner.Header().Get("Content-Type")
	if strings.Contains(ct, "application/json") && !strings.Contains(ct, "event-stream") {
		return s.inner.Write(p)
	}

	s.ensureSSEHeaders()
	s.lineBuf.Write(p)
	for {
		data := s.lineBuf.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := string(data[:idx])
		s.lineBuf.Next(idx + 1)
		line = strings.TrimRight(line, "\r")
		if err := s.handleLine(line); err != nil {
			s.err = err
			return 0, err
		}
	}
	return len(p), nil
}

// Finish 在 Forward 返回后调用：补发未闭合的 item / completed 事件。
func (s *StreamWriter) Finish() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.state.finished {
		return nil
	}
	s.ensureSSEHeaders()
	return s.emitCompletion()
}

func (s *StreamWriter) ensureSSEHeaders() {
	h := s.inner.Header()
	if h.Get("Content-Type") == "" {
		h.Set("Content-Type", "text/event-stream")
	}
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
}

func (s *StreamWriter) handleLine(line string) error {
	if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
		return nil
	}
	if !strings.HasPrefix(line, "data: ") {
		return nil
	}
	data := strings.TrimPrefix(line, "data: ")
	if data == "[DONE]" {
		return s.emitCompletion()
	}

	var chunk map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return nil // 忽略坏块，继续
	}
	if m := decodeString(chunk["model"]); m != "" {
		s.model = m
	}
	if u, ok := chunk["usage"]; ok && string(u) != "null" {
		s.state.usage = convertUsage(u)
	}

	var choices []map[string]json.RawMessage
	_ = json.Unmarshal(chunk["choices"], &choices)
	if len(choices) == 0 {
		return nil
	}
	choice := choices[0]
	var delta map[string]json.RawMessage
	_ = json.Unmarshal(choice["delta"], &delta)

	var events []map[string]any

	if rc := decodeString(delta["reasoning_content"]); rc != "" {
		// 简化：reasoning 直接并入文本前缀，避免引入完整 reasoning item 生命周期。
		// Codex 主要依赖 output_text / function_call；reasoning 缺失通常可接受。
		_ = rc
	}

	if content := decodeString(delta["content"]); content != "" {
		events = append(events, s.emitTextDelta(content)...)
	}

	if toolCallsRaw, ok := delta["tool_calls"]; ok && string(toolCallsRaw) != "null" {
		var tcs []map[string]json.RawMessage
		_ = json.Unmarshal(toolCallsRaw, &tcs)
		events = append(events, s.emitToolDeltas(tcs)...)
	}

	if len(events) > 0 && !s.state.started {
		prefix := s.emitLifecycle()
		events = append(prefix, events...)
		s.state.started = true
	}

	for _, ev := range events {
		if err := s.writeEvent(ev); err != nil {
			return err
		}
	}
	return nil
}

func (s *StreamWriter) emitLifecycle() []map[string]any {
	resp := s.partialResponse("in_progress")
	return []map[string]any{
		{"type": "response.created", "response": resp, "sequence_number": s.nextSeq()},
		{"type": "response.in_progress", "response": resp, "sequence_number": s.nextSeq()},
	}
}

func (s *StreamWriter) emitTextDelta(content string) []map[string]any {
	var events []map[string]any
	if !s.state.textStarted {
		idx := s.nextOutputIndex()
		s.state.msgIndex = idx
		events = append(events,
			map[string]any{
				"type":          "response.output_item.added",
				"output_index":  idx,
				"sequence_number": s.nextSeq(),
				"item": map[string]any{
					"type":    "message",
					"id":      s.state.msgID,
					"role":    "assistant",
					"status":  "in_progress",
					"content": []any{},
				},
			},
			map[string]any{
				"type":            "response.content_part.added",
				"content_index":   0,
				"item_id":         s.state.msgID,
				"output_index":    idx,
				"sequence_number": s.nextSeq(),
				"part": map[string]any{
					"type":        "output_text",
					"text":        "",
					"annotations": []any{},
				},
			},
		)
		s.state.textStarted = true
	}
	s.state.text.WriteString(content)
	events = append(events, map[string]any{
		"type":            "response.output_text.delta",
		"delta":           content,
		"item_id":         s.state.msgID,
		"output_index":    s.state.msgIndex,
		"content_index":   0,
		"sequence_number": s.nextSeq(),
	})
	return events
}

func (s *StreamWriter) emitToolDeltas(tcs []map[string]json.RawMessage) []map[string]any {
	var events []map[string]any
	for _, tc := range tcs {
		idx := decodeInt(tc["index"])
		acc, ok := s.state.toolCalls[idx]
		if !ok {
			acc = &toolAcc{}
			s.state.toolCalls[idx] = acc
		}
		first := !acc.started
		if id := decodeString(tc["id"]); id != "" {
			acc.id = id
		}
		var fn map[string]json.RawMessage
		_ = json.Unmarshal(tc["function"], &fn)
		if name := decodeString(fn["name"]); name != "" {
			acc.name = name
		}
		if args := decodeString(fn["arguments"]); args != "" {
			acc.arguments.WriteString(args)
		}
		if first {
			acc.outIndex = s.nextOutputIndex()
			acc.fcID = "fc_" + firstNonEmpty(acc.id, fmt.Sprintf("%d", idx))
			acc.started = true
			events = append(events, map[string]any{
				"type":            "response.output_item.added",
				"output_index":    acc.outIndex,
				"sequence_number": s.nextSeq(),
				"item": map[string]any{
					"type":      "function_call",
					"id":        acc.fcID,
					"call_id":   acc.id,
					"name":      acc.name,
					"arguments": "",
					"status":    "in_progress",
				},
			})
		}
		if args := decodeString(fn["arguments"]); args != "" {
			events = append(events, map[string]any{
				"type":            "response.function_call_arguments.delta",
				"delta":           args,
				"item_id":         acc.fcID,
				"output_index":    acc.outIndex,
				"sequence_number": s.nextSeq(),
			})
		}
	}
	return events
}

func (s *StreamWriter) emitCompletion() error {
	if s.state.finished {
		return nil
	}
	if !s.state.started {
		// 空流也要给客户端完整生命周期，避免挂死。
		for _, ev := range s.emitLifecycle() {
			if err := s.writeEvent(ev); err != nil {
				return err
			}
		}
		s.state.started = true
	}

	// 关闭 tool calls（按 index 升序，避免 map 稀疏/非 0 起漏关）
	indexes := make([]int, 0, len(s.state.toolCalls))
	for idx := range s.state.toolCalls {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	for _, idx := range indexes {
		acc := s.state.toolCalls[idx]
		if acc == nil || !acc.started {
			continue
		}
		args := acc.arguments.String()
		item := map[string]any{
			"type":      "function_call",
			"id":        acc.fcID,
			"call_id":   acc.id,
			"name":      acc.name,
			"arguments": args,
			"status":    "completed",
		}
		if err := s.writeEvent(map[string]any{
			"type":            "response.function_call_arguments.done",
			"arguments":       args,
			"item_id":         acc.fcID,
			"output_index":    acc.outIndex,
			"sequence_number": s.nextSeq(),
		}); err != nil {
			return err
		}
		if err := s.writeEvent(map[string]any{
			"type":            "response.output_item.done",
			"output_index":    acc.outIndex,
			"sequence_number": s.nextSeq(),
			"item":            item,
		}); err != nil {
			return err
		}
		s.state.completedItems = append(s.state.completedItems, item)
	}

	// 关闭 message
	if s.state.textStarted {
		text := s.state.text.String()
		item := map[string]any{
			"type":   "message",
			"id":     s.state.msgID,
			"role":   "assistant",
			"status": "completed",
			"content": []map[string]any{{
				"type":        "output_text",
				"text":        text,
				"annotations": []any{},
			}},
		}
		if err := s.writeEvent(map[string]any{
			"type":            "response.output_text.done",
			"text":            text,
			"item_id":         s.state.msgID,
			"output_index":    s.state.msgIndex,
			"content_index":   0,
			"sequence_number": s.nextSeq(),
		}); err != nil {
			return err
		}
		if err := s.writeEvent(map[string]any{
			"type":            "response.content_part.done",
			"content_index":   0,
			"item_id":         s.state.msgID,
			"output_index":    s.state.msgIndex,
			"sequence_number": s.nextSeq(),
			"part": map[string]any{
				"type":        "output_text",
				"text":        text,
				"annotations": []any{},
			},
		}); err != nil {
			return err
		}
		if err := s.writeEvent(map[string]any{
			"type":            "response.output_item.done",
			"output_index":    s.state.msgIndex,
			"sequence_number": s.nextSeq(),
			"item":            item,
		}); err != nil {
			return err
		}
		s.state.completedItems = append(s.state.completedItems, item)
	}

	completed := s.partialResponse("completed")
	completed["output"] = s.state.completedItems
	if s.state.usage != nil {
		completed["usage"] = s.state.usage
	}
	if err := s.writeEvent(map[string]any{
		"type":            "response.completed",
		"response":        completed,
		"sequence_number": s.nextSeq(),
	}); err != nil {
		return err
	}
	s.state.finished = true
	return nil
}

func (s *StreamWriter) partialResponse(status string) map[string]any {
	return map[string]any{
		"id":         s.responseID,
		"object":     "response",
		"created_at": 0,
		"status":     status,
		"model":      s.model,
		"output":     []any{},
	}
}

func (s *StreamWriter) writeEvent(ev map[string]any) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	typ, _ := ev["type"].(string)
	var b strings.Builder
	if typ != "" {
		b.WriteString("event: ")
		b.WriteString(typ)
		b.WriteByte('\n')
	}
	b.WriteString("data: ")
	b.Write(body)
	b.WriteString("\n\n")
	if _, err := s.inner.Write([]byte(b.String())); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

func (s *StreamWriter) nextSeq() int {
	n := s.state.seq
	s.state.seq++
	return n
}

func (s *StreamWriter) nextOutputIndex() int {
	n := s.state.outputIndex
	s.state.outputIndex++
	return n
}

// BufferWriter 捕获上游非流式响应，供 ConvertResponse 使用。
type BufferWriter struct {
	HeaderMap http.Header
	Code      int
	Body      bytes.Buffer
}

func NewBufferWriter() *BufferWriter {
	return &BufferWriter{HeaderMap: make(http.Header), Code: http.StatusOK}
}

func (b *BufferWriter) Header() http.Header { return b.HeaderMap }
func (b *BufferWriter) WriteHeader(statusCode int) {
	b.Code = statusCode
}
func (b *BufferWriter) Write(p []byte) (int, error) {
	if b.Code == 0 {
		b.Code = http.StatusOK
	}
	return b.Body.Write(p)
}

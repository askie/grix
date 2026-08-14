package model

import "encoding/json"

// MsgType 常量（补充 Phase 3）
const (
	MsgTypeText        int16 = 1
	MsgTypeImage       int16 = 2
	MsgTypeSystem      int16 = 3
	MsgTypeAIStream    int16 = 4
	MsgTypeIntervene   int16 = 5
	MsgTypeCallSegment int16 = 6 // Phase 3: 通话片段（转写 + 音频）
)

// CallSegmentExtra 是 msg_type=6 消息的 extra 字段结构。
// 对应文档 §9.3 数据结构。
type CallSegmentExtra struct {
	Kind               string `json:"kind"`                          // 固定 "call_segment"
	CallID             string `json:"call_id"`
	SegmentSeq         int    `json:"segment_seq"`                   // 片段序号（从 1 开始）
	SpeakerRole        string `json:"speaker_role"`                  // "caller" | "callee" | "ai_bot"
	SpeakerUserID      string `json:"speaker_user_id,omitempty"`
	AudioURL           string `json:"audio_url,omitempty"`           // OSS 音频 URL
	AudioDurationMs    int    `json:"audio_duration_ms,omitempty"`
	Transcript         string `json:"transcript"`                    // 改写后转写文本
	TranscriptRaw      string `json:"transcript_raw,omitempty"`      // 原始转写（保留）
	TranscriptStatus   string `json:"transcript_status"`             // "final"
	TranscriptProvider string `json:"transcript_provider,omitempty"` // "openai_realtime"
	TranscriptRefined  bool   `json:"transcript_refined"`
	StartedAtMs        int64  `json:"started_at_ms,omitempty"`
	EndedAtMs          int64  `json:"ended_at_ms,omitempty"`
}

// ToJSON 序列化为 JSONB 存储格式。
func (e *CallSegmentExtra) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// CallSegmentExtraFromJSON 从 JSONB 反序列化。
func CallSegmentExtraFromJSON(data []byte) (*CallSegmentExtra, error) {
	var e CallSegmentExtra
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

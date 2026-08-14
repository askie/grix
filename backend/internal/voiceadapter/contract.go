// Package voiceadapter 定义语音 LLM Provider 的抽象接口与注册中心。
//
// 设计约束（来自架构文档 §3.3 原则 2）：
//   - 本包禁止 import agentadapter；
//   - 本包禁止 import internal/ws/handler/call_*；
//   - 本包只关心音频流协议，不涉及文本卡片协议。
package voiceadapter

import "context"

// PCMFrame 是一帧原始 PCM 音频数据（16-bit LE, 24kHz, 单声道）。
// 匹配 OpenAI Realtime API 的音频格式要求。
type PCMFrame struct {
	Data []byte
}

// EventKind 标识 Event 的类型。
type EventKind string

const (
	EventKindTranscript EventKind = "transcript" // 转写文本（final）
	EventKindError      EventKind = "error"       // Provider 侧错误
	EventKindClosed     EventKind = "closed"      // 连接已关闭
)

// Event 是 VoiceAgentBridge 向调用方推送的事件。
type Event struct {
	Kind       EventKind
	Transcript string // EventKindTranscript 时有效
	Err        error  // EventKindError 时有效
}

// Mode 控制 Bridge 的工作模式。
type Mode string

const (
	ModeDuplex           Mode = "duplex"            // 双工：接收音频 + 生成应答音频
	ModeTranscriptionOnly Mode = "transcription_only" // 仅转写，不生成应答
)

// VoiceAgentConfig 启动 Bridge 所需的配置（BYOK：模型与凭证均来自 agent 级配置）。
type VoiceAgentConfig struct {
	AgentID      int64
	Mode         Mode
	Provider     string // voice_provider 标识（可选，供多 provider 适配器分流）
	Model        string // Provider 侧模型名（必填，无全局兜底）
	Endpoint     string // 可选自定义 base URL
	Voice        string // Provider 侧音色 ID（可空，使用 provider 默认）
	SystemPrompt string
	APIKey       string // 用户自带 API key（必填，无全局兜底）
	Language     string // 语言代码，如 "zh-CN"
}

// VoiceAgentBridge 是语音 LLM Provider 的统一抽象接口。
// 每个 Provider 实现一个 Bridge 实例（非单例，每次通话创建一个）。
type VoiceAgentBridge interface {
	// AdapterID 返回唯一标识，如 "doubao_realtime_v1"。
	AdapterID() string
	// Family 返回 Provider 家族，如 "doubao_realtime"。
	Family() string

	// Start 建立与 Provider 的连接，开始双工音频流。
	// callerAudio: 来自 SFU 的 caller PCM 帧（只读）。
	// 返回 outAudio（Provider 生成的应答 PCM 帧）和 events（转写/错误事件）。
	Start(ctx context.Context, cfg VoiceAgentConfig,
		callerAudio <-chan PCMFrame) (outAudio <-chan PCMFrame, events <-chan Event, err error)

	// Interrupt 打断当前 AI 应答（接管时调用）。
	Interrupt(ctx context.Context) error

	// Close 关闭连接，释放资源。
	Close(ctx context.Context) error
}

package protocol

// 协议层全局上限。
// 这些常量用于服务端在 agentapi 入口、send_msg 入口等位置做硬约束，
// 防止任何单个 Agent 通过超大 payload、超频 chunk、跳序 chunk 影响整个协议层。
//
// 调整策略：
//   - 上限只放宽不收紧（避免老客户端发出去后被拒）；
//   - 触发上限时统一以 4xxx 业务错误码反馈，而不是直接断链（除 MaxPacketBytes）。
const (
	// MaxPacketBytes 单条 WebSocket packet 字节上限。超出直接关闭连接。
	// 1 MiB 已可覆盖正常的多媒体卡片、上下文消息等用例。
	MaxPacketBytes = 1 << 20

	// MaxDeltaContentChars 单个流式分片 delta_content 字符数上限。
	// 16 KiB 足够 ~5000 字的中文段落或 ~10000 字的英文。
	MaxDeltaContentChars = 16 << 10

	// MaxContentChars 一条 send_msg 的 content 字符数上限。
	MaxContentChars = 64 << 10

	// MaxExtraBytes send_msg 的 extra 字段序列化后字节上限。
	MaxExtraBytes = 32 << 10

	// StreamChunkCountWarnThreshold 是同一 event_id 下 client_stream_chunk
	// 数量的观测阈值。超过阈值只记录一次告警，不拒绝分片，也不改变事件状态。
	// 长时间工作的 Agent（尤其是细粒度 thinking 流）可以合法超过该值。
	StreamChunkCountWarnThreshold = 5000

	// MaxChunkSeqGap 相邻两个 chunk 的 chunk_seq 允许跳跃的最大值。
	// 超过此值视为非法（疑似 Agent bug 或恶意），拒绝该分片。
	MaxChunkSeqGap = 16
)

// 协议层错误码（4xxx 客户端错误,5xxx 服务端错误）。
// 这些常量用于限制类拒绝场景,与 send_nack/error 的 Code 字段配合使用。
const (
	CodeInvalidPayload  = 4001 // payload 字段缺失或不合法
	CodeUnauthorized    = 4003 // 当前 Agent 无权对该资源操作（归属不符）
	CodeUnsupportedCmd  = 4004 // 该 cmd 在当前协议面下不允许
	CodePayloadTooLarge = 4006 // 超过协议层硬上限
	CodeRateLimited     = 4029 // 累计违规过多/触发熔断
	CodeServerInternal  = 5001 // 服务端内部错误
)

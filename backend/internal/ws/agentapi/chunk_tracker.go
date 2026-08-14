package agentapi

import (
	"sync"
	"sync/atomic"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

// chunkTrackerEntry 跟踪单个 client_stream_chunk 流的状态：
//   - lastSeq:  迄今见过的最大 chunk_seq, 仅用于观测/调试。
//   - count:    累计已接收的 chunk 数量,用于越过软阈值时记录一次告警。
type chunkTrackerEntry struct {
	lastSeq atomic.Int64
	count   atomic.Int64
}

// chunkTrackers 是 Manager 持有的轻量跟踪表。
// key 选择：优先使用 event_id（多端 Agent 的同一 event 必定共用同一 key),
// 当 event_id 为空（例如老协议路径）退化为 client_msg_id。
type chunkTrackers struct {
	m sync.Map // key: string → *chunkTrackerEntry
}

// observe 记录一次 chunk 到达。crossedWarnThreshold 只在首次越过观测阈值时
// 返回 true，调用方可据此记录一次告警，但不得拒绝分片或终结事件。
//
// 设计要点：chunk_seq 的顺序语义（去重、滞后丢弃、跳序重排）由服务端重排层
// (agent_api_bridge_stream 的 expected_seq 缓冲) 统一负责。Agent 在长停顿/工具
// 执行后恢复、重发、或单调计数器跨非文本事件前跳都是常见且合法的，不应在此被判
// 为致命错误而整轮判死。分片数量同样不能代表任务失败：细粒度 thinking 流可合法
// 产生数千甚至数万片，因此这里只观测，不做协议拒绝。
func (t *chunkTrackers) observe(key string, seq int64) (count int64, crossedWarnThreshold bool) {
	if t == nil || key == "" {
		return 0, false
	}
	v, _ := t.m.LoadOrStore(key, &chunkTrackerEntry{})
	entry := v.(*chunkTrackerEntry)

	// 仅做尽力而为的最大 seq 记录，用于观测；不因顺序问题拒绝分片。
	for {
		last := entry.lastSeq.Load()
		if seq <= last || entry.lastSeq.CompareAndSwap(last, seq) {
			break
		}
	}
	count = entry.count.Add(1)
	return count, count == int64(protocol.StreamChunkCountWarnThreshold)+1
}

// release 清理某个 key 的 tracker, 通常在 finish chunk 收到后或 event 终态时调用。
func (t *chunkTrackers) release(key string) {
	if t == nil || key == "" {
		return
	}
	t.m.Delete(key)
}

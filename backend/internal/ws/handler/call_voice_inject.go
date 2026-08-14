package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// SubjectVoiceInjectPrefix 是下行注入 NATS 主题前缀；完整主题为 voicebridge.inject.<call_id>。
// Python voicebridge 订阅后用 502 把文本注入豆包当前 session（接点B）。
const SubjectVoiceInjectPrefix = "voicebridge.inject."

// voiceInjectTimeout 是大脑超时兜底阈值（架构文档 29 §5）：
// 从最近一次 caller 转写写入算起，超过此时间产出的回复不再注入语音侧。
var voiceInjectTimeout = 15 * time.Second

// voiceInjectDirectTimeout 是直拨场景（用户呼自己的语音 agent 调度文字 agent）的超时阈值。
// 文字 agent 执行任务动辄数十秒到数分钟，回复必须仍可注入播报（架构文档 33）。
var voiceInjectDirectTimeout = 5 * time.Minute

// voiceInjectStateTTL 是 Redis 轮次状态 key 的过期时间，覆盖最长单通话时长后自动清理。
const voiceInjectStateTTL = 40 * time.Minute

// ActiveVoiceCallLookup 反查指定会话当前活跃的 AI 代接通话。
// 多副本部署下通话内存态只在 call owner 节点，须以 DB/Redis 等跨节点可见状态兜底。
// direct 表示用户直拨自己的语音 agent（注入侧据此放宽轮次与超时限制）。
type ActiveVoiceCallLookup func(ctx context.Context, sessionID string) (callID int64, provider string, direct bool, ok bool)

// activeCallDBLookup 由 ws main 注入（基于 call_records 的 DB 反查），作为内存反查的跨节点兜底。
var activeCallDBLookup ActiveVoiceCallLookup

// SetActiveVoiceCallDBLookup 注入跨节点活跃通话反查（由 ws main.go 调用）。
func SetActiveVoiceCallDBLookup(fn ActiveVoiceCallLookup) {
	activeCallDBLookup = fn
}

// LookupActiveVoiceCall 反查会话的活跃 AI 语音通话：
// 本机内存态优先（call owner 节点命中，零开销），未命中再走跨节点 DB 兜底。
// 两路都查不到返回 ok=false（普通文字会话天然跳过）。
func LookupActiveVoiceCall(ctx context.Context, sessionID string) (int64, string, bool, bool) {
	if sessionID == "" {
		return 0, "", false, false
	}
	if callCtrl != nil {
		if callID, provider, direct, ok := callCtrl.GetActiveCallBySession(sessionID); ok {
			return callID, provider, direct, true
		}
	}
	if activeCallDBLookup != nil {
		return activeCallDBLookup(ctx, sessionID)
	}
	return 0, "", false, false
}

// ── 轮次状态（caller 转写时间戳 + 每轮注入一次）────────────────────
//
// 接点A（转写消费，NATS queue group 随机节点）和接点B（Hermes 回复，agent 连接所在节点）
// 可能落在不同 ws 副本上，轮次状态必须跨节点可见：Redis 可用时为唯一事实源，
// 仅在 Redis 未配置（单机测试）时退化为本机内存 map。

var (
	voiceInjectMu sync.Mutex
	callerTS      = map[string]time.Time{} // sessionID → 最近 caller 转写时间（仅无 Redis 时使用）
	injectOnce    = map[string]bool{}      // sessionID → 本轮已注入（仅无 Redis 时使用）
)

func voiceInjectTSKey(sessionID string) string {
	return "im:voice:inject:ts:" + sessionID
}

func voiceInjectOnceKey(sessionID string) string {
	return "im:voice:inject:once:" + sessionID
}

// RecordCallerTranscript 记录 caller 转写写入时间（接点A 触发时调用）。
// 同时清除本轮 injectOnce 标记，允许新一轮文字大脑回复注入。
func RecordCallerTranscript(sessionID string) {
	if store.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		store.RDB.Set(ctx, voiceInjectTSKey(sessionID), time.Now().UnixMilli(), voiceInjectStateTTL)
		store.RDB.Del(ctx, voiceInjectOnceKey(sessionID))
		return
	}
	voiceInjectMu.Lock()
	callerTS[sessionID] = time.Now()
	delete(injectOnce, sessionID)
	voiceInjectMu.Unlock()
}

// ClearCallerTranscript 通话结束时清理（避免状态残留）。
func ClearCallerTranscript(sessionID string) {
	if store.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		store.RDB.Del(ctx, voiceInjectTSKey(sessionID), voiceInjectOnceKey(sessionID))
		return
	}
	voiceInjectMu.Lock()
	delete(callerTS, sessionID)
	delete(injectOnce, sessionID)
	voiceInjectMu.Unlock()
}

// lastCallerTranscriptAt 返回最近一次 caller 转写时间；不存在返回 ok=false。
func lastCallerTranscriptAt(ctx context.Context, sessionID string) (time.Time, bool) {
	if store.RDB != nil {
		ms, err := store.RDB.Get(ctx, voiceInjectTSKey(sessionID)).Int64()
		if err != nil {
			return time.Time{}, false
		}
		return time.UnixMilli(ms), true
	}
	voiceInjectMu.Lock()
	ts, ok := callerTS[sessionID]
	voiceInjectMu.Unlock()
	return ts, ok
}

// tryMarkInjected 标记本轮已注入；已标记过返回 false（每轮只注入一次）。
func tryMarkInjected(ctx context.Context, sessionID string) bool {
	if store.RDB != nil {
		ok, err := store.RDB.SetNX(ctx, voiceInjectOnceKey(sessionID), 1, voiceInjectStateTTL).Result()
		if err != nil {
			// Redis 故障时放行注入：重复注入由 Python 侧 round_seq 去重兜底，宁多勿断。
			logger.L.Warnf("voice inject once setnx failed session=%s err=%v", sessionID, err)
			return true
		}
		return ok
	}
	voiceInjectMu.Lock()
	defer voiceInjectMu.Unlock()
	if injectOnce[sessionID] {
		return false
	}
	injectOnce[sessionID] = true
	return true
}

// voiceInjectGuardsPass 执行注入前的轮次与超时守卫，返回 false 表示本条回复放弃注入。
//
//   - 超时兜底：距最近 caller 转写超过阈值放弃；直拨场景阈值放宽到 voiceInjectDirectTimeout，
//     文字 agent 执行任务可达数分钟（架构文档 33）。
//   - 每轮只注入一次：托管同轮多条消息只取第一条；直拨场景放开——执行型 agent
//     习惯先应答后出结果，每条都应播报，过期保护由 Python 侧 round_seq/user_querying 兜底。
func voiceInjectGuardsPass(ctx context.Context, callID int64, sessionID string, direct bool) bool {
	timeout := voiceInjectTimeout
	if direct {
		timeout = voiceInjectDirectTimeout
	}
	if ts, hasTS := lastCallerTranscriptAt(ctx, sessionID); hasTS && time.Since(ts) > timeout {
		logger.L.Infof("voice inject timeout call=%d session=%s direct=%t elapsed=%v, skip", callID, sessionID, direct, time.Since(ts))
		return false
	}
	if !direct && !tryMarkInjected(ctx, sessionID) {
		logger.L.Infof("voice inject skip dup call=%d session=%s", callID, sessionID)
		return false
	}
	return true
}

// transcriptFlusher 由装配方注入 transcriptConsumer.FlushCall，通话结束时调用。
var transcriptFlusher func(callID int64)

// SetTranscriptFlusher 注入 transcriptConsumer.FlushCall（由 ws main.go 调用）。
func SetTranscriptFlusher(fn func(callID int64)) {
	transcriptFlusher = fn
}

// FlushCallTranscript 立即 flush 指定通话缓冲的访客句子，通话结束时调用。
func FlushCallTranscript(callID int64) {
	if transcriptFlusher != nil {
		transcriptFlusher(callID)
	}
}

// voiceInjectExtraEnvelope 仅解析判别注入与否所需的 channel_data.grix 标记。
type voiceInjectExtraEnvelope struct {
	ChannelData struct {
		Grix struct {
			ToolExecution json.RawMessage `json:"toolExecution"`
			Thinking      json.RawMessage `json:"thinking"`
		} `json:"grix"`
	} `json:"channel_data"`
}

// ShouldInjectVoiceMessage 判断一条 agent 消息是否应注入语音侧念回。
// 只放行给人看的最终文字回复；排除三类非答复内容（否则会被豆包当文字念出来污染通话）：
//   - 通话转写片段（msg_type=6）：caller/owner/ai_bot 的转写，注入会形成回声/自答；
//   - 工具执行卡片（channel_data.grix.toolExecution）：内容是 grix://card 卡片链接，是噪音；
//   - 思考过程（channel_data.grix.thinking）：agent 内部推理，不该读给人听。
func ShouldInjectVoiceMessage(msgType int16, extraRaw []byte) bool {
	if msgType == model.MsgTypeCallSegment {
		return false
	}
	if len(extraRaw) > 0 {
		var env voiceInjectExtraEnvelope
		if json.Unmarshal(extraRaw, &env) == nil {
			if len(env.ChannelData.Grix.ToolExecution) > 0 || len(env.ChannelData.Grix.Thinking) > 0 {
				return false
			}
		}
	}
	return true
}

// MaybeInjectVoiceReply 在文字托管回复产出后，若该会话存在活跃 AI 语音通话，
// 把回复文本注入语音侧（接点B 下行注入）。
//
// 每轮 caller 说话只注入一次（tryMarkInjected）：Hermes 可能产出多条消息，
// 豆包只需要第一条语义完整的回复，避免重复 502 打断正在播报的内容。
// 超时兜底（架构文档 29 §5）：距最近 caller 转写超过 voiceInjectTimeout 的回复放弃注入。
// 防串线（架构文档 29 §4.2.1）：反查走内存态 + DB 活跃态（时间窗+歧义守卫），
// 普通文字会话反查为空即静默跳过，不影响文字主链路。
//
// 直拨调度场景（direct，架构文档 33）放宽两条规则：
//   - 超时阈值放宽到 voiceInjectDirectTimeout——文字 agent 执行任务可达数分钟；
//   - 不限"每轮一次"——执行型 agent 习惯先应答后出结果，每条回复都应播报，
//     过期保护由 Python 侧 round_seq 与 user_querying 守卫兜底。
func MaybeInjectVoiceReply(ctx context.Context, sessionID, content string) {
	if store.NC == nil || sessionID == "" {
		return
	}
	text := strings.TrimSpace(content)
	if text == "" {
		return
	}
	callID, provider, direct, ok := LookupActiveVoiceCall(ctx, sessionID)
	if !ok || callID <= 0 {
		return // 无活跃语音通话：普通文字会话天然跳过
	}

	if !voiceInjectGuardsPass(ctx, callID, sessionID, direct) {
		return
	}

	payload, err := json.Marshal(map[string]any{
		"text":      text,
		"provider":  provider,
		"round_seq": time.Now().UnixMilli(), // 单调递增，Python 侧丢弃过期注入
	})
	if err != nil {
		logger.L.Warnf("voice inject marshal failed call=%d session=%s err=%v", callID, sessionID, err)
		return
	}

	subject := fmt.Sprintf("%s%d", SubjectVoiceInjectPrefix, callID)
	if err := store.NC.Publish(subject, payload); err != nil {
		logger.L.Warnf("voice inject publish failed call=%d session=%s err=%v", callID, sessionID, err)
		return
	}
	logger.L.Infof("voice inject published call=%d session=%s provider=%s len=%d", callID, sessionID, provider, len(text))
}

// ── 边写边念（流式按句注入）────────────────────────────────────────
//
// 文字大脑流式产出期间，把已成型的完整句逐句注入语音侧，让音频紧跟文字、不再等整段写完。
// 仅对有活跃语音通话的会话生效（HasActiveVoiceCallHint 廉价门控，避免给所有 agent 流加开销）。
// 是否真正逐句念由 voicebridge 按通话是否为 STT+TTS 管线决定：
//   - 管线（语音大脑 relay）：念分句(eot=false) + 收尾补未念尾段(tail)；
//   - 端到端（豆包/openai 自答）：忽略分句，仅用收尾帧的整段 text，行为与改动前完全一致。
// 因此后端无需区分通话类型，统一发"分句 + 收尾整段"，由桥侧路由。

func voiceStreamStateKey(agentID int64, clientMsgID string) string {
	return fmt.Sprintf("im:voice:stream:%d:%s", agentID, clientMsgID)
}

// HasActiveVoiceCallHint 廉价门控：仅看会话是否存在活跃语音通话的 Redis 痕迹（caller 转写时间戳 key，
// 通话期间存在），以极小代价过滤掉绝大多数无语音通话的会话，避免每个流式 chunk 做昂贵的活跃通话反查。
func HasActiveVoiceCallHint(ctx context.Context, sessionID string) bool {
	if store.RDB == nil || sessionID == "" {
		return false
	}
	n, err := store.RDB.Exists(ctx, voiceInjectTSKey(sessionID)).Result()
	return err == nil && n > 0
}

var voiceSentenceEnders = map[rune]bool{
	'。': true, '！': true, '？': true, '!': true, '?': true,
	'\n': true, '；': true, ';': true, '…': true,
}

// splitCompleteSentences 把文本切成完整句（到最后一个句末标点为止），返回各完整句与已消费字节数；
// 尾部不足一句的残段不返回（留待更多内容到达或收尾时处理）。consumed 落在 UTF-8 边界，可安全切片。
func splitCompleteSentences(s string) (sentences []string, consumed int) {
	start := 0
	for i, r := range s {
		if voiceSentenceEnders[r] {
			end := i + utf8.RuneLen(r)
			if seg := strings.TrimSpace(s[start:end]); seg != "" {
				sentences = append(sentences, seg)
			}
			start = end
			consumed = end
		}
	}
	return sentences, consumed
}

// publishVoiceInjectFrame 发一帧注入（分句或收尾）到 voicebridge.inject.<callID>。
func publishVoiceInjectFrame(callID int64, provider, text string, round int64, seq int, eot, streamed bool, tail string) {
	if store.NC == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"text":      text,
		"provider":  provider,
		"round_seq": round,
		"seq":       seq,
		"eot":       eot,
		"streamed":  streamed,
		"tail":      tail,
	})
	if err != nil {
		return
	}
	subject := fmt.Sprintf("%s%d", SubjectVoiceInjectPrefix, callID)
	if err := store.NC.Publish(subject, payload); err != nil {
		logger.L.Warnf("voice stream inject publish failed call=%d err=%v", callID, err)
	}
}

// MaybeStreamVoiceSentence 边写边念主入口。rawContent 为到目前为止累积的（原始 builder）文本，
// cursor/tail 全程基于它计算以保证一致；cleanFull 为收尾整段（端到端通话整体念用，保持原 clean 口径）。
// isFinal=false：注入本次新成型的完整句（分句帧）；isFinal=true：补发收尾帧（整段 text + 未念 tail + streamed）并清状态。
func MaybeStreamVoiceSentence(ctx context.Context, sessionID string, agentID int64, clientMsgID, rawContent, cleanFull string, isFinal bool) {
	if store.RDB == nil || store.NC == nil || sessionID == "" || strings.TrimSpace(clientMsgID) == "" {
		return
	}
	if !HasActiveVoiceCallHint(ctx, sessionID) {
		return
	}
	callID, provider, direct, ok := LookupActiveVoiceCall(ctx, sessionID)
	if !ok || callID <= 0 {
		return
	}

	stateKey := voiceStreamStateKey(agentID, clientMsgID)
	vals, _ := store.RDB.HMGet(ctx, stateKey, "cursor", "round", "ok").Result()
	cursor := 0
	if len(vals) > 0 && vals[0] != nil {
		cursor, _ = strconv.Atoi(fmt.Sprint(vals[0]))
	}
	var round int64
	if len(vals) > 1 && vals[1] != nil {
		round, _ = strconv.ParseInt(fmt.Sprint(vals[1]), 10, 64)
	}
	gateState := ""
	if len(vals) > 2 && vals[2] != nil {
		gateState = fmt.Sprint(vals[2])
	}
	if gateState == "0" {
		return // 本条消息已被护栏拦下（过期/非本轮），整条不注入
	}
	// 每条消息只过一次护栏（超时兜底 + 每轮一次防串线），结果存状态供后续分片/收尾复用，
	// 不重复 mark。首帧(round 未定)时判定；护栏与端到端旧逻辑一致，端到端通话因此零回归。
	if round == 0 {
		if !voiceInjectGuardsPass(ctx, callID, sessionID, direct) {
			store.RDB.HSet(ctx, stateKey, "ok", "0")
			store.RDB.Expire(ctx, stateKey, voiceInjectStateTTL)
			return
		}
		round = time.Now().UnixMilli()
		store.RDB.HSet(ctx, stateKey, "round", round, "ok", "1")
		store.RDB.Expire(ctx, stateKey, voiceInjectStateTTL)
	}
	if cursor < 0 || cursor > len(rawContent) {
		cursor = 0
	}

	if isFinal {
		tail := strings.TrimSpace(rawContent[cursor:])
		streamed := cursor > 0
		// 收尾帧：text=clean 整段（端到端通话整体念，口径同旧 MaybeInjectVoiceReply）；
		// tail=未念尾段（管线只补尾）；streamed=是否已分句念过（管线据此避免整段重复念）。
		publishVoiceInjectFrame(callID, provider, cleanFull, round, cursor+1, true, streamed, tail)
		store.RDB.Del(ctx, stateKey)
		logger.L.Infof("voice stream final call=%d session=%s streamed=%t taillen=%d fulllen=%d", callID, sessionID, streamed, len(tail), len(cleanFull))
		return
	}

	sentences, consumed := splitCompleteSentences(rawContent[cursor:])
	if len(sentences) == 0 {
		return // 尚无新完整句，等更多内容
	}
	seq := cursor
	for _, snt := range sentences {
		seq++
		publishVoiceInjectFrame(callID, provider, snt, round, seq, false, false, "")
	}
	newCursor := cursor + consumed
	store.RDB.HSet(ctx, stateKey, "cursor", newCursor)
	store.RDB.Expire(ctx, stateKey, voiceInjectStateTTL)
}

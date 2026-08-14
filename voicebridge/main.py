"""
Voice Bridge Worker — Python 实现

架构：
  Go NATSBridgeManager.StartBridge()
    → NATS: voicebridge.control.start {call_id, agent_id, voice_provider, system_prompt, voice}
    → Python 订阅此消息，主动加入 LiveKit 房间，启动 AgentSession
    → 回复 {"ok": true} 确认（等 session.start() 成功后才回复）

  Go NATSBridgeManager.InterruptBridge()
    → NATS: voicebridge.control.interrupt {call_id}
    → Python 关闭当前 session 并移出活跃表，回复确认

  Go NATSBridgeManager.StopBridge()
    → NATS: voicebridge.control.stop {call_id}
    → Python 关闭 session

环境变量：
  LIVEKIT_URL          LiveKit Server URL（如 ws://localhost:7880）
  LIVEKIT_INTERNAL_URL 可选，k8s 集群内直连地址（如 ws://aibot-livekit:7880），优先使用
  LIVEKIT_API_KEY      LiveKit API Key
  LIVEKIT_API_SECRET   LiveKit API Secret
  NATS_URL             NATS Server URL（默认 nats://localhost:4222）

说明：语音大模型的 provider/model/endpoint/api_key/voice 均由 start 消息下发（BYOK），
不再使用 OPENAI_API_KEY / OPENAI_REALTIME_MODEL 等环境变量兜底。
"""

import asyncio
import json
import logging
import os
import sys
import traceback
import time

import nats
from dotenv import load_dotenv
from livekit import rtc
from livekit.agents import Agent, AgentSession
from livekit.agents.voice.room_io import RoomInputOptions
from livekit.agents.utils import http_context
from livekit.plugins import openai as lk_openai

try:
    from livekit.plugins import volcengine as lk_volcengine
except ImportError:
    lk_volcengine = None

# reload 守卫：下面对 livekit/volcengine 的 monkey-patch 每进程只能应用一次。
# 重复执行模块体（importlib.reload / 双路径 import）时，包装函数经模块全局
# _orig_* 取原函数，而全局已被覆盖成上一轮的包装 → 自引用无限递归。
# 哨兵挂在 sys 上（reload 不清），各 patch 块首行抛 _AlreadyPatched 跳过。
_MONKEY_PATCHED = bool(getattr(sys, "_grix_voicebridge_patched", False))
sys._grix_voicebridge_patched = True


class _AlreadyPatched(Exception):
    """monkey-patch 已应用过（模块体重复执行），本块静默跳过。"""

# Monkey-patch: volcengine 插件 ChanClosed 竞态 bug 修复
try:
    if _MONKEY_PATCHED:
        raise _AlreadyPatched()
    from livekit.agents.utils.aio.channel import Chan, ChanClosed

    _original_send_nowait = Chan.send_nowait

    def _safe_send_nowait(self, item):
        try:
            _original_send_nowait(self, item)
        except ChanClosed:
            pass

    Chan.send_nowait = _safe_send_nowait
except Exception:
    pass

# Monkey-patch: livekit-plugins-volcengine==1.3.0 在 _recv_task 中直接
# parse_response(msg.data)，当 ws 收到 close/ping 等控制帧时 msg.data 可能为 int，
# 触发 TypeError 并导致接收协程退出。这里将非业务帧转换为空响应，保持会话持续。
if lk_volcengine is not None:
    try:
        if _MONKEY_PATCHED:
            raise _AlreadyPatched()
        from livekit.plugins.volcengine import realtime as _ve_realtime

        _orig_parse_response = _ve_realtime.parse_response

        def _safe_parse_response(res):
            if isinstance(res, int):
                return {}
            if isinstance(res, memoryview):
                res = res.tobytes()
            elif isinstance(res, bytearray):
                res = bytes(res)
            return _orig_parse_response(res)

        _ve_realtime.parse_response = _safe_parse_response
    except Exception:
        pass

from bridge_config import BridgeConfigError, build_realtime_model, parse_start_message, sanitize_doubao_system_role
from node_identity import resolve_node_id
from openai_orchestrator import OpenAIVoiceOrchestrator
from owner_audio import OwnerTurnDetector, frame_rms
from prompt_template import build_openai_instructions, tag_authoritative
import stt_tts_pipeline
from volc_fix import should_drop_dup_tts_end


# 丢弃帧的哨兵：volcengine _recv_task 循环对 data 为 None 的消息会 continue 跳过。
class _SkipMessage:
    data = None


_SKIP_MSG = _SkipMessage()

load_dotenv()
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("voicebridge")

# 诊断模式：临时开启 volcengine 插件全量日志，排查 segments=0 问题
logging.getLogger("livekit.plugins.volcengine").setLevel(logging.DEBUG)

# handover 中转：on_interrupt 保存旧 room，start_bridge 时断开避免 DuplicateIdentity
_handover_rooms: dict[int, rtc.Room] = {}

# Monkey-patch: volcengine _start_session 诊断日志 + 超时
try:
    if _MONKEY_PATCHED:
        raise _AlreadyPatched()
    from livekit.plugins.volcengine.realtime import RealtimeSession

    _orig_start_session = RealtimeSession._start_session
    _orig_run_ws = RealtimeSession._run_ws

    # 解析 volcengine 二进制协议的辅助函数
    # 用火山官方 SDK 的 parse_response 精确解析协议
    def _parse_volcengine_response(data: bytes) -> str:
        """使用火山官方 SDK 的 parse_response 逻辑解析二进制响应。"""
        import gzip as _gz, json as _json
        try:
            header_size = data[0] & 0x0F
            message_type = data[1] >> 4
            message_type_specific_flags = data[1] & 0x0F
            serialization_method = data[2] >> 4
            message_compression = data[2] & 0x0F
            payload = data[header_size * 4:]

            result = {"raw_type": message_type}
            start = 0

            if message_type == 0x0F:  # SERVER_ERROR_RESPONSE
                code = int.from_bytes(payload[:4], "big", signed=False)
                payload_size = int.from_bytes(payload[4:8], "big", signed=False)
                payload_msg = payload[8:]
                if message_compression == 1:
                    payload_msg = _gz.decompress(payload_msg)
                if serialization_method == 1:
                    payload_msg = _json.loads(str(payload_msg, "utf-8"))
                else:
                    payload_msg = str(payload_msg, "utf-8", errors="replace")
                return f"ERROR code={code} msg={payload_msg}"

            type_names = {9: "FULL_RESPONSE", 0xB: "ACK"}
            type_name = type_names.get(message_type, f"TYPE_0x{message_type:X}")

            if message_type_specific_flags & 0x02 > 0:  # NEG_SEQUENCE
                start += 4
            if message_type_specific_flags & 0x04 > 0:  # MSG_WITH_EVENT
                result['event'] = int.from_bytes(payload[start:start+4], "big", signed=False)
                start += 4

            payload = payload[start:]
            sid_size = int.from_bytes(payload[:4], "big", signed=True)
            result['session_id'] = str(payload[4:4+sid_size])
            payload = payload[4 + sid_size:]
            payload_size = int.from_bytes(payload[:4], "big", signed=False)
            payload_msg = payload[4:]

            if message_compression == 1:
                payload_msg = _gz.decompress(payload_msg)
            if serialization_method == 1:
                payload_msg = _json.loads(str(payload_msg, "utf-8"))
            else:
                payload_msg = str(payload_msg, "utf-8", errors="replace")[:300]
            result['payload'] = payload_msg
            return f"{type_name} event={result.get('event')} payload={payload_msg}"
        except Exception as e:
            return f"parse_error: {e}"

    # 直接 patch 原始 _start_session 来记录回复内容
    from livekit.plugins.volcengine.realtime import RealtimeSession as _RS

    async def _diag_start_session(self, ws_conn, dialog_id):
        # 用 IM session_id 作为 dialog_id，保证同一会话跨通话上下文连续（挂断重拨豆包仍记得历史）。
        # session_id 不存在时退化到 call_id，保证同通话 suspend/resume 连续。
        # 注意：只改 dialog_id（JSON payload 里的对话标识），不改 self.session_id（二进制协议路由标识）
        _ctx = getattr(getattr(self, '_realtime_model', None), '_grix_ctx', None)
        if _ctx:
            sid = _ctx.get('session_id') or ''
            dialog_id = sid if sid else str(_ctx.get('call_id', dialog_id))
            logger.info(f"[VolcDiag] dialog_id override: session_id={sid!r} call_id={_ctx.get('call_id')} -> dialog_id={dialog_id}")
        logger.info(f"[VolcDiag] _start_session sending request, dialog_id={dialog_id}, session_id={self.session_id}")
        try:
            import gzip, json

            request_params = self._realtime_model._opts.get_start_session_reqs(dialog_id=dialog_id)
            # 打印发给 volcengine 的完整参数
            logger.info(f"[VolcDiag] _start_session request params: {json.dumps(request_params, ensure_ascii=False)}")

            payload_bytes = str.encode(json.dumps(request_params))
            payload_bytes = gzip.compress(payload_bytes)
            from livekit.plugins.volcengine.realtime import generate_header
            start_session_request = bytearray(generate_header())
            start_session_request.extend(int(100).to_bytes(4, "big"))
            start_session_request.extend((len(self.session_id)).to_bytes(4, "big"))
            start_session_request.extend(str.encode(self.session_id))
            start_session_request.extend((len(payload_bytes)).to_bytes(4, "big"))
            start_session_request.extend(payload_bytes)
            await ws_conn.send_bytes(start_session_request)

            response = await asyncio.wait_for(ws_conn.receive_bytes(), timeout=15)
            logger.info(f"[VolcDiag] _start_session response: {_parse_volcengine_response(response)}")
        except asyncio.TimeoutError:
            logger.error("[VolcDiag] _start_session TIMEOUT 15s")
            raise

    async def _diag_run_ws(self, ws_conn):
        logger.info("[VolcDiag] _run_ws started")
        try:
            await _orig_run_ws(self, ws_conn)
        except Exception as e:
            logger.error(f"[VolcDiag] _run_ws failed: {type(e).__name__}: {e}")
            raise
        finally:
            logger.info("[VolcDiag] _run_ws ended")

    RealtimeSession._start_session = _diag_start_session
    RealtimeSession._run_ws = _diag_run_ws

    # 修复：volcengine 插件 get_start_session_reqs 缺少 recv_timeout 和 input_mod
    # 导致 DialogAudioIdleTimeoutError (code=52000042)
    from livekit.plugins.volcengine.realtime import _RealtimeOptions

    _orig_get_start_session_reqs = _RealtimeOptions.get_start_session_reqs

    def _fixed_get_start_session_reqs(self, dialog_id: str | None) -> dict:
        req = _orig_get_start_session_reqs(self, dialog_id)
        if "dialog" in req and "extra" in req["dialog"]:
            req["dialog"]["extra"]["recv_timeout"] = 60
            req["dialog"]["extra"]["input_mod"] = "audio"
        logger.info("[VolcDiag] get_start_session_reqs patched: added recv_timeout=60, input_mod=audio")
        return req

    _RealtimeOptions.get_start_session_reqs = _fixed_get_start_session_reqs
    logger.info("Patched _RealtimeOptions.get_start_session_reqs with recv_timeout + input_mod")

    # 诊断：push_audio 是否被调用（per-session 计数，每次通话独立）
    _orig_push_audio = RealtimeSession.push_audio

    def _diag_push_audio(self, frame):
        if not hasattr(self, '_diag_push_count'):
            self._diag_push_count = 0
        self._diag_push_count += 1
        if self._diag_push_count <= 5:
            logger.info(f"[VolcDiag] push_audio called #{self._diag_push_count} frame={frame.sample_rate}Hz/{frame.samples_per_channel}samples")
        elif self._diag_push_count == 6:
            logger.info(f"[VolcDiag] push_audio ... (subsequent frames suppressed)")
        _orig_push_audio(self, frame)

    RealtimeSession.push_audio = _diag_push_audio

    # 诊断：AgentSession.start() 后检查 input.audio 是否就绪
    from livekit.agents.voice import AgentSession as _AgentSession

    _orig_start = _AgentSession.start

    async def _diag_start(self, agent, **kwargs):
        result = await _orig_start(self, agent, **kwargs)
        has_audio = self.input.audio is not None
        has_forward = self._forward_audio_atask is not None
        logger.info(f"[VolcDiag] AgentSession.start() done: input.audio={'SET' if has_audio else 'NONE'}, _forward_audio_atask={'STARTED' if has_forward else 'NONE'}")
        return result

    _AgentSession.start = _diag_start

    logger.info("Patched volcengine RealtimeSession + AgentSession with diagnostic logging")

    # 诊断：RoomIO 音频输入流是否收到 track_subscribed 事件
    # 修复：iOS 客户端发布音频时 source=SOURCE_UNKNOWN，也应收录。
    from livekit.agents.voice.room_io._input import _ParticipantInputStream
    from livekit import rtc as _rtc

    _SOURCE_MIC = _rtc.TrackSource.SOURCE_MICROPHONE
    _SOURCE_UNK = _rtc.TrackSource.SOURCE_UNKNOWN

    _orig_on_track_available = _ParticipantInputStream._on_track_available

    def _diag_on_track_available(self, track, publication, participant):
        identity_match = self._participant_identity == participant.identity
        source_ok = publication.source in self._accepted_sources
        logger.info(
            f"[VolcDiag] track_subscribed: participant={participant.identity} "
            f"self._participant_identity={self._participant_identity} "
            f"source={publication.source} accepted={self._accepted_sources} "
            f"identity_match={identity_match} source_ok={source_ok} "
            f"track={type(track).__name__}"
        )
        # 修复：SOURCE_UNKNOWN 等同 SOURCE_MICROPHONE 接受
        if not source_ok and publication.source == _SOURCE_UNK and _SOURCE_MIC in self._accepted_sources:
            self._accepted_sources.add(_SOURCE_UNK)
            logger.info(f"[VolcDiag] auto-added SOURCE_UNKNOWN to accepted_sources (iOS workaround)")
        return _orig_on_track_available(self, track, publication, participant)

    _ParticipantInputStream._on_track_available = _diag_on_track_available

    # 诊断 + 转写回灌：拦截 volcengine WebSocket 消息，
    # 提取事件 459(用户ASR) 和 350(AI TTS文字) 的文本，发布到 NATS。
    _orig_run_ws2 = RealtimeSession._run_ws  # 已经被 patch 过，拿到最新版

    def _extract_text_event(data: bytes):
        """从豆包二进制协议中提取转写文字事件。
        返回 (role, text) 或 None。role 为 "caller" 或 "ai_bot"。

        实际协议行为（来自线上日志验证）：
        - 事件 451(ASR streaming): payload.results[0].text 包含转写文字，
          is_interim=False 时为最终结果。用户说的 "豆包，豆包在不在？"
        - 事件 459(ASRSentenceDone): 仅包含时间元数据，无 text 字段
        - 事件 350(TTSSentenceStart): text 字段为空字符串，AI 文字不在此事件
        """
        import gzip as _gz, json as _json
        try:
            if len(data) < 4:
                return None
            header_size = (data[0] & 0x0F) * 4
            message_type = data[1] >> 4
            flags = data[1] & 0x0F
            serialization = data[2] >> 4
            compression = data[2] & 0x0F
            payload = data[header_size:]

            # 只处理 SERVER_FULL_RESPONSE (0x9)
            if message_type != 0x9:
                return None

            start = 0
            # POS_SEQUENCE(0b0001) 和 NEG_SEQUENCE(0b0010) 都占 4 字节
            if flags & 0x03:  # any sequence flag
                start += 4
            if flags & 0x04:  # MSG_WITH_EVENT
                event_num = int.from_bytes(payload[start:start+4], "big", signed=False)
                start += 4
            else:
                return None

            # 只关心 ASR streaming result(451)
            # 事件 459 无 text，事件 350 text 为空
            if event_num != 451:
                return None

            payload = payload[start:]
            sid_size = int.from_bytes(payload[:4], "big", signed=True)
            payload = payload[4 + sid_size:]
            payload_size = int.from_bytes(payload[:4], "big", signed=False)
            payload_msg = payload[4:4 + payload_size]

            if compression == 1:
                payload_msg = _gz.decompress(payload_msg)
            if serialization == 1:
                payload_msg = _json.loads(str(payload_msg, "utf-8"))
            else:
                return None

            if not isinstance(payload_msg, dict):
                return None

            # 只取最终结果（is_interim=False）
            results = payload_msg.get("results", [])
            if not results or not isinstance(results, list):
                return None
            first = results[0] if isinstance(results[0], dict) else {}
            if first.get("is_interim", True):
                return None  # 跳过中间结果
            text = first.get("text", "")
            if not text or not text.strip():
                return None
            return ("caller", text.strip())
        except Exception:
            return None

    # volcengine 静默超时：session 初始化后 N 秒无 ASR 回复则报错
    _VOLCENGINE_SILENCE_TIMEOUT = 15  # 秒
    # 应答僵死超时：用户说完(459)后豆包 N 秒既不应答(550)也无新一轮(450)，判定会话僵死。
    # 正常应答仅 1~2 秒，取 20 秒为 10 倍冗余，避免误杀慢应答；用户再开口会自动解除武装。
    _VOLCENGINE_STALL_TIMEOUT = 20  # 秒

    async def _diag_recv_run_ws(self, ws_conn):
        import gzip as _gzip, json as _json

        _recv_counter = [0]
        _got_asr = [False]  # 是否收到过 ASR 事件(450/451)
        _awaiting_since = [None]  # 用户说完(459)等待豆包应答的起点(monotonic)；550/450 时解除
        _orig_receive = ws_conn.receive

        # 从 RealtimeModel 上获取回灌上下文
        _ctx = getattr(getattr(self, '_realtime_model', None), '_grix_ctx', None)
        _call_id = _ctx['call_id'] if _ctx else 'unknown'

        async def _logging_receive(timeout=None):
            msg = await _orig_receive(timeout)
            if msg.data is not None and len(msg.data) > 0:
                # 修复 volcengine 1.3.0 _recv_task 崩溃：豆包一轮回复可能重复发
                # TTSEnded(359)，第二次到达时 _current_generation 已为 None，插件会执行
                # None.message_ch.close() 抛 AttributeError → recv 循环 break → 整条豆包
                # 连接断开 → 此后用户说话无人应答。丢弃这帧即可绕开崩溃分支。
                if should_drop_dup_tts_end(msg.data, getattr(self, '_current_generation', None) is not None):
                    logger.info(f"[VolcFix] drop duplicate TTSEnded(359) (no active generation) call_id={_call_id}")
                    return _SKIP_MSG
                _recv_counter[0] += 1
                if _recv_counter[0] <= 20:
                    logger.info(f"[VolcDiag] ws_recv #{_recv_counter[0]} bytes={len(msg.data)} parsed={_parse_volcengine_response(msg.data)}")
                elif _recv_counter[0] == 21:
                    logger.info(f"[VolcDiag] ws_recv ... (subsequent suppressed)")

                # 检测是否收到 ASR 相关事件(450=ASRInfo, 451=ASRResponse)
                try:
                    if len(msg.data) > 8:
                        flags = msg.data[1] & 0x0F
                        offset = ((msg.data[0] & 0x0F) * 4)
                        if flags & 0x03:
                            offset += 4
                        if flags & 0x04 and len(msg.data) > offset + 4:
                            ev = int.from_bytes(msg.data[offset:offset+4], "big")
                            if ev in (450, 451):
                                _got_asr[0] = True
                            # 事件 450 = 新一轮用户 ASR 开始：清除 AI 回复标记，置位用户说话中。
                            # 同时解除僵死武装——用户又开口了，不再等上一轮的应答。
                            if ev == 450 and _ctx is not None:
                                if 'ai_responding' in _ctx:
                                    _ctx['ai_responding'][0] = False
                                if 'user_querying' in _ctx:
                                    _ctx['user_querying'][0] = True
                                _awaiting_since[0] = None
                            # 事件 459 = ASRSentenceDone：用户说完，清除用户说话标记，开始等豆包应答。
                            # 方案C：不压制豆包、不等待，让其自主即时应答；Hermes 详细答案
                            # 随后经 502 external_rag 作为背景资料注入，供豆包后续回答参考。
                            if ev == 459 and _ctx is not None and 'user_querying' in _ctx:
                                _ctx['user_querying'][0] = False
                                _awaiting_since[0] = time.monotonic()
                            # 语音大脑(方案A)：放行豆包自答的简短寒暄当"垫场"，填住用户说完到
                            # agent 答复念出之间的静音空档；豆包系统提词已定义为"接待员，只热情寒暄
                            # /过渡、不替 agent 答业务"。文字 agent 的真实答复仍经注入(forwarding 期间)
                            # 的独立播放通道念出；不再静音豆包自答。
                            if _ctx is not None and _ctx.get('relay_mode'):
                                _fwd = _ctx.get('relay_forwarding', [False])[0]
                                # 打断：念稿播放中(forwarding)用户一开口(450 首字识别)，立即停止往
                                # 房间喂注入音频、并强制关闭当前念稿通道，使其秒静。否则插件层无主动
                                # 停播，会"边清缓冲边喂"、喊多次才停（长段尤甚）——老郭实测痛点。
                                # 强制关后 doubao 念稿余下的 352/359 由 forwarding=False 与 VolcFix
                                # 兜底丢弃，下一轮注入新建干净播放通道。
                                if ev == 450 and _fwd:
                                    _ctx['relay_forwarding'][0] = False
                                    # 强制打断 AgentSession：绕过 0.5s 阈值、立即冲掉已排队音频，秒静。
                                    _sess = _ctx.get('agent_session')
                                    if _sess is not None:
                                        try:
                                            _sess.interrupt(force=True)
                                        except Exception:
                                            pass
                                    try:
                                        self._grix_close_playback_turn()
                                    except Exception:
                                        pass
                                    logger.info(f"[Inject] barge-in stop reading call_id={_call_id}")
                                elif ev == 359 and _fwd:
                                    _ctx['relay_forwarding'][0] = False
                            # 事件 550 = 豆包 ChatResponse（开始应答）：解除僵死武装。
                            if ev == 550:
                                _awaiting_since[0] = None
                except Exception:
                    pass

                # 转写回灌：提取用户ASR文字，发布到NATS
                # AI 回复期间豆包会回送 451 事件（echo），跳过避免重复
                if _ctx is not None and not _ctx.get('ai_responding', [False])[0]:
                    result = _extract_text_event(msg.data)
                    if result is not None:
                        role, text = result
                        # 方案B：主人插话期间，这段 451 转写来自主人而非客户，
                        # 豆包要听见并回应，但不能把主人的话落进 IM 会话。
                        if _owner_suppressed(_ctx['call_id']):
                            logger.info(f"[OwnerAudio] transcript suppressed (owner) call_id={_ctx['call_id']} text={text[:80]}")
                        else:
                            _ctx['seq'][0] += 1
                            _publish_transcript(
                                _ctx['nc'], _ctx['call_id'],
                                _ctx['seq'][0], role, text, _ctx['provider'],
                            )
                            logger.info(f"[VolcDiag] transcript published call_id={_ctx['call_id']} role={role} text={text[:80]}")
            return msg

        ws_conn.receive = _logging_receive

        # 启动静默超时检测任务
        async def _silence_watchdog():
            await asyncio.sleep(_VOLCENGINE_SILENCE_TIMEOUT)
            if not _got_asr[0]:
                push_count = getattr(self, '_diag_push_count', 0)
                logger.error(
                    f"[VolcDiag] SILENCE_TIMEOUT: volcengine 初始化后 {_VOLCENGINE_SILENCE_TIMEOUT}s "
                    f"无 ASR 回复 call_id={_call_id} ws_recv_count={_recv_counter[0]} "
                    f"push_audio_count={push_count}"
                )
                if _ctx is not None:
                    _publish_bridge_error(
                        _ctx['nc'], _ctx['call_id'],
                        'volcengine_silence_timeout',
                        f'volcengine 初始化后 {_VOLCENGINE_SILENCE_TIMEOUT}s 无 ASR 回复, push_audio={push_count}',
                        _ctx['provider'],
                    )
                    # 立即终止 bridge，触发正常的 bridge_exit 清理流程，
                    # 避免等待 30 分钟超时兜底导致 busy key 长期残留。
                    async def _stop_self():
                        try:
                            stop_payload = json.dumps({"call_id": _ctx['call_id']}).encode()
                            await _ctx['nc'].publish(SUBJECT_STOP, stop_payload)
                            logger.info(f"[VolcDiag] silence_timeout stop sent call_id={_ctx['call_id']}")
                        except Exception as _e:
                            logger.warning(f"[VolcDiag] silence_timeout stop failed call_id={_ctx['call_id']}: {_e}")
                    asyncio.create_task(_stop_self())

        # 应答僵死看门狗：用户说完(459)后豆包既不应答(550)也无新一轮(450)超 N 秒，
        # 判定豆包会话僵死，干净结束本通话（释放占用、用户可重拨），避免无限干等。
        async def _stall_watchdog():
            while True:
                await asyncio.sleep(3)
                since = _awaiting_since[0]
                if since is None:
                    continue
                if (time.monotonic() - since) <= _VOLCENGINE_STALL_TIMEOUT:
                    continue
                _awaiting_since[0] = None  # 防重复触发
                logger.error(
                    f"[VolcDiag] STALL_TIMEOUT: 用户说完后豆包 {_VOLCENGINE_STALL_TIMEOUT}s "
                    f"无应答，判定会话僵死 call_id={_call_id} ws_recv_count={_recv_counter[0]}"
                )
                if _ctx is not None:
                    _publish_bridge_error(
                        _ctx['nc'], _ctx['call_id'],
                        'volcengine_response_stall',
                        f'用户说完后豆包 {_VOLCENGINE_STALL_TIMEOUT}s 无应答，会话僵死',
                        _ctx['provider'],
                    )
                    try:
                        stop_payload = json.dumps({"call_id": _ctx['call_id']}).encode()
                        await _ctx['nc'].publish(SUBJECT_STOP, stop_payload)
                        logger.info(f"[VolcDiag] stall_timeout stop sent call_id={_ctx['call_id']}")
                    except Exception as _e:
                        logger.warning(f"[VolcDiag] stall_timeout stop failed call_id={_ctx['call_id']}: {_e}")
                return

        watchdog = asyncio.create_task(_silence_watchdog())
        stall_watchdog = asyncio.create_task(_stall_watchdog())
        # 接点B：暴露 ws_conn 供外部注入（502），并把 session 注册到注入路由表。
        self._grix_ws_conn = ws_conn
        if _ctx is not None and isinstance(_ctx.get('call_id'), int):
            _inject_sessions[_ctx['call_id']] = self
            logger.info(f"[Inject] session registered call_id={_ctx['call_id']}")
        try:
            return await _orig_run_ws2(self, ws_conn)
        finally:
            watchdog.cancel()
            stall_watchdog.cancel()
            self._grix_ws_conn = None
            if _ctx is not None and isinstance(_ctx.get('call_id'), int):
                if _inject_sessions.get(_ctx['call_id']) is self:
                    _inject_sessions.pop(_ctx['call_id'], None)
                _inject_last_seq.pop(_ctx['call_id'], None)

    RealtimeSession._run_ws = _diag_recv_run_ws

    # 接点B：外部注入方法——把文字大脑回复用 502 ChatRAGText 送入豆包当前 session。
    # 豆包消化注入内容后用自己口气作答（必要时在后续话里自然调整说法）。
    async def _grix_inject_rag(self, content: str) -> bool:
        import gzip as _g, json as _j
        from livekit.plugins.volcengine.realtime import generate_header as _gen_header
        ws_conn = getattr(self, '_grix_ws_conn', None)
        if ws_conn is None or not content:
            return False
        # external_rag 按官方 SDK 规范封装为 JSON 文档数组字符串（[{title,content}]）；
        # 裸文本可能被豆包侧丢弃，导致注入内容未被采纳。
        rag_str = _j.dumps([{"title": "客服参考答案", "content": content}], ensure_ascii=False)
        payload_bytes = _g.compress(str.encode(_j.dumps({"external_rag": rag_str})))
        req = bytearray(_gen_header())
        req.extend(int(502).to_bytes(4, "big"))
        req.extend((len(self.session_id)).to_bytes(4, "big"))
        req.extend(str.encode(self.session_id))
        req.extend((len(payload_bytes)).to_bytes(4, "big"))
        req.extend(payload_bytes)
        await ws_conn.send_bytes(req)
        return True

    RealtimeSession._grix_inject_rag = _grix_inject_rag

    # 接点B（传声筒模式）：把文字大脑回复用 300 SayHello 送入豆包，令其一字不差念出，
    # 不经豆包大脑重组。与 _grix_inject_rag(502 external_rag、豆包自行组织措辞) 互斥，
    # 仅语音大脑(relay_mode) 通话走此路径。
    # 用 300 而非 500：300 SayHello 是"立刻念这段文字"的独立原语、与对话轮次解耦，开场白即用它
    # 可靠出声；500 ChatTTSText 绑死"用户当轮 query"、要赶在豆包自答前替换，外部 agent 慢 1.5~2s
    # 赶不上。payload 对齐开场白：{content}。
    # 注意：仅发 300 还不够——注入合成的音频必须落在一条活跃"播放轮次"里才会被插件送进房间，
    # 调用方需先 _grix_open_playback_turn 开通道，详见该方法注释。
    async def _grix_inject_tts(self, content: str) -> bool:
        import gzip as _g, json as _j
        from livekit.plugins.volcengine.realtime import generate_header as _gen_header
        ws_conn = getattr(self, '_grix_ws_conn', None)
        if ws_conn is None or not content:
            return False
        payload_bytes = _g.compress(str.encode(_j.dumps(
            {"content": content})))
        req = bytearray(_gen_header())
        req.extend(int(300).to_bytes(4, "big"))
        req.extend((len(self.session_id)).to_bytes(4, "big"))
        req.extend(str.encode(self.session_id))
        req.extend((len(payload_bytes)).to_bytes(4, "big"))
        req.extend(payload_bytes)
        await ws_conn.send_bytes(req)
        return True

    RealtimeSession._grix_inject_tts = _grix_inject_tts

    # 接点B 关键修复：注入合成的音频必须落在一条活跃"播放轮次(generation)"里，插件才会把它送进
    # 房间。插件仅在 ASRResponse(451)/开场白时建轮次，并在 TTSEnded(359) 时关闭、audio_ch 销毁。
    # 文字 agent 慢 1.5~2s，注入到达时豆包自己那轮早结束、通道已关，注入的音频被丢（日志
    # "no active generation"）。此处仿照开场白(realtime.py:474-510)在注入前新建一条播放轮次，
    # 让随后到达的 352 音频帧能进入房间；该轮次由注入念稿自身的 359 正常关闭，不会卡死。
    # 已有活跃轮次则复用（豆包自答尚未结束的边界），避免双开。
    def _grix_open_playback_turn(self, content: str) -> bool:
        import asyncio as _aio, time as _t
        from livekit.agents import llm as _llm, utils as _u
        from livekit.plugins.volcengine.realtime import (
            _ResponseGeneration as _RG, _MessageGeneration as _MG,
        )
        if getattr(self, '_current_generation', None) is not None:
            return False  # 复用现有轮次
        gen = _RG(
            message_ch=_u.aio.Chan(),
            function_ch=_u.aio.Chan(),
            messages={},
            _created_timestamp=_t.time(),
            _done_fut=_aio.Future(),
        )
        self._current_generation = gen
        self.emit("generation_created", _llm.GenerationCreatedEvent(
            message_stream=gen.message_ch,
            function_stream=gen.function_ch,
            user_initiated=False,
        ))
        item_id = _u.shortuuid()
        modalities_fut = _aio.Future()
        item = _MG(
            message_id=item_id,
            text_ch=_u.aio.Chan(),
            audio_ch=_u.aio.Chan(),
            modalities=modalities_fut,
        )
        item.modalities.set_result(["audio", "text"])
        self._current_item = item
        gen.message_ch.send_nowait(_llm.MessageGeneration(
            message_id=item_id,
            text_stream=item.text_ch,
            audio_stream=item.audio_ch,
            modalities=item.modalities,
        ))
        # 文字已完整：灌入并关闭 text 流；audio 流留待豆包念稿 352 帧填充、由 359 关闭
        item.text_ch.send_nowait(content)
        item.text_ch.close()
        self._first_tts_response = True
        return True

    RealtimeSession._grix_open_playback_turn = _grix_open_playback_turn

    # 打断用：强制关闭当前念稿播放通道，使注入的念稿立即停止送进房间。
    # 复刻插件 TTSEnded(359) 的关闭动作（realtime.py:669-679，去掉 opening 分支）：
    # 关 audio_ch（停止向房间输出）+ 关 message/function_ch + 置 _current_generation=None。
    # 关后 doubao 念稿余下的 352 因 forwarding=False 被静音、余下的 359 因 _current_generation
    # 为 None 被 VolcFix 丢弃，下一轮注入会新建干净通道，不残留。
    def _grix_close_playback_turn(self):
        gen = getattr(self, '_current_generation', None)
        if gen is None:
            return
        item = getattr(self, '_current_item', None)
        if item is not None:
            try:
                if not item.audio_ch.closed:
                    item.audio_ch.close()
            except Exception:
                pass
        try:
            gen.message_ch.close()
            gen.function_ch.close()
        except Exception:
            pass
        self._current_generation = None
        self._first_tts_response = True

    RealtimeSession._grix_close_playback_turn = _grix_close_playback_turn

except _AlreadyPatched:
    pass
except Exception as e:
    logger.warning(f"Failed to patch volcengine: {e}")

SUBJECT_START     = "voicebridge.control.start"
SUBJECT_STOP      = "voicebridge.control.stop"
SUBJECT_INTERRUPT = "voicebridge.control.interrupt"
# 四档语音通话：接管=静音 AI（保留 session，连接/上下文不断），交回=恢复发声。
# 与原 interrupt（aclose 销毁 session）不同，mute/unmute 不关闭 session，
# 满足"AI 始终在线、交回秒恢复、API 不断"。
SUBJECT_MUTE      = "voicebridge.control.mute"
SUBJECT_UNMUTE    = "voicebridge.control.unmute"
SUBJECT_HEALTH    = "voicebridge.control.health"
SUBJECT_TRANSCRIPT = "voicebridge.transcript"
SUBJECT_ERROR     = "voicebridge.control.error"
SUBJECT_BRIDGE_EXIT = "voicebridge.control.bridge_exit"
SUBJECT_INJECT    = "voicebridge.inject.*"  # 接点B：下行注入 voicebridge.inject.<call_id>

# 多节点：本实例标识与 start 负载均衡的 queue group 名。
# start 经 queue group 分流（一条消息只有一个节点处理）；
# 命中节点在回执中带上 node_id，Go 侧据此把后续控制指令发往节点定向主题。
NODE_ID = resolve_node_id()
QUEUE_GROUP = "voicebridge"


def _node_subject(base: str) -> str:
    """节点定向主题：base.<NODE_ID>（如 voicebridge.control.mute.<node>）。"""
    return f"{base}.{NODE_ID}"


def _ack(ok: bool, error: str = "") -> bytes:
    """控制指令回执。带 node_id 供 Go 侧建立 call→节点 归属。"""
    payload: dict = {"ok": ok, "node_id": NODE_ID}
    if error:
        payload["error"] = error
    return json.dumps(payload).encode()

# start 确认等待：略小于 Go 侧 NATS request 超时（15s），超时主动清理半启动 bridge
# 并回 ok:false，避免 Go 回滚后 Python 后台继续起出"幽灵 AI"。
_SESSION_START_TIMEOUT = 12.0

# 活跃 session 表：call_id → { session, started_at, room, provider }
_sessions: dict[int, dict] = {}


def _set_ai_muted(entry: dict, muted: bool) -> None:
    """切换一个活跃通话的 AI 静音态（四档：接管=True，其余=False）。

    muted=True ：取消当前回复 + 停止把访客音频喂给模型 + 停止 AI 发声；
                 不 aclose——session/ws/dialog 上下文全部保留，API 不断。
    muted=False：恢复喂音频 + 恢复发声，AI 接着之前的上下文继续。
    realtime 模型听到即答，必须连 input 一起关，否则它仍会生成回复（文字落库污染记录）。
    """
    session = entry.get("session")
    if session is None:
        return
    if muted:
        try:
            session.interrupt()  # 打断 AI 当前正在播放的回复
        except Exception:
            pass
    session.input.set_audio_enabled(not muted)
    session.output.set_audio_enabled(not muted)
    entry["muted"] = muted


# 静音保活帧节奏：按实时速率喂 100ms 静音帧（gzip 后体积可忽略）。
_KEEPALIVE_FRAME_MS = 100


def _start_mute_keepalive(call_id: int, entry: dict) -> None:
    """接管静音期间向豆包持续喂静音帧。

    豆包服务端 recv_timeout（当前 60s）内收不到音频会判空闲超时杀掉 dialog，
    且插件无重连逻辑——接管超过 60s 后交回，AI 将永久沉默。
    静音帧走插件自身发送队列（push_audio → Chan → _send_task 单写者），
    不存在并发写 ws 的问题。仅 doubao 需要；session 死亡或交回时自动退出。
    """
    if entry.get("provider") != "doubao_realtime":
        return
    rt = _inject_sessions.get(call_id)
    if rt is None:
        logger.warning(f"[Keepalive] no realtime session call_id={call_id}, skip")
        return

    async def _loop():
        sr = 24000
        try:
            sr = int(rt._realtime_model._opts.sample_rate)
        except Exception:
            pass
        samples = sr * _KEEPALIVE_FRAME_MS // 1000
        silence = b"\x00\x00" * samples
        sent = 0
        while entry.get("muted"):
            # 通话结束 / session 重建后 _inject_sessions 中已不是当前对象，停止保活
            if _inject_sessions.get(call_id) is not rt:
                logger.info(f"[Keepalive] session gone call_id={call_id}, stop after {sent} frames")
                return
            try:
                rt.push_audio(rtc.AudioFrame(
                    data=silence, sample_rate=sr,
                    num_channels=1, samples_per_channel=samples,
                ))
                sent += 1
            except Exception as e:
                logger.warning(f"[Keepalive] push failed call_id={call_id}: {e}")
                return
            await asyncio.sleep(_KEEPALIVE_FRAME_MS / 1000)
        logger.info(f"[Keepalive] unmuted call_id={call_id}, stop after {sent} frames")

    _stop_mute_keepalive(entry)
    entry["keepalive_task"] = asyncio.create_task(_loop())
    logger.info(f"[Keepalive] started call_id={call_id}")


def _stop_mute_keepalive(entry: dict) -> None:
    task = entry.pop("keepalive_task", None)
    if task is not None:
        task.cancel()

# 注入路由表（接点B）：call_id → RealtimeSession（仅 doubao，_run_ws 期间注册）
_inject_sessions: dict[int, object] = {}
# 注入去重：call_id → 最近一次已注入的 (round_seq, seq) 复合序（丢弃过期注入）
_inject_last_seq: dict[int, tuple[int, int]] = {}


def _env_float(name: str, default: float) -> float:
    try:
        return float(os.environ.get(name, "") or default)
    except (TypeError, ValueError):
        return default


# 三方通话主人插话音频接入（方案B）参数，可用环境变量覆盖。
_OWNER_VAD_THRESHOLD = _env_float("GRIX_OWNER_VAD_THRESHOLD", 400.0)   # 16-bit PCM RMS 说话阈值
_OWNER_RELEASE_SEC = _env_float("GRIX_OWNER_RELEASE_SEC", 1.2)         # 说完判定的释放尾窗(秒)
_OWNER_TRANSCRIPT_HOLD = _env_float("GRIX_OWNER_TRANSCRIPT_HOLD", 0.8)  # 轮次结束后转写抑制额外保持(秒)

# 主人插话期间转写抑制开关（provider 中性）：call_id → [bool]。
# 由 _run_owner_audio_bridge 维护；doubao(451 raw WS) 与 openai(user_input_transcribed)
# 两条转写发布路径都查它，主人说话期间不落 IM（守方案C 红线）。
_owner_speaking: dict[int, list] = {}


def _owner_suppressed(call_id) -> bool:
    return bool(_owner_speaking.get(call_id, [False])[0])


def _realtime_push_target(call_id, session, provider):
    """取可 push_audio 的实时 session（provider 中性）。

    doubao：_run_ws 期间注册进 _inject_sessions 的 volcengine RealtimeSession；
    openai：AgentActivity.realtime_llm_session。
    """
    if provider == "doubao_realtime":
        return _inject_sessions.get(call_id)
    activity = getattr(session, "_activity", None)
    return getattr(activity, "realtime_llm_session", None) if activity else None


async def _run_owner_audio_bridge(call_id, owner_id, room, session, realtime_model, provider) -> None:
    """三方通话：把主人(被叫)的插话音频接入 AI，并在其说话期间抑制转写落 IM（方案B）。

    provider 中性：doubao / openai 同一套逻辑，仅"往哪 push、查哪条转写路径"不同。
    轮流让位（避免双路 push_audio 帧率翻倍杂音）：
      - 主人开口 → 停客户音频(input.set_audio_enabled False) + 改喂主人音频；
      - 主人停口 → 恢复客户音频；owner_speaking 延后清除，覆盖句尾 ASR 收尾延迟。
    接管(entry.muted)期间不介入：AI 已静音、由 takeover 独占 input 开关。
    主人可能后进房或中途离开，故外层循环等待其音频轨。
    """
    owner_identity = f"user-{owner_id}"
    try:
        model_sr = int(realtime_model._opts.sample_rate)
    except Exception:
        model_sr = 24000

    flag = _owner_speaking.setdefault(call_id, [False])
    detector = OwnerTurnDetector(threshold=_OWNER_VAD_THRESHOLD, release_sec=_OWNER_RELEASE_SEC)
    state = {"turn": False, "release": None}

    def _is_muted() -> bool:
        e = _sessions.get(call_id)
        return bool(e and e.get("muted"))

    def _begin() -> None:
        state["turn"] = True
        rel = state.get("release")
        if rel is not None:
            rel.cancel()
            state["release"] = None
        flag[0] = True
        if not _is_muted():
            try:
                session.input.set_audio_enabled(False)
            except Exception:
                pass
        logger.info(f"[OwnerAudio] owner turn begin call_id={call_id}")

    async def _release_after_hold() -> None:
        try:
            await asyncio.sleep(_OWNER_TRANSCRIPT_HOLD)
            flag[0] = False
            logger.info(f"[OwnerAudio] owner_speaking cleared call_id={call_id}")
        except asyncio.CancelledError:
            pass

    def _end() -> None:
        state["turn"] = False
        if not _is_muted():
            try:
                session.input.set_audio_enabled(True)
            except Exception:
                pass
        try:
            state["release"] = asyncio.create_task(_release_after_hold())
        except Exception:
            flag[0] = False
        logger.info(f"[OwnerAudio] owner turn end call_id={call_id}")

    async def _consume(track) -> None:
        stream = rtc.AudioStream(track, sample_rate=model_sr, num_channels=1)
        try:
            async for ev in stream:
                if _is_muted():
                    if state["turn"]:
                        _end()
                    continue
                frame = ev.frame
                rms = frame_rms(bytes(frame.data))
                active = detector.feed(rms, time.monotonic())
                if active and not state["turn"]:
                    _begin()
                if active:
                    rt = _realtime_push_target(call_id, session, provider)
                    if rt is not None:
                        try:
                            rt.push_audio(frame)
                        except Exception as e:
                            logger.warning(f"[OwnerAudio] push failed call_id={call_id}: {e}")
                if state["turn"] and not active:
                    _end()
        finally:
            try:
                await stream.aclose()
            except Exception:
                pass
            if state["turn"]:
                _end()

    logger.info(f"[OwnerAudio] waiting owner track call_id={call_id} identity={owner_identity} model_sr={model_sr}")
    try:
        while True:
            loop = asyncio.get_running_loop()
            track_fut = loop.create_future()

            def _on_sub(track, pub, participant, _fut=track_fut):
                if (participant.identity == owner_identity
                        and getattr(track, "kind", None) == rtc.TrackKind.KIND_AUDIO
                        and not _fut.done()):
                    _fut.set_result(track)

            room.on("track_subscribed", _on_sub)
            try:
                # 已在房：直接取既有音频轨
                for p in list(room.remote_participants.values()):
                    if p.identity != owner_identity:
                        continue
                    for pub in list(p.track_publications.values()):
                        if (pub.track is not None
                                and getattr(pub, "kind", None) == rtc.TrackKind.KIND_AUDIO
                                and not track_fut.done()):
                            track_fut.set_result(pub.track)
                track = await track_fut
            finally:
                room.off("track_subscribed", _on_sub)
            logger.info(f"[OwnerAudio] owner track acquired call_id={call_id}")
            await _consume(track)
            logger.info(f"[OwnerAudio] owner track ended call_id={call_id}, awaiting re-publish")
    except asyncio.CancelledError:
        if not _is_muted():
            try:
                session.input.set_audio_enabled(True)
            except Exception:
                pass
        raise
    finally:
        _owner_speaking.pop(call_id, None)

# openai 语序编排器路由（接点B）：call_id → OpenAIVoiceOrchestrator
# 与 _inject_sessions 互斥（一通只属于一个 provider）。
_openai_orchestrators: dict[int, object] = {}


def _attach_openai_orchestrator(call_id: int, session, cfg: dict) -> None:
    """为一通 openai 通话挂载语序编排器，并接 AgentSession 的用户说话状态事件。

    rt_session 经 AgentActivity.realtime_llm_session 获取（session.start 后才就绪）。
    抢答时长 GRIX_VOICE_RACE_MS（默认 800ms，文档 31 §8 决策4）。
    """
    activity = getattr(session, "_activity", None)
    rt_session = getattr(activity, "realtime_llm_session", None) if activity else None
    if rt_session is None:
        logger.error(f"[GPT-Seq] no realtime session call_id={call_id}, orchestrator disabled")
        return

    async def _append_system(text: str, _rt=rt_session) -> None:
        # 安全追加：从当前远端 ctx 复制后 add_message，update_chat_ctx 做 diff
        # 只会新增这一条 system 条目，不会误删既有对话项。
        ctx = _rt.chat_ctx
        ctx.add_message(role="system", content=text)
        await _rt.update_chat_ctx(ctx)

    async def _inject_authoritative(text: str) -> None:
        # 大脑答案打权威资料标记（与 prompt 守则第 2 条呼应，GPT 据此采信）。
        await _append_system(tag_authoritative(text))

    def _trigger_reply(_s=session) -> None:
        _s.generate_reply()

    try:
        race_ms = int(os.environ.get("GRIX_VOICE_RACE_MS", "800") or 800)
    except (TypeError, ValueError):
        race_ms = 800

    orch = OpenAIVoiceOrchestrator(
        call_id, _trigger_reply, _inject_authoritative, asyncio.get_running_loop(), race_ms,
    )
    _openai_orchestrators[call_id] = orch
    # 存到 entry 供 on_mute/on_unmute 用（接管/交回标记注入、静音切换）
    entry = _sessions.get(call_id)
    if entry is not None:
        entry["orchestrator"] = orch
        entry["inject_note"] = _append_system  # 未打权威标记的纯系统提示

    def _on_user_state(ev, _o=orch) -> None:
        ns = getattr(ev, "new_state", None)
        if ns == "speaking":
            _o.notify_user_started()
        elif ns == "listening":
            _o.notify_user_stopped()

    session.on("user_state_changed", _on_user_state)
    logger.info(f"[GPT-Seq] orchestrator attached call_id={call_id} race_ms={race_ms}")


# 接管/交回时给 GPT 注入的状态提示（未打权威标记的系统消息）。
_TAKEOVER_NOTE = (
    "【系统】人工客服现在接管了与用户的对话，接下来由人工和用户交流。"
    "你只负责安静聆听，不要说话、不要生成回复。"
    "请记住接下来用户说的内容，稍后人工把对话交回给你时，你要能自然接续。"
)
_HANDBACK_NOTE = (
    "【系统】人工客服已把对话交回给你，现在恢复由你与用户交流。"
    "请参考接管期间用户说过的内容，自然地接续服务。"
)


async def _openai_set_takeover(entry: dict, call_id: int, muted: bool) -> None:
    """openai 接管/交回：只听不说 ↔ 恢复发声。

    接管(muted=True)：停 generate_reply（编排器静音）+ 打断在途回复 + 关 output；
                     **input 保持开**——GPT 全程继续听访客、上下文持续积累（这是
                     豆包补不了的关键体验）。注意：仅听访客，不混入 owner 音频——
                     混入会导致 owner 语音被 GPT 转写并经接点A 落 IM，违反方案C。
    交回(muted=False)：恢复 output + 编排器解除静音；注入交回提示让其自然接续。
    """
    session = entry.get("session")
    orch = entry.get("orchestrator")
    note = entry.get("inject_note")
    if session is None:
        return
    if orch is not None:
        orch.set_muted(muted)
    if muted:
        try:
            session.interrupt()  # 打断 AI 当前正在播放的回复
        except Exception:
            pass
    try:
        # 只切 output；input 始终保持开，保证接管期间持续聆听访客
        session.output.set_audio_enabled(not muted)
    except Exception as e:
        logger.warning(f"[GPT-Seq] set output enabled failed call_id={call_id}: {e}")
    entry["muted"] = muted
    if note is not None:
        try:
            await note(_TAKEOVER_NOTE if muted else _HANDBACK_NOTE)
        except Exception as e:
            logger.warning(f"[GPT-Seq] takeover note inject failed call_id={call_id}: {e}")


def _detach_openai_orchestrator(call_id: int) -> None:
    orch = _openai_orchestrators.pop(call_id, None)
    if orch is not None:
        try:
            orch.aclose()
        except Exception:
            pass

# handover 中转：on_interrupt 保存旧 room，start_bridge 时断开避免 DuplicateIdentity
_handover_rooms: dict[int, rtc.Room] = {}

# start 超时中止标记：start_bridge 等待超时后登记，_run_and_signal 据此补触发
# disconnect 清理（监听注册晚于断房时事件会漏）。finally 中清除。
_aborted_calls: set[int] = set()


def _resolve_livekit_url() -> str:
    """优先使用集群内直连地址，减少外部网络跳数。"""
    internal = os.environ.get("LIVEKIT_INTERNAL_URL", "").strip()
    if internal:
        return internal
    return os.environ["LIVEKIT_URL"]


def _publish_transcript(nc: nats.aio.client.Client, call_id: int, segment_seq: int,
                        role: str, text: str, provider: str) -> None:
    """发布一条 transcript 到 NATS，Go 侧消费后写入 IM 消息。"""
    if not text or not text.strip():
        return
    payload = {
        "call_id": str(call_id),
        "segment_seq": segment_seq,
        "speaker_role": role,
        "transcript_raw": text.strip(),
        "provider": provider,
        "started_at_ms": int(time.time() * 1000),
    }
    subject = f"{SUBJECT_TRANSCRIPT}.{call_id}"

    async def _publish():
        try:
            await nc.publish(subject, json.dumps(payload).encode())
            logger.info(f"Transcript call_id={call_id} seq={segment_seq} role={role} len={len(text)}")
        except Exception as e:
            logger.warning(f"Failed to publish transcript call_id={call_id}: {e}")

    asyncio.create_task(_publish())


def _publish_bridge_error(nc: nats.aio.client.Client, call_id: int,
                          error_type: str, error_msg: str, provider: str) -> None:
    """发布 bridge 运行错误到 NATS，Go 端可订阅用于监控/自动挂断。"""
    payload = {
        "call_id": str(call_id),
        "error_type": error_type,
        "error_msg": error_msg,
        "provider": provider,
        "ts": int(time.time() * 1000),
    }
    subject = f"{SUBJECT_ERROR}.{call_id}"

    async def _publish():
        try:
            await nc.publish(subject, json.dumps(payload).encode())
            logger.warning(f"Bridge error published call_id={call_id} type={error_type}")
        except Exception as e:
            logger.warning(f"Failed to publish bridge error call_id={call_id}: {e}")

    asyncio.create_task(_publish())


async def start_bridge(msg: nats.aio.client.Msg, nc: nats.aio.client.Client) -> None:
    data = json.loads(msg.data)
    call_id: int = data.get("call_id", 0)

    try:
        cfg = parse_start_message(data)
    except BridgeConfigError as e:
        logger.error(f"Invalid start config call_id={call_id}: {e}")
        if msg.reply:
            await nc.publish(msg.reply, _ack(False, str(e)))
        return

    system_prompt = cfg["system_prompt"]
    room_name = f"call-{call_id}"

    if call_id in _sessions:
        logger.warning(f"Session already exists call_id={call_id}, ignoring")
        if msg.reply:
            await nc.publish(msg.reply, _ack(True))
        return

    # handover 场景：断开旧 LiveKit Room，避免 DuplicateIdentity
    # 不 pop，_run_and_signal 的 finally 块需要检查此 dict 来抑制 BRIDGE_EXIT
    old_room = _handover_rooms.get(call_id)
    if old_room:
        logger.info(f"Handover: disconnecting old room for call_id={call_id}")
        try:
            await old_room.disconnect()
        except Exception:
            pass
        # 等待旧 bridge 的 _run_and_signal 协程清理完毕
        await asyncio.sleep(0.3)

    logger.info(f"Starting bridge call_id={call_id} room={room_name} provider={cfg['provider']} model={cfg['model']}")

    try:
        from livekit.api import AccessToken, VideoGrants
        token = (
            AccessToken(
                api_key=os.environ["LIVEKIT_API_KEY"],
                api_secret=os.environ["LIVEKIT_API_SECRET"],
            )
            .with_identity(f"ai_bot_{call_id}")
            .with_grants(VideoGrants(
                room_join=True,
                room=room_name,
                can_publish=True,
                can_subscribe=True,
                can_publish_data=True,
                can_update_own_metadata=True,
            ))
            .to_jwt()
        )

        livekit_url = _resolve_livekit_url()
        logger.info(f"Connecting to LiveKit call_id={call_id} url={livekit_url}")

        room = rtc.Room()
        await room.connect(livekit_url, token)

        # 语音大脑(relay_mode)：走 STT+TTS 管线——STT/TTS 只当耳朵+嘴，文字 agent 是唯一大脑，
        # 打断/轮次走框架原生(turn_detection="stt")，不再需要静音/垫场/强制打断那套 hack。
        # 豆包、OpenAI 两家后端共用此管线(STT/TTS 厂商由 provider 决定)。
        # 其余场景(客服/普通语音)仍走端到端对话模型。
        pipeline_mode = bool(cfg.get("relay_mode")) and cfg['provider'] in ('doubao_realtime', 'openai_realtime')
        realtime_model = None
        if pipeline_mode:
            stt_engine, tts_engine, vad_engine = stt_tts_pipeline.build_pipeline_components(cfg)
            stt_tts_pipeline.register_call(call_id)
            session_kwargs = dict(
                stt=stt_engine,
                llm=stt_tts_pipeline.TextAgentBridgeLLM(call_id),
                tts=tts_engine,
            )
            if vad_engine is not None:
                # OpenAI：非流式 STT 无判停，silero VAD 切句+原生打断，轮次走 VAD。
                session_kwargs["vad"] = vad_engine
                session_kwargs["turn_detection"] = "vad"
            else:
                # 豆包：流式 STT 自带判停，轮次走 STT；无 VAD 时用词数门槛抑制单字误打断。
                session_kwargs["turn_detection"] = "stt"
                session_kwargs["min_interruption_words"] = 1
            session = AgentSession(**session_kwargs)
            logger.info(f"STT+TTS pipeline enabled call_id={call_id} provider={cfg['provider']} vad={vad_engine is not None} session_id={cfg.get('session_id','')!r}")
        else:
            realtime_model = build_realtime_model(lk_openai, cfg, lk_volcengine)

            # 为 volcengine 的 WebSocket 拦截器注入回灌上下文
            # （仅在 volcengine 插件可用时生效，OpenAI 走 AgentSession 事件）
            if lk_volcengine is not None and cfg['provider'] == 'doubao_realtime':
                realtime_model._grix_ctx = {
                    'nc': nc,
                    'call_id': call_id,
                    'session_id': cfg.get('session_id', ''),  # 跨通话 dialog 连续性
                    'provider': cfg['provider'],
                    'seq': [0],
                    'ai_responding': [False],  # AI 回复期间跳过 451 ASR 发布
                    'user_querying': [False],  # 用户正在说话（450→True, 459→False）；on_inject 守卫用
                    'relay_mode': bool(cfg.get('relay_mode', False)),  # 传声筒：注入走事件300逐字念回
                    'relay_forwarding': [False],  # 传声筒：True=正在转发注入念稿音频；False=静音豆包自答
                }
                logger.info(f"Transcript injection enabled call_id={call_id} session_id={cfg.get('session_id', '')!r} relay_mode={bool(cfg.get('relay_mode', False))}")

            session = AgentSession(llm=realtime_model)
            # 传声筒打断用：把 AgentSession 暴露给豆包接收循环。barge-in 时用 session.interrupt(force=True)
            # 强制打断，绕过框架默认 min_interruption_duration=0.5s 阈值、立即清空已排队音频，做到秒静。
            if getattr(realtime_model, '_grix_ctx', None) is not None:
                realtime_model._grix_ctx['agent_session'] = session
        # openai：用混合快答守则前缀包住 agent 人设（稳定前缀吃 prompt caching）。
        # doubao 维持原样（其大脑环走 502 raw WS，不经本前缀）。
        if cfg["provider"] == "openai_realtime":
            instructions = build_openai_instructions(system_prompt, cfg.get("language", ""))
        else:
            instructions = system_prompt
        # 豆包端到端对话：system_role 含换行会被火山服务端整会话卡死（2026-07-03 起，
        # 见 bridge_config.sanitize_doubao_system_role），出口处单行化。管线模式的
        # instructions 只进文字大脑、不进豆包 dialog，保持原样。
        if cfg["provider"] == "doubao_realtime" and not pipeline_mode:
            instructions = sanitize_doubao_system_role(instructions)
        agent = Agent(instructions=instructions)

        started_at = time.monotonic()
        _sessions[call_id] = {
            "session": session, "started_at": started_at,
            "room": room, "provider": cfg["provider"],
        }
        # 超时清理用独立引用：_run_and_signal 的 finally 会 del nonlocal room
        room_ref = room

        start_done = asyncio.Event()
        start_error: list[Exception | None] = [None]

        async def _run_and_signal():
            nonlocal realtime_model, session, room
            # segment_seq 用于 NATS transcript 消息排序
            segment_seq = [0]

            def _next_seq() -> int:
                segment_seq[0] += 1
                return segment_seq[0]

            # doubao raw WS ASR 和 AgentSession AI 文本共用同一个序号源，
            # 避免同一通话里 caller/ai_bot 分别从 seq=1 开始。
            if cfg["provider"] == "doubao_realtime":
                ctx = getattr(realtime_model, "_grix_ctx", None)
                if ctx is not None:
                    ctx["seq"] = segment_seq

            # 用户语音转文字（最终结果）
            def on_user_transcribed(ev):
                # 诊断：管线模式打印每条 STT 转写(含 interim)，确认 STT 是否真在出字。
                if pipeline_mode:
                    logger.info(f"[Pipeline][STT] call_id={call_id} is_final={getattr(ev,'is_final',None)} text={(getattr(ev,'transcript','') or '')[:120]!r}")
                # 端到端 doubao_realtime 的用户 ASR 由 raw WS 事件 451 回灌，AgentSession
                # 也会触发同一文本，不能再发布一次。但 STT+TTS 管线没有 raw WS——这里的
                # user_input_transcribed 就是用户转写的唯一来源，必须发布(发布即触发文字 agent)。
                if cfg["provider"] == "doubao_realtime" and not pipeline_mode:
                    return
                if not ev.is_final:
                    return
                # 方案B：主人插话期间这段转写来自主人，AI 要听见回应但不落 IM。
                if _owner_suppressed(call_id):
                    logger.info(f"[OwnerAudio] transcript suppressed (owner) call_id={call_id} text={ev.transcript[:80]}")
                    return
                _publish_transcript(nc, call_id, _next_seq(), "caller",
                                    ev.transcript, cfg["provider"])

            # AI 回复文字（每条完整回复）
            def on_conversation_item(ev):
                item = ev.item
                text = getattr(item, "text_content", None)
                if not text:
                    # item.content 可能是 list，尝试提取文字
                    content = getattr(item, "content", None)
                    if content:
                        parts = [c for c in content if isinstance(c, str)]
                        text = "\n".join(parts) if parts else None
                if not text:
                    return
                role = "ai_bot" if getattr(item, "role", "") == "assistant" else "caller"
                # 人声文本去重：caller 文本在每种模式都有专门来源——
                #   pipeline = on_user_transcribed(STT) / doubao = raw WS 451 ASR /
                #   openai 端到端 = on_user_transcribed(user_input_transcribed)。
                # conversation_item 里的 caller 永远是上述来源的重复，一律丢弃；ai_bot 文本照常发布。
                if role != "ai_bot":
                    return
                # （caller 的主人插话抑制在 on_user_transcribed 内处理；此处只剩 ai_bot，照常发布。）
                _publish_transcript(nc, call_id, _next_seq(), role, text, cfg["provider"])
                # 标记 AI 正在回复，阻止 raw WS 451 路径重复发布同一文本
                if role == "ai_bot" and cfg["provider"] == "doubao_realtime":
                    ctx = getattr(realtime_model, "_grix_ctx", None)
                    if ctx is not None:
                        ctx["ai_responding"][0] = True

            session.on("user_input_transcribed", on_user_transcribed)
            session.on("conversation_item_added", on_conversation_item)

            # 诊断：管线模式监听 AgentSession 错误事件，暴露 STT/TTS/LLM 层的隐藏错误。
            if pipeline_mode:
                def on_pipeline_error(ev):
                    logger.warning(f"[Pipeline][error] call_id={call_id} ev={ev!r}")
                try:
                    session.on("error", on_pipeline_error)
                except Exception:
                    pass

            # doubao_realtime 回灌：raw WS 拦截提取事件 451 的用户转写。管线无 raw WS，跳过。
            if cfg['provider'] == 'doubao_realtime' and lk_volcengine is not None and not pipeline_mode:
                logger.info(f"Transcript: doubao raw-ASR path enabled call_id={call_id}")

            owner_audio_task = None
            try:
                http_context._new_session_ctx()
                # 使用 caller_id 指定监听哪个 participant 的音频，
                # 确保 handover 回 AI 后不会误听成 callee。
                caller_id = cfg.get("caller_id", 0)
                if caller_id:
                    room_input_opts = RoomInputOptions(
                        close_on_disconnect=False,
                        participant_identity=f"user-{caller_id}",
                    )
                    logger.info(f"[VolcDiag] participant_identity set to user-{caller_id} call_id={call_id}")
                else:
                    room_input_opts = RoomInputOptions(close_on_disconnect=False)


                await session.start(agent=agent, room=room, room_input_options=room_input_opts)
                start_done.set()
                logger.info(f"Session started call_id={call_id}")

                # openai 语序编排（接点B + 抢答）：create_response=False 下由编排器
                # 控制何时 generate_reply。doubao 走 raw WS 502 路径，不在此处。
                # 管线模式(语音大脑)的大脑是文字 agent、由 LLM 桥驱动，不走端到端编排器。
                if cfg["provider"] == "openai_realtime" and not pipeline_mode:
                    _attach_openai_orchestrator(call_id, session, cfg)
                    # 开场白：doubao 走插件原生 opening(SayHello)，openai realtime 没有
                    # 等价机制，这里用一次带指令的 generate_reply 主动播报，用与 opening
                    # 一致的语言、逐字念出，不额外发挥。
                    opening = (cfg.get("opening") or "").strip()
                    if opening:
                        session.generate_reply(
                            instructions=f"请用与下面这句话相同的语言，一字不差地说出这句开场白，不要补充其他内容：{opening}"
                        )

                # 三方通话：主人(被叫)插话音频接入（doubao + openai 同一套；其语音不落 IM，方案B）。
                # 管线模式不适用(语音大脑为 owner↔文字 agent、无第三方；且 realtime_model 为 None)。
                _owner_id = int(cfg.get("owner_id") or 0)
                if not pipeline_mode and cfg["provider"] in ("doubao_realtime", "openai_realtime") and _owner_id > 0:
                    owner_audio_task = asyncio.create_task(
                        _run_owner_audio_bridge(call_id, _owner_id, room, session, realtime_model, cfg["provider"])
                    )
                    logger.info(f"[OwnerAudio] bridge task started call_id={call_id} owner_id={_owner_id} provider={cfg['provider']}")

                disconnect_event = asyncio.Event()
                room.on("disconnected", lambda *_: disconnect_event.set())
                # start 超时清理路径可能在本监听注册前就断了 room：补查中止标记防漏事件
                if call_id in _aborted_calls:
                    disconnect_event.set()
                # P0 护栏：bridge 最大存活时间。
                # volcengine recv task 可能因协议解析错误崩溃，
                # 导致 session 卡死不退出、room 不断开。
                # 超时后主动断开 room，触发 finally 清理。
                # agent 配了单通话上限时随之对齐（+120s 缓冲，Go 侧定时器先触发正常挂断），
                # 未配置时 30 分钟兜底。
                _max_secs = int(cfg.get("max_call_seconds") or 0)
                _BRIDGE_MAX_LIFETIME = _max_secs + 120 if _max_secs > 0 else 30 * 60
                try:
                    await asyncio.wait_for(
                        disconnect_event.wait(),
                        timeout=_BRIDGE_MAX_LIFETIME,
                    )
                except asyncio.TimeoutError:
                    logger.warning(f"Bridge max lifetime reached call_id={call_id}, forcing disconnect")
                    try:
                        await room.disconnect()
                    except Exception:
                        pass
                except (BlockingIOError, OSError):
                    # Self-pipe overflow under heavy concurrent I/O.
                    # The room will be cleaned up in the finally block.
                    logger.warning(f"Bridge I/O pressure call_id={call_id}, cleaning up")

                # Room 断开后，主动关闭 session。
                # close_on_disconnect=False 导致 volcengine WebSocket 不会自动关闭，
                # 必须显式 aclose()，否则 _run_ws 任务持续运行，阻塞事件循环，
                # 后续 NATS 回调（on_stop / on_health）全部无法处理，进程卡死。
                logger.info(f"Room disconnected, closing session call_id={call_id}")
                _CLOSE_TIMEOUT = 5.0
                try:
                    await asyncio.wait_for(session.aclose(), timeout=_CLOSE_TIMEOUT)
                    logger.info(f"Session closed ok call_id={call_id}")
                except asyncio.TimeoutError:
                    logger.warning(f"Session aclose timeout ({_CLOSE_TIMEOUT}s) call_id={call_id}")
                except Exception as e:
                    logger.warning(f"Session aclose error call_id={call_id}: {e}")
            except Exception as e:
                start_error[0] = e
                start_done.set()
                error_type = type(e).__name__
                logger.error(f"Session start failed call_id={call_id}: {e}\n{traceback.format_exc()}")
                _publish_bridge_error(nc, call_id, error_type, str(e), cfg['provider'])
            except (BlockingIOError, OSError) as e:
                # Guard against self-pipe overflow crashes
                start_done.set()
                logger.error(f"Bridge I/O error call_id={call_id}: {e}")
            finally:
                duration = time.monotonic() - started_at
                _aborted_calls.discard(call_id)
                if owner_audio_task is not None:
                    owner_audio_task.cancel()
                _detach_openai_orchestrator(call_id)
                current = _sessions.get(call_id)
                if current is not None and current.get("session") is session:
                    _sessions.pop(call_id, None)
                try:
                    await asyncio.wait_for(http_context._close_http_ctx(), timeout=3.0)
                except Exception:
                    pass
                try:
                    await asyncio.wait_for(room.disconnect(), timeout=3.0)
                except asyncio.TimeoutError:
                    logger.warning(f"Room disconnect timeout call_id={call_id}")
                except Exception:
                    pass
                # 管线模式：清理该通话的回复队列（幂等，非管线通话无副作用）。
                stt_tts_pipeline.unregister_call(call_id)
                # 注入去重表兜底清理：豆包端到端在 _run_ws finally 已清，
                # 管线/openai 通话不走该路径，不清会随通话数缓慢累积。
                _inject_last_seq.pop(call_id, None)
                # Release heavy references promptly so Python GC can reclaim
                # memory before the next bridge session allocates.
                del session
                del room
                if hasattr(realtime_model, '_grix_ctx'):
                    realtime_model._grix_ctx = None
                del realtime_model
                import gc as _gc; _gc.collect()
                # handover 场景下抑制 BRIDGE_EXIT，避免后端 endCall 挂断访客
                if call_id in _handover_rooms:
                    _handover_rooms.pop(call_id, None)
                    logger.info(f"Bridge ended (handover suppressed BRIDGE_EXIT) call_id={call_id} duration={duration:.1f}s segments={segment_seq[0]}")
                else:
                    # 通知 Go 端 bridge 已退出，触发 busy key 清理
                    try:
                        exit_payload = json.dumps({"call_id": str(call_id), "duration_sec": int(duration)}).encode()
                        await nc.publish(SUBJECT_BRIDGE_EXIT, exit_payload)
                    except Exception as _e:
                        logger.warning(f"Failed to publish bridge_exit call_id={call_id}: {_e}")
                    logger.info(f"Bridge ended call_id={call_id} duration={duration:.1f}s segments={segment_seq[0]}")

        asyncio.create_task(_run_and_signal())

        try:
            await asyncio.wait_for(start_done.wait(), timeout=_SESSION_START_TIMEOUT)
        except asyncio.TimeoutError:
            # 超时即放弃这次启动并主动清理，避免 Go 回滚后 Python 后台
            # 继续把 session 起起来变成"幽灵 AI"（进房接待但后端无此通话）。
            # 借用 handover 标记抑制 BRIDGE_EXIT（Go 已按启动失败处理，无需再触发 endCall）。
            logger.warning(f"Session start timed out call_id={call_id}, aborting and cleaning up")
            _sessions.pop(call_id, None)
            _aborted_calls.add(call_id)
            _handover_rooms[call_id] = room_ref
            try:
                await room_ref.disconnect()  # 触发 _run_and_signal 的 finally 清理
            except Exception:
                pass
            if msg.reply:
                await nc.publish(msg.reply, _ack(False, "session start timeout"))
            return

        if start_error[0] is not None:
            _sessions.pop(call_id, None)
            err_msg = str(start_error[0])
            logger.error(f"Bridge start failed call_id={call_id}: {err_msg}")
            if msg.reply:
                await nc.publish(msg.reply, _ack(False, err_msg))
            return

        if msg.reply:
            await nc.publish(msg.reply, _ack(True))
        logger.info(f"Bridge confirmed call_id={call_id} node={NODE_ID}")

    except Exception as e:
        logger.error(f"Failed to start bridge call_id={call_id}: {e}\n{traceback.format_exc()}")
        _sessions.pop(call_id, None)
        if msg.reply:
            await nc.publish(msg.reply, _ack(False, str(e)))


async def on_inject(msg):
    """接点B：文字大脑回复 → 注入豆包当前 session（502）。subject: voicebridge.inject.<call_id>"""
    try:
        call_id = int(msg.subject.rsplit(".", 1)[-1])
    except (ValueError, IndexError):
        return
    try:
        data = json.loads(msg.data)
    except Exception:
        return
    text = (data.get("text") or "").strip()
    if not text:
        return
    round_seq = int(data.get("round_seq", 0) or 0)
    # seq：同一轮回复内的片段序号(流式边写边念时按句递增)；eot：本轮最后一段标记。
    # 非流式整段注入不带这两个字段，默认 seq=0/eot=True，行为与原来一致。
    seq = int(data.get("seq", 0) or 0)
    eot = bool(data.get("eot", True))
    # streamed：本轮已按句分片念过(收尾时据此只补尾段，避免整段重复念)；tail：未念尾段。
    # 非流式整段注入不带这些字段，默认 streamed=False/tail=""，行为与原来一致。
    streamed = bool(data.get("streamed", False))
    tail = (data.get("tail") or "").strip()
    # 丢弃过期注入：以 (round_seq, seq) 复合序判定——同一轮的后续句(round 相同、seq 递增)
    # 必须放行；只丢更早的轮或同轮已处理过的序号。
    ordinal = (round_seq, seq)
    if round_seq and _inject_last_seq.get(call_id, (0, 0)) >= ordinal:
        logger.info(f"[Inject] drop stale call_id={call_id} round_seq={round_seq} seq={seq}")
        return

    # 语音大脑 STT+TTS 管线：边写边念——分句(eot=false)逐句入队让 TTS 紧跟文字；
    # 收尾(eot=true)时，已分句念过(streamed)只补未念尾段 tail，否则整段 text 念出。
    if stt_tts_pipeline.is_pipeline_call(call_id):
        if eot:
            spoken = tail if streamed else text
            stt_tts_pipeline.push_reply(call_id, spoken, eot=True)
        else:
            stt_tts_pipeline.push_reply(call_id, text, eot=False)
        _inject_last_seq[call_id] = ordinal
        logger.info(f"[Pipeline] reply queued call_id={call_id} round_seq={round_seq} seq={seq} eot={eot} streamed={streamed} len={len(text)}")
        return

    # —— 端到端通话(豆包502/openai 自答)：不是管线，忽略流式分句，只在收尾整段(eot)时注入 ——
    if not eot:
        return

    # openai：路由到语序编排器（注入权威资料 + 视轮次决定是否立即开口）
    orch = _openai_orchestrators.get(call_id)
    if orch is not None:
        try:
            await orch.on_brain_inject(text, round_seq)
            _inject_last_seq[call_id] = ordinal
        except Exception as e:
            logger.warning(f"[Inject] openai orchestrator failed call_id={call_id}: {e}")
        return

    session = _inject_sessions.get(call_id)
    if session is None:
        logger.info(f"[Inject] skip: no active doubao session call_id={call_id}")
        return
    # is_user_querying 守卫：用户已开始说下一句则丢弃旧轮注入，避免用旧答案回答新问题
    _ctx = getattr(getattr(session, '_realtime_model', None), '_grix_ctx', None)
    if _ctx and _ctx.get('user_querying', [False])[0]:
        logger.info(f"[Inject] skip: user querying call_id={call_id} round_seq={round_seq}")
        return
    # 传声筒模式（语音大脑）：走事件300 SayHello 逐字念回；否则走事件502 参考资料（客服快答）。
    relay = bool(_ctx and _ctx.get('relay_mode'))
    # 传声筒：先开/复用一条播放通道，让豆包念稿的音频帧有处可落、能送进房间（否则注入到达时
    # 豆包自答那轮通道早关、音频被丢）。开启 relay_forwarding 后接收拦截器才放行 352 音频。
    if relay:
        opener = getattr(session, '_grix_open_playback_turn', None)
        if opener is not None:
            try:
                opened = opener(text)
                logger.info(f"[Inject] playback turn {'opened' if opened else 'reused'} call_id={call_id}")
            except Exception as e:
                logger.warning(f"[Inject] open playback turn failed call_id={call_id}: {e}")
    inject_attr = "_grix_inject_tts" if relay else "_grix_inject_rag"
    inject_fn = getattr(session, inject_attr, None)
    if inject_fn is None:
        return
    try:
        if relay and 'relay_forwarding' in _ctx:
            _ctx['relay_forwarding'][0] = True
        if await inject_fn(text):
            _inject_last_seq[call_id] = ordinal
            logger.info(f"[Inject] ok call_id={call_id} round_seq={round_seq} len={len(text)} mode={'tts' if relay else 'rag'}")
        elif relay and 'relay_forwarding' in _ctx:
            _ctx['relay_forwarding'][0] = False  # 注入未发出，复位避免误转发豆包自答
    except Exception as e:
        if relay and 'relay_forwarding' in _ctx:
            _ctx['relay_forwarding'][0] = False
        logger.warning(f"[Inject] failed call_id={call_id}: {e}")


def _owner_only(handler):
    """广播控制主题的多节点守卫：非 owner 节点静默忽略（不应答）。

    正常路径下 Go 侧按归属表走节点定向主题；广播仅作为 Go 无归属信息时的
    回退。多副本同时收到广播时，只有持有该通话 session 的节点应答，
    避免非 owner 抢答（Request 取首个回复）导致指令落空。
    """
    async def _wrapped(msg):
        try:
            call_id = json.loads(msg.data).get("call_id")
        except Exception:
            return
        if call_id not in _sessions:
            logger.info(f"broadcast {msg.subject}: not owner call_id={call_id}, ignoring")
            return
        await handler(msg)
    return _wrapped


async def main() -> None:
    logger.info(
        "voicebridge starting: version=%s commit=%s build_time=%s",
        os.getenv("VOICEBRIDGE_VERSION", "dev"),
        os.getenv("VOICEBRIDGE_COMMIT", "unknown"),
        os.getenv("VOICEBRIDGE_BUILD_TIME", "unknown"),
    )
    nats_url = os.getenv("NATS_URL", "nats://localhost:4222")
    stop_event = asyncio.Event()

    async def on_nats_disconnected():
        logger.warning("Disconnected from NATS")

    async def on_nats_reconnected():
        logger.info(f"Reconnected to NATS: {nats_url}")

    async def on_nats_closed():
        logger.error("NATS connection closed")
        stop_event.set()

    async def on_nats_error(error):
        logger.error(f"NATS client error: {error}")

    nc = await nats.connect(
        nats_url,
        disconnected_cb=on_nats_disconnected,
        reconnected_cb=on_nats_reconnected,
        closed_cb=on_nats_closed,
        error_cb=on_nats_error,
    )
    logger.info(f"Connected to NATS: {nats_url}")

    livekit_url = _resolve_livekit_url()
    logger.info(f"LiveKit URL: {livekit_url}")

    async def on_start(msg):
        await start_bridge(msg, nc)

    async def on_interrupt(msg):
        data = json.loads(msg.data)
        call_id = data.get("call_id")
        # 接管必须让 AI 持续静默。仅调用 session.interrupt() 只能取消当前回复，
        # 后续用户语音仍可能触发新的回答，因此关闭 session 并移出活跃表。
        # hand_back 会再次发送 control.start，建立新的 AI session。
        entry = _sessions.pop(call_id, None)
        if entry:
            _stop_mute_keepalive(entry)
            _detach_openai_orchestrator(call_id)
            try:
                await entry["session"].interrupt()
            except Exception:
                pass
            try:
                await asyncio.wait_for(entry["session"].aclose(), timeout=5.0)
            except asyncio.TimeoutError:
                logger.warning(f"on_interrupt: session aclose timeout call_id={call_id}")
            except Exception:
                pass
            # 保存旧 room 引用供 start_bridge 清理，但不立即断开，
            # 否则会触发 BRIDGE_EXIT 导致后端 endCall 挂断访客。
            old_room = entry.get("room")
            if old_room:
                _handover_rooms[call_id] = old_room
            logger.info(f"Suspended call_id={call_id}")
        if msg.reply:
            await nc.publish(msg.reply, _ack(True))

    async def on_mute(msg):
        """四档『接管』：静音 AI（停止喂访客音频 + 停止 AI 发声），但保留 session。
        与 on_interrupt 不同，不 aclose——连接与 dialog 上下文保持，unmute 可秒恢复，
        满足『接管时 AI 不出声、但连接不断、API 不断』。
        静音期间对豆包喂静音帧保活（防服务端空闲超时杀 dialog）。
        静音动作失败回 ok:false（AI 可能仍在发声，接管不能放行）；
        无活跃 session 视为成功（AI 本就无声，不阻塞人工接管）。"""
        data = json.loads(msg.data)
        call_id = data.get("call_id")
        entry = _sessions.get(call_id)
        ok, err = True, ""
        if entry:
            try:
                if entry.get("provider") == "openai_realtime":
                    # 只听不说：input 保持开，持续听访客并积累上下文
                    await _openai_set_takeover(entry, call_id, True)
                else:
                    # doubao：关 IO + 静音帧保活（防服务端空闲超时）
                    _set_ai_muted(entry, True)
                    _start_mute_keepalive(call_id, entry)
                logger.info(f"AI muted (takeover) call_id={call_id}")
            except Exception as e:
                ok, err = False, str(e)
                logger.warning(f"on_mute failed call_id={call_id}: {e}")
        else:
            logger.warning(f"on_mute: no active session call_id={call_id}")
        if msg.reply:
            await nc.publish(msg.reply, _ack(ok, err))

    async def on_unmute(msg):
        """四档从『接管』切回（加入/旁听/待命）：恢复 AI 听 + 发声。
        如实上报失败（ok:false）：session 不存在 / 恢复动作异常 / 豆包侧
        realtime 会话已死（接管期间被服务端断开）。Go 收到 nack 会走
        Interrupt+Start 重建桥（dialog_id=session_id，上下文连续）。"""
        data = json.loads(msg.data)
        call_id = data.get("call_id")
        entry = _sessions.get(call_id)
        ok, err = True, ""
        if entry is None:
            ok, err = False, "no active session"
            logger.warning(f"on_unmute: no active session call_id={call_id}")
        else:
            try:
                if entry.get("provider") == "openai_realtime":
                    # 恢复发声 + 解除编排器静音 + 注入交回提示（session 全程未断，必成功）
                    await _openai_set_takeover(entry, call_id, False)
                    logger.info(f"AI unmuted call_id={call_id}")
                else:
                    _stop_mute_keepalive(entry)
                    _set_ai_muted(entry, False)
                    if call_id not in _inject_sessions:
                        ok, err = False, "doubao realtime session dead"
                        logger.warning(f"on_unmute: doubao session dead call_id={call_id}")
                    else:
                        logger.info(f"AI unmuted call_id={call_id}")
            except Exception as e:
                ok, err = False, str(e)
                logger.warning(f"on_unmute failed call_id={call_id}: {e}")
        if msg.reply:
            await nc.publish(msg.reply, _ack(ok, err))

    async def on_stop(msg):
        data = json.loads(msg.data)
        call_id = data.get("call_id")
        entry = _sessions.pop(call_id, None)
        if entry:
            _stop_mute_keepalive(entry)
            _detach_openai_orchestrator(call_id)
            try:
                await asyncio.wait_for(entry["session"].aclose(), timeout=5.0)
            except asyncio.TimeoutError:
                logger.warning(f"on_stop: session aclose timeout call_id={call_id}")
            except Exception:
                pass
            logger.info(f"Stopped call_id={call_id}")

    async def on_health(msg):
        if msg.reply:
            await nc.publish(msg.reply, _ack(True))

    # start：queue group 负载均衡，一条 start 只有一个节点处理（多副本不再重复拉起 AI）。
    # 定向 start 供 handover/交回重建使用——旧 room 引用在 owner 节点的
    # _handover_rooms 里，必须由原节点断开，否则新节点进房撞 DuplicateIdentity。
    await nc.subscribe(SUBJECT_START, queue=QUEUE_GROUP, cb=on_start)
    await nc.subscribe(_node_subject(SUBJECT_START), cb=on_start)
    # mute/unmute/interrupt：正常走节点定向主题；广播保留为无归属信息时的回退，
    # 仅 owner 应答（_owner_only），非 owner 静默。
    await nc.subscribe(SUBJECT_INTERRUPT, cb=_owner_only(on_interrupt))
    await nc.subscribe(_node_subject(SUBJECT_INTERRUPT), cb=on_interrupt)
    await nc.subscribe(SUBJECT_MUTE, cb=_owner_only(on_mute))
    await nc.subscribe(_node_subject(SUBJECT_MUTE), cb=on_mute)
    await nc.subscribe(SUBJECT_UNMUTE, cb=_owner_only(on_unmute))
    await nc.subscribe(_node_subject(SUBJECT_UNMUTE), cb=on_unmute)
    # stop 是无应答 publish：广播即可，非 owner 查不到本地 session 自动忽略。
    await nc.subscribe(SUBJECT_STOP, cb=on_stop)
    # health：k8s 探针必须打本节点定向主题——广播会被其他副本代答，掩盖本节点故障。
    # 广播 health 保留用于"至少一个节点存活"的人工排查。
    await nc.subscribe(SUBJECT_HEALTH, cb=on_health)
    await nc.subscribe(_node_subject(SUBJECT_HEALTH), cb=on_health)
    await nc.subscribe(SUBJECT_INJECT, cb=on_inject)

    # 启动自检：确认 volcengine 注入 patch 已生效（§7 patch 失效降级告警）
    if lk_volcengine is not None:
        from livekit.plugins.volcengine.realtime import RealtimeSession as _RS
        if not hasattr(_RS, '_grix_inject_rag'):
            logger.error("[Inject] CRITICAL: volcengine inject patch NOT applied — 502 注入将不可用，请检查插件版本")
        else:
            logger.info("[Inject] volcengine inject patch verified OK")

    logger.info(f"Voice Bridge Worker ready, node_id={NODE_ID}, waiting for control messages...")

    loop = asyncio.get_running_loop()

    def _safe_stop():
        try:
            stop_event.set()
        except Exception:
            # BlockingIOError can occur when self-pipe is full under heavy I/O.
            # Fall back to os._exit to ensure clean shutdown.
            import os
            os._exit(0)

    for sig in (2, 15):  # SIGINT, SIGTERM
        try:
            loop.add_signal_handler(sig, _safe_stop)
        except NotImplementedError:
            pass

    await stop_event.wait()
    logger.info("Shutting down...")

    for entry in list(_sessions.values()):
        await entry["session"].aclose()
    await nc.close()


if __name__ == "__main__":
    asyncio.run(main())

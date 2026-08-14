"""下发配置解析与校验 + 实时模型构造选择。

本模块刻意不依赖 livekit / nats 等第三方包，便于在无依赖环境下单元测试。
真实的 RealtimeModel 由调用方注入的 provider 插件构造（依赖注入，便于 mock）。
"""

import re
import uuid

# 豆包端到端实时对话(realtime/dialogue)鉴权常量。新版火山 API Key 单 Key 鉴权下，
# 用 X-Api-Key 取代旧版 X-Api-App-ID + X-Api-Access-Key；Resource-Id 与固定 App-Key 仍是协议必填。
_DOUBAO_REALTIME_RESOURCE_ID = "volc.speech.dialog"
_DOUBAO_REALTIME_APP_KEY = "PlgvMymc7f3tQnJ6"  # 协议固定值，与鉴权方式无关

# 当前 Python voicebridge 已实现的 voice provider。
SUPPORTED_PROVIDERS = {"openai_realtime", "doubao_realtime"}

DEFAULT_VOICE = "alloy"
DEFAULT_SYSTEM_PROMPT = "You are a helpful voice assistant."

# OpenAI Realtime 上行转写模型（开启后 AgentSession 产出 user_input_transcribed，
# 供接点A 回灌；不开则语音侧无用户转写，文字大脑环断裂）。
OPENAI_TRANSCRIBE_MODEL = "gpt-4o-transcribe"


class BridgeConfigError(Exception):
    """下发配置非法（缺字段 / provider 不支持）。"""


def sanitize_doubao_system_role(text: str) -> str:
    """豆包端到端实时对话的 system_role 单行化。

    火山 realtime/dialogue 服务端（2026-07-03 起实测）对含换行符（\\n / \\r\\n）的
    system_role 会在 StartSession 后永久无响应（无 150/450 等任何事件），整通电话
    静默。与长度、Markdown 语法均无关，唯一触发条件是换行。此处把连续换行及其
    两侧空白折叠为一个"；"，保持语义分隔；单行输入原样返回。
    """
    return re.sub(r"\s*[\r\n]+\s*", "；", str(text or "").strip())


def parse_start_message(data: dict) -> dict:
    """解析 NATS start 消息。api_key / model 必填（无 env 兜底）。"""
    provider = (data.get("voice_provider") or "").strip()
    model = (data.get("model") or "").strip()
    api_key = (data.get("api_key") or "").strip()
    endpoint = (data.get("endpoint") or "").strip()
    voice = (data.get("voice") or "").strip()
    language = (data.get("language") or "").strip()
    opening = (data.get("opening") or "").strip()
    system_prompt = data.get("system_prompt") or DEFAULT_SYSTEM_PROMPT

    if not api_key or not model:
        raise BridgeConfigError("missing api_key or model")
    if provider not in SUPPORTED_PROVIDERS:
        raise BridgeConfigError(f"unsupported voice provider: {provider!r}")

    try:
        max_call_seconds = int(data.get("max_call_seconds") or 0)
    except (TypeError, ValueError):
        max_call_seconds = 0

    return {
        "call_id": data.get("call_id", 0),
        "session_id": (data.get("session_id") or "").strip(),
        "caller_id": data.get("caller_id", 0),
        # 三方通话(AnswerWithAI)：owner_id=被叫主人，>0 时 main 侧接入主人插话音频（其语音不落 IM）。
        "owner_id": data.get("owner_id", 0),
        "provider": provider,
        "model": model,
        "api_key": api_key,
        "endpoint": endpoint,
        "voice": voice,
        "language": language,
        # 语音开场白：主叫接通后主动播报的一句问候，按语言配置（agent.voice_welcome_i18n
        # 按 language 选取后由后端下发）；空串=不主动打招呼。
        "opening": opening,
        "system_prompt": system_prompt,
        "max_call_seconds": max_call_seconds,
        # 语音大脑：relay_mode=True → 走 STT+TTS 管线（豆包只当耳朵+嘴，文字 agent 是大脑）；
        # 缺省 False → 客服/普通语音通话走端到端对话模型，互不影响。
        "relay_mode": bool(data.get("relay_mode", False)),
        # STT+TTS 管线的 TTS 配置（BYOK：app_id/access_token 复用 api_key）。
        # tts_cluster 缺省 volcano_tts。音色优先级：tts_voice > voice(agent 配置音色) > provider 默认，
        # 见 stt_tts_pipeline；当前后端不发 tts_voice/tts_cluster，念稿音色即 agent 配置的 voice。
        "tts_cluster": (data.get("tts_cluster") or "").strip(),
        "tts_voice": (data.get("tts_voice") or "").strip(),
    }


def build_openai_turn_detection(create_response: bool):
    """构造 OpenAI server_vad turn_detection。

    Phase 1（直答模式）：create_response=True，VAD 判定用户说完即自动生成回复。
    Phase 2 起改为 create_response=False，由 voicebridge 在注入文字大脑答案后
    手动 generate_reply，从而控制语序（先注入权威资料再开口）。
    类型来自 openai SDK（lazy import，保持本模块纯解析路径无第三方依赖）。
    """
    from openai.types.realtime.realtime_audio_input_turn_detection import ServerVad
    return ServerVad(
        type="server_vad",
        create_response=create_response,
        interrupt_response=True,   # 用户插话立即打断 AI 当前回复
        threshold=0.5,
        prefix_padding_ms=300,
        silence_duration_ms=500,
    )


def build_openai_transcription():
    """构造 OpenAI 上行转写配置（接点A 依赖）。lazy import 同上。"""
    from openai.types.realtime import AudioTranscription
    return AudioTranscription(model=OPENAI_TRANSCRIBE_MODEL)


def build_realtime_model(openai_plugin, cfg: dict, volcengine_plugin=None):
    """按 provider 构造 RealtimeModel。插件注入便于测试。"""
    provider = cfg["provider"]
    if provider == "openai_realtime":
        # create_response=False：服务端检测到用户说完只提交 turn、不自动生成回复，
        # 由 OpenAIVoiceOrchestrator 在注入文字大脑答案后（或抢答超时后）手动
        # generate_reply 控制语序（文档 31 §3.1）。
        kwargs = {
            "model": cfg["model"],  # 档位由 agent 配置决定（建议 gpt-realtime-2，128k）
            "api_key": cfg["api_key"],
            "voice": cfg["voice"] or DEFAULT_VOICE,
            "modalities": ["text", "audio"],
            "input_audio_transcription": build_openai_transcription(),
            "turn_detection": build_openai_turn_detection(create_response=False),
        }
        if cfg["endpoint"]:
            kwargs["base_url"] = cfg["endpoint"]
        return openai_plugin.realtime.RealtimeModel(**kwargs)

    if provider == "doubao_realtime":
        if volcengine_plugin is None:
            raise BridgeConfigError("livekit-plugins-volcengine not installed")
        # 豆包账号已切火山新版 API Key 单 Key 鉴权（与 STT/TTS 念稿那路一致）：api_key 即控制台
        # API Key，不再是 appid:access_token。构造时插件仍要求 app_id/access_token，给占位符即可，
        # 真正鉴权走下面覆盖的 X-Api-Key 请求头。
        api_key = (cfg["api_key"] or "").strip()
        if not api_key:
            raise BridgeConfigError("doubao api_key (X-Api-Key) is required")
        kwargs = {
            "app_id": "-",          # 占位：实际鉴权走下面覆盖的 X-Api-Key
            "access_token": "-",
            "model": cfg["model"],  # "O" 或 "SC"
            # 语音大脑(relay)：把"判定用户说完"的静音窗收短到 800ms，让文字 agent 更早被触发、
            # 缩短问答间的静音空档；普通通话保持 1500ms（官方推荐值，减少长停顿被误判为说完）。
            "end_smooth_window_ms": 800 if cfg.get("relay_mode") else 1500,
        }
        if cfg["voice"]:
            kwargs["speaker"] = cfg["voice"]
        # 开场白显式受配置控制：非空则主动播报该文案；为空传 None 抑制插件默认的
        # 硬编码中文问候语（"你好啊，今天过得怎么样？"），避免跟 agent 配置/语言脱节。
        kwargs["opening"] = (cfg.get("opening") or "").strip() or None
        rt = volcengine_plugin.RealtimeModel(**kwargs)
        # 把端到端实时对话的 WS 鉴权头由 App-ID+Access-Key 换成 X-Api-Key。_RealtimeOptions 是
        # dataclass 实例，直接覆盖实例上的 get_ws_headers 方法（与 STT 的 _opts.get_ws_header 覆盖同理）。
        def _realtime_ws_headers() -> dict:
            return {
                "X-Api-Resource-Id": _DOUBAO_REALTIME_RESOURCE_ID,
                "X-Api-Key": api_key,
                "X-Api-App-Key": _DOUBAO_REALTIME_APP_KEY,
                "X-Api-Connect-Id": str(uuid.uuid4()),
            }
        rt._opts.get_ws_headers = _realtime_ws_headers
        return rt

    raise BridgeConfigError(f"voice provider not supported in voicebridge: {provider}")

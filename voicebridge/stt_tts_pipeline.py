"""语音大脑 STT+TTS 管线：豆包只当耳朵 + 嘴，文字 agent 是唯一大脑。

管线（替代端到端对话模型 realtime.py）：
  房间音频 → 豆包流式 STT(BigModelSTT) → 用户转写发布(触发文字 agent)
  → 文字 agent 异步回复(经 inject NATS) → 自定义 LLM 桥 → 豆包流式 TTS → 房间音频。

豆包不参与思考，不存在"自答/抢答"，因此不需要静音/垫场/强制打断那一套 hack——
打断、轮流说话、缓冲清理全部走 livekit AgentSession 框架原生(turn_detection="stt")。

仅 relay_mode(语音大脑) 通话走此路径；客服/普通语音仍走端到端对话模型。
"""
from __future__ import annotations

import asyncio
import logging
import uuid
from typing import Any

from livekit.agents import llm
from livekit.agents.llm import (
    ChatContext,
    FunctionTool,
    RawFunctionTool,
    ToolChoice,
)
from livekit.agents.types import (
    DEFAULT_API_CONNECT_OPTIONS,
    NOT_GIVEN,
    APIConnectOptions,
    NotGivenOr,
)

logger = logging.getLogger("voicebridge")

# 每通话一条回复队列：on_inject 收到文字 agent 回复后投入；LLM 桥每轮取出喂 TTS。
_reply_queues: dict[int, asyncio.Queue] = {}

# LLM 桥等待回复的超时：执行型文字 agent 可能数十秒，给足；超时则本轮不出声、正常结束。
_REPLY_TIMEOUT_S = 180.0

# 流式(边写边念)：一轮回复按句陆续到达，本句念完等下一句的最长间隔；超过即认为本轮结束(兜底，
# 正常由 eot 标记收尾)。设宽松，覆盖文字大脑句间生成停顿。
_STREAM_GAP_TIMEOUT_S = 12.0


def register_call(call_id: int) -> None:
    """通话起始：建立该通话的回复队列。"""
    _reply_queues[call_id] = asyncio.Queue()


def unregister_call(call_id: int) -> None:
    """通话结束：清理回复队列。"""
    _reply_queues.pop(call_id, None)


def is_pipeline_call(call_id: int) -> bool:
    """该通话是否走 STT+TTS 管线（据此 on_inject 决定投队列还是注入端到端模型）。"""
    return call_id in _reply_queues


def push_reply(call_id: int, text: str, eot: bool = True) -> bool:
    """on_inject 调用：把文字 agent 的回复(片段)投入队列，供 LLM 桥取出喂 TTS。
    eot=True 表示该片段是本轮回复的最后一段(整段非流式注入默认即 True)；
    流式(边写边念)时按句多次调用，最后一段 eot=True 收尾。
    返回 True 表示该通话是管线通话、已入队。"""
    q = _reply_queues.get(call_id)
    if q is None:
        return False
    q.put_nowait({"text": text, "eot": eot})
    return True


class TextAgentBridgeLLM(llm.LLM):
    """把异步文字 agent 接成 livekit LLM。

    AgentSession 在用户一轮说完(STT 判停)后调用 chat()；本桥不调用任何真实 LLM，
    而是 await 该通话回复队列里、由 on_inject 投入的文字 agent 真实回复，原样喂给 TTS。
    """

    def __init__(self, call_id: int):
        super().__init__()
        self._call_id = call_id

    @property
    def model(self) -> str:
        return "text-agent-bridge"

    def chat(
        self,
        *,
        chat_ctx: ChatContext,
        tools: list[FunctionTool | RawFunctionTool] | None = None,
        conn_options: APIConnectOptions = DEFAULT_API_CONNECT_OPTIONS,
        parallel_tool_calls: NotGivenOr[bool] = NOT_GIVEN,
        tool_choice: NotGivenOr[ToolChoice] = NOT_GIVEN,
        extra_kwargs: NotGivenOr[dict[str, Any]] = NOT_GIVEN,
    ) -> llm.LLMStream:
        return _BridgeStream(
            self,
            chat_ctx=chat_ctx,
            tools=tools or [],
            conn_options=conn_options,
            call_id=self._call_id,
        )


class _BridgeStream(llm.LLMStream):
    def __init__(self, llm_, *, chat_ctx, tools, conn_options, call_id):
        super().__init__(llm_, chat_ctx=chat_ctx, tools=tools, conn_options=conn_options)
        self._call_id = call_id

    def _emit(self, text: str) -> None:
        """把一段文字作为一个 ChatChunk 喂给 TTS（多次调用即流式：分句陆续合成出声）。"""
        text = (text or "").strip()
        if not text:
            return
        self._event_ch.send_nowait(
            llm.ChatChunk(
                id=str(uuid.uuid4()),
                delta=llm.ChoiceDelta(role="assistant", content=text),
            )
        )

    async def _run(self) -> None:
        q = _reply_queues.get(self._call_id)
        if q is None:
            return  # 通话已结束/非管线：空回复，框架正常收尾
        # 本轮开口前先清掉队列里的过期回复：用户刚说完本轮，此刻队列残留的都是上一轮的
        # 回复（文字 agent 一轮可能产出多条消息/句，每轮只取一条会积压），不清就会"答案慢一轮"。
        stale = 0
        while True:
            try:
                q.get_nowait()
                stale += 1
            except asyncio.QueueEmpty:
                break
        if stale:
            logger.info(f"[Pipeline] dropped {stale} stale reply(ies) at turn start call_id={self._call_id}")
        # 纯等待真实回复，不垫场：念稿模式要求"慢就等着"、中途不自己插话。
        try:
            item = await asyncio.wait_for(q.get(), timeout=_REPLY_TIMEOUT_S)
        except asyncio.TimeoutError:
            logger.warning(f"[Pipeline] reply timeout call_id={self._call_id}")
            return
        # 边写边念：本轮回复可能按句陆续到达(流式)；逐句喂 TTS 直到 eot 标记，让音频紧跟文字。
        # 非流式整段注入时 item.eot=True，念一句即止——行为与原来一致。
        self._emit(item.get("text", ""))
        while not item.get("eot", True):
            try:
                item = await asyncio.wait_for(q.get(), timeout=_STREAM_GAP_TIMEOUT_S)
            except asyncio.TimeoutError:
                logger.warning(f"[Pipeline] stream gap timeout, end turn call_id={self._call_id}")
                break
            self._emit(item.get("text", ""))


def build_pipeline_components(cfg: dict):
    """据 cfg.provider 构建管线的 STT + TTS(+VAD)（耳朵 + 嘴），文字 agent 仍是唯一大脑。

    provider=doubao_realtime → 豆包大模型 BigModelSTT + 豆包大模型 TTS（BYOK：api_key=火山控制台 API Key，
        新版 X-Api-Key 鉴权，不再用 appid+access_token），STT 自带判停，vad=None，轮次走 turn_detection="stt"；
    provider=openai_realtime → OpenAI gpt-4o-transcribe(非流式 HTTP 转写) + gpt-4o-mini-tts
        （BYOK：api_key=sk-...）。非流式 STT 无判停，配 silero VAD 做轮次与打断(turn_detection="vad")。
        ——OpenAI 实时转写已转正(GA，含原生流式模型 gpt-realtime-whisper)，但 GA 改了接口形态；
          本栈被 volcengine 插件(最新 1.3.0 仍硬钉 livekit-agents==1.2.9)锁在 agents 1.2.9，
          旧 openai 插件用的是 beta 接口形态、不兼容 GA，故暂走"VAD 切句 + 非流式转写"这条受支持的路径。
    返回 (stt, tts, vad)；vad 为 None 表示用 STT 判停。
    """
    provider = cfg.get("provider")
    if provider == "openai_realtime":
        return _build_openai_components(cfg)
    return _build_doubao_components(cfg)


# 豆包大模型语音新版鉴权：仅在请求头加 X-Api-Key(火山控制台 API Key)，不再用 appid+access_token。
# 文档：API Key 使用 https://www.volcengine.com/docs/6561/1816214
#       大模型流式语音识别 https://www.volcengine.com/docs/6561/1354869
_DOUBAO_STT_RESOURCE = "volc.seedasr.sauc.duration"       # 豆包识别 2.0 小时版（按时长计费）
_DOUBAO_DEFAULT_VOICE = "zh_female_cancan_mars_bigtts"    # 大模型音色（旧版 BV 标准音色新 key 无授权）
_DOUBAO_TTS_CLUSTER = "volcano_tts"


def _build_doubao_components(cfg: dict):
    """豆包大模型 STT + TTS，新版 X-Api-Key 鉴权（BYOK：api_key=控制台 API Key）。vad=None：STT 自带判停。

    插件(volcengine 1.x)仍按旧版 appid+token 拼鉴权头，这里不动插件源码，构造后就地把两端的
    鉴权头替换为 X-Api-Key——STT 的 _opts 是 dataclass(直接覆盖实例方法)，TTS 的 _opts 是 pydantic
    模型(子类覆盖 get_ws_header 后整体替换)。
    """
    from livekit.agents import utils as _u
    from livekit.plugins.volcengine import TTS, BigModelSTT
    from livekit.plugins.volcengine.tts import _TTSOptions

    api_key = (cfg.get("api_key") or "").strip()
    if not api_key:
        raise ValueError("doubao api_key (X-Api-Key) is required")

    # —— STT：v3/sauc/bigmodel，按时长版；鉴权头改为 X-Api-Key ——
    stt = BigModelSTT(
        app_id="-",            # 占位：实际鉴权走下面覆盖的 X-Api-Key
        access_token="-",
        enable_punc=True,
        end_window_size=800,   # 静音判停 800ms：实时性优先，与端到端方案一致
        force_to_speech_time=1000,
    )
    stt._opts.source_type = "duration"

    def _stt_header(reqid: str | None = None) -> dict[str, str]:
        return {
            "X-Api-Resource-Id": _DOUBAO_STT_RESOURCE,
            "X-Api-Key": api_key,
            "X-Api-Request-Id": reqid or _u.shortuuid(),
        }

    stt._opts.get_ws_header = _stt_header  # dataclass 实例：直接覆盖方法

    # —— TTS：v1/tts/ws_binary，大模型音色；鉴权头改为 X-Api-Key ——
    voice = cfg.get("tts_voice") or cfg.get("voice") or _DOUBAO_DEFAULT_VOICE
    cluster = cfg.get("tts_cluster") or _DOUBAO_TTS_CLUSTER
    tts = TTS(
        app_id="-",            # 占位：payload 里的 appid/token 在 X-Api-Key 鉴权下被忽略
        cluster=cluster,
        access_token="-",
        voice=voice,
        sample_rate=24000,
    )

    class _ApiKeyTTSOptions(_TTSOptions):
        def get_ws_header(self) -> dict[str, str]:
            return {"X-Api-Key": api_key}

    tts._opts = _ApiKeyTTSOptions(**tts._opts.model_dump())
    return stt, tts, None


# OpenAI 管线默认模型：转写用 gpt-4o-transcribe(非流式 HTTP /v1/audio/transcriptions，受支持)，
# 合成用 gpt-4o-mini-tts；均可由 agent 配置 stt_model/tts_model 覆盖。
_OPENAI_STT_MODEL = "gpt-4o-transcribe"
_OPENAI_TTS_MODEL = "gpt-4o-mini-tts"
_OPENAI_DEFAULT_VOICE = "alloy"

# silero VAD 单例：进程内仅加载一次 ONNX 模型，跨通话复用（每路 stream 各自维护状态）。
_silero_vad = None


def _get_silero_vad():
    """懒加载 silero VAD 单例。本地 CPU 推理(force_cpu)，模型随插件打包、加载不联网。
    min_silence_duration=0.45：静音 450ms 即判定用户说完，换取问答间更短停顿。"""
    global _silero_vad
    if _silero_vad is None:
        from livekit.plugins import silero
        _silero_vad = silero.VAD.load(min_silence_duration=0.45)
    return _silero_vad


def _build_openai_components(cfg: dict):
    """OpenAI 非流式 STT(gpt-4o-transcribe) + TTS(gpt-4o-mini-tts) + silero VAD（BYOK：api_key=sk-...）。

    use_realtime=False：转写走 HTTP /v1/audio/transcriptions（这条非流式路径本身就是 GA 接口、实测正常）。
    不开流式的真因：本栈被 volcengine 1.3.0 锁在 agents 1.2.9，而 1.2.9 的 openai 流式转写走的是
    已被官方停用的 beta WS 协议（连上即被 GA 端点拒，报 beta_api_shape_disabled）；GA 流式协议要
    openai 插件 1.6.x（须 agents>=1.6.1，与火山冲突→须拆栈），故念稿暂用受支持的非流式路径。非流式 STT 无判停，
    由 silero VAD 切句并触发转写、做原生打断，AgentSession 用 turn_detection="vad"。
    voice 取自 agent 配置 voice 字段（OpenAI 音色名，如 alloy/ash/coral...），缺省 alloy。
    endpoint(若配) 作为 base_url 透传，支持自建代理出网。返回 (stt, tts, vad)。
    """
    from livekit.plugins import openai as lk_openai

    api_key = cfg.get("api_key") or ""
    if not api_key:
        raise ValueError("openai api_key required")
    # 语言：zh-CN → zh，喂给转写接口，缺省中文。
    lang = ((cfg.get("language") or "zh").split("-")[0]).strip() or "zh"
    base_url = (cfg.get("endpoint") or "").strip()

    stt_kwargs = dict(
        model=cfg.get("stt_model") or _OPENAI_STT_MODEL,
        language=lang,
        use_realtime=False,
        api_key=api_key,
    )
    tts_kwargs = dict(
        model=cfg.get("tts_model") or _OPENAI_TTS_MODEL,
        voice=cfg.get("tts_voice") or cfg.get("voice") or _OPENAI_DEFAULT_VOICE,
        api_key=api_key,
    )
    if base_url:
        stt_kwargs["base_url"] = base_url
        tts_kwargs["base_url"] = base_url

    stt = lk_openai.STT(**stt_kwargs)
    tts = lk_openai.TTS(**tts_kwargs)
    vad = _get_silero_vad()
    return stt, tts, vad

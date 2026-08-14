"""OpenAI Realtime 语序编排器（接点B + 抢答计时器）。

豆包"听到即答压不住"，GPT 通过 turn_detection.create_response=False 把"何时开口"
交给我们控制。本编排器实现文档 31 §3.1 的轮次时序：

  用户说完一句（turn 结束）
    → 启动抢答计时器（默认 800ms）
    → 计时器内文字大脑答案到：注入权威资料 + 立即开口（又快又准）
    → 计时器超时大脑未到：凭内置知识先开口（不冷场），大脑答案晚到则作为
       滚动上下文注入，供后续轮次校准（决策2：仅滚动注入，不打断已说内容）
    → 用户开始说下一句：取消计时器、关闭本轮，丢弃过期注入

本模块刻意不依赖 livekit / openai，全部副作用经注入的回调完成，便于 stdlib 单测：
  - reply_fn():           触发 GPT 生成一次回复（generate_reply）
  - inject_fn(text)->awaitable: 把权威资料追加进对话上下文（update_chat_ctx）
  - loop:                 asyncio 事件循环（计时器用 call_later）
"""

import asyncio
import logging

logger = logging.getLogger("voicebridge")

DEFAULT_RACE_MS = 800


class OpenAIVoiceOrchestrator:
    """单通 openai 通话的语序编排。线程模型：全部在 bridge 的事件循环单线程内调用。"""

    def __init__(self, call_id, reply_fn, inject_fn, loop, race_ms=DEFAULT_RACE_MS):
        self._call_id = call_id
        self._reply_fn = reply_fn
        self._inject_fn = inject_fn
        self._loop = loop
        self._race_ms = max(0, int(race_ms))

        self._round = 0            # 用户每说完一句 +1
        self._open_round = 0       # 当前等待回复的轮次（0=无）
        self._replied_round = 0    # 已回复的最大轮次（保证每轮只答一次）
        self._race_handle = None   # asyncio.TimerHandle
        self._muted = False        # 接管静音：True 时不开口（Phase 4 用）
        self._closed = False

    # ── 来自 AgentSession 事件的通知（main.py 在 user_state_changed 回调里调用）──

    def notify_user_started(self):
        """用户开始说话：取消抢答计时器，避免抢在用户头上开口；关闭旧轮。"""
        if self._closed:
            return
        self._cancel_timer()
        # 用户开口意味着上一轮（若还没答）已被打断，标记为已结束，丢弃其过期注入
        if self._open_round and self._open_round > self._replied_round:
            logger.info(f"[GPT-Seq] call={self._call_id} round={self._open_round} abandoned (user spoke)")
        self._open_round = 0

    def notify_user_stopped(self):
        """用户说完一句（turn 结束）：开一轮，启动抢答计时器。"""
        if self._closed:
            return
        self._round += 1
        self._open_round = self._round
        self._cancel_timer()
        if self._muted:
            # 接管静音：只听不说，开轮但不安排开口
            logger.info(f"[GPT-Seq] call={self._call_id} round={self._round} muted, listen-only")
            return
        self._race_handle = self._loop.call_later(
            self._race_ms / 1000.0, self._on_race_timeout, self._round
        )
        logger.info(f"[GPT-Seq] call={self._call_id} round={self._round} race timer {self._race_ms}ms")

    # ── 接点B：文字大脑答案注入 ──

    async def on_brain_inject(self, text, round_seq=0):
        """文字大脑回复到达：先把权威资料注入上下文；若本轮还没开口，抢答立即开口。"""
        if self._closed or not text:
            return
        try:
            await self._inject_fn(text)
        except Exception as e:
            logger.warning(f"[GPT-Seq] call={self._call_id} inject failed: {e}")
            return
        # 本轮尚未回复 → 抢答命中：带着权威资料立即开口
        if not self._muted and self._open_round and self._open_round > self._replied_round:
            logger.info(f"[GPT-Seq] call={self._call_id} round={self._open_round} brain-win, reply now")
            self._do_reply(self._open_round)
        else:
            logger.info(f"[GPT-Seq] call={self._call_id} brain answer injected as rolling context")

    # ── 接管静音（Phase 4）──

    def set_muted(self, muted):
        """接管=True：抑制开口（停止 generate_reply 调度），但音频继续喂、上下文继续积累。"""
        self._muted = bool(muted)
        if self._muted:
            self._cancel_timer()
        logger.info(f"[GPT-Seq] call={self._call_id} muted={self._muted}")

    # ── 内部 ──

    def _on_race_timeout(self, round_id):
        self._race_handle = None
        if self._closed or self._muted:
            return
        # 计时器期间大脑没来，凭内置知识先答
        if round_id == self._open_round and round_id > self._replied_round:
            logger.info(f"[GPT-Seq] call={self._call_id} round={round_id} race timeout, reply from built-in")
            self._do_reply(round_id)

    def _do_reply(self, round_id):
        if round_id <= self._replied_round:
            return
        self._replied_round = round_id
        self._cancel_timer()
        try:
            self._reply_fn()
        except Exception as e:
            logger.warning(f"[GPT-Seq] call={self._call_id} reply_fn failed: {e}")

    def _cancel_timer(self):
        if self._race_handle is not None:
            self._race_handle.cancel()
            self._race_handle = None

    def aclose(self):
        self._closed = True
        self._cancel_timer()

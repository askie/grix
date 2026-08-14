"""OpenAIVoiceOrchestrator 单测（stdlib unittest + 真实 asyncio 事件循环）。

抢答时序用很短的 race_ms（如 30ms）配合 asyncio.sleep 推进，确定性验证：
  - 抢答超时 → 凭内置知识开口
  - 大脑答案先到 → 抢答命中立即开口（且只答一次）
  - 用户插话 → 取消计时器、不开口
  - 接管静音 → 只听不说，注入不触发开口
  - 过期注入（已答轮）→ 仅滚动注入不重复开口
"""
import asyncio
import unittest

from openai_orchestrator import OpenAIVoiceOrchestrator


class _Spy:
    def __init__(self):
        self.replies = 0
        self.injected = []

    def reply(self):
        self.replies += 1

    async def inject(self, text):
        self.injected.append(text)


def _mk(loop, race_ms=30):
    spy = _Spy()
    orch = OpenAIVoiceOrchestrator(
        call_id=1, reply_fn=spy.reply, inject_fn=spy.inject, loop=loop, race_ms=race_ms,
    )
    return orch, spy


class OrchestratorTest(unittest.IsolatedAsyncioTestCase):

    async def test_race_timeout_replies_from_builtin(self):
        loop = asyncio.get_running_loop()
        orch, spy = _mk(loop, race_ms=30)
        orch.notify_user_stopped()           # 用户说完一句
        self.assertEqual(spy.replies, 0)     # 还没到点
        await asyncio.sleep(0.06)
        self.assertEqual(spy.replies, 1)     # 抢答超时，凭内置知识开口
        orch.aclose()

    async def test_brain_win_replies_immediately_once(self):
        loop = asyncio.get_running_loop()
        orch, spy = _mk(loop, race_ms=500)   # 计时器故意调长，制造"大脑先到"
        orch.notify_user_stopped()
        await orch.on_brain_inject("权威答案", round_seq=1)
        self.assertEqual(spy.injected, ["权威答案"])
        self.assertEqual(spy.replies, 1)     # 抢答命中，立即开口
        await asyncio.sleep(0.6)             # 计时器到点也不应再答
        self.assertEqual(spy.replies, 1)     # 每轮只答一次
        orch.aclose()

    async def test_user_interrupt_cancels_reply(self):
        loop = asyncio.get_running_loop()
        orch, spy = _mk(loop, race_ms=30)
        orch.notify_user_stopped()
        orch.notify_user_started()           # 用户又开口，取消本轮
        await asyncio.sleep(0.06)
        self.assertEqual(spy.replies, 0)     # 不抢用户的话
        orch.aclose()

    async def test_muted_listens_only(self):
        loop = asyncio.get_running_loop()
        orch, spy = _mk(loop, race_ms=30)
        orch.set_muted(True)                 # 接管
        orch.notify_user_stopped()
        await asyncio.sleep(0.06)
        self.assertEqual(spy.replies, 0)     # 静音不开口
        await orch.on_brain_inject("资料", round_seq=1)
        self.assertEqual(spy.injected, ["资料"])  # 仍注入上下文
        self.assertEqual(spy.replies, 0)     # 但不开口
        orch.aclose()

    async def test_late_inject_is_rolling_context_only(self):
        loop = asyncio.get_running_loop()
        orch, spy = _mk(loop, race_ms=20)
        orch.notify_user_stopped()
        await asyncio.sleep(0.05)            # 本轮已凭内置知识答过
        self.assertEqual(spy.replies, 1)
        await orch.on_brain_inject("晚到的精确答案", round_seq=1)
        self.assertEqual(spy.injected, ["晚到的精确答案"])  # 注入了
        self.assertEqual(spy.replies, 1)     # 但不重复开口（决策2：滚动注入供下轮）
        orch.aclose()

    async def test_two_rounds_each_reply_once(self):
        loop = asyncio.get_running_loop()
        orch, spy = _mk(loop, race_ms=20)
        orch.notify_user_stopped()           # round 1
        await asyncio.sleep(0.05)
        orch.notify_user_started()
        orch.notify_user_stopped()           # round 2
        await asyncio.sleep(0.05)
        self.assertEqual(spy.replies, 2)     # 两轮各答一次
        orch.aclose()

    async def test_closed_is_inert(self):
        loop = asyncio.get_running_loop()
        orch, spy = _mk(loop, race_ms=20)
        orch.aclose()
        orch.notify_user_stopped()
        await asyncio.sleep(0.05)
        await orch.on_brain_inject("x", 1)
        self.assertEqual(spy.replies, 0)
        self.assertEqual(spy.injected, [])


if __name__ == "__main__":
    unittest.main()

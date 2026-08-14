"""语音大脑"念稿模式"(STT+TTS 管线) 的 LLM 桥接 _BridgeStream._run() 单测。

用标准库 unittest（不依赖 pytest），可在裸 venv 直接跑：
    python -m unittest test_stt_tts_pipeline -v

覆盖念稿模式的两条核心行为：
1. 慢回复不垫场——真实回复 1.5s 后才到，桥接期间不主动出声，最终只喂一句真实回复；
2. 超时静默收尾——文字 agent 一直不回，达到 _REPLY_TIMEOUT_S 后本轮不出声，也不抛异常。
"""
import asyncio
import unittest
from unittest.mock import patch

import stt_tts_pipeline as p


class _RecordingBridge(p._BridgeStream):
    """绕开 livekit.LLMStream 基类的 event_ch/task 机制，只测 _run() 逻辑。

    直接手工塞入需要的实例字段，用列表记录 _emit 调用，避免真起协程/管道。
    """

    def __init__(self, call_id: int):
        # 不调 super().__init__：不需要 livekit 的 _event_ch / _task / _tools 等
        self._call_id = call_id
        self.emitted: list[str] = []

    def _emit(self, text: str) -> None:
        text = (text or "").strip()
        if text:
            self.emitted.append(text)


class TestBridgeRunSlowReply(unittest.IsolatedAsyncioTestCase):
    """慢回复(1.5s)场景：桥接期间不主动开口，最终只播真实回复。"""

    async def test_slow_reply_no_filler_and_emits_real_text(self):
        call_id = 90001
        p.register_call(call_id)
        self.addCleanup(p.unregister_call, call_id)

        bridge = _RecordingBridge(call_id)

        # 收紧超时避免用例本身耗时；但仍远大于"慢回复"延迟，模拟真实等待。
        with patch.object(p, "_REPLY_TIMEOUT_S", 5.0):
            run_task = asyncio.create_task(bridge._run())

            # 模拟文字 agent 1.5s 后才回，中间桥接不应有任何 _emit。
            await asyncio.sleep(1.5)
            self.assertEqual(
                bridge.emitted, [],
                "念稿模式下慢回复期间禁止垫场，_emit 不应被调用",
            )
            self.assertFalse(run_task.done(), "真实回复未到前 _run 应仍在等待")

            self.assertTrue(
                p.push_reply(call_id, "真实的答复内容", eot=True),
                "push_reply 应返回 True 表示已入队",
            )
            await asyncio.wait_for(run_task, timeout=2.0)

        self.assertEqual(
            bridge.emitted, ["真实的答复内容"],
            "慢回复场景最终只应播出文字 agent 的真实回复一次",
        )


class TestBridgeRunTimeout(unittest.IsolatedAsyncioTestCase):
    """超时场景：达到 _REPLY_TIMEOUT_S 仍无回复，静默收尾且不抛异常。"""

    async def test_timeout_stays_silent_and_returns_cleanly(self):
        call_id = 90002
        p.register_call(call_id)
        self.addCleanup(p.unregister_call, call_id)

        bridge = _RecordingBridge(call_id)

        # 把超时压到 100ms，测试快速走完超时分支。
        with patch.object(p, "_REPLY_TIMEOUT_S", 0.1):
            # _run 不应抛异常——TimeoutError 被内部捕获、直接 return。
            await asyncio.wait_for(bridge._run(), timeout=1.0)

        self.assertEqual(
            bridge.emitted, [],
            "超时应当静默收尾，不产出任何音频文本",
        )


if __name__ == "__main__":
    unittest.main()

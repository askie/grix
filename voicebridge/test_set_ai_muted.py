"""四档语音通话 接管=静音/交回=恢复 的 _set_ai_muted 与静音保活单测。

用标准库 unittest（不依赖 pytest），可在裸 venv 直接跑：
    python -m unittest test_set_ai_muted -v

覆盖：mute 关输入+输出+打断、unmute 恢复且不打断、接管↔交回循环 session 不重建、
无 session 不报错；静音保活帧仅 doubao 启动、unmute/session 替换后自动停止。
"""
import asyncio
import unittest

import main


class _FakeIO:
    def __init__(self):
        self.enabled = True

    def set_audio_enabled(self, v):
        self.enabled = v


class _FakeSession:
    def __init__(self):
        self.input = _FakeIO()
        self.output = _FakeIO()
        self.interrupted = 0

    def interrupt(self):
        self.interrupted += 1


class TestSetAiMuted(unittest.TestCase):
    def test_mute_disables_io_and_interrupts(self):
        s = _FakeSession()
        entry = {"session": s}
        main._set_ai_muted(entry, True)
        self.assertFalse(s.input.enabled, "接管应关闭 AI 输入（不再喂访客音频）")
        self.assertFalse(s.output.enabled, "接管应关闭 AI 输出（不出声）")
        self.assertEqual(s.interrupted, 1, "接管应打断当前回复一次")
        self.assertTrue(entry["muted"])

    def test_unmute_reenables_io_without_interrupt(self):
        s = _FakeSession()
        entry = {"session": s}
        main._set_ai_muted(entry, True)
        main._set_ai_muted(entry, False)
        self.assertTrue(s.input.enabled, "交回应恢复 AI 输入")
        self.assertTrue(s.output.enabled, "交回应恢复 AI 发声")
        self.assertEqual(s.interrupted, 1, "交回不应再次打断")
        self.assertFalse(entry["muted"])

    def test_takeover_handback_loop_keeps_same_session(self):
        """接管→交回→再接管多次循环：始终同一个 session，不重建。"""
        s = _FakeSession()
        entry = {"session": s}
        for _ in range(3):
            main._set_ai_muted(entry, True)
            self.assertFalse(s.output.enabled)
            main._set_ai_muted(entry, False)
            self.assertTrue(s.output.enabled)
        self.assertIs(entry["session"], s, "全程应是同一个 session 对象")
        self.assertEqual(s.interrupted, 3, "三次接管各打断一次")

    def test_no_session_is_noop(self):
        # 不应抛异常
        main._set_ai_muted({}, True)
        main._set_ai_muted({"session": None}, False)


class _FakeRT:
    """模拟 volcengine RealtimeSession：记录 push_audio 帧。"""

    def __init__(self):
        self.frames = []

        class _Opts:
            sample_rate = 24000

        class _Model:
            _opts = _Opts()

        self._realtime_model = _Model()

    def push_audio(self, frame):
        self.frames.append(frame)


class TestMuteKeepalive(unittest.TestCase):
    def test_non_doubao_does_not_start(self):
        entry = {"provider": "openai_realtime", "muted": True}
        main._start_mute_keepalive(1, entry)
        self.assertNotIn("keepalive_task", entry)

    def test_doubao_pushes_silence_and_stops_on_unmute(self):
        async def run():
            rt = _FakeRT()
            main._inject_sessions[42] = rt
            try:
                entry = {"provider": "doubao_realtime", "muted": True}
                main._start_mute_keepalive(42, entry)
                self.assertIn("keepalive_task", entry)
                await asyncio.sleep(0.35)
                self.assertGreaterEqual(len(rt.frames), 2, "静音期间应持续喂静音帧")
                # 帧参数正确：100ms @ 24kHz = 2400 samples
                self.assertEqual(rt.frames[0].samples_per_channel, 2400)
                entry["muted"] = False  # 交回
                await asyncio.sleep(0.25)
                n = len(rt.frames)
                await asyncio.sleep(0.25)
                self.assertEqual(len(rt.frames), n, "交回后应停止喂帧")
                main._stop_mute_keepalive(entry)
            finally:
                main._inject_sessions.pop(42, None)

        asyncio.run(run())

    def test_stops_when_session_gone(self):
        async def run():
            rt = _FakeRT()
            main._inject_sessions[43] = rt
            entry = {"provider": "doubao_realtime", "muted": True}
            main._start_mute_keepalive(43, entry)
            task = entry["keepalive_task"]
            await asyncio.sleep(0.15)
            main._inject_sessions.pop(43, None)  # 模拟 session 死亡
            await asyncio.sleep(0.25)
            self.assertTrue(task.done(), "session 消失后保活任务应自行退出")

        asyncio.run(run())

    def test_stop_cancels_task(self):
        async def run():
            rt = _FakeRT()
            main._inject_sessions[44] = rt
            try:
                entry = {"provider": "doubao_realtime", "muted": True}
                main._start_mute_keepalive(44, entry)
                task = entry["keepalive_task"]
                main._stop_mute_keepalive(entry)
                self.assertNotIn("keepalive_task", entry)
                await asyncio.sleep(0.05)
                self.assertTrue(task.done())
            finally:
                main._inject_sessions.pop(44, None)

        asyncio.run(run())


if __name__ == "__main__":
    unittest.main()

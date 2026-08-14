"""三方通话主人插话音频接入（方案B）纯逻辑单测。

用标准库 unittest（不依赖 pytest），可在裸 venv 直接跑：
    python -m unittest test_owner_audio -v

覆盖：frame_rms 静音/定幅能量；OwnerTurnDetector 起音去抖、释放尾窗、
说完后再次说话可重新激活、起音前的瞬时噪声不触发。
"""
import array
import asyncio
import unittest

import main
from owner_audio import OwnerTurnDetector, frame_rms


def _pcm(amplitude: int, n: int) -> bytes:
    return array.array("h", [amplitude] * n).tobytes()


class TestFrameRMS(unittest.TestCase):
    def test_empty_is_zero(self):
        self.assertEqual(frame_rms(b""), 0.0)

    def test_silence_is_zero(self):
        self.assertEqual(frame_rms(_pcm(0, 480)), 0.0)

    def test_constant_amplitude_equals_amplitude(self):
        # 定幅 PCM 的 RMS 等于幅值
        self.assertAlmostEqual(frame_rms(_pcm(1000, 480)), 1000.0, places=3)

    def test_odd_trailing_byte_ignored(self):
        # 半个样本的尾字节被丢弃，不应抛异常
        data = _pcm(800, 10) + b"\x01"
        self.assertAlmostEqual(frame_rms(data), 800.0, places=3)

    def test_speech_louder_than_silence(self):
        self.assertGreater(frame_rms(_pcm(2000, 480)), frame_rms(_pcm(10, 480)))


class TestOwnerTurnDetector(unittest.TestCase):
    def test_attack_debounce_requires_consecutive_frames(self):
        d = OwnerTurnDetector(threshold=400.0, attack_frames=3, release_sec=1.0)
        # 单帧响声不触发
        self.assertFalse(d.feed(1000.0, 0.00))
        # 中间插一帧静音，连续计数被打断
        self.assertFalse(d.feed(10.0, 0.02))
        self.assertFalse(d.feed(1000.0, 0.04))
        self.assertFalse(d.feed(1000.0, 0.06))
        # 第三帧连续过阈值 → 激活
        self.assertTrue(d.feed(1000.0, 0.08))

    def test_release_holds_then_deactivates(self):
        d = OwnerTurnDetector(threshold=400.0, attack_frames=2, release_sec=1.0)
        self.assertFalse(d.feed(1000.0, 0.0))
        self.assertTrue(d.feed(1000.0, 0.02))  # 激活
        # 进入静音，但未到释放尾窗 → 仍保持激活
        self.assertTrue(d.feed(10.0, 0.5))
        self.assertTrue(d.feed(10.0, 1.0))
        # 距最后一帧过阈值已超过 release_sec → 关闭
        self.assertFalse(d.feed(10.0, 1.03))

    def test_voiced_frame_extends_release_window(self):
        d = OwnerTurnDetector(threshold=400.0, attack_frames=1, release_sec=1.0)
        self.assertTrue(d.feed(1000.0, 0.0))   # 单帧即激活
        self.assertTrue(d.feed(10.0, 0.9))     # 尾窗内静音，保持
        self.assertTrue(d.feed(1000.0, 1.5))   # 又说话，刷新 last_voiced
        self.assertTrue(d.feed(10.0, 2.4))     # 距 1.5 未满 1s，仍保持
        self.assertFalse(d.feed(10.0, 2.6))    # 距 1.5 已过 1s，关闭

    def test_reactivates_after_release(self):
        d = OwnerTurnDetector(threshold=400.0, attack_frames=1, release_sec=0.5)
        self.assertTrue(d.feed(1000.0, 0.0))
        self.assertFalse(d.feed(10.0, 0.6))    # 释放
        # 再次说话可重新激活
        self.assertTrue(d.feed(1000.0, 1.0))

    def test_silence_only_never_activates(self):
        d = OwnerTurnDetector(threshold=400.0, attack_frames=2, release_sec=1.0)
        for i in range(20):
            self.assertFalse(d.feed(50.0, i * 0.02))
        self.assertFalse(d.active)


import livekit.rtc as rtc  # noqa: E402


class _Frame:
    def __init__(self, data):
        self.data = data
        self.sample_rate = 24000
        self.num_channels = 1


class _Ev:
    def __init__(self, frame):
        self.frame = frame


class _Track:
    kind = rtc.TrackKind.KIND_AUDIO

    def __init__(self, frames, participant):
        self._frames = frames
        self._participant = participant


class _Pub:
    kind = rtc.TrackKind.KIND_AUDIO

    def __init__(self, track):
        self.track = track


class _Participant:
    def __init__(self, identity):
        self.identity = identity
        self.track_publications = {}


class _Room:
    def __init__(self, participant):
        self.remote_participants = {participant.identity: participant}

    def on(self, *_):
        pass

    def off(self, *_):
        pass


import time as _real_time  # noqa: E402


class _FakeClock:
    """只替换 monotonic，其余属性透传给真实 time，避免影响 asyncio 计时。"""

    def __init__(self, gen):
        self._gen = gen

    def monotonic(self):
        return next(self._gen)

    def __getattr__(self, name):
        return getattr(_real_time, name)


class _Opts:
    sample_rate = 24000


class _Model:
    def __init__(self, ctx):
        self._grix_ctx = ctx
        self._opts = _Opts()


class _IO:
    def __init__(self):
        self.calls = []

    def set_audio_enabled(self, v):
        self.calls.append(v)


class _Session:
    def __init__(self):
        self.input = _IO()


class _RT:
    def __init__(self):
        self.pushed = 0

    def push_audio(self, _frame):
        self.pushed += 1


class _FakeAudioStream:
    """一次性回放脚本帧；消费后清空该 participant 的发布，避免外层循环重放。"""

    def __init__(self, track, sample_rate=None, num_channels=None, **kw):
        self._frames = list(track._frames)
        track._participant.track_publications.clear()

    def __aiter__(self):
        self._it = iter(self._frames)
        return self

    async def __anext__(self):
        try:
            return next(self._it)
        except StopIteration:
            raise StopAsyncIteration

    async def aclose(self):
        pass


def _loud(n=480):
    return _Ev(_Frame(array.array("h", [1200] * n).tobytes()))


def _silent(n=480):
    return _Ev(_Frame(array.array("h", [5] * n).tobytes()))


class _Activity:
    def __init__(self, rt):
        self.realtime_llm_session = rt


class _SessionWithActivity(_Session):
    def __init__(self, rt):
        super().__init__()
        self._activity = _Activity(rt)


class TestPushTarget(unittest.TestCase):
    def test_doubao_uses_inject_sessions(self):
        rt = _RT()
        main._inject_sessions[7001] = rt
        try:
            self.assertIs(main._realtime_push_target(7001, _Session(), "doubao_realtime"), rt)
        finally:
            main._inject_sessions.pop(7001, None)

    def test_openai_uses_activity_realtime_session(self):
        rt = _RT()
        session = _SessionWithActivity(rt)
        self.assertIs(main._realtime_push_target(7002, session, "openai_realtime"), rt)

    def test_missing_target_is_none(self):
        self.assertIsNone(main._realtime_push_target(7003, _Session(), "openai_realtime"))

    def test_owner_suppressed_default_false(self):
        self.assertFalse(main._owner_suppressed(123456))


class TestOwnerAudioBridge(unittest.IsolatedAsyncioTestCase):
    async def _run_case(self, call_id, provider, session, push_rt):
        """跑一遍脚本帧（4 响 + 2 静音），返回观测：是否抑制过、是否抑制清除、input 让位记录、推帧数。"""
        model = _Model(None)
        participant = _Participant("user-7")
        times = [0.00, 0.02, 0.04, 0.06, 0.30, 0.32]  # 每帧对应一次 monotonic
        frames = [_loud(), _loud(), _loud(), _loud(), _silent(), _silent()]
        track = _Track(frames, participant)
        participant.track_publications["t"] = _Pub(track)
        room = _Room(participant)

        main._sessions[call_id] = {"muted": False}
        if provider == "doubao_realtime":
            main._inject_sessions[call_id] = push_rt

        orig_release = main._OWNER_RELEASE_SEC
        orig_hold = main._OWNER_TRANSCRIPT_HOLD
        orig_audio_stream = rtc.AudioStream
        orig_time = main.time
        main._OWNER_RELEASE_SEC = 0.1
        main._OWNER_TRANSCRIPT_HOLD = 0.05
        rtc.AudioStream = _FakeAudioStream

        def _clk():
            for t in times:
                yield t
            while True:
                yield times[-1]

        main.time = _FakeClock(_clk())

        saw_true = False
        saw_clear = False
        try:
            task = asyncio.create_task(
                main._run_owner_audio_bridge(call_id, 7, room, session, model, provider)
            )
            for _ in range(60):
                await asyncio.sleep(0.01)
                sp = main._owner_suppressed(call_id)
                if sp:
                    saw_true = True
                if saw_true and not sp and push_rt.pushed:
                    saw_clear = True
                    break
            cleaned_while_alive = main._owner_speaking.get(call_id, [False])[0]
            task.cancel()
            try:
                await task
            except asyncio.CancelledError:
                pass
        finally:
            main._OWNER_RELEASE_SEC = orig_release
            main._OWNER_TRANSCRIPT_HOLD = orig_hold
            rtc.AudioStream = orig_audio_stream
            main.time = orig_time
            main._sessions.pop(call_id, None)
            main._inject_sessions.pop(call_id, None)
        return saw_true, saw_clear, session.input.calls, push_rt.pushed, cleaned_while_alive

    async def test_doubao_owner_turn_suppresses_and_ducks(self):
        rt = _RT()
        saw_true, saw_clear, calls, pushed, _ = await self._run_case(
            9001, "doubao_realtime", _Session(), rt)
        self.assertTrue(saw_true, "主人说话期间应置位转写抑制")
        self.assertTrue(saw_clear, "保持窗过后应解除转写抑制")
        self.assertIn(False, calls, "应曾关闭客户输入(让位)")
        self.assertEqual(calls[-1], True, "轮次结束应恢复客户输入")
        self.assertGreaterEqual(pushed, 2, "应把主人音频帧推给实时 session")
        # 任务结束后注册表清理
        self.assertNotIn(9001, main._owner_speaking)

    async def test_openai_owner_turn_pushes_to_activity_session(self):
        rt = _RT()
        session = _SessionWithActivity(rt)
        saw_true, saw_clear, calls, pushed, _ = await self._run_case(
            9002, "openai_realtime", session, rt)
        self.assertTrue(saw_true, "openai 同样应置位转写抑制")
        self.assertTrue(saw_clear)
        self.assertIn(False, calls)
        self.assertGreaterEqual(pushed, 2, "openai 应推给 activity.realtime_llm_session")
        self.assertNotIn(9002, main._owner_speaking)


if __name__ == "__main__":
    unittest.main()

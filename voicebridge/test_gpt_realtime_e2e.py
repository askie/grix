"""GPT (OpenAI Realtime) 语音接入端到端测试 —— 连真实 GPT 验证整条链路。

与 test_e2e.py 同样用本机 LiveKit + NATS，但额外连真实 OpenAI Realtime，
in-process 调用 main.start_bridge（一个 bridge 实例，避免多进程 DuplicateIdentity）。
覆盖文档 31 的 openai 路径：连接/接点A 转写/语序编排(create_response=False+抢答)/
接点B 注入路由/四档接管(只听不说)。

⚠️ 会真实调用 GPT，产生费用，故默认跳过。需三个前置同时满足才运行：
  1. 环境变量 GRIX_GPT_E2E=1                显式开启（避免误跑烧钱）
  2. 环境变量 OPEN_API_KEY 或 OPENAI_API_KEY 真实 key
  3. 本机 LiveKit(7880,devkey:secret) + NATS(4222) 已起
     （docker compose -f backend/docker-compose.yml up -d nats livekit）

运行：
  GRIX_GPT_E2E=1 cd voicebridge && ../.venv/bin/python -m unittest test_gpt_realtime_e2e -v
  （OPEN_API_KEY 通常已在 shell 环境里）

音频素材：voicebridge/testdata/caller_zh_48k_mono.pcm（48kHz 单声道 s16le 中文提问）；
缺失时在 macOS 上用 say 自动生成。
"""
import array
import asyncio
import json
import os
import subprocess
import sys
import time
import unittest

sys.path.insert(0, os.path.dirname(__file__))

LK_URL = "ws://localhost:7880"
LK_API_KEY = "devkey"
LK_API_SECRET = "secret"
NATS_URL = "nats://localhost:4222"
MODEL = os.environ.get("GRIX_GPT_E2E_MODEL", "gpt-realtime-2")

_PCM_PATH = os.path.join(os.path.dirname(__file__), "testdata", "caller_zh_48k_mono.pcm")
_CALLER_TEXT = "你好，请问你们今天营业到几点？另外周末有没有优惠活动？"
_SYSTEM_PROMPT = (
    "你是好望角茶楼的电话客服小望。已知信息：营业时间每天上午10点到晚上10点；"
    "周末（周六日）全场茶水8折；地址在望湖路88号。"
)

_infra_imported = False
try:
    import nats
    from livekit import rtc
    from livekit.api import AccessToken, VideoGrants
    _infra_imported = True
except ImportError:
    pass


def _openai_key():
    return os.environ.get("OPEN_API_KEY") or os.environ.get("OPENAI_API_KEY")


def _skip_unless_enabled():
    if os.environ.get("GRIX_GPT_E2E") != "1":
        raise unittest.SkipTest("GRIX_GPT_E2E != 1 (真实 GPT 计费，默认跳过)")
    if not _infra_imported:
        raise unittest.SkipTest("livekit/nats 未安装")
    if not _openai_key():
        raise unittest.SkipTest("OPEN_API_KEY / OPENAI_API_KEY 未设置")


def _generate_token(identity, room):
    return (AccessToken(LK_API_KEY, LK_API_SECRET).with_identity(identity)
            .with_grants(VideoGrants(room_join=True, room=room,
                                     can_publish=True, can_subscribe=True)).to_jwt())


async def _check_alive():
    try:
        room = rtc.Room()
        await asyncio.wait_for(room.connect(LK_URL, _generate_token("hc", "hc")), timeout=5.0)
        await room.disconnect()
        nc = await asyncio.wait_for(nats.connect(NATS_URL), timeout=3.0)
        await nc.close()
        return True
    except Exception:
        return False


def _ensure_pcm():
    """返回 48kHz 单声道 s16le 中文语音 PCM。优先用提交的 fixture，缺失则 macOS say 生成。"""
    if os.path.exists(_PCM_PATH):
        with open(_PCM_PATH, "rb") as f:
            return f.read()
    if sys.platform != "darwin":
        raise unittest.SkipTest("缺音频 fixture 且非 macOS（无 say 可生成）")
    os.makedirs(os.path.dirname(_PCM_PATH), exist_ok=True)
    aiff = _PCM_PATH + ".aiff"
    try:
        subprocess.run(["say", "-v", "Tingting", "-o", aiff, _CALLER_TEXT], check=True)
        subprocess.run(["ffmpeg", "-y", "-i", aiff, "-ar", "48000", "-ac", "1",
                        "-f", "s16le", _PCM_PATH, "-loglevel", "error"], check=True)
    except (subprocess.CalledProcessError, FileNotFoundError) as e:
        raise unittest.SkipTest(f"音频生成失败（需 say + ffmpeg）：{e}")
    finally:
        if os.path.exists(aiff):
            os.remove(aiff)
    with open(_PCM_PATH, "rb") as f:
        return f.read()


def _frame_energy(data) -> int:
    a = array.array("h"); a.frombytes(bytes(data))
    return sum(abs(x) for x in a)


class _CallHarness:
    """in-process 起一通 openai 通话并驱动音频/采集结果。"""

    def __init__(self, call_id, caller_id=12345):
        self.call_id = call_id
        self.caller_id = caller_id
        self.room_name = f"call-{call_id}"
        self.caller_transcript = []
        self.ai_transcript = []
        self.ai_energy = 0
        self.nc = None
        self.caller_room = None
        self.source = None
        self.main = None
        self._audio_tasks = []

    async def start(self):
        import main as m
        self.main = m
        m._sessions.clear()
        m._openai_orchestrators.clear()

        self.nc = await nats.connect(NATS_URL)

        async def on_t(msg):
            d = json.loads(msg.data); r = d.get("speaker_role"); t = d.get("transcript_raw", "")
            (self.caller_transcript if r == "caller" else self.ai_transcript).append(t)
        await self.nc.subscribe(f"voicebridge.transcript.{self.call_id}", cb=on_t)

        # caller 进房并发布音频轨（身份须为 user-<caller_id>，bridge 据此监听）
        self.caller_room = rtc.Room()

        @self.caller_room.on("track_subscribed")
        def _ot(track, pub, p):
            if track.kind == rtc.TrackKind.KIND_AUDIO and p.identity.startswith("ai_bot"):
                async def _c():
                    async for ev in rtc.AudioStream(track):
                        self.ai_energy += _frame_energy(ev.frame.data)
                self._audio_tasks.append(asyncio.create_task(_c()))

        await self.caller_room.connect(LK_URL, _generate_token(f"user-{self.caller_id}", self.room_name))
        self.source = rtc.AudioSource(48000, 1)
        track = rtc.LocalAudioTrack.create_audio_track("mic", self.source)
        await self.caller_room.local_participant.publish_track(
            track, rtc.TrackPublishOptions(source=rtc.TrackSource.SOURCE_MICROPHONE))
        await asyncio.sleep(0.5)

        # in-process 起 bridge（真实 openai，不 mock）
        os.environ["LIVEKIT_URL"] = LK_URL
        os.environ["LIVEKIT_API_KEY"] = LK_API_KEY
        os.environ["LIVEKIT_API_SECRET"] = LK_API_SECRET
        payload = {
            "call_id": self.call_id, "session_id": f"e2e-{self.call_id}", "agent_id": 7,
            "caller_id": self.caller_id, "voice_provider": "openai_realtime", "model": MODEL,
            "endpoint": "", "api_key": _openai_key(), "system_prompt": _SYSTEM_PROMPT,
            "voice": "", "language": "zh-CN", "max_call_seconds": 120,
        }
        msg = _FakeMsg(json.dumps(payload).encode(), reply=f"_inbox.{self.call_id}")
        await m.start_bridge(msg, self.nc)
        await asyncio.sleep(1.5)
        return self.call_id in m._sessions

    @property
    def orchestrator(self):
        return self.main._openai_orchestrators.get(self.call_id)

    async def speak(self, pcm):
        fb = 480 * 2
        for i in range(0, len(pcm) - fb, fb):
            await self.source.capture_frame(rtc.AudioFrame(
                data=pcm[i:i+fb], sample_rate=48000, num_channels=1, samples_per_channel=480))
            await asyncio.sleep(0.01)
        sil = b"\x00\x00" * 480
        for _ in range(180):  # 1.8s 静音触发 end-of-turn
            await self.source.capture_frame(rtc.AudioFrame(
                data=sil, sample_rate=48000, num_channels=1, samples_per_channel=480))
            await asyncio.sleep(0.01)

    async def wait_for(self, predicate, timeout=12.0, interval=0.2):
        """轮询等待条件成立，避免依赖固定 sleep（异步事件到达时间不定）。"""
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if predicate():
                return True
            await asyncio.sleep(interval)
        return predicate()

    async def close(self):
        for t in self._audio_tasks:
            t.cancel()
        try:
            entry = self.main._sessions.pop(self.call_id, None)
            self.main._detach_openai_orchestrator(self.call_id)
            if entry:
                await asyncio.wait_for(entry["session"].aclose(), timeout=5.0)
        except Exception:
            pass
        try:
            await self.caller_room.disconnect()
        except Exception:
            pass
        if self.nc:
            await self.nc.close()


class _FakeMsg:
    def __init__(self, data, reply=None):
        self.data = data
        self.reply = reply


class GPTRealtimeE2ETest(unittest.IsolatedAsyncioTestCase):

    async def asyncSetUp(self):
        _skip_unless_enabled()
        if not await _check_alive():
            raise unittest.SkipTest("LiveKit/NATS 本机不可达")
        self.pcm = _ensure_pcm()

    async def test_full_loop(self):
        """连接→接点A 访客转写→语序编排器触发→GPT 出声+AI 转写→接点B 注入路由。"""
        h = _CallHarness(call_id=int(time.time() * 1000))
        try:
            self.assertTrue(await h.start(), "bridge session 应注册")
            self.assertIsNotNone(h.orchestrator, "openai 编排器应挂载")

            await h.speak(self.pcm)
            # 等编排器触发回复（race 超时→generate_reply）
            await h.wait_for(lambda: h.orchestrator and h.orchestrator._replied_round > 0)
            # 轮询等 GPT 出声 + AI 文本转写到达（异步，时间不定）
            await h.wait_for(lambda: h.ai_energy > 1_000_000 and len(h.ai_transcript) > 0, timeout=15.0)

            self.assertGreater(len(h.caller_transcript), 0, "接点A：访客转写应落地")
            self.assertGreater(h.orchestrator._replied_round, 0, "语序编排器应触发了 generate_reply")
            self.assertGreater(h.ai_energy, 1_000_000, "GPT 应实际出声（音频能量）")
            self.assertGreater(len(h.ai_transcript), 0, "AI 回复转写应落地")

            # 接点B：注入应路由到 openai 编排器且不报错
            seq = int(time.time() * 1000)
            await h.orchestrator.on_brain_inject("补充：本周六日全场茶水8折，到晚上10点闭店。", seq)
            await asyncio.sleep(1.0)
            # 不抛异常即通过（注入为滚动上下文）
        finally:
            await h.close()

    async def test_takeover_listen_only(self):
        """四档接管：mute 期编排器只听不说（不再触发回复），unmute 后恢复。"""
        h = _CallHarness(call_id=int(time.time() * 1000) + 1)
        try:
            self.assertTrue(await h.start())
            orch = h.orchestrator
            self.assertIsNotNone(orch)

            # turn1：正常应答
            await h.speak(self.pcm)
            for _ in range(120):
                if orch._replied_round > 0:
                    break
                await asyncio.sleep(0.1)
            await asyncio.sleep(4.0)
            replied_after_turn1 = orch._replied_round
            self.assertGreater(replied_after_turn1, 0, "turn1 应触发回复")

            # 接管：只听不说（input 不关、output 关、编排器静音）
            entry = h.main._sessions[h.call_id]
            await h.main._openai_set_takeover(entry, h.call_id, True)
            self.assertTrue(orch._muted, "编排器应进入静音")
            self.assertTrue(entry.get("muted"))

            caller_before = len(h.caller_transcript)
            await h.speak(self.pcm)  # turn2：用户说话，GPT 应静默
            await asyncio.sleep(3.0)
            self.assertEqual(orch._replied_round, replied_after_turn1,
                             "接管期不得再触发回复（只听不说）")
            self.assertGreater(len(h.caller_transcript), caller_before,
                               "接管期仍应在听（访客转写继续）")

            # 交回：恢复
            await h.main._openai_set_takeover(entry, h.call_id, False)
            self.assertFalse(orch._muted)
            await h.speak(self.pcm)  # turn3：恢复应答
            for _ in range(120):
                if orch._replied_round > replied_after_turn1:
                    break
                await asyncio.sleep(0.1)
            self.assertGreater(orch._replied_round, replied_after_turn1,
                               "交回后应恢复触发回复")
        finally:
            await h.close()


if __name__ == "__main__":
    unittest.main(verbosity=2)

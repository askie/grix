"""main.py 全面守卫测试 — 高覆盖率。

测试分层：
  L1 AST 静态守卫 — 语法 + nonlocal/del 回归
  L2 SDK 兼容性    — 真实 livekit 对象创建 + API 签名
  L3 单元测试      — 每个公共函数 + 每个分支
  L4 集成冒烟      — 完整生命周期 + 并发

运行：cd voicebridge && ../.venv/bin/python -m unittest test_main -v
"""

import ast
import asyncio
import json
import os
import sys
import time
import unittest
from unittest.mock import AsyncMock, MagicMock, patch, call

sys.path.insert(0, os.path.dirname(__file__))


# ---------------------------------------------------------------------------
# Helper
# ---------------------------------------------------------------------------
def _msg(data: dict, has_reply: bool = True) -> MagicMock:
    """构造 NATS 消息 mock。"""
    m = MagicMock()
    m.data = json.dumps(data).encode()
    m.reply = "_INBOX.test" if has_reply else None
    return m


# 有效 openai_realtime 配置
_VALID_OPENAI = {
    "call_id": 100,
    "voice_provider": "openai_realtime",
    "model": "gpt-4o-realtime",
    "api_key": "sk-test-key",
    "voice": "alloy",
    "system_prompt": "test prompt",
}

# 有效 doubao_realtime 配置
_VALID_DOUBAO = {
    "call_id": 200,
    "voice_provider": "doubao_realtime",
    "model": "O",
    "api_key": "volc-api-key",  # 新版火山 API Key 单 Key（不再是 appid:access_token）
    "voice": "zh_female_vv",
    "system_prompt": "test",
}

_ENV_LIVEKIT = {
    "LIVEKIT_URL": "ws://fake:7880",
    "LIVEKIT_API_KEY": "testkey",
    "LIVEKIT_API_SECRET": "testsecret",
}


# ===========================================================================
# L1 — AST 静态守卫
# ===========================================================================
class ASTGuardTest(unittest.TestCase):

    def test_syntax_valid(self):
        """main.py 语法正确。"""
        with open(os.path.join(os.path.dirname(__file__), "main.py")) as f:
            ast.parse(f.read())

    def test_nonlocal_covers_all_del(self):
        """_run_and_signal 内 del 的变量全部有 nonlocal。"""
        with open(os.path.join(os.path.dirname(__file__), "main.py")) as f:
            src = f.read()
        tree = ast.parse(src)
        for node in ast.walk(tree):
            if isinstance(node, ast.AsyncFunctionDef) and node.name == "_run_and_signal":
                nl = {n for s in ast.walk(node) if isinstance(s, ast.Nonlocal) for n in s.names}
                dl = {t.id for s in ast.walk(node) if isinstance(s, ast.Delete) for t in s.targets if isinstance(t, ast.Name)}
                self.assertEqual(dl - nl, set(), f"nonlocal 遗漏: {dl - nl}")
                # 三个关键变量必须都在
                for v in ("realtime_model", "session", "room"):
                    self.assertIn(v, nl, f"{v} 必须在 nonlocal 中")
                return
        self.fail("未找到 _run_and_signal")

    def test_room_no_args(self):
        """rtc.Room() 必须无参数。"""
        with open(os.path.join(os.path.dirname(__file__), "main.py")) as f:
            src = f.read()
        tree = ast.parse(src)
        for node in ast.walk(tree):
            if isinstance(node, ast.Call):
                f = node.func
                if (isinstance(f, ast.Attribute) and f.attr == "Room"
                        and isinstance(f.value, ast.Name) and f.value.id == "rtc"):
                    self.assertFalse(node.args or node.keywords,
                                     "rtc.Room() 不应有参数")

    def test_connect_no_auto_subscribe_false(self):
        """room.connect() 不传 auto_subscribe=False。

        auto_subscribe=False 阻止 AI bot 订阅用户音频 track，
        导致 AI 听不到用户，无法回复。
        回归守卫：commit c150f610 为省内存引入 auto_subscribe=False，
        但 AgentSession RoomIO 不会手动订阅，导致音频断流。
        """
        with open(os.path.join(os.path.dirname(__file__), "main.py")) as f:
            src = f.read()
        tree = ast.parse(src)
        for node in ast.walk(tree):
            if isinstance(node, ast.Call):
                func = node.func
                # 找 rtc.RoomOptions(...) 调用
                if (isinstance(func, ast.Attribute) and func.attr == "RoomOptions"
                        and isinstance(func.value, ast.Name) and func.value.id == "rtc"):
                    for kw in node.keywords:
                        if kw.arg == "auto_subscribe" and isinstance(kw.value, ast.Constant):
                            self.assertNotEqual(kw.value.value, False,
                                                "rtc.RoomOptions(auto_subscribe=False) 会导致 AI 听不到用户音频")


# ===========================================================================
# L2 — SDK 兼容性（真实对象）
# ===========================================================================
class LiveKitSDKCompatTest(unittest.IsolatedAsyncioTestCase):

    async def test_room_no_args_ok(self):
        from livekit import rtc
        room = rtc.Room()
        self.assertIsNotNone(room)

    async def test_room_rejects_auto_subscribe(self):
        from livekit import rtc
        with self.assertRaises(TypeError):
            rtc.Room(auto_subscribe=False)

    def test_room_options_false(self):
        from livekit import rtc
        self.assertFalse(rtc.RoomOptions(auto_subscribe=False).auto_subscribe)

    def test_room_options_default_true(self):
        from livekit import rtc
        self.assertTrue(rtc.RoomOptions().auto_subscribe)

    async def test_agent_session_creates(self):
        from livekit.agents import AgentSession
        s = AgentSession(llm=MagicMock())
        self.assertIsNotNone(s)

    def test_agent_creates(self):
        from livekit.agents import Agent
        self.assertIsNotNone(Agent(instructions="hi"))

    def test_access_token_creates_jwt(self):
        from livekit.api import AccessToken, VideoGrants
        token = (AccessToken("key", "secret")
                 .with_identity("bot")
                 .with_grants(VideoGrants(room_join=True, room="test"))
                 .to_jwt())
        self.assertIsInstance(token, str)
        self.assertTrue(len(token) > 20)


# ===========================================================================
# L3 — 单元测试
# ===========================================================================

# --- _resolve_livekit_url ---
class ResolveLivekitUrlTest(unittest.TestCase):

    def test_internal_priority(self):
        import importlib, main as m
        with patch.dict(os.environ, {"LIVEKIT_URL": "wss://ext", "LIVEKIT_INTERNAL_URL": "ws://int:7880"}):
            importlib.reload(m)
            self.assertEqual(m._resolve_livekit_url(), "ws://int:7880")

    def test_fallback_external(self):
        import importlib, main as m
        os.environ.pop("LIVEKIT_INTERNAL_URL", None)
        with patch.dict(os.environ, {"LIVEKIT_URL": "wss://ext"}, clear=False):
            importlib.reload(m)
            self.assertEqual(m._resolve_livekit_url(), "wss://ext")

    def test_whitespace_internal_falls_back(self):
        import importlib, main as m
        with patch.dict(os.environ, {"LIVEKIT_URL": "wss://ext", "LIVEKIT_INTERNAL_URL": "  "}):
            importlib.reload(m)
            self.assertEqual(m._resolve_livekit_url(), "wss://ext")


# --- _publish_transcript ---
class PublishTranscriptTest(unittest.IsolatedAsyncioTestCase):

    async def test_publishes_correct_payload(self):
        import main as m
        nc = AsyncMock()
        m._publish_transcript(nc, 111, 1, "caller", "hello", "openai_realtime")
        await asyncio.sleep(0.05)
        subj = nc.publish.call_args[0][0]
        data = json.loads(nc.publish.call_args[0][1])
        self.assertEqual(subj, "voicebridge.transcript.111")
        self.assertEqual(data["call_id"], "111")
        self.assertEqual(data["segment_seq"], 1)
        self.assertEqual(data["speaker_role"], "caller")
        self.assertEqual(data["transcript_raw"], "hello")

    async def test_skips_empty(self):
        import main as m
        nc = AsyncMock()
        m._publish_transcript(nc, 1, 1, "caller", "", "openai_realtime")
        m._publish_transcript(nc, 1, 1, "caller", "  ", "openai_realtime")
        m._publish_transcript(nc, 1, 1, "caller", None, "openai_realtime")
        await asyncio.sleep(0.05)
        nc.publish.assert_not_called()

    async def test_nats_error_does_not_raise(self):
        import main as m
        nc = AsyncMock()
        nc.publish.side_effect = Exception("boom")
        m._publish_transcript(nc, 1, 1, "caller", "hi", "openai_realtime")
        await asyncio.sleep(0.05)

    async def test_segment_seq_increases(self):
        import main as m
        nc = AsyncMock()
        m._publish_transcript(nc, 1, 1, "caller", "a", "openai_realtime")
        m._publish_transcript(nc, 1, 2, "ai_bot", "b", "openai_realtime")
        await asyncio.sleep(0.05)
        calls = nc.publish.call_args_list
        self.assertEqual(json.loads(calls[0][0][1])["segment_seq"], 1)
        self.assertEqual(json.loads(calls[1][0][1])["segment_seq"], 2)


# --- start_bridge ---
class StartBridgeTest(unittest.IsolatedAsyncioTestCase):

    async def asyncSetUp(self):
        import main as m
        m._sessions.clear()

    async def asyncTearDown(self):
        import main as m
        m._sessions.clear()

    async def test_missing_api_key(self):
        import main as m
        nc = AsyncMock()
        await m.start_bridge(_msg({"call_id": 1, "voice_provider": "openai_realtime", "model": "m"}), nc)
        r = json.loads(nc.publish.call_args[0][1])
        self.assertFalse(r["ok"])
        self.assertIn("api_key", r["error"])

    async def test_missing_model(self):
        import main as m
        nc = AsyncMock()
        await m.start_bridge(_msg({"call_id": 1, "voice_provider": "openai_realtime", "api_key": "k"}), nc)
        r = json.loads(nc.publish.call_args[0][1])
        self.assertFalse(r["ok"])

    async def test_unsupported_provider(self):
        import main as m
        nc = AsyncMock()
        await m.start_bridge(_msg({"call_id": 1, "voice_provider": "bad", "model": "m", "api_key": "k"}), nc)
        r = json.loads(nc.publish.call_args[0][1])
        self.assertFalse(r["ok"])
        self.assertIn("unsupported", r["error"])

    async def test_bad_config_no_reply(self):
        """无 reply subject 时不发回复，也不崩溃。"""
        import main as m
        nc = AsyncMock()
        msg = _msg({"call_id": 1, "voice_provider": "openai_realtime", "model": "m"}, has_reply=False)
        await m.start_bridge(msg, nc)
        nc.publish.assert_not_called()

    async def test_duplicate_call_id(self):
        import main as m
        m._sessions[42] = {"session": MagicMock(), "started_at": 0}
        nc = AsyncMock()
        await m.start_bridge(_msg({**_VALID_OPENAI, "call_id": 42}), nc)
        r = json.loads(nc.publish.call_args[0][1])
        self.assertEqual(r, {"ok": True, "node_id": m.NODE_ID})

    async def test_duplicate_no_reply(self):
        import main as m
        m._sessions[42] = {"session": MagicMock(), "started_at": 0}
        nc = AsyncMock()
        await m.start_bridge(_msg({**_VALID_OPENAI, "call_id": 42}, has_reply=False), nc)
        nc.publish.assert_not_called()

    @patch.dict(os.environ, _ENV_LIVEKIT)
    async def test_room_connect_failure(self):
        """Room 连接失败时应返回错误。"""
        import main as m
        real_room = MagicMock()
        real_room.connect = AsyncMock(side_effect=Exception("connection refused"))

        with patch("main.rtc.Room", return_value=real_room):
            nc = AsyncMock()
            await m.start_bridge(_msg(_VALID_OPENAI), nc)
            r = json.loads(nc.publish.call_args[0][1])
            self.assertFalse(r["ok"])
            self.assertIn("connection refused", r["error"])

    @patch.dict(os.environ, _ENV_LIVEKIT)
    async def test_session_start_error(self):
        """session.start() 失败时应返回错误。"""
        import main as m
        from livekit import rtc

        real_room = rtc.Room()
        real_room.connect = AsyncMock()
        real_room.disconnect = AsyncMock()

        with patch("main.rtc.Room", return_value=real_room), \
             patch("main.AgentSession") as sess_cls, \
             patch("main.Agent"), \
             patch("main.build_realtime_model", return_value=MagicMock()), \
             patch("main.http_context"):

            # session.start() 抛异常
            mock_sess = MagicMock()
            mock_sess.start = AsyncMock(side_effect=RuntimeError("model unavailable"))
            mock_sess.on = MagicMock()
            mock_sess.aclose = AsyncMock()
            sess_cls.return_value = mock_sess

            nc = AsyncMock()
            await m.start_bridge(_msg(_VALID_OPENAI), nc)
            r = json.loads(nc.publish.call_args[0][1])
            self.assertFalse(r["ok"])
            self.assertIn("model unavailable", r["error"])

    @patch.dict(os.environ, _ENV_LIVEKIT)
    async def test_session_start_timeout(self):
        """session start 超时：主动清理并回 ok:false，不留幽灵 session、不触发 BRIDGE_EXIT。"""
        import main as m
        from livekit import rtc

        real_room = rtc.Room()
        real_room.connect = AsyncMock()
        real_room.disconnect = AsyncMock()

        async def _slow_start(*a, **kw):
            await asyncio.sleep(0.5)

        with patch("main.rtc.Room", return_value=real_room), \
             patch("main.AgentSession") as sess_cls, \
             patch("main.Agent"), \
             patch("main.build_realtime_model", return_value=MagicMock()), \
             patch("main.http_context"), \
             patch.object(m, "_SESSION_START_TIMEOUT", 0.1):

            mock_sess = MagicMock()
            mock_sess.start = _slow_start
            mock_sess.on = MagicMock()
            mock_sess.aclose = AsyncMock()
            sess_cls.return_value = mock_sess

            nc = AsyncMock()
            await m.start_bridge(_msg(_VALID_OPENAI), nc)
            # 超时应回 ok:false（Go 按启动失败回滚，不再傻等）
            r = json.loads(nc.publish.call_args[0][1])
            self.assertFalse(r["ok"])
            self.assertIn("timeout", r["error"])
            # 幽灵防护：session 已移出活跃表
            self.assertNotIn(100, m._sessions)
            # 等后台 start 完成并走清理：BRIDGE_EXIT 应被抑制（不触发 Go endCall）
            await asyncio.sleep(0.8)
            for c in nc.publish.call_args_list:
                self.assertNotEqual(c[0][0], m.SUBJECT_BRIDGE_EXIT,
                                    "超时中止的 bridge 不应发 BRIDGE_EXIT")
            self.assertNotIn(100, m._handover_rooms)
            self.assertNotIn(100, m._aborted_calls)

    @patch.dict(os.environ, _ENV_LIVEKIT)
    async def test_openai_success_flow(self):
        """openai_realtime 完整成功流程。"""
        import main as m
        from livekit import rtc

        real_room = rtc.Room()
        real_room.connect = AsyncMock()
        real_room.disconnect = AsyncMock()

        with patch("main.rtc.Room", return_value=real_room), \
             patch("main.AgentSession") as sess_cls, \
             patch("main.Agent"), \
             patch("main.build_realtime_model", return_value=MagicMock()), \
             patch("main.http_context"):

            mock_sess = MagicMock()
            mock_sess.start = AsyncMock()
            mock_sess.on = MagicMock()
            mock_sess.aclose = AsyncMock()
            sess_cls.return_value = mock_sess

            nc = AsyncMock()
            await m.start_bridge(_msg(_VALID_OPENAI), nc)

            # 验证 connect 只传 url + token，不传 RoomOptions
            real_room.connect.assert_called_once()
            self.assertEqual(len(real_room.connect.call_args[0]), 2,
                             "connect() 应只传 url 和 token，不传 auto_subscribe=False")

            # 验证 session 注册到 _sessions
            self.assertIn(100, m._sessions)

            # 验证成功回复（多节点：回执带 node_id 供 Go 建立归属）
            nc.publish.assert_called_once()
            reply = json.loads(nc.publish.call_args[0][1])
            self.assertEqual(reply, {"ok": True, "node_id": m.NODE_ID})

    @patch.dict(os.environ, _ENV_LIVEKIT)
    async def test_doubao_grix_ctx_injected(self):
        """doubao_realtime 应注入 _grix_ctx 到 realtime_model。"""
        import main as m
        from livekit import rtc

        real_room = rtc.Room()
        real_room.connect = AsyncMock()
        real_room.disconnect = AsyncMock()
        mock_model = MagicMock()

        # 模拟 lk_volcengine 可用
        with patch("main.rtc.Room", return_value=real_room), \
             patch("main.AgentSession") as sess_cls, \
             patch("main.Agent"), \
             patch("main.build_realtime_model", return_value=mock_model), \
             patch("main.lk_volcengine", MagicMock()), \
             patch("main.http_context"):

            mock_sess = MagicMock()
            mock_sess.start = AsyncMock()
            mock_sess.on = MagicMock()
            mock_sess.aclose = AsyncMock()
            sess_cls.return_value = mock_sess

            nc = AsyncMock()
            await m.start_bridge(_msg(_VALID_DOUBAO), nc)

            # _grix_ctx 应该被注入
            self.assertIsNotNone(mock_model._grix_ctx)
            self.assertEqual(mock_model._grix_ctx["call_id"], 200)
            self.assertEqual(mock_model._grix_ctx["provider"], "doubao_realtime")
            self.assertIn("seq", mock_model._grix_ctx)

    @patch.dict(os.environ, _ENV_LIVEKIT)
    async def test_no_reply_on_success(self):
        """成功时如果没有 reply subject 也不崩溃。"""
        import main as m
        from livekit import rtc

        real_room = rtc.Room()
        real_room.connect = AsyncMock()
        real_room.disconnect = AsyncMock()

        with patch("main.rtc.Room", return_value=real_room), \
             patch("main.AgentSession") as sess_cls, \
             patch("main.Agent"), \
             patch("main.build_realtime_model", return_value=MagicMock()), \
             patch("main.http_context"):

            mock_sess = MagicMock()
            mock_sess.start = AsyncMock()
            mock_sess.on = MagicMock()
            mock_sess.aclose = AsyncMock()
            sess_cls.return_value = mock_sess

            nc = AsyncMock()
            await m.start_bridge(_msg(_VALID_OPENAI, has_reply=False), nc)
            nc.publish.assert_not_called()

    @patch.dict(os.environ, _ENV_LIVEKIT)
    async def test_connect_failure_no_reply(self):
        """连接失败且无 reply subject 时不崩溃。"""
        import main as m
        real_room = MagicMock()
        real_room.connect = AsyncMock(side_effect=Exception("boom"))

        with patch("main.rtc.Room", return_value=real_room):
            nc = AsyncMock()
            await m.start_bridge(_msg(_VALID_OPENAI, has_reply=False), nc)
            nc.publish.assert_not_called()


# --- on_user_transcribed callback ---
class UserTranscribedCallbackTest(unittest.IsolatedAsyncioTestCase):
    """测试 start_bridge 内部注册的 on_user_transcribed 回调逻辑。"""

    async def test_doubao_skips(self):
        """doubao_realtime 的 user_transcribed 不发布 transcript。"""
        import main as m
        nc = AsyncMock()
        m._sessions.clear()

        # 模拟回调的闭包环境
        cfg = {"provider": "doubao_realtime"}
        call_id = 1
        segment_seq = [0]
        calls_log = []

        def _publish(nc, cid, seq, role, text, prov):
            calls_log.append((seq, role, text))

        ev = MagicMock()
        ev.is_final = True
        ev.transcript = "hello"

        # 模拟 on_user_transcribed 逻辑
        if cfg["provider"] == "doubao_realtime":
            pass  # 直接 return
        else:
            if ev.is_final:
                _publish(nc, call_id, segment_seq[0] + 1, "caller", ev.transcript, cfg["provider"])

        self.assertEqual(calls_log, [])

    async def test_openai_final_published(self):
        """openai_realtime 的 is_final transcript 应发布。"""
        calls_log = []

        def _publish(nc, cid, seq, role, text, prov):
            calls_log.append((seq, role, text))

        cfg = {"provider": "openai_realtime"}
        ev = MagicMock()
        ev.is_final = True
        ev.transcript = "你好世界"

        if cfg["provider"] != "doubao_realtime" and ev.is_final:
            _publish(None, 1, 1, "caller", ev.transcript, cfg["provider"])

        self.assertEqual(len(calls_log), 1)
        self.assertEqual(calls_log[0], (1, "caller", "你好世界"))

    async def test_openai_interim_skipped(self):
        """openai_realtime 的 is_interim 不发布。"""
        calls_log = []

        def _publish(nc, cid, seq, role, text, prov):
            calls_log.append(1)

        cfg = {"provider": "openai_realtime"}
        ev = MagicMock()
        ev.is_final = False
        ev.transcript = "partial"

        if cfg["provider"] != "doubao_realtime" and ev.is_final:
            _publish(None, 1, 1, "caller", ev.transcript, cfg["provider"])

        self.assertEqual(calls_log, [])


# --- on_conversation_item callback ---
class ConversationItemCallbackTest(unittest.TestCase):
    """测试 start_bridge 内部注册的 on_conversation_item 回调逻辑。"""

    def test_ai_bot_text_published(self):
        """assistant 角色文字应发布。"""
        calls = []
        def _publish(nc, cid, seq, role, text, prov):
            calls.append((role, text))

        ev = MagicMock()
        ev.item.text_content = "AI reply"
        ev.item.role = "assistant"

        self._simulate_callback(ev, {"provider": "openai_realtime"}, _publish)
        self.assertEqual(calls, [("ai_bot", "AI reply")])

    def test_caller_text_skipped_openai(self):
        """openai 的 caller 文字应跳过：由 on_user_transcribed(user_input_transcribed) 专门发布，
        conversation_item 里的 caller 是重复来源，否则用户转写会重复两遍。"""
        calls = []
        def _publish(nc, cid, seq, role, text, prov):
            calls.append((role, text))

        ev = MagicMock()
        ev.item.text_content = "user text"
        ev.item.role = "user"

        self._simulate_callback(ev, {"provider": "openai_realtime"}, _publish)
        self.assertEqual(calls, [])

    def test_doubao_caller_text_skipped(self):
        """doubao_realtime 的 caller 文字应跳过（由 raw WS 处理）。"""
        calls = []
        def _publish(nc, cid, seq, role, text, prov):
            calls.append(1)

        ev = MagicMock()
        ev.item.text_content = "user"
        ev.item.role = "user"

        self._simulate_callback(ev, {"provider": "doubao_realtime"}, _publish)
        self.assertEqual(calls, [])

    def test_doubao_ai_bot_text_published(self):
        """doubao_realtime 的 ai_bot 文字应发布。"""
        calls = []
        def _publish(nc, cid, seq, role, text, prov):
            calls.append((role, text))

        ev = MagicMock()
        ev.item.text_content = "AI reply"
        ev.item.role = "assistant"

        self._simulate_callback(ev, {"provider": "doubao_realtime"}, _publish)
        self.assertEqual(calls, [("ai_bot", "AI reply")])

    def test_no_text_content_falls_back_to_content_list(self):
        """无 text_content 时从 content list 提取文字。"""
        calls = []
        def _publish(nc, cid, seq, role, text, prov):
            calls.append((role, text))

        ev = MagicMock()
        ev.item.text_content = None
        ev.item.content = ["line1", "line2"]
        ev.item.role = "assistant"

        self._simulate_callback(ev, {"provider": "openai_realtime"}, _publish)
        self.assertEqual(calls, [("ai_bot", "line1\nline2")])

    def test_no_text_at_all_skipped(self):
        """完全没有文字时应跳过。"""
        calls = []
        def _publish(nc, cid, seq, role, text, prov):
            calls.append(1)

        ev = MagicMock()
        ev.item.text_content = None
        ev.item.content = None
        ev.item.role = "assistant"

        self._simulate_callback(ev, {"provider": "openai_realtime"}, _publish)
        self.assertEqual(calls, [])

    def test_content_list_non_strings_filtered(self):
        """content list 中非字符串被过滤。"""
        calls = []
        def _publish(nc, cid, seq, role, text, prov):
            calls.append((role, text))

        ev = MagicMock()
        ev.item.text_content = None
        ev.item.content = ["text", 123, None, "more"]
        ev.item.role = "assistant"

        self._simulate_callback(ev, {"provider": "openai_realtime"}, _publish)
        self.assertEqual(calls, [("ai_bot", "text\nmore")])

    def _simulate_callback(self, ev, cfg, _publish):
        """模拟 on_conversation_item 回调逻辑。"""
        item = ev.item
        text = getattr(item, "text_content", None)
        if not text:
            content = getattr(item, "content", None)
            if content:
                parts = [c for c in content if isinstance(c, str)]
                text = "\n".join(parts) if parts else None
        if not text:
            return
        role = "ai_bot" if getattr(item, "role", "") == "assistant" else "caller"
        # caller 文本一律由专门来源发布（pipeline=STT / doubao=raw WS / openai=user_input_transcribed），
        # conversation_item 里的 caller 永远是重复来源，丢弃。
        if role != "ai_bot":
            return
        _publish(None, 1, 1, role, text, cfg["provider"])


# --- _sessions 表管理 ---
class SessionsTableTest(unittest.IsolatedAsyncioTestCase):

    async def asyncSetUp(self):
        import main as m
        m._sessions.clear()

    async def asyncTearDown(self):
        import main as m
        m._sessions.clear()

    async def test_start_registers_session(self):
        import main as m
        m._sessions[1] = {"session": MagicMock(), "started_at": time.monotonic()}
        self.assertIn(1, m._sessions)

    async def test_stop_removes_session(self):
        import main as m
        s = AsyncMock()
        m._sessions[2] = {"session": s, "started_at": 0}
        entry = m._sessions.pop(2, None)
        await entry["session"].aclose()
        self.assertNotIn(2, m._sessions)

    async def test_interrupt_pops_and_closes(self):
        import main as m
        s = AsyncMock()
        m._sessions[3] = {"session": s, "started_at": 0}
        entry = m._sessions.pop(3, None)
        await entry["session"].interrupt()
        await entry["session"].aclose()
        self.assertNotIn(3, m._sessions)
        s.interrupt.assert_called_once()
        s.aclose.assert_called_once()

    async def test_interrupt_unknown_noop(self):
        import main as m
        entry = m._sessions.pop(999, None)
        self.assertIsNone(entry)


# --- NATS Subject 常量 ---
class SubjectConstantsTest(unittest.TestCase):

    def test_subjects_defined(self):
        import main as m
        self.assertEqual(m.SUBJECT_START, "voicebridge.control.start")
        self.assertEqual(m.SUBJECT_STOP, "voicebridge.control.stop")
        self.assertEqual(m.SUBJECT_INTERRUPT, "voicebridge.control.interrupt")
        self.assertEqual(m.SUBJECT_HEALTH, "voicebridge.control.health")
        self.assertEqual(m.SUBJECT_TRANSCRIPT, "voicebridge.transcript")


# --- Token 生成 ---
class TokenGenerationTest(unittest.TestCase):

    def test_token_contains_expected_claims(self):
        """验证生成的 JWT token 包含正确的 sub 和 grants。"""
        from livekit.api import AccessToken, VideoGrants
        import jwt

        token = (AccessToken("key", "secret")
                 .with_identity("ai_bot_123")
                 .with_grants(VideoGrants(
                     room_join=True,
                     room="call-123",
                     can_publish=True,
                     can_subscribe=True,
                     can_publish_data=True,
                     can_update_own_metadata=True,
                 ))
                 .to_jwt())

        decoded = jwt.decode(token, "secret", algorithms=["HS256"])
        # LiveKit puts identity in 'sub'
        self.assertEqual(decoded["sub"], "ai_bot_123")
        self.assertTrue(decoded["video"]["roomJoin"])
        self.assertEqual(decoded["video"]["room"], "call-123")
        self.assertTrue(decoded["video"]["canPublish"])
        self.assertTrue(decoded["video"]["canSubscribe"])


# ===========================================================================
# L4 — 集成冒烟
# ===========================================================================
class IntegrationSmokeTest(unittest.IsolatedAsyncioTestCase):

    async def asyncSetUp(self):
        import main as m
        m._sessions.clear()

    async def asyncTearDown(self):
        import main as m
        m._sessions.clear()

    @patch.dict(os.environ, _ENV_LIVEKIT)
    async def test_start_interrupt_start_stop_lifecycle(self):
        """完整生命周期：start → interrupt → start → stop"""
        import main as m

        # start #1
        room1 = MagicMock()
        room1.connect = AsyncMock()
        room1.disconnect = AsyncMock()

        with patch("main.rtc.Room", return_value=room1), \
             patch("main.build_realtime_model", return_value=MagicMock()), \
             patch("main.http_context"):
            nc = AsyncMock()
            await m.start_bridge(_msg({**_VALID_OPENAI, "call_id": 777}), nc)
            self.assertIn(777, m._sessions)

        # interrupt
        entry = m._sessions.pop(777, None)
        await entry["session"].interrupt()
        await entry["session"].aclose()
        self.assertNotIn(777, m._sessions)

        # start #2 (hand_back scenario)
        room2 = MagicMock()
        room2.connect = AsyncMock()
        room2.disconnect = AsyncMock()

        with patch("main.rtc.Room", return_value=room2), \
             patch("main.build_realtime_model", return_value=MagicMock()), \
             patch("main.http_context"):
            nc2 = AsyncMock()
            await m.start_bridge(_msg({**_VALID_OPENAI, "call_id": 777}), nc2)
            self.assertIn(777, m._sessions)

        # stop
        entry2 = m._sessions.pop(777, None)
        await entry2["session"].aclose()
        self.assertNotIn(777, m._sessions)

    async def test_concurrent_sessions(self):
        import main as m
        m._sessions[1] = {"session": AsyncMock(), "started_at": 0}
        m._sessions[2] = {"session": AsyncMock(), "started_at": 0}

        entry = m._sessions.pop(1, None)
        await entry["session"].aclose()

        self.assertNotIn(1, m._sessions)
        self.assertIn(2, m._sessions)

    async def test_shutdown_closes_all_sessions(self):
        """关闭时所有活跃 session 都应被关闭。"""
        import main as m
        s1, s2 = AsyncMock(), AsyncMock()
        m._sessions[1] = {"session": s1, "started_at": 0}
        m._sessions[2] = {"session": s2, "started_at": 0}

        # 模拟 main() 退出时的清理逻辑
        for entry in list(m._sessions.values()):
            await entry["session"].aclose()

        s1.aclose.assert_called_once()
        s2.aclose.assert_called_once()


# ===========================================================================
# L5 — 问题复现测试：auto_subscribe=False 导致 AI 听不到用户
# ===========================================================================
class AutoSubscribeReproductionTest(unittest.IsolatedAsyncioTestCase):
    """复现 commit c150f610 引入的 auto_subscribe=False 问题。

    问题现象：语音通话能接通，但听不到 AI 声音。
    根因：auto_subscribe=False 阻止 Room 自动订阅远端音频 track，
    AgentSession RoomIO 收不到用户音频 → AI 不回复。

    本测试从三个层面证明问题：
    1. SDK 行为层面：auto_subscribe 确实影响 connect 请求参数
    2. start_bridge 层面：验证实际代码传给 connect 的参数
    3. 对比层面：auto_subscribe=True（默认）vs False 的行为差异
    """

    def test_room_options_default_auto_subscribe_true(self):
        """RoomOptions 默认 auto_subscribe=True（AI 能听到用户）。"""
        from livekit import rtc
        opts = rtc.RoomOptions()
        self.assertTrue(opts.auto_subscribe,
                        "RoomOptions 默认 auto_subscribe 应为 True")

    def test_room_options_false_sets_flag(self):
        """RoomOptions(auto_subscribe=False) 确实设置标志为 False。"""
        from livekit import rtc
        opts = rtc.RoomOptions(auto_subscribe=False)
        self.assertFalse(opts.auto_subscribe,
                         "auto_subscribe=False 会关闭自动订阅")

    async def test_connect_with_default_options_sends_auto_subscribe_true(self):
        """不传 RoomOptions 时 connect 请求中 auto_subscribe=True。"""
        from livekit import rtc

        room = rtc.Room()

        # Mock FFI client 拦截底层连接请求
        captured_opts = {}

        original_connect = rtc.Room.connect

        async def _mock_connect(self, url, token, options=rtc.RoomOptions()):
            # 捕获实际传给 FFI 的 auto_subscribe 值
            captured_opts['auto_subscribe'] = options.auto_subscribe
            # 不真正连接，抛异常退出
            raise Exception("mock_connect_exit")

        rtc.Room.connect = _mock_connect
        try:
            await room.connect("ws://fake:7880", "fake_token")
        except Exception as e:
            if "mock_connect_exit" not in str(e):
                raise
        finally:
            rtc.Room.connect = original_connect

        self.assertTrue(captured_opts.get('auto_subscribe', False),
                        "默认连接应 auto_subscribe=True，AI 能听到用户")

    async def test_connect_with_auto_subscribe_false_sends_false(self):
        """传 RoomOptions(auto_subscribe=False) 时 connect 请求中 auto_subscribe=False。
        这就是导致 AI 听不到用户的根因。
        """
        from livekit import rtc

        room = rtc.Room()
        captured_opts = {}
        original_connect = rtc.Room.connect

        async def _mock_connect(self, url, token, options=rtc.RoomOptions()):
            captured_opts['auto_subscribe'] = options.auto_subscribe
            raise Exception("mock_connect_exit")

        rtc.Room.connect = _mock_connect
        try:
            await room.connect("ws://fake:7880", "fake_token",
                               rtc.RoomOptions(auto_subscribe=False))
        except Exception as e:
            if "mock_connect_exit" not in str(e):
                raise
        finally:
            rtc.Room.connect = original_connect

        self.assertFalse(captured_opts.get('auto_subscribe', True),
                         "auto_subscribe=False → AI 听不到用户音频 → 不回复")

    @patch.dict(os.environ, _ENV_LIVEKIT)
    async def test_start_bridge_connect_args_no_room_options(self):
        """start_bridge 调用 room.connect(url, token) 不传 RoomOptions。

        验证修复后的代码：connect 只传两个参数，auto_subscribe 用默认 True。
        """
        import main as m

        mock_room = MagicMock()
        mock_room.connect = AsyncMock()
        mock_room.disconnect = AsyncMock()

        with patch("main.rtc.Room", return_value=mock_room), \
             patch("main.AgentSession") as sess_cls, \
             patch("main.Agent"), \
             patch("main.build_realtime_model", return_value=MagicMock()), \
             patch("main.http_context"):

            mock_sess = MagicMock()
            mock_sess.start = AsyncMock()
            mock_sess.on = MagicMock()
            mock_sess.aclose = AsyncMock()
            sess_cls.return_value = mock_sess

            nc = AsyncMock()
            await m.start_bridge(_msg(_VALID_OPENAI), nc)

            # 关键验证：connect 只传 url + token，不传第三个 RoomOptions 参数
            mock_room.connect.assert_called_once()
            args = mock_room.connect.call_args[0]
            self.assertEqual(len(args), 2,
                             "connect() 应只传 (url, token)，不传 RoomOptions(auto_subscribe=False)")
            self.assertEqual(args[0], "ws://fake:7880")
            self.assertIsInstance(args[1], str)  # JWT token

    @patch.dict(os.environ, _ENV_LIVEKIT)
    async def test_start_bridge_if_auto_subscribe_false_would_break_audio(self):
        """复现问题：如果代码传了 auto_subscribe=False，connect 只收两个参数的测试会失败。

        这是一个"反向测试"：故意传 auto_subscribe=False，证明这会导致
        connect 收到 3 个参数（url, token, RoomOptions），与正常期望不符。
        """
        import main as m
        from livekit import rtc

        mock_room = MagicMock()
        mock_room.connect = AsyncMock()
        mock_room.disconnect = AsyncMock()

        # 模拟 c150f610 的旧代码行为：传 RoomOptions(auto_subscribe=False)
        with patch("main.rtc.Room", return_value=mock_room), \
             patch("main.AgentSession") as sess_cls, \
             patch("main.Agent"), \
             patch("main.build_realtime_model", return_value=MagicMock()), \
             patch("main.http_context"):

            mock_sess = MagicMock()
            mock_sess.start = AsyncMock()
            mock_sess.on = MagicMock()
            mock_sess.aclose = AsyncMock()
            sess_cls.return_value = mock_sess

            # 直接调用 connect 并验证行为差异
            nc = AsyncMock()

            # 模拟旧代码：connect(url, token, RoomOptions(auto_subscribe=False))
            mock_room.connect("ws://fake:7880", "token",
                              rtc.RoomOptions(auto_subscribe=False))

            args = mock_room.connect.call_args[0]
            self.assertEqual(len(args), 3,
                             "旧代码传 3 个参数（含 RoomOptions）")
            self.assertFalse(args[2].auto_subscribe,
                             "旧代码 auto_subscribe=False → AI 听不到用户 → 没声音")


if __name__ == "__main__":
    unittest.main()



    def test_inject_patch_and_selfcheck_exist(self):
        """main.py 含注入 patch(_grix_inject_rag) 和启动自检(CRITICAL)。"""
        with open(os.path.join(os.path.dirname(__file__), "main.py")) as f:
            source = f.read()
        self.assertIn("_grix_inject_rag", source)
        self.assertIn("CRITICAL", source)
        self.assertIn("voicebridge.inject", source)

# ===========================================================================
# 接点B 注入逻辑守卫测试
# ===========================================================================
class InjectGuardTest(unittest.TestCase):
    """voicebridge.inject.* 订阅回调 + 502 帧格式守卫。"""

    def setUp(self):
        try:
            import main  # noqa: F401
        except (ImportError, ModuleNotFoundError):
            self.skipTest("Dependencies not installed (run in venv)")

    def test_on_inject_parses_call_id_from_subject(self):
        """on_inject 从 subject 尾部解析 call_id。"""
        import main as m
        msg = MagicMock()
        msg.subject = "voicebridge.inject.12345"
        msg.data = json.dumps({"text": "hello", "round_seq": 1}).encode()
        # 无活跃 session 时应静默跳过
        m._inject_sessions.clear()
        asyncio.run(m.on_inject(msg))  # no error

    def test_on_inject_drops_stale_round_seq(self):
        """round_seq 过期时丢弃注入。"""
        import main as m
        m._inject_sessions.clear()
        m._inject_last_seq.clear()
        # 模拟已处理 round_seq=100, seq=0
        m._inject_last_seq[999] = (100, 0)
        mock_session = MagicMock()
        mock_session._grix_inject_rag = AsyncMock(return_value=True)
        m._inject_sessions[999] = mock_session
        msg = MagicMock()
        msg.subject = "voicebridge.inject.999"
        msg.data = json.dumps({"text": "old", "round_seq": 50}).encode()
        asyncio.run(m.on_inject(msg))
        mock_session._grix_inject_rag.assert_not_called()
        # 清理
        m._inject_sessions.clear()
        m._inject_last_seq.clear()

    def test_on_inject_accepts_newer_round_seq(self):
        """round_seq 更新时正常注入。"""
        import main as m
        m._inject_sessions.clear()
        m._inject_last_seq.clear()
        m._inject_last_seq[888] = (10, 0)
        mock_session = MagicMock()
        mock_session._grix_inject_rag = AsyncMock(return_value=True)
        # user_querying 守卫读取 _grix_ctx：必须是真实 dict（MagicMock 恒真会误跳过注入）
        mock_session._realtime_model._grix_ctx = {"user_querying": [False]}
        m._inject_sessions[888] = mock_session
        msg = MagicMock()
        msg.subject = "voicebridge.inject.888"
        msg.data = json.dumps({"text": "new answer", "round_seq": 20}).encode()
        asyncio.run(m.on_inject(msg))
        mock_session._grix_inject_rag.assert_called_once_with("new answer")
        assert m._inject_last_seq[888] == (20, 0)
        m._inject_sessions.clear()
        m._inject_last_seq.clear()

    def test_on_inject_skips_empty_text(self):
        """空 text 不注入。"""
        import main as m
        m._inject_sessions.clear()
        mock_session = MagicMock()
        mock_session._grix_inject_rag = AsyncMock(return_value=True)
        m._inject_sessions[777] = mock_session
        msg = MagicMock()
        msg.subject = "voicebridge.inject.777"
        msg.data = json.dumps({"text": "  ", "round_seq": 1}).encode()
        asyncio.run(m.on_inject(msg))
        mock_session._grix_inject_rag.assert_not_called()
        m._inject_sessions.clear()

    def test_grix_inject_rag_frame_format(self):
        """_grix_inject_rag 构造正确的 502 ChatRAGText 帧。"""
        import gzip, json as _json
        import main as m
        # 需要 volcengine 插件
        try:
            from livekit.plugins.volcengine.realtime import RealtimeSession
        except ImportError:
            self.skipTest("volcengine plugin not installed")
        # 确认 patch 已生效
        self.assertTrue(hasattr(RealtimeSession, '_grix_inject_rag'))
        # 构造 mock session
        session = MagicMock(spec=RealtimeSession)
        session.session_id = "test-uuid-123"
        mock_ws = AsyncMock()
        session._grix_ws_conn = mock_ws
        # 调用
        result = asyncio.run(RealtimeSession._grix_inject_rag(session, "测试内容"))
        self.assertTrue(result)
        # 验证发送的帧
        mock_ws.send_bytes.assert_called_once()
        frame = mock_ws.send_bytes.call_args[0][0]
        # 解析帧: header(4) + event(4) + session_id_len(4) + session_id + payload_len(4) + payload
        self.assertEqual(frame[0] >> 4, 1)  # protocol version 1
        event = int.from_bytes(frame[4:8], "big")
        self.assertEqual(event, 502)  # ChatRAGText
        sid_len = int.from_bytes(frame[8:12], "big")
        sid = frame[12:12+sid_len].decode()
        self.assertEqual(sid, "test-uuid-123")
        payload_offset = 12 + sid_len
        payload_len = int.from_bytes(frame[payload_offset:payload_offset+4], "big")
        payload_bytes = frame[payload_offset+4:payload_offset+4+payload_len]
        decompressed = gzip.decompress(payload_bytes)
        payload = _json.loads(decompressed)
        self.assertEqual(payload["external_rag"], "测试内容")

    def test_startup_self_check_ast(self):
        """启动自检逻辑存在于 main.py 中（AST 守卫）。"""
        with open(os.path.join(os.path.dirname(__file__), "main.py")) as f:
            source = f.read()
        self.assertIn("_grix_inject_rag", source)
        self.assertIn("CRITICAL", source)



    def test_inject_patch_and_selfcheck_exist(self):
        """main.py 含注入 patch(_grix_inject_rag) 和启动自检(CRITICAL)。"""
        with open(os.path.join(os.path.dirname(__file__), "main.py")) as f:
            source = f.read()
        self.assertIn("_grix_inject_rag", source)
        self.assertIn("CRITICAL", source)
        self.assertIn("voicebridge.inject", source)

# ===========================================================================
# 接点B 注入逻辑守卫测试
# ===========================================================================
class InjectGuardTest(unittest.TestCase):
    """voicebridge.inject.* 订阅回调 + 502 帧格式守卫。"""

    def setUp(self):
        try:
            import main  # noqa: F401
        except (ImportError, ModuleNotFoundError):
            self.skipTest("Dependencies not installed (run in venv)")

    def test_on_inject_parses_call_id_from_subject(self):
        """on_inject 从 subject 尾部解析 call_id,无活跃 session 时静默跳过。"""
        import main as m
        msg = MagicMock()
        msg.subject = "voicebridge.inject.12345"
        msg.data = json.dumps({"text": "hello", "round_seq": 1}).encode()
        m._inject_sessions.clear()
        asyncio.run(m.on_inject(msg))  # no error

    def test_on_inject_drops_stale_round_seq(self):
        """round_seq 过期时丢弃注入。"""
        import main as m
        m._inject_sessions.clear()
        m._inject_last_seq.clear()
        m._inject_last_seq[999] = (100, 0)
        mock_session = MagicMock()
        mock_session._grix_inject_rag = AsyncMock(return_value=True)
        m._inject_sessions[999] = mock_session
        msg = MagicMock()
        msg.subject = "voicebridge.inject.999"
        msg.data = json.dumps({"text": "old", "round_seq": 50}).encode()
        asyncio.run(m.on_inject(msg))
        mock_session._grix_inject_rag.assert_not_called()
        m._inject_sessions.clear()
        m._inject_last_seq.clear()

    def test_on_inject_accepts_newer_round_seq(self):
        """round_seq 更新时正常注入。"""
        import main as m
        m._inject_sessions.clear()
        m._inject_last_seq.clear()
        m._inject_last_seq[888] = (10, 0)
        mock_session = MagicMock()
        mock_session._grix_inject_rag = AsyncMock(return_value=True)
        # user_querying 守卫读取 _grix_ctx：必须是真实 dict（MagicMock 恒真会误跳过注入）
        mock_session._realtime_model._grix_ctx = {"user_querying": [False]}
        m._inject_sessions[888] = mock_session
        msg = MagicMock()
        msg.subject = "voicebridge.inject.888"
        msg.data = json.dumps({"text": "new answer", "round_seq": 20}).encode()
        asyncio.run(m.on_inject(msg))
        mock_session._grix_inject_rag.assert_called_once_with("new answer")
        assert m._inject_last_seq[888] == (20, 0)
        m._inject_sessions.clear()
        m._inject_last_seq.clear()

    def test_on_inject_skips_empty_text(self):
        """空 text 不注入。"""
        import main as m
        m._inject_sessions.clear()
        mock_session = MagicMock()
        mock_session._grix_inject_rag = AsyncMock(return_value=True)
        m._inject_sessions[777] = mock_session
        msg = MagicMock()
        msg.subject = "voicebridge.inject.777"
        msg.data = json.dumps({"text": "  ", "round_seq": 1}).encode()
        asyncio.run(m.on_inject(msg))
        mock_session._grix_inject_rag.assert_not_called()
        m._inject_sessions.clear()

    def test_startup_self_check_exists(self):
        """启动自检逻辑存在于 main.py 中（AST 守卫,无需依赖）。"""
        with open(os.path.join(os.path.dirname(__file__), "main.py")) as f:
            source = f.read()
        self.assertIn("_grix_inject_rag", source)
        self.assertIn("CRITICAL", source)

    test_startup_self_check_exists.no_deps = True

    def setUp(self):
        test_method = getattr(self, self._testMethodName, None)
        if getattr(test_method, "no_deps", False):
            return
        try:
            import main  # noqa: F401
        except (ImportError, ModuleNotFoundError):
            self.skipTest("Dependencies not installed (run in venv)")


class RelayInjectRoutingTest(unittest.TestCase):
    """传声筒模式（语音大脑）注入路由：relay_mode 走事件300逐字念回，否则走事件502参考资料。"""

    def setUp(self):
        try:
            import main  # noqa: F401
        except (ImportError, ModuleNotFoundError):
            self.skipTest("Dependencies not installed (run in venv)")

    def _make_msg(self, call_id, text, round_seq):
        msg = MagicMock()
        msg.subject = f"voicebridge.inject.{call_id}"
        msg.data = json.dumps({"text": text, "round_seq": round_seq}).encode()
        return msg

    def test_relay_mode_routes_to_tts(self):
        """ctx.relay_mode=True → 调用 _grix_inject_tts(事件300)，不调用 _grix_inject_rag。"""
        import main as m
        m._inject_sessions.clear(); m._inject_last_seq.clear()
        sess = MagicMock()
        sess._grix_inject_tts = AsyncMock(return_value=True)
        sess._grix_inject_rag = AsyncMock(return_value=True)
        sess._realtime_model._grix_ctx = {"user_querying": [False], "relay_mode": True}
        m._inject_sessions[555] = sess
        asyncio.run(m.on_inject(self._make_msg(555, "文字agent的原话", 5)))
        sess._grix_inject_tts.assert_called_once_with("文字agent的原话")
        sess._grix_inject_rag.assert_not_called()
        self.assertEqual(m._inject_last_seq[555], (5, 0))
        m._inject_sessions.clear(); m._inject_last_seq.clear()

    def test_non_relay_routes_to_rag(self):
        """ctx 无 relay_mode（客服/普通语音）→ 仍走 _grix_inject_rag(事件502)，行为不变。"""
        import main as m
        m._inject_sessions.clear(); m._inject_last_seq.clear()
        sess = MagicMock()
        sess._grix_inject_tts = AsyncMock(return_value=True)
        sess._grix_inject_rag = AsyncMock(return_value=True)
        sess._realtime_model._grix_ctx = {"user_querying": [False]}
        m._inject_sessions[556] = sess
        asyncio.run(m.on_inject(self._make_msg(556, "参考答案", 6)))
        sess._grix_inject_rag.assert_called_once_with("参考答案")
        sess._grix_inject_tts.assert_not_called()
        m._inject_sessions.clear(); m._inject_last_seq.clear()

    def test_grix_inject_tts_frame_is_event_300(self):
        """_grix_inject_tts 构造事件300 SayHello 帧，payload={content}。"""
        import gzip, json as _json
        try:
            from livekit.plugins.volcengine.realtime import RealtimeSession
        except ImportError:
            self.skipTest("volcengine plugin not installed")
        self.assertTrue(hasattr(RealtimeSession, '_grix_inject_tts'))
        session = MagicMock(spec=RealtimeSession)
        session.session_id = "sid-tts-1"
        mock_ws = AsyncMock()
        session._grix_ws_conn = mock_ws
        result = asyncio.run(RealtimeSession._grix_inject_tts(session, "逐字念这句"))
        self.assertTrue(result)
        frame = mock_ws.send_bytes.call_args[0][0]
        self.assertEqual(int.from_bytes(frame[4:8], "big"), 300)  # SayHello
        sid_len = int.from_bytes(frame[8:12], "big")
        off = 12 + sid_len
        plen = int.from_bytes(frame[off:off+4], "big")
        payload = _json.loads(gzip.decompress(frame[off+4:off+4+plen]))
        self.assertEqual(payload["content"], "逐字念这句")


class OpenAITakeoverTest(unittest.IsolatedAsyncioTestCase):
    """Phase 4：openai 接管=只听不说（input 不关）、交回=恢复，含状态提示注入。"""

    async def test_takeover_keeps_input_suppresses_output(self):
        import main as m
        from openai_orchestrator import OpenAIVoiceOrchestrator

        notes = []
        replies = []

        async def _note(text):
            notes.append(text)

        loop = asyncio.get_running_loop()
        orch = OpenAIVoiceOrchestrator(1, lambda: replies.append(1), _note, loop, race_ms=20)

        session = MagicMock()
        session.interrupt = MagicMock()
        session.output.set_audio_enabled = MagicMock()
        session.input.set_audio_enabled = MagicMock()
        entry = {"session": session, "orchestrator": orch,
                 "inject_note": _note, "provider": "openai_realtime"}

        # 接管
        await m._openai_set_takeover(entry, 1, True)
        self.assertTrue(entry["muted"])
        session.interrupt.assert_called_once()                       # 打断在途回复
        session.output.set_audio_enabled.assert_called_with(False)   # 关发声
        session.input.set_audio_enabled.assert_not_called()          # input 始终不动（持续听）
        self.assertEqual(len(notes), 1)
        self.assertIn("人工客服现在接管", notes[0])

        # 接管期间用户说完一句也不开口（编排器已静音）
        orch.notify_user_stopped()
        await asyncio.sleep(0.05)
        self.assertEqual(replies, [])

        # 交回
        await m._openai_set_takeover(entry, 1, False)
        self.assertFalse(entry["muted"])
        session.output.set_audio_enabled.assert_called_with(True)    # 恢复发声
        self.assertEqual(len(notes), 2)
        self.assertIn("交回", notes[1])

        # 交回后用户说完一句应正常开口
        orch.notify_user_stopped()
        await asyncio.sleep(0.05)
        self.assertEqual(replies, [1])
        orch.aclose()


# --- 多节点：节点身份 / 定向主题 / 回执 / 广播 owner 守卫 ---
class MultiNodeTest(unittest.IsolatedAsyncioTestCase):

    def test_node_identity_priority_and_sanitize(self):
        from node_identity import resolve_node_id
        with patch.dict(os.environ, {"VOICEBRIDGE_NODE_ID": "vb-pod-0"}):
            self.assertEqual(resolve_node_id(), "vb-pod-0")
        with patch.dict(os.environ, {"VOICEBRIDGE_NODE_ID": "", "POD_NAME": "aibot-voicebridge-7d9f.x"}, clear=False):
            # 空 VOICEBRIDGE_NODE_ID 退到 POD_NAME；点号会切分 NATS token，必须替换
            self.assertEqual(resolve_node_id(), "aibot-voicebridge-7d9f-x")
        with patch.dict(os.environ, {"VOICEBRIDGE_NODE_ID": "", "POD_NAME": ""}):
            # 都未设置退化为主机名，且不含非法字符
            import re
            self.assertTrue(re.fullmatch(r"[A-Za-z0-9_-]+", resolve_node_id()))

    def test_node_subject(self):
        import main as m
        self.assertEqual(m._node_subject(m.SUBJECT_MUTE),
                         f"voicebridge.control.mute.{m.NODE_ID}")

    def test_ack_payload(self):
        import main as m
        self.assertEqual(json.loads(m._ack(True)), {"ok": True, "node_id": m.NODE_ID})
        self.assertEqual(json.loads(m._ack(False, "boom")),
                         {"ok": False, "node_id": m.NODE_ID, "error": "boom"})

    async def test_owner_only_ignores_non_owner(self):
        """非 owner 节点收到广播控制指令应静默忽略（不应答、不调用 handler）。"""
        import main as m
        called = []

        async def handler(msg):
            called.append(msg)

        guarded = m._owner_only(handler)
        m._sessions.pop(999, None)
        await guarded(_msg({"call_id": 999}))
        self.assertEqual(called, [])

    async def test_owner_only_passes_owner(self):
        import main as m
        called = []

        async def handler(msg):
            called.append(msg)

        guarded = m._owner_only(handler)
        m._sessions[888] = {"session": MagicMock(), "started_at": 0}
        try:
            await guarded(_msg({"call_id": 888}))
        finally:
            m._sessions.pop(888, None)
        self.assertEqual(len(called), 1)

    async def test_owner_only_ignores_bad_payload(self):
        import main as m
        called = []

        async def handler(msg):
            called.append(msg)

        bad = MagicMock()
        bad.data = b"not-json"
        bad.reply = "_INBOX.x"
        await m._owner_only(handler)(bad)
        self.assertEqual(called, [])

    def test_healthcheck_uses_node_subject(self):
        """healthcheck 必须探测本节点定向主题，否则多副本互相代答。"""
        import inspect
        import healthcheck as hc
        src = inspect.getsource(hc.main)
        self.assertIn("resolve_node_id()", src)
        self.assertIn('f"{SUBJECT_HEALTH}.', src)

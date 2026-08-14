"""bridge_config 单元测试（stdlib unittest，无第三方依赖）。"""

import unittest

from bridge_config import (
    BridgeConfigError,
    build_realtime_model,
    parse_start_message,
    sanitize_doubao_system_role,
)


class FakeRealtime:
    def __init__(self):
        self.last_kwargs = None

    def RealtimeModel(self, **kwargs):
        self.last_kwargs = kwargs
        return ("model", kwargs)


class FakeOpenAIPlugin:
    def __init__(self):
        self.realtime = FakeRealtime()


class ParseStartMessageTest(unittest.TestCase):
    def test_full_message(self):
        cfg = parse_start_message({
            "call_id": 7,
            "voice_provider": "openai_realtime",
            "model": "gpt-4o-realtime-preview",
            "api_key": "sk-user",
            "endpoint": "wss://x/realtime",
            "voice": "verse",
            "system_prompt": "你是助理",
        })
        self.assertEqual(cfg["call_id"], 7)
        self.assertEqual(cfg["api_key"], "sk-user")
        self.assertEqual(cfg["model"], "gpt-4o-realtime-preview")
        self.assertEqual(cfg["endpoint"], "wss://x/realtime")
        self.assertEqual(cfg["voice"], "verse")
        self.assertEqual(cfg["system_prompt"], "你是助理")

    def test_owner_id_passthrough(self):
        """三方通话契约：后端 start 消息的 owner_id 必须透传进 cfg（>0 才会启动主人插话音频桥）。"""
        base = {"voice_provider": "doubao_realtime", "model": "m", "api_key": "k"}
        cfg = parse_start_message({**base, "owner_id": 42})
        self.assertEqual(cfg["owner_id"], 42)
        # 两人通话不带 owner_id → 0，主人桥不启动
        self.assertEqual(parse_start_message(base)["owner_id"], 0)

    def test_missing_api_key(self):
        with self.assertRaises(BridgeConfigError):
            parse_start_message({"voice_provider": "openai_realtime", "model": "m"})

    def test_missing_model(self):
        with self.assertRaises(BridgeConfigError):
            parse_start_message({"voice_provider": "openai_realtime", "api_key": "k"})

    def test_unsupported_provider(self):
        with self.assertRaises(BridgeConfigError):
            parse_start_message({"voice_provider": "mystery", "model": "m", "api_key": "k"})

    def test_default_system_prompt(self):
        cfg = parse_start_message({"voice_provider": "openai_realtime", "model": "m", "api_key": "k"})
        self.assertTrue(cfg["system_prompt"])

    def test_max_call_seconds(self):
        base = {"voice_provider": "openai_realtime", "model": "m", "api_key": "k"}
        cfg = parse_start_message({**base, "max_call_seconds": 600})
        self.assertEqual(cfg["max_call_seconds"], 600)
        # 缺省/非法值回退 0（= 用 30 分钟兜底）
        self.assertEqual(parse_start_message(base)["max_call_seconds"], 0)
        self.assertEqual(parse_start_message({**base, "max_call_seconds": "abc"})["max_call_seconds"], 0)

    def test_opening_passthrough(self):
        base = {"voice_provider": "openai_realtime", "model": "m", "api_key": "k"}
        self.assertEqual(parse_start_message({**base, "opening": "你好，有什么可以帮你？"})["opening"], "你好，有什么可以帮你？")
        # 缺省不打招呼
        self.assertEqual(parse_start_message(base)["opening"], "")


class _FakeRealtimeOpts:
    """模拟 volcengine 插件 _RealtimeOptions：默认 get_ws_headers 走旧版 App-ID 鉴权，
    build_realtime_model 会就地覆盖成 X-Api-Key（与真实 dataclass 实例覆盖同理）。"""
    def __init__(self, app_id, access_token):
        self.app_id = app_id
        self.access_token = access_token

    def get_ws_headers(self):
        return {"X-Api-App-ID": self.app_id, "X-Api-Access-Key": self.access_token}


class _FakeRealtimeModel:
    def __init__(self, kwargs):
        self._opts = _FakeRealtimeOpts(kwargs.get("app_id"), kwargs.get("access_token"))


class FakeVolcenginePlugin:
    def __init__(self):
        self.last_kwargs = None

    def RealtimeModel(self, **kwargs):
        self.last_kwargs = kwargs
        return _FakeRealtimeModel(kwargs)


class SanitizeDoubaoSystemRoleTest(unittest.TestCase):
    """守卫：豆包 system_role 必须单行（火山服务端对含换行提示词整会话卡死，2026-07-03 实测）。"""

    def test_multiline_markdown_prompt_flattened(self):
        role = "***禁止说豆包***\n\n## 一、身份定位\n你是Grix官方客服。\n\n## 二、FAQ\n**Q1**：略。"
        out = sanitize_doubao_system_role(role)
        self.assertNotIn("\n", out)
        self.assertNotIn("\r", out)
        self.assertEqual(out, "***禁止说豆包***；## 一、身份定位；你是Grix官方客服。；## 二、FAQ；**Q1**：略。")

    def test_crlf_and_trailing_newline(self):
        self.assertEqual(sanitize_doubao_system_role("甲\r\n乙\n"), "甲；乙")

    def test_consecutive_newlines_collapse_to_one_separator(self):
        self.assertEqual(sanitize_doubao_system_role("甲\n\n\n乙"), "甲；乙")

    def test_surrounding_spaces_absorbed(self):
        self.assertEqual(sanitize_doubao_system_role("甲  \n  乙"), "甲；乙")

    def test_single_line_unchanged(self):
        self.assertEqual(sanitize_doubao_system_role("你是客服，简短回答。"), "你是客服，简短回答。")

    def test_empty_and_none_safe(self):
        self.assertEqual(sanitize_doubao_system_role(""), "")
        self.assertEqual(sanitize_doubao_system_role(None), "")


class BuildRealtimeModelTest(unittest.TestCase):
    def test_openai_uses_message_fields(self):
        plugin = FakeOpenAIPlugin()
        cfg = {
            "provider": "openai_realtime",
            "model": "gpt-4o-realtime-preview",
            "api_key": "sk-user",
            "endpoint": "wss://custom/realtime",
            "voice": "verse",
        }
        build_realtime_model(plugin, cfg)
        kw = plugin.realtime.last_kwargs
        self.assertEqual(kw["model"], "gpt-4o-realtime-preview")
        self.assertEqual(kw["api_key"], "sk-user")
        self.assertEqual(kw["voice"], "verse")
        self.assertEqual(kw["base_url"], "wss://custom/realtime")

    def test_empty_voice_uses_default_no_base_url(self):
        plugin = FakeOpenAIPlugin()
        cfg = {"provider": "openai_realtime", "model": "m", "api_key": "k", "endpoint": "", "voice": ""}
        build_realtime_model(plugin, cfg)
        kw = plugin.realtime.last_kwargs
        self.assertEqual(kw["voice"], "alloy")
        self.assertNotIn("base_url", kw)

    def test_openai_session_config(self):
        """会话配置：modalities + 上行转写 + server_vad 语序控制模式都应注入。"""
        plugin = FakeOpenAIPlugin()
        cfg = {"provider": "openai_realtime", "model": "gpt-realtime-2",
               "api_key": "k", "endpoint": "", "voice": ""}
        build_realtime_model(plugin, cfg)
        kw = plugin.realtime.last_kwargs
        self.assertEqual(kw["modalities"], ["text", "audio"])
        # 上行转写已开（接点A 依赖）
        self.assertIsNotNone(kw["input_audio_transcription"])
        self.assertEqual(kw["input_audio_transcription"].model, "gpt-4o-transcribe")
        # 语序控制：create_response=False（编排器手动 generate_reply）、用户插话可打断
        td = kw["turn_detection"]
        self.assertEqual(td.type, "server_vad")
        self.assertFalse(td.create_response)
        self.assertTrue(td.interrupt_response)
        self.assertNotIn("truncation", kw)  # 新版插件已移除该参数，由插件自行处理

    def test_openai_turn_detection_builder_flips_create_response(self):
        """构造器两种模式都可用（Phase 1 直答曾用 True，语序控制用 False）。"""
        from bridge_config import build_openai_turn_detection
        self.assertFalse(build_openai_turn_detection(create_response=False).create_response)
        self.assertTrue(build_openai_turn_detection(create_response=True).create_response)

    def test_doubao_realtime(self):
        """新版火山 API Key 单 Key 鉴权：api_key 整串即 X-Api-Key，不再拆 appid:token；
        构造给插件的 app_id/access_token 是占位符，真正鉴权头被覆盖成 X-Api-Key。"""
        volc = FakeVolcenginePlugin()
        cfg = {
            "provider": "doubao_realtime",
            "model": "O",
            "api_key": "sk-volc-apikey-xyz",
            "endpoint": "",
            "voice": "zh_female_vv_jupiter_bigtts",
        }
        rt = build_realtime_model(FakeOpenAIPlugin(), cfg, volc)
        kw = volc.last_kwargs
        # 不再拆冒号；app_id/access_token 仅占位
        self.assertEqual(kw["app_id"], "-")
        self.assertEqual(kw["access_token"], "-")
        self.assertEqual(kw["model"], "O")
        self.assertEqual(kw["speaker"], "zh_female_vv_jupiter_bigtts")
        # 鉴权头已换成 X-Api-Key（整串 api_key），不再带 App-ID/Access-Key
        headers = rt._opts.get_ws_headers()
        self.assertEqual(headers["X-Api-Key"], "sk-volc-apikey-xyz")
        self.assertEqual(headers["X-Api-Resource-Id"], "volc.speech.dialog")
        self.assertEqual(headers["X-Api-App-Key"], "PlgvMymc7f3tQnJ6")
        self.assertIn("X-Api-Connect-Id", headers)
        self.assertNotIn("X-Api-App-ID", headers)
        self.assertNotIn("X-Api-Access-Key", headers)

    def test_doubao_apikey_with_colon_kept_whole(self):
        """api_key 含冒号也不拆：整串作为 X-Api-Key（新版 Key 本身可能含冒号）。"""
        volc = FakeVolcenginePlugin()
        cfg = {"provider": "doubao_realtime", "model": "O", "api_key": "abc:def:ghi", "endpoint": "", "voice": ""}
        rt = build_realtime_model(FakeOpenAIPlugin(), cfg, volc)
        self.assertEqual(rt._opts.get_ws_headers()["X-Api-Key"], "abc:def:ghi")

    def test_doubao_opening_explicit_text(self):
        """非空 opening 显式传给插件，覆盖默认硬编码问候语。"""
        volc = FakeVolcenginePlugin()
        cfg = {"provider": "doubao_realtime", "model": "O", "api_key": "k", "endpoint": "", "voice": "",
               "opening": "您好，欢迎咨询，请问有什么可以帮您？"}
        build_realtime_model(FakeOpenAIPlugin(), cfg, volc)
        self.assertEqual(volc.last_kwargs["opening"], "您好，欢迎咨询，请问有什么可以帮您？")

    def test_doubao_opening_empty_suppresses_default_greeting(self):
        """未配置开场白时传 None（而非缺省字段），抑制插件默认的硬编码中文问候。"""
        volc = FakeVolcenginePlugin()
        cfg = {"provider": "doubao_realtime", "model": "O", "api_key": "k", "endpoint": "", "voice": ""}
        build_realtime_model(FakeOpenAIPlugin(), cfg, volc)
        self.assertIsNone(volc.last_kwargs["opening"])

    def test_doubao_empty_api_key_raises(self):
        volc = FakeVolcenginePlugin()
        cfg = {"provider": "doubao_realtime", "model": "O", "api_key": "", "endpoint": "", "voice": ""}
        with self.assertRaises(BridgeConfigError):
            build_realtime_model(FakeOpenAIPlugin(), cfg, volc)

    def test_doubao_no_plugin_raises(self):
        cfg = {"provider": "doubao_realtime", "model": "O", "api_key": "key", "endpoint": "", "voice": ""}
        with self.assertRaises(BridgeConfigError):
            build_realtime_model(FakeOpenAIPlugin(), cfg, None)

    def test_unsupported_provider_raises(self):
        with self.assertRaises(BridgeConfigError):
            build_realtime_model(FakeOpenAIPlugin(), {"provider": "mystery", "model": "m", "api_key": "k", "endpoint": "", "voice": ""})


if __name__ == "__main__":
    unittest.main()

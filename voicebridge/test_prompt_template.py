"""prompt_template 单测（stdlib unittest，无第三方依赖）。"""
import unittest

from prompt_template import (
    AUTHORITATIVE_TAG,
    build_openai_instructions,
    tag_authoritative,
)


class BuildInstructionsTest(unittest.TestCase):
    def test_wraps_agent_prompt_with_strategy(self):
        out = build_openai_instructions("你是好望角茶楼的客服小望。", "")
        # 守则关键条目都在
        self.assertIn("快速响应", out)
        self.assertIn("权威资料优先", out)
        self.assertIn(AUTHORITATIVE_TAG, out)            # 守则引用了标记，与注入侧一致
        self.assertIn("你是好望角茶楼的客服小望。", out)   # agent 人设被包进去
        # agent 人设在守则之后（稳定前缀在前，吃缓存）
        self.assertLess(out.index("快速响应"), out.index("好望角茶楼"))

    def test_language_default_is_chinese(self):
        out = build_openai_instructions("x", "")
        self.assertIn("简体中文", out)

    def test_language_explicit(self):
        out = build_openai_instructions("x", "en-US")
        self.assertIn("en-US", out)

    def test_empty_agent_prompt_still_usable(self):
        out = build_openai_instructions("", "")
        self.assertIn("通用礼貌客服", out)
        self.assertTrue(out.strip())

    def test_stable_prefix_identical_across_agents(self):
        """不同 agent 人设，守则前缀逐字相同（缓存命中的前提）。"""
        a = build_openai_instructions("人设A", "")
        b = build_openai_instructions("人设B（完全不同）", "")
        prefix_a = a.split("以下是你的角色设定")[0]
        prefix_b = b.split("以下是你的角色设定")[0]
        self.assertEqual(prefix_a, prefix_b)


class TagAuthoritativeTest(unittest.TestCase):
    def test_adds_tag(self):
        self.assertEqual(tag_authoritative("订单已发货"), AUTHORITATIVE_TAG + "订单已发货")

    def test_idempotent(self):
        once = tag_authoritative("库存为0")
        self.assertEqual(tag_authoritative(once), once)

    def test_empty(self):
        self.assertEqual(tag_authoritative(""), "")
        self.assertEqual(tag_authoritative("  "), "")


if __name__ == "__main__":
    unittest.main()

"""volc_fix 单元测试（纯 stdlib，无需 livekit）。

运行： python3 test_volc_fix.py
"""
import unittest

from volc_fix import parse_event_num, should_drop_dup_tts_end, TTS_ENDED_EVENT


def make_frame(event_num, with_event=True, with_seq=False, header_words=1, tail=b"\x00"):
    """构造一帧豆包二进制协议数据，仅满足 parse_event_num 的解析需要。

    header_words 个 4 字节 header；data[1] 低 4 位置 flags：
      0x04 = 带 event 字段，0x01 = 带序列号。
    """
    flags = 0
    if with_seq:
        flags |= 0x01
    if with_event:
        flags |= 0x04
    header = bytearray(header_words * 4)
    header[0] = (0x90 | (header_words & 0x0F))  # 高位随意，低 4 位为 header_size
    header[1] = (0x90 | (flags & 0x0F))
    body = bytearray()
    if with_seq:
        body += (1).to_bytes(4, "big")
    if with_event:
        body += int(event_num).to_bytes(4, "big")
    return bytes(header) + bytes(body) + tail


class TestParseEventNum(unittest.TestCase):
    def test_tts_ended(self):
        self.assertEqual(parse_event_num(make_frame(359)), 359)

    def test_asr_response(self):
        self.assertEqual(parse_event_num(make_frame(451)), 451)

    def test_with_sequence_offset(self):
        self.assertEqual(parse_event_num(make_frame(359, with_seq=True)), 359)

    def test_no_event_flag(self):
        self.assertIsNone(parse_event_num(make_frame(359, with_event=False)))

    def test_too_short(self):
        self.assertIsNone(parse_event_num(b"\x11\x04"))

    def test_empty(self):
        self.assertIsNone(parse_event_num(b""))
        self.assertIsNone(parse_event_num(None))


class TestShouldDropDupTtsEnd(unittest.TestCase):
    def test_drop_dup_tts_end_when_no_active_generation(self):
        # 崩溃条件：359 且当前无活跃生成对象 → 应丢弃
        self.assertTrue(should_drop_dup_tts_end(make_frame(359), has_active_generation=False))

    def test_keep_first_tts_end_with_active_generation(self):
        # 首个 359，生成对象仍在 → 放行，让插件正常收尾
        self.assertFalse(should_drop_dup_tts_end(make_frame(359), has_active_generation=True))

    def test_keep_non_tts_end_event(self):
        # 非 359（如 ASR 451）即便无生成对象也放行
        self.assertFalse(should_drop_dup_tts_end(make_frame(451), has_active_generation=False))

    def test_keep_garbage_frame(self):
        self.assertFalse(should_drop_dup_tts_end(b"\x00\x00", has_active_generation=False))


if __name__ == "__main__":
    unittest.main(verbosity=2)

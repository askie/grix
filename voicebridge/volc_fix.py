"""volc_fix —— volcengine(豆包) 实时语音插件的纯函数修复辅助。

单一职责：从豆包二进制协议帧里解析 event 编号，并判断某一帧是否应当丢弃，
以规避 livekit-plugins-volcengine==1.3.0 在 _recv_task 中的崩溃 bug。

崩溃背景：
    豆包一轮回复结束时发送 TTSEnded(event=359)。插件处理首个 359 时会执行
    `self._current_generation = None`；若同一轮再次收到 359（线上实测会重复发），
    插件无判空地执行 `self._current_generation.message_ch.close()`，触发
    `AttributeError: 'NoneType' object has no attribute 'message_ch'`，
    接收协程 break 退出，整条豆包 WebSocket 连接被拆除，
    此后用户再说话无人应答（"只说一句就没反应"）。

修复策略：
    在 ws_conn.receive 拦截层判断——当帧为 TTSEnded(359) 且当前已无活跃生成对象
    （_current_generation is None）时，丢弃这一帧。插件 _recv_task 的循环遇到
    data 为 None 的消息会 `continue` 跳过，从而绕开崩溃分支。该判定恰好命中崩溃
    条件（首个 359 时生成对象非 None，正常放行；重复 359 时已为 None，丢弃）。

本模块不依赖 livekit，可独立单元测试。
"""

TTS_ENDED_EVENT = 359


def parse_event_num(data):
    """从豆包二进制帧解析 event 编号，失败返回 None。

    协议：data[0] 低 4 位为 header_size(单位 4 字节)；data[1] 低 4 位为 flags，
    其中 0x01/0x02 表示带序列号(占 4 字节)、0x04 表示带 event 字段(占 4 字节)。
    event 紧随 header 与可选序列号之后。
    """
    try:
        if not data or len(data) <= 8:
            return None
        flags = data[1] & 0x0F
        offset = (data[0] & 0x0F) * 4
        if flags & 0x03:  # POS/NEG_SEQUENCE
            offset += 4
        if flags & 0x04 and len(data) > offset + 4:  # MSG_WITH_EVENT
            return int.from_bytes(data[offset:offset + 4], "big")
    except Exception:
        return None
    return None


def should_drop_dup_tts_end(data, has_active_generation):
    """判断该帧是否为"会触发崩溃的重复 TTSEnded"，需要丢弃。

    仅当帧是 TTSEnded(359) 且当前已无活跃生成对象时返回 True。
    """
    if has_active_generation:
        return False
    return parse_event_num(data) == TTS_ENDED_EVENT

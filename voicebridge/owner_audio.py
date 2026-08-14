"""三方通话主人插话音频接入（方案 B）的纯逻辑部分。

豆包语音桥默认只订阅客户(caller)一路音频。三方通话里，主人(owner)也在房间内，
但其音频既没被喂给豆包、其语音也不应落 IM。本模块提供"主人是否正在说话"的判定，
供 main.py 据此在客户音频与主人音频之间做轮流让位，并抑制主人语音的转写发布。

这里只放可单测的纯逻辑（能量计算 + 说话状态机）；与 LiveKit / 豆包 session 的实际
交互留在 main.py。
"""

import array


def frame_rms(pcm16: bytes) -> float:
    """计算 16-bit 小端 PCM 单声道帧的均方根能量（响度近似）。

    静音帧约 < 50，正常说话约 > 800。空数据返回 0。
    """
    if not pcm16:
        return 0.0
    usable = len(pcm16) - (len(pcm16) % 2)
    if usable <= 0:
        return 0.0
    samples = array.array("h")
    samples.frombytes(pcm16[:usable])
    if not samples:
        return 0.0
    acc = 0
    for s in samples:
        acc += s * s
    return (acc / len(samples)) ** 0.5


class OwnerTurnDetector:
    """主人说话状态机：能量 VAD + 起音去抖 + 释放尾窗。

    - 起音：连续 attack_frames 帧能量过阈值才认定开始说话（滤掉瞬时噪声）；
    - 释放：最后一帧过阈值后再保持 release_sec 秒才认定说完（覆盖句中停顿，
      以及豆包句尾 ASR 收尾的延迟，避免主人话尾被误判为客户而落 IM）。

    feed(rms, now) 每来一帧音频调用一次，返回当前是否处于"主人说话"轮次。
    now 由调用方传入（time.monotonic()），便于单测注入时间。
    """

    def __init__(self, *, threshold: float = 400.0, attack_frames: int = 3,
                 release_sec: float = 1.2) -> None:
        self.threshold = threshold
        self.attack_frames = max(1, attack_frames)
        self.release_sec = release_sec
        self._voiced_run = 0
        self._active = False
        self._last_voiced_at = 0.0

    @property
    def active(self) -> bool:
        return self._active

    def feed(self, rms: float, now: float) -> bool:
        voiced = rms >= self.threshold
        if voiced:
            self._voiced_run += 1
            self._last_voiced_at = now
        else:
            self._voiced_run = 0

        if not self._active:
            if self._voiced_run >= self.attack_frames:
                self._active = True
        else:
            if not voiced and (now - self._last_voiced_at) >= self.release_sec:
                self._active = False
        return self._active

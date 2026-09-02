import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// 把 iOS 音频会话的声道归还给系统，让之前被打断的背景音乐（例如其他 App
/// 正在播放的音乐）得以恢复。
///
/// iOS 上 `video_player` 播放音视频时会激活 `AVAudioSession`（playback 类别），
/// 抢占系统声道并打断其他 App 的播放；播放结束后若不主动停用会话，被打断的
/// 音乐就无法恢复。这里复用原生侧的
/// `AVAudioSession.setActive(false, notifyOthersOnDeactivation)`，与通话模块
/// 共用同一方法通道。
///
/// 仅 iOS 需要；其他平台为安全的空操作。
class AudioSessionReleaser {
  AudioSessionReleaser._();

  static const MethodChannel _channel = MethodChannel(
    'pub.dhf.grix/audio_session',
  );

  /// 释放音频会话，归还系统声道。任何失败都静默忽略，不影响播放体验。
  static Future<void> release() async {
    if (kIsWeb || defaultTargetPlatform != TargetPlatform.iOS) {
      return;
    }
    try {
      await _channel.invokeMethod('releaseAudioSession');
    } catch (_) {
      // 通道缺失或原生异常都不应影响 UI，静默忽略。
    }
  }
}

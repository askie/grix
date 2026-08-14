import 'package:flutter/foundation.dart';

/// 语音通话平台能力开关。
///
/// 语音通话功能因国内合规无法在 iOS 上线，移动端（iOS/Android）一律禁用入口；
/// 仅在浏览器（Web）与桌面端（macOS/Windows）启用，用于测试体验与桌面商业场景。
class VoiceCallCapability {
  const VoiceCallCapability._();

  /// 当前运行平台是否启用语音通话入口。
  ///
  /// 注意：Web 分支对所有浏览器（含移动端浏览器）生效，这是“Web 端开放测试”的预期结果；
  /// iOS/Android 原生 App 因合规禁用。Linux 桌面同步开启。
  static bool get isEnabled {
    if (kIsWeb) return true;
    switch (defaultTargetPlatform) {
      case TargetPlatform.macOS:
      case TargetPlatform.windows:
        return true;
      case TargetPlatform.linux:
        return true;
      case TargetPlatform.iOS:
      case TargetPlatform.android:
      case TargetPlatform.fuchsia:
        return false;
    }
  }
}

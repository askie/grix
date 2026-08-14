import 'package:flutter/foundation.dart';

/// 统一的平台能力判断工具
/// 所有桌面/移动/Web 的平台分支判断都应通过此类进行
abstract class PlatformCapability {
  PlatformCapability._();

  /// 当前是否运行在桌面平台（macOS / Windows / Linux）
  static bool get isDesktop =>
      !kIsWeb &&
      (defaultTargetPlatform == TargetPlatform.macOS ||
          defaultTargetPlatform == TargetPlatform.windows ||
          defaultTargetPlatform == TargetPlatform.linux);

  /// 当前是否运行在移动平台（iOS / Android）
  static bool get isMobile =>
      !kIsWeb &&
      (defaultTargetPlatform == TargetPlatform.iOS ||
          defaultTargetPlatform == TargetPlatform.android);

  /// 当前是否运行在 macOS
  static bool get isMacOS =>
      !kIsWeb && defaultTargetPlatform == TargetPlatform.macOS;

  /// 当前是否运行在 Windows
  static bool get isWindows =>
      !kIsWeb && defaultTargetPlatform == TargetPlatform.windows;

  /// 当前是否运行在 Web
  static bool get isWeb => kIsWeb;
}

import 'dart:io';

import 'package:flutter/services.dart';

/// iOS 剪贴板缓存服务。
///
/// iOS 16+ 每次通过 Clipboard.getData() 读取剪贴板都会触发系统权限弹窗。
/// 本服务通过 MethodChannel 获取 UIPasteboard.general.changeCount（不触发弹窗），
/// 在 changeCount 未变化时复用缓存的剪贴板文本，将弹窗频率降至
/// "每次复制新内容仅弹一次"。
class NativeClipboardService {
  NativeClipboardService._();

  static const _channel = MethodChannel('pub.dhf.grix/native_clipboard');

  /// 上一次读取时的 changeCount，用于判断剪贴板内容是否变化。
  static int? _cachedChangeCount;

  /// 上一次读取到的剪贴板文本。
  static String? _cachedText;

  /// 获取剪贴板文本。
  ///
  /// 非 iOS 平台直接使用 Flutter 标准 Clipboard API。
  /// iOS 平台先检查 changeCount，未变化时返回缓存文本（不触发权限弹窗）；
  /// changeCount 变化时才真正读取剪贴板（会弹一次权限窗）并更新缓存。
  static Future<String?> getText() async {
    if (!Platform.isIOS) {
      final data = await Clipboard.getData(Clipboard.kTextPlain);
      return data?.text;
    }

    // iOS: 通过 native 获取 changeCount（不触发权限弹窗）
    final int currentChangeCount;
    try {
      currentChangeCount =
          await _channel.invokeMethod<int>('getChangeCount') ?? -1;
    } on PlatformException {
      // MethodChannel 不可用时回退到标准 Clipboard
      final data = await Clipboard.getData(Clipboard.kTextPlain);
      return data?.text;
    }

    // changeCount 未变化 → 缓存命中
    if (currentChangeCount == _cachedChangeCount && _cachedText != null) {
      return _cachedText;
    }

    // changeCount 变化 → 需要重新读取（触发一次权限弹窗）
    final data = await Clipboard.getData(Clipboard.kTextPlain);
    final text = data?.text;

    _cachedChangeCount = currentChangeCount;
    _cachedText = text;

    return text;
  }

  /// 清除缓存（用于测试或特殊场景）。
  static void clearCache() {
    _cachedChangeCount = null;
    _cachedText = null;
  }
}

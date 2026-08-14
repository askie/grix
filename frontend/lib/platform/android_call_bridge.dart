import 'dart:async';
import 'package:flutter/services.dart';

/// AndroidCallBridge 处理 Android FCM 来电通知的 Dart 侧接收。
/// 通过 EventChannel 接收来自 MainActivity 广播的来电事件。
class AndroidCallBridge {
  static const _methodChannel = MethodChannel('com.aibot/android_call');

  static final AndroidCallBridge _instance = AndroidCallBridge._();
  factory AndroidCallBridge() => _instance;
  AndroidCallBridge._() {
    _methodChannel.setMethodCallHandler(_handleNativeCall);
  }

  /// 来电事件回调（call_id, caller_name）
  void Function(Map<String, dynamic>)? onIncomingCall;

  Future<void> _handleNativeCall(MethodCall call) async {
    if (call.method != 'onIncomingCall') return;
    final args = Map<String, dynamic>.from(call.arguments as Map? ?? {});
    onIncomingCall?.call(args);
  }
}

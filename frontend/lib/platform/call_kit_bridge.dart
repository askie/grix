import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../data/providers/auth_service.dart';
import '../shared/utils/app_runtime_endpoints.dart';
import '../shared/utils/device_identity.dart';

/// CallKitBridge 封装 iOS CallKit / PushKit 的 MethodChannel 通信，
/// 并负责将 VoIP push token 上报给后端。
class CallKitBridge {
  static const _channel = MethodChannel('com.aibot/callkit');
  static const _prefVoipToken = 'voip_last_token';

  static final CallKitBridge _instance = CallKitBridge._();
  factory CallKitBridge() => _instance;
  CallKitBridge._() {
    _channel.setMethodCallHandler(_handleNativeCall);
  }

  void Function(Map<String, dynamic>)? onIncomingCall;
  void Function(String uuid)? onCallAnswered;
  void Function(String uuid)? onCallEnded;

  Future<void> _handleNativeCall(MethodCall call) async {
    final args = Map<String, dynamic>.from(call.arguments as Map? ?? {});
    switch (call.method) {
      case 'onIncomingCall':
        onIncomingCall?.call(args);
        break;
      case 'onCallAnswered':
        onCallAnswered?.call(args['uuid']?.toString() ?? '');
        break;
      case 'onCallEnded':
        onCallEnded?.call(args['uuid']?.toString() ?? '');
        break;
      case 'onVoIPTokenUpdated':
        final token = args['token']?.toString() ?? '';
        if (token.isNotEmpty) {
          await _uploadVoIPToken(token);
        }
        break;
    }
  }

  /// 上报 VoIP token 到后端 /devices/bind（platform=ios_voip）
  Future<void> _uploadVoIPToken(String token) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final lastToken = prefs.getString(_prefVoipToken) ?? '';
      if (lastToken == token) return; // 未变化，跳过

      final authService = Get.find<AuthService>();
      final userId = authService.userId ?? '';
      if (userId.isEmpty) return;

      final deviceId = await DeviceIdentity.resolveDeviceId();
      final dio = Dio(
        BaseOptions(
          baseUrl: AppRuntimeEndpoints.apiBaseUrl,
          connectTimeout: const Duration(seconds: 10),
          receiveTimeout: const Duration(seconds: 10),
        ),
      );
      authService.attachAuthInterceptor(dio);

      final resp = await dio.post(
        '/devices/bind',
        data: {
          'platform': 'ios_voip',
          'push_env': 'apns_sandbox', // 由 build config 决定，此处简化
          'device_token': token,
          'device_id': '${deviceId}_voip',
        },
      );

      final ok =
          resp.statusCode == 200 &&
          resp.data is Map &&
          resp.data['code'].toString() == '0';
      if (ok) {
        await prefs.setString(_prefVoipToken, token);
        debugPrint('VoIP token uploaded successfully');
      }
    } catch (e) {
      debugPrint('VoIP token upload error: $e');
    }
  }

  /// 通知 iOS 通话已结束（更新 CallKit UI）
  Future<void> reportCallEnded(String callId) async {
    try {
      await _channel.invokeMethod('reportCallEnded', {'call_id': callId});
    } catch (_) {}
  }
}

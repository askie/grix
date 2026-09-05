import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../shared/utils/app_runtime_endpoints.dart';
import '../../shared/utils/device_identity.dart';
import 'auth_service.dart';
import 'push_registration_service.dart';

/// iOS 实时活动（锁屏 / 灵动岛 / 手表 Smart Stack 上那张 agent 运行卡片）的
/// token 上报。
///
/// 两种 token 分两条路走：
/// - **启动 token**（push-to-start，每设备一个）：后端拿它把卡片从零推起来。
///   存进 [LiveActivityStartToken]，随 `/devices/bind` 捎带上去，不单开请求。
/// - **活动 token**（每张卡一个）：卡开出来之后才有，POST 到
///   `/v1/live_activities/token`。
///
/// 上报一律是"发了就算"：卡片是锦上添花，绝不能让它阻塞任何别的流程。尤其不能在
/// 登录链路里同步 await——这条 Dio 带鉴权拦截器，登录中 await 它会等自己。
class LiveActivityService extends GetxService {
  static const _channel = MethodChannel('pub.dhf.grix/live_activity');

  final Dio _dio = Dio(
    BaseOptions(
      baseUrl: AppRuntimeEndpoints.apiBaseUrl,
      connectTimeout: const Duration(seconds: 10),
      sendTimeout: const Duration(seconds: 10),
      receiveTimeout: const Duration(seconds: 10),
    ),
  );

  Worker? _authWorker;

  /// 没登录时收到的活动 token 先攒着，登录后一次性补报。按 session_id 去重：
  /// 同一张卡重复报最新那次就够了。
  final Map<String, Map<String, String>> _pendingActivityTokens = {};

  Future<LiveActivityService> init() async {
    if (!_isSupported) return this;

    final authService = Get.find<AuthService>();
    authService.attachAuthInterceptor(_dio);

    _channel.setMethodCallHandler(_onMethodCall);

    _authWorker = ever<bool>(authService.isLoggedInRx, (isLoggedIn) {
      if (!isLoggedIn) {
        _pendingActivityTokens.clear();
        return;
      }
      // 登录流程里绝不 await：这条 Dio 挂着鉴权拦截器。
      unawaited(_flushPendingActivityTokens());
    });

    unawaited(_startNativeObserver());
    return this;
  }

  @override
  void onClose() {
    _authWorker?.dispose();
    _authWorker = null;
    super.onClose();
  }

  bool get _isSupported => !kIsWeb && defaultTargetPlatform == TargetPlatform.iOS;

  Future<void> _startNativeObserver() async {
    try {
      await _channel.invokeMethod<void>('start');
      // 原生可能在 Flutter 起来之前就收到过 token（卡片还在锁屏上、App 刚被拉起）。
      final pending = await _channel.invokeMapMethod<String, dynamic>('drainPending');
      if (pending == null) return;
      final startToken = pending['start_token']?.toString() ?? '';
      if (startToken.isNotEmpty) {
        _handleStartToken(startToken);
      }
      final tokens = pending['activity_tokens'];
      if (tokens is List) {
        for (final item in tokens) {
          if (item is Map) {
            _handleActivityToken(item);
          }
        }
      }
    } catch (error) {
      debugPrint('LiveActivity native observer failed: $error');
    }
  }

  Future<void> _onMethodCall(MethodCall call) async {
    switch (call.method) {
      case 'onPushToStartToken':
        final args = call.arguments as Map<dynamic, dynamic>?;
        _handleStartToken(args?['token']?.toString() ?? '');
      case 'onActivityToken':
        final args = call.arguments as Map<dynamic, dynamic>?;
        if (args != null) {
          _handleActivityToken(args);
        }
    }
  }

  void _handleStartToken(String token) {
    final normalized = token.trim();
    if (normalized.isEmpty || normalized == LiveActivityStartToken.value) {
      return;
    }
    LiveActivityStartToken.value = normalized;
    // 强制重新注册一次设备，把新的启动 token 带上去。走的是设备注册那条既有链路，
    // 不为一个 token 单开接口。
    if (Get.isRegistered<PushRegistrationService>()) {
      unawaited(
        Get.find<PushRegistrationService>().refreshBindingIfNeeded(force: true),
      );
    }
  }

  void _handleActivityToken(Map<dynamic, dynamic> args) {
    final sessionId = args['session_id']?.toString().trim() ?? '';
    final activityId = args['activity_id']?.toString().trim() ?? '';
    final token = args['token']?.toString().trim() ?? '';
    if (sessionId.isEmpty || activityId.isEmpty || token.isEmpty) return;

    final payload = {
      'session_id': sessionId,
      'activity_id': activityId,
      'token': token,
    };
    if (!Get.find<AuthService>().isLoggedIn) {
      _pendingActivityTokens[sessionId] = payload;
      return;
    }
    unawaited(_reportActivityToken(payload));
  }

  Future<void> _flushPendingActivityTokens() async {
    if (_pendingActivityTokens.isEmpty) return;
    final pending = List<Map<String, String>>.from(_pendingActivityTokens.values);
    _pendingActivityTokens.clear();
    for (final payload in pending) {
      await _reportActivityToken(payload);
    }
  }

  Future<void> _reportActivityToken(Map<String, String> payload) async {
    final deviceId = await DeviceIdentity.resolveDeviceId();
    if (deviceId.isEmpty) return;
    try {
      await _dio.post(
        '/live_activities/token',
        data: {...payload, 'device_id': deviceId},
      );
    } catch (error) {
      // 报不上去就算了：这张卡最多停在当前状态，下一次 run 会重开一张。
      // 重试在这里没有价值——token 只在这张卡活着的时候有用。
      debugPrint('LiveActivity token report failed: $error');
    }
  }
}

/// 当前设备的实时活动启动 token。
///
/// 放成一个独立的静态持有者，而不是让 [PushRegistrationService] 反查
/// [LiveActivityService]：两个服务的初始化先后不定，谁先起来都不该拿不到值。
class LiveActivityStartToken {
  LiveActivityStartToken._();

  static String value = '';
}

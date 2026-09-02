import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:permission_handler/permission_handler.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../shared/utils/app_runtime_endpoints.dart';
import '../../shared/utils/device_identity.dart';
import 'auth_service.dart';
import 'web_push_registration_stub.dart'
    if (dart.library.js_interop) 'web_push_registration_web.dart'
    as web_push_registration;

class PushRegistrationService extends GetxService {
  static const _channel = MethodChannel('pub.dhf.grix/push_registration');

  static const _prefLastUserId = 'push_last_user_id';
  static const _prefLastPlatform = 'push_last_platform';
  static const _prefLastPushEnv = 'push_last_push_env';
  static const _prefLastToken = 'push_last_token';
  static const _prefLastDeviceId = 'push_last_device_id';
  static const _prefBindingRev = 'push_binding_rev';

  /// 绑定协议版本。客户端处理 /devices/bind 的方式有实质变化时递增，
  /// 让升级后的客户端强制重新注册一次，而不是一直吃本地缓存
  /// （否则已绑在被关通道上的老设备永远等不到降级）。
  static const _bindingRev = 2;

  /// 后端业务码：该推送通道被塘主关闭，客户端应排除它后取下一条通道。
  static const _codePushChannelDisabled = 10012;

  static const _maxPushRetries = 3;

  /// 原生取 token 的兜底超时。需覆盖原生降级链的最坏耗时
  /// （华为异步轮询 10s + 极光轮询 10s），留出余量。
  static const _androidPushResolveTimeout = Duration(seconds: 30);

  final Dio _dio = Dio(
    BaseOptions(
      baseUrl: AppRuntimeEndpoints.apiBaseUrl,
      connectTimeout: const Duration(seconds: 10),
      sendTimeout: const Duration(seconds: 10),
      receiveTimeout: const Duration(seconds: 10),
    ),
  );

  SharedPreferences? _prefs;
  Worker? _authWorker;
  Future<void>? _refreshFuture;

  /// Notification permission state for web platforms.
  /// Values: 'granted', 'denied', 'default', 'unsupported'
  final permissionState = 'default'.obs;

  Future<PushRegistrationService> init() async {
    _prefs = await SharedPreferences.getInstance();

    final authService = Get.find<AuthService>();
    authService.attachAuthInterceptor(_dio);

    _authWorker = ever<bool>(authService.isLoggedInRx, (isLoggedIn) {
      if (!isLoggedIn) {
        _clearCachedBinding();
        return;
      }
      checkPermissionState();
      unawaited(refreshBindingIfNeeded(force: true));
    });

    if (authService.isLoggedIn) {
      unawaited(refreshBindingIfNeeded());
    }
    checkPermissionState();
    return this;
  }

  @override
  void onClose() {
    _authWorker?.dispose();
    super.onClose();
  }

  /// Check current notification permission state (web only).
  void checkPermissionState() {
    if (!kIsWeb) return;
    try {
      permissionState.value = web_push_registration.notificationPermissionState;
    } catch (_) {
      permissionState.value = 'unsupported';
    }
  }

  /// Request notification permission with a user gesture (web only).
  /// Must be called from a tap handler.
  Future<bool> requestPermissionWithGesture() async {
    if (!kIsWeb) return true;
    final result = await web_push_registration
        .requestNotificationPermissionWithGesture();
    permissionState.value = result;
    if (result == 'granted') {
      await refreshBindingIfNeeded(force: true);
      return true;
    }
    return false;
  }

  Future<void> refreshBindingIfNeeded({bool force = false}) async {
    if (!_supportsPushRegistration()) {
      return;
    }
    final authService = Get.find<AuthService>();
    if (!authService.isLoggedIn) {
      return;
    }

    final inflight = _refreshFuture;
    if (inflight != null) {
      return inflight;
    }

    final future = _refreshBindingInternal(force: force);
    _refreshFuture = future;
    await future.whenComplete(() {
      if (identical(_refreshFuture, future)) {
        _refreshFuture = null;
      }
    });
  }

  bool _supportsPushRegistration() {
    if (kIsWeb) {
      return web_push_registration.isWebPushBindingSupported;
    }
    switch (defaultTargetPlatform) {
      case TargetPlatform.android:
      case TargetPlatform.iOS:
        return true;
      default:
        return false;
    }
  }

  Future<void> _refreshBindingInternal({required bool force}) async {
    // 后端明确拒绝（通道已关闭）的通道，重新解析时跳过，落到下一条投得出去的通道。
    final disabledPlatforms = <String>{};
    for (int attempt = 0; attempt <= _maxPushRetries; attempt++) {
      final binding = await _resolveBinding(disabledPlatforms);
      if (binding == null) {
        // If web and permission not granted, don't retry – permanent failure
        // until user grants permission via UI.
        if (kIsWeb &&
            web_push_registration.notificationPermissionState != 'granted') {
          return;
        }
        if (attempt < _maxPushRetries) {
          final delay = _retryDelay(attempt);
          debugPrint(
            'Push binding resolve failed, retrying in '
            '${delay.inMilliseconds}ms (attempt ${attempt + 1}/$_maxPushRetries)',
          );
          await Future<void>.delayed(delay);
          continue;
        }
        return;
      }

      final authService = Get.find<AuthService>();
      final userId = authService.userId ?? '';
      if (userId.isEmpty) {
        return;
      }

      if (!force && _isSameBinding(userId, binding)) {
        return;
      }

      try {
        final response = await _dio.post(
          '/devices/bind',
          data: {
            'platform': binding.platform,
            'push_env': binding.pushEnv,
            'device_token': binding.deviceToken,
            'device_id': binding.deviceId,
            if (binding.channelTrail != null &&
                binding.channelTrail!.isNotEmpty)
              'channel_trail': binding.channelTrail,
          },
        );
        final body = response.data;
        final ok =
            response.statusCode == 200 &&
            body is Map &&
            body['code'].toString() == '0';
        if (!ok) {
          debugPrint('Push device bind failed: ${response.data}');
          return;
        }
        await _cacheBinding(userId, binding);
        return;
      } on DioException catch (error) {
        final disabledPlatform = _disabledPlatformFrom(error.response?.data);
        if (disabledPlatform != null &&
            disabledPlatforms.add(disabledPlatform)) {
          debugPrint(
            'Push channel $disabledPlatform disabled by server, '
            'falling back to the next channel',
          );
          // 通道降级不占用网络重试次数；disabledPlatforms 只增不减，循环必然收敛。
          attempt--;
          continue;
        }
        if (_isRetryableDioError(error) && attempt < _maxPushRetries) {
          final delay = _retryDelay(attempt);
          debugPrint(
            'Push device bind network error, retrying in '
            '${delay.inMilliseconds}ms: ${error.message}',
          );
          await Future<void>.delayed(delay);
          continue;
        }
        debugPrint('Push device bind request failed: ${error.message}');
        return;
      } catch (error) {
        debugPrint('Push device bind failed: $error');
        return;
      }
    }
  }

  /// 解析"通道被关闭"的拒绝响应，返回应排除的通道；不是该场景时返回 null。
  String? _disabledPlatformFrom(dynamic body) {
    if (body is! Map) {
      return null;
    }
    if (body['code'].toString() != '$_codePushChannelDisabled') {
      return null;
    }
    final data = body['data'];
    final platform = data is Map
        ? data['disabled_platform']?.toString().trim() ?? ''
        : '';
    return platform.isEmpty ? null : platform;
  }

  Duration _retryDelay(int attempt) {
    final exp = attempt.clamp(0, 5);
    final seconds = (1 * (1 << exp)).clamp(1, 8);
    return Duration(seconds: seconds);
  }

  bool _isRetryableDioError(DioException error) {
    switch (error.type) {
      case DioExceptionType.connectionTimeout:
      case DioExceptionType.sendTimeout:
      case DioExceptionType.receiveTimeout:
      case DioExceptionType.connectionError:
        return true;
      case DioExceptionType.badResponse:
        return error.response?.statusCode != null &&
            error.response!.statusCode! >= 500;
      default:
        return false;
    }
  }

  Future<_PushBinding?> _resolveBinding(Set<String> disabledPlatforms) async {
    if (kIsWeb) {
      return _resolveWebBinding();
    }

    switch (defaultTargetPlatform) {
      case TargetPlatform.iOS:
        return _resolveIOSBinding();
      case TargetPlatform.android:
        return _resolveAndroidBinding(disabledPlatforms);
      default:
        return null;
    }
  }

  Future<_PushBinding?> _resolveWebBinding() async {
    try {
      final result = await web_push_registration.resolveWebPushBinding();
      return _bindingFromMap(result);
    } catch (error) {
      debugPrint('Web push registration failed: $error');
      return null;
    }
  }

  Future<_PushBinding?> _resolveIOSBinding() async {
    try {
      final result = await _channel.invokeMapMethod<String, dynamic>(
        'registerApplePush',
      );
      return _bindingFromMap(result);
    } on PlatformException catch (error) {
      debugPrint('APNs registration failed: ${error.code} ${error.message}');
      return null;
    }
  }

  Future<_PushBinding?> _resolveAndroidBinding(
    Set<String> disabledPlatforms,
  ) async {
    final permission = await Permission.notification.request();
    if (!permission.isGranted) {
      debugPrint('Notification permission not granted on Android');
      return null;
    }

    try {
      // 兜底超时：原生侧的通道降级链会逐条尝试取 token，其中厂商 SDK 的取值调用
      // 自身没有超时（如 HMS Core 卡死时 getToken 不返回）。若原生一直不回调，
      // 这里的 Future 会永远挂起，而在途保护会让后续所有重试短路，
      // 推送注册就此永久卡死。宁可超时后按失败重试。
      final result = await _channel
          .invokeMapMethod<String, dynamic>('resolveAndroidPushInfo', {
            'excludedPlatforms': disabledPlatforms.toList(),
          })
          .timeout(_androidPushResolveTimeout);
      return _bindingFromMap(result);
    } on TimeoutException {
      debugPrint('Android push provider resolution timed out');
      return null;
    } on PlatformException catch (error) {
      debugPrint(
        'Android push provider resolution failed: ${error.code} ${error.message}',
      );
      return null;
    }
  }

  Future<_PushBinding?> _bindingFromMap(Map<String, dynamic>? data) async {
    if (data == null) {
      return null;
    }
    final platform = data['platform']?.toString().trim() ?? '';
    final pushEnv = data['pushEnv']?.toString().trim() ?? '';
    final deviceToken = data['deviceToken']?.toString().trim() ?? '';
    if (platform.isEmpty || pushEnv.isEmpty || deviceToken.isEmpty) {
      return null;
    }
    final deviceId = await DeviceIdentity.resolveDeviceId();
    return _PushBinding(
      platform: platform,
      pushEnv: pushEnv,
      deviceToken: deviceToken,
      deviceId: deviceId,
      channelTrail: data['channelTrail']?.toString().trim(),
    );
  }

  bool _isSameBinding(String userId, _PushBinding binding) {
    final prefs = _prefs;
    if (prefs == null) {
      return false;
    }
    return prefs.getInt(_prefBindingRev) == _bindingRev &&
        prefs.getString(_prefLastUserId) == userId &&
        prefs.getString(_prefLastPlatform) == binding.platform &&
        prefs.getString(_prefLastPushEnv) == binding.pushEnv &&
        prefs.getString(_prefLastToken) == binding.deviceToken &&
        prefs.getString(_prefLastDeviceId) == binding.deviceId;
  }

  Future<void> _cacheBinding(String userId, _PushBinding binding) async {
    final prefs = _prefs;
    if (prefs == null) {
      return;
    }
    await prefs.setString(_prefLastUserId, userId);
    await prefs.setString(_prefLastPlatform, binding.platform);
    await prefs.setString(_prefLastPushEnv, binding.pushEnv);
    await prefs.setString(_prefLastToken, binding.deviceToken);
    await prefs.setString(_prefLastDeviceId, binding.deviceId);
    await prefs.setInt(_prefBindingRev, _bindingRev);
  }

  Future<void> _clearCachedBinding() async {
    final prefs = _prefs;
    if (prefs == null) {
      return;
    }
    await prefs.remove(_prefLastUserId);
    await prefs.remove(_prefLastPlatform);
    await prefs.remove(_prefLastPushEnv);
    await prefs.remove(_prefLastToken);
    await prefs.remove(_prefLastDeviceId);
    await prefs.remove(_prefBindingRev);
  }
}

class _PushBinding {
  const _PushBinding({
    required this.platform,
    required this.pushEnv,
    required this.deviceToken,
    required this.deviceId,
    this.channelTrail,
  });

  final String platform;
  final String pushEnv;
  final String deviceToken;
  final String deviceId;

  /// Android 原生通道降级链的失败记录（如 "android_huawei:timeout"），
  /// 随注册请求带给后端打日志，用于排查"本该走厂商通道却兜底到极光"一类问题。
  /// 仅 Android 会填充，其余平台恒为 null。
  final String? channelTrail;
}

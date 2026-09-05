import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// 把「手表专属的 access + refresh token + 后端地址」交给 iOS 原生，由原生用
/// `WCSession.updateApplicationContext` 同步到 Apple Watch。
///
/// 这里传的 refresh token 来自 `POST /v1/auth/watch/issue`，属于手表自己的
/// refresh 家族：refresh token 每次使用都会轮转并作废整条家族，两台设备共用一份
/// 会互相踢下线，各自一份就互不影响。手机自己的 refresh token 永远不外传。
class WatchCredentialSync {
  const WatchCredentialSync._();

  static const MethodChannel _channel = MethodChannel(
    'pub.dhf.grix/watch_session',
  );

  /// 仅 iOS 有手表伴侣端；其它平台（含 Web / 测试）直接跳过。
  /// 调用方据此避免为没有手表的平台白跑一次签发请求。
  static bool get isSupported =>
      debugSupportedOverride ?? (!kIsWeb && Platform.isIOS);

  /// 单测在非 iOS 宿主上跑，用它冒充有手表的平台。
  @visibleForTesting
  static bool? debugSupportedOverride;

  /// 注册原生 → Dart 的反向回调。
  ///
  /// 手机冷启动、回到前台、用户刚在手表上装好 App，或手表自己发现没有可用凭证
  /// 时，原生侧都会通过同一条 channel 调 `ensureCredentials` 把补推顶起来——
  /// 手表凭证只在登录那一刻签发是不够的：已登录的用户不会再登录第二次。
  ///
  /// [watchRequested] 为 true 表示是手表主动索要的，此时手机若已退出登录要回一份
  /// 空凭证，让手表把陈旧的 token 丢掉。
  static void registerEnsureHandler(
    Future<void> Function({required bool watchRequested}) handler,
  ) {
    if (!isSupported) return;
    _channel.setMethodCallHandler((call) async {
      if (call.method != 'ensureCredentials') return null;
      final args = call.arguments;
      final watchRequested =
          args is Map && args['watch_requested'] == true;
      await handler(watchRequested: watchRequested);
      return null;
    });
    // 冷启动时原生的检查可能早于这次注册（Flutter 引擎起来得比 AuthService.init
    // 早），那一次会打空。注册完主动让原生复查一遍，补上这个窗口。
    unawaited(_invoke('checkCredentialsState', const <String, dynamic>{}));
  }

  /// 登录成功并为手表签发凭证后调用。[apiBaseUrl] 是 REST 接口根（含 /v1），
  /// [wsBaseUrl] 是 ws 服务的 HTTPS 根（手表用它请求 /v1/owner-action）。
  static Future<void> push({
    required String accessToken,
    required String refreshToken,
    required String apiBaseUrl,
    required String wsBaseUrl,
    required int accessExpiresAtMs,
  }) async {
    if (!isSupported) return;
    await _invoke('syncCredentials', <String, dynamic>{
      'access_token': accessToken,
      'refresh_token': refreshToken,
      'api_base_url': apiBaseUrl,
      'ws_base_url': wsBaseUrl,
      'access_expires_at_ms': accessExpiresAtMs,
    });
  }

  /// 退出登录时清空手表上的凭证，否则手表会继续拿着一枚仍然有效的 token。
  static Future<void> clear() async {
    if (!isSupported) return;
    await _invoke('clearCredentials', const <String, dynamic>{});
  }

  static Future<void> _invoke(String method, Map<String, dynamic> args) async {
    try {
      await _channel.invokeMethod<void>(method, args);
    } on MissingPluginException {
      // 老版本原生壳没有这个 channel，静默跳过。
    } on PlatformException catch (e) {
      debugPrint('⌚️ watch credential sync failed: ${e.message}');
    }
  }
}

/// 把 ws 端点（wss://host/ws）转换成 ws 服务的 HTTPS 根（https://host）。
/// 手表不连 WebSocket，只对 ws 服务发 HTTPS 请求（/v1/owner-action）。
String watchWsHttpBaseUrl(String wsUrl) {
  final uri = Uri.tryParse(wsUrl.trim());
  if (uri == null || uri.host.isEmpty) return '';
  final secure = uri.scheme == 'wss' || uri.scheme == 'https';
  final port = uri.hasPort ? ':${uri.port}' : '';
  return '${secure ? 'https' : 'http'}://${uri.host}$port';
}

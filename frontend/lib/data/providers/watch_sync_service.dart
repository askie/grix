import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// 把「当前 access token + 后端地址」交给 iOS 原生，由原生用
/// `WCSession.updateApplicationContext` 同步到 Apple Watch。
///
/// 只传 access token：refresh token 每次使用都会轮转并作废整条家族，手表和手机
/// 共用一个 refresh 家族会互相踢下线。手表因此只在 access token 有效期内可用，
/// 过期后由手机下一次登录/刷新重新同步。
class WatchCredentialSync {
  const WatchCredentialSync._();

  static const MethodChannel _channel = MethodChannel(
    'pub.dhf.grix/watch_session',
  );

  /// 仅 iOS 有手表伴侣端；其它平台（含 Web / 测试）直接跳过。
  static bool get _supported => !kIsWeb && Platform.isIOS;

  /// 登录或刷新成功后调用。[apiBaseUrl] 是 REST 接口根（含 /v1），
  /// [wsBaseUrl] 是 ws 服务的 HTTPS 根（手表用它请求 /v1/owner-action）。
  static Future<void> push({
    required String accessToken,
    required String apiBaseUrl,
    required String wsBaseUrl,
    required int accessExpiresAtMs,
  }) async {
    if (!_supported) return;
    await _invoke('syncCredentials', <String, dynamic>{
      'access_token': accessToken,
      'api_base_url': apiBaseUrl,
      'ws_base_url': wsBaseUrl,
      'access_expires_at_ms': accessExpiresAtMs,
    });
  }

  /// 退出登录时清空手表上的凭证，否则手表会继续拿着一枚仍然有效的 token。
  static Future<void> clear() async {
    if (!_supported) return;
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

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';

import 'tailnet_cert_pin.dart';
import 'tailnet_host.dart';

/// 改写 tailnet 自签 HTTPS 媒体 URL，让原生播放器走本机 loopback HTTP 反代。
///
/// 为什么需要：图片走 `Image.network` → `dart:io HttpClient` →
/// `HttpOverrides.global` 信任 Grix 自签 CA，所以 in-app 零安装就能看；
/// 但 video_player 在 iOS/Android/macOS 是原生播放器栈，绕过 `HttpClient`，
/// 系统证书库不认我们的 CA，整段拒绝。本反代把外层换成明文 loopback，
/// 反代里发出的请求继续走 `HttpClient` 命中 `HttpOverrides`，自签放行。
///
/// 仅对 tailnet (100.64.0.0/10) + scheme https 的 URL 改写，
/// 其他地址原样返回，不影响外部网站。
Future<Uri> rewriteTailnetMediaUrl(Uri original) async {
  if (!_shouldProxy(original)) return original;
  final base = await _LocalTailnetMediaProxy.instance.ensureBase();
  final token = base64Url.encode(utf8.encode(original.toString()));
  return base.replace(
    path: '/u',
    queryParameters: {'t': token},
  );
}

typedef ProxyEligibility = bool Function(Uri uri);

bool _defaultShouldProxy(Uri uri) {
  if (uri.scheme.toLowerCase() != 'https') return false;
  if (uri.host.isEmpty) return false;
  return isTailnetHostString(uri.host);
}

ProxyEligibility _shouldProxy = _defaultShouldProxy;

/// 测试钩子：替换"要不要反代"的判定函数。
///
/// 仅用于本仓库的反代端到端单测（在 loopback 上模拟一个上游服务）。
/// 传 `null` 恢复默认（仅放行 tailnet + https）。生产代码绝不调用。
@visibleForTesting
void debugSetProxyEligibilityOverride(ProxyEligibility? override) {
  _shouldProxy = override ?? _defaultShouldProxy;
}

class _LocalTailnetMediaProxy with WidgetsBindingObserver {
  _LocalTailnetMediaProxy._();
  static final _LocalTailnetMediaProxy instance = _LocalTailnetMediaProxy._();

  HttpServer? _server;
  Future<HttpServer>? _starting;
  Future<void>? _verifying;
  int? _preferredPort;
  bool _lifecycleRegistered = false;
  late final HttpClient _client = _buildClient();

  HttpClient _buildClient() {
    final c = HttpClient();
    // 上游 tailnet 服务有时响应较慢（首次签发/磁盘读盘），给宽松超时。
    c.connectionTimeout = const Duration(seconds: 10);
    c.idleTimeout = const Duration(seconds: 30);
    // badCertificateCallback 在这里再加一道保险：当 HttpOverrides 因任何原因
    // 没装上（如测试环境），仍按同样规则只放行 tailnet + Grix CA 指纹钉扎。
    c.badCertificateCallback = (cert, host, port) {
      return trustTailnetSelfSignedCert(cert, host);
    };
    return c;
  }

  void _ensureLifecycleObserver() {
    if (_lifecycleRegistered) return;
    try {
      WidgetsBinding.instance.addObserver(this);
      _lifecycleRegistered = true;
    } catch (_) {}
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _verifying = _verifyOrRestart().whenComplete(() => _verifying = null);
    }
  }

  Future<void> _verifyOrRestart() async {
    final server = _server;
    if (server == null) return;
    bool alive = false;
    try {
      final probe = await Socket.connect(
        server.address,
        server.port,
        timeout: const Duration(seconds: 2),
      );
      alive = true;
      try {
        await probe.close();
      } catch (_) {}
    } catch (_) {}
    if (!alive) {
      debugPrint('LocalTailnetMediaProxy: server dead after resume, restarting');
      _preferredPort = server.port;
      _server = null;
      _starting = null;
      try {
        await server.close(force: true);
      } catch (_) {}
    }
  }

  Future<Uri> ensureBase() async {
    final pending = _verifying;
    if (pending != null) await pending;
    final server = await _ensureStarted();
    return Uri(
      scheme: 'http',
      host: server.address.address,
      port: server.port,
    );
  }

  Future<HttpServer> _ensureStarted() {
    final existing = _server;
    if (existing != null) return Future.value(existing);
    return _starting ??= _start();
  }

  Future<HttpServer> _start() async {
    _ensureLifecycleObserver();
    try {
      final port = _preferredPort ?? 0;
      _preferredPort = null;
      HttpServer server;
      try {
        server = await HttpServer.bind(
          InternetAddress.loopbackIPv4, port, shared: false,
        );
      } catch (_) {
        if (port != 0) {
          server = await HttpServer.bind(
            InternetAddress.loopbackIPv4, 0, shared: false,
          );
        } else {
          rethrow;
        }
      }
      server.autoCompress = false;
      _server = server;
      server.listen(
        _handle,
        onError: (Object e, StackTrace s) {
          debugPrint('LocalTailnetMediaProxy listen error: $e');
        },
        onDone: () {
          debugPrint(
            'LocalTailnetMediaProxy server closed, '
            'will restart on next request',
          );
          if (_server == server) {
            _preferredPort = server.port;
            _server = null;
            _starting = null;
          }
        },
      );
      return server;
    } catch (e) {
      _starting = null;
      rethrow;
    }
  }

  Future<void> _handle(HttpRequest req) async {
    final res = req.response;
    HttpClientResponse? upstreamRes;
    try {
      if (req.uri.path != '/u') {
        res.statusCode = HttpStatus.notFound;
        await res.close();
        return;
      }
      final token = req.uri.queryParameters['t'];
      if (token == null || token.isEmpty) {
        res.statusCode = HttpStatus.badRequest;
        await res.close();
        return;
      }
      late final Uri target;
      try {
        target = Uri.parse(utf8.decode(base64Url.decode(token)));
      } catch (_) {
        res.statusCode = HttpStatus.badRequest;
        await res.close();
        return;
      }
      if (!_shouldProxy(target)) {
        res.statusCode = HttpStatus.forbidden;
        await res.close();
        return;
      }

      final upstreamReq = await _client.openUrl(req.method, target);
      upstreamReq.followRedirects = true;
      _copyRequestHeaders(req.headers, upstreamReq.headers);
      if (req.method != 'GET' && req.method != 'HEAD') {
        await upstreamReq.addStream(req);
      }
      upstreamRes = await upstreamReq.close();

      res.statusCode = upstreamRes.statusCode;
      _copyResponseHeaders(upstreamRes.headers, res.headers);
      await upstreamRes.pipe(res);
      upstreamRes = null;
    } catch (e, s) {
      debugPrint('LocalTailnetMediaProxy handle error: $e\n$s');
      if (upstreamRes != null) {
        try {
          await upstreamRes.drain<void>();
        } catch (_) {}
      }
      try {
        res.statusCode = HttpStatus.badGateway;
      } catch (_) {}
      try {
        await res.close();
      } catch (_) {}
    }
  }

  void _copyRequestHeaders(HttpHeaders from, HttpHeaders to) {
    from.forEach((name, values) {
      final lower = name.toLowerCase();
      // Host 必须由上游栈基于 target 自填；其它逐跳/连接管理头不能透传。
      if (_blockedReqHeaders.contains(lower)) return;
      to.set(name, values);
    });
  }

  void _copyResponseHeaders(HttpHeaders from, HttpHeaders to) {
    from.forEach((name, values) {
      final lower = name.toLowerCase();
      if (_blockedRespHeaders.contains(lower)) return;
      to.set(name, values);
    });
  }

  static const _blockedReqHeaders = <String>{
    'host',
    'connection',
    'keep-alive',
    'proxy-connection',
    'transfer-encoding',
    'upgrade',
    'te',
    'trailer',
    'expect',
    'content-length',
  };

  static const _blockedRespHeaders = <String>{
    'connection',
    'keep-alive',
    'transfer-encoding',
    'upgrade',
    'te',
    'trailer',
  };
}

@visibleForTesting
bool debugShouldProxy(Uri uri) => _shouldProxy(uri);

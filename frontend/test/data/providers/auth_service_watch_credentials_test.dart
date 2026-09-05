import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/watch_sync_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 记录请求并按路径给固定应答；[beforeRespond] 用来在「签发请求还没回来」的那个
/// 窗口里改动会话状态。
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter({this.beforeRespond, this.issueStatus = 200});

  final Future<void> Function(RequestOptions options)? beforeRespond;

  /// `/auth/watch/issue` 的应答状态码；401 用来复现「请求没带鉴权头」的后果。
  final int issueStatus;
  final requests = <String>[];
  final issueHeaders = <Map<String, dynamic>>[];

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options.uri.path);
    await beforeRespond?.call(options);
    if (options.uri.path.endsWith('/auth/watch/issue')) {
      issueHeaders.add(Map<String, dynamic>.from(options.headers));
      if (issueStatus != 200) {
        return _json({
          'code': 40100,
          'msg': 'unauthorized',
        }, status: issueStatus);
      }
      return _json({
        'code': 0,
        'data': {
          'access_token': 'watch_access',
          'refresh_token': 'watch_refresh',
          'expires_in': 7200,
        },
      });
    }
    return _json({'code': 0, 'data': {}});
  }
}

ResponseBody _json(Map<String, dynamic> body, {int status = 200}) =>
    ResponseBody.fromString(
  jsonEncode(body),
  status,
  headers: {
    Headers.contentTypeHeader: [Headers.jsonContentType],
  },
);

int _issueCount(_FakeAdapter adapter) =>
    adapter.requests.where((p) => p.endsWith('/auth/watch/issue')).length;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const channel = MethodChannel('pub.dhf.grix/watch_session');
  final nativeCalls = <MethodCall>[];

  setUp(() {
    Get.testMode = true;
    Get.reset();
    nativeCalls.clear();
    // 单测跑在非 iOS 宿主上，冒充一台有手表的 iPhone。
    WatchCredentialSync.debugSupportedOverride = true;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          nativeCalls.add(call);
          return null;
        });
  });

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
    WatchCredentialSync.debugSupportedOverride = null;
    Get.reset();
  });

  Future<AuthService> buildService(_FakeAdapter adapter) async {
    final service = AuthService();
    service.dioForTest.httpClientAdapter = adapter;
    await service.init();
    return service;
  }

  List<MethodCall> callsNamed(String method) =>
      nativeCalls.where((c) => c.method == method).toList();

  test('logged-out phone never issues watch credentials', () async {
    SharedPreferences.setMockInitialValues({});
    final adapter = _FakeAdapter();
    final service = await buildService(adapter);

    expect(service.isLoggedIn, isFalse);
    await service.ensureWatchCredentials();

    expect(_issueCount(adapter), 0);
    expect(callsNamed('syncCredentials'), isEmpty);
  });

  test(
    'logged-out phone answers a watch request with empty credentials',
    () async {
      SharedPreferences.setMockInitialValues({});
      final adapter = _FakeAdapter();
      final service = await buildService(adapter);

      await service.ensureWatchCredentials(watchRequested: true);

      expect(_issueCount(adapter), 0);
      expect(callsNamed('clearCredentials'), hasLength(1));
    },
  );

  test('native-triggered ensure issues once and then throttles', () async {
    SharedPreferences.setMockInitialValues({
      'access_token': 'stored_access',
      'refresh_token': 'stored_refresh',
      'access_expires_at_ms': 9999999999999,
      'user_id': '1001',
      'username': 'tester',
      'nickname': 'Tester',
    });
    final adapter = _FakeAdapter();
    final service = await buildService(adapter);
    expect(service.isLoggedIn, isTrue);

    await service.ensureWatchCredentials();
    expect(_issueCount(adapter), 1);

    final pushes = callsNamed('syncCredentials');
    expect(pushes, hasLength(1));
    final args = Map<String, dynamic>.from(pushes.single.arguments as Map);
    expect(args['access_token'], 'watch_access');
    // 手机自己的 refresh token 永远不外传。
    expect(args['refresh_token'], 'watch_refresh');
    expect(args['refresh_token'], isNot('stored_refresh'));

    // 手表反复刷新页面时会连着索要，60 秒内只能签发一次：每次 issue 都会撤销
    // 上一条家族，连发会把刚推出去的那份作废。
    await service.ensureWatchCredentials();
    await service.ensureWatchCredentials(watchRequested: true);
    expect(_issueCount(adapter), 1);
    expect(callsNamed('syncCredentials'), hasLength(1));
  });

  test('a fresh login issues watch credentials for the watch', () async {
    // _applyAuthPayload 先落 token 再置 isLoggedIn，签发是在这两步之间发出的：
    // 拿登录态当门槛会把「退出再重登」这条唯一有效的路径重新堵死。
    SharedPreferences.setMockInitialValues({});
    final adapter = _FakeAdapter();
    final service = await buildService(adapter);
    expect(service.isLoggedIn, isFalse);

    final applied = await service.applyAuthPayloadForTest({
      'access_token': 'fresh_access',
      'refresh_token': 'fresh_refresh',
      'expires_in': 7200,
      'user': {'id': '1001', 'username': 'tester', 'nickname': 'Tester'},
    });
    expect(applied, isTrue);
    // 签发是 unawaited 的（不能阻塞登录流程），等它落地。
    await Future<void>.delayed(const Duration(milliseconds: 50));

    expect(_issueCount(adapter), 1);
    expect(adapter.issueHeaders.single['Authorization'], 'Bearer fresh_access');
    expect(callsNamed('syncCredentials'), hasLength(1));
  });

  test('watch issue request carries the granted access token', () async {
    SharedPreferences.setMockInitialValues({
      'access_token': 'stored_access',
      'refresh_token': 'stored_refresh',
      'access_expires_at_ms': 9999999999999,
      'user_id': '1001',
      'username': 'tester',
      'nickname': 'Tester',
    });
    final adapter = _FakeAdapter();
    final service = await buildService(adapter);

    await service.ensureWatchCredentials();

    // AuthService 的 _dio 没有鉴权拦截器：漏掉这个头，服务端的 middleware.Auth()
    // 必然回 401，手表一份凭证都收不到。
    expect(adapter.issueHeaders, hasLength(1));
    expect(adapter.issueHeaders.single['Authorization'], 'Bearer stored_access');
  });

  test('a rejected watch issue never pushes credentials', () async {
    SharedPreferences.setMockInitialValues({
      'access_token': 'stored_access',
      'refresh_token': 'stored_refresh',
      'access_expires_at_ms': 9999999999999,
      'user_id': '1001',
      'username': 'tester',
      'nickname': 'Tester',
    });
    final adapter = _FakeAdapter(issueStatus: 401);
    final service = await buildService(adapter);

    await service.ensureWatchCredentials();

    expect(_issueCount(adapter), 1);
    expect(callsNamed('syncCredentials'), isEmpty);
  });

  test('credentials issued for a session that ended are dropped', () async {
    SharedPreferences.setMockInitialValues({
      'access_token': 'stored_access',
      'refresh_token': 'stored_refresh',
      'access_expires_at_ms': 9999999999999,
      'user_id': '1001',
      'username': 'tester',
      'nickname': 'Tester',
    });
    late AuthService service;
    final adapter = _FakeAdapter(
      beforeRespond: (options) async {
        if (!options.uri.path.endsWith('/auth/watch/issue')) return;
        // 签发还在路上时用户退出了登录：这一份不能再推出去，也不能盖掉
        // logout 推的那份空凭证。
        await service.logout(notifyServer: false);
      },
    );
    service = await buildService(adapter);

    await service.ensureWatchCredentials();

    expect(_issueCount(adapter), 1);
    expect(callsNamed('syncCredentials'), isEmpty);
    expect(callsNamed('clearCredentials'), isNotEmpty);
  });
}

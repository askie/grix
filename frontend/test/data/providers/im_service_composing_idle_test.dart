import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

/// 回归场景：人类输入框有内容后人离开电脑，composing 续期循环会无限刷新
/// 服务端 TTL，对端一直显示"正在输入"。
/// 修复后本地增加空闲超时：每次文本变化驱动的 updateSessionComposing 都会
/// 重新计时，超时未再输入则主动发出 active:false 结束 composing。

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';

  @override
  String? get token => 'test_access_token';

  @override
  bool hasUsableAccessToken({Duration minRemaining = Duration.zero}) => true;

  @override
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = const Duration(minutes: 5),
  }) async => TokenRefreshStatus.ready;
}

class _FakeSessionService extends SessionService {
  @override
  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    return const SessionSnapshotFetchResult(snapshots: [], success: true);
  }
}

class _RecordingSink implements WebSocketSink {
  final List<Map<String, dynamic>> packets = [];

  @override
  void add(dynamic data) {
    packets.add(jsonDecode(data as String) as Map<String, dynamic>);
  }

  @override
  Future<void> close([int? closeCode, String? closeReason]) async {}

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _FakeWebSocketChannel implements WebSocketChannel {
  _FakeWebSocketChannel({
    required this.ready,
    required Stream<dynamic> stream,
    required WebSocketSink sink,
  }) : _stream = stream,
       _sink = sink;

  @override
  final Future<void> ready;

  final Stream<dynamic> _stream;
  final WebSocketSink _sink;

  @override
  Stream<dynamic> get stream => _stream;

  @override
  WebSocketSink get sink => _sink;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _Env {
  _Env(this.service, this.sink, this.downstream);

  final ImService service;
  final _RecordingSink sink;
  final StreamController<dynamic> downstream;
}

Future<void> expectEventually(
  bool Function() condition, {
  Duration timeout = const Duration(seconds: 5),
  String? reason,
}) async {
  final deadline = DateTime.now().add(timeout);
  while (DateTime.now().isBefore(deadline)) {
    if (condition()) return;
    await Future<void>.delayed(const Duration(milliseconds: 25));
  }
  expect(condition(), isTrue, reason: reason);
}

List<Map<String, dynamic>> composingPackets(
  List<Map<String, dynamic>> packets, {
  required bool active,
}) {
  return packets.where((p) {
    if (p['cmd'] != 'session_activity_set') return false;
    final payload = p['payload'];
    if (payload is! Map) return false;
    return payload['kind'] == 'composing' && payload['active'] == active;
  }).toList();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const idleTimeout = Duration(milliseconds: 300);

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    Get.put<AuthService>(_FakeAuthService());
    Get.put<SessionService>(_FakeSessionService());
    ImService.composingIdleTimeoutForTest = idleTimeout;
  });

  tearDown(() {
    ImService.composingIdleTimeoutForTest = null;
    ImService.channelConnectorForTest = null;
    Get.reset();
  });

  Future<_Env> connectAuthenticatedService() async {
    final sink = _RecordingSink();
    final downstream = StreamController<dynamic>();
    ImService.channelConnectorForTest = (uri) => _FakeWebSocketChannel(
      ready: Future<void>.value(),
      stream: downstream.stream,
      sink: sink,
    );

    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');
    await expectEventually(
      () => sink.packets.any((p) => p['cmd'] == 'auth'),
      reason: '连接后应发出 auth 包',
    );
    downstream.add(
      jsonEncode({
        'cmd': 'auth_ack',
        'payload': {'code': 0, 'user_id': '1001'},
      }),
    );
    await expectEventually(
      () => service.isAuthenticated,
      reason: 'auth_ack code=0 后应进入已鉴权态',
    );
    return _Env(service, sink, downstream);
  }

  test('空闲超时后主动结束 composing 并通知对端', () async {
    final env = await connectAuthenticatedService();

    env.service.updateSessionComposing('s1', active: true);
    await expectEventually(
      () => composingPackets(env.sink.packets, active: true).isNotEmpty,
      reason: '开始输入应发出 composing active:true',
    );
    expect(env.service.isSessionComposingActiveForTest, isTrue);
    expect(env.service.hasComposingIdleTimerForTest, isTrue);

    await expectEventually(
      () => composingPackets(env.sink.packets, active: false).isNotEmpty,
      timeout: const Duration(seconds: 2),
      reason: '空闲超时后应发出 composing active:false',
    );
    expect(env.service.isSessionComposingActiveForTest, isFalse);
    expect(env.service.hasComposingIdleTimerForTest, isFalse);

    env.service.disconnect();
    await env.downstream.close();
  });

  test('超时前再次输入会重置空闲计时', () async {
    final env = await connectAuthenticatedService();

    env.service.updateSessionComposing('s1', active: true);
    await expectEventually(
      () => composingPackets(env.sink.packets, active: true).isNotEmpty,
      reason: '开始输入应发出 composing active:true',
    );

    // 在超时前再次输入（模拟文本变化），空闲计时应重新开始。
    await Future<void>.delayed(const Duration(milliseconds: 200));
    env.service.updateSessionComposing('s1', active: true);
    // 等到越过原截止时间（300ms）但离新截止时间（500ms）仍有 150ms 余量。
    await Future<void>.delayed(const Duration(milliseconds: 150));
    expect(
      composingPackets(env.sink.packets, active: false),
      isEmpty,
      reason: '重新输入后原截止点不应结束 composing',
    );
    expect(env.service.isSessionComposingActiveForTest, isTrue);

    await expectEventually(
      () => composingPackets(env.sink.packets, active: false).isNotEmpty,
      timeout: const Duration(seconds: 2),
      reason: '最后一次输入后仍会按新截止时间结束 composing',
    );

    env.service.disconnect();
    await env.downstream.close();
  });

  test('快速连续输入持续重置空闲计时且不重复发包', () async {
    final env = await connectAuthenticatedService();

    env.service.updateSessionComposing('s1', active: true);
    await expectEventually(
      () => composingPackets(env.sink.packets, active: true).isNotEmpty,
      reason: '开始输入应发出 composing active:true',
    );
    final activeTrueBaseline = composingPackets(
      env.sink.packets,
      active: true,
    ).length;

    // 模拟击键间隔 <500ms 防抖窗口的持续输入：每次触达都应重置空闲计时，
    // 总时长 500ms 已远超 300ms 空闲超时，但 composing 不应被掐掉。
    for (var i = 0; i < 5; i++) {
      await Future<void>.delayed(const Duration(milliseconds: 100));
      env.service.updateSessionComposing('s1', active: true);
    }
    expect(
      composingPackets(env.sink.packets, active: false),
      isEmpty,
      reason: '持续输入期间不应结束 composing',
    );
    expect(env.service.isSessionComposingActiveForTest, isTrue);
    expect(
      composingPackets(env.sink.packets, active: true),
      hasLength(activeTrueBaseline),
      reason: '续期中的再次触达不应重复发 active:true',
    );

    // 停止输入后，空闲超时仍应生效。
    await expectEventually(
      () => composingPackets(env.sink.packets, active: false).isNotEmpty,
      timeout: const Duration(seconds: 2),
      reason: '停止输入后空闲超时应结束 composing',
    );

    env.service.disconnect();
    await env.downstream.close();
  });

  test('断开连接会取消空闲计时', () async {
    final env = await connectAuthenticatedService();

    env.service.updateSessionComposing('s1', active: true);
    await expectEventually(
      () => composingPackets(env.sink.packets, active: true).isNotEmpty,
      reason: '开始输入应发出 composing active:true',
    );
    expect(env.service.hasComposingIdleTimerForTest, isTrue);

    env.service.disconnect();
    expect(env.service.hasComposingIdleTimerForTest, isFalse);

    await env.downstream.close();
  });
}

import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

/// 回归场景：iOS 切后台期间 WS 保持连接，切回前台时 ensureConnected 是
/// no-op，没有重连→auth_ack→pull_sync 链路来完成"权威刷新"，被 defer 的
/// 图标角标同步整个前台期都不执行，离线推送写上去的陈旧角标清不掉。
/// 修复后恢复前台调用 reconcileUnreadBadgeOnResume()，已连接已鉴权时必须
/// 主动发出一次 pull_sync 对账；未连接时必须静默不发（重连链路自会补拉）。

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

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    Get.put<AuthService>(_FakeAuthService());
    Get.put<SessionService>(_FakeSessionService());
  });

  tearDown(() {
    ImService.channelConnectorForTest = null;
    Get.reset();
  });

  test('已连接已鉴权时，恢复前台对账必须发出 pull_sync', () async {
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
    // auth_ack 链路自身会触发一次 pull_sync；等它出现后取基线计数。
    await expectEventually(
      () => sink.packets.any((p) => p['cmd'] == 'pull_sync'),
      reason: '鉴权成功链路应触发首个 pull_sync',
    );
    final baseline = sink.packets.where((p) => p['cmd'] == 'pull_sync').length;

    service.reconcileUnreadBadgeOnResume();
    // _triggerPullSyncThrottled 有 2s 节流窗口，最迟应在窗口后发出。
    await expectEventually(
      () =>
          sink.packets.where((p) => p['cmd'] == 'pull_sync').length > baseline,
      reason: '恢复前台对账没有发出新的 pull_sync',
    );

    service.disconnect();
    await downstream.close();
  });

  test('未连接时对账必须静默不发、不抛错', () {
    final service = ImService();
    expect(service.isConnected, isFalse);
    // 不应抛错，也没有任何通道可发——调用本身即验证不崩溃。
    service.reconcileUnreadBadgeOnResume();
    service.onClose();
  });
}

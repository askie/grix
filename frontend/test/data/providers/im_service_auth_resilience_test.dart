import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

class _FakeAuthService extends AuthService {
  TokenRefreshStatus refreshStatus = TokenRefreshStatus.ready;
  int unauthorizedCalls = 0;
  int logoutCalls = 0;
  int refreshCalls = 0;
  bool hasUsableToken = false;

  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';

  @override
  String? get token => 'test_access_token';

  @override
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = const Duration(minutes: 5),
  }) async {
    refreshCalls++;
    return refreshStatus;
  }

  @override
  bool hasUsableAccessToken({Duration minRemaining = Duration.zero}) {
    return hasUsableToken;
  }

  @override
  void handleUnauthorized({String? expectedAccessToken}) {
    unauthorizedCalls++;
  }

  @override
  Future<void> logout({bool notifyServer = true}) async {
    logoutCalls++;
  }
}

class _RecordingSink implements WebSocketSink {
  final packets = <Map<String, dynamic>>[];

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

class _RealtimeEnv {
  _RealtimeEnv(this.service, this.sink, this.downstream);

  final ImService service;
  final _RecordingSink sink;
  final StreamController<dynamic> downstream;
}

Future<Map<String, dynamic>> _readDeletedSessions() async {
  final prefs = await SharedPreferences.getInstance();
  final raw = prefs.getString('deleted_sessions_1001');
  if (raw == null || raw.trim().isEmpty) {
    return <String, dynamic>{};
  }
  final decoded = jsonDecode(raw);
  if (decoded is Map<String, dynamic>) {
    return decoded;
  }
  return Map<String, dynamic>.from(decoded as Map);
}

Future<void> _expectDeletedSessionClearedEventually(String sessionId) async {
  for (var i = 0; i < 10; i++) {
    final deleted = await _readDeletedSessions();
    if (!deleted.containsKey(sessionId)) {
      return;
    }
    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
  final deleted = await _readDeletedSessions();
  expect(deleted.containsKey(sessionId), isFalse);
}

Future<void> _expectDeletedSessionPersistedEventually(
  String sessionId,
  int deletedAtMs,
) async {
  for (var i = 0; i < 10; i++) {
    final deleted = await _readDeletedSessions();
    if (deleted[sessionId] == deletedAtMs) {
      return;
    }
    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
  final deleted = await _readDeletedSessions();
  expect(deleted[sessionId], deletedAtMs);
}

Future<void> _expectEventually(
  bool Function() condition, {
  Duration timeout = const Duration(seconds: 5),
  String? reason,
}) async {
  final deadline = DateTime.now().add(timeout);
  while (DateTime.now().isBefore(deadline)) {
    if (condition()) return;
    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
  expect(condition(), isTrue, reason: reason);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late _FakeAuthService authService;

  Future<void> pumpShell(
    WidgetTester tester, {
    String initialRoute = AppRoutes.login,
  }) async {
    await tester.pumpWidget(
      GetMaterialApp(
        initialRoute: initialRoute,
        getPages: [
          GetPage(
            name: AppRoutes.login,
            page: () => const Scaffold(body: Text('login')),
          ),
          GetPage(
            name: AppRoutes.home,
            page: () => const Scaffold(body: Text('home')),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();
  }

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    authService = _FakeAuthService();
    Get.put<AuthService>(authService);
  });

  tearDown(() {
    ImService.channelConnectorForTest = null;
    ImService.sessionHistoryResetRetryMsForTest = null;
    Get.reset();
  });

  Future<_RealtimeEnv> connectAuthenticatedService() async {
    final sink = _RecordingSink();
    final downstream = StreamController<dynamic>();
    ImService.channelConnectorForTest = (uri) => _FakeWebSocketChannel(
      ready: Future<void>.value(),
      stream: downstream.stream,
      sink: sink,
    );

    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');
    await _expectEventually(
      () => sink.packets.any((p) => p['cmd'] == 'auth'),
      reason: '连接后应发出 auth 包',
    );
    downstream.add(
      jsonEncode({
        'cmd': 'auth_ack',
        'payload': {'code': 0, 'user_id': '1001'},
      }),
    );
    await _expectEventually(
      () => service.isAuthenticated,
      reason: 'auth_ack code=0 后应进入已鉴权态',
    );
    return _RealtimeEnv(service, sink, downstream);
  }

  test('re_auth_ack temporary refresh failure keeps session state', () async {
    authService.refreshStatus = TokenRefreshStatus.temporaryFailure;
    final service = ImService();

    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 're_auth_ack',
        'payload': {'code': 10002, 'msg': '凭证已失效，请重新登录'},
      }),
    );

    expect(authService.unauthorizedCalls, 0);
    expect(service.connectionStage, ImConnectionStage.reconnecting);
  });

  test('re_auth_ack invalid session triggers unauthorized handling', () async {
    authService.refreshStatus = TokenRefreshStatus.invalidSession;
    final service = ImService();

    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 're_auth_ack',
        'payload': {'code': 10002, 'msg': '凭证已失效，请重新登录'},
      }),
    );

    expect(authService.unauthorizedCalls, 1);
  });

  test(
    'triggerAuthForTest only logs out when session is truly invalid',
    () async {
      final service = ImService();

      authService.refreshStatus = TokenRefreshStatus.temporaryFailure;
      await service.triggerAuthForTest();
      expect(authService.unauthorizedCalls, 0);
      expect(service.connectionStage, ImConnectionStage.reconnecting);

      authService.refreshStatus = TokenRefreshStatus.invalidSession;
      await service.triggerAuthForTest();
      expect(authService.unauthorizedCalls, 1);
    },
  );

  test(
    'triggerAuthForTest fast-path skips pre-refresh for usable token',
    () async {
      final service = ImService();

      authService.hasUsableToken = true;
      authService.refreshStatus = TokenRefreshStatus.temporaryFailure;
      await service.triggerAuthForTest();

      expect(authService.refreshCalls, 0);
      expect(authService.unauthorizedCalls, 0);
    },
  );

  test('same_platform_login kick keeps local login state', () async {
    final service = ImService();

    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'kicked',
        'payload': {'reason': 'same_platform_login'},
      }),
    );

    expect(authService.logoutCalls, 0);
    expect(service.connectionStage, ImConnectionStage.kicked);
  });

  testWidgets('auth success on login route redirects to home', (tester) async {
    await pumpShell(tester);
    final service = ImService();

    service.redirectToHomeAfterAuthSuccessForTest();
    await tester.pumpAndSettle();

    expect(Get.currentRoute, AppRoutes.home);
    expect(find.text('home'), findsOneWidget);
  });

  testWidgets(
    'auth_ack success on login route does not navigate inside socket callback',
    (tester) async {
      await pumpShell(tester);
      final service = ImService();

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'auth_ack',
          'payload': {'code': 0, 'user_id': '1001'},
        }),
      );
      await tester.pumpAndSettle();

      expect(Get.currentRoute, AppRoutes.login);
      expect(find.text('login'), findsOneWidget);
    },
  );

  test(
    'session_history_reset_ack permission denied clears local delete mark',
    () async {
      final service = ImService();
      const sid = '846f11de-7a50-4381-a984-217e80121702';
      final deletedAtMs = DateTime.utc(
        2026,
        3,
        17,
        19,
        36,
        10,
      ).millisecondsSinceEpoch;

      service.seedDeletedSessionForTest(sid, deletedAtMs: deletedAtMs);
      expect(service.isSessionLocallyDeletedForTest(sid), isTrue);
      await _expectDeletedSessionPersistedEventually(sid, deletedAtMs);

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_history_reset_ack',
          'payload': {
            'session_id': sid,
            'code': 4003,
            'msg': 'permission denied',
          },
        }),
      );

      expect(service.isSessionLocallyDeletedForTest(sid), isFalse);
      await _expectDeletedSessionClearedEventually(sid);
    },
  );

  test(
    'session_history_reset in-flight state dedupes until ack or retry window',
    () async {
      final service = ImService();
      service.markSessionHistoryResetInFlightForTest(
        's-reset-dedupe',
        sentAtMs: 1000,
        deletedAtMs: 5000,
      );

      expect(
        service.hasRecentSessionHistoryResetInFlightForTest(
          's-reset-dedupe',
          nowMs: 30999,
          deletedAtMs: 5000,
        ),
        isTrue,
      );
      expect(
        service.hasRecentSessionHistoryResetInFlightForTest(
          's-reset-dedupe',
          nowMs: 30999,
          deletedAtMs: 6000,
        ),
        isFalse,
      );
      expect(
        service.hasRecentSessionHistoryResetInFlightForTest(
          's-reset-dedupe',
          nowMs: 31001,
          deletedAtMs: 5000,
        ),
        isFalse,
      );

      service.markSessionHistoryResetInFlightForTest(
        's-reset-dedupe',
        sentAtMs: 2000,
      );
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_history_reset_ack',
          'payload': {'session_id': 's-reset-dedupe', 'code': 0},
        }),
      );

      expect(
        service.hasRecentSessionHistoryResetInFlightForTest(
          's-reset-dedupe',
          nowMs: 2500,
        ),
        isFalse,
      );
    },
  );

  test('stale session_history_reset_ack does not clear newer reset', () async {
    final env = await connectAuthenticatedService();
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser('1001');
    try {
      const sid = 's-reset-stale-ack';
      await env.service.deleteConversation(sid);
      await _expectEventually(
        () => env.sink.packets
            .where((p) => p['cmd'] == 'session_history_reset')
            .isNotEmpty,
        reason: '删除会话后应发送 session_history_reset',
      );
      final first = env.sink.packets.lastWhere(
        (p) => p['cmd'] == 'session_history_reset',
      );

      await Future<void>.delayed(const Duration(milliseconds: 2));
      await env.service.deleteConversation(sid);
      await _expectEventually(
        () =>
            env.sink.packets
                .where((p) => p['cmd'] == 'session_history_reset')
                .length >=
            2,
        reason: '更晚 deleted_at 应立即发送新 reset',
      );
      final second = env.sink.packets.lastWhere(
        (p) => p['cmd'] == 'session_history_reset',
      );
      final secondPayload = second['payload'] as Map;
      final secondDeletedAt = secondPayload['deleted_at'] as int;

      env.downstream.add(
        jsonEncode({
          'cmd': 'session_history_reset_ack',
          'seq': first['seq'],
          'payload': {'session_id': sid, 'code': 0},
        }),
      );

      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(
        env.service.hasRecentSessionHistoryResetInFlightForTest(
          sid,
          nowMs: (second['seq'] as int) + 100,
          deletedAtMs: secondDeletedAt,
        ),
        isTrue,
        reason: '旧 ack 不能清掉更晚 deleted_at 的 in-flight 状态',
      );
    } finally {
      env.service.disconnect();
      await env.downstream.close();
      await LocalDb.setActiveUser(null);
    }
  });

  test('session_history_reset retries when ack is missing', () async {
    ImService.sessionHistoryResetRetryMsForTest = 40;
    final env = await connectAuthenticatedService();
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser('1001');
    try {
      const sid = 's-reset-retry';
      await env.service.deleteConversation(sid);
      await _expectEventually(
        () =>
            env.sink.packets
                .where((p) => p['cmd'] == 'session_history_reset')
                .length >=
            2,
        timeout: const Duration(seconds: 2),
        reason: '未收到 ack 时应按 retry 间隔重发 reset',
      );
    } finally {
      env.service.disconnect();
      await env.downstream.close();
      await LocalDb.setActiveUser(null);
    }
  });

  test('session_history_reset ack success cancels retry', () async {
    ImService.sessionHistoryResetRetryMsForTest = 40;
    final env = await connectAuthenticatedService();
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser('1001');
    try {
      const sid = 's-reset-ack-cancel';
      await env.service.deleteConversation(sid);
      await _expectEventually(
        () => env.sink.packets
            .where((p) => p['cmd'] == 'session_history_reset')
            .isNotEmpty,
        reason: '删除会话后应发送 session_history_reset',
      );
      final first = env.sink.packets.lastWhere(
        (p) => p['cmd'] == 'session_history_reset',
      );

      env.downstream.add(
        jsonEncode({
          'cmd': 'session_history_reset_ack',
          'seq': first['seq'],
          'payload': {'session_id': sid, 'code': 0},
        }),
      );

      await Future<void>.delayed(const Duration(milliseconds: 120));
      expect(
        env.sink.packets
            .where((p) => p['cmd'] == 'session_history_reset')
            .length,
        1,
        reason: '成功 ack 后 retry timer 应取消，不应再次发送 reset',
      );
    } finally {
      env.service.disconnect();
      await env.downstream.close();
      await LocalDb.setActiveUser(null);
    }
  });

  test('session_history_reset retry stops after disconnect', () async {
    ImService.sessionHistoryResetRetryMsForTest = 40;
    final env = await connectAuthenticatedService();
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser('1001');
    try {
      await env.service.deleteConversation('s-reset-disconnect-cancel');
      await _expectEventually(
        () => env.sink.packets
            .where((p) => p['cmd'] == 'session_history_reset')
            .isNotEmpty,
        reason: '删除会话后应发送 session_history_reset',
      );
      env.service.disconnect();

      await Future<void>.delayed(const Duration(milliseconds: 120));
      expect(
        env.sink.packets
            .where((p) => p['cmd'] == 'session_history_reset')
            .length,
        1,
        reason: 'disconnect 后 retry timer 应取消，不应离线重发 reset',
      );
    } finally {
      await env.downstream.close();
      await LocalDb.setActiveUser(null);
    }
  });

  test(
    'session_history_reset retry stops after account switch reset',
    () async {
      ImService.sessionHistoryResetRetryMsForTest = 40;
      final env = await connectAuthenticatedService();
      await LocalDb.initDatabaseFactory();
      await LocalDb.setActiveUser('1001');
      try {
        await env.service.deleteConversation('s-reset-account-switch-cancel');
        await _expectEventually(
          () => env.sink.packets
              .where((p) => p['cmd'] == 'session_history_reset')
              .isNotEmpty,
          reason: '删除会话后应发送 session_history_reset',
        );

        await env.service.resetForAccountSwitch();
        await Future<void>.delayed(const Duration(milliseconds: 120));
        expect(
          env.sink.packets
              .where((p) => p['cmd'] == 'session_history_reset')
              .length,
          1,
          reason: '账号切换清理后 retry timer 应取消，不应跨账号重发 reset',
        );
      } finally {
        await env.downstream.close();
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test('suspendForAppBackground clears active viewing renewals', () {
    final service = ImService();
    addTearDown(service.onClose);

    service.setCurrentSessionForTest('session_1');
    service.startSessionViewingForTest('session_1');

    expect(service.isSessionViewingActiveForTest, isTrue);
    expect(service.viewingSessionIdForTest, 'session_1');

    service.suspendForAppBackground();

    expect(service.isSessionViewingActiveForTest, isFalse);
    expect(service.viewingSessionIdForTest, isEmpty);
    expect(service.currentSessionId, 'session_1');
    expect(service.connectionStage, ImConnectionStage.disconnected);
    expect(service.isSuspendedForAppBackgroundForTest, isTrue);
  });

  test('syncNow re-enables reconnect after manual disconnect', () {
    final service = ImService();
    addTearDown(service.onClose);

    // 端点已知（由 init / connect 设过，挂起后仍保留）才能重连：
    // syncNow 不再回落编译时默认值，避免全球区用户被重连到国区端点。
    service.seedRealtimeStateForTest(wsUrl: 'wss://example.test/ws');
    service.suspendForAppBackground();
    expect(service.isSuspendedForAppBackgroundForTest, isTrue);
    service.syncNow();

    expect(service.connectionStage, ImConnectionStage.reconnecting);
    expect(service.wsUrlForTest, 'wss://example.test/ws');
    expect(service.hasReconnectTimerForTest, isTrue);
    expect(service.isSuspendedForAppBackgroundForTest, isFalse);
  });

  test('syncNow forces reconnect when socket is stuck authenticating', () {
    final service = ImService();
    addTearDown(service.onClose);

    service.seedRealtimeStateForTest(
      wsUrl: 'wss://example.test/ws',
      connected: true,
      authenticated: false,
      stage: ImConnectionStage.authenticating,
    );

    service.syncNow();

    expect(service.connectionStage, ImConnectionStage.reconnecting);
    expect(service.wsUrlForTest, 'wss://example.test/ws');
    expect(service.hasReconnectTimerForTest, isTrue);
  });

  test('syncNow bypasses the pending backoff window instead of no-op', () {
    final service = ImService();
    addTearDown(service.onClose);

    // 退避已经涨到 30 秒上限，且有一个存活的重连定时器在等待——这正是用户
    // 看到"连接断开，正在重连"横幅时的真实状态。
    service.seedRealtimeStateForTest(
      wsUrl: 'wss://example.test/ws',
      stage: ImConnectionStage.reconnecting,
      reconnectAttempts: 6,
    );
    service.handleDisconnectForTest();
    expect(service.hasReconnectTimerForTest, isTrue);
    expect(service.reconnectAttemptsForTest, greaterThan(1));

    service.syncNow();

    // 点"重试"必须丢掉旧退避、立刻重连；旧实现会被存活定时器静默短路，
    // 退避计数原地不动，用户只能继续干等 30 秒。
    expect(service.reconnectAttemptsForTest, 1);
    expect(service.connectionStage, ImConnectionStage.reconnecting);
  });

  test('backoff regrows after an immediate manual retry', () async {
    final service = ImService();
    addTearDown(service.onClose);

    service.seedRealtimeStateForTest(
      wsUrl: 'wss://example.test/ws',
      stage: ImConnectionStage.reconnecting,
      reconnectAttempts: 6,
    );
    service.handleDisconnectForTest();

    service.syncNow();
    expect(service.reconnectAttemptsForTest, 1);

    // 让 Timer(0) 真正 fire 掉（_reconnectTimer 归 null），并等这一轮连接失败。
    // 不等的话存活的定时器会把后面的 _scheduleReconnect 短路掉，测不出真实退避。
    await Future<void>.delayed(const Duration(milliseconds: 200));

    // 立刻重连仍然失败 → 必须重新进入退避阶梯，不能一直 delay=0 疯狂重连打服务端。
    service.handleDisconnectForTest();
    expect(service.reconnectAttemptsForTest, greaterThan(1));
  });

  test('auth handshake timeout reconnects when auth ack is stuck', () {
    final service = ImService();
    addTearDown(service.onClose);

    service.seedRealtimeStateForTest(
      wsUrl: 'wss://example.test/ws',
      connected: true,
      authenticated: false,
      stage: ImConnectionStage.authenticating,
    );

    service.handleAuthHandshakeTimeoutForTest();

    expect(service.connectionStage, ImConnectionStage.reconnecting);
    expect(service.wsUrlForTest, 'wss://example.test/ws');
    expect(service.hasReconnectTimerForTest, isTrue);
  });

  test('restoreCurrentSessionRealtimeStateForTest resumes viewing state', () {
    final service = ImService();
    addTearDown(service.onClose);

    service.setCurrentSessionForTest('session_1');
    service.restoreCurrentSessionRealtimeStateForTest();

    expect(service.isSessionViewingActiveForTest, isTrue);
    expect(service.viewingSessionIdForTest, 'session_1');
  });

  /// 让服务端拒绝一次 auth_ack。code 由调用方给定——决策只看它，不看文案。
  Future<void> rejectAuth(ImService service, int code, String msg) async {
    service.seedRealtimeStateForTest(
      wsUrl: 'wss://example.test/ws',
      connected: true,
      authenticated: false,
      stage: ImConnectionStage.authenticating,
    );
    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'auth_ack',
        'payload': {'code': code, 'msg': msg},
      }),
    );
  }

  test('auth_ack fatal code with an invalid session logs out', () async {
    // 终态码 + 凭证刷新时服务端明确判定会话无效 → 重连救不回来，清会话回登录页。
    authService.refreshStatus = TokenRefreshStatus.invalidSession;
    final service = ImService();
    addTearDown(service.onClose);

    await rejectAuth(service, 10001, '用户身份不匹配');

    expect(authService.unauthorizedCalls, 1);
    expect(service.hasReconnectTimerForTest, isFalse);
  });

  test('auth_ack fatal code with a refreshable token self-heals', () async {
    // access token 过期时后端也报"凭证已失效"。但 refresh token 还有效，刷得动，
    // 换新凭证重连即可——不该把用户踢到登录页。
    authService.refreshStatus = TokenRefreshStatus.ready;
    final service = ImService();
    addTearDown(service.onClose);

    await rejectAuth(service, 10001, '凭证已失效，请重新登录');

    expect(authService.refreshCalls, 1);
    expect(authService.unauthorizedCalls, 0);
    expect(service.connectionStage, ImConnectionStage.reconnecting);
  });

  test('auth_ack retryable code never logs out and never refreshes', () async {
    // 服务端明说是它自己的存储层故障，凭证没问题。既不该清会话，也不该去刷凭证
    // （刷新走的是同一套存储，八成也是坏的）。保留会话继续重连，等它恢复自愈。
    //
    // 这是最要命的一条：后端一旦把数据库抖动报成凭证失效，全部在线用户会在几秒内
    // 被集体踢回登录页，且恢复后回不来。
    final service = ImService();
    addTearDown(service.onClose);

    for (var i = 0; i < 5; i++) {
      await rejectAuth(service, ImService.authCodeRetryable, '服务暂时不可用，请稍后重试');
    }

    expect(authService.unauthorizedCalls, 0, reason: '服务端瞬时故障把用户踢回登录页 = 误登出');
    expect(authService.refreshCalls, 0, reason: '服务端故障时不该去刷凭证');
    expect(service.connectionStage, ImConnectionStage.reconnecting);
  });

  test('re_auth_ack retryable code keeps the session', () async {
    final service = ImService();
    addTearDown(service.onClose);

    service.seedRealtimeStateForTest(
      wsUrl: 'wss://example.test/ws',
      connected: true,
      authenticated: true,
      stage: ImConnectionStage.connected,
    );
    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 're_auth_ack',
        'payload': {
          'code': ImService.authCodeRetryable,
          'msg': '服务暂时不可用，请稍后重试',
        },
      }),
    );

    expect(authService.unauthorizedCalls, 0);
    expect(authService.refreshCalls, 0);
    expect(service.connectionStage, ImConnectionStage.reconnecting);
  });

  test('fatal auth_ack is never decided by the message text', () async {
    // ⛔ 发布顺序约束：必须先发后端（ws + api），再发客户端包。
    //
    // 可重试码 10003 是这次和后端一起加的。旧后端不认识它，存储层抖动时仍会回
    // 10001 + "鉴权失败"/"用户已被禁用"。新客户端遇到 10001 会去刷凭证兜底——
    // 而旧后端刷新时碰上同一场数据库故障会回 401，客户端据此清掉会话。
    //
    //   新后端 + 旧客户端：后端回 10003，旧客户端不认 → 走原文案匹配 → 无限重连 → 安全
    //   旧后端 + 新客户端：后端回 10001，新客户端去刷 → 旧后端回 401 → 误登出 → 危险
    //
    // 本用例锁住的是：无论文案说什么，只要凭证还刷得动，就不该把用户踢走。
    // 客户端不再拿文案当判据——那正是这次要根除的东西。
    authService.refreshStatus = TokenRefreshStatus.ready;
    final service = ImService();
    addTearDown(service.onClose);

    for (final msg in ['鉴权失败', '用户已被禁用', '凭证已失效，请重新登录', '用户身份不匹配']) {
      await rejectAuth(service, 10001, msg);
    }

    expect(
      authService.unauthorizedCalls,
      0,
      reason: '凭证刷得动却把用户踢回登录页 = 拿文案当判据的老毛病',
    );
    expect(service.connectionStage, ImConnectionStage.reconnecting);
  });
}

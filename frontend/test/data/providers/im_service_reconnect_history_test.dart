import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => 'reconnect-history-test-user';

  @override
  String? get token => 'test_access_token';

  @override
  Future<void> logout({bool notifyServer = true}) async {}
}

class _SpySessionService extends SessionService {
  int historyCalls = 0;
  List<String> historyCallSessionIds = [];

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    return const SessionDetailResult(code: 50001, message: 'not set');
  }

  @override
  Future<List<SessionSnapshot>> fetchSessionSnapshots({
    int limit = 200,
    int maxPages = 5,
  }) async {
    return const [];
  }

  @override
  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    return const SessionSnapshotFetchResult(snapshots: [], success: true);
  }

  @override
  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) async {
    historyCalls++;
    historyCallSessionIds.add(sessionId);
    return const SessionMessageHistoryResult(
      code: 0,
      messages: [],
      hasMore: false,
    );
  }
}

final _trackedImServices = <ImService>[];

ImService _makeImService() {
  final service = ImService();
  _trackedImServices.add(service);
  return service;
}

void main() {
  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    Get.put<AuthService>(_FakeAuthService());
  });

  tearDown(() async {
    for (final service in _trackedImServices.reversed) {
      service.onClose();
    }
    _trackedImServices.clear();
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  group('refreshActiveSessionOnReconnect', () {
    test('有活跃会话时触发 history 拉取', () async {
      final sessionService = _SpySessionService();
      Get.put<SessionService>(sessionService);
      await LocalDb.setActiveUser('reconnect-history-test-user');

      final service = _makeImService();
      service.setCurrentSessionForTest('session-active-001');

      await service.refreshActiveSessionOnReconnectForTest();

      expect(sessionService.historyCalls, 1);
      expect(
        sessionService.historyCallSessionIds,
        contains('session-active-001'),
      );
    });

    test('无活跃会话时跳过 history 拉取', () async {
      final sessionService = _SpySessionService();
      Get.put<SessionService>(sessionService);
      await LocalDb.setActiveUser('reconnect-history-test-user');

      final service = _makeImService();
      // 不设置 currentSessionId → 无活跃会话

      await service.refreshActiveSessionOnReconnectForTest();

      expect(sessionService.historyCalls, 0);
    });

    test('currentSessionId 为空字符串时跳过', () async {
      final sessionService = _SpySessionService();
      Get.put<SessionService>(sessionService);
      await LocalDb.setActiveUser('reconnect-history-test-user');

      final service = _makeImService();
      service.setCurrentSessionForTest(''); // empty string

      await service.refreshActiveSessionOnReconnectForTest();

      expect(sessionService.historyCalls, 0);
    });
  });

  group('loadOlder 触底回源（网页历史卡住回归）', () {
    // 回归 Bug：网页端向上翻页翻到本地缓存最底部（返回的是"非空但不满一页"
    // 的最后一页）时，旧逻辑直接判定"没有更早消息"，再也不向服务器回源，
    // 导致历史卡在本地缓存下沿翻不动。修复后：本地翻到底应触发一次服务器
    // 回源（fetchMessageHistoryResult），由服务端权威 hasMore 决定是否到头。
    test('本地缓存翻到底后仍会向服务器请求更早消息', () async {
      final sessionService = _SpySessionService();
      Get.put<SessionService>(sessionService);
      final userId =
          'reconnect-history-test-user-${DateTime.now().microsecondsSinceEpoch}';
      const sessionId = 'session-history-floor';
      await LocalDb.setActiveUser(userId);

      // 种入 50 条本地消息（真实毫秒时间戳，递增）。轻量初始窗口只装最新
      // 30 条，剩余 20 条更早的落在窗口下方 —— 制造"非空且不满一页"的
      // 触底场景。时间戳须 > 1e10 以避开本地库的秒→毫秒归一化。
      const baseTs = 1782000000000;
      final rows = <Map<String, dynamic>>[];
      for (var i = 0; i < 50; i++) {
        rows.add({
          'msg_id': (100000 + i).toString(),
          'session_id': sessionId,
          'sender_id': 'peer',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'm$i',
          'created_at': baseTs + i,
        });
      }
      await LocalDb.batchInsertMessages(rows);

      final service = _makeImService();
      await service.loadInitialWindowForTest(sessionId);
      await Future<void>.delayed(Duration.zero);

      // 初始加载本身也会做一次远端同步，先归零，只观察触底翻页是否回源。
      sessionService.historyCalls = 0;
      sessionService.historyCallSessionIds.clear();

      // 触底翻页：读到本地最后 20 条（不满一页），旧逻辑到此为止、不回源；
      // 修复后应触发服务器回源。
      await service.loadOlderForCurrentSession();
      await Future<void>.delayed(const Duration(milliseconds: 50));

      expect(
        sessionService.historyCalls,
        greaterThanOrEqualTo(1),
        reason: '本地翻到底后应向服务器回源，而非直接判定到头',
      );
      expect(sessionService.historyCallSessionIds, contains(sessionId));

      await LocalDb.setActiveUser(null);
    });
  });
}

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  _FakeAuthService(this.userIdValue);

  final String userIdValue;

  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => userIdValue;

  @override
  String? get token => 'test_access_token';

  @override
  Future<void> logout({bool notifyServer = true}) async {}
}

class _FakeSessionService extends SessionService {
  int historyCalls = 0;
  SessionMessageHistoryResult historyResult =
      const SessionMessageHistoryResult();

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
    return historyResult;
  }
}

/// 模拟 HTTP 请求失败的 SessionService。
class _FailingSessionService extends SessionService {
  int historyCalls = 0;
  int failCount;
  final List<Map<String, dynamic>> messagesAfterRecovery;

  _FailingSessionService({
    this.failCount = 2,
    this.messagesAfterRecovery = const [],
  });

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
    if (historyCalls <= failCount) {
      // 模拟网络失败
      return const SessionMessageHistoryResult(
        code: 50001,
        message: 'network error',
      );
    }
    // 恢复后返回消息
    return SessionMessageHistoryResult(
      code: 0,
      messages: messagesAfterRecovery,
      hasMore: false,
    );
  }
}

/// 模拟抛异常的 SessionService。
class _ThrowingSessionService extends SessionService {
  int historyCalls = 0;
  int throwCount;

  _ThrowingSessionService({this.throwCount = 1});

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
    if (historyCalls <= throwCount) {
      throw Exception('模拟网络异常');
    }
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

/// Poll until [cond] is true. Prefer this over fixed delays when waiting for a
/// Timer plus follow-on async work (history / LocalDb) under CI load.
Future<void> _waitUntil(
  bool Function() cond, {
  Duration timeout = const Duration(seconds: 10),
  Duration pollInterval = const Duration(milliseconds: 50),
  String? description,
}) async {
  final deadline = DateTime.now().add(timeout);
  while (!cond()) {
    if (DateTime.now().isAfter(deadline)) {
      fail(
        'Timed out after ${timeout.inMilliseconds}ms'
        '${description == null ? '' : ' waiting for: $description'}',
      );
    }
    await Future<void>.delayed(pollInterval);
  }
}

String _testUserId = 'retry-test-user';

void main() {
  setUp(() {
    Get.testMode = true;
    Get.reset();
    MessageStreamController.resetForTest();
    SharedPreferences.setMockInitialValues({});
    _testUserId = 'retry-test-${DateTime.now().microsecondsSinceEpoch}';
    Get.put<AuthService>(_FakeAuthService(_testUserId));
  });

  tearDown(() async {
    for (final service in _trackedImServices.reversed) {
      service.onClose();
    }
    _trackedImServices.clear();
    await LocalDb.setActiveUser(null);
    ImService.initialRenderHydrationStartedForTest = null;
    ImService.sessionWindowCacheNowMsForTest = null;
    MessageStreamController.resetForTest();
    Get.reset();
  });

  group('初始消息加载重试机制', () {
    testWidgets('本地首屏发布后下一帧才启动 Markdown 磁盘 hydrate', (tester) async {
      await tester.pumpWidget(const SizedBox());
      var hydrationStarts = 0;
      ImService.initialRenderHydrationStartedForTest = () {
        hydrationStarts++;
      };

      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.scheduleInitialRenderHydrationForTest('s1', [
        List.filled(MessageBubble.maxInlineContentCharacters + 1, 'x').join(),
      ]);

      expect(service.currentSessionId, 's1');
      expect(hydrationStarts, 0);
      tester.binding.scheduleFrame();
      await tester.pump();
      expect(hydrationStarts, 1);
    });

    test('远程同步失败且本地为空时，安排重试', () async {
      final sessionService = _FailingSessionService(
        failCount: 1,
        messagesAfterRecovery: [
          {
            'msg_id': '1001',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': '你好',
            'created_at': 1700000001000,
          },
        ],
      );
      Get.put<SessionService>(sessionService);
      await LocalDb.setActiveUser(_testUserId);

      try {
        final service = _makeImService();
        await service.loadInitialWindowForTest('s1');

        // 第一次加载失败，消息为空，应安排重试
        expect(service.currentMessages, isEmpty);
        expect(service.hasInitialLoadRetryTimerForTest, isTrue);
        expect(service.initialLoadRetryCountForTest, 1);
        expect(sessionService.historyCalls, 1);
        expect(service.isInitialHistoryReady, isFalse);

        // 等重试 Timer + history/LocalDb 异步完成（勿用固定 2100ms，CI 余量不足）
        await _waitUntil(
          () =>
              sessionService.historyCalls >= 2 &&
              service.currentMessages.isNotEmpty &&
              !service.hasInitialLoadRetryTimerForTest &&
              service.isInitialHistoryReady,
          description: 'retry load: historyCalls>=2, messages non-empty, ready',
        );

        // 重试成功，消息应该加载出来
        expect(service.currentMessages.length, 1);
        expect(service.currentMessages.first.msgId, '1001');
        expect(service.currentMessages.first.content, '你好');
        expect(service.hasInitialLoadRetryTimerForTest, isFalse);
        expect(sessionService.historyCalls, 2);
        expect(service.isInitialHistoryReady, isTrue);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('重试达到上限后停止', () async {
      // 始终失败的 SessionService
      final sessionService = _FailingSessionService(failCount: 100);
      Get.put<SessionService>(sessionService);
      await LocalDb.setActiveUser(_testUserId);

      try {
        final service = _makeImService();
        await service.loadInitialWindowForTest('s1');

        // 第一次失败
        expect(service.currentMessages, isEmpty);
        expect(service.initialLoadRetryCountForTest, 1);

        // 等待第一次重试
        await _waitUntil(
          () => service.initialLoadRetryCountForTest >= 2,
          description: 'first retry scheduled (count>=2)',
        );
        expect(service.initialLoadRetryCountForTest, 2);

        // 等待第二次重试
        await _waitUntil(
          () => service.initialLoadRetryCountForTest >= 3,
          description: 'second retry scheduled (count>=3)',
        );
        expect(service.initialLoadRetryCountForTest, 3);

        // 等待第三次重试跑完并停止安排新 Timer（达到上限）
        await _waitUntil(
          () =>
              sessionService.historyCalls >= 4 &&
              !service.hasInitialLoadRetryTimerForTest &&
              service.isInitialHistoryReady,
          description: 'third retry done, no further timer, history ready',
        );

        // 不应再安排新的重试
        expect(service.hasInitialLoadRetryTimerForTest, isFalse);
        expect(service.initialLoadRetryCountForTest, 3);
        // 1次初始 + 3次重试 = 4次调用
        expect(sessionService.historyCalls, 4);
        // 放弃重试后空会话也要标就绪，空白页才能展示快捷绑定。
        expect(service.isInitialHistoryReady, isTrue);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('异常时也安排重试', () async {
      final sessionService = _ThrowingSessionService(throwCount: 1);
      Get.put<SessionService>(sessionService);
      await LocalDb.setActiveUser(_testUserId);

      try {
        final service = _makeImService();
        await service.loadInitialWindowForTest('s1');

        // 第一次抛异常，应安排重试
        expect(service.currentMessages, isEmpty);
        expect(service.hasInitialLoadRetryTimerForTest, isTrue);
        expect(service.initialLoadRetryCountForTest, 1);

        // 等重试完成（第二次 history 成功，不再安排 Timer）
        await _waitUntil(
          () =>
              sessionService.historyCalls >= 2 &&
              !service.hasInitialLoadRetryTimerForTest,
          description: 'throw-path retry: historyCalls>=2, timer cleared',
        );

        // 第二次不再抛异常
        expect(service.hasInitialLoadRetryTimerForTest, isFalse);
        expect(sessionService.historyCalls, 2);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('session 切换后重试被取消', () async {
      final sessionService = _FailingSessionService(failCount: 100);
      Get.put<SessionService>(sessionService);
      await LocalDb.setActiveUser(_testUserId);

      try {
        final service = _makeImService();
        await service.loadInitialWindowForTest('s1');

        expect(service.hasInitialLoadRetryTimerForTest, isTrue);

        // 切换到另一个 session
        service.setCurrentSessionForTest('s2');

        // 负向断言：必须等满原 2s 重试窗口，确认重试没有发生。
        // 不能改成 _waitUntil（没有「没发生」可轮询的正向条件）。
        await Future<void>.delayed(const Duration(milliseconds: 2200));

        // 重试不应执行（因为 currentSessionId 已变）
        // historyCalls 应该只有初始的 1 次
        expect(sessionService.historyCalls, 1);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('远程同步成功时不安排重试', () async {
      final sessionService = _FakeSessionService();
      sessionService.historyResult = const SessionMessageHistoryResult(
        code: 0,
        messages: [
          {
            'msg_id': '2001',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': '正常消息',
            'created_at': 1700000002000,
          },
        ],
        hasMore: false,
      );
      Get.put<SessionService>(sessionService);
      await LocalDb.setActiveUser(_testUserId);

      try {
        final service = _makeImService();
        await service.loadInitialWindowForTest('s1');

        // 加载成功
        expect(service.currentMessages.length, 1);
        expect(service.hasInitialLoadRetryTimerForTest, isFalse);
        expect(service.initialLoadRetryCountForTest, 0);
        expect(service.isInitialHistoryReady, isTrue);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('本地有缓存时即使远程失败也不重试', () async {
      final sessionService = _FailingSessionService(failCount: 100);
      Get.put<SessionService>(sessionService);
      await LocalDb.setActiveUser(_testUserId);

      try {
        // 先在本地 DB 插入消息
        await LocalDb.upsertMessage({
          'msg_id': '3001',
          'session_id': 's1',
          'sender_id': 'u2',
          'msg_type': 1,
          'content': '本地缓存消息',
          'created_at': 1700000003000,
        });

        final service = _makeImService();
        await service.loadInitialWindowForTest('s1');

        // 虽然远程失败，但本地有数据，不需要重试
        expect(service.currentMessages.length, 1);
        expect(service.currentMessages.first.content, '本地缓存消息');
        expect(service.hasInitialLoadRetryTimerForTest, isFalse);
        expect(service.initialLoadRetryCountForTest, 0);
        expect(service.isInitialHistoryReady, isTrue);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('本地为空且远程也无消息时，backfill 结束后才标历史就绪', () async {
      final sessionService = _FakeSessionService();
      sessionService.historyResult = const SessionMessageHistoryResult(
        code: 0,
        messages: [],
        hasMore: false,
      );
      Get.put<SessionService>(sessionService);
      await LocalDb.setActiveUser(_testUserId);

      try {
        final service = _makeImService();
        expect(service.isInitialHistoryReady, isFalse);
        await service.loadInitialWindowForTest('s1');

        expect(service.currentMessages, isEmpty);
        expect(service.isInitialHistoryReady, isTrue);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('leaveSession 清理重试 Timer', () async {
      final sessionService = _FailingSessionService(failCount: 100);
      Get.put<SessionService>(sessionService);
      await LocalDb.setActiveUser(_testUserId);

      try {
        final service = _makeImService();
        await service.loadInitialWindowForTest('s1');

        expect(service.hasInitialLoadRetryTimerForTest, isTrue);

        // 离开会话
        service.leaveSession('s1');

        // Timer 应被清理
        expect(service.hasInitialLoadRetryTimerForTest, isFalse);
        expect(service.isInitialHistoryReady, isFalse);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('本地窗口存在中间缺口时，首屏归档页会补回缺失消息', () async {
      final sessionService = _FakeSessionService();
      // 服务端权威列表包含本地缺失的中间一条（1002）。inbox_seq 在全局序列里
      // 不连续（1001/1050/1099），正是无法用单会话内部 seq 差判断空洞的原因。
      sessionService.historyResult = const SessionMessageHistoryResult(
        code: 0,
        messages: [
          {
            'msg_id': '1001',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'a',
            'inbox_seq': 1001,
            'created_at': 1700000001000,
          },
          {
            'msg_id': '1002',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': '实时丢失的中间消息',
            'inbox_seq': 1050,
            'created_at': 1700000002000,
          },
          {
            'msg_id': '1003',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'c',
            'inbox_seq': 1099,
            'created_at': 1700000003000,
          },
        ],
        hasMore: false,
      );
      Get.put<SessionService>(sessionService);
      await LocalDb.setActiveUser(_testUserId);

      try {
        // 本地已有首尾两条，但缺中间的 1002（实时 push 抖动丢失留下的空洞）。
        await LocalDb.batchInsertMessages([
          {
            'msg_id': '1001',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'a',
            'inbox_seq': 1001,
            'created_at': 1700000001000,
          },
          {
            'msg_id': '1003',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'c',
            'inbox_seq': 1099,
            'created_at': 1700000003000,
          },
        ]);

        final service = _makeImService();
        await service.loadInitialWindowForTest('s1');

        expect(sessionService.historyCalls, 1);

        final latest = await LocalDb.getLatestMessages('s1', limit: 60);
        final ids = latest.map((m) => m['msg_id']).toList();
        expect(ids, containsAll(['1001', '1002', '1003']));
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('首屏归档页通过变更总线补齐当前窗口缺失消息', () async {
      final sessionService = _FakeSessionService();
      sessionService.historyResult = const SessionMessageHistoryResult(
        code: 0,
        messages: [
          {
            'msg_id': '1001',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'a',
            'inbox_seq': 1001,
            'created_at': 1700000001000,
          },
          {
            'msg_id': '1002',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': '实时丢失的中间消息',
            'inbox_seq': 1050,
            'created_at': 1700000002000,
          },
          {
            'msg_id': '1003',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'c',
            'inbox_seq': 1099,
            'created_at': 1700000003000,
          },
        ],
        hasMore: false,
      );
      Get.put<SessionService>(sessionService);
      await LocalDb.setActiveUser(_testUserId);

      try {
        await LocalDb.batchInsertMessages([
          {
            'msg_id': '1001',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'a',
            'inbox_seq': 1001,
            'created_at': 1700000001000,
          },
          {
            'msg_id': '1003',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'c',
            'inbox_seq': 1099,
            'created_at': 1700000003000,
          },
        ]);

        final service = _makeImService();
        // 先进入会话启动变更总线订阅，再加载窗口（贴近真实进会话路径）。
        service.setCurrentSessionForTest('s1');
        await service.loadInitialWindowForTest('s1');

        expect(service.currentMessages.map((e) => e.msgId).toSet(), {
          '1001',
          '1002',
          '1003',
        });

        await Future<void>.delayed(const Duration(milliseconds: 100));

        expect(sessionService.historyCalls, 1);
        expect(service.currentMessages.length, 3);
        expect(service.currentMessages.map((e) => e.msgId).toSet(), {
          '1001',
          '1002',
          '1003',
        });
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });
  });
}

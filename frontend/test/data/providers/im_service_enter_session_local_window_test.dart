import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/models/message_model.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';

  @override
  String? get token => 'test_access_token';
}

class _FakeSessionService extends SessionService {
  int historyCalls = 0;

  @override
  bool get isInitialized => true;

  @override
  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) async {
    historyCalls++;
    return const SessionMessageHistoryResult(
      code: 0,
      messages: [],
      hasMore: false,
    );
  }
}

Map<String, dynamic> _row(
  String sessionId,
  int id, {
  String? content,
  String status = 'sent',
}) {
  return {
    'msg_id': '$id',
    'session_id': sessionId,
    'sender_id': id.isOdd ? 'user-1' : 'agent-1',
    'sender_type': id.isOdd ? 1 : 2,
    'msg_type': 1,
    'content': content ?? 'message $id',
    'created_at': 1700000000000 + id * 1000,
    'status': status,
    'state_version': '1',
  };
}

Future<void> _waitUntil(
  bool Function() cond, {
  Duration timeout = const Duration(seconds: 5),
  String? description,
}) async {
  final deadline = DateTime.now().add(timeout);
  while (!cond()) {
    if (DateTime.now().isAfter(deadline)) {
      fail('Timed out waiting for: ${description ?? 'condition'}');
    }
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late ImService imService;
  late String userId;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    userId = 'enter-local-${DateTime.now().microsecondsSinceEpoch}';
    Get.put<AuthService>(_FakeAuthService());
    Get.put<SessionService>(_FakeSessionService());
    await LocalDb.setActiveUser(userId);
    imService = ImService();
    Get.put<ImService>(imService);
  });

  tearDown(() async {
    imService.onClose();
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  test('内存缓存仅1条、DB有5条时，进页无手势恢复完整首屏窗口', () async {
    const sid = 'cache-short-session';
    // 第一次进入时本地只有最新一条，窗口缓存也只能留下这 1 条。
    await LocalDb.upsertMessage(_row(sid, 5));
    imService.enterSession(sid);
    await _waitUntil(
      () => imService.currentMessages.length == 1,
      description: 'first entry loads the single local message',
    );

    // 切走再补 4 条更老的消息进 DB（模拟缓存窗口落后于本地库）。
    imService.enterSession('other-session');
    await _waitUntil(
      () => imService.currentSessionId == 'other-session',
      description: 'switched to other session',
    );
    await LocalDb.batchInsertMessages([
      _row(sid, 1),
      _row(sid, 2),
      _row(sid, 3),
      _row(sid, 4),
    ]);

    // 重新进入：命中仅 1 条的缓存窗口，但 DB 查询必须立刻把它补全。
    imService.enterSession(sid);
    await _waitUntil(
      () => imService.currentMessages.length == 5,
      description: 'DB window replaces the 1-message cache without gesture',
    );
    expect(imService.currentMessages.map((m) => m.msgId).toList(), [
      '1',
      '2',
      '3',
      '4',
      '5',
    ]);
    expect(imService.isInitialHistoryReady, isTrue);
  });

  test('缓存未命中时不等页面过渡延时，立即读到本地30条', () async {
    const sid = 'cache-miss-session';
    await LocalDb.batchInsertMessages(
      List.generate(30, (index) => _row(sid, index + 1)),
    );

    final stopwatch = Stopwatch()..start();
    imService.enterSession(sid);
    await _waitUntil(
      () => imService.currentMessages.isNotEmpty,
      description: 'cache-miss local window visible',
    );
    stopwatch.stop();

    // 旧的 cache-miss 链路要等 post-frame + 330ms timer 才查 DB；
    // 本地 30 行的 SQLite 查询应远快于此，证明首屏不再依赖该 timer。
    expect(stopwatch.elapsedMilliseconds, lessThan(300));
    await _waitUntil(
      () => imService.currentMessages.length == 30,
      description: 'full 30-message local window',
    );
    expect(imService.isInitialHistoryReady, isTrue);
  });

  test('切会话后，旧会话晚到的 DB 查询结果不会覆盖当前窗口', () async {
    const sidA = 'stale-session-a';
    const sidB = 'stale-session-b';
    await LocalDb.batchInsertMessages([
      _row(sidA, 101),
      _row(sidA, 102),
      _row(sidA, 103),
      _row(sidB, 201),
      _row(sidB, 202),
    ]);

    // 两次同步调用之间没有事件循环空隙，A 的 DB 查询必然在 B 激活后才返回。
    imService.enterSession(sidA);
    imService.enterSession(sidB);

    await _waitUntil(
      () =>
          imService.currentMessages.length == 2 &&
          imService.isInitialHistoryReady,
      description: 'session B window loaded',
    );
    expect(imService.currentMessages.map((m) => m.msgId).toList(), [
      '201',
      '202',
    ]);

    // 负向断言：再等一段事件循环，确认 A 的结果没有串窗。
    await Future<void>.delayed(const Duration(milliseconds: 300));
    expect(imService.currentSessionId, sidB);
    expect(imService.currentMessages.map((m) => m.msgId).toList(), [
      '201',
      '202',
    ]);
  });

  test('缓存过渡重进后，流式占位与未确认发送消息都保留', () async {
    const sid = 'transient-session';
    await LocalDb.upsertMessage(_row(sid, 1));
    imService.enterSession(sid);
    await _waitUntil(
      () => imService.currentMessages.length == 1,
      description: 'initial window loaded',
    );

    // 注入一条流式占位（msgType=4，不落库）。
    imService.upsertUIMessageForTest(
      MessageModel.fromJson({
        'msg_id': 'stream-1',
        'session_id': sid,
        'sender_id': 'agent-1',
        'sender_type': 2,
        'msg_type': 4,
        'content': 'streaming…',
        'created_at': 1700000005000,
      }),
    );
    expect(imService.currentMessages.any((m) => m.msgId == 'stream-1'), isTrue);

    // 切走（缓存窗口含流式占位），并在 DB 里补一条未确认的乐观发送。
    imService.enterSession('transient-other');
    await LocalDb.upsertMessage(
      _row(sid, 2, content: 'pending send', status: 'sending'),
    );

    imService.enterSession(sid);
    await _waitUntil(
      () =>
          imService.currentMessages.any((m) => m.msgId == '2') &&
          imService.isInitialHistoryReady,
      description: 'DB window restored on re-entry',
    );
    expect(
      imService.currentMessages.map((m) => m.msgId).toList(),
      containsAll(['1', '2', 'stream-1']),
    );
    expect(
      imService.currentMessages.firstWhere((m) => m.msgId == '2').status,
      'sending',
    );
  });
}

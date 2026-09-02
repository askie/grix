import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 覆盖 pull_sync 多批补拉（hasMore 链）期间会话列表刷新节流的行为契约：
/// - 中间批（链继续）在节流间隔内跳过整刷；
/// - 链停止的批次必刷收尾——包括 has_more=true 但消息被服务端整批过滤为空
///   （web_widget 访客路径）导致客户端不再续拉的情况，防止前一批已落盘的
///   消息因节流滞留在会话列表之外。
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
  @override
  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    return const SessionSnapshotFetchResult(snapshots: [], success: true);
  }
}

final _trackedImServices = <ImService>[];

ImService _makeImService() {
  final service = ImService();
  _trackedImServices.add(service);
  return service;
}

String _testUserId = 'drain-refresh-test-user';

Map<String, dynamic> _msgRow({
  required String msgId,
  required String sessionId,
  required int inboxSeq,
  required String content,
  int createdAt = 1700000000000,
}) {
  return {
    'msg_id': msgId,
    'session_id': sessionId,
    'sender_id': 'peer',
    'sender_type': 2,
    'msg_type': 1,
    'content': content,
    'created_at': createdAt,
    'inbox_seq': inboxSeq,
  };
}

Future<void> _dispatchPullSyncResp(
  ImService service, {
  required bool hasMore,
  required List<Map<String, dynamic>> messages,
}) {
  return service.handleDownstreamForTest(
    jsonEncode({
      'cmd': 'pull_sync_resp',
      'payload': {'has_more': hasMore, 'messages': messages},
    }),
  );
}

void main() {
  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    _testUserId = 'drain-refresh-${DateTime.now().microsecondsSinceEpoch}';
    Get.put<AuthService>(_FakeAuthService(_testUserId));
    Get.put<SessionService>(_FakeSessionService());
  });

  tearDown(() async {
    for (final service in _trackedImServices.reversed) {
      service.onClose();
    }
    _trackedImServices.clear();
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  test('hasMore 链上中间批节流跳刷，空批停链时必刷收尾', () async {
    await LocalDb.setActiveUser(_testUserId);
    try {
      const sid = 's-drain';
      final service = _makeImService();

      // 第一批：链上首批，节流时间戳为 0 → 必刷，列表出现消息 A。
      await _dispatchPullSyncResp(
        service,
        hasMore: true,
        messages: [
          _msgRow(
            msgId: '9001',
            sessionId: sid,
            inboxSeq: 10,
            content: 'msg-a',
            createdAt: 1700000000000,
          ),
        ],
      );
      var session = service.sessions.firstWhere((s) => s.sessionId == sid);
      expect(session.lastMessage, 'msg-a', reason: '链上首批应整刷会话列表');

      // 第二批：链继续（hasMore=true 且有消息），距上次刷新 < 1.5s → 节流跳刷。
      // 消息 B 已落盘，但列表预览应仍停留在 A。
      await _dispatchPullSyncResp(
        service,
        hasMore: true,
        messages: [
          _msgRow(
            msgId: '9002',
            sessionId: sid,
            inboxSeq: 11,
            content: 'msg-b',
            createdAt: 1700000001000,
          ),
        ],
      );
      session = service.sessions.firstWhere((s) => s.sessionId == sid);
      expect(session.lastMessage, 'msg-a', reason: '节流间隔内链继续的中间批不应整刷');

      // 第三批：has_more=true 但消息为空（服务端整批过滤，web_widget 访客
      // 路径），客户端不会续拉 → 链在此停止，必须刷新收尾，把第二批已落盘
      // 的消息 B 带进列表。
      await _dispatchPullSyncResp(service, hasMore: true, messages: const []);
      session = service.sessions.firstWhere((s) => s.sessionId == sid);
      expect(
        session.lastMessage,
        'msg-b',
        reason: '空批停链必须刷新收尾，否则前批已落盘消息滞留在列表之外',
      );
    } finally {
      await LocalDb.setActiveUser(null);
    }
  });

  test('末批（hasMore=false）在节流间隔内也必刷', () async {
    await LocalDb.setActiveUser(_testUserId);
    try {
      const sid = 's-final';
      final service = _makeImService();

      await _dispatchPullSyncResp(
        service,
        hasMore: true,
        messages: [
          _msgRow(
            msgId: '8001',
            sessionId: sid,
            inboxSeq: 20,
            content: 'msg-a',
            createdAt: 1700000000000,
          ),
        ],
      );

      // 末批紧随其后（< 1.5s），仍应整刷并带出消息 B。
      await _dispatchPullSyncResp(
        service,
        hasMore: false,
        messages: [
          _msgRow(
            msgId: '8002',
            sessionId: sid,
            inboxSeq: 21,
            content: 'msg-b',
            createdAt: 1700000001000,
          ),
        ],
      );
      final session = service.sessions.firstWhere((s) => s.sessionId == sid);
      expect(session.lastMessage, 'msg-b', reason: '末批不受节流约束，必刷收尾');
    } finally {
      await LocalDb.setActiveUser(null);
    }
  });
}

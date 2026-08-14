import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 本组用例覆盖前后端同步两处修复：
/// - 缺陷二：pull_sync 多轮拉取时，未读快照只在最后一批（has_more=false）应用，
///   中途批次不替换未读，消除未读数抖动。
/// - 缺陷一：inbox_seq 游标只在消息确实落盘成功后才推进（此处验证成功路径，
///   防止守卫逻辑被误改成不推进）。
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

String _testUserId = 'sync-cursor-test-user';

Future<void> _seedSession(
  String sessionId, {
  required int unreadCount,
  int updatedAt = 1,
}) async {
  await LocalDb.upsertSession({
    'session_id': sessionId,
    'title': '',
    'type': 'private',
    'peer_id': '',
    'peer_type': 0,
    'peer_nickname': '',
    'peer_username': '',
    'updated_at': updatedAt,
    'is_pinned': false,
    'is_muted': false,
    'pinned_at': 0,
    'unread_count': unreadCount,
    'last_message': 'seed',
    'last_message_time': updatedAt,
  });
}

Map<String, dynamic> _msgRow({
  required String msgId,
  required String sessionId,
  required int inboxSeq,
  int createdAt = 1700000000000,
  String content = 'hello',
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

void main() {
  setUp(() {
    Get.testMode = true;
    Get.reset();
    MessageStreamController.resetForTest();
    SharedPreferences.setMockInitialValues({});
    _testUserId = 'sync-cursor-${DateTime.now().microsecondsSinceEpoch}';
    Get.put<AuthService>(_FakeAuthService(_testUserId));
    Get.put<SessionService>(_FakeSessionService());
  });

  tearDown(() async {
    for (final service in _trackedImServices.reversed) {
      service.onClose();
    }
    _trackedImServices.clear();
    ImService.failDbWriteOpForTest = null;
    await LocalDb.setActiveUser(null);
    MessageStreamController.resetForTest();
    Get.reset();
  });

  group('缺陷二：未读快照仅在最后一批应用', () {
    test('has_more=true 的中途批次不替换未读，has_more=false 的末批才应用', () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        const sid = 's1';
        await _seedSession(sid, unreadCount: 5);

        final service = _makeImService();

        // 第一批：has_more=true，携带会把未读改成 3 的快照。修复后不应应用。
        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'pull_sync_resp',
            'payload': {
              'has_more': true,
              'snapshot_seq': 100,
              'messages': [
                _msgRow(msgId: '9001', sessionId: sid, inboxSeq: 10),
              ],
              'unread_snapshot': {sid: 3},
            },
          }),
        );

        var session = service.sessions.firstWhere((s) => s.sessionId == sid);
        expect(
          session.unreadCount,
          5,
          reason: 'has_more=true 的中途批次不应替换未读，应保持本地原值 5',
        );

        // 末批：has_more=false，应用快照 → 未读变为 3。
        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'pull_sync_resp',
            'payload': {
              'has_more': false,
              'snapshot_seq': 100,
              'messages': [
                _msgRow(msgId: '9002', sessionId: sid, inboxSeq: 11),
              ],
              'unread_snapshot': {sid: 3},
            },
          }),
        );

        session = service.sessions.firstWhere((s) => s.sessionId == sid);
        expect(
          session.unreadCount,
          3,
          reason: 'has_more=false 的最后一批应应用未读快照，未读变为 3',
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('单批（has_more=false）即应用未读快照', () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        const sid = 's1';
        await _seedSession(sid, unreadCount: 9);

        final service = _makeImService();
        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'pull_sync_resp',
            'payload': {
              'has_more': false,
              'snapshot_seq': 50,
              'messages': [
                _msgRow(msgId: '7001', sessionId: sid, inboxSeq: 20),
              ],
              'unread_snapshot': {sid: 2},
            },
          }),
        );

        final session = service.sessions.firstWhere((s) => s.sessionId == sid);
        expect(session.unreadCount, 2, reason: '只有一批时应直接应用快照');
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });
  });

  group('缺陷一：落盘成功后推进 inbox_seq 游标', () {
    test('push_msg 落盘成功后游标推进到该消息 inbox_seq', () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        final service = _makeImService();
        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'push_msg',
            'payload': _msgRow(msgId: '8001', sessionId: 's1', inboxSeq: 42),
          }),
        );

        expect(
          service.resolvePullSyncCursorForTest(0),
          42,
          reason: '落盘成功后游标应推进到 42',
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('pull_sync_resp 落盘成功后游标推进到批次最大 inbox_seq', () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        final service = _makeImService();
        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'pull_sync_resp',
            'payload': {
              'has_more': false,
              'messages': [
                _msgRow(msgId: '6001', sessionId: 's1', inboxSeq: 50),
                _msgRow(msgId: '6002', sessionId: 's1', inboxSeq: 51),
              ],
            },
          }),
        );

        expect(
          service.resolvePullSyncCursorForTest(0),
          51,
          reason: '落盘成功后游标应推进到批次最大 inbox_seq 51',
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('pull_sync_resp edit 落库失败后不推进游标并安排重拉', () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        const sid = 's-edit-fail';
        const msgId = 'edit-fail-msg';
        await _seedSession(sid, unreadCount: 0);
        await LocalDb.upsertMessage({
          'msg_id': msgId,
          'session_id': sid,
          'sender_id': 'peer',
          'sender_type': 2,
          'msg_type': 1,
          'content': 'old',
          'created_at': 1700000000000,
          'inbox_seq': 10,
        });

        final service = _makeImService();
        service.observeInboxSeqForTest(10);
        service.setLastPullSyncRequestMsForTest(
          DateTime.now().millisecondsSinceEpoch,
        );
        ImService.failDbWriteOpForTest = (op) =>
            op == 'batchUpsertMessages(pull_sync_resp_edit)';

        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'pull_sync_resp',
            'payload': {
              'has_more': false,
              'messages': [
                {
                  ..._msgRow(
                    msgId: msgId,
                    sessionId: sid,
                    inboxSeq: 11,
                    content: 'edited',
                  ),
                  'sync_event': 'edit',
                },
              ],
            },
          }),
        );

        final row = await LocalDb.getMessageByMsgId(msgId);
        expect(row?['content'], 'old');
        expect(
          service.resolvePullSyncCursorForTest(0),
          10,
          reason: 'edit 落库失败时不能把 cursor 推进到 11',
        );
        expect(
          service.hasPullSyncThrottleTimerForTest,
          isTrue,
          reason: '失败批次应进入节流重拉，后续用旧 cursor 重新补拉',
        );
      } finally {
        ImService.failDbWriteOpForTest = null;
        await LocalDb.setActiveUser(null);
      }
    });
  });
}

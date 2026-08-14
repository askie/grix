import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
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
  final fetchedSessionIds = <String>[];
  final detailBySession = <String, SessionDetailResult>{};

  @override
  bool get isInitialized => true;

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(
    String sessionId,
  ) async {
    fetchedSessionIds.add(sessionId);
    return detailBySession[sessionId] ?? const SessionDetailResult(data: null);
  }

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

Future<void> _seedSession(
  String sessionId, {
  required int unreadCount,
  String peerId = '',
  int peerType = 0,
}) async {
  await LocalDb.upsertSession({
    'session_id': sessionId,
    'title': '',
    'type': 'private',
    'peer_id': peerId,
    'peer_type': peerType,
    'peer_nickname': '',
    'peer_username': '',
    'updated_at': 1700000000000,
    'is_pinned': false,
    'is_muted': false,
    'pinned_at': 0,
    'unread_count': unreadCount,
    'last_message': 'hello',
    'last_message_time': 1700000000000,
  });
}

void main() {
  late String testUserId;
  late _FakeSessionService sessionService;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    testUserId = 'backfill-test-${DateTime.now().microsecondsSinceEpoch}';
    sessionService = _FakeSessionService();
    Get.put<AuthService>(_FakeAuthService(testUserId));
    Get.put<SessionService>(sessionService);
  });

  tearDown(() async {
    for (final service in _trackedImServices.reversed) {
      service.onClose();
    }
    _trackedImServices.clear();
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  group('缺 peer 信息的私聊会话补拉回填', () {
    test('有未读且缺 peer 的会话补拉详情后回填内存与本地库', () async {
      await LocalDb.setActiveUser(testUserId);
      try {
        await _seedSession('thread-unread', unreadCount: 3);
        sessionService.detailBySession['thread-unread'] = SessionDetailResult(
          data: {
            'session_type': 1,
            'members': [
              {'member_id': testUserId, 'member_type': 1},
              {'member_id': '2001', 'member_type': 2, 'nickname': 'Claude'},
            ],
          },
        );

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);
        // 回填是 unawaited 的异步任务，留出排空时间
        await Future<void>.delayed(const Duration(milliseconds: 100));

        expect(sessionService.fetchedSessionIds, contains('thread-unread'));
        final session = service.sessions.firstWhere(
          (s) => s.sessionId == 'thread-unread',
        );
        expect(session.peerId, '2001');
        expect(session.peerType, 2);
        expect(session.peerNickname, 'Claude');
        expect(session.unreadCount, 3);

        // 本地库同步回填，重启后依然有效
        final rows = await LocalDb.getSessions();
        final row = rows.firstWhere(
          (r) => r['session_id'] == 'thread-unread',
        );
        expect(row['peer_id'].toString(), '2001');
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('无未读或已有 peer 的会话不触发补拉', () async {
      await LocalDb.setActiveUser(testUserId);
      try {
        await _seedSession('thread-read', unreadCount: 0);
        await _seedSession(
          'thread-known-peer',
          unreadCount: 2,
          peerId: '3001',
          peerType: 1,
        );

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);
        await Future<void>.delayed(const Duration(milliseconds: 100));

        expect(sessionService.fetchedSessionIds, isEmpty);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('临时失败（网络抖动/服务端错误）下一轮重试，成功后回填', () async {
      await LocalDb.setActiveUser(testUserId);
      try {
        await _seedSession('thread-transient-fail', unreadCount: 1);
        // 第一轮：网络错误 → 不放弃，留待下一轮
        sessionService.detailBySession['thread-transient-fail'] =
            const SessionDetailResult(code: 50001, networkError: true);

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);
        await Future<void>.delayed(const Duration(milliseconds: 100));

        // 第二轮：恢复正常 → 补拉成功回填
        sessionService.detailBySession['thread-transient-fail'] =
            SessionDetailResult(
              data: {
                'session_type': 1,
                'members': [
                  {'member_id': testUserId, 'member_type': 1},
                  {'member_id': '4001', 'member_type': 2, 'nickname': 'Peer'},
                ],
              },
            );
        await service.loadSessions(refreshFromServer: false);
        await Future<void>.delayed(const Duration(milliseconds: 100));

        expect(
          sessionService.fetchedSessionIds
              .where((sid) => sid == 'thread-transient-fail')
              .length,
          2,
        );
        final session = service.sessions.firstWhere(
          (s) => s.sessionId == 'thread-transient-fail',
        );
        expect(session.peerId, '4001');

        // 已成功回填后不再重复请求
        await service.loadSessions(refreshFromServer: false);
        await Future<void>.delayed(const Duration(milliseconds: 100));
        expect(
          sessionService.fetchedSessionIds
              .where((sid) => sid == 'thread-transient-fail')
              .length,
          2,
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('明确业务失败（403/404）只尝试一次，不重复请求', () async {
      await LocalDb.setActiveUser(testUserId);
      try {
        await _seedSession('thread-gone', unreadCount: 1);
        sessionService.detailBySession['thread-gone'] =
            const SessionDetailResult(code: 4004, httpStatus: 404);

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);
        await Future<void>.delayed(const Duration(milliseconds: 100));
        await service.loadSessions(refreshFromServer: false);
        await Future<void>.delayed(const Duration(milliseconds: 100));

        expect(
          sessionService.fetchedSessionIds
              .where((sid) => sid == 'thread-gone')
              .length,
          1,
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('瞬时失败不触发热循环续跑', () async {
      await LocalDb.setActiveUser(testUserId);
      try {
        await _seedSession('thread-flaky', unreadCount: 1);
        sessionService.detailBySession['thread-flaky'] =
            const SessionDetailResult(code: 50001, networkError: true);

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);
        await Future<void>.delayed(const Duration(milliseconds: 300));

        // 续跑只捞"本轮未尝试过"的新占位；本轮瞬时失败的会话
        // 仍留给下一次 loadSessions 重试，不能被立即重打。
        expect(
          sessionService.fetchedSessionIds
              .where((sid) => sid == 'thread-flaky')
              .length,
          1,
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('超过批量的占位自动续跑补齐，无需再等下一次 loadSessions', () async {
      await LocalDb.setActiveUser(testUserId);
      try {
        // 批量上限 6：造 8 个缺 peer 的未读会话，首轮只能处理 6 个，
        // 其余 2 个应由续跑立即接力，而不是等下一次 loadSessions。
        for (var i = 0; i < 8; i++) {
          final sid = 'thread-batch-$i';
          await _seedSession(sid, unreadCount: 1);
          sessionService.detailBySession[sid] = SessionDetailResult(
            data: {
              'session_type': 1,
              'members': [
                {'member_id': testUserId, 'member_type': 1},
                {'member_id': '90$i', 'member_type': 1, 'nickname': 'P$i'},
              ],
            },
          );
        }

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);
        // 首轮 6 个 + 续跑 2 个，顺序网络请求，留足排空时间
        await Future<void>.delayed(const Duration(milliseconds: 500));

        for (var i = 0; i < 8; i++) {
          final sid = 'thread-batch-$i';
          expect(
            sessionService.fetchedSessionIds,
            contains(sid),
            reason: '$sid 应被首轮或续跑补拉',
          );
          final session = service.sessions.firstWhere(
            (s) => s.sessionId == sid,
          );
          expect(session.peerId, '90$i');
        }
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('前 6 个瞬时失败时续跑仍能推进到第 7 个，且失败者不被重打', () async {
      await LocalDb.setActiveUser(testUserId);
      try {
        // 审查回归：续跑链必须把"本链已尝试"穿透到 pending 选取，否则
        // 前 6 个全瞬时失败时每轮重复取同一批、第 7 个永远排不进，
        // 形成饿死事件循环的自旋。
        for (var i = 0; i < 6; i++) {
          final sid = 'thread-chain-fail-$i';
          await _seedSession(sid, unreadCount: 1);
          sessionService.detailBySession[sid] =
              const SessionDetailResult(code: 50001, networkError: true);
        }
        await _seedSession('thread-chain-ok', unreadCount: 1);
        sessionService.detailBySession['thread-chain-ok'] =
            SessionDetailResult(
              data: {
                'session_type': 1,
                'members': [
                  {'member_id': testUserId, 'member_type': 1},
                  {'member_id': '9500', 'member_type': 1, 'nickname': 'Ok'},
                ],
              },
            );

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);
        await Future<void>.delayed(const Duration(milliseconds: 500));

        // 第 7 个被续跑补齐
        final ok = service.sessions.firstWhere(
          (s) => s.sessionId == 'thread-chain-ok',
        );
        expect(ok.peerId, '9500');

        // 失败的 6 个在本链内各只被请求一次，且链路已终止（不再增长）
        for (var i = 0; i < 6; i++) {
          final sid = 'thread-chain-fail-$i';
          expect(
            sessionService.fetchedSessionIds.where((s) => s == sid).length,
            1,
            reason: '$sid 链内至多尝试一次',
          );
        }
        final fetchedSoFar = sessionService.fetchedSessionIds.length;
        await Future<void>.delayed(const Duration(milliseconds: 300));
        expect(sessionService.fetchedSessionIds.length, fetchedSoFar);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });
  });

  group('消息创建会话时落入对端身份（peer_id 源头根治）', () {
    test('updateSessionLastMsg 带 peerId 时新建会话直接落对端身份', () async {
      await LocalDb.setActiveUser(testUserId);
      try {
        await LocalDb.updateSessionLastMsg(
          'fresh-thread',
          'hi',
          1700000000000,
          type: 'private',
          peerId: '5001',
          peerType: 1,
        );
        final rows = await LocalDb.getSessions();
        final row = rows.firstWhere((r) => r['session_id'] == 'fresh-thread');
        expect(row['peer_id'].toString(), '5001');
        expect(row['peer_type'], 1);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('对现有缺 peer 的会话补填，且不覆盖已有正确 peer', () async {
      await LocalDb.setActiveUser(testUserId);
      try {
        // 现有记录 peer 为空 → 被消息携带的对端身份补齐
        await _seedSession('peerless', unreadCount: 1);
        await LocalDb.updateSessionLastMsg(
          'peerless',
          'hi',
          1700000001000,
          type: 'private',
          peerId: '6001',
          peerType: 1,
        );
        var rows = await LocalDb.getSessions();
        expect(
          rows
              .firstWhere((r) => r['session_id'] == 'peerless')['peer_id']
              .toString(),
          '6001',
        );

        // 现有记录已有正确 peer → 不被覆盖
        await _seedSession(
          'has-peer',
          unreadCount: 1,
          peerId: '7001',
          peerType: 1,
        );
        await LocalDb.updateSessionLastMsg(
          'has-peer',
          'hi',
          1700000002000,
          type: 'private',
          peerId: '9999',
          peerType: 2,
        );
        rows = await LocalDb.getSessions();
        expect(
          rows
              .firstWhere((r) => r['session_id'] == 'has-peer')['peer_id']
              .toString(),
          '7001',
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });
  });
}

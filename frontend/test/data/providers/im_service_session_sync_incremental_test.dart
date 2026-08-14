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

/// 可配置的假 SessionService：fetchSessionSnapshotsResult 提供全量基线，
/// fetchSessionSyncResult 提供一次增量结果，并记录被调用时的 since 与次数。
class _FakeSessionService extends SessionService {
  _FakeSessionService({
    required this.fullSnapshots,
    required this.fullCursor,
    required this.syncResult,
  });

  final List<SessionSnapshot> fullSnapshots;
  final int fullCursor;
  final SessionSyncFetchResult syncResult;

  int syncCalls = 0;
  int? lastSyncSince;

  @override
  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    return SessionSnapshotFetchResult(
      snapshots: fullSnapshots,
      success: true,
      cursor: fullCursor,
    );
  }

  @override
  Future<SessionSyncFetchResult> fetchSessionSyncResult({
    required int since,
    int limit = 200,
  }) async {
    syncCalls++;
    lastSyncSince = since;
    return syncResult;
  }
}

final _trackedImServices = <ImService>[];

ImService _makeImService() {
  final service = ImService();
  _trackedImServices.add(service);
  return service;
}

String _testUserId = 'sync-incr-test-user';

SessionSnapshot _groupSnap(String sid) => SessionSnapshot(
  sessionId: sid,
  title: 'Group $sid',
  type: 'group',
  peerId: '',
  peerType: 0,
  peerNickname: '',
  peerUsername: '',
  updatedAt: 1700000000000,
  unreadCount: 0,
  lastMessage: 'hi',
);

void main() {
  setUp(() {
    Get.testMode = true;
    Get.reset();
    MessageStreamController.resetForTest();
    SharedPreferences.setMockInitialValues({});
    _testUserId = 'sync-incr-${DateTime.now().microsecondsSinceEpoch}';
    Get.put<AuthService>(_FakeAuthService(_testUserId));
  });

  tearDown(() async {
    for (final service in _trackedImServices.reversed) {
      service.onClose();
    }
    _trackedImServices.clear();
    await LocalDb.setActiveUser(null);
    MessageStreamController.resetForTest();
    Get.reset();
  });

  test('全量建基线后，日常刷新走增量：以基线 cursor 作 since 并按删除列表清本地', () async {
    await LocalDb.setActiveUser(_testUserId);
    try {
      final fake = _FakeSessionService(
        fullSnapshots: [_groupSnap('grp-keep'), _groupSnap('grp-drop')],
        fullCursor: 1000,
        syncResult: const SessionSyncFetchResult(
          snapshots: [],
          deletedSessionIds: ['grp-drop'],
          success: true,
          cursor: 2000,
        ),
      );
      Get.put<SessionService>(fake);

      final service = _makeImService();

      // 全量建基线：两个会话落本地，基线游标=1000。
      await service.refreshSessionsNow();
      final afterFull = service.sessions.map((s) => s.sessionId).toList();
      expect(afterFull, containsAll(<String>['grp-keep', 'grp-drop']));

      // 日常刷新：cursor>0 → 走增量。maxAge=0 强制视为过期。
      await service.refreshSessionsIfStale(maxAge: Duration.zero);

      // 走了增量，且用基线 cursor=1000 作为 since。
      expect(fake.syncCalls, 1);
      expect(fake.lastSyncSince, 1000);

      // 增量删除列表里的会话被清，未删除的保留。
      final afterIncr = service.sessions.map((s) => s.sessionId).toList();
      expect(afterIncr, contains('grp-keep'));
      expect(afterIncr, isNot(contains('grp-drop')));
    } finally {
      await LocalDb.setActiveUser(null);
    }
  });

  test('增量同步以服务端返回的新 cursor 续推游标', () async {
    await LocalDb.setActiveUser(_testUserId);
    try {
      final fake = _FakeSessionService(
        fullSnapshots: [_groupSnap('grp-keep')],
        fullCursor: 1000,
        syncResult: const SessionSyncFetchResult(
          snapshots: [],
          deletedSessionIds: [],
          success: true,
          cursor: 2000,
        ),
      );
      Get.put<SessionService>(fake);

      final service = _makeImService();
      await service.refreshSessionsNow();

      // 第一次增量：since 应为基线 cursor=1000。
      await service.refreshSessionsIfStale(maxAge: Duration.zero);
      expect(fake.lastSyncSince, 1000);

      // 第二次增量：since 应推进到上次返回的 cursor=2000。
      await service.refreshSessionsIfStale(maxAge: Duration.zero);
      expect(fake.lastSyncSince, 2000);
      expect(fake.syncCalls, 2);
    } finally {
      await LocalDb.setActiveUser(null);
    }
  });
}

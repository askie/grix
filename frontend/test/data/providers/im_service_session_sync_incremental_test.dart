import 'dart:async';

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
    this.fullHasMore = false,
  });

  List<SessionSnapshot> fullSnapshots;
  int fullCursor;
  SessionSyncFetchResult syncResult;
  Completer<void>? fullStarted;
  Completer<void>? fullGate;
  Completer<void>? syncStarted;
  Completer<void>? syncGate;

  /// 全量快照是否还有未拉取的页（会话量超过 limit*maxPages 时为 true）。
  bool fullHasMore;

  int syncCalls = 0;
  int? lastSyncSince;
  int fullCalls = 0;

  @override
  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    fullCalls++;
    fullStarted?.complete();
    final gate = fullGate;
    if (gate != null) await gate.future;
    return SessionSnapshotFetchResult(
      snapshots: fullSnapshots,
      success: true,
      hasMore: fullHasMore,
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
    syncStarted?.complete();
    final gate = syncGate;
    if (gate != null) await gate.future;
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

  test('快照未拉完时仍建立基线游标，且不整表对账删除窗口外的本地会话', () async {
    await LocalDb.setActiveUser(_testUserId);
    try {
      await LocalDb.clearActiveUserData();
      // 本地已有一条落在全量窗口之外的会话：服务端快照这次没拉到它，
      // 不能把它当成「服务端已删除」清掉。
      await LocalDb.upsertSession({
        'session_id': 'grp-outside-window',
        'title': 'Outside Window',
        'type': 'group',
        'updated_at': 1699000000000,
        'last_message': 'old',
        'last_message_time': 1699000000000,
      });

      final fake = _FakeSessionService(
        fullSnapshots: [_groupSnap('grp-keep')],
        fullCursor: 1000,
        fullHasMore: true,
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

      // 未拉完 → 不做整表对账，窗口外的本地会话保留。
      final afterFull = service.sessions.map((s) => s.sessionId).toList();
      expect(
        afterFull,
        containsAll(<String>['grp-keep', 'grp-outside-window']),
      );

      // 未拉完也已建立基线游标 → 日常刷新切到增量，不再退回全量。
      await service.refreshSessionsIfStale(maxAge: Duration.zero);
      expect(fake.syncCalls, 1);
      expect(fake.lastSyncSince, 1000);
    } finally {
      await LocalDb.setActiveUser(null);
    }
  });

  test('增量变更超过单次上限时回退全量，游标不越过未拉取的变更', () async {
    await LocalDb.setActiveUser(_testUserId);
    try {
      await LocalDb.clearActiveUserData();
      final fake = _FakeSessionService(
        fullSnapshots: [_groupSnap('grp-keep')],
        fullCursor: 1000,
        syncResult: const SessionSyncFetchResult(
          snapshots: [],
          deletedSessionIds: [],
          success: true,
          hasMore: true,
          cursor: 9000,
        ),
      );
      Get.put<SessionService>(fake);

      final service = _makeImService();

      // 建基线：cursor=1000。
      await service.refreshSessionsNow();
      expect(fake.fullCalls, 1);

      // 增量返回 hasMore=true → 不采用 cursor=9000，改走全量兜底。
      await service.refreshSessionsIfStale(maxAge: Duration.zero);
      expect(fake.syncCalls, 1);
      expect(fake.fullCalls, 2);

      // 游标仍停在全量建立的 1000，未被 9000 越过。
      await service.refreshSessionsIfStale(maxAge: Duration.zero);
      expect(fake.lastSyncSince, 1000);
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

  test(
    'stale full and incremental REST results cannot mutate a replacement account',
    () async {
      await LocalDb.setActiveUser(_testUserId);
      final fake = _FakeSessionService(
        fullSnapshots: [_groupSnap('account-a-full')],
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
      try {
        await LocalDb.upsertSession({
          'session_id': 'account-a-stale-local',
          'title': 'A stale local',
          'type': 'group',
          'updated_at': 1,
        });
        fake
          ..fullStarted = Completer<void>()
          ..fullGate = Completer<void>();
        final staleFull = service.refreshSessionsNow();
        await fake.fullStarted!.future;

        await service.resetForAccountSwitch();
        final accountB = '${_testUserId}_b';
        await LocalDb.setActiveUser(accountB);
        await LocalDb.upsertSession({
          'session_id': 'account-b-keep',
          'title': 'B keep',
          'type': 'group',
          'updated_at': 2,
        });
        fake.fullGate!.complete();
        await staleFull;

        var bRows = await LocalDb.getSessions();
        expect(bRows.map((row) => row['session_id']), ['account-b-keep']);
        expect(
          service.sessions.map((session) => session.sessionId),
          isNot(contains('account-a-full')),
        );

        fake
          ..fullStarted = null
          ..fullGate = null
          ..fullSnapshots = [
            _groupSnap('account-b-keep'),
            _groupSnap('account-b-from-full'),
          ]
          ..fullCursor = 3000;
        await service.refreshSessionsNow();
        bRows = await LocalDb.getSessions();
        expect(
          bRows.map((row) => row['session_id']),
          containsAll(['account-b-keep', 'account-b-from-full']),
        );

        fake
          ..syncResult = SessionSyncFetchResult(
            snapshots: [_groupSnap('account-a-incremental')],
            deletedSessionIds: const ['account-b-keep'],
            success: true,
            cursor: 4000,
          )
          ..syncStarted = Completer<void>()
          ..syncGate = Completer<void>();
        final staleIncremental = service.refreshSessionsIfStale(
          maxAge: Duration.zero,
        );
        await fake.syncStarted!.future;

        await service.resetForAccountSwitch();
        final accountC = '${_testUserId}_c';
        await LocalDb.setActiveUser(accountC);
        await LocalDb.upsertSession({
          'session_id': 'account-c-keep',
          'title': 'C keep',
          'type': 'group',
          'updated_at': 3,
          'unread_count': 7,
        });
        fake.syncGate!.complete();
        await staleIncremental;

        final cRows = await LocalDb.getSessions();
        expect(cRows.map((row) => row['session_id']), ['account-c-keep']);
        expect(cRows.single['unread_count'], 7);
        expect(
          service.sessions.map((session) => session.sessionId),
          isNot(contains('account-a-incremental')),
        );

        fake
          ..syncStarted = null
          ..syncGate = null
          ..fullSnapshots = [_groupSnap('account-c-keep')]
          ..fullCursor = 5000
          ..syncResult = SessionSyncFetchResult(
            snapshots: [_groupSnap('account-c-valid-incremental')],
            deletedSessionIds: const [],
            success: true,
            cursor: 6000,
          );
        await service.refreshSessionsNow();
        await service.refreshSessionsIfStale(maxAge: Duration.zero);
        expect(
          (await LocalDb.getSessions()).map((row) => row['session_id']),
          containsAll(['account-c-keep', 'account-c-valid-incremental']),
        );
      } finally {
        final fullGate = fake.fullGate;
        if (fullGate != null && !fullGate.isCompleted) fullGate.complete();
        final syncGate = fake.syncGate;
        if (syncGate != null && !syncGate.isCompleted) syncGate.complete();
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test('快照的会话活跃时间不污染列表时间：展示与排序都走最后一条可见消息', () async {
    await LocalDb.setActiveUser(_testUserId);
    try {
      await LocalDb.clearActiveUserData();
      // 服务端会话活跃时间被卡片等不可见消息推进（updated_at 远新于
      // last_msg_time）：展示与排序都必须停在最后一条可见消息。
      const snapshot = SessionSnapshot(
        sessionId: 'grp-activity',
        title: 'Activity',
        type: 'group',
        peerId: '',
        peerType: 0,
        peerNickname: '',
        peerUsername: '',
        updatedAt: 1700000000000,
        unreadCount: 1,
        lastMessage: 'hi',
        lastMessageTime: 1699000000000,
      );
      final fake = _FakeSessionService(
        fullSnapshots: [snapshot],
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

      final session = service.sessions.firstWhere(
        (s) => s.sessionId == 'grp-activity',
      );
      expect(session.updatedAt, 1700000000000);
      expect(session.activityAt, 1699000000000);
      expect(session.displayTime, 1699000000000);
    } finally {
      await LocalDb.setActiveUser(null);
    }
  });
}

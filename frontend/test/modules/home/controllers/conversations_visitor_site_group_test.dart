import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/conversation_summary_model.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/home/controllers/conversations_controller.dart';
import 'package:grix/modules/home/services/friend_qr_flow_service.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

const _baseTime = 1800000000000;
const _siteA = '7101';
const _siteB = '7102';
const _domainA = 'shop-a.example.com';
const _domainB = 'shop-b.example.com';
const _siteAVisitorOne = 'visitor-a1';
const _siteAVisitorTwo = 'visitor-a2';
const _siteBVisitor = 'visitor-b1';
const _fallbackVisitor = 'visitor-unknown-site';
const _normalSession = 'normal-peer-thread';
const _groupSession = 'group-thread';
const _normalGroupKey = 'private:2:peer-1';

String _siteGroupKey(String siteId) =>
    '${ConversationsController.visitorSiteGroupPrefix}$siteId';

class _FakeAuthService extends AuthService {
  _FakeAuthService(this._userId);

  final String _userId;

  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => _userId;

  @override
  String? get token => 'test_access_token';

  @override
  Future<void> logout({bool notifyServer = true}) async {}

  @override
  bool hasUsableAccessToken({Duration minRemaining = Duration.zero}) => true;

  @override
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = const Duration(minutes: 5),
  }) async => TokenRefreshStatus.ready;
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
  _FakeWebSocketChannel({required Stream<dynamic> stream, required this.sink})
    : _stream = stream;

  final Stream<dynamic> _stream;

  @override
  final WebSocketSink sink;

  @override
  Future<void> get ready => Future<void>.value();

  @override
  Stream<dynamic> get stream => _stream;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

Future<void> _eventually(bool Function() condition, {String? reason}) async {
  final deadline = DateTime.now().add(const Duration(seconds: 5));
  while (DateTime.now().isBefore(deadline)) {
    if (condition()) return;
    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
  expect(condition(), isTrue, reason: reason);
}

Future<void> _eventuallyAsync(
  Future<bool> Function() condition, {
  String? reason,
}) async {
  final deadline = DateTime.now().add(const Duration(seconds: 5));
  while (DateTime.now().isBefore(deadline)) {
    if (await condition()) return;
    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
  expect(await condition(), isTrue, reason: reason);
}

class _FakeSessionService extends SessionService {
  SessionSnapshotFetchResult snapshotsResult = const SessionSnapshotFetchResult(
    snapshots: [],
    success: true,
  );
  final List<String> bootstrapPageRequests = [];
  final List<ConversationPageResult> conversationPageResults = [];
  SessionSnapshotFetchResult bootstrapResult = const SessionSnapshotFetchResult(
    snapshots: [],
    success: true,
  );

  @override
  bool get isInitialized => true;

  @override
  Future<SessionSnapshotFetchResult> fetchSyncV2BootstrapPageResult({
    int limit = 200,
    String cursor = '',
    int? headCursor,
  }) async {
    bootstrapPageRequests.add(cursor);
    return bootstrapResult;
  }

  @override
  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    return snapshotsResult;
  }

  @override
  Future<SessionPinResult> setSessionPinnedResult(
    String sessionId, {
    required bool isPinned,
    String? commandId,
  }) async {
    return SessionPinResult(
      sessionId: sessionId,
      isPinned: isPinned,
      pinnedAt: isPinned ? _baseTime : 0,
    );
  }

  @override
  Future<SessionMuteResult> setSessionMutedResult(
    String sessionId, {
    required bool isMuted,
    String? commandId,
  }) async {
    return SessionMuteResult(sessionId: sessionId, isMuted: isMuted, code: 0);
  }

  @override
  Future<ConversationPageResult> fetchConversationPage({
    int limit = 30,
    String cursor = '',
  }) async {
    if (conversationPageResults.isEmpty) {
      return const ConversationPageResult(success: false);
    }
    return conversationPageResults.removeAt(0);
  }

  @override
  Future<ConversationThreadPageResult> fetchConversationThreads({
    required String groupKey,
    int limit = 20,
    String cursor = '',
  }) async {
    return ConversationThreadPageResult(groupKey: groupKey, success: false);
  }

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    return const SessionDetailResult(data: null);
  }
}

class _V2Harness {
  _V2Harness._(this.service, this.sink, this.downstream, this.generation);

  final ImService service;
  final _RecordingSink sink;
  final StreamController<dynamic> downstream;
  final String generation;

  static Future<_V2Harness> resume() async {
    await LocalDb.prepareSyncGeneration('pre-bootstrap');
    await LocalDb.markSyncBootstrapComplete(1, committedCursor: 0);
    final sink = _RecordingSink();
    final downstream = StreamController<dynamic>.broadcast();
    ImService.channelConnectorForTest = (_) =>
        _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');
    await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
    downstream.add(
      jsonEncode({
        'cmd': 'auth_ack',
        'payload': {
          'code': 0,
          'user_id': LocalDb.activeUserId,
          'active_sync': 'v2',
          'capabilities': ['sync_v2', 'compound_v1'],
        },
      }),
    );
    await _eventually(() => sink.packets.any((p) => p['cmd'] == 'sync_resume'));
    final generation = sink.packets
        .firstWhere((p) => p['cmd'] == 'sync_resume')['payload']['generation']
        .toString();
    return _V2Harness._(service, sink, downstream, generation);
  }

  int get acks => sink.packets.where((p) => p['cmd'] == 'sync_ack').length;

  Future<void> sendBatch({
    required int from,
    required int next,
    required List<Map<String, dynamic>> events,
  }) async {
    final acksBefore = acks;
    downstream.add(
      jsonEncode({
        'cmd': 'sync_batch',
        'payload': {
          'generation': generation,
          'from_cursor': '$from',
          'next_cursor': '$next',
          'head_cursor': '$next',
          'has_more': false,
          'events': events,
        },
      }),
    );
    await _eventually(() => acks == acksBefore + 1);
  }

  Future<void> close() async {
    service.disconnect();
    await downstream.close();
  }
}

SessionSnapshot _snapshot({
  required String sessionId,
  required String type,
  required int updatedAt,
  String title = '',
  String peerId = '',
  int peerType = 0,
  bool isVisitor = false,
  String visitorSiteId = '',
  String visitorSiteDomain = '',
  int unread = 0,
}) {
  return SessionSnapshot(
    sessionId: sessionId,
    title: title,
    type: type,
    peerId: peerId,
    peerType: peerType,
    peerNickname: peerId.isEmpty ? '' : 'peer-$peerId',
    peerUsername: '',
    updatedAt: updatedAt,
    unreadCount: unread,
    lastMessage: 'hello',
    lastMessageTime: updatedAt,
    isVisitor: isVisitor,
    visitorSiteId: visitorSiteId,
    visitorSiteDomain: visitorSiteDomain,
  );
}

void main() {
  late _FakeSessionService sessionService;
  late ImService imService;
  late List<ImService> services;
  late String testUserId;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    UserImageCacheManager.setDisabledForTest(true);
    ConversationsController.useConversationListApiForTest = false;
    testUserId = 'visitor-site-group-${DateTime.now().microsecondsSinceEpoch}';
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser(testUserId);
    await LocalDb.clearActiveUserData();
    sessionService = _FakeSessionService();
    imService = ImService();
    services = [imService];
    Get.put<AuthService>(_FakeAuthService(testUserId));
    Get.put<SessionService>(sessionService);
    Get.put<ImService>(imService);
    Get.put<FriendQrFlowService>(FriendQrFlowService());
  });

  tearDown(() async {
    ConversationsController.useConversationListApiForTest = null;
    ImService.channelConnectorForTest = null;
    ImService.syncV2ResumeSentForTest = null;
    for (final service in services.reversed) {
      service.onClose();
    }
    UserImageCacheManager.setDisabledForTest(false);
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  Future<void> seedSnapshots(List<SessionSnapshot> snapshots) async {
    sessionService.snapshotsResult = SessionSnapshotFetchResult(
      snapshots: snapshots,
      success: true,
    );
    await imService.refreshSessionsWindowNow();
  }

  Future<ImService> restartFromLocalDb() async {
    final restarted = ImService();
    services.add(restarted);
    Get.delete<ImService>(force: true);
    Get.put<ImService>(restarted);
    await restarted.loadSessions(
      refreshFromServer: false,
      backfillMissingPeerIdentities: false,
    );
    return restarted;
  }

  List<SessionSnapshot> twoSitesAndFallback({int baseUnread = 0}) => [
    _snapshot(
      sessionId: _siteAVisitorOne,
      type: 'private',
      updatedAt: _baseTime + 3000,
      isVisitor: true,
      visitorSiteId: _siteA,
      visitorSiteDomain: _domainA,
      unread: baseUnread,
    ),
    _snapshot(
      sessionId: _siteAVisitorTwo,
      type: 'private',
      updatedAt: _baseTime + 1000,
      isVisitor: true,
      visitorSiteId: _siteA,
      visitorSiteDomain: _domainA,
    ),
    _snapshot(
      sessionId: _siteBVisitor,
      type: 'private',
      updatedAt: _baseTime + 2000,
      isVisitor: true,
      visitorSiteId: _siteB,
      visitorSiteDomain: _domainB,
    ),
    _snapshot(
      sessionId: _fallbackVisitor,
      type: 'private',
      updatedAt: _baseTime + 500,
      isVisitor: true,
    ),
    _snapshot(
      sessionId: _normalSession,
      type: 'private',
      peerId: 'peer-1',
      peerType: 2,
      updatedAt: _baseTime + 2500,
    ),
    _snapshot(
      sessionId: _groupSession,
      type: 'group',
      title: '群聊',
      updatedAt: _baseTime + 1500,
    ),
  ];

  test('两个站点各自成组，标题是各自的域名；无站点访客归兜底组', () async {
    await seedSnapshots(twoSitesAndFallback());

    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();

    final byKey = <String, ConversationListItem>{
      for (final item in controller.groupedSessions) item.groupKey: item,
    };
    final siteAKey = _siteGroupKey(_siteA);
    final siteBKey = _siteGroupKey(_siteB);

    expect(byKey.keys, containsAll([siteAKey, siteBKey]));
    expect(byKey[siteAKey]!.sessions.map((s) => s.sessionId).toSet(), {
      _siteAVisitorOne,
      _siteAVisitorTwo,
    });
    expect(byKey[siteBKey]!.sessions.map((s) => s.sessionId).toSet(), {
      _siteBVisitor,
    });
    expect(controller.getConversationListTitle(byKey[siteAKey]!), _domainA);
    expect(controller.getConversationListTitle(byKey[siteBKey]!), _domainB);

    // 没有 site id 的访客会话继续归 visitor:group 兜底组。
    const fallbackKey = ConversationsController.visitorGroupKey;
    expect(byKey.keys, contains(fallbackKey));
    expect(byKey[fallbackKey]!.sessions.map((s) => s.sessionId).toSet(), {
      _fallbackVisitor,
    });
    expect(
      controller.getConversationListTitle(byKey[fallbackKey]!),
      'conversations_visitor_group_fallback_title'.tr,
    );

    // 反向用例：普通私聊和群聊的分组键、显示不受影响。
    expect(byKey.keys, contains(_normalGroupKey));
    expect(byKey.keys, contains('session:$_groupSession'));
    expect(
      controller.getConversationListTitle(byKey[_normalGroupKey]!),
      'peer-peer-1',
    );
    expect(
      controller.getConversationListTitle(byKey['session:$_groupSession']!),
      '群聊',
    );
    expect(byKey.containsKey('visitor:group'), isTrue);
    // 访客会话不再并进普通私聊组。
    expect(byKey[_normalGroupKey]!.sessions.map((s) => s.sessionId).toSet(), {
      _normalSession,
    });
  });

  test('组未读求和并计入 Tab 角标', () async {
    final snapshots = twoSitesAndFallback(baseUnread: 2);
    snapshots[1] = _snapshot(
      sessionId: _siteAVisitorTwo,
      type: 'private',
      updatedAt: _baseTime + 1000,
      isVisitor: true,
      visitorSiteId: _siteA,
      visitorSiteDomain: _domainA,
      unread: 3,
    );
    await seedSnapshots(snapshots);

    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();

    final siteA = controller.groupedSessions.singleWhere(
      (item) => item.groupKey == _siteGroupKey(_siteA),
    );
    expect(siteA.unreadCount, 5);
    expect(siteA.badgeUnreadCount, 5);
    // 组未读同样计入底部 Tab 角标（全局按会话求和）。
    expect(imService.totalUnread, 5);
  });

  test('组按组内最新一条会话的活跃时间排序', () async {
    await seedSnapshots(twoSitesAndFallback());

    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();

    final order = controller.groupedSessions
        .map((item) => item.groupKey)
        .toList();
    // site A 最新 3000 > 普通私聊 2500 > site B 2000 > 群聊 1500 > 兜底 500
    expect(
      order.indexOf(_siteGroupKey(_siteA)),
      lessThan(order.indexOf(_normalGroupKey)),
    );
    expect(
      order.indexOf(_normalGroupKey),
      lessThan(order.indexOf(_siteGroupKey(_siteB))),
    );
    expect(
      order.indexOf(_siteGroupKey(_siteB)),
      lessThan(order.indexOf('session:$_groupSession')),
    );
    expect(
      order.indexOf('session:$_groupSession'),
      lessThan(order.indexOf(ConversationsController.visitorGroupKey)),
    );
  });

  test('组置顶作用于组内全部会话，组内任一置顶即整组置顶', () async {
    await seedSnapshots(twoSitesAndFallback());

    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();
    final siteAKey = _siteGroupKey(_siteA);

    var item = controller.groupedSessions.singleWhere(
      (entry) => entry.groupKey == siteAKey,
    );
    expect(item.isPinned, isFalse);

    final ok = await controller.setSessionGroupPinned(item, isPinned: true);
    expect(ok, isTrue);
    await _eventually(
      () => controller.groupedSessions
          .singleWhere((entry) => entry.groupKey == siteAKey)
          .isPinned,
      reason: '组内全部会话置顶后，组本身必须显示为置顶',
    );
    for (final sessionId in [_siteAVisitorOne, _siteAVisitorTwo]) {
      expect(
        imService.findSessionById(sessionId)!.isPinned,
        isTrue,
        reason: '组置顶必须作用于组内所有会话',
      );
    }
    // 组置顶后排在未置顶组之前。
    expect(controller.groupedSessions.first.groupKey, siteAKey);

    item = controller.groupedSessions.singleWhere(
      (entry) => entry.groupKey == siteAKey,
    );
    final unpinned = await controller.setSessionGroupPinned(
      item,
      isPinned: false,
    );
    expect(unpinned, isTrue);
    await _eventually(
      () =>
          controller.groupedSessions
              .singleWhere((entry) => entry.groupKey == siteAKey)
              .isPinned ==
          false,
    );
    for (final sessionId in [_siteAVisitorOne, _siteAVisitorTwo]) {
      expect(imService.findSessionById(sessionId)!.isPinned, isFalse);
    }
  });

  test('组免打扰只有组内全部会话都免打扰才算', () async {
    final snapshots = twoSitesAndFallback(baseUnread: 2);
    snapshots[1] = _snapshot(
      sessionId: _siteAVisitorTwo,
      type: 'private',
      updatedAt: _baseTime + 1000,
      isVisitor: true,
      visitorSiteId: _siteA,
      visitorSiteDomain: _domainA,
      unread: 3,
    );
    await seedSnapshots(snapshots);

    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();
    final siteAKey = _siteGroupKey(_siteA);

    expect(
      controller.groupedSessions
          .singleWhere((item) => item.groupKey == siteAKey)
          .isMuted,
      isFalse,
    );

    // 只免打扰组内一条：整组不算免打扰，角标只扣掉这一条。
    await imService.setSessionMuted(_siteAVisitorOne, isMuted: true);
    await _eventually(() {
      final item = controller.groupedSessions.singleWhere(
        (entry) => entry.groupKey == siteAKey,
      );
      return !item.isMuted && item.badgeUnreadCount == 3;
    }, reason: '组内还有未免打扰会话时，整组不算免打扰');

    // 组内全部免打扰：整组才是免打扰，角标归零。
    final ok = await controller.setSessionGroupMuted(
      controller.groupedSessions.singleWhere(
        (item) => item.groupKey == siteAKey,
      ),
      isMuted: true,
    );
    expect(ok, isTrue);
    await _eventually(() {
      final item = controller.groupedSessions.singleWhere(
        (entry) => entry.groupKey == siteAKey,
      );
      return item.isMuted && item.badgeUnreadCount == 0;
    }, reason: '组内全部会话免打扰后整组才算免打扰');
  });

  test('冷启动从本地库重建后，访客站点分组不变', () async {
    await seedSnapshots(twoSitesAndFallback());
    await imService.loadSessions(
      refreshFromServer: false,
      backfillMissingPeerIdentities: false,
    );

    final restarted = await restartFromLocalDb();
    expect(
      restarted.sessions
          .firstWhere((s) => s.sessionId == _siteAVisitorOne)
          .visitorSiteId,
      _siteA,
    );
    expect(
      restarted.sessions
          .firstWhere((s) => s.sessionId == _siteAVisitorOne)
          .visitorSiteDomain,
      _domainA,
    );

    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();
    final byKey = <String, ConversationListItem>{
      for (final item in controller.groupedSessions) item.groupKey: item,
    };
    expect(
      byKey.keys,
      containsAll([
        _siteGroupKey(_siteA),
        _siteGroupKey(_siteB),
        ConversationsController.visitorGroupKey,
      ]),
    );
    expect(
      byKey[_siteGroupKey(_siteA)]!.sessions.map((s) => s.sessionId).toSet(),
      {_siteAVisitorOne, _siteAVisitorTwo},
    );
    expect(
      controller.getConversationListTitle(byKey[_siteGroupKey(_siteA)]!),
      _domainA,
    );
    expect(
      controller.getConversationListTitle(byKey[_siteGroupKey(_siteB)]!),
      _domainB,
    );
  });

  test('API 摘要路径同样按站点折叠访客会话，未读求和并支持组置顶', () async {
    ConversationsController.useConversationListApiForTest = true;
    ConversationSummaryModel visitorSummary({
      required String sessionId,
      required int activeAt,
      String visitorSiteId = '',
      String visitorSiteDomain = '',
      int unread = 0,
    }) {
      return ConversationSummaryModel(
        groupKey: 'session:$sessionId',
        conversationType: 'private',
        latestSessionId: sessionId,
        title: '访客',
        sessionType: 1,
        isVisitor: true,
        visitorSiteId: visitorSiteId,
        visitorSiteDomain: visitorSiteDomain,
        lastMsg: 'visitor message',
        lastMsgTime: activeAt,
        unread: unread,
        badgeUnread: unread,
        updatedAt: activeAt,
        latestActiveAt: activeAt,
        threadCount: 1,
      );
    }

    sessionService.conversationPageResults.add(
      ConversationPageResult(
        items: [
          visitorSummary(
            sessionId: _siteAVisitorOne,
            activeAt: _baseTime + 3000,
            visitorSiteId: _siteA,
            visitorSiteDomain: _domainA,
            unread: 2,
          ),
          visitorSummary(
            sessionId: _siteAVisitorTwo,
            activeAt: _baseTime + 1000,
            visitorSiteId: _siteA,
            visitorSiteDomain: _domainA,
            unread: 3,
          ),
          visitorSummary(
            sessionId: _siteBVisitor,
            activeAt: _baseTime + 2000,
            visitorSiteId: _siteB,
            visitorSiteDomain: _domainB,
            unread: 1,
          ),
          visitorSummary(
            sessionId: _fallbackVisitor,
            activeAt: _baseTime + 500,
          ),
          const ConversationSummaryModel(
            groupKey: _normalGroupKey,
            conversationType: 'private',
            latestSessionId: _normalSession,
            title: 'Agent',
            peerId: 'peer-1',
            peerType: 2,
            peerNickname: 'peer-peer-1',
            sessionType: 1,
            lastMsg: 'normal',
            lastMsgTime: _baseTime + 2500,
            unread: 0,
            badgeUnread: 0,
            updatedAt: _baseTime + 2500,
            latestActiveAt: _baseTime + 2500,
            threadCount: 1,
          ),
        ],
      ),
    );

    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();

    final byKey = <String, ConversationListItem>{
      for (final item in controller.groupedSessions) item.groupKey: item,
    };
    final siteAKey = _siteGroupKey(_siteA);
    final siteBKey = _siteGroupKey(_siteB);
    expect(
      byKey.keys,
      containsAll([
        siteAKey,
        siteBKey,
        ConversationsController.visitorGroupKey,
        _normalGroupKey,
      ]),
    );

    final siteA = byKey[siteAKey]!;
    expect(siteA.sessions.map((s) => s.sessionId).toSet(), {
      _siteAVisitorOne,
      _siteAVisitorTwo,
    });
    expect(siteA.unreadCount, 5);
    expect(siteA.badgeUnreadCount, 5);
    expect(siteA.threadCount, 2);
    expect(controller.getConversationListTitle(siteA), _domainA);
    expect(controller.getConversationListTitle(byKey[siteBKey]!), _domainB);
    // 摘要行上的兜底组同样只含无站点归属的访客。
    expect(
      byKey[ConversationsController.visitorGroupKey]!.sessions
          .map((s) => s.sessionId)
          .toSet(),
      {_fallbackVisitor},
    );
    // 组按组内最新活动时间排序：site A 最新，其次普通私聊，再 site B。
    final order = controller.groupedSessions.map((i) => i.groupKey).toList();
    expect(order.first, siteAKey);
    expect(order.indexOf(_normalGroupKey), lessThan(order.indexOf(siteBKey)));
    // 普通私聊的键与标题不受影响。
    expect(
      controller.getConversationListTitle(byKey[_normalGroupKey]!),
      'peer-peer-1',
    );

    // 摘要路径的组置顶作用于组内所有会话，且即时重排。
    await controller.setSessionGroupPinned(siteA, isPinned: true);
    await _eventually(
      () =>
          controller.groupedSessions.first.groupKey == siteAKey &&
          controller.groupedSessions.first.isPinned,
      reason: '摘要路径组置顶后整组应置顶并排在首位',
    );
  });

  test('v2 增量 session.upsert 新建的访客会话无需重启即进入对应站点组', () async {
    final harness = await _V2Harness.resume();
    services.add(harness.service);
    Get.delete<ImService>(force: true);
    Get.put<ImService>(harness.service);

    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();
    expect(
      controller.groupedSessions.where(
        (item) => ConversationsController.isVisitorGroupKey(item.groupKey),
      ),
      isEmpty,
    );

    const v2Visitor = 'v2-visitor-a';
    const v2VisitorSite = '9001';
    const compoundVisitor = 'v2-visitor-b';
    const compoundVisitorSite = '9002';
    await harness.sendBatch(
      from: 0,
      next: 2,
      events: [
        {
          'cursor': '1',
          'kind': 'session.upsert',
          'entity_type': 'session',
          'entity_id': v2Visitor,
          'entity_version': '1',
          'payload': {
            'session_id': v2Visitor,
            'session_type': 1,
            'updated_at': '2026-09-22T00:00:00Z',
            'last_msg_summary': 'hello v2',
            'is_visitor': true,
            'visitor_site_id': v2VisitorSite,
            'visitor_site_domain': 'shop-v2.example.com',
          },
        },
        {
          'cursor': '2',
          'kind': 'message.upsert',
          'entity_type': 'message',
          'entity_id': 'v2-msg-b',
          'entity_version': '1',
          'payload': {
            'msg_id': 'v2-msg-b',
            'session_id': compoundVisitor,
            'sender_id': '9',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'hello compound',
            'extra': <String, dynamic>{},
            'created_at': '2026-09-22T00:00:01Z',
            'session': {
              'session_id': compoundVisitor,
              'session_type': 1,
              'updated_at': '2026-09-22T00:00:01Z',
              'is_visitor': true,
              'visitor_site_id': compoundVisitorSite,
              'visitor_site_domain': 'shop-v2b.example.com',
            },
            'unread': {
              'session_id': compoundVisitor,
              'unread_count': 0,
              'state_version': 1,
            },
          },
        },
      ],
    );

    await _eventuallyAsync(() async {
      final row = await LocalDb.getSessionRecord(v2Visitor);
      return row != null &&
          row['is_visitor'] == 1 &&
          row['visitor_site_id'] == v2VisitorSite;
    }, reason: 'v2 session.upsert 必须投影 is_visitor 与站点字段');
    final compoundRow = await LocalDb.getSessionRecord(compoundVisitor);
    expect(compoundRow!['is_visitor'], 1);
    expect(compoundRow['visitor_site_id'], compoundVisitorSite);
    expect(compoundRow['visitor_site_domain'], 'shop-v2b.example.com');

    // 同一个进程内（未重连、未重启）列表立刻按站点分组。
    await _eventually(() {
      final keys = controller.groupedSessions.map((i) => i.groupKey).toSet();
      return keys.contains(_siteGroupKey(v2VisitorSite)) &&
          keys.contains(_siteGroupKey(compoundVisitorSite));
    }, reason: 'v2 增量写入后无需重启就要进入对应站点组');

    final siteItem = controller.groupedSessions.singleWhere(
      (item) => item.groupKey == _siteGroupKey(v2VisitorSite),
    );
    expect(siteItem.sessions.single.sessionId, v2Visitor);
    expect(
      controller.getConversationListTitle(siteItem),
      'shop-v2.example.com',
    );
    expect(
      controller.getConversationListTitle(
        controller.groupedSessions.singleWhere(
          (item) => item.groupKey == _siteGroupKey(compoundVisitorSite),
        ),
      ),
      'shop-v2b.example.com',
    );

    await harness.close();
  });

  test('搜索结果仍按单条会话显示，不按站点折叠', () async {
    await seedSnapshots(twoSitesAndFallback());
    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();

    controller.updateSearchQuery('hello');
    await _eventually(
      () =>
          !controller.searchInFlight.value &&
          controller.groupedSessions.any(
            (item) => item.groupKey == 'session:$_siteAVisitorOne',
          ),
    );

    final keys = controller.groupedSessions.map((i) => i.groupKey).toSet();
    expect(keys, contains('session:$_siteAVisitorOne'));
    expect(keys, contains('session:$_siteAVisitorTwo'));
    expect(keys, isNot(contains(_siteGroupKey(_siteA))));
  });
}

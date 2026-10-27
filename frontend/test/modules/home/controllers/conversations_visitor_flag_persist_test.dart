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

const _baseTime = 1800000000000;
const _visitorA = 'visitor-a';
const _visitorB = 'visitor-b';
const _normalSession = 'normal-peer-thread';
const _groupSession = 'group-thread';
const _normalGroupKey = 'private:2:peer-1';

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
}

class _FakeSessionService extends SessionService {
  SessionSnapshotFetchResult snapshotsResult = const SessionSnapshotFetchResult(
    snapshots: [],
    success: true,
  );

  @override
  bool get isInitialized => true;

  @override
  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    return snapshotsResult;
  }

  @override
  Future<ConversationPageResult> fetchConversationPage({
    int limit = 30,
    String cursor = '',
  }) async {
    return const ConversationPageResult(success: false);
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

SessionSnapshot _snapshot({
  required String sessionId,
  required String type,
  required int updatedAt,
  String title = '',
  String peerId = '',
  int peerType = 0,
  bool isVisitor = false,
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
    unreadCount: 0,
    lastMessage: 'hello',
    lastMessageTime: updatedAt,
    isVisitor: isVisitor,
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
    testUserId = 'visitor-flag-${DateTime.now().microsecondsSinceEpoch}';
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
    for (final service in services.reversed) {
      service.onClose();
    }
    UserImageCacheManager.setDisabledForTest(false);
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  /// 服务端下发的会话快照先落库，然后丢掉进程内状态：新 ImService 实例只能
  /// 从本地库重建列表，模拟冷启动。
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

  test('服务端快照的 is_visitor 落库，冷启动重建后仍归入 visitor:group', () async {
    sessionService.snapshotsResult = SessionSnapshotFetchResult(
      snapshots: [
        _snapshot(
          sessionId: _visitorA,
          type: 'private',
          title: '访客',
          updatedAt: _baseTime,
          isVisitor: true,
        ),
        _snapshot(
          sessionId: _visitorB,
          type: 'private',
          title: '访客',
          updatedAt: _baseTime + 2000,
          isVisitor: true,
        ),
        _snapshot(
          sessionId: _normalSession,
          type: 'private',
          peerId: 'peer-1',
          peerType: 2,
          updatedAt: _baseTime + 1000,
        ),
        _snapshot(
          sessionId: _groupSession,
          type: 'group',
          title: '群聊',
          updatedAt: _baseTime + 500,
        ),
      ],
      success: true,
    );

    await imService.refreshSessionsWindowNow();
    // 首轮在线身份正确，这是修复前就已成立的基线。
    expect(
      imService.sessions.firstWhere((s) => s.sessionId == _visitorA).isVisitor,
      isTrue,
    );

    final restarted = await restartFromLocalDb();
    expect(
      restarted.sessions.firstWhere((s) => s.sessionId == _visitorA).isVisitor,
      isTrue,
      reason: '冷启动后访客标记必须还在',
    );
    expect(
      restarted.sessions.firstWhere((s) => s.sessionId == _visitorB).isVisitor,
      isTrue,
    );

    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();

    final keys = controller.groupedSessions.map((i) => i.groupKey).toList();
    expect(keys, contains(ConversationsController.visitorGroupKey));
    final visitorItem = controller.groupedSessions.firstWhere(
      (i) => i.groupKey == ConversationsController.visitorGroupKey,
    );
    expect(visitorItem.sessions.map((s) => s.sessionId).toSet(), {
      _visitorA,
      _visitorB,
    });
  });

  test('会话摘要下发的 is_visitor 落库，冷启动重建后仍归入 visitor:group', () async {
    await imService.loadSessions(
      refreshFromServer: false,
      backfillMissingPeerIdentities: false,
    );
    await imService.persistConversationSummaryIdentities([
      const ConversationSummaryModel(
        groupKey: 'session:$_visitorA',
        conversationType: 'private',
        latestSessionId: _visitorA,
        title: '访客',
        sessionType: 1,
        isVisitor: true,
        lastMsg: 'hi',
        lastMsgTime: _baseTime,
        latestActiveAt: _baseTime,
        updatedAt: _baseTime,
      ),
    ]);

    final restarted = await restartFromLocalDb();
    expect(
      restarted.sessions.firstWhere((s) => s.sessionId == _visitorA).isVisitor,
      isTrue,
      reason: '摘要路径的访客标记也必须落库',
    );

    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();
    expect(
      controller.groupedSessions.single.groupKey,
      ConversationsController.visitorGroupKey,
    );
  });

  test('普通会话与群聊不受访客标记影响', () async {
    sessionService.snapshotsResult = SessionSnapshotFetchResult(
      success: true,
      snapshots: [
        _snapshot(
          sessionId: _visitorA,
          type: 'private',
          updatedAt: _baseTime,
          isVisitor: true,
        ),
        _snapshot(
          sessionId: _normalSession,
          type: 'private',
          peerId: 'peer-1',
          peerType: 2,
          updatedAt: _baseTime + 1000,
        ),
        _snapshot(
          sessionId: _groupSession,
          type: 'group',
          title: '群聊',
          updatedAt: _baseTime + 500,
        ),
      ],
    );
    await imService.refreshSessionsWindowNow();

    final restarted = await restartFromLocalDb();
    final normal = restarted.sessions.firstWhere(
      (s) => s.sessionId == _normalSession,
    );
    final group = restarted.sessions.firstWhere(
      (s) => s.sessionId == _groupSession,
    );
    expect(normal.isVisitor, isFalse);
    expect(group.isVisitor, isFalse);

    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();
    final keys = controller.groupedSessions.map((i) => i.groupKey).toSet();
    expect(keys, contains(_normalGroupKey));
    expect(keys, contains('session:$_groupSession'));
    expect(keys, contains(ConversationsController.visitorGroupKey));
  });
}

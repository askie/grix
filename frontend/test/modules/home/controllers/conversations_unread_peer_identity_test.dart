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

const _myUserId = '1000';
const _agentId = '8001';
const _agentGroupKey = 'private:2:$_agentId';
const _baseTime = 1700000000000;

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => _myUserId;

  @override
  String? get token => 'test_access_token';

  @override
  Future<void> logout({bool notifyServer = true}) async {}
}

class _FakeSessionService extends SessionService {
  final fetchedSessionIds = <String>[];
  SessionDetailResult detailResult = const SessionDetailResult(data: null);
  final List<ConversationPageResult> conversationPageResults =
      <ConversationPageResult>[];

  @override
  bool get isInitialized => true;

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    fetchedSessionIds.add(sessionId);
    return detailResult;
  }

  @override
  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    return const SessionSnapshotFetchResult(snapshots: [], success: true);
  }

  @override
  Future<ConversationPageResult> fetchConversationPage({
    int limit = 30,
    String cursor = '',
  }) async {
    if (conversationPageResults.isEmpty) {
      return const ConversationPageResult(success: false);
    }
    if (conversationPageResults.length == 1) {
      return conversationPageResults.first;
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
}

/// 只掐掉网络刷新，保留真实的消息入库/会话对账逻辑。
class _TestImService extends ImService {
  @override
  bool get isConnected => true;

  @override
  Future<void> refreshSessionsNow() async {}

  @override
  Future<void> refreshSessionsWindowNow() async {}

  @override
  Future<void> refreshSessionsIfStale({
    Duration maxAge = const Duration(seconds: 45),
  }) async {}

  @override
  Future<bool> loadMoreSessionWindowIfNeeded({bool force = false}) async =>
      false;

  @override
  Future<void> reconcilePinsFromConversationSummaries(
    List<ConversationSummaryModel> items, {
    required bool hasMore,
  }) async {}
}

SessionDetailResult _agentDetail() => const SessionDetailResult(
  data: {
    'session_type': 1,
    'members': [
      {'member_id': _myUserId, 'member_type': 1},
      {'member_id': _agentId, 'member_type': 2, 'nickname': 'Claude'},
    ],
  },
);

Future<void> _seedSession(
  String sessionId, {
  required int unreadCount,
  String peerId = '',
  int peerType = 0,
  int updatedAt = _baseTime,
}) async {
  await LocalDb.upsertSession({
    'session_id': sessionId,
    'title': '',
    'type': 'private',
    'peer_id': peerId,
    'peer_type': peerType,
    'peer_nickname': '',
    'peer_username': '',
    'updated_at': updatedAt,
    'is_pinned': false,
    'is_muted': false,
    'pinned_at': 0,
    'unread_count': unreadCount,
    'last_message': 'hello',
    'last_message_time': updatedAt,
  });
}

/// 只排空微任务：不推进任何 Timer，用来证明列表落地不依赖定时器。
Future<void> _drainMicrotasks() async {
  for (var i = 0; i < 8; i++) {
    await Future<void>.microtask(() {});
  }
}

String _pullSyncMessage({
  required String sessionId,
  required int senderType,
  required String senderId,
  required int inboxSeq,
  required int msgId,
  List<Map<String, dynamic>>? sessionMembers,
}) {
  return jsonEncode({
    'cmd': 'pull_sync_resp',
    'payload': {
      'has_more': false,
      'messages': [
        {
          'inbox_seq': inboxSeq,
          'msg_id': '$msgId',
          'session_id': sessionId,
          'session_type': 1,
          'sender_id': senderId,
          'sender_type': senderType,
          'msg_type': 1,
          'content': '系统通知',
          'created_at': _baseTime + 5000,
          if (sessionMembers != null) 'session_members': sessionMembers,
        },
      ],
    },
  });
}

String _pushMessage({
  required String sessionId,
  required int senderType,
  required String senderId,
  required int inboxSeq,
  required int msgId,
  List<Map<String, dynamic>>? sessionMembers,
}) {
  return jsonEncode({
    'cmd': 'push_msg',
    'payload': {
      'inbox_seq': inboxSeq,
      'msg_id': '$msgId',
      'session_id': sessionId,
      'session_type': 1,
      'sender_id': senderId,
      'sender_type': senderType,
      'msg_type': 1,
      'content': '系统通知',
      'created_at': _baseTime + 5000 + msgId,
      if (sessionMembers != null) 'session_members': sessionMembers,
    },
  });
}

void main() {
  late _TestImService imService;
  late _FakeSessionService sessionService;
  late String testUserId;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    UserImageCacheManager.setDisabledForTest(true);
    testUserId = 'unread-peer-${DateTime.now().microsecondsSinceEpoch}';
    await LocalDb.setActiveUser(testUserId);
    await LocalDb.clearActiveUserData();
    sessionService = _FakeSessionService();
    imService = _TestImService();
    Get.put<AuthService>(_FakeAuthService());
    Get.put<SessionService>(sessionService);
    Get.put<ImService>(imService);
    Get.put<FriendQrFlowService>(FriendQrFlowService());
  });

  tearDown(() async {
    imService.onClose();
    UserImageCacheManager.setDisabledForTest(false);
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  test('载荷带成员身份的系统消息：新线程未读当场并入已展示的 agent 分组行', () async {
    // 已展示的分组行：agent 8001 下已有一条读完的线程 thread-a。
    await _seedSession('thread-a', unreadCount: 0, peerId: _agentId, peerType: 2);
    sessionService.conversationPageResults.add(
      const ConversationPageResult(
        items: [
          ConversationSummaryModel(
            groupKey: _agentGroupKey,
            conversationType: 'private',
            latestSessionId: 'thread-a',
            title: 'Claude',
            peerId: _agentId,
            peerType: 2,
            sessionType: 1,
            lastMsg: 'hello',
            lastMsgTime: _baseTime,
            latestActiveAt: _baseTime,
            updatedAt: _baseTime,
          ),
        ],
      ),
    );

    await imService.loadSessions(refreshFromServer: false);
    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();
    expect(controller.groupedSessions.single.groupKey, _agentGroupKey);
    expect(controller.groupedSessions.single.badgeUnreadCount, 0);

    // 同一个 agent 下的新线程收到一条 sender_type=3 的私聊系统消息，
    // 服务端不是对端却带上了会话成员身份。
    final commitsBefore = controller.groupedSessionsCommitCount;
    await imService.handleDownstreamForTest(
      _pullSyncMessage(
        sessionId: 'thread-b',
        senderType: 3,
        senderId: '0',
        inboxSeq: 1,
        msgId: 9001,
        sessionMembers: const [
          {'member_id': _myUserId, 'member_type': 1},
          {'member_id': _agentId, 'member_type': 2},
        ],
      ),
    );
    await _drainMicrotasks();

    // 新线程从落库那一刻就带对端身份，归组键与服务端摘要一致。
    final threadB = imService.sessions.firstWhere(
      (s) => s.sessionId == 'thread-b',
    );
    expect(threadB.peerId, _agentId);
    expect(threadB.peerType, 2);

    // 列表与底部角标同一轮内对齐，且没有多补出一行。
    expect(imService.notificationUnread, 1);
    expect(controller.groupedSessions, hasLength(1));
    expect(controller.groupedSessions.single.groupKey, _agentGroupKey);
    expect(controller.groupedSessions.single.badgeUnreadCount, 1);
    expect(
      controller.groupedSessions.fold<int>(
        0,
        (sum, item) => sum + item.badgeUnreadCount,
      ),
      imService.notificationUnread,
    );
    // 新线程全程没有为对端身份补拉过网络（列表行的详情预取是另一条既有链路）。
    expect(sessionService.fetchedSessionIds, isNot(contains('thread-b')));
    // 一条消息最多让列表落地一次：未读对齐不能把会话页刷成高频重建。
    expect(controller.groupedSessionsCommitCount - commitsBefore, lessThanOrEqualTo(1));
  });

  test('旧格式载荷（不带成员身份）：未读增加后立刻重试回填，4003/4004 标记不再永久封死', () async {
    // 第一轮补拉返回 4004 → 会话被永久标记，之后不再重试。
    sessionService.detailResult = const SessionDetailResult(
      code: 4004,
      httpStatus: 404,
    );
    await _seedSession('thread-legacy', unreadCount: 1);
    await imService.loadSessions(refreshFromServer: false);
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(
      sessionService.fetchedSessionIds.where((s) => s == 'thread-legacy').length,
      1,
    );

    // 第二轮改为可用：一条不带成员身份的旧格式实时消息推高未读，
    // 应当就地重新放行回填，而不是干等下一次 loadSessions。
    sessionService.detailResult = _agentDetail();
    await imService.handleDownstreamForTest(
      _pushMessage(
        sessionId: 'thread-legacy',
        senderType: 3,
        senderId: '0',
        inboxSeq: 1,
        msgId: 9101,
      ),
    );
    await Future<void>.delayed(const Duration(milliseconds: 50));

    expect(
      sessionService.fetchedSessionIds.where((s) => s == 'thread-legacy').length,
      2,
    );
    final session = imService.sessions.firstWhere(
      (s) => s.sessionId == 'thread-legacy',
    );
    expect(session.peerId, _agentId);
    expect(session.peerType, 2);
  });

  test('回填重试有上限：始终 4004 的会话不会被每条消息重打', () async {
    sessionService.detailResult = const SessionDetailResult(
      code: 4004,
      httpStatus: 404,
    );
    await _seedSession('thread-gone', unreadCount: 1);
    await imService.loadSessions(refreshFromServer: false);
    await Future<void>.delayed(const Duration(milliseconds: 50));

    for (var i = 0; i < 5; i++) {
      await imService.handleDownstreamForTest(
        _pushMessage(
          sessionId: 'thread-gone',
          senderType: 3,
          senderId: '0',
          inboxSeq: i + 1,
          msgId: 9200 + i,
        ),
      );
      await Future<void>.delayed(const Duration(milliseconds: 30));
    }

    // 首轮 1 次 + 至多 2 次重新放行。
    expect(
      sessionService.fetchedSessionIds.where((s) => s == 'thread-gone').length,
      3,
    );
  });
}

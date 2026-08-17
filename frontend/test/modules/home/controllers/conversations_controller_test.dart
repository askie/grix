import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/conversation_summary_model.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/home/controllers/conversations_controller.dart';
import 'package:grix/modules/home/services/friend_qr_flow_service.dart';
import 'package:grix/shared/models/session_avatar_member.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';

class _FakeImService extends ImService {
  final Map<String, bool> muteResults = <String, bool>{};
  final List<String> mutedSessionIds = <String>[];
  final List<String> mutedPeerIds = <String>[];
  final Map<String, bool> peerMuteState = <String, bool>{};
  final List<String> deletedSessionIds = <String>[];
  int refreshSessionsNowCalls = 0;
  int refreshSessionsWindowNowCalls = 0;
  int refreshSessionsIfStaleCalls = 0;
  int loadMoreSessionWindowCalls = 0;
  bool loadMoreSessionWindowResult = false;
  bool shouldRefreshStaleSessions = false;

  @override
  bool get isConnected => true;

  @override
  Future<void> deleteConversation(String sessionId) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    deletedSessionIds.add(sid);
    sessions.removeWhere((session) => session.sessionId == sid);
  }

  @override
  Future<bool> setSessionMuted(
    String sessionId, {
    required bool isMuted,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return false;
    }
    mutedSessionIds.add(sid);
    final result = muteResults[sid] ?? true;
    if (!result) {
      return false;
    }
    final idx = sessions.indexWhere((s) => s.sessionId == sid);
    if (idx >= 0) {
      sessions[idx] = sessions[idx].copyWith(isMuted: isMuted);
    }
    return true;
  }

  @override
  Future<void> refreshSessionsNow() async {
    refreshSessionsNowCalls++;
  }

  @override
  Future<void> refreshSessionsWindowNow() async {
    refreshSessionsWindowNowCalls++;
    refreshSessionsNowCalls++;
  }

  @override
  Future<void> refreshSessionsIfStale({
    Duration maxAge = const Duration(seconds: 45),
  }) async {
    refreshSessionsIfStaleCalls++;
    if (shouldRefreshStaleSessions) {
      refreshSessionsNowCalls++;
    }
  }

  @override
  Future<bool> loadMoreSessionWindowIfNeeded({bool force = false}) async {
    loadMoreSessionWindowCalls++;
    return loadMoreSessionWindowResult;
  }

  // 探针：记录所有走 session 级置顶接口（/sessions/pin）的会话。
  // 仅记录、不改状态，避免影响依赖“fallback 失败”假设的既有测试。
  final List<String> pinnedViaSessionApi = <String>[];

  @override
  Future<bool> setSessionPinned(
    String sessionId, {
    required bool isPinned,
  }) async {
    pinnedViaSessionApi.add(sessionId);
    return false;
  }

  @override
  Future<void> applyLocalFriendPin({
    required List<String> sessionIds,
    required bool isPinned,
    required int pinnedAt,
  }) async {
    for (final sid in sessionIds) {
      final normalized = sid.trim();
      if (normalized.isEmpty) continue;
      final idx = sessions.indexWhere((s) => s.sessionId == normalized);
      if (idx < 0) continue;
      sessions[idx] = sessions[idx].copyWith(
        friendIsPinned: isPinned,
        friendPinnedAt: pinnedAt,
      );
    }
    sessions.refresh();
  }

  @override
  Future<void> applyLocalFriendMute({
    required String peerId,
    required List<String> sessionIds,
    required bool isMuted,
  }) async {
    mutedPeerIds.add(peerId.trim());
    peerMuteState[peerId.trim()] = isMuted;
    for (var i = 0; i < sessions.length; i++) {
      final session = sessions[i];
      final matchesPeer =
          session.type == 'private' && session.peerId.trim() == peerId.trim();
      final matchesId = sessionIds.contains(session.sessionId);
      if (!matchesPeer && !matchesId) continue;
      sessions[i] = session.copyWith(friendIsMuted: isMuted);
    }
    sessions.refresh();
  }

  @override
  bool isPeerMuted(String peerId) {
    return peerMuteState[peerId.trim()] == true;
  }

  @override
  void reconcilePeerMuteFromServer(String peerId, bool isMuted) {
    final id = peerId.trim();
    if (id.isEmpty) return;
    if (peerMuteState.containsKey(id)) return;
    peerMuteState[id] = isMuted;
  }

  @override
  Future<void> reconcilePinsFromConversationSummaries(
    List<ConversationSummaryModel> items, {
    required bool hasMore,
  }) async {}
}

class _FakeSessionService extends SessionService {
  SessionDetailResult detailResult = const SessionDetailResult(data: null);
  @override
  bool initialized = false;
  final List<ConversationPageResult> conversationPageResults =
      <ConversationPageResult>[];
  final Map<String, ConversationThreadPageResult> threadResults =
      <String, ConversationThreadPageResult>{};
  int fetchCalls = 0;
  int conversationPageCalls = 0;
  int conversationThreadCalls = 0;
  int createCalls = 0;
  String? createdPeerId;
  int? createdPeerType;
  String? createResultSessionId;

  @override
  bool get isInitialized => initialized;

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    fetchCalls++;
    return detailResult;
  }

  @override
  Future<ConversationPageResult> fetchConversationPage({
    int limit = 30,
    String cursor = '',
  }) async {
    conversationPageCalls++;
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
    conversationThreadCalls++;
    return threadResults[groupKey] ??
        ConversationThreadPageResult(groupKey: groupKey, success: false);
  }

  @override
  Future<String?> createSession(String peerId, int peerType) async {
    createCalls++;
    createdPeerId = peerId;
    createdPeerType = peerType;
    return createResultSessionId;
  }
}

class _FakeFriendService extends FriendService {
  final Map<String, String> nicknames = <String, String>{};
  bool setFriendPinnedResult = true;
  bool setFriendMutedResult = true;
  final List<String> pinnedFriendUserIds = <String>[];
  final List<String> mutedFriendUserIds = <String>[];

  @override
  Future<void> loadFriendList() async {}

  @override
  Future<void> ensureUserProfiles(List<String> userIds) async {}

  @override
  Future<String?> fetchUserProfile(String userId) async => nicknames[userId];

  @override
  String? getUserNickname(String userId) => nicknames[userId];

  @override
  Future<bool> setFriendPinned({
    required String friendUserId,
    required bool isPinned,
  }) async {
    pinnedFriendUserIds.add(friendUserId);
    return setFriendPinnedResult;
  }

  @override
  Future<bool> setFriendMuted({
    required String friendUserId,
    required bool isMuted,
  }) async {
    mutedFriendUserIds.add(friendUserId);
    return setFriendMutedResult;
  }
}

class _FakeAgentService extends AgentService {
  @override
  Future<void> loadAgents({String? categoryId}) async {}
}

class _FakeAuthService extends AuthService {
  _FakeAuthService(this._userId);

  final String _userId;

  @override
  String? get userId => _userId;
}

void main() {
  late _FakeImService imService;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    UserImageCacheManager.setDisabledForTest(true);

    imService = _FakeImService();

    Get.put<ImService>(imService);
    Get.put<FriendQrFlowService>(FriendQrFlowService());
  });

  tearDown(() async {
    UserImageCacheManager.setDisabledForTest(false);
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  test('groupedSessions returns all when query is empty', () {
    final now = DateTime.now().millisecondsSinceEpoch;
    imService.sessions.assignAll(
      List.generate(
        5000,
        (index) => SessionModel(
          sessionId: 'g_$index',
          title: 'Group $index',
          type: 'group',
          updatedAt: now - index,
          unreadCount: index % 3,
          lastMessage: 'hello $index',
          lastMessageTime: now - index,
        ),
      ),
    );

    final controller = Get.put(ConversationsController());
    expect(controller.groupedSessions.length, 5000);
  });

  test('search queries local database by title', () async {
    const userId = 'search-user-1';
    final now = DateTime.now().millisecondsSinceEpoch;
    await LocalDb.setActiveUser(userId);
    await LocalDb.clearActiveUserData();

    await LocalDb.upsertSession({
      'session_id': 'g_alpha',
      'title': 'Alpha Support Room',
      'type': 'group',
      'updated_at': now,
      'last_message': 'hello',
      'last_message_time': now,
    });
    await LocalDb.upsertSession({
      'session_id': 'g_beta',
      'title': 'Beta Channel',
      'type': 'group',
      'updated_at': now - 1000,
      'last_message': 'world',
      'last_message_time': now - 1000,
    });

    final controller = Get.put(ConversationsController());

    controller.updateSearchQuery('alpha');
    await Future<void>.delayed(const Duration(milliseconds: 300));
    var result = controller.groupedSessions;
    expect(result.length, 1);
    expect(result.first.latestSession.sessionId, 'g_alpha');

    controller.updateSearchQuery('beta');
    await Future<void>.delayed(const Duration(milliseconds: 300));
    result = controller.groupedSessions;
    expect(result.length, 1);
    expect(result.first.latestSession.sessionId, 'g_beta');

    await LocalDb.setActiveUser(null);
  });

  test('search queries local database by last_message content', () async {
    const userId = 'search-user-2';
    final now = DateTime.now().millisecondsSinceEpoch;
    await LocalDb.setActiveUser(userId);
    await LocalDb.clearActiveUserData();

    await LocalDb.upsertSession({
      'session_id': 'g_alarm',
      'title': 'Group A',
      'type': 'group',
      'updated_at': now,
      'last_message': 'system critical alarm token',
      'last_message_time': now,
    });
    await LocalDb.upsertSession({
      'session_id': 'g_normal',
      'title': 'Group B',
      'type': 'group',
      'updated_at': now - 1000,
      'last_message': 'hello world',
      'last_message_time': now - 1000,
    });

    final controller = Get.put(ConversationsController());

    controller.updateSearchQuery('alarm');
    await Future<void>.delayed(const Duration(milliseconds: 300));
    final result = controller.groupedSessions;
    expect(result.length, 1);
    expect(result.first.latestSession.sessionId, 'g_alarm');

    await LocalDb.setActiveUser(null);
  });

  test('search queries local database by peer_nickname', () async {
    const userId = 'search-user-3';
    final now = DateTime.now().millisecondsSinceEpoch;
    await LocalDb.setActiveUser(userId);
    await LocalDb.clearActiveUserData();

    await LocalDb.upsertSession({
      'session_id': 's_alice',
      'title': '',
      'type': 'private',
      'peer_id': '1001',
      'peer_nickname': 'Alice Wang',
      'updated_at': now,
      'last_message': 'hi',
      'last_message_time': now,
    });
    await LocalDb.upsertSession({
      'session_id': 's_bob',
      'title': '',
      'type': 'private',
      'peer_id': '1002',
      'peer_nickname': 'Bob Li',
      'updated_at': now - 1000,
      'last_message': 'hey',
      'last_message_time': now - 1000,
    });

    final controller = Get.put(ConversationsController());

    controller.updateSearchQuery('Alice');
    await Future<void>.delayed(const Duration(milliseconds: 300));
    final result = controller.groupedSessions;
    expect(result.length, 1);
    expect(result.first.latestSession.sessionId, 's_alice');

    await LocalDb.setActiveUser(null);
  });

  test('clearing search restores full in-memory session list', () async {
    const userId = 'search-user-4';
    final now = DateTime.now().millisecondsSinceEpoch;
    await LocalDb.setActiveUser(userId);
    await LocalDb.clearActiveUserData();

    await LocalDb.upsertSession({
      'session_id': 'g_only_db',
      'title': 'DB Only Session',
      'type': 'group',
      'updated_at': now - 5000,
      'last_message': 'archived',
      'last_message_time': now - 5000,
    });

    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'g_mem1',
        title: 'Memory Session 1',
        type: 'group',
        updatedAt: now,
        lastMessage: 'recent',
        lastMessageTime: now,
      ),
      SessionModel(
        sessionId: 'g_mem2',
        title: 'Memory Session 2',
        type: 'group',
        updatedAt: now - 1000,
        lastMessage: 'older',
        lastMessageTime: now - 1000,
      ),
    ]);

    final controller = Get.put(ConversationsController());
    expect(controller.groupedSessions.length, 2);

    controller.updateSearchQuery('DB Only');
    await Future<void>.delayed(const Duration(milliseconds: 300));
    expect(controller.groupedSessions.length, 1);
    expect(
      controller.groupedSessions.first.latestSession.sessionId,
      'g_only_db',
    );

    controller.updateSearchQuery('');
    await Future<void>.delayed(const Duration(milliseconds: 300));
    expect(controller.groupedSessions.length, 2);

    await LocalDb.setActiveUser(null);
  });

  test('search groups private sessions by peer_id', () async {
    const userId = 'search-user-5';
    final now = DateTime.now().millisecondsSinceEpoch;
    await LocalDb.setActiveUser(userId);
    await LocalDb.clearActiveUserData();

    await LocalDb.upsertSession({
      'session_id': 's_thread1',
      'title': 'Alice Thread 1',
      'type': 'private',
      'peer_id': '1001',
      'peer_type': 1,
      'peer_nickname': 'Alice',
      'updated_at': now - 1000,
      'last_message': 'hello',
      'last_message_time': now - 1000,
    });
    await LocalDb.upsertSession({
      'session_id': 's_thread2',
      'title': 'Alice Thread 2',
      'type': 'private',
      'peer_id': '1001',
      'peer_type': 1,
      'peer_nickname': 'Alice',
      'updated_at': now,
      'last_message': 'world',
      'last_message_time': now,
    });

    final controller = Get.put(ConversationsController());

    controller.updateSearchQuery('Alice');
    await Future<void>.delayed(const Duration(milliseconds: 300));
    final result = controller.groupedSessions;
    expect(result.length, 1);
    expect(result.first.threadCount, 2);

    await LocalDb.setActiveUser(null);
  });

  test('memory rebuild is skipped while search is active', () async {
    const userId = 'search-user-6';
    final now = DateTime.now().millisecondsSinceEpoch;
    await LocalDb.setActiveUser(userId);
    await LocalDb.clearActiveUserData();

    await LocalDb.upsertSession({
      'session_id': 'g_target',
      'title': 'Target',
      'type': 'group',
      'updated_at': now,
      'last_message': 'found',
      'last_message_time': now,
    });

    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'g_live1',
        title: 'Live 1',
        type: 'group',
        updatedAt: now,
        lastMessage: 'msg1',
        lastMessageTime: now,
      ),
    ]);

    final controller = Get.put(ConversationsController());

    controller.updateSearchQuery('Target');
    await Future<void>.delayed(const Duration(milliseconds: 300));
    expect(controller.groupedSessions.length, 1);
    expect(
      controller.groupedSessions.first.latestSession.sessionId,
      'g_target',
    );

    imService.sessions.add(
      SessionModel(
        sessionId: 'g_live2',
        title: 'Live 2',
        type: 'group',
        updatedAt: now + 1000,
        lastMessage: 'new msg',
        lastMessageTime: now + 1000,
      ),
    );
    imService.sessions.refresh();
    await Future<void>.delayed(const Duration(milliseconds: 100));

    expect(controller.groupedSessions.length, 1);
    expect(
      controller.groupedSessions.first.latestSession.sessionId,
      'g_target',
    );

    await LocalDb.setActiveUser(null);
  });

  test('groupedSessions folds private sessions by peer id', () {
    final now = DateTime.now().millisecondsSinceEpoch;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 's-1',
        title: 'Alice',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now - 2000,
        unreadCount: 2,
        lastMessage: 'old',
        lastMessageTime: now - 2000,
      ),
      SessionModel(
        sessionId: 's-2',
        title: 'Alice',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now - 1000,
        unreadCount: 1,
        lastMessage: 'new',
        lastMessageTime: now - 1000,
      ),
      SessionModel(
        sessionId: 's-3',
        title: 'Bob',
        type: 'private',
        peerId: '1002',
        peerType: 1,
        updatedAt: now,
        unreadCount: 0,
        lastMessage: 'bob msg',
        lastMessageTime: now,
      ),
    ]);

    final controller = Get.put(ConversationsController());
    final groups = controller.groupedSessions;
    expect(groups.length, 2);

    final aliceGroup = groups.firstWhere(
      (item) => item.latestSession.peerId == '1001',
    );
    expect(aliceGroup.threadCount, 2);
    expect(aliceGroup.unreadCount, 3);
    expect(aliceGroup.latestSession.sessionId, 's-2');
  });

  test('thread popup sessions sort by latest activity descending', () {
    final now = DateTime.now().millisecondsSinceEpoch;
    final controller = Get.put(ConversationsController());

    final ordered = controller.getThreadSessionsByLatestActivityDesc([
      SessionModel(
        sessionId: 's-new-empty-thread',
        title: 'Alice new thread',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now + 5000,
        unreadCount: 0,
        lastMessage: '',
        lastMessageTime: 0,
      ),
      SessionModel(
        sessionId: 's-mid-msg',
        title: 'Alice mid',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now - 1000,
        unreadCount: 0,
        lastMessage: 'middle message',
        lastMessageTime: now - 1000,
      ),
      SessionModel(
        sessionId: 's-latest-msg',
        title: 'Alice latest',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now - 2000,
        unreadCount: 0,
        lastMessage: 'latest message',
        lastMessageTime: now,
      ),
    ]);

    expect(ordered.map((session) => session.sessionId).toList(), [
      's-new-empty-thread',
      's-latest-msg',
      's-mid-msg',
    ]);
  });

  test(
    'createFreshPrivateSession creates a new session for grouped peer',
    () async {
      final now = DateTime.now().millisecondsSinceEpoch;
      final sessionService = _FakeSessionService();
      Get.put<SessionService>(sessionService);

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's-1',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());

      await controller.createFreshPrivateSession(
        controller.groupedSessions.single,
      );

      expect(sessionService.createCalls, 1);
      expect(sessionService.createdPeerId, '1001');
      expect(sessionService.createdPeerType, 1);
    },
  );

  test(
    'grouped private session summary follows newest message even when another thread is pinned',
    () {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's-pinned',
          title: 'Alice pinned',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now - 5000,
          friendIsPinned: true,
          friendPinnedAt: now - 1000,
          unreadCount: 0,
          lastMessage: 'older pinned',
          lastMessageTime: now - 5000,
        ),
        SessionModel(
          sessionId: 's-fresh',
          title: 'Alice fresh',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now,
          unreadCount: 1,
          lastMessage: 'latest message',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final group = controller.groupedSessions.single;

      expect(group.isPinned, isTrue);
      expect(group.latestSession.sessionId, 's-fresh');
      expect(controller.getConversationLatestSummary(group), 'latest message');
    },
  );

  test(
    'grouped conversation summary prefers the active short stream reply',
    () async {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assign(
        SessionModel(
          sessionId: 'stream-preview-session',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now,
          lastMessage: '上一条消息',
          lastMessageTime: now,
        ),
      );
      final controller = Get.put(ConversationsController());

      await imService.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'stream-preview-msg',
            'session_id': 'stream-preview-session',
            'sender_id': 'agent-1',
            'sender_type': 2,
            'delta_content': '好的，我来',
            'chunk_seq': 1,
            'created_at': now + 1000,
          },
        }),
      );

      await Future<void>.delayed(const Duration(milliseconds: 230));
      expect(
        controller.getConversationLatestSummary(
          controller.groupedSessions.single,
        ),
        '好的，我来',
      );
    },
  );

  test(
    'groupedSessions tracks muted unread and visible badge unread separately',
    () {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's-1',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now - 2000,
          unreadCount: 2,
          isMuted: true,
          lastMessage: 'muted',
          lastMessageTime: now - 2000,
        ),
        SessionModel(
          sessionId: 's-2',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now - 1000,
          unreadCount: 1,
          isMuted: false,
          lastMessage: 'active',
          lastMessageTime: now - 1000,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final group = controller.groupedSessions.single;
      expect(group.unreadCount, 3);
      expect(group.badgeUnreadCount, 1);
      expect(group.hasMutedUnread, isTrue);
    },
  );

  test('groupedSessions unread excludes current session after switch', () {
    final now = DateTime.now().millisecondsSinceEpoch;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 's-current',
        title: 'Alice',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now,
        unreadCount: 3,
        isMuted: false,
        lastMessage: 'latest',
        lastMessageTime: now,
      ),
    ]);

    final controller = Get.put(ConversationsController());
    expect(controller.groupedSessions.single.unreadCount, 3);

    imService.setCurrentSessionForTest('s-current');

    expect(controller.groupedSessions.single.unreadCount, 0);
    expect(controller.groupedSessions.single.badgeUnreadCount, 0);
  });

  test(
    'grouped avatar unread total stays aligned with bottom notificationUnread',
    () {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's-muted',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now - 4000,
          unreadCount: 2,
          isMuted: true,
          lastMessage: 'muted',
          lastMessageTime: now - 4000,
        ),
        SessionModel(
          sessionId: 's-visible',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now - 3000,
          unreadCount: 3,
          isMuted: false,
          lastMessage: 'visible',
          lastMessageTime: now - 3000,
        ),
        SessionModel(
          sessionId: 's-group',
          title: 'Team',
          type: 'group',
          peerId: '',
          peerType: 0,
          updatedAt: now - 2000,
          unreadCount: 1,
          isMuted: false,
          lastMessage: 'group',
          lastMessageTime: now - 2000,
        ),
        SessionModel(
          sessionId: 's-current',
          title: 'Bob',
          type: 'private',
          peerId: '2002',
          peerType: 1,
          updatedAt: now - 1000,
          unreadCount: 4,
          isMuted: false,
          lastMessage: 'current',
          lastMessageTime: now - 1000,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      imService.setCurrentSessionForTest('s-current');

      final groupedBadgeTotal = controller.groupedSessions.fold<int>(
        0,
        (sum, item) => sum + item.badgeUnreadCount,
      );

      expect(groupedBadgeTotal, imService.notificationUnread);
      expect(groupedBadgeTotal, 4);
    },
  );

  test(
    'groupedSessions marks grouped conversation when unread remote mention targets current user',
    () async {
      const currentUserId = '9001';
      final now = DateTime.now().millisecondsSinceEpoch;
      await LocalDb.setActiveUser(currentUserId);
      await LocalDb.clearActiveUserData();
      Get.put<AuthService>(_FakeAuthService(currentUserId));

      await LocalDb.upsertMessage({
        'msg_id': 'msg-mention-1',
        'session_id': 's-mention',
        'sender_id': '2002',
        'sender_type': 1,
        'msg_type': 1,
        'content': '@me please check',
        'extra': jsonEncode({
          'mention_user_ids': [currentUserId],
        }),
        'inbox_seq': 1,
        'created_at': now - 2000,
      });
      await LocalDb.upsertMessage({
        'msg_id': 'msg-self-latest',
        'session_id': 's-mention',
        'sender_id': currentUserId,
        'sender_type': 1,
        'msg_type': 1,
        'content': 'latest message from me',
        'extra': '{}',
        'inbox_seq': 2,
        'created_at': now - 1000,
      });
      await LocalDb.upsertMessage({
        'msg_id': 'msg-plain-1',
        'session_id': 's-plain',
        'sender_id': '2002',
        'sender_type': 1,
        'msg_type': 1,
        'content': 'plain unread',
        'extra': '{}',
        'inbox_seq': 3,
        'created_at': now - 500,
      });

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's-mention',
          title: 'Alice main',
          type: 'private',
          peerId: '2002',
          peerType: 1,
          updatedAt: now,
          unreadCount: 1,
          lastMessage: 'latest message from me',
          lastMessageTime: now - 1000,
        ),
        SessionModel(
          sessionId: 's-plain',
          title: 'Alice side',
          type: 'private',
          peerId: '2002',
          peerType: 1,
          updatedAt: now - 500,
          unreadCount: 1,
          lastMessage: 'plain unread',
          lastMessageTime: now - 500,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      await Future<void>.delayed(const Duration(milliseconds: 250));

      final group = controller.groupedSessions.single;
      expect(group.unreadCount, 2);
      expect(group.hasUnreadMention, isTrue);
    },
  );

  test(
    'groupedSessions sorts by latest message time when updatedAt is stale',
    () {
      const staleUpdatedAt = 1700000000000;
      const newerUpdatedAt = 1700000005000;
      const latestMessageTime = 1700000010000;

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's-stale',
          title: 'stale',
          type: 'group',
          updatedAt: staleUpdatedAt,
          unreadCount: 0,
          lastMessage: 'newest',
          lastMessageTime: latestMessageTime,
        ),
        SessionModel(
          sessionId: 's-mid',
          title: 'mid',
          type: 'group',
          updatedAt: newerUpdatedAt,
          unreadCount: 0,
          lastMessage: 'older',
          lastMessageTime: newerUpdatedAt,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final groups = controller.groupedSessions;

      expect(groups.map((item) => item.latestSession.sessionId).toList(), [
        's-stale',
        's-mid',
      ]);
      expect(groups.first.latestSession.activityAt, latestMessageTime);
    },
  );

  test('groupedSessions puts pinned conversations first', () {
    final now = DateTime.now().millisecondsSinceEpoch;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 's-unpinned',
        title: 'unpinned',
        type: 'group',
        updatedAt: now + 1000,
        unreadCount: 0,
        lastMessage: 'newer',
        lastMessageTime: now + 1000,
      ),
      SessionModel(
        sessionId: 's-pinned',
        title: 'pinned',
        type: 'group',
        updatedAt: now,
        isPinned: true,
        pinnedAt: now,
        unreadCount: 0,
        lastMessage: 'older',
        lastMessageTime: now,
      ),
    ]);

    final controller = Get.put(ConversationsController());
    final groups = controller.groupedSessions;

    expect(groups.first.latestSession.sessionId, 's-pinned');
    expect(groups.first.isPinned, isTrue);
  });

  test('groupedSessions reorders pinned conversations by latest activity', () {
    final now = DateTime.now().millisecondsSinceEpoch;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 's-pinned-old',
        title: 'pinned old',
        type: 'group',
        updatedAt: now - 1000,
        isPinned: true,
        pinnedAt: now,
        unreadCount: 0,
        lastMessage: 'older activity',
        lastMessageTime: now - 1000,
      ),
      SessionModel(
        sessionId: 's-pinned-fresh',
        title: 'pinned fresh',
        type: 'group',
        updatedAt: now,
        isPinned: true,
        pinnedAt: now - 2000,
        unreadCount: 0,
        lastMessage: 'newer activity',
        lastMessageTime: now,
      ),
    ]);

    final controller = Get.put(ConversationsController());
    final groups = controller.groupedSessions;

    expect(groups.map((item) => item.latestSession.sessionId).toList(), [
      's-pinned-fresh',
      's-pinned-old',
    ]);
  });

  test(
    'AI(agent) private pin should write friend-level, NOT session-level',
    () async {
      final now = DateTime.now().millisecondsSinceEpoch;
      final friendService = _FakeFriendService();
      Get.put<FriendService>(friendService);
      // AI 助手私聊：peerType=2，peerId 是 agent 的真实 id。
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'private-agent-1',
          title: 'Agent A',
          type: 'private',
          peerId: 'agent-123',
          peerType: 2,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hi',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final item = controller.groupedSessions.single;

      // 置顶
      await controller.setSessionGroupPinned(item, isPinned: true);
      await Future<void>.delayed(Duration.zero);

      // 期望：AI 私聊也应走好友/对端级置顶（写 user_peer_pins），
      // 即调用 setFriendPinned，且不落到 session 级置顶接口。
      expect(friendService.pinnedFriendUserIds, [
        'agent-123',
      ], reason: 'AI 私聊置顶应写对端级 pin');
      expect(
        imService.pinnedViaSessionApi,
        isEmpty,
        reason: 'AI 私聊不应走 session 级 pin 接口',
      );

      // 取消置顶
      await controller.setSessionGroupPinned(
        controller.groupedSessions.single,
        isPinned: false,
      );
      await Future<void>.delayed(Duration.zero);

      expect(friendService.pinnedFriendUserIds, [
        'agent-123',
        'agent-123',
      ], reason: 'AI 私聊取消置顶应再次写对端级 pin=false');
      expect(imService.pinnedViaSessionApi, isEmpty);
    },
  );

  test(
    'human friend private pin writes friend-level (control group)',
    () async {
      final now = DateTime.now().millisecondsSinceEpoch;
      final friendService = _FakeFriendService();
      Get.put<FriendService>(friendService);
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'private-human-1',
          title: 'Human B',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hi',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final item = controller.groupedSessions.single;

      await controller.setSessionGroupPinned(item, isPinned: true);
      await Future<void>.delayed(Duration.zero);

      expect(friendService.pinnedFriendUserIds, ['1001']);
      expect(imService.pinnedViaSessionApi, isEmpty);
    },
  );

  test('private conversation pin writes friend-level state', () async {
    final now = DateTime.now().millisecondsSinceEpoch;
    final friendService = _FakeFriendService();
    Get.put<FriendService>(friendService);
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'private-pin-success',
        title: 'Alice',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now,
        unreadCount: 0,
        lastMessage: 'hello',
        lastMessageTime: now,
      ),
    ]);

    final controller = Get.put(ConversationsController());
    final item = controller.groupedSessions.single;

    await controller.setSessionGroupPinned(item, isPinned: true);
    await Future<void>.delayed(Duration.zero);

    expect(friendService.pinnedFriendUserIds, ['1001']);
    expect(imService.sessions.single.isPinned, isFalse);
    expect(imService.sessions.single.friendIsPinned, isTrue);
    expect(controller.groupedSessions.single.isPinned, isTrue);
  });

  test(
    'private conversation pin failure does not optimistically reorder',
    () async {
      final now = DateTime.now().millisecondsSinceEpoch;
      final friendService = _FakeFriendService()..setFriendPinnedResult = false;
      Get.put<FriendService>(friendService);
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'private-pin-fail',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final item = controller.groupedSessions.single;

      await controller.setSessionGroupPinned(item, isPinned: true);
      await Future<void>.delayed(Duration.zero);

      expect(friendService.pinnedFriendUserIds, ['1001']);
      expect(imService.sessions.single.isPinned, isFalse);
      expect(imService.sessions.single.friendIsPinned, isFalse);
      expect(controller.groupedSessions.single.isPinned, isFalse);
    },
  );

  test('private conversation summary ignores session-level pin state', () {
    final now = DateTime.now().millisecondsSinceEpoch;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'private-session-pinned-only',
        title: 'Alice',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now,
        isPinned: true,
        pinnedAt: now,
        unreadCount: 0,
        lastMessage: 'hello',
        lastMessageTime: now,
      ),
    ]);

    final controller = Get.put(ConversationsController());

    expect(controller.groupedSessions.single.isPinned, isFalse);
    expect(controller.groupedSessions.single.pinnedAt, 0);
  });

  test(
    'rapid session bursts coalesce and settle on the latest order',
    () async {
      final now = DateTime.now().millisecondsSinceEpoch;
      final controller = Get.put(ConversationsController());

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's-a',
          title: 'A',
          type: 'group',
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'A first',
          lastMessageTime: now,
        ),
        SessionModel(
          sessionId: 's-b',
          title: 'B',
          type: 'group',
          updatedAt: now - 1000,
          unreadCount: 0,
          lastMessage: 'B old',
          lastMessageTime: now - 1000,
        ),
      ]);
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's-a',
          title: 'A',
          type: 'group',
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'A first',
          lastMessageTime: now,
        ),
        SessionModel(
          sessionId: 's-b',
          title: 'B',
          type: 'group',
          updatedAt: now + 2000,
          unreadCount: 0,
          lastMessage: 'B latest',
          lastMessageTime: now + 2000,
        ),
      ]);

      await Future<void>.delayed(const Duration(milliseconds: 150));

      expect(controller.groupedSessions.first.latestSession.sessionId, 's-b');
      expect(
        controller.getConversationLatestSummary(
          controller.groupedSessions.first,
        ),
        'B latest',
      );
    },
  );

  test('getDisplayTitle prefers title and falls back to session id', () {
    final controller = Get.put(ConversationsController());

    final titled = SessionModel(
      sessionId: '18d9406d-f7ff-403f-8080-c768a6f75f2b',
      title: 'Agent Nine',
      type: 'private',
      updatedAt: 0,
      lastMessageTime: 0,
    );
    final untitled = SessionModel(
      sessionId: '94c7be24-baad-4013-b076-f8f0f94ba5d8',
      title: '',
      type: 'group',
      updatedAt: 0,
      lastMessageTime: 0,
    );

    expect(controller.getDisplayTitle(titled), 'Agent Nine');
    expect(
      controller.getDisplayTitle(untitled),
      '94c7be24-baad-4013-b076-f8f0f94ba5d8',
    );
  });

  test(
    'getDisplayTitle upgrades placeholder title to bound nickname',
    () async {
      const sessionId = '9a767aa4-84df-48a1-b47e-d4f87d20c2b0';
      imService.sessions.assignAll([
        SessionModel(
          sessionId: sessionId,
          title: sessionId,
          type: 'private',
          updatedAt: 0,
          lastMessageTime: 0,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      expect(controller.getDisplayTitle(imService.sessions.first), sessionId);

      await imService.bindSessionDisplayTitle(sessionId, 'Alice');
      expect(controller.getDisplayTitle(imService.sessions.first), 'Alice');
    },
  );

  test(
    'group list title stays on group name while summary uses latest message',
    () {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'group-title-1',
          title: '谁在?',
          type: 'group',
          updatedAt: now,
          unreadCount: 0,
          lastMessage: '我在这里!',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final item = controller.groupedSessions.single;

      expect(controller.getConversationListTitle(item), '谁在?');
      expect(controller.getConversationLatestSummary(item), '我在这里!');
      expect(controller.getConversationSecondaryText(item), '我在这里!');
    },
  );

  test(
    'bound group title appears in list without waiting for second reload',
    () async {
      await imService.bindSessionDisplayTitle(
        'group-bound-title-1',
        '小龙虾讨论组',
        type: 'group',
      );

      final controller = Get.put(ConversationsController());
      final item = controller.groupedSessions.single;

      expect(controller.getConversationListTitle(item), '小龙虾讨论组');
    },
  );

  test('group avatar cache refreshes when agent avatar changes', () async {
    final sessionService = _FakeSessionService();
    final agentService = _FakeAgentService();
    Get.put<SessionService>(sessionService);
    Get.put<AgentService>(agentService);

    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'group-agent-avatar-1',
        title: 'Ops Group',
        type: 'group',
        updatedAt: 1,
        unreadCount: 0,
        lastMessage: 'hello',
        lastMessageTime: 1,
      ),
    ]);
    sessionService.detailResult = const SessionDetailResult(
      data: {
        'session_type': 2,
        'members': [
          {'member_id': 'agent-1', 'member_type': 2, 'nickname': 'Agent Bot'},
        ],
      },
    );
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-1',
        agentName: 'Agent Bot',
        avatarUrl: 'https://example.com/agent-1.png',
      ),
    ]);

    final controller = Get.put(ConversationsController());
    final item = controller.groupedSessions.single;

    expect(controller.getConversationAvatarMembers(item), isEmpty);
    await Future<void>.delayed(Duration.zero);

    final initialMembers = controller.getConversationAvatarMembers(item);
    expect(initialMembers.single.avatarUrl, 'https://example.com/agent-1.png');

    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-1',
        agentName: 'Agent Bot',
        avatarUrl: 'https://example.com/agent-1-v2.png',
      ),
    ]);
    await Future<void>.delayed(Duration.zero);

    expect(controller.getConversationAvatarMembers(item), isEmpty);
    await Future<void>.delayed(Duration.zero);

    final refreshedMembers = controller.getConversationAvatarMembers(item);
    expect(
      refreshedMembers.single.avatarUrl,
      'https://example.com/agent-1-v2.png',
    );
  });

  test(
    'private list title prefers peer nickname over session id placeholder',
    () {
      const sessionId = 'eee18048-9724-4e38-84f6-65f100000001';
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: sessionId,
          title: '',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          peerNickname: 'Eason',
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hi',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final item = controller.groupedSessions.single;

      expect(controller.getConversationListTitle(item), 'Eason');
      expect(controller.getConversationLatestSummary(item), 'hi');
      expect(controller.getConversationSecondaryText(item), 'hi');
    },
  );

  test(
    'private list title falls back to bound session title before peer sync',
    () {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session-private-bound-title-1',
          title: 'Alice',
          type: 'private',
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final item = controller.groupedSessions.single;

      expect(controller.getConversationListTitle(item), 'Alice');
      expect(controller.getConversationMetaLine(item), 'Alice');
    },
  );

  test(
    'private agent list title keeps fresher session snapshot over stale agent cache',
    () {
      final now = DateTime.now().millisecondsSinceEpoch;
      final agentService = _FakeAgentService();
      Get.put<AgentService>(agentService);
      agentService.agents.assignAll([
        AgentModel(id: 'agent-rename-1', agentName: 'Old Agent Name'),
      ]);
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session-private-agent-rename-1',
          title: 'New Agent Name',
          type: 'private',
          peerId: 'agent-rename-1',
          peerType: 2,
          peerNickname: 'New Agent Name',
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final item = controller.groupedSessions.single;

      expect(controller.getConversationListTitle(item), 'New Agent Name');
      expect(controller.getConversationMetaLine(item), 'New Agent Name');
    },
  );

  test(
    'private agent custom thread title remains visible with stale agent cache',
    () {
      final now = DateTime.now().millisecondsSinceEpoch;
      final agentService = _FakeAgentService();
      Get.put<AgentService>(agentService);
      agentService.agents.assignAll([
        AgentModel(id: 'agent-rename-2', agentName: 'Old Agent Name'),
      ]);
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session-private-agent-rename-2',
          title: 'Custom Topic',
          type: 'private',
          peerId: 'agent-rename-2',
          peerType: 2,
          peerNickname: 'New Agent Name',
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final item = controller.groupedSessions.single;

      expect(controller.getConversationListTitle(item), 'New Agent Name');
      expect(controller.getConversationMetaLine(item), 'Custom Topic');
    },
  );

  test(
    'private list title does not use first-message snippet as primary title',
    () {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session-private-hi-1',
          title: 'hi',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          peerUsername: 'alice_user',
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hi',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final item = controller.groupedSessions.single;

      expect(controller.getConversationListTitle(item), 'alice_user');
      expect(controller.getConversationLatestSummary(item), 'hi');
      expect(controller.getConversationSecondaryText(item), 'hi');
    },
  );

  test(
    'getSessionThreadTitle falls back to normalized last message when title is placeholder',
    () {
      final controller = Get.put(ConversationsController());
      final session = SessionModel(
        sessionId: 'sid-placeholder-1',
        title: 'sid-placeholder-1',
        type: 'group',
        updatedAt: 0,
        lastMessage: '  Topic   Alpha   Summary  ',
        lastMessageTime: 0,
      );

      expect(controller.getSessionThreadTitle(session), 'Topic Alpha Summary');
    },
  );

  test('getSessionThreadPreview normalizes whitespace and returns empty '
      'when there is nothing to preview', () {
    final controller = Get.put(ConversationsController());
    final withMessage = SessionModel(
      sessionId: 'sid-preview-1',
      title: '',
      type: 'group',
      updatedAt: 0,
      lastMessage: '  hello   world ',
      lastMessageTime: 0,
    );
    final emptyMessage = SessionModel(
      sessionId: 'sid-preview-2',
      title: '',
      type: 'group',
      updatedAt: 0,
      lastMessage: '   ',
      lastMessageTime: 0,
    );

    expect(controller.getSessionThreadPreview(withMessage), 'hello world');
    expect(controller.getSessionThreadPreview(emptyMessage), '');
  });

  test('getSessionThreadPreview returns empty for standalone card messages', () {
    final controller = Get.put(ConversationsController());
    final cardSession = SessionModel(
      sessionId: 'sid-preview-card',
      title: '',
      type: 'group',
      updatedAt: 0,
      lastMessage:
          '[已创建远端 Agent](grix://card/egg_install_status?install_id=1&status=running)',
      lastMessageTime: 0,
    );

    expect(controller.getSessionThreadPreview(cardSession), '');
  });

  test('getSessionThreadPreview summarizes markdown image content', () {
    final controller = Get.put(ConversationsController());
    final session = SessionModel(
      sessionId: 'sid-preview-image',
      title: '',
      type: 'group',
      updatedAt: 0,
      lastMessage: '![image](https://example.com/demo.png)',
      lastMessageTime: 0,
    );

    expect(controller.getSessionThreadPreview(session), '[image]');
  });

  test(
    'getConversationLatestSummary replaces @userId with friend nickname',
    () {
      final now = DateTime.now().millisecondsSinceEpoch;
      final friendService = _FakeFriendService();
      friendService.nicknames['2030840865701756928'] = '老郭';
      Get.put<FriendService>(friendService);

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'g-mention',
          title: 'group',
          type: 'group',
          updatedAt: now,
          lastMessage: 'hi @2030840865701756928 在吗',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final item = controller.groupedSessions.firstWhere(
        (it) => it.latestSession.sessionId == 'g-mention',
      );
      expect(controller.getConversationLatestSummary(item), 'hi @老郭 在吗');
    },
  );

  test(
    'getConversationLatestSummary keeps raw @userId when no display name found',
    () {
      final now = DateTime.now().millisecondsSinceEpoch;
      final friendService = _FakeFriendService();
      Get.put<FriendService>(friendService);

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'g-mention-miss',
          title: 'group',
          type: 'group',
          updatedAt: now,
          lastMessage: 'ping @9999999999999999999 上线了',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final item = controller.groupedSessions.firstWhere(
        (it) => it.latestSession.sessionId == 'g-mention-miss',
      );
      expect(
        controller.getConversationLatestSummary(item),
        'ping @9999999999999999999 上线了',
      );
    },
  );

  test('getSessionThreadTitle extracts structured text summary', () {
    final controller = Get.put(ConversationsController());
    final session = SessionModel(
      sessionId: 'sid-structured-title',
      title: '',
      type: 'group',
      updatedAt: 0,
      lastMessage: '{"content":"收到！ ✅测试正常～ 还需要测试什么吗？"}',
      lastMessageTime: 0,
    );

    expect(controller.getSessionThreadTitle(session), '收到！ ✅测试正常～ 还需要测试什么吗？');
  });

  test(
    'private list title uses peer identity while meta keeps thread title',
    () {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's-a',
          title: 'custom-a',
          type: 'private',
          peerId: '2029516931815444480',
          peerType: 0,
          peerUsername: 'alice_user',
          updatedAt: now - 10,
          unreadCount: 0,
          lastMessage: 'older',
          lastMessageTime: now - 10,
        ),
        SessionModel(
          sessionId: 's-b',
          title: 'custom-b',
          type: 'private',
          peerId: '2029516931815444480',
          peerType: 0,
          peerUsername: 'alice_user',
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'latest',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final groups = controller.groupedSessions;
      expect(groups.length, 1);

      expect(controller.getConversationListTitle(groups.first), 'alice_user');
      final meta = controller.getConversationMetaLine(groups.first);
      expect(meta.contains('custom-b'), isTrue);
      expect(meta.contains('2'), isTrue);
    },
  );

  test('private search still matches hidden thread title', () async {
    final now = DateTime.now().millisecondsSinceEpoch;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 's-search-private-1',
        title: 'Topic Alpha',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        peerNickname: 'Alice',
        updatedAt: now,
        unreadCount: 0,
        lastMessage: 'latest',
        lastMessageTime: now,
      ),
    ]);

    final controller = Get.put(ConversationsController());
    controller.updateSearchQuery('topic alpha');
    await Future<void>.delayed(const Duration(milliseconds: 150));

    final groups = controller.groupedSessions;
    expect(groups.length, 1);
    expect(controller.getConversationListTitle(groups.first), 'Alice');
  });

  test(
    'canOpenAccountInfo returns true for private and group conversations',
    () {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'private-s1',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now,
          lastMessageTime: now,
        ),
        SessionModel(
          sessionId: 'group-s1',
          title: 'Project Group',
          type: 'group',
          updatedAt: now - 1000,
          lastMessageTime: now - 1000,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final items = controller.groupedSessions;
      final privateItem = items.firstWhere(
        (item) => item.latestSession.type == 'private',
      );
      final groupItem = items.firstWhere(
        (item) => item.latestSession.type == 'group',
      );

      expect(controller.canOpenAccountInfo(privateItem), isTrue);
      expect(controller.canOpenAccountInfo(groupItem), isTrue);
    },
  );

  test(
    'conversation group avatar member display prefers session member nickname',
    () async {
      final sessionService = _FakeSessionService();
      sessionService.detailResult = const SessionDetailResult(
        data: {
          'session_type': 2,
          'members': [
            {'member_id': '1001', 'member_type': 1, 'nickname': '会话昵称A'},
            {'member_id': '1002', 'member_type': 1, 'nickname': '会话昵称B'},
          ],
        },
      );
      final friendService = _FakeFriendService();
      friendService.nicknames['1002'] = '本地昵称B';
      Get.put<SessionService>(sessionService);
      Get.put<FriendService>(friendService);

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'group-avatar-1',
          title: 'Avatar Group',
          type: 'group',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      final item = controller.groupedSessions.single;

      expect(controller.getConversationAvatarMembers(item), isEmpty);
      await Future<void>.delayed(Duration.zero);
      await Future<void>.delayed(Duration.zero);

      final members = controller.getConversationAvatarMembers(item);
      final member = members.firstWhere((m) => m.memberId == '1002');
      expect(member.displayName, '会话昵称B');
    },
  );

  test('cached group avatar members are used on first render', () async {
    const cachedMembers = [
      SessionAvatarMember(
        memberId: '1001',
        memberType: 1,
        displayName: '缓存A',
        avatarUrl: 'https://example.com/a.png',
      ),
      SessionAvatarMember(
        memberId: '1002',
        memberType: 1,
        displayName: '缓存B',
        avatarUrl: '',
      ),
    ];
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'group-avatar-cached',
        title: 'Avatar Group',
        type: 'group',
        updatedAt: 1,
        lastMessageTime: 1,
        cachedGroupAvatarMembers: cachedMembers,
      ),
    ]);

    final controller = Get.put(ConversationsController());
    final item = controller.groupedSessions.single;

    expect(controller.getConversationAvatarMembers(item), cachedMembers);
  });

  test('conversation summary group avatar inherits local cache', () async {
    const cachedMembers = [
      SessionAvatarMember(
        memberId: '1001',
        memberType: 1,
        displayName: '缓存A',
        avatarUrl: 'https://example.com/a.png',
      ),
    ];
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'summary-group-avatar-cached',
        title: 'Local Group',
        type: 'group',
        updatedAt: 1700000000000,
        lastMessageTime: 1700000000000,
        cachedGroupAvatarMembers: cachedMembers,
      ),
    ]);
    final sessionService = _FakeSessionService()
      ..initialized = true
      ..conversationPageResults.add(
        const ConversationPageResult(
          items: [
            ConversationSummaryModel(
              groupKey: 'session:summary-group-avatar-cached',
              conversationType: 'group',
              latestSessionId: 'summary-group-avatar-cached',
              title: 'Summary Group',
              sessionType: 2,
              latestActiveAt: 1700000000000,
            ),
          ],
        ),
      );
    Get.put<SessionService>(sessionService);

    final controller = Get.put(ConversationsController());
    await controller.refreshSessionsOnPageVisible();

    final item = controller.groupedSessions.single;
    expect(controller.getConversationAvatarMembers(item), cachedMembers);
  });

  test('empty group avatar refresh keeps cached members', () async {
    const cachedMembers = [
      SessionAvatarMember(
        memberId: '1001',
        memberType: 1,
        displayName: '缓存A',
        avatarUrl: 'https://example.com/a.png',
      ),
    ];
    final sessionService = _FakeSessionService();
    sessionService.detailResult = const SessionDetailResult(
      data: {'session_type': 2, 'members': []},
    );
    Get.put<SessionService>(sessionService);
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'group-avatar-empty-refresh',
        title: 'Avatar Group',
        type: 'group',
        updatedAt: 1,
        lastMessageTime: 1,
        cachedGroupAvatarMembers: cachedMembers,
      ),
    ]);

    final controller = Get.put(ConversationsController());
    final item = controller.groupedSessions.single;

    expect(controller.getConversationAvatarMembers(item), cachedMembers);
    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    expect(controller.getConversationAvatarMembers(item), cachedMembers);
  });

  test('loaded group avatar members are persisted for cold start', () async {
    final userId = 'avatar_cache_${DateTime.now().millisecondsSinceEpoch}';
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser(userId);
    await LocalDb.clearActiveUserData();

    final sessionService = _FakeSessionService();
    sessionService.detailResult = const SessionDetailResult(
      data: {
        'session_type': 2,
        'members': [
          {'member_id': '1001', 'member_type': 1, 'nickname': '成员A'},
          {'member_id': '1002', 'member_type': 1, 'nickname': '成员B'},
        ],
      },
    );
    Get.put<SessionService>(sessionService);

    final session = SessionModel(
      sessionId: 'group-avatar-persist',
      title: 'Avatar Group',
      type: 'group',
      updatedAt: 1,
      unreadCount: 2,
      lastMessage: 'keep preview',
      lastMessageTime: 1,
    );
    await LocalDb.upsertSession(session.toJson());
    imService.sessions.assignAll([session]);

    final controller = Get.put(ConversationsController());
    final item = controller.groupedSessions.single;

    expect(controller.getConversationAvatarMembers(item), isEmpty);
    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    final members = controller.getConversationAvatarMembers(item);
    expect(members.map((member) => member.displayName), ['成员A', '成员B']);

    final row = await LocalDb.getSessionRecord('group-avatar-persist');
    final restored = SessionModel.fromJson(row!);
    expect(restored.cachedGroupAvatarMembers, members);
    expect(restored.title, 'Avatar Group');
    expect(restored.lastMessage, 'keep preview');
    expect(restored.unreadCount, 2);
  });

  test(
    'visible group avatar request loads session detail immediately',
    () async {
      final sessionService = _FakeSessionService();
      sessionService.detailResult = const SessionDetailResult(
        data: {
          'session_type': 2,
          'members': [
            {'member_id': '1001', 'member_type': 1, 'nickname': 'A'},
          ],
        },
      );
      Get.put<SessionService>(sessionService);

      final session = SessionModel(
        sessionId: 'group-avatar-visible',
        title: 'Avatar Group',
        type: 'group',
        updatedAt: 1,
        lastMessageTime: 1,
      );
      final item = ConversationListItem(
        groupKey: 'session:${session.sessionId}',
        latestSession: session,
        sessions: [session],
        unreadCount: 0,
        isPinned: false,
        pinnedAt: 0,
      );

      final controller = Get.put(ConversationsController());

      expect(sessionService.fetchCalls, 0);
      expect(controller.getConversationAvatarMembers(item), isEmpty);
      expect(sessionService.fetchCalls, 1);

      await Future<void>.delayed(Duration.zero);
      await Future<void>.delayed(Duration.zero);

      expect(controller.getConversationAvatarMembers(item), hasLength(1));
      expect(sessionService.fetchCalls, 1);
    },
  );

  test('private session detail 404 prunes stale local session', () async {
    final sessionService = _FakeSessionService();
    sessionService.detailResult = const SessionDetailResult(
      code: 4004,
      httpStatus: 404,
      message: 'session not found',
    );
    Get.put<SessionService>(sessionService);

    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'stale-private-session',
        title: 'Ghost Thread',
        type: 'private',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);

    Get.put(ConversationsController());

    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    expect(imService.deletedSessionIds, contains('stale-private-session'));
    expect(imService.sessions, isEmpty);
  });

  test('group avatar detail 404 prunes stale local session', () async {
    final sessionService = _FakeSessionService();
    sessionService.detailResult = const SessionDetailResult(
      code: 4004,
      httpStatus: 404,
      message: 'session not found',
    );
    Get.put<SessionService>(sessionService);

    final session = SessionModel(
      sessionId: 'stale-group-session',
      title: 'Ghost Group',
      type: 'group',
      updatedAt: 1,
      lastMessageTime: 1,
    );
    imService.sessions.assignAll([session]);

    final controller = Get.put(ConversationsController());
    final item = ConversationListItem(
      groupKey: 'session:${session.sessionId}',
      latestSession: session,
      sessions: [session],
      unreadCount: 0,
      isPinned: false,
      pinnedAt: 0,
    );

    expect(controller.getConversationAvatarMembers(item), isEmpty);

    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    expect(imService.deletedSessionIds, contains('stale-group-session'));
    expect(imService.sessions, isEmpty);
  });

  test(
    'setSessionGroupMuted uses peer mute for private conversations',
    () async {
      final now = DateTime.now().millisecondsSinceEpoch;
      final friendService = _FakeFriendService();
      Get.put<FriendService>(friendService);
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'mute-ok-1',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now - 1,
          unreadCount: 1,
          lastMessage: 'a',
          lastMessageTime: now - 1,
        ),
        SessionModel(
          sessionId: 'mute-ok-2',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now,
          unreadCount: 2,
          lastMessage: 'b',
          lastMessageTime: now,
        ),
      ]);
      final controller = Get.put(ConversationsController());
      final group = controller.groupedSessions.single;

      final ok = await controller.setSessionGroupMuted(group, isMuted: true);
      expect(ok, isTrue);
      expect(imService.refreshSessionsNowCalls, 0);
      expect(friendService.mutedFriendUserIds, ['1001']);
      expect(imService.mutedSessionIds, isEmpty);
      expect(imService.mutedPeerIds, ['1001']);
      expect(imService.sessions.every((s) => s.friendIsMuted), isTrue);
      expect(controller.groupedSessions.single.isMuted, isTrue);
      expect(controller.groupedSessions.single.badgeUnreadCount, 0);

      imService.sessions.add(
        SessionModel(
          sessionId: 'mute-ok-3',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now + 1,
          unreadCount: 4,
          lastMessage: 'new thread',
          lastMessageTime: now + 1,
        ),
      );
      await Future<void>.delayed(const Duration(milliseconds: 150));
      expect(imService.isPeerMuted('1001'), isTrue);
      expect(imService.notificationUnread, 0);
      expect(controller.groupedSessions.single.badgeUnreadCount, 0);
    },
  );

  test(
    'setSessionGroupMuted keeps session mute for group conversations',
    () async {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'mute-group-1',
          title: 'Team',
          type: 'group',
          updatedAt: now,
          unreadCount: 2,
          lastMessage: 'hi',
          lastMessageTime: now,
        ),
      ]);
      final controller = Get.put(ConversationsController());
      final group = controller.groupedSessions.single;

      final ok = await controller.setSessionGroupMuted(group, isMuted: true);
      expect(ok, isTrue);
      expect(imService.mutedSessionIds, ['mute-group-1']);
    },
  );

  test(
    'setSessionGroupMuted uses peer mute when conversation summary only has latest',
    () async {
      const groupKey = 'private:1:4001';
      final friendService = _FakeFriendService();
      Get.put<FriendService>(friendService);
      final sessionService = _FakeSessionService()
        ..initialized = true
        ..conversationPageResults.add(
          const ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: groupKey,
                conversationType: 'private',
                latestSessionId: 'mute-latest',
                title: 'Dana',
                peerId: '4001',
                peerType: 1,
                lastMsg: 'latest',
                unread: 5,
                badgeUnread: 5,
                latestActiveAt: 1700000000000,
                threadCount: 2,
                hasMoreThreads: true,
              ),
            ],
          ),
        );
      Get.put<SessionService>(sessionService);

      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();

      final group = controller.groupedSessions.single;
      expect(group.sessions.map((s) => s.sessionId), ['mute-latest']);
      expect(group.threadCount, 2);
      expect(group.badgeUnreadCount, 5);

      final ok = await controller.setSessionGroupMuted(group, isMuted: true);
      expect(ok, isTrue);
      expect(sessionService.conversationThreadCalls, 0);
      expect(friendService.mutedFriendUserIds, ['4001']);
      expect(imService.mutedSessionIds, isEmpty);
      expect(imService.mutedPeerIds, ['4001']);
      expect(controller.groupedSessions.single.isMuted, isTrue);
      expect(controller.groupedSessions.single.badgeUnreadCount, 0);
      expect(controller.groupedSessions.single.hasMutedUnread, isTrue);
    },
  );

  test(
    'page-visible refresh uses stale-aware refresh when sessions already exist',
    () async {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'existing-session',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();

      expect(imService.refreshSessionsIfStaleCalls, 1);
      expect(imService.refreshSessionsNowCalls, 0);
    },
  );

  test(
    'page-visible refresh forces full refresh when session list is empty',
    () async {
      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();

      expect(imService.refreshSessionsNowCalls, 1);
      expect(imService.refreshSessionsIfStaleCalls, 0);
    },
  );

  test(
    'conversation summary api drives homepage without old session pagination',
    () async {
      final sessionService = _FakeSessionService()
        ..initialized = true
        ..conversationPageResults.addAll([
          const ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: 'private:1:2001',
                conversationType: 'private',
                latestSessionId: 'summary-s1',
                title: 'Alice',
                peerId: '2001',
                peerType: 1,
                peerNickname: 'Alice',
                lastMsg: 'hello',
                unread: 5,
                badgeUnread: 5,
                latestActiveAt: 1700000000000,
                threadCount: 3,
                hasMoreThreads: true,
              ),
            ],
            hasMore: true,
            nextCursor: '1',
          ),
          const ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: 'session:g1',
                conversationType: 'group',
                latestSessionId: 'g1',
                title: 'Team',
                sessionType: 2,
                lastMsg: 'group hello',
                latestActiveAt: 1699999999000,
                threadCount: 1,
              ),
            ],
          ),
        ]);
      Get.put<SessionService>(sessionService);

      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();

      expect(sessionService.conversationPageCalls, 1);
      expect(imService.refreshSessionsNowCalls, 0);
      expect(imService.loadMoreSessionWindowCalls, 0);
      expect(controller.groupedSessions, hasLength(1));
      expect(controller.groupedSessions.first.groupKey, 'private:1:2001');
      expect(controller.groupedSessions.first.threadCount, 3);
      expect(
        controller.groupedSessions.first.latestSession.sessionId,
        'summary-s1',
      );

      await Future<void>.delayed(const Duration(milliseconds: 1100));
      await controller.loadMoreSessionsForVisibleListIfNeeded();

      expect(sessionService.conversationPageCalls, 2);
      expect(imService.loadMoreSessionWindowCalls, 0);
      expect(controller.groupedSessions.map((item) => item.groupKey), [
        'private:1:2001',
        'session:g1',
      ]);
    },
  );

  test(
    'conversation summary path clears mention highlight after session is read',
    () async {
      const currentUserId = '9001';
      final now = DateTime.now().millisecondsSinceEpoch;
      await LocalDb.setActiveUser(currentUserId);
      await LocalDb.clearActiveUserData();
      Get.put<AuthService>(_FakeAuthService(currentUserId));

      await LocalDb.upsertMessage({
        'msg_id': 'msg-mention-api-1',
        'session_id': 's-mention-api',
        'sender_id': '2002',
        'sender_type': 1,
        'msg_type': 1,
        'content': '@me please check',
        'extra': jsonEncode({
          'mention_user_ids': [currentUserId],
        }),
        'inbox_seq': 1,
        'created_at': now - 1000,
      });

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's-mention-api',
          title: 'Alice',
          type: 'private',
          peerId: '2002',
          peerType: 1,
          updatedAt: now,
          unreadCount: 1,
          lastMessage: '@me please check',
          lastMessageTime: now - 1000,
        ),
      ]);

      final sessionService = _FakeSessionService()
        ..initialized = true
        ..conversationPageResults.add(
          ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: 'private:1:2002',
                conversationType: 'private',
                latestSessionId: 's-mention-api',
                title: 'Alice',
                peerId: '2002',
                peerType: 1,
                lastMsg: '@me please check',
                unread: 1,
                badgeUnread: 1,
                latestActiveAt: now,
                threadCount: 1,
              ),
            ],
          ),
        );
      Get.put<SessionService>(sessionService);

      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();
      await Future<void>.delayed(const Duration(milliseconds: 250));

      var item = controller.groupedSessions.firstWhere(
        (it) => it.groupKey == 'private:1:2002',
      );
      expect(item.hasUnreadMention, isTrue);

      // 模拟用户查看完会话：本地未读清零并触发 sessions 变更。
      // 摘要 API 路径此前不会再执行 mention 同步，高亮会永久残留。
      imService.sessions[0] = imService.sessions.first.copyWith(unreadCount: 0);
      await Future<void>.delayed(const Duration(milliseconds: 250));

      item = controller.groupedSessions.firstWhere(
        (it) => it.groupKey == 'private:1:2002',
      );
      expect(item.hasUnreadMention, isFalse);
    },
  );

  test(
    'conversation summary keeps server badge when local thread coverage incomplete',
    () async {
      final sessionService = _FakeSessionService()
        ..initialized = true
        ..conversationPageResults.add(
          const ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: 'private:1:2001',
                conversationType: 'private',
                latestSessionId: 'thread-new',
                title: 'Alice',
                peerId: '2001',
                peerType: 1,
                lastMsg: 'hello',
                unread: 3,
                badgeUnread: 3,
                latestActiveAt: 1700000000000,
                threadCount: 2,
                hasMoreThreads: true,
              ),
            ],
          ),
        );
      Get.put<SessionService>(sessionService);

      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        // 本地窗口里该对端只有这一条线程（未读 0）
        SessionModel(
          sessionId: 'thread-new',
          title: 'Alice',
          type: 'private',
          peerId: '2001',
          peerType: 1,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
        // 未读所在线程缺 peer 信息，按对端归组失配，但底部角标会计入它
        SessionModel(
          sessionId: 'thread-old',
          title: '',
          type: 'private',
          peerId: '',
          peerType: 0,
          updatedAt: now - 1000,
          unreadCount: 3,
          lastMessage: 'unread here',
          lastMessageTime: now - 1000,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();

      final item = controller.groupedSessions.firstWhere(
        (it) => it.groupKey == 'private:1:2001',
      );
      // 本地校准覆盖不全时必须保留服务端未读数，不能清成 0
      expect(item.unreadCount, 3);
      expect(item.badgeUnreadCount, 3);
      // 列表行角标与底部角标对齐
      final listBadgeTotal = controller.groupedSessions.fold<int>(
        0,
        (sum, it) => sum + it.badgeUnreadCount,
      );
      expect(listBadgeTotal, imService.notificationUnread);
    },
  );

  test(
    'conversation summary keeps server badge floor even when thread coverage complete',
    () async {
      // 复现"切到消息页角标闪一下"的根因：服务端快照带回真实未读数，但本地
      // 会话在刷新瞬间临时为 0（DB 尚未同步），且线程已被本地完整覆盖。此时
      // 不能用本地 0 把服务端未读数往下清，否则角标会先消失再恢复。
      final sessionService = _FakeSessionService()
        ..initialized = true
        ..conversationPageResults.add(
          const ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: 'private:1:2001',
                conversationType: 'private',
                latestSessionId: 'thread-new',
                title: 'Alice',
                peerId: '2001',
                peerType: 1,
                lastMsg: 'hello',
                unread: 3,
                badgeUnread: 3,
                latestActiveAt: 1700000000000,
                threadCount: 1,
              ),
            ],
          ),
        );
      Get.put<SessionService>(sessionService);

      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        // 本地完整覆盖该组（threadCount=1），但未读临时为 0
        SessionModel(
          sessionId: 'thread-new',
          title: 'Alice',
          type: 'private',
          peerId: '2001',
          peerType: 1,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
      ]);

      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();

      final item = controller.groupedSessions.firstWhere(
        (it) => it.groupKey == 'private:1:2001',
      );
      // 服务端未读数是下界：本地瞬时 0 不能把角标清掉（防闪烁）；未读数与角标
      // 同时保留服务端值，不出现 unreadCount < badgeUnreadCount 的矛盾态。
      // 用户主动已读走 override 分支立即清零，不受此处影响。
      expect(item.unreadCount, 3);
      expect(item.badgeUnreadCount, 3);
    },
  );

  test(
    'conversation summary unpin reorders list immediately in api mode',
    () async {
      final friendService = _FakeFriendService();
      Get.put<FriendService>(friendService);
      final sessionService = _FakeSessionService()
        ..initialized = true
        ..conversationPageResults.add(
          const ConversationPageResult(
            items: [
              // 置顶但活动时间较旧，初始排在前
              ConversationSummaryModel(
                groupKey: 'private:1:2001',
                conversationType: 'private',
                latestSessionId: 'summary-alice',
                title: 'Alice',
                peerId: '2001',
                peerType: 1,
                lastMsg: 'older pinned',
                latestActiveAt: 1699999999000,
                isPinned: true,
                pinnedAt: 1700000500000,
                threadCount: 1,
              ),
              // 未置顶但活动更新
              ConversationSummaryModel(
                groupKey: 'private:1:2002',
                conversationType: 'private',
                latestSessionId: 'summary-bob',
                title: 'Bob',
                peerId: '2002',
                peerType: 1,
                lastMsg: 'newer',
                latestActiveAt: 1700000000000,
                threadCount: 1,
              ),
            ],
          ),
        );
      Get.put<SessionService>(sessionService);

      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();

      // 初始顺序：置顶的 Alice 在前
      expect(controller.groupedSessions.map((it) => it.groupKey), [
        'private:1:2001',
        'private:1:2002',
      ]);
      expect(controller.groupedSessions.first.isPinned, isTrue);

      // 取消置顶 Alice
      final alice = controller.groupedSessions.first;
      await controller.setSessionGroupPinned(alice, isPinned: false);
      await Future<void>.delayed(Duration.zero);

      // 已调用好友级取消置顶
      expect(friendService.pinnedFriendUserIds, ['2001']);
      // 即时重排：活动更新的 Bob 升到首位，Alice 取消置顶后下移
      expect(controller.groupedSessions.map((it) => it.groupKey), [
        'private:1:2002',
        'private:1:2001',
      ]);
      expect(
        controller.groupedSessions
            .firstWhere((it) => it.groupKey == 'private:1:2001')
            .isPinned,
        isFalse,
      );
    },
  );

  test(
    'conversation summary api refreshes first page once for realtime bursts',
    () async {
      final sessionService = _FakeSessionService()
        ..initialized = true
        ..conversationPageResults.addAll([
          const ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: 'private:1:2002',
                conversationType: 'private',
                latestSessionId: 'summary-bob',
                title: 'Bob',
                peerId: '2002',
                peerType: 1,
                lastMsg: 'older',
                latestActiveAt: 1699999999000,
                threadCount: 1,
              ),
            ],
          ),
          const ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: 'private:1:2001',
                conversationType: 'private',
                latestSessionId: 'summary-alice',
                title: 'Alice',
                peerId: '2001',
                peerType: 1,
                lastMsg: 'new realtime',
                latestActiveAt: 1700000000000,
                threadCount: 2,
              ),
              ConversationSummaryModel(
                groupKey: 'private:1:2002',
                conversationType: 'private',
                latestSessionId: 'summary-bob',
                title: 'Bob',
                peerId: '2002',
                peerType: 1,
                lastMsg: 'older',
                latestActiveAt: 1699999999000,
                threadCount: 1,
              ),
            ],
          ),
        ]);
      Get.put<SessionService>(sessionService);

      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();
      expect(controller.groupedSessions.first.groupKey, 'private:1:2002');

      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.add(
        SessionModel(
          sessionId: 'local-alice-1',
          title: 'Alice',
          type: 'private',
          peerId: '2001',
          peerType: 1,
          updatedAt: now,
          unreadCount: 1,
          lastMessage: 'new realtime',
          lastMessageTime: now,
        ),
      );
      imService.sessions.add(
        SessionModel(
          sessionId: 'local-alice-2',
          title: 'Alice',
          type: 'private',
          peerId: '2001',
          peerType: 1,
          updatedAt: now + 1,
          unreadCount: 1,
          lastMessage: 'new realtime again',
          lastMessageTime: now + 1,
        ),
      );

      await Future<void>.delayed(const Duration(milliseconds: 2500));

      expect(sessionService.conversationPageCalls, 1);

      await Future<void>.delayed(const Duration(milliseconds: 3500));

      expect(sessionService.conversationPageCalls, 2);
      expect(imService.loadMoreSessionWindowCalls, 0);
      expect(controller.groupedSessions.map((item) => item.groupKey), [
        'private:1:2001',
        'private:1:2002',
      ]);
    },
  );

  test(
    'conversation summary first-page refresh preserves loaded tail',
    () async {
      final sessionService = _FakeSessionService()
        ..initialized = true
        ..conversationPageResults.addAll([
          const ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: 'private:1:2001',
                conversationType: 'private',
                latestSessionId: 'summary-alice-old',
                title: 'Alice',
                peerId: '2001',
                peerType: 1,
                lastMsg: 'old',
                latestActiveAt: 1700000000000,
                threadCount: 1,
              ),
            ],
            hasMore: true,
            nextCursor: 'page-2',
          ),
          const ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: 'session:g1',
                conversationType: 'group',
                latestSessionId: 'g1',
                title: 'Team',
                sessionType: 2,
                lastMsg: 'group hello',
                latestActiveAt: 1699999999000,
                threadCount: 1,
              ),
            ],
            hasMore: true,
            nextCursor: 'page-3',
          ),
          const ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: 'private:1:2001',
                conversationType: 'private',
                latestSessionId: 'summary-alice-new',
                title: 'Alice',
                peerId: '2001',
                peerType: 1,
                lastMsg: 'new',
                latestActiveAt: 1700000001000,
                threadCount: 1,
              ),
            ],
            hasMore: true,
            nextCursor: 'page-2',
          ),
        ]);
      Get.put<SessionService>(sessionService);

      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();
      await Future<void>.delayed(const Duration(milliseconds: 1100));
      await controller.loadMoreSessionsForVisibleListIfNeeded();

      expect(controller.groupedSessions.map((item) => item.groupKey), [
        'private:1:2001',
        'session:g1',
      ]);

      await Future<void>.delayed(const Duration(milliseconds: 1100));
      await controller.refreshSessionsOnPageVisible();

      expect(sessionService.conversationPageCalls, 3);
      expect(controller.groupedSessions.map((item) => item.groupKey), [
        'private:1:2001',
        'session:g1',
      ]);
      expect(
        controller.groupedSessions.first.latestSession.sessionId,
        'summary-alice-new',
      );
    },
  );

  test(
    'thread popup pulls folded sessions from local imService and skips backend',
    () async {
      // 修复后弹窗线程列表口径与资料页一致：从 imService.sessions 按
      // groupKey 过滤并按会话级 isPinned 排序，不再走 sessionService。
      final sessionService = _FakeSessionService()..initialized = true;
      Get.put<SessionService>(sessionService);

      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'thread-new',
          title: 'New topic',
          type: 'private',
          peerId: '3001',
          peerType: 1,
          updatedAt: 1700000000000,
          lastMessage: 'new',
          lastMessageTime: 1700000000000,
        ),
        SessionModel(
          sessionId: 'thread-old',
          title: 'Old topic',
          type: 'private',
          peerId: '3001',
          peerType: 1,
          updatedAt: 1699999999000,
          lastMessage: 'old',
          lastMessageTime: 1699999999000,
        ),
      ]);

      sessionService.conversationPageResults.add(
        const ConversationPageResult(
          items: [
            ConversationSummaryModel(
              groupKey: 'private:1:3001',
              conversationType: 'private',
              latestSessionId: 'thread-new',
              peerId: '3001',
              peerType: 1,
              threadCount: 2,
            ),
          ],
        ),
      );
      await controller.refreshSessionsOnPageVisible();
      final item = controller.groupedSessions.first;
      final threads = await controller.fetchConversationThreadSessions(item);

      expect(
        sessionService.conversationThreadCalls,
        0,
        reason: '修复后不再调用后端 conversation_threads API',
      );
      expect(threads.sessions.map((session) => session.sessionId), [
        'thread-new',
        'thread-old',
      ]);
    },
  );

  test(
    'local session changes do not alter list when conversation API is active',
    () async {
      final sessionService = _FakeSessionService()
        ..initialized = true
        ..conversationPageResults.addAll([
          const ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: 'private:1:3001',
                conversationType: 'private',
                latestSessionId: 's-bob',
                title: 'Bob',
                peerId: '3001',
                peerType: 1,
                lastMsg: 'bob older',
                latestActiveAt: 1690000001000,
                threadCount: 1,
              ),
              ConversationSummaryModel(
                groupKey: 'private:1:2001',
                conversationType: 'private',
                latestSessionId: 's-alice',
                title: 'Alice',
                peerId: '2001',
                peerType: 1,
                lastMsg: 'alice older',
                latestActiveAt: 1690000000000,
                threadCount: 1,
              ),
            ],
          ),
        ]);
      Get.put<SessionService>(sessionService);

      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();

      // Backend order: Bob first, Alice second.
      expect(controller.groupedSessions[0].groupKey, 'private:1:3001');
      expect(controller.groupedSessions[1].groupKey, 'private:1:2001');

      // Local session changes trigger optimistic activity reorder so the
      // list reflects updated activity immediately (with animation), while
      // keeping non-activity fields unchanged until the API refreshes.
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.add(
        SessionModel(
          sessionId: 's-alice-new',
          title: 'Alice',
          type: 'private',
          peerId: '2001',
          peerType: 1,
          updatedAt: now,
          unreadCount: 1,
          lastMessage: 'alice newest',
          lastMessageTime: now,
        ),
      );

      await Future<void>.delayed(const Duration(milliseconds: 50));

      // Alice now has the most recent activity and should sort first.
      expect(controller.groupedSessions[0].groupKey, 'private:1:2001');
      expect(controller.groupedSessions[1].groupKey, 'private:1:3001');
      // The latestSession identity is preserved (not replaced with local data).
      expect(controller.groupedSessions[0].latestSession.sessionId, 's-alice');
    },
  );

  test(
    'api summary synthesizes a row for a local unread session missing from the page',
    () async {
      final now = DateTime.now().millisecondsSinceEpoch;
      // 两个本地未读私聊：Alice 会进入服务端会话页快照，Carol 不在快照里
      // （模拟新会话/快照窗口外/刷新被节流）。底部角标按本地会话求和应为 5。
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's-alice',
          title: 'Alice',
          type: 'private',
          peerId: '2001',
          peerType: 1,
          updatedAt: now - 2000,
          unreadCount: 3,
          lastMessage: 'alice',
          lastMessageTime: now - 2000,
        ),
        SessionModel(
          sessionId: 's-carol',
          title: 'Carol',
          type: 'private',
          peerId: '3003',
          peerType: 1,
          updatedAt: now - 1000,
          unreadCount: 2,
          lastMessage: 'carol',
          lastMessageTime: now - 1000,
        ),
        // 仅静音未读、且不在快照里：不贡献底部角标，不应被补行。
        SessionModel(
          sessionId: 's-dave-muted',
          title: 'Dave',
          type: 'private',
          peerId: '4004',
          peerType: 1,
          updatedAt: now - 3000,
          unreadCount: 9,
          isMuted: true,
          lastMessage: 'dave',
          lastMessageTime: now - 3000,
        ),
      ]);

      final sessionService = _FakeSessionService()
        ..initialized = true
        ..conversationPageResults.add(
          const ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: 'private:1:2001',
                conversationType: 'private',
                latestSessionId: 's-alice',
                title: 'Alice',
                peerId: '2001',
                peerType: 1,
                peerNickname: 'Alice',
                lastMsg: 'alice',
                unread: 3,
                badgeUnread: 3,
                latestActiveAt: 1700000000000,
                threadCount: 1,
              ),
            ],
          ),
        );
      Get.put<SessionService>(sessionService);

      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();

      final keys = controller.groupedSessions.map((i) => i.groupKey).toList();
      // Carol 被本地补行，Alice 来自快照；静音的 Dave 不补行。
      expect(keys, containsAll(<String>['private:1:2001', 'private:1:3003']));
      expect(keys, isNot(contains('private:1:4004')));

      final carol = controller.groupedSessions.firstWhere(
        (i) => i.groupKey == 'private:1:3003',
      );
      expect(carol.badgeUnreadCount, 2);

      // 关键不变量：列表角标总和 == 底部 notificationUnread，不再有"底部有数列表没有"。
      final groupedBadgeTotal = controller.groupedSessions.fold<int>(
        0,
        (sum, item) => sum + item.badgeUnreadCount,
      );
      expect(groupedBadgeTotal, imService.notificationUnread);
      expect(groupedBadgeTotal, 5);
    },
  );

  test(
    'api summary synthesizes the visitor group row for local unread visitor sessions',
    () async {
      final now = DateTime.now().millisecondsSinceEpoch;
      // 本地两个访客会话有未读，但都不在服务端会话页快照里：
      // 应合成一条 visitor:group 补行，角标为两者之和。
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's-visitor-1',
          title: '',
          type: 'private',
          isVisitor: true,
          updatedAt: now - 1000,
          unreadCount: 2,
          lastMessage: 'hi',
          lastMessageTime: now - 1000,
        ),
        SessionModel(
          sessionId: 's-visitor-2',
          title: '',
          type: 'private',
          isVisitor: true,
          updatedAt: now - 2000,
          unreadCount: 1,
          lastMessage: 'hello',
          lastMessageTime: now - 2000,
        ),
      ]);

      final sessionService = _FakeSessionService()
        ..initialized = true
        ..conversationPageResults.add(
          const ConversationPageResult(
            items: [
              ConversationSummaryModel(
                groupKey: 'private:1:2001',
                conversationType: 'private',
                latestSessionId: 's-alice',
                title: 'Alice',
                peerId: '2001',
                peerType: 1,
                lastMsg: 'alice',
                latestActiveAt: 1700000000000,
                threadCount: 1,
              ),
            ],
          ),
        );
      Get.put<SessionService>(sessionService);

      final controller = Get.put(ConversationsController());
      await controller.refreshSessionsOnPageVisible();

      final keys = controller.groupedSessions.map((i) => i.groupKey).toList();
      expect(keys, contains(ConversationsController.visitorGroupKey));

      final visitorRow = controller.groupedSessions.firstWhere(
        (i) => i.groupKey == ConversationsController.visitorGroupKey,
      );
      expect(visitorRow.badgeUnreadCount, 3);

      final groupedBadgeTotal = controller.groupedSessions.fold<int>(
        0,
        (sum, item) => sum + item.badgeUnreadCount,
      );
      expect(groupedBadgeTotal, imService.notificationUnread);
    },
  );
}

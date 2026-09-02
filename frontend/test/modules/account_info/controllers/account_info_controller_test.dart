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
import 'package:grix/modules/account_info/controllers/account_info_controller.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';
import 'package:grix/modules/chat/message_cards/models/chat_user_profile_card_data.dart';

class _FakeImService extends ImService {
  final Set<String> locallyDeletedSessionIds = <String>{};
  String lastResolvedSessionId = '';
  String lastResolvedSessionType = '';
  int sendMessageCalls = 0;
  String? lastSentContent;
  String? lastSentSessionId;
  Map<String, dynamic>? lastSentExtra;

  @override
  bool get isConnected => true;

  @override
  String resolveSessionDisplayTitleById(
    String sessionId, {
    String fallbackTitle = '',
    String type = 'private',
  }) {
    lastResolvedSessionId = sessionId.trim();
    lastResolvedSessionType = type.trim();
    return fallbackTitle;
  }

  @override
  bool hasSessionDisplayTitleById(String sessionId) => true;

  @override
  bool isSessionLocallyDeleted(String sessionId) =>
      locallyDeletedSessionIds.contains(sessionId.trim());

  @override
  Future<void> bindSessionDisplayTitle(
    String sessionId,
    String title, {
    String type = 'private',
    String peerId = '',
    int peerType = 0,
  }) async {}

  @override
  Future<void> refreshSessionsWindowNow() async {}

  @override
  Future<void> sendMessage(
    String content,
    String sessionId, {
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    bool updateCurrentSessionUi = true,
  }) async {
    sendMessageCalls++;
    lastSentContent = content;
    lastSentSessionId = sessionId;
    lastSentExtra = extra == null ? null : Map<String, dynamic>.from(extra);
  }
}

class _FakeFriendService extends FriendService {
  int fetchProfileCalls = 0;
  int sendFriendRequestCalls = 0;
  int deleteFriendCalls = 0;
  String? lastRequestUserId;
  String? lastDeleteUserId;
  bool sendFriendRequestResult = true;
  bool sendFriendRequestAutoApproved = false;
  bool deleteFriendResult = true;
  final Map<String, Map<String, String>> _profileCache =
      <String, Map<String, String>>{};

  @override
  String? getUserNickname(String userId) {
    final friend = getFriendItem(userId);
    if (friend != null) {
      return friend.nickname.isNotEmpty ? friend.nickname : friend.username;
    }

    final cached = _profileCache[userId.trim()] ?? const <String, String>{};
    final nickname = cached['nickname']?.trim() ?? '';
    if (nickname.isNotEmpty) {
      return nickname;
    }
    final username = cached['username']?.trim() ?? '';
    if (username.isNotEmpty) {
      return username;
    }
    return null;
  }

  @override
  String? getUserUsername(String userId) {
    final friend = getFriendItem(userId);
    if (friend != null) {
      final username = friend.username.trim();
      if (username.isNotEmpty) {
        return username;
      }
    }
    final cached = _profileCache[userId.trim()] ?? const <String, String>{};
    final username = cached['username']?.trim() ?? '';
    return username.isNotEmpty ? username : null;
  }

  @override
  String? getUserAvatarUrl(String userId) {
    final friend = getFriendItem(userId);
    if (friend != null) {
      final avatarUrl = friend.avatarUrl.trim();
      if (avatarUrl.isNotEmpty) {
        return avatarUrl;
      }
    }
    final cached = _profileCache[userId.trim()] ?? const <String, String>{};
    final avatarUrl = cached['avatar_url']?.trim() ?? '';
    return avatarUrl.isNotEmpty ? avatarUrl : null;
  }

  @override
  String? getUserIntroduction(String userId) {
    final friend = getFriendItem(userId);
    if (friend != null) {
      final introduction = friend.introduction.trim();
      if (introduction.isNotEmpty) {
        return introduction;
      }
    }
    final cached = _profileCache[userId.trim()] ?? const <String, String>{};
    final introduction = cached['introduction']?.trim() ?? '';
    return introduction.isNotEmpty ? introduction : null;
  }

  @override
  Future<String?> fetchUserProfile(String userId) async {
    fetchProfileCalls++;
    final existing = getFriendItem(userId);
    if (existing != null) {
      return existing.nickname.isNotEmpty
          ? existing.nickname
          : existing.username;
    }

    final nickname = 'Nick$userId';
    final username = 'user_$userId';
    _profileCache[userId.trim()] = <String, String>{
      'nickname': nickname,
      'username': username,
      'introduction': 'Intro$userId',
      'avatar_url': 'https://example.com/$userId.png',
    };
    return nickname;
  }

  @override
  Future<FriendRequestSendResult> sendFriendRequest({
    String? toUserId,
    String? toUsername,
    String message = '',
  }) async {
    sendFriendRequestCalls++;
    lastRequestUserId = toUserId?.trim();
    if (!sendFriendRequestResult) {
      return const FriendRequestSendResult.failed();
    }
    if (sendFriendRequestAutoApproved) {
      final uid = (toUserId ?? '').trim();
      if (uid.isNotEmpty && !friendList.any((f) => f.userId == uid)) {
        friendList.insert(
          0,
          FriendItem(
            id: 'f-$uid',
            userId: uid,
            username: 'user_$uid',
            nickname: 'User $uid',
            remarkName: '',
            introduction: 'Intro$uid',
            avatarUrl: '',
          ),
        );
      }
    }
    return FriendRequestSendResult(
      success: true,
      autoApproved: sendFriendRequestAutoApproved,
    );
  }

  @override
  Future<bool> deleteFriend(String friendUserId) async {
    deleteFriendCalls++;
    lastDeleteUserId = friendUserId.trim();
    if (!deleteFriendResult) {
      return false;
    }
    friendList.removeWhere((f) => f.userId == friendUserId);
    friendRequests.removeWhere((r) => r.fromUserId == friendUserId);
    return true;
  }
}

class _FakeAgentService extends AgentService {
  int loadAgentsCalls = 0;

  @override
  Future<void> loadAgents({String? categoryId}) async {
    loadAgentsCalls++;
  }
}

class _FakeAuthService extends AuthService {
  _FakeAuthService({required String userId, String username = 'owner'})
    : _fakeUser = User(id: userId, username: username, nickname: username);

  final User _fakeUser;

  @override
  User? get user => _fakeUser;

  @override
  String? get userId => _fakeUser.id;
}

class _FakeSessionService extends SessionService {
  String? openLatestResult;
  String? createResult;
  int openLatestCalls = 0;
  int createCalls = 0;
  int fetchDetailCalls = 0;
  String lastOpenLatestPeerId = '';
  int lastOpenLatestPeerType = 0;
  String lastCreatePeerId = '';
  int lastCreatePeerType = 0;
  final Map<String, SessionDetailResult> detailsBySessionId =
      <String, SessionDetailResult>{};

  @override
  Future<String?> openLatestSession(String peerId, int peerType) async {
    openLatestCalls++;
    lastOpenLatestPeerId = peerId.trim();
    lastOpenLatestPeerType = peerType;
    return openLatestResult;
  }

  @override
  Future<String?> createSession(String peerId, int peerType) async {
    createCalls++;
    lastCreatePeerId = peerId.trim();
    lastCreatePeerType = peerType;
    return createResult;
  }

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    fetchDetailCalls++;
    final sid = sessionId.trim();
    return detailsBySessionId[sid] ??
        const SessionDetailResult(code: 50001, message: 'missing detail');
  }

  final List<ConversationThreadPageResult> threadPages =
      <ConversationThreadPageResult>[];
  final List<String> threadRequestCursors = <String>[];

  @override
  bool get isInitialized => true;

  @override
  Future<ConversationThreadPageResult> fetchConversationThreads({
    required String groupKey,
    int limit = 20,
    String cursor = '',
  }) async {
    threadRequestCursors.add(cursor);
    if (threadPages.isEmpty) {
      return ConversationThreadPageResult(groupKey: groupKey);
    }
    return threadPages.removeAt(0);
  }
}

void main() {
  setUp(() {
    Get.reset();
    Get.testMode = true;
  });

  tearDown(() {
    Get.reset();
  });

  test('filters and sorts conversation records by private group key', () {
    final imService = _FakeImService();
    final friendService = _FakeFriendService();

    final now = DateTime.now().millisecondsSinceEpoch;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 's-1',
        title: 'Thread A',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now - 2000,
        unreadCount: 1,
        lastMessage: 'older',
        lastMessageTime: now - 2000,
      ),
      SessionModel(
        sessionId: 's-2',
        title: 'Thread B',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now - 500,
        unreadCount: 0,
        lastMessage: 'newer',
        lastMessageTime: now - 500,
      ),
      SessionModel(
        sessionId: 's-3',
        title: 'Another Peer',
        type: 'private',
        peerId: '2002',
        peerType: 1,
        updatedAt: now,
        unreadCount: 0,
        lastMessage: 'other',
        lastMessageTime: now,
      ),
    ]);

    friendService.friendList.assignAll([
      FriendItem(
        id: 'f-1',
        userId: '1001',
        username: 'alice_01',
        nickname: 'Alice',
        remarkName: '',
        introduction: 'Alice intro',
        avatarUrl: 'https://example.com/a.png',
      ),
    ]);

    final controller = AccountInfoController(
      initialArguments: {
        'group_key': 'private:1:1001',
        'session_id': 's-1',
        'peer_id': '1001',
        'peer_type': '1',
      },
      imService: imService,
      friendService: friendService,
    );
    controller.onInit();

    final records = controller.conversationSessions;
    expect(records.map((s) => s.sessionId).toList(), ['s-2', 's-1']);
    expect(controller.displayNickname, 'Alice');
    expect(controller.displayAccount, '@alice_01');
    expect(controller.displayUserId, '1001');
    expect(controller.canStartChat, isTrue);
    expect(controller.canAddFriend, isFalse);
    expect(friendService.fetchProfileCalls, 0);

    controller.onClose();
  });

  test(
    'owner agent can start chat and read grouped conversation history',
    () async {
      final imService = _FakeImService();
      final agentService = _FakeAgentService();
      final authService = _FakeAuthService(userId: 'owner-1');
      final now = DateTime.now().millisecondsSinceEpoch;

      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-9',
          agentName: 'Planner',
          introduction: 'Agent planner intro',
          ownerID: 'owner-1',
          avatarUrl: 'https://example.com/agent.png',
        ),
      ]);

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'agent-old',
          title: 'Old Thread',
          type: 'private',
          peerId: 'agent-9',
          peerType: 2,
          updatedAt: now - 2000,
          unreadCount: 0,
          lastMessage: 'older',
          lastMessageTime: now - 2000,
        ),
        SessionModel(
          sessionId: 'agent-new',
          title: 'Latest Thread',
          type: 'private',
          peerId: 'agent-9',
          peerType: 2,
          updatedAt: now - 300,
          unreadCount: 0,
          lastMessage: 'newer',
          lastMessageTime: now - 300,
        ),
        SessionModel(
          sessionId: 'other-agent',
          title: 'Other',
          type: 'private',
          peerId: 'agent-10',
          peerType: 2,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'other',
          lastMessageTime: now,
        ),
      ]);

      final controller = AccountInfoController(
        initialArguments: {
          'group_key': 'private:2:agent-9',
          'session_id': 'agent-old',
          'peer_id': 'agent-9',
          'peer_type': '2',
        },
        imService: imService,
        agentService: agentService,
        authService: authService,
      );
      controller.onInit();
      await Future<void>.delayed(Duration.zero);

      expect(controller.isOwnedAgent, isTrue);
      expect(controller.canStartChat, isTrue);
      expect(controller.canAddFriend, isFalse);
      expect(controller.displayNickname, 'Planner');
      expect(controller.displayIntroduction, 'Agent planner intro');
      expect(
        controller.conversationSessions
            .map((session) => session.sessionId)
            .toList(),
        ['agent-new', 'agent-old'],
      );

      controller.onClose();
    },
  );

  test(
    'resolves user peer identity from private session detail when route metadata is missing',
    () async {
      final imService = _FakeImService();
      final friendService = _FakeFriendService();
      final authService = _FakeAuthService(userId: '9009');
      final sessionService = _FakeSessionService();
      final now = DateTime.now().millisecondsSinceEpoch;

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'seed-user-1',
          title: 'Alice',
          type: 'private',
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
      ]);

      sessionService.detailsBySessionId['seed-user-1'] =
          const SessionDetailResult(
            data: <String, dynamic>{
              'session_type': 1,
              'members': <Map<String, dynamic>>[
                <String, dynamic>{
                  'member_id': '1001',
                  'member_type': 1,
                  'nickname': 'Alice',
                },
                <String, dynamic>{'member_id': '9009', 'member_type': 1},
              ],
            },
          );

      final controller = AccountInfoController(
        initialArguments: {
          'session_id': 'seed-user-1',
          'peer_type': '0',
          'title': 'Alice',
        },
        imService: imService,
        friendService: friendService,
        authService: authService,
        sessionService: sessionService,
      );
      controller.onInit();
      await Future<void>.delayed(Duration.zero);

      expect(sessionService.fetchDetailCalls, 1);
      expect(friendService.fetchProfileCalls, 1);
      expect(controller.peerId.value, '1001');
      expect(controller.peerTypeHint, 1);
      expect(controller.displayNickname, 'Nick1001');
      expect(controller.displayAccount, '@user_1001');
      expect(controller.displayUserId, '1001');
      expect(controller.displayIntroduction, 'Intro1001');

      controller.onClose();
    },
  );

  test(
    'resolves agent peer identity from private session detail when route metadata is missing',
    () async {
      final imService = _FakeImService();
      final agentService = _FakeAgentService();
      final authService = _FakeAuthService(userId: 'owner-1');
      final sessionService = _FakeSessionService();
      final now = DateTime.now().millisecondsSinceEpoch;

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'seed-agent-1',
          title: 'openclaw-debug',
          type: 'private',
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
      ]);
      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-9',
          agentName: 'openclaw-debug',
          introduction: 'OpenClaw agent intro',
          ownerID: 'owner-1',
          avatarUrl: 'https://example.com/agent.png',
        ),
      ]);
      sessionService.detailsBySessionId['seed-agent-1'] =
          const SessionDetailResult(
            data: <String, dynamic>{
              'session_type': 1,
              'members': <Map<String, dynamic>>[
                <String, dynamic>{
                  'member_id': 'agent-9',
                  'member_type': 2,
                  'nickname': 'openclaw-debug',
                },
                <String, dynamic>{'member_id': 'owner-1', 'member_type': 1},
              ],
            },
          );

      final controller = AccountInfoController(
        initialArguments: {
          'session_id': 'seed-agent-1',
          'peer_type': '0',
          'title': 'openclaw-debug',
        },
        imService: imService,
        agentService: agentService,
        authService: authService,
        sessionService: sessionService,
      );
      controller.onInit();
      await Future<void>.delayed(Duration.zero);

      expect(sessionService.fetchDetailCalls, 1);
      expect(controller.peerId.value, 'agent-9');
      expect(controller.peerTypeHint, 2);
      expect(controller.displayNickname, 'openclaw-debug');
      expect(controller.displayUserId, 'agent-9');
      expect(controller.displayIntroduction, 'OpenClaw agent intro');
      expect(controller.avatarUrl.value, 'https://example.com/agent.png');

      controller.onClose();
    },
  );

  test('fetches profile when account fields are missing', () async {
    final imService = _FakeImService();
    final friendService = _FakeFriendService();

    final now = DateTime.now().millisecondsSinceEpoch;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'seed-1',
        title: '',
        type: 'private',
        peerId: '3003',
        peerType: 1,
        updatedAt: now,
        unreadCount: 0,
        lastMessage: 'hello',
        lastMessageTime: now,
      ),
    ]);

    final controller = AccountInfoController(
      initialArguments: {
        'session_id': 'seed-1',
        'peer_id': '3003',
        'peer_type': '1',
      },
      imService: imService,
      friendService: friendService,
    );
    controller.onInit();
    await Future<void>.delayed(Duration.zero);

    expect(friendService.fetchProfileCalls, 1);
    expect(controller.displayNickname, 'Nick3003');
    expect(controller.displayAccount, '@user_3003');
    expect(controller.displayIntroduction, 'Intro3003');

    controller.onClose();
  });

  test(
    'shows add-friend action and submits friend request for non-friend',
    () async {
      final imService = _FakeImService();
      final friendService = _FakeFriendService();
      final now = DateTime.now().millisecondsSinceEpoch;

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'sid-1',
          title: 'User 4004',
          type: 'private',
          peerId: '4004',
          peerType: 1,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: '',
          lastMessageTime: now,
        ),
      ]);

      final controller = AccountInfoController(
        initialArguments: {
          'peer_id': '4004',
          'peer_type': '1',
          'nickname': 'User 4004',
          'username': 'user_4004',
          'introduction': 'Intro4004',
          'avatar_url': 'https://example.com/4004.png',
        },
        imService: imService,
        friendService: friendService,
      );
      controller.onInit();

      expect(controller.canStartChat, isFalse);
      expect(controller.canAddFriend, isTrue);

      await controller.sendFriendRequest();
      expect(friendService.sendFriendRequestCalls, 1);
      expect(friendService.lastRequestUserId, '4004');
      expect(controller.friendRequestSent.value, isTrue);
      expect(controller.canAddFriend, isFalse);

      controller.onClose();
    },
  );

  test('auto-approved friend request opens private chat directly', () async {
    final imService = _FakeImService();
    final friendService = _FakeFriendService();
    final sessionService = _FakeSessionService();
    Get.put<ImService>(imService);
    Get.put<SessionService>(sessionService);
    final now = DateTime.now().millisecondsSinceEpoch;

    friendService.sendFriendRequestAutoApproved = true;
    sessionService.createResult = 'private-auto-sid';
    sessionService.detailsBySessionId['private-auto-sid'] =
        const SessionDetailResult(
          data: <String, dynamic>{
            'session_type': 1,
            'members': <Map<String, dynamic>>[
              <String, dynamic>{'member_id': '4004', 'member_type': 1},
              <String, dynamic>{'member_id': '9999', 'member_type': 1},
            ],
          },
        );

    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'sid-1',
        title: 'User 4004',
        type: 'private',
        peerId: '4004',
        peerType: 1,
        updatedAt: now,
        unreadCount: 0,
        lastMessage: '',
        lastMessageTime: now,
      ),
    ]);

    final controller = AccountInfoController(
      initialArguments: {
        'peer_id': '4004',
        'peer_type': '1',
        'nickname': 'User 4004',
        'username': 'user_4004',
        'introduction': 'Intro4004',
      },
      imService: imService,
      friendService: friendService,
      sessionService: sessionService,
    );
    controller.onInit();
    await Future<void>.delayed(Duration.zero);

    await controller.sendFriendRequest();

    expect(friendService.sendFriendRequestCalls, 1);
    expect(friendService.lastRequestUserId, '4004');
    expect(controller.friendRequestSent.value, isFalse);
    expect(controller.canStartChat, isTrue);
    expect(sessionService.openLatestCalls, 0);
    expect(sessionService.createCalls, 1);
    expect(imService.lastResolvedSessionId, 'private-auto-sid');
    expect(imService.lastResolvedSessionType, 'private');

    controller.onClose();
  });

  test('deletes friend and switches to add-friend state', () async {
    final imService = _FakeImService();
    final friendService = _FakeFriendService();
    final now = DateTime.now().millisecondsSinceEpoch;

    friendService.friendList.assignAll([
      FriendItem(
        id: 'f-1',
        userId: '7777',
        username: 'u_7777',
        nickname: 'User 7777',
        remarkName: '',
        avatarUrl: '',
      ),
    ]);

    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'sid-7777',
        title: 'User 7777',
        type: 'private',
        peerId: '7777',
        peerType: 1,
        updatedAt: now,
        unreadCount: 0,
        lastMessage: '',
        lastMessageTime: now,
      ),
    ]);

    final controller = AccountInfoController(
      initialArguments: {'peer_id': '7777', 'peer_type': '1'},
      imService: imService,
      friendService: friendService,
    );
    controller.onInit();
    await Future<void>.delayed(Duration.zero);

    expect(controller.canDeleteFriend, isTrue);
    expect(controller.canStartChat, isTrue);
    expect(controller.canAddFriend, isFalse);

    final success = await controller.deleteFriend();
    expect(success, isTrue);
    expect(friendService.deleteFriendCalls, 1);
    expect(friendService.lastDeleteUserId, '7777');
    expect(controller.canDeleteFriend, isFalse);
    expect(controller.canStartChat, isFalse);
    expect(controller.canAddFriend, isTrue);

    controller.onClose();
  });

  test(
    'does not fetch profile when nickname and username are already present',
    () async {
      final imService = _FakeImService();
      final friendService = _FakeFriendService();
      final now = DateTime.now().millisecondsSinceEpoch;

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'sid-2',
          title: 'User 5005',
          type: 'private',
          peerId: '5005',
          peerType: 1,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: '',
          lastMessageTime: now,
        ),
      ]);

      final controller = AccountInfoController(
        initialArguments: {
          'session_id': 'sid-2',
          'peer_id': '5005',
          'peer_type': '1',
          'nickname': 'User 5005',
          'username': 'user_5005',
          'introduction': 'Intro5005',
          'avatar_url': '',
        },
        imService: imService,
        friendService: friendService,
      );
      controller.onInit();
      await Future<void>.delayed(Duration.zero);

      expect(friendService.fetchProfileCalls, 0);
      expect(controller.isProfileLoading.value, isFalse);

      controller.onClose();
    },
  );

  test('forwardProfileCard sends user profile card message', () async {
    final imService = _FakeImService();
    final controller = AccountInfoController(
      initialArguments: {
        'peer_id': '5005',
        'peer_type': '1',
        'nickname': 'User 5005',
        'username': 'user_5005',
        'introduction': 'Intro5005',
        'avatar_url': 'https://example.com/5005.png',
      },
      imService: imService,
      friendService: _FakeFriendService(),
    );
    controller.onInit();

    final sentCount = await controller.forwardProfileCard(
      targetSessionId: 'session-target-1',
    );

    expect(sentCount, 1);
    expect(imService.sendMessageCalls, 1);
    expect(imService.lastSentSessionId, 'session-target-1');
    expect(imService.lastSentContent, contains('User 5005'));

    final content = imService.lastSentContent ?? '';
    final uriMatch = RegExp(r'\((grix://card/[^)]+)\)').firstMatch(content);
    expect(uriMatch, isNotNull);
    final card = ChatMessageCardCodec.decodeGrixUriCard(uriMatch!.group(1)!);
    expect(card, isNotNull);
    expect(card, isA<ChatUserProfileCardData>());
    expect((card as ChatUserProfileCardData).userId, '5005');
    expect(card.nickname, 'User 5005');
    expect(card.peerType, 1);

    controller.onClose();
  });

  test('forwardProfileCard sends agent profile card message', () async {
    final imService = _FakeImService();
    final agentService = _FakeAgentService();
    agentService.agents.assignAll([
      AgentModel(
        id: 'agent-9',
        agentName: 'Ops Agent',
        ownerID: '42',
        providerType: 3,
        avatarUrl: 'https://example.com/avatar/agent-9.png',
      ),
    ]);
    final controller = AccountInfoController(
      initialArguments: {
        'peer_id': 'agent-9',
        'peer_type': '2',
        'nickname': 'Ops Agent',
        'avatar_url': 'https://example.com/avatar/agent-9.png',
      },
      imService: imService,
      friendService: _FakeFriendService(),
      agentService: agentService,
      authService: _FakeAuthService(userId: '42'),
    );
    controller.onInit();

    final sentCount = await controller.forwardProfileCard(
      targetSessionId: 'session-target-agent-1',
    );

    expect(sentCount, 1);
    expect(imService.sendMessageCalls, 1);
    expect(imService.lastSentSessionId, 'session-target-agent-1');

    final content = imService.lastSentContent ?? '';
    final uriMatch = RegExp(r'\((grix://card/[^)]+)\)').firstMatch(content);
    expect(uriMatch, isNotNull);
    final card = ChatMessageCardCodec.decodeGrixUriCard(uriMatch!.group(1)!);
    expect(card, isA<ChatUserProfileCardData>());
    expect((card as ChatUserProfileCardData).userId, 'agent-9');
    expect(card.peerType, 2);
    expect(card.nickname, 'Ops Agent');

    controller.onClose();
  });

  test('group source profile keeps target user private context', () async {
    final imService = _FakeImService();
    final friendService = _FakeFriendService();
    final sessionService = _FakeSessionService();
    Get.put<ImService>(imService);
    Get.put<SessionService>(sessionService);
    final now = DateTime.now().millisecondsSinceEpoch;

    friendService.friendList.assignAll([
      FriendItem(
        id: 'f-1',
        userId: '1001',
        username: 'alice_01',
        nickname: 'Alice',
        remarkName: '',
        avatarUrl: '',
      ),
    ]);

    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'group-sid',
        title: '项目群',
        type: 'group',
        peerId: '',
        peerType: 0,
        updatedAt: now - 1000,
        unreadCount: 0,
        lastMessage: '群消息',
        lastMessageTime: now - 1000,
      ),
      SessionModel(
        sessionId: 'private-sid-1',
        title: 'Alice',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now,
        unreadCount: 0,
        lastMessage: '私聊1',
        lastMessageTime: now,
      ),
      SessionModel(
        sessionId: 'private-sid-2',
        title: 'Alice 旧会话',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now - 500,
        unreadCount: 0,
        lastMessage: '私聊2',
        lastMessageTime: now - 500,
      ),
    ]);

    sessionService.createResult = 'private-sid-new';
    sessionService.detailsBySessionId['private-sid-new'] =
        const SessionDetailResult(
          data: <String, dynamic>{
            'session_type': 1,
            'members': <Map<String, dynamic>>[
              <String, dynamic>{'member_id': '1001', 'member_type': 1},
              <String, dynamic>{'member_id': '9009', 'member_type': 1},
            ],
          },
        );

    final controller = AccountInfoController(
      initialArguments: {
        'group_key': 'session:group-sid',
        'session_id': 'group-sid',
        'peer_id': '1001',
        'peer_type': '1',
        'nickname': 'Alice',
        'username': 'alice_01',
      },
      imService: imService,
      friendService: friendService,
      sessionService: sessionService,
    );
    controller.onInit();
    await Future<void>.delayed(Duration.zero);

    expect(controller.conversationSessions.map((s) => s.sessionId).toList(), [
      'private-sid-1',
      'private-sid-2',
    ]);

    await controller.startChatFromProfile();

    expect(sessionService.openLatestCalls, 0);
    expect(sessionService.createCalls, 1);
    expect(sessionService.lastCreatePeerId, '1001');
    expect(sessionService.lastCreatePeerType, 1);
    expect(imService.lastResolvedSessionId, 'private-sid-new');
    expect(imService.lastResolvedSessionType, 'private');

    controller.onClose();
  });

  test(
    'startChatFromProfile creates owned agent session with peer type 2',
    () async {
      final imService = _FakeImService();
      final agentService = _FakeAgentService();
      final authService = _FakeAuthService(userId: 'owner-1');
      final sessionService = _FakeSessionService();
      Get.put<ImService>(imService);
      Get.put<SessionService>(sessionService);
      final now = DateTime.now().millisecondsSinceEpoch;

      agentService.agents.assignAll([
        AgentModel(id: 'agent-9', agentName: 'Planner', ownerID: 'owner-1'),
      ]);

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'agent-old-sid',
          title: 'Planner',
          type: 'private',
          peerId: 'agent-9',
          peerType: 2,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: '',
          lastMessageTime: now,
        ),
      ]);

      sessionService.createResult = 'agent-sid';
      sessionService.detailsBySessionId['agent-sid'] =
          const SessionDetailResult(
            data: <String, dynamic>{
              'session_type': 1,
              'members': <Map<String, dynamic>>[
                <String, dynamic>{'member_id': 'agent-9', 'member_type': 2},
                <String, dynamic>{'member_id': 'owner-1', 'member_type': 1},
              ],
            },
          );

      final controller = AccountInfoController(
        initialArguments: {'peer_id': 'agent-9', 'peer_type': '2'},
        imService: imService,
        agentService: agentService,
        authService: authService,
        sessionService: sessionService,
      );
      controller.onInit();
      await Future<void>.delayed(Duration.zero);

      await controller.startChatFromProfile();

      expect(sessionService.openLatestCalls, 0);
      expect(sessionService.createCalls, 1);
      expect(sessionService.lastCreatePeerId, 'agent-9');
      expect(sessionService.lastCreatePeerType, 2);
      expect(imService.lastResolvedSessionId, 'agent-sid');
      expect(imService.lastResolvedSessionType, 'private');

      controller.onClose();
    },
  );

  test('non-owner agent cannot start chat', () async {
    final imService = _FakeImService();
    final agentService = _FakeAgentService();
    final authService = _FakeAuthService(userId: 'owner-2');

    agentService.agents.assignAll([
      AgentModel(id: 'agent-9', agentName: 'Planner', ownerID: 'owner-1'),
    ]);

    final controller = AccountInfoController(
      initialArguments: {'peer_id': 'agent-9', 'peer_type': '2'},
      imService: imService,
      agentService: agentService,
      authService: authService,
    );
    controller.onInit();
    await Future<void>.delayed(Duration.zero);

    expect(controller.isOwnedAgent, isFalse);
    expect(controller.canStartChat, isFalse);
    expect(controller.conversationSessions, isEmpty);

    controller.onClose();
  });

  test('startChatFromProfile creates a new private session', () async {
    final imService = _FakeImService();
    final friendService = _FakeFriendService();
    final sessionService = _FakeSessionService();
    Get.put<ImService>(imService);
    Get.put<SessionService>(sessionService);
    final now = DateTime.now().millisecondsSinceEpoch;

    friendService.friendList.assignAll([
      FriendItem(
        id: 'f-1',
        userId: '1001',
        username: 'alice_01',
        nickname: 'Alice',
        remarkName: '',
        avatarUrl: '',
      ),
    ]);

    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'group-sid',
        title: 'G',
        type: 'group',
        peerId: '',
        peerType: 0,
        updatedAt: now,
        unreadCount: 0,
        lastMessage: '',
        lastMessageTime: now,
      ),
    ]);

    sessionService.createResult = 'private-sid';
    sessionService.detailsBySessionId['private-sid'] =
        const SessionDetailResult(
          data: <String, dynamic>{
            'session_type': 1,
            'members': <Map<String, dynamic>>[
              <String, dynamic>{'member_id': '1001', 'member_type': 1},
              <String, dynamic>{'member_id': '9009', 'member_type': 1},
            ],
          },
        );

    final controller = AccountInfoController(
      initialArguments: {
        'peer_id': '1001',
        'peer_type': '1',
        'nickname': 'Alice',
        'username': 'alice_01',
      },
      imService: imService,
      friendService: friendService,
      sessionService: sessionService,
    );
    controller.onInit();
    await Future<void>.delayed(Duration.zero);

    await controller.startChatFromProfile();

    expect(sessionService.openLatestCalls, 0);
    expect(sessionService.createCalls, 1);
    expect(sessionService.lastCreatePeerId, '1001');
    expect(sessionService.lastCreatePeerType, 1);
    expect(imService.lastResolvedSessionId, 'private-sid');
    expect(imService.lastResolvedSessionType, 'private');

    controller.onClose();
  });

  test(
    'startChatFromProfile creates a new session instead of reusing latest',
    () async {
      final imService = _FakeImService();
      final friendService = _FakeFriendService();
      final sessionService = _FakeSessionService();
      Get.put<ImService>(imService);
      Get.put<SessionService>(sessionService);
      final now = DateTime.now().millisecondsSinceEpoch;

      friendService.friendList.assignAll([
        FriendItem(
          id: 'f-1',
          userId: '1001',
          username: 'alice_01',
          nickname: 'Alice',
          remarkName: '',
          avatarUrl: '',
        ),
      ]);

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'private-sid',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: '',
          lastMessageTime: now,
        ),
      ]);

      sessionService.createResult = 'private-new-sid';
      sessionService.detailsBySessionId['private-new-sid'] =
          const SessionDetailResult(
            data: <String, dynamic>{
              'session_type': 1,
              'members': <Map<String, dynamic>>[
                <String, dynamic>{'member_id': '1001', 'member_type': 1},
                <String, dynamic>{'member_id': '9009', 'member_type': 1},
              ],
            },
          );

      final controller = AccountInfoController(
        initialArguments: {
          'peer_id': '1001',
          'peer_type': '1',
          'nickname': 'Alice',
          'username': 'alice_01',
        },
        imService: imService,
        friendService: friendService,
        sessionService: sessionService,
      );
      controller.onInit();
      await Future<void>.delayed(Duration.zero);

      await controller.startChatFromProfile();

      expect(sessionService.openLatestCalls, 0);
      expect(sessionService.createCalls, 1);
      expect(imService.lastResolvedSessionId, 'private-new-sid');
      expect(imService.lastResolvedSessionType, 'private');

      controller.onClose();
    },
  );

  test(
    'startChatFromProfile accepts created private detail with partial members',
    () async {
      final imService = _FakeImService();
      final friendService = _FakeFriendService();
      final sessionService = _FakeSessionService();
      Get.put<ImService>(imService);
      Get.put<SessionService>(sessionService);

      friendService.friendList.assignAll([
        FriendItem(
          id: 'f-1',
          userId: '1001',
          username: 'alice_01',
          nickname: 'Alice',
          remarkName: '',
          avatarUrl: '',
        ),
      ]);

      sessionService.createResult = 'private-sid';
      sessionService.detailsBySessionId['private-sid'] =
          const SessionDetailResult(
            data: <String, dynamic>{
              'session_type': 1,
              'members': <Map<String, dynamic>>[
                <String, dynamic>{'member_id': '1001', 'member_type': 1},
              ],
            },
          );

      final controller = AccountInfoController(
        initialArguments: {
          'peer_id': '1001',
          'peer_type': '1',
          'nickname': 'Alice',
          'username': 'alice_01',
        },
        imService: imService,
        friendService: friendService,
        sessionService: sessionService,
      );
      controller.onInit();
      await Future<void>.delayed(Duration.zero);

      await controller.startChatFromProfile();

      expect(sessionService.openLatestCalls, 0);
      expect(sessionService.createCalls, 1);
      expect(imService.lastResolvedSessionId, 'private-sid');
      expect(imService.lastResolvedSessionType, 'private');

      controller.onClose();
    },
  );

  test(
    'startChatFromProfile does not fall back to local private when create is invalid',
    () async {
      final imService = _FakeImService();
      final friendService = _FakeFriendService();
      final sessionService = _FakeSessionService();
      Get.put<ImService>(imService);
      Get.put<SessionService>(sessionService);
      final now = DateTime.now().millisecondsSinceEpoch;

      friendService.friendList.assignAll([
        FriendItem(
          id: 'f-1',
          userId: '1001',
          username: 'alice_01',
          nickname: 'Alice',
          remarkName: '',
          avatarUrl: '',
        ),
      ]);

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'group-sid',
          title: 'group',
          type: 'group',
          peerId: '',
          peerType: 0,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: '',
          lastMessageTime: now,
        ),
        SessionModel(
          sessionId: 'private-local-sid',
          title: 'Alice',
          type: 'private',
          peerId: '1001',
          peerType: 1,
          updatedAt: now - 1,
          unreadCount: 0,
          lastMessage: '',
          lastMessageTime: now - 1,
        ),
      ]);

      sessionService.createResult = 'group-sid';
      sessionService.detailsBySessionId['group-sid'] =
          const SessionDetailResult(
            data: <String, dynamic>{
              'session_type': 2,
              'members': <Map<String, dynamic>>[
                <String, dynamic>{'member_id': '1001', 'member_type': 1},
                <String, dynamic>{'member_id': '9009', 'member_type': 1},
              ],
            },
          );

      final controller = AccountInfoController(
        initialArguments: {
          'peer_id': '1001',
          'peer_type': '1',
          'nickname': 'Alice',
          'username': 'alice_01',
        },
        imService: imService,
        friendService: friendService,
        sessionService: sessionService,
      );
      controller.onInit();
      await Future<void>.delayed(Duration.zero);

      await controller.startChatFromProfile();

      expect(sessionService.openLatestCalls, 0);
      expect(sessionService.createCalls, 1);
      expect(imService.lastResolvedSessionId, 'group-sid');
      expect(imService.lastResolvedSessionType, 'private');

      controller.onClose();
    },
  );

  test(
    'openSession records the last tapped session id for highlighting',
    () async {
      final imService = _FakeImService();
      final friendService = _FakeFriendService();
      final now = DateTime.now().millisecondsSinceEpoch;

      friendService.friendList.assignAll([
        FriendItem(
          id: 'f-1',
          userId: '1001',
          username: 'alice_01',
          nickname: 'Alice',
          remarkName: '',
          avatarUrl: '',
        ),
      ]);

      final sessionA = SessionModel(
        sessionId: 'session-a',
        title: 'Thread A',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now - 200,
        unreadCount: 0,
        lastMessage: 'hello A',
        lastMessageTime: now - 200,
      );
      final sessionB = SessionModel(
        sessionId: 'session-b',
        title: 'Thread B',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now,
        unreadCount: 0,
        lastMessage: 'hello B',
        lastMessageTime: now,
      );
      imService.sessions.assignAll([sessionA, sessionB]);

      final controller = AccountInfoController(
        initialArguments: {
          'peer_id': '1001',
          'peer_type': '1',
          'nickname': 'Alice',
          'username': 'alice_01',
        },
        imService: imService,
        friendService: friendService,
      );
      controller.onInit();

      // 初始无任何高亮项
      expect(controller.lastTappedSessionId.value, isEmpty);

      // 点击 A 后，最后点击 id 等于 A
      controller.openSession(sessionA);
      expect(controller.lastTappedSessionId.value, 'session-a');

      // 切换点击 B 后，最后点击 id 迁移到 B（不累计）
      controller.openSession(sessionB);
      expect(controller.lastTappedSessionId.value, 'session-b');

      controller.onClose();
    },
  );

  test('db search filters sessions by group key and keyword', () async {
    const userId = 'db-search-user-1';
    final now = DateTime.now().millisecondsSinceEpoch;
    await LocalDb.setActiveUser(userId);
    await LocalDb.clearActiveUserData();

    await LocalDb.upsertSession({
      'session_id': 'thread-a',
      'title': 'Design Discussion',
      'type': 'private',
      'peer_id': '1001',
      'peer_type': 1,
      'peer_nickname': 'Alice',
      'updated_at': now,
      'last_message': 'hello',
      'last_message_time': now,
    });
    await LocalDb.upsertSession({
      'session_id': 'thread-b',
      'title': 'Code Review',
      'type': 'private',
      'peer_id': '1001',
      'peer_type': 1,
      'peer_nickname': 'Alice',
      'updated_at': now - 1000,
      'last_message': 'world',
      'last_message_time': now - 1000,
    });
    await LocalDb.upsertSession({
      'session_id': 'thread-other',
      'title': 'Design Chat',
      'type': 'private',
      'peer_id': '2002',
      'peer_type': 1,
      'peer_nickname': 'Bob',
      'updated_at': now - 500,
      'last_message': 'other',
      'last_message_time': now - 500,
    });

    final imService = _FakeImService();
    final friendService = _FakeFriendService();
    friendService.friendList.assignAll([
      FriendItem(
        id: 'f-1',
        userId: '1001',
        username: 'alice_01',
        nickname: 'Alice',
        remarkName: '',
        avatarUrl: '',
      ),
    ]);

    final controller = AccountInfoController(
      initialArguments: {
        'peer_id': '1001',
        'peer_type': '1',
        'nickname': 'Alice',
        'username': 'alice_01',
        'group_key': 'private:1:1001',
      },
      imService: imService,
      friendService: friendService,
    );
    controller.onInit();

    controller.searchQuery.value = 'Design';
    await Future<void>.delayed(const Duration(milliseconds: 300));

    final results = controller.conversationSessions;
    expect(results.length, 1);
    expect(results.first.sessionId, 'thread-a');

    controller.searchQuery.value = '';
    await Future<void>.delayed(const Duration(milliseconds: 300));

    controller.onClose();
    await LocalDb.setActiveUser(null);
  });

  test('db search matches last_message content', () async {
    const userId = 'db-search-user-2';
    final now = DateTime.now().millisecondsSinceEpoch;
    await LocalDb.setActiveUser(userId);
    await LocalDb.clearActiveUserData();

    await LocalDb.upsertSession({
      'session_id': 'thread-msg',
      'title': 'Thread 1',
      'type': 'private',
      'peer_id': '3003',
      'peer_type': 2,
      'peer_nickname': 'Agent X',
      'updated_at': now,
      'last_message': 'deployment completed successfully',
      'last_message_time': now,
    });
    await LocalDb.upsertSession({
      'session_id': 'thread-nomatch',
      'title': 'Thread 2',
      'type': 'private',
      'peer_id': '3003',
      'peer_type': 2,
      'peer_nickname': 'Agent X',
      'updated_at': now - 1000,
      'last_message': 'hello world',
      'last_message_time': now - 1000,
    });

    final imService = _FakeImService();
    final agentService = _FakeAgentService();
    final authService = _FakeAuthService(userId: 'owner-1');
    agentService.agents.assignAll([
      AgentModel(id: '3003', agentName: 'Agent X', ownerID: 'owner-1'),
    ]);

    final controller = AccountInfoController(
      initialArguments: {
        'peer_id': '3003',
        'peer_type': '2',
        'nickname': 'Agent X',
        'group_key': 'private:2:3003',
      },
      imService: imService,
      agentService: agentService,
      authService: authService,
    );
    controller.onInit();

    controller.searchQuery.value = 'deployment';
    await Future<void>.delayed(const Duration(milliseconds: 300));

    final results = controller.conversationSessions;
    expect(results.length, 1);
    expect(results.first.sessionId, 'thread-msg');

    controller.onClose();
    await LocalDb.setActiveUser(null);
  });

  test('clearing search query returns to in-memory sessions', () async {
    const userId = 'db-search-user-3';
    await LocalDb.setActiveUser(userId);
    await LocalDb.clearActiveUserData();

    final now = DateTime.now().millisecondsSinceEpoch;
    final imService = _FakeImService();
    final friendService = _FakeFriendService();

    friendService.friendList.assignAll([
      FriendItem(
        id: 'f-1',
        userId: '1001',
        username: 'alice_01',
        nickname: 'Alice',
        remarkName: '',
        avatarUrl: '',
      ),
    ]);

    imService.sessions.assignAll([
      SessionModel(
        sessionId: 's-mem-1',
        title: 'Memory Thread',
        type: 'private',
        peerId: '1001',
        peerType: 1,
        updatedAt: now,
        unreadCount: 0,
        lastMessage: 'in memory',
        lastMessageTime: now,
      ),
    ]);

    final controller = AccountInfoController(
      initialArguments: {
        'peer_id': '1001',
        'peer_type': '1',
        'nickname': 'Alice',
        'group_key': 'private:1:1001',
      },
      imService: imService,
      friendService: friendService,
    );
    controller.onInit();

    expect(controller.conversationSessions.length, 1);
    expect(controller.conversationSessions.first.sessionId, 's-mem-1');

    controller.searchQuery.value = 'nonexistent-keyword-xyz';
    await Future<void>.delayed(const Duration(milliseconds: 300));
    expect(controller.conversationSessions, isEmpty);

    controller.searchQuery.value = '';
    await Future<void>.delayed(const Duration(milliseconds: 300));
    expect(controller.conversationSessions.length, 1);
    expect(controller.conversationSessions.first.sessionId, 's-mem-1');

    controller.onClose();
    await LocalDb.setActiveUser(null);
  });

  test(
    'server thread pages backfill history outside the local window',
    () async {
      final now = DateTime.now().millisecondsSinceEpoch;
      final imService = _FakeImService();
      final sessionService = _FakeSessionService();

      imService.sessions.assignAll([
        SessionModel(
          sessionId: 's-mem-1',
          title: 'Local Thread',
          type: 'private',
          peerId: '2001',
          peerType: 2,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'local',
          lastMessageTime: now,
        ),
      ]);

      // 服务端分页返回：一条本地窗口外的老会话 + 一条已被本地删除的会话。
      sessionService.threadPages.add(
        ConversationThreadPageResult(
          groupKey: 'private:2:2001',
          sessions: [
            SessionModel(
              sessionId: 's-old-1',
              title: 'Archived Thread',
              type: 'private',
              peerId: '2001',
              peerType: 2,
              updatedAt: now - 900000,
              unreadCount: 0,
              lastMessage: 'archived',
              lastMessageTime: now - 900000,
            ),
            SessionModel(
              sessionId: 's-deleted-1',
              title: 'Deleted Thread',
              type: 'private',
              peerId: '2001',
              peerType: 2,
              updatedAt: now - 800000,
              unreadCount: 0,
              lastMessage: 'deleted',
              lastMessageTime: now - 800000,
            ),
          ],
        ),
      );
      imService.locallyDeletedSessionIds.add('s-deleted-1');

      final controller = AccountInfoController(
        initialArguments: {
          'peer_id': '2001',
          'peer_type': '2',
          'group_key': 'private:2:2001',
        },
        imService: imService,
        sessionService: sessionService,
      );
      controller.onInit();
      await Future<void>.delayed(const Duration(milliseconds: 20));

      final ids = controller.conversationSessions
          .map((session) => session.sessionId)
          .toList();
      expect(ids, ['s-mem-1', 's-old-1']);
      expect(sessionService.threadRequestCursors, ['']);

      controller.onClose();
    },
  );

  test('server thread pages do not shadow live local session state', () async {
    final now = DateTime.now().millisecondsSinceEpoch;
    final imService = _FakeImService();
    final sessionService = _FakeSessionService();

    imService.sessions.assignAll([
      SessionModel(
        sessionId: 's-shared-1',
        title: 'Local Title',
        type: 'private',
        peerId: '2001',
        peerType: 2,
        updatedAt: now,
        unreadCount: 3,
        lastMessage: 'local',
        lastMessageTime: now,
      ),
    ]);
    sessionService.threadPages.add(
      ConversationThreadPageResult(
        groupKey: 'private:2:2001',
        sessions: [
          SessionModel(
            sessionId: 's-shared-1',
            title: 'Server Title',
            type: 'private',
            peerId: '2001',
            peerType: 2,
            updatedAt: now - 1000,
            unreadCount: 0,
            lastMessage: 'server',
            lastMessageTime: now - 1000,
          ),
        ],
      ),
    );

    final controller = AccountInfoController(
      initialArguments: {
        'peer_id': '2001',
        'peer_type': '2',
        'group_key': 'private:2:2001',
      },
      imService: imService,
      sessionService: sessionService,
    );
    controller.onInit();
    await Future<void>.delayed(const Duration(milliseconds: 20));

    expect(controller.conversationSessions.length, 1);
    expect(controller.conversationSessions.first.unreadCount, 3);
    expect(controller.conversationSessions.first.title, 'Local Title');

    controller.onClose();
  });

  test('search also covers server-paged history sessions', () async {
    const userId = 'db-search-user-4';
    await LocalDb.setActiveUser(userId);
    await LocalDb.clearActiveUserData();

    final now = DateTime.now().millisecondsSinceEpoch;
    final imService = _FakeImService();
    final sessionService = _FakeSessionService();

    sessionService.threadPages.add(
      ConversationThreadPageResult(
        groupKey: 'private:2:2001',
        sessions: [
          SessionModel(
            sessionId: 's-old-1',
            title: '发布流程复盘',
            type: 'private',
            peerId: '2001',
            peerType: 2,
            updatedAt: now - 900000,
            unreadCount: 0,
            lastMessage: 'archived',
            lastMessageTime: now - 900000,
          ),
          SessionModel(
            sessionId: 's-old-2',
            title: '无关会话',
            type: 'private',
            peerId: '2001',
            peerType: 2,
            updatedAt: now - 800000,
            unreadCount: 0,
            lastMessage: 'nothing',
            lastMessageTime: now - 800000,
          ),
        ],
      ),
    );

    final controller = AccountInfoController(
      initialArguments: {
        'peer_id': '2001',
        'peer_type': '2',
        'group_key': 'private:2:2001',
      },
      imService: imService,
      sessionService: sessionService,
    );
    controller.onInit();
    await Future<void>.delayed(const Duration(milliseconds: 20));

    controller.searchQuery.value = '发布流程';
    await Future<void>.delayed(const Duration(milliseconds: 300));

    expect(
      controller.conversationSessions.map((session) => session.sessionId),
      ['s-old-1'],
    );

    controller.onClose();
    await LocalDb.setActiveUser(null);
  });

  group('introductionPreview', () {
    test('keeps content before the first blank line only', () {
      final controller = AccountInfoController(
        initialArguments: const {},
        imService: _FakeImService(),
      );
      controller.introduction.value = '第一段简介。\n\n\n\n\n空行之后的内部内容';
      expect(controller.introductionPreview, '第一段简介。');
    });

    test('returns full text when no blank line exists', () {
      final controller = AccountInfoController(
        initialArguments: const {},
        imService: _FakeImService(),
      );
      controller.introduction.value = 'line1\nline2\nline3';
      expect(controller.introductionPreview, 'line1\nline2\nline3');
    });

    test('treats whitespace-only line as blank separator', () {
      final controller = AccountInfoController(
        initialArguments: const {},
        imService: _FakeImService(),
      );
      controller.introduction.value = 'line1\n   \nhidden';
      expect(controller.introductionPreview, 'line1');
    });

    test('empty introduction yields empty preview', () {
      final controller = AccountInfoController(
        initialArguments: const {},
        imService: _FakeImService(),
      );
      controller.introduction.value = '   \n\n';
      expect(controller.introductionPreview, '');
    });
  });
}

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/group_info/controllers/group_info_controller.dart';

class _FakeImService extends ImService {}

class _FakeSessionService extends SessionService {
  SessionDetailResult detailResult = const SessionDetailResult(data: null);
  int fetchDetailCalls = 0;

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    fetchDetailCalls++;
    return detailResult;
  }
}

class _FakeFriendService extends FriendService {
  final _profileCache = <String, Map<String, String>>{};

  @override
  Future<String?> fetchUserProfile(String userId) async {
    final nickname = 'Nick$userId';
    final username = 'user_$userId';
    _profileCache[userId] = {
      'nickname': nickname,
      'username': username,
      'avatar_url': 'https://example.com/$userId.png',
    };
    return nickname;
  }

  @override
  String? getUserNickname(String userId) {
    final friend = friendList.where((f) => f.userId == userId).toList();
    if (friend.isNotEmpty) {
      final item = friend.first;
      return item.nickname.isNotEmpty ? item.nickname : item.username;
    }
    final profile = _profileCache[userId];
    if (profile == null) return null;
    final nickname = profile['nickname']?.trim() ?? '';
    if (nickname.isNotEmpty) return nickname;
    final username = profile['username']?.trim() ?? '';
    if (username.isNotEmpty) return username;
    return null;
  }

  @override
  String? getUserUsername(String userId) {
    final friend = friendList.where((f) => f.userId == userId).toList();
    if (friend.isNotEmpty) {
      final username = friend.first.username.trim();
      if (username.isNotEmpty) return username;
    }
    final profile = _profileCache[userId];
    final username = profile?['username']?.trim() ?? '';
    if (username.isNotEmpty) return username;
    return null;
  }

  @override
  String? getUserAvatarUrl(String userId) {
    final friend = friendList.where((f) => f.userId == userId).toList();
    if (friend.isNotEmpty) {
      final avatar = friend.first.avatarUrl.trim();
      if (avatar.isNotEmpty) return avatar;
    }
    final profile = _profileCache[userId];
    final avatar = profile?['avatar_url']?.trim() ?? '';
    if (avatar.isNotEmpty) return avatar;
    return null;
  }
}

class _FakeAgentService extends AgentService {
  @override
  Future<void> loadAgents({String? categoryId}) async {}
}

void main() {
  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(() {
    Get.reset();
  });

  test('loads group members and prefers session member nickname in group',
      () async {
    final imService = _FakeImService();
    final sessionService = _FakeSessionService();
    final friendService = _FakeFriendService();
    final agentService = _FakeAgentService();

    friendService.friendList.assignAll([
      FriendItem(
        id: 'f1',
        userId: 'u-1',
        username: 'owner_user',
        nickname: 'Owner Local',
        remarkName: '',
        avatarUrl: 'https://example.com/u-1.png',
      ),
    ]);
    agentService.agents.assignAll([
      AgentModel(
        id: 'a-1',
        agentName: 'Agent One',
        avatarUrl: 'https://example.com/a-1.png',
      ),
    ]);

    sessionService.detailResult = const SessionDetailResult(
      data: {
        'session_type': 2,
        'title': 'Dev Group',
        'members': [
          {
            'member_id': 'u-1',
            'member_type': 1,
            'role': 3,
            'nickname': 'Owner API',
          },
          {
            'member_id': 'u-2',
            'member_type': 1,
            'role': 1,
            'nickname': 'Guest API',
          },
          {
            'member_id': 'a-1',
            'member_type': 2,
            'role': 0,
            'nickname': 'Bot API',
          },
        ],
      },
    );

    final controller = GroupInfoController(
      initialArguments: {
        'session_id': 'g-1',
        'title': 'Fallback Name',
      },
      imService: imService,
      sessionService: sessionService,
      friendService: friendService,
      agentService: agentService,
    );
    controller.onInit();
    await Future<void>.delayed(Duration.zero);

    expect(sessionService.fetchDetailCalls, 1);
    expect(controller.groupName.value, 'Dev Group');
    expect(controller.memberCount, 3);

    final owner = controller.members.firstWhere((m) => m.memberId == 'u-1');
    expect(owner.isUser, isTrue);
    expect(owner.isFriend, isTrue);
    expect(owner.displayName, 'Owner API');

    final guest = controller.members.firstWhere((m) => m.memberId == 'u-2');
    expect(guest.isUser, isTrue);
    expect(guest.isFriend, isFalse);
    expect(guest.displayName, 'Guest API');

    final bot = controller.members.firstWhere((m) => m.memberId == 'a-1');
    expect(bot.isUser, isFalse);
    expect(bot.displayName, 'Bot API');
    expect(bot.avatarUrl, 'https://example.com/a-1.png');

    agentService.agents.assignAll([
      AgentModel(
        id: 'a-1',
        agentName: 'Agent One',
        avatarUrl: 'https://example.com/a-1-v2.png',
      ),
    ]);
    await Future<void>.delayed(Duration.zero);

    final refreshedBot =
        controller.members.firstWhere((m) => m.memberId == 'a-1');
    expect(refreshedBot.avatarUrl, 'https://example.com/a-1-v2.png');

    controller.onClose();
  });
}

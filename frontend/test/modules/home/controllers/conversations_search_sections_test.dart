import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/local_search_result.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/home/controllers/conversations_controller.dart';
import 'package:grix/modules/home/services/friend_qr_flow_service.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';

class _FakeImService extends ImService {
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
  Future<bool> loadMoreSessionWindowIfNeeded({bool force = false}) async {
    return false;
  }
}

SessionModel _session(String id, String title, {int updatedAt = 1000}) {
  return SessionModel(
    sessionId: id,
    title: title,
    type: 'group',
    peerId: '',
    peerType: 0,
    updatedAt: updatedAt,
    lastMessage: '',
    lastMessageTime: updatedAt,
  );
}

ConversationListItem _item(SessionModel session) {
  return ConversationListItem(
    groupKey: 'session:${session.sessionId}',
    latestSession: session,
    sessions: [session],
    unreadCount: 0,
    isPinned: false,
    pinnedAt: 0,
  );
}

MatchedMessage _message(String msgId, String content) {
  return MatchedMessage(
    msgId: msgId,
    sessionId: 's-alpha',
    content: content,
    createdAt: 1000,
  );
}

/// 搜索去抖 200ms，留出余量让三段结果落地。
Future<void> _settle() =>
    Future<void>.delayed(const Duration(milliseconds: 320));

void main() {
  late _FakeImService imService;
  late FriendService friendService;
  late AgentService agentService;
  late ConversationsController controller;
  late SessionModel alpha;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    UserImageCacheManager.setDisabledForTest(true);
    imService = _FakeImService();
    Get.put<ImService>(imService);
    Get.put<FriendQrFlowService>(FriendQrFlowService());
    friendService = Get.put<FriendService>(FriendService());
    agentService = Get.put<AgentService>(AgentService());
    friendService.friendList.assignAll([
      FriendItem(
        id: 'f1',
        userId: 'u-wang',
        username: 'laowang',
        nickname: '老王',
        remarkName: '',
        avatarUrl: '',
      ),
      FriendItem(
        id: 'f2',
        userId: 'u-li',
        username: 'xiaoli',
        nickname: '小李',
        remarkName: '',
        avatarUrl: '',
      ),
    ]);
    agentService.agents.assignAll([
      AgentModel(id: 'a1', agentName: '装修助手'),
      AgentModel(id: 'a2', agentName: '翻译助手'),
    ]);
    controller = Get.put(ConversationsController());

    alpha = _session('s-alpha', '装修群', updatedAt: 3000);
    controller.seedConversationSummaryItemsForTest([_item(alpha)]);
    controller.applyConversationSummaryItemsForTest();
  });

  tearDown(() {
    UserImageCacheManager.setDisabledForTest(false);
    Get.reset();
  });

  test('搜一个词同时出会话段和聊天记录段', () async {
    controller.searchSessionRecordsOverrideForTest = (_) async => [
      alpha.toJson(),
    ];
    controller.searchMessagesOverrideForTest = (_) async => [
      _message('m-1', '装修下周开工'),
      _message('m-2', '装修报价单'),
    ];

    controller.updateSearchQuery('装修');
    await _settle();

    expect(
      controller.groupedSessions.map((item) => item.groupKey).toList(),
      ['session:s-alpha'],
      reason: '会话段没有结果',
    );
    expect(
      controller.searchMessages.map((m) => m.msgId).toList(),
      ['m-1', 'm-2'],
      reason: '聊天记录段没有结果',
    );
    expect(controller.hasAnySearchResult, isTrue);
  });

  test('联系人和 Agent 段命中本地好友与 agent 缓存', () async {
    controller.searchSessionRecordsOverrideForTest = (_) async => [];
    controller.searchMessagesOverrideForTest = (_) async => [];

    controller.updateSearchQuery('助手');
    await _settle();

    expect(
      controller.searchContacts.map((c) => c.peerId).toList(),
      ['a1', 'a2'],
    );
    expect(controller.searchContacts.every((c) => c.peerType == 2), isTrue);
    expect(controller.hasAnySearchResult, isTrue);
  });

  test('三段全空时没有任何搜索结果', () async {
    controller.searchSessionRecordsOverrideForTest = (_) async => [];
    controller.searchMessagesOverrideForTest = (_) async => [];

    controller.updateSearchQuery('查不到的词');
    await _settle();

    expect(controller.groupedSessions, isEmpty);
    expect(controller.searchContacts, isEmpty);
    expect(controller.searchMessages, isEmpty);
    expect(controller.hasAnySearchResult, isFalse);
  });

  test('清空搜索后三段结果一并清掉', () async {
    controller.searchSessionRecordsOverrideForTest = (_) async => [
      alpha.toJson(),
    ];
    controller.searchMessagesOverrideForTest = (_) async => [
      _message('m-1', '装修下周开工'),
    ];

    controller.updateSearchQuery('装修');
    await _settle();
    expect(controller.searchMessages, isNotEmpty);

    controller.updateSearchQuery('');
    await _settle();

    expect(controller.isSearching, isFalse);
    expect(controller.searchContacts, isEmpty);
    expect(controller.searchMessages, isEmpty);
    expect(
      controller.groupedSessions.map((item) => item.groupKey).toList(),
      ['session:s-alpha'],
      reason: '清空搜索后没有恢复全量列表',
    );
  });

  test('AI 带入的关键词同时写进搜索框并触发搜索', () async {
    controller.searchSessionRecordsOverrideForTest = (_) async => [
      alpha.toJson(),
    ];
    controller.searchMessagesOverrideForTest = (_) async => [
      _message('m-1', '装修下周开工'),
    ];

    controller.applyExternalSearchQuery('装修 开工');
    await _settle();

    expect(controller.searchInputController.text, '装修 开工');
    expect(controller.isSearching, isTrue);
    expect(controller.searchMessages.map((m) => m.msgId).toList(), ['m-1']);
  });
}

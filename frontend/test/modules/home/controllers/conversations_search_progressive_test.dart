import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/local_search_result.dart';
import 'package:grix/data/models/session_model.dart';
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

/// 搜索去抖 200ms，留出余量让搜索派发落地。
Future<void> _settle() =>
    Future<void>.delayed(const Duration(milliseconds: 320));

/// 短暂等待，让某一段搜索结果的 Completer 落地生效。
Future<void> _tick() => Future<void>.delayed(const Duration(milliseconds: 20));

void main() {
  late _FakeImService imService;
  late ConversationsController controller;
  late SessionModel alpha;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    UserImageCacheManager.setDisabledForTest(true);
    imService = _FakeImService();
    Get.put<ImService>(imService);
    Get.put<FriendQrFlowService>(FriendQrFlowService());
    controller = Get.put(ConversationsController());

    alpha = _session('s-alpha', '装修群', updatedAt: 3000);
    controller.seedConversationSummaryItemsForTest([_item(alpha)]);
    controller.applyConversationSummaryItemsForTest();
  });

  tearDown(() {
    UserImageCacheManager.setDisabledForTest(false);
    Get.reset();
  });

  test('in-flight 在派发时为 true，最新版本两段都落地后为 false，过期版本的迟到完成不改动它', () async {
    final sessionsGate1 = Completer<List<Map<String, dynamic>>>();
    final messagesGate1 = Completer<List<MatchedMessage>>();
    controller.searchSessionRecordsOverrideForTest = (_) =>
        sessionsGate1.future;
    controller.searchMessagesOverrideForTest = (_) => messagesGate1.future;

    expect(controller.searchInFlight.value, isFalse);

    controller.updateSearchQuery('装修');
    await _settle();
    expect(
      controller.searchInFlight.value,
      isTrue,
      reason: '派发搜索后应立即进入 in-flight',
    );

    sessionsGate1.complete(const []);
    await _tick();
    expect(
      controller.searchInFlight.value,
      isTrue,
      reason: '消息段还没回来，不能提前收起 in-flight',
    );

    // 改词触发下一版搜索（v2），换新的 gate。
    final sessionsGate2 = Completer<List<Map<String, dynamic>>>();
    final messagesGate2 = Completer<List<MatchedMessage>>();
    controller.searchSessionRecordsOverrideForTest = (_) =>
        sessionsGate2.future;
    controller.searchMessagesOverrideForTest = (_) => messagesGate2.future;
    controller.updateSearchQuery('装修工');
    await _settle();
    expect(controller.searchInFlight.value, isTrue);

    // v1 的消息段这时才姗姗来迟，不该影响由 v2 决定的 in-flight。
    messagesGate1.complete(const []);
    await _tick();
    expect(
      controller.searchInFlight.value,
      isTrue,
      reason: '过期版本(v1)的迟到完成不该把 in-flight 收起',
    );

    sessionsGate2.complete(const []);
    messagesGate2.complete(const []);
    await _tick();
    expect(
      controller.searchInFlight.value,
      isFalse,
      reason: '最新版本(v2)两段都落地后应该收起 in-flight',
    );
  });

  test('会话段先于聊天记录段落地：会话立即返回、聊天记录延迟返回', () async {
    controller.searchSessionRecordsOverrideForTest = (_) async => [
      alpha.toJson(),
    ];
    final messagesGate = Completer<List<MatchedMessage>>();
    controller.searchMessagesOverrideForTest = (_) => messagesGate.future;

    controller.updateSearchQuery('装修');
    await _settle();

    expect(
      controller.groupedSessions.map((i) => i.groupKey).toList(),
      ['session:s-alpha'],
      reason: '会话段应该已经先落地',
    );
    expect(controller.sessionsSearchPending, isFalse);
    expect(controller.searchMessages, isEmpty, reason: '聊天记录段还没回来');
    expect(controller.messagesSearchPending, isTrue, reason: '聊天记录段应仍在等待中');

    messagesGate.complete([_message('m-1', '装修下周开工')]);
    await _tick();

    expect(controller.searchMessages.map((m) => m.msgId).toList(), ['m-1']);
    expect(controller.messagesSearchPending, isFalse);
  });

  test('in-flight 期间三段全空也不触发 no_match 空态', () async {
    final sessionsGate = Completer<List<Map<String, dynamic>>>();
    final messagesGate = Completer<List<MatchedMessage>>();
    controller.searchSessionRecordsOverrideForTest = (_) =>
        sessionsGate.future;
    controller.searchMessagesOverrideForTest = (_) => messagesGate.future;

    controller.updateSearchQuery('查无此词');
    await _settle();

    expect(controller.hasAnySearchResult, isFalse);
    expect(
      controller.shouldShowSearchNoMatch,
      isFalse,
      reason: 'in-flight 期间即使三段全空也不该出现 no_match',
    );

    sessionsGate.complete(const []);
    messagesGate.complete(const []);
    await _tick();

    expect(
      controller.shouldShowSearchNoMatch,
      isTrue,
      reason: '两段都落地、确实全空后才应该出现 no_match',
    );
  });

  test('从空输入进入搜索：结果落地前不展示全量会话列表，连打改词期间沿用上一版结果兜底', () async {
    final sessionsGate = Completer<List<Map<String, dynamic>>>();
    controller.searchSessionRecordsOverrideForTest = (_) =>
        sessionsGate.future;
    controller.searchMessagesOverrideForTest = (_) async => const [];

    expect(
      controller.groupedSessions.map((i) => i.groupKey).toList(),
      ['session:s-alpha'],
      reason: '搜索前应该是全量列表',
    );

    controller.updateSearchQuery('装');
    await _settle();
    expect(
      controller.groupedSessions,
      isEmpty,
      reason: '第一次进入搜索、结果还没回来时不能继续显示全量列表',
    );
    expect(controller.sessionsSearchPending, isTrue);

    // 连打改词：新一版搜索派发前，会话段应该继续沿用当前状态（这里仍是空），
    // 不会在还没拿到结果时反复横跳回全量。
    controller.searchSessionRecordsOverrideForTest = (_) async => [
      alpha.toJson(),
    ];
    controller.updateSearchQuery('装修');
    await _settle();
    expect(
      controller.groupedSessions.map((i) => i.groupKey).toList(),
      ['session:s-alpha'],
      reason: '改词后的新一版搜索结果应该正常落地',
    );

    sessionsGate.complete(const []);
    await _tick();
    expect(
      controller.groupedSessions.map((i) => i.groupKey).toList(),
      ['session:s-alpha'],
      reason: '过期版本(第一版)的迟到结果不该覆盖当前已经落地的结果',
    );
  });

  test('叉掉/退格删空关键词立即恢复全量列表，不等 200ms 去抖，过期结果不会覆盖', () async {
    final sessionsGate = Completer<List<Map<String, dynamic>>>();
    controller.searchSessionRecordsOverrideForTest = (_) => sessionsGate.future;
    controller.searchMessagesOverrideForTest = (_) async => const [];

    controller.updateSearchQuery('装修');
    await _settle(); // 去抖真正派发一次搜索，进入 in-flight，会话段被清空占位

    expect(controller.isSearching, isTrue);
    expect(controller.searchInFlight.value, isTrue);
    expect(
      controller.groupedSessions,
      isEmpty,
      reason: '首次搜索结果还没回来，会话段已经被清空占位',
    );

    // 点叉：不等 200ms 去抖，状态应该同步复位。
    controller.applyExternalSearchQuery('');

    expect(controller.isSearching, isFalse, reason: '叉掉后应立即退出搜索态');
    expect(controller.searchInFlight.value, isFalse, reason: '叉掉后应立即收起 in-flight');
    expect(
      controller.groupedSessions.map((i) => i.groupKey).toList(),
      ['session:s-alpha'],
      reason: '叉掉后应立即恢复全量列表，不等去抖',
    );

    // 过期的会话查询这时才姗姗来迟，不该覆盖已经恢复的全量列表。
    sessionsGate.complete([alpha.toJson()]);
    await _tick();
    expect(
      controller.groupedSessions.map((i) => i.groupKey).toList(),
      ['session:s-alpha'],
      reason: '过期结果不该覆盖已经恢复的全量列表',
    );

    // 退格删到空走的是同一入口（updateSearchQuery），同样应该立即生效。
    controller.searchSessionRecordsOverrideForTest = (_) async => [
      alpha.toJson(),
    ];
    controller.updateSearchQuery('装');
    await _settle();
    expect(controller.isSearching, isTrue);

    controller.updateSearchQuery('');
    expect(controller.isSearching, isFalse);
    expect(controller.searchInFlight.value, isFalse);
    expect(
      controller.groupedSessions.map((i) => i.groupKey).toList(),
      ['session:s-alpha'],
      reason: '退格删空同样应该立即恢复全量列表，不等去抖',
    );
  });
}

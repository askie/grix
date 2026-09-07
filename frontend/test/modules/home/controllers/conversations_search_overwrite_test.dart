import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
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

List<String> _keys(ConversationsController controller) =>
    controller.groupedSessions.map((item) => item.groupKey).toList();

/// 搜索去抖 200ms，留出余量并让重跑搜索的 microtask 落地。
Future<void> _settle() =>
    Future<void>.delayed(const Duration(milliseconds: 320));

void main() {
  late _FakeImService imService;
  late ConversationsController controller;

  late SessionModel alpha;
  late SessionModel beta;
  late SessionModel gamma;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    UserImageCacheManager.setDisabledForTest(true);
    imService = _FakeImService();
    Get.put<ImService>(imService);
    Get.put<FriendQrFlowService>(FriendQrFlowService());
    controller = Get.put(ConversationsController());

    alpha = _session('s-alpha', '项目 Alpha', updatedAt: 3000);
    beta = _session('s-beta', '项目 Beta', updatedAt: 2000);
    gamma = _session('s-gamma', '闲聊 Gamma', updatedAt: 1000);
    controller.seedConversationSummaryItemsForTest([
      _item(alpha),
      _item(beta),
      _item(gamma),
    ]);
    // 用户进入会话页时先看到的是全量列表。
    controller.applyConversationSummaryItemsForTest();
  });

  tearDown(() {
    UserImageCacheManager.setDisabledForTest(false);
    Get.reset();
  });

  test('搜索态下会话摘要刷新不得覆盖搜索结果', () async {
    controller.searchSessionRecordsOverrideForTest = (_) async => [
      alpha.toJson(),
    ];

    controller.updateSearchQuery('Alpha');
    await _settle();
    expect(_keys(controller), ['session:s-alpha']);

    // 会话摘要分页刷新 / 加载更多 / 实时消息触发的延时刷新都走这条路径。
    controller.applyConversationSummaryItemsForTest(throttleReorder: true);
    await _settle();

    expect(
      _keys(controller),
      ['session:s-alpha'],
      reason: '摘要刷新把搜索结果盖回了全量列表',
    );
  });

  test('搜索态下删除会话仍有即时反馈', () async {
    controller.searchSessionRecordsOverrideForTest = (_) async => [
      alpha.toJson(),
      beta.toJson(),
    ];

    controller.updateSearchQuery('项目');
    await _settle();
    expect(_keys(controller), ['session:s-alpha', 'session:s-beta']);

    // 用户删除 alpha：本地库行已被删掉，摘要条目也同步移除。
    controller.searchSessionRecordsOverrideForTest = (_) async => [
      beta.toJson(),
    ];
    controller.removeConversationItemByGroupKeyForTest('session:s-alpha');
    await _settle();

    expect(_keys(controller), ['session:s-beta'], reason: '删除后搜索结果没有更新');
  });

  test('清空搜索后恢复全量会话列表', () async {
    controller.searchSessionRecordsOverrideForTest = (_) async => [
      alpha.toJson(),
    ];

    controller.updateSearchQuery('Alpha');
    await _settle();
    expect(_keys(controller), ['session:s-alpha']);

    controller.updateSearchQuery('');
    await _settle();

    expect(_keys(controller), [
      'session:s-alpha',
      'session:s-beta',
      'session:s-gamma',
    ]);
  });

  test('连打改词期间上一版搜索结果仍可落地，列表不退回全量', () async {
    final gate = Completer<void>();
    controller.searchSessionRecordsOverrideForTest = (_) async {
      await gate.future;
      return [alpha.toJson()];
    };

    expect(_keys(controller), hasLength(3));

    controller.updateSearchQuery('项目 A');
    await _settle();
    expect(_keys(controller), hasLength(3), reason: '搜索尚未返回，列表仍是原样');

    // 关键词已改，但新一轮去抖（200ms）还没触发。
    controller.updateSearchQuery('项目 AB');
    gate.complete();
    await Future<void>.delayed(const Duration(milliseconds: 40));

    expect(
      _keys(controller),
      ['session:s-alpha'],
      reason: '改词瞬间把上一版搜索结果丢掉，列表露出了全量',
    );
  });

  test('搜索已清空后迟到的搜索结果不再落地', () async {
    final gate = Completer<void>();
    controller.searchSessionRecordsOverrideForTest = (_) async {
      await gate.future;
      return [alpha.toJson()];
    };

    controller.updateSearchQuery('Alpha');
    await _settle();

    controller.searchSessionRecordsOverrideForTest = (_) async => [
      alpha.toJson(),
    ];
    controller.updateSearchQuery('');
    await _settle();
    expect(_keys(controller), hasLength(3));

    gate.complete();
    await _settle();

    expect(_keys(controller), hasLength(3), reason: '过期的搜索结果盖掉了全量列表');
  });
}

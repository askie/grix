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

SessionModel _privateSession({
  required String id,
  required int activity,
  bool pinned = false,
  int pinnedAt = 0,
  String peerId = 'user-alice',
  int peerType = 1,
}) {
  return SessionModel(
    sessionId: id,
    title: 'Alice',
    type: 'private',
    peerId: peerId,
    peerType: peerType,
    updatedAt: activity,
    lastMessageTime: activity,
    isPinned: pinned,
    pinnedAt: pinnedAt,
  );
}

void main() {
  late _FakeImService imService;
  late ConversationsController controller;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    UserImageCacheManager.setDisabledForTest(true);

    imService = _FakeImService();
    Get.put<ImService>(imService);
    Get.put<FriendQrFlowService>(FriendQrFlowService());

    controller = Get.put(ConversationsController());
  });

  tearDown(() {
    UserImageCacheManager.setDisabledForTest(false);
    Get.reset();
  });

  group('fetchConversationThreadSessions — 弹窗线程列表口径与资料页一致', () {
    test('置顶会话全部出现，且排在最前；非置顶按活跃倒序', () async {
      // 3 条置顶（会话级 isPinned）+ 3 条非置顶，全部属同一联系人 alice
      final pinnedOld = _privateSession(
        id: 'p_pinned_old', activity: 1000, pinned: true, pinnedAt: 100,
      );
      final pinnedMid = _privateSession(
        id: 'p_pinned_mid', activity: 1500, pinned: true, pinnedAt: 200,
      );
      final pinnedNew = _privateSession(
        id: 'p_pinned_new', activity: 5000, pinned: true, pinnedAt: 300,
      );
      final normalOld = _privateSession(id: 'p_normal_old', activity: 800);
      final normalMid = _privateSession(id: 'p_normal_mid', activity: 2000);
      final normalNew = _privateSession(id: 'p_normal_new', activity: 3000);

      // 故意打乱顺序加入 imService
      imService.sessions.assignAll([
        normalOld,
        pinnedOld,
        normalNew,
        pinnedMid,
        normalMid,
        pinnedNew,
      ]);

      // 模拟弹窗入参：item 只带 latestSession（summary 模式分页未覆盖到所有线程）
      final item = ConversationListItem(
        groupKey: 'private:1:user-alice',
        latestSession: pinnedNew,
        sessions: [pinnedNew],
        unreadCount: 0,
        isPinned: true,
        pinnedAt: 300,
        threadCountOverride: 6,
      );

      final result = await controller.fetchConversationThreadSessions(item);

      // 1. 总条数 = 同 groupKey 下所有 session 数量（与资料页 conversationSessions 一致）
      expect(result.sessions.length, 6,
          reason: '弹窗应展示该联系人下全部 6 条会话，等价于资料页');

      // 2. 置顶数量 = 3，且全部排在前面
      final pinnedCount = result.sessions.where((s) => s.isPinned).length;
      expect(pinnedCount, 3, reason: '会话级置顶共 3 条都应进入弹窗');

      final firstThree = result.sessions.sublist(0, 3);
      expect(firstThree.every((s) => s.isPinned), isTrue,
          reason: '置顶必须排在最前 3 位');

      // 3. 置顶内部按活跃倒序：pinnedNew > pinnedMid > pinnedOld
      expect(firstThree.map((s) => s.sessionId).toList(),
          ['p_pinned_new', 'p_pinned_mid', 'p_pinned_old']);

      // 4. 非置顶按活跃倒序：normalNew > normalMid > normalOld
      final lastThree = result.sessions.sublist(3, 6);
      expect(lastThree.map((s) => s.sessionId).toList(),
          ['p_normal_new', 'p_normal_mid', 'p_normal_old']);
    });

    test('与资料页 conversationSessions 排序完全等价（置顶在前 + 活跃倒序）', () async {
      // 这里复刻资料页的纯排序规则：先 isPinned 再 activityAt 倒序
      final sessions = [
        _privateSession(id: 'a', activity: 1000),
        _privateSession(id: 'b', activity: 2000, pinned: true, pinnedAt: 10),
        _privateSession(id: 'c', activity: 3000),
        _privateSession(id: 'd', activity: 1500, pinned: true, pinnedAt: 20),
        _privateSession(id: 'e', activity: 500),
      ];
      imService.sessions.assignAll(sessions);

      final item = ConversationListItem(
        groupKey: 'private:1:user-alice',
        latestSession: sessions[2],
        sessions: [sessions[2]],
        unreadCount: 0,
        isPinned: true,
        pinnedAt: 20,
        threadCountOverride: 5,
      );

      final result = await controller.fetchConversationThreadSessions(item);

      // 资料页期望顺序：置顶（b activity=2000 → d activity=1500），
      // 非置顶（c=3000 → a=1000 → e=500）
      expect(result.sessions.map((s) => s.sessionId).toList(),
          ['b', 'd', 'c', 'a', 'e']);
    });

    test('访客分组 visitor:group 走原有访客路径，不影响其它分组', () async {
      final v1 = SessionModel(
        sessionId: 'v1',
        title: 'visitor1',
        type: 'private',
        updatedAt: 1000,
        lastMessageTime: 1000,
        isVisitor: true,
      );
      final v2 = SessionModel(
        sessionId: 'v2',
        title: 'visitor2',
        type: 'private',
        updatedAt: 2000,
        lastMessageTime: 2000,
        isVisitor: true,
      );
      final other = _privateSession(id: 'p1', activity: 5000);

      imService.sessions.assignAll([v1, v2, other]);

      final item = ConversationListItem(
        groupKey: 'visitor:group',
        latestSession: v2,
        sessions: [v1, v2],
        unreadCount: 0,
        isPinned: false,
        pinnedAt: 0,
      );

      final result = await controller.fetchConversationThreadSessions(item);
      final ids = result.sessions.map((s) => s.sessionId).toSet();
      expect(ids, {'v1', 'v2'}, reason: '访客分组应只返回访客会话');
      expect(ids.contains('p1'), isFalse);
    });

    test('imService 中无匹配 session 时回退到 item.sessions（按活跃倒序）', () async {
      imService.sessions.clear();

      final s1 = _privateSession(id: 's1', activity: 1000);
      final s2 = _privateSession(id: 's2', activity: 2000);
      final item = ConversationListItem(
        groupKey: 'private:1:user-alice',
        latestSession: s2,
        sessions: [s1, s2],
        unreadCount: 0,
        isPinned: false,
        pinnedAt: 0,
      );

      final result = await controller.fetchConversationThreadSessions(item);
      expect(result.sessions.map((s) => s.sessionId).toList(), ['s2', 's1'],
          reason: 'matched 为空时应回退到 item.sessions 按活跃倒序');
    });

    test('同一会话即便在 imService 中重复出现也只计一次', () async {
      final s1 = _privateSession(id: 'p1', activity: 1000);
      final s2 = _privateSession(id: 'p2', activity: 2000, pinned: true);
      // 故意重复
      imService.sessions.assignAll([s1, s2, s1, s2]);

      final item = ConversationListItem(
        groupKey: 'private:1:user-alice',
        latestSession: s2,
        sessions: [s2],
        unreadCount: 0,
        isPinned: true,
        pinnedAt: 1,
      );

      final result = await controller.fetchConversationThreadSessions(item);
      expect(result.sessions.length, 2, reason: 'sessionId 去重不应放大数量');
      expect(result.sessions.first.sessionId, 'p2',
          reason: '置顶仍应排第一');
    });
  });
}

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

  group('resolveDirectChatTarget — 唯一未读 session 直达守卫', () {
    test('单 thread 会话直接返回 latestSession', () {
      final session = SessionModel(
        sessionId: 's1',
        title: 'Chat',
        type: 'group',
        updatedAt: 1000,
        lastMessageTime: 1000,
      );
      final item = ConversationListItem(
        groupKey: 'session:s1',
        latestSession: session,
        sessions: [session],
        unreadCount: 0,
        isPinned: true,
        pinnedAt: 100,
      );

      final target = controller.resolveDirectChatTarget(item);
      expect(target, isNotNull);
      expect(target!.sessionId, 's1');
    });

    test('多 thread + 本地 sessions 完整 + 唯一未读 → 返回该 session', () {
      final s1 = SessionModel(
        sessionId: 'p1',
        title: 'Alice',
        type: 'private',
        peerId: 'user-alice',
        peerType: 1,
        updatedAt: 1000,
        lastMessageTime: 1000,
        unreadCount: 0,
      );
      final s2 = SessionModel(
        sessionId: 'p2',
        title: 'Alice',
        type: 'private',
        peerId: 'user-alice',
        peerType: 1,
        updatedAt: 2000,
        lastMessageTime: 2000,
        unreadCount: 3,
      );
      final s3 = SessionModel(
        sessionId: 'p3',
        title: 'Alice',
        type: 'private',
        peerId: 'user-alice',
        peerType: 1,
        updatedAt: 900,
        lastMessageTime: 900,
        unreadCount: 0,
      );
      final item = ConversationListItem(
        groupKey: 'private:1:user-alice',
        latestSession: s2,
        sessions: [s1, s2, s3],
        unreadCount: 3,
        isPinned: true,
        pinnedAt: 100,
      );

      final target = controller.resolveDirectChatTarget(item);
      expect(target, isNotNull);
      expect(target!.sessionId, 'p2');
    });

    test('多 thread + 本地 sessions 完整 + 多个未读 → 返回 null（应弹窗）', () {
      final s1 = SessionModel(
        sessionId: 'p1',
        title: 'Alice',
        type: 'private',
        peerId: 'user-alice',
        peerType: 1,
        updatedAt: 1000,
        lastMessageTime: 1000,
        unreadCount: 2,
      );
      final s2 = SessionModel(
        sessionId: 'p2',
        title: 'Alice',
        type: 'private',
        peerId: 'user-alice',
        peerType: 1,
        updatedAt: 2000,
        lastMessageTime: 2000,
        unreadCount: 3,
      );
      final item = ConversationListItem(
        groupKey: 'private:1:user-alice',
        latestSession: s2,
        sessions: [s1, s2],
        unreadCount: 5,
        isPinned: true,
        pinnedAt: 100,
      );

      final target = controller.resolveDirectChatTarget(item);
      expect(target, isNull);
    });

    test('多 thread + 本地 sessions 完整 + 零未读 → 返回 null（应弹窗）', () {
      final s1 = SessionModel(
        sessionId: 'p1',
        title: 'Alice',
        type: 'private',
        peerId: 'user-alice',
        peerType: 1,
        updatedAt: 1000,
        lastMessageTime: 1000,
        unreadCount: 0,
      );
      final s2 = SessionModel(
        sessionId: 'p2',
        title: 'Alice',
        type: 'private',
        peerId: 'user-alice',
        peerType: 1,
        updatedAt: 2000,
        lastMessageTime: 2000,
        unreadCount: 0,
      );
      final item = ConversationListItem(
        groupKey: 'private:1:user-alice',
        latestSession: s2,
        sessions: [s1, s2],
        unreadCount: 0,
        isPinned: true,
        pinnedAt: 100,
      );

      final target = controller.resolveDirectChatTarget(item);
      expect(target, isNull);
    });

    test('summary 模式：item.sessions 不完整但 imService 有全量 → 唯一未读直达', () {
      // 模拟 summary 模式：item.sessions 只有 latestSession，threadCountOverride=3
      // 但 imService.sessions 有同 groupKey 的全部 3 个 session
      final s1 = SessionModel(
        sessionId: 'p1',
        title: 'Alice',
        type: 'private',
        peerId: 'user-alice',
        peerType: 1,
        updatedAt: 1000,
        lastMessageTime: 1000,
        unreadCount: 0,
      );
      final s2 = SessionModel(
        sessionId: 'p2',
        title: 'Alice',
        type: 'private',
        peerId: 'user-alice',
        peerType: 1,
        updatedAt: 2000,
        lastMessageTime: 2000,
        unreadCount: 5,
      );
      final s3 = SessionModel(
        sessionId: 'p3',
        title: 'Alice',
        type: 'private',
        peerId: 'user-alice',
        peerType: 1,
        updatedAt: 900,
        lastMessageTime: 900,
        unreadCount: 0,
      );

      // 把所有 session 加入 imService
      imService.sessions.assignAll([s1, s2, s3]);

      // item 只包含 latestSession（模拟 summary 模式）
      final item = ConversationListItem(
        groupKey: 'private:1:user-alice',
        latestSession: s2,
        sessions: [s2],
        unreadCount: 5,
        isPinned: true,
        pinnedAt: 100,
        threadCountOverride: 3,
      );

      final target = controller.resolveDirectChatTarget(item);
      expect(target, isNotNull);
      expect(target!.sessionId, 'p2');
    });

    test(
      'summary 模式：item.sessions 不完整且 imService 也不完整 → 返回 null（走 fallback）',
      () {
        // imService 只加载了 2 个 session，但 threadCount 是 3
        final s1 = SessionModel(
          sessionId: 'p1',
          title: 'Alice',
          type: 'private',
          peerId: 'user-alice',
          peerType: 1,
          updatedAt: 1000,
          lastMessageTime: 1000,
          unreadCount: 0,
        );
        final s2 = SessionModel(
          sessionId: 'p2',
          title: 'Alice',
          type: 'private',
          peerId: 'user-alice',
          peerType: 1,
          updatedAt: 2000,
          lastMessageTime: 2000,
          unreadCount: 5,
        );

        imService.sessions.assignAll([s1, s2]);

        final item = ConversationListItem(
          groupKey: 'private:1:user-alice',
          latestSession: s2,
          sessions: [s2],
          unreadCount: 5,
          isPinned: true,
          pinnedAt: 100,
          threadCountOverride: 3,
        );

        final target = controller.resolveDirectChatTarget(item);
        expect(target, isNull);
      },
    );

    test('private 会话 summary 模式：按 peerId 分组查找本地 sessions', () {
      final s1 = SessionModel(
        sessionId: 'p1',
        title: 'Alice',
        type: 'private',
        peerId: 'user-alice',
        peerType: 1,
        updatedAt: 1000,
        lastMessageTime: 1000,
        unreadCount: 0,
      );
      final s2 = SessionModel(
        sessionId: 'p2',
        title: 'Alice',
        type: 'private',
        peerId: 'user-alice',
        peerType: 1,
        updatedAt: 2000,
        lastMessageTime: 2000,
        unreadCount: 2,
      );

      imService.sessions.assignAll([s1, s2]);

      final item = ConversationListItem(
        groupKey: 'private:1:user-alice',
        latestSession: s2,
        sessions: [s2],
        unreadCount: 2,
        isPinned: true,
        pinnedAt: 100,
        threadCountOverride: 2,
      );

      final target = controller.resolveDirectChatTarget(item);
      expect(target, isNotNull);
      expect(target!.sessionId, 'p2');
    });
  });
}

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

  test('mergeApiThreadsWithLocalPreview 用本地 unread/预览覆盖 API', () {
    final api = [
      SessionModel(
        sessionId: 't1',
        title: 'API',
        type: 'private',
        peerId: 'u1',
        peerType: 1,
        updatedAt: 200,
        unreadCount: 9,
        lastMessage: '',
        lastMessageTime: 0,
      ),
    ];
    final local = [
      SessionModel(
        sessionId: 't1',
        title: 'Local',
        type: 'private',
        peerId: 'u1',
        peerType: 1,
        updatedAt: 100,
        unreadCount: 2,
        lastMessage: 'hi',
        lastMessageTime: 150,
      ),
    ];

    final merged = ConversationsController.mergeApiThreadsWithLocalPreview(
      apiThreads: api,
      localSessions: local,
    );

    expect(merged, hasLength(1));
    expect(merged.first.unreadCount, 2);
    expect(merged.first.lastMessage, 'hi');
    expect(merged.first.title, 'API');
  });

  test('latestSession clearUnread override 不能把同组其它 thread 未读清掉', () {
    final latest = SessionModel(
      sessionId: 'latest',
      title: 'Alice',
      type: 'private',
      peerId: '2001',
      peerType: 1,
      updatedAt: 2000,
      unreadCount: 0,
      lastMessageTime: 2000,
    );
    final older = SessionModel(
      sessionId: 'older',
      title: 'Alice',
      type: 'private',
      peerId: '2001',
      peerType: 1,
      updatedAt: 1000,
      unreadCount: 4,
      lastMessageTime: 1000,
    );
    imService.sessions.assignAll([latest, older]);
    imService.clearUnread('latest');

    // 服务端摘要仍为 7（含已读的 latest）；本地 older=4。
    // 旧 bug：latest override=0 会把整组变成 0。
    // 新口径：per-session 汇总后再与摘要取 max → 至少保留 older 的 4，且不被清成 0。
    final itemWithServerFloor = ConversationListItem(
      groupKey: 'private:1:2001',
      latestSession: latest,
      sessions: [latest],
      unreadCount: 7,
      badgeUnreadCount: 7,
      isPinned: false,
      pinnedAt: 0,
      threadCountOverride: 2,
    );
    final withFloor = controller.effectiveUnreadForConversationItemForTest(
      item: itemWithServerFloor,
      localSessions: [latest, older],
    );
    expect(withFloor.unread, 7);
    expect(withFloor.badge, 7);

    final itemNoFloor = ConversationListItem(
      groupKey: 'private:1:2001',
      latestSession: latest,
      sessions: [latest],
      unreadCount: 0,
      badgeUnreadCount: 0,
      isPinned: false,
      pinnedAt: 0,
      threadCountOverride: 2,
    );
    final noFloor = controller.effectiveUnreadForConversationItemForTest(
      item: itemNoFloor,
      localSessions: [latest, older],
    );
    expect(noFloor.unread, 4);
    expect(noFloor.badge, 4);
  });

  test('组内全部 session 都有 clear override 时允许整组低于服务端下界', () {
    final a = SessionModel(
      sessionId: 'a',
      title: 'Alice',
      type: 'private',
      peerId: '2001',
      peerType: 1,
      updatedAt: 2000,
      unreadCount: 0,
      lastMessageTime: 2000,
    );
    final b = SessionModel(
      sessionId: 'b',
      title: 'Alice',
      type: 'private',
      peerId: '2001',
      peerType: 1,
      updatedAt: 1000,
      unreadCount: 0,
      lastMessageTime: 1000,
    );
    imService.sessions.assignAll([a, b]);
    imService.clearUnread('a');
    imService.clearUnread('b');

    final item = ConversationListItem(
      groupKey: 'private:1:2001',
      latestSession: a,
      sessions: [a],
      unreadCount: 9,
      badgeUnreadCount: 9,
      isPinned: false,
      pinnedAt: 0,
      threadCountOverride: 2,
    );

    final effective = controller.effectiveUnreadForConversationItemForTest(
      item: item,
      localSessions: [a, b],
    );

    expect(effective.unread, 0);
    expect(effective.badge, 0);
  });
}

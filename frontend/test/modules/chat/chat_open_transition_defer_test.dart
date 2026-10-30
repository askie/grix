import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/modules/home/controllers/conversations_controller.dart';
import 'package:grix/modules/home/services/friend_qr_flow_service.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';

import 'history_test_harness.dart';

class _QuietConversationsController extends ConversationsController {
  @override
  Future<void> reloadFavoriteIds() async {}
}

void main() {
  const sid = 'open-defer-session';

  SessionModel session() => SessionModel(
    sessionId: sid,
    type: 'private',
    peerId: 'peer-defer',
    peerType: 1,
    peerNickname: 'Ada',
    updatedAt: 1000,
    lastMessageTime: 1000,
    unreadCount: 4,
  );

  Future<({ImService im, ConversationsController conversations})> prepare(
    WidgetTester tester,
  ) async {
    final im = await initOffline(tester, 'open-defer');
    UserImageCacheManager.setDisabledForTest(true);
    addTearDown(() => UserImageCacheManager.setDisabledForTest(false));
    await runDb(
      tester,
      () =>
          LocalDb.batchInsertMessages(List.generate(4, (i) => row(sid, i + 1))),
    );
    im.sessions.assignAll([session()]);
    Get.put<FriendQrFlowService>(FriendQrFlowService());
    final conversations = Get.put<ConversationsController>(
      _QuietConversationsController(),
    );
    return (im: im, conversations: conversations);
  }

  int unreadOf(ImService im) =>
      im.sessions.firstWhere((item) => item.sessionId == sid).unreadCount;

  testWidgets('conversation list push defers regroup and clearUnread until the '
      'frame after the transition, while messages still publish', (
    tester,
  ) async {
    final setup = await prepare(tester);
    final im = setup.im;
    final conversations = setup.conversations;
    final baseline = conversations.immediateRebuildCountForTest;
    expect(im.unreadOverrideForSession(sid), isNull);
    expect(
      conversations.findConversationItemBySession(sid)?.badgeUnreadCount,
      4,
    );

    final controller = await openRoute(tester, sid);
    var gate = false;
    controller.routeTransitionSettled.then((_) => gate = true);
    final route = ModalRoute.of(
      tester.element(find.byType(ChatView, skipOffstage: false)),
    )!;

    expect(im.currentSessionId, sid);
    expect(im.unreadOverrideForSession(sid), isNull);
    expect(unreadOf(im), 4);
    expect(conversations.immediateRebuildCountForTest, baseline);
    expect(
      conversations.findConversationItemBySession(sid)?.badgeUnreadCount,
      4,
    );

    if (im.currentMessages.isEmpty) {
      await flushDb(tester, () => im.currentMessages.isNotEmpty);
    }
    await tester.pump(const Duration(milliseconds: 100));
    expect(route.animation!.status, AnimationStatus.forward);
    expect(gate, isFalse);
    expect(im.currentSessionId, sid);
    expect(im.currentMessages, isNotEmpty);
    expect(im.unreadOverrideForSession(sid), isNull);
    expect(unreadOf(im), 4);
    expect(conversations.immediateRebuildCountForTest, baseline);

    await tester.pump(const Duration(milliseconds: 200));
    expect(gate, isTrue);
    expect(im.unreadOverrideForSession(sid), isNull);
    expect(unreadOf(im), 4);
    expect(conversations.immediateRebuildCountForTest, baseline);
    expect(
      conversations.findConversationItemBySession(sid)?.badgeUnreadCount,
      4,
    );

    await tester.pump();
    expect(im.unreadOverrideForSession(sid), 0);
    expect(unreadOf(im), 0);
    expect(conversations.immediateRebuildCountForTest, baseline + 1);
    expect(
      conversations.findConversationItemBySession(sid)?.badgeUnreadCount,
      0,
    );
    await closeOffline(tester);
  });

  testWidgets(
    'zero-duration open clears unread and regroups on the opening frame',
    (tester) async {
      final setup = await prepare(tester);
      final im = setup.im;
      final conversations = setup.conversations;
      final baseline = conversations.immediateRebuildCountForTest;

      await openRoute(tester, sid, zero: true);
      final route = ModalRoute.of(
        tester.element(find.byType(ChatView, skipOffstage: false)),
      )!;
      expect(route.transitionDuration, Duration.zero);
      expect(im.currentSessionId, sid);
      expect(im.unreadOverrideForSession(sid), 0);
      expect(unreadOf(im), 0);
      expect(conversations.immediateRebuildCountForTest, baseline + 1);
      expect(
        conversations.findConversationItemBySession(sid)?.badgeUnreadCount,
        0,
      );
      await closeOffline(tester);
    },
  );
}

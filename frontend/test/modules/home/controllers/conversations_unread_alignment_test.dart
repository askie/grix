import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/home/controllers/conversations_controller.dart';
import 'package:grix/modules/home/conversations_view.dart';
import 'package:grix/modules/home/services/friend_qr_flow_service.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _LocalConversationsController extends ConversationsController {
  @override
  Future<void> reloadFavoriteIds() async {}
}

class _LocalImService extends ImService {
  @override
  Future<void> loadSessions({
    bool refreshFromServer = true,
    bool backfillMissingPeerIdentities = true,
  }) async {}
}

SessionModel _thread(String id, int unread, {String peer = 'old-peer'}) =>
    SessionModel(
      sessionId: id,
      type: 'private',
      peerId: peer,
      peerType: 2,
      peerNickname: 'Agent',
      updatedAt: 1000,
      lastMessageTime: 1000,
      unreadCount: unread,
    );

ConversationListItem _summary(
  String key,
  SessionModel latest, {
  int unread = 0,
}) => ConversationListItem(
  groupKey: key,
  latestSession: latest,
  sessions: [latest],
  unreadCount: unread,
  badgeUnreadCount: unread,
  isPinned: false,
  pinnedAt: 0,
  threadCountOverride: 2,
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late _LocalImService im;
  late ConversationsController controller;
  const key = 'private:2:agent';

  int displayedUnread() => controller.groupedSessions.fold(
    0,
    (sum, row) => sum + row.badgeUnreadCount,
  );

  void seedIdentityMismatch() {
    // Complete but stale local identity bypasses the peerless backfill.
    im.sessions.assignAll([_thread('latest', 2), _thread('older', 3)]);
    controller.seedConversationSummaryItemsForTest([
      _summary(key, _thread('latest', 0, peer: 'agent')),
    ]);
    controller.applyConversationSummaryItemsForTest();
  }

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    UserImageCacheManager.setDisabledForTest(true);
    im = _LocalImService();
    Get.put<ImService>(im);
    Get.put<FriendQrFlowService>(FriendQrFlowService());
    controller = Get.put<ConversationsController>(
      _LocalConversationsController(),
    );
  });

  tearDown(() {
    Get.reset();
    UserImageCacheManager.setDisabledForTest(false);
  });

  testWidgets(
    'local projection publishes unread during continuous session updates',
    (tester) async {
      im.sessions.assignAll([_thread('latest', 0)]);
      await tester.pump(const Duration(milliseconds: 150));
      final commits = controller.groupedSessionsCommitCount;
      // The default local projection must not wait forever for a quiet interval.
      for (var tick = 0; tick < 20; tick++) {
        im.sessions[0] = _thread('latest', 5).copyWith(updatedAt: 1000 + tick);
        await tester.pump(const Duration(milliseconds: 50));
      }
      debugPrint(
        'local continuous updates: tab=${im.notificationUnread}, rows=${displayedUnread()}, commits=${controller.groupedSessionsCommitCount - commits}',
      );
      expect(im.notificationUnread, 5);
      expect(
        displayedUnread(),
        5,
        reason: 'continuous updates starved the unread row',
      );
      expect(
        controller.groupedSessionsCommitCount - commits,
        inInclusiveRange(1, 10),
      );
      await tester.pump(const Duration(milliseconds: 150));
    },
  );

  test('T1 session-id match assigns the entire local thread group', () {
    seedIdentityMismatch();
    final row = controller.groupedSessions.firstWhere((r) => r.groupKey == key);
    expect(row.badgeUnreadCount, 5);
    expect(controller.groupedSessions, hasLength(1));
  });

  test('T2 every local unread contributes to exactly one displayed badge', () {
    seedIdentityMismatch();
    expect(im.notificationUnread, 5);
    expect(displayedUnread(), im.notificationUnread);
  });

  test(
    'reconciled unread threads remain reachable and can all be marked read',
    () async {
      seedIdentityMismatch();
      final row = controller.groupedSessions.single;
      final threads = await controller.fetchConversationThreadSessions(row);
      expect(
        threads.sessions.map((s) => s.sessionId),
        unorderedEquals(['latest', 'older']),
      );
      expect(controller.findConversationItemBySession('older')?.groupKey, key);
      controller.markSessionGroupRead(row);
      expect(im.notificationUnread, 0);
    },
  );

  test(
    'thread navigation includes mapped members alongside canonical local threads',
    () async {
      im.sessions.assignAll([
        _thread('latest', 2),
        _thread('canonical', 3, peer: 'agent'),
      ]);
      controller.seedConversationSummaryItemsForTest([
        _summary(key, _thread('latest', 0, peer: 'agent')),
      ]);
      controller.applyConversationSummaryItemsForTest();
      final row = controller.groupedSessions.single;
      expect(row.badgeUnreadCount, 5);
      final threads = await controller.fetchConversationThreadSessions(row);
      expect(
        threads.sessions.map((s) => s.sessionId),
        unorderedEquals(['latest', 'canonical']),
      );
    },
  );

  test(
    'T3 page-visible reconciliation converges instead of rebuilding each tick',
    () async {
      seedIdentityMismatch();
      final before = controller.immediateRebuildCountForTest;
      for (var tick = 0; tick < 10; tick++) {
        await controller.refreshSessionsOnPageVisible();
      }
      final calls = controller.immediateRebuildCountForTest - before;
      debugPrint('T3: 10 page-visible ticks, immediate rebuild calls=$calls');
      expect(calls, lessThanOrEqualTo(1));
      expect(displayedUnread(), im.notificationUnread);
    },
  );

  test('explicit session matches do not merge ambiguous local groups', () {
    im.sessions.assignAll([
      _thread('a', 2),
      _thread('b', 3),
      _thread('unclaimed', 4),
    ]);
    controller.seedConversationSummaryItemsForTest([
      _summary('private:2:a', _thread('a', 0, peer: 'a')),
      _summary('private:2:b', _thread('b', 0, peer: 'b')),
    ]);
    controller.applyConversationSummaryItemsForTest();
    expect(controller.groupedSessions, hasLength(3));
    expect(displayedUnread(), im.notificationUnread);
    expect(
      controller.groupedSessions
          .firstWhere((r) => r.groupKey == 'private:2:old-peer')
          .badgeUnreadCount,
      4,
    );
  });

  test('existing canonical summary keeps its remaining local threads', () {
    im.sessions.assignAll([_thread('a', 2), _thread('b', 3)]);
    controller.seedConversationSummaryItemsForTest([
      _summary('private:2:old-peer', _thread('b', 0)),
      _summary(key, _thread('a', 0, peer: 'agent')),
    ]);
    controller.applyConversationSummaryItemsForTest();
    expect(controller.groupedSessions, hasLength(2));
    expect(displayedUnread(), im.notificationUnread);
    expect(
      controller.groupedSessions
          .firstWhere((r) => r.groupKey == key)
          .badgeUnreadCount,
      2,
    );
  });

  testWidgets(
    'T4 search owns the displayed subset; clearing restores unread alignment',
    (tester) async {
      seedIdentityMismatch();
      controller.searchSessionRecordsOverrideForTest = (_) async => [];
      controller.searchMessagesOverrideForTest = (_) async => [];
      controller.updateSearchQuery('no match');
      await tester.pump(const Duration(milliseconds: 250));
      expect(controller.groupedSessions, isEmpty);
      final before = controller.immediateRebuildCountForTest;
      await controller.refreshSessionsOnPageVisible();
      expect(controller.immediateRebuildCountForTest, before);
      expect(controller.groupedSessions, isEmpty);
      controller.updateSearchQuery('');
      expect(displayedUnread(), im.notificationUnread);
      await tester.pump(const Duration(seconds: 1));
    },
  );

  testWidgets(
    'conversation view renders the whole-group badge after identity reconciliation',
    (tester) async {
      seedIdentityMismatch();
      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          home: const ConversationsView(),
        ),
      );
      await tester.pump(const Duration(seconds: 1));
      expect(find.text('5'), findsOneWidget);
      expect(controller.groupedSessions, hasLength(1));
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(seconds: 1));
    },
  );
}

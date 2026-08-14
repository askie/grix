import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/models/conversation_summary_model.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/private_chat_creating_view.dart';
import 'package:grix/modules/home/controllers/contacts_controller.dart';
import 'package:grix/modules/home/controllers/conversations_controller.dart';
import 'package:grix/modules/home/conversations_view.dart';
import 'package:grix/modules/home/widgets/conversation_reorder_sliver_list.dart';
import 'package:grix/modules/home/services/friend_qr_flow_service.dart';
import 'package:grix/shared/models/session_avatar_member.dart';
import 'package:grix/shared/utils/chat_draft_index.dart';
import 'package:grix/shared/widgets/session_avatar.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeImService extends ImService {
  final RxBool _connected = true.obs;

  @override
  bool get isConnected => _connected.value;

  @override
  void connect(String wsUrl) {}

  @override
  void enterSession(
    String sessionId, {
    Duration initialLoadDelay = Duration.zero,
  }) {}

  @override
  void leaveSession([String? explicitSessionId]) {}

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

  @override
  Future<void> reconcilePinsFromConversationSummaries(
    List<ConversationSummaryModel> items, {
    required bool hasMore,
  }) async {}
}

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';

  @override
  String? get token => 'test_access_token';

  @override
  bool hasUsableAccessToken({Duration minRemaining = Duration.zero}) => true;

  @override
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = const Duration(minutes: 5),
  }) async => TokenRefreshStatus.ready;
}

class _FakeFriendService extends FriendService {
  @override
  Future<void> loadFriendList() async {}

  @override
  Future<void> loadFriendRequests() async {}

  @override
  Future<String?> fetchUserProfile(String userId) async => null;
}

class _FakeUnreadMentionConversationsController
    extends ConversationsController {
  _FakeUnreadMentionConversationsController(this._items);

  final List<ConversationListItem> _items;
  final RxInt _tick = 0.obs;

  @override
  List<ConversationListItem> get groupedSessions {
    _tick.value;
    return List<ConversationListItem>.unmodifiable(_items);
  }

  @override
  bool get hasUnfilteredSessions {
    _tick.value;
    return _items.isNotEmpty;
  }

  @override
  void updateSearchQuery(String query) {}

  @override
  Future<void> openUserQrScanner() async {}

  @override
  Future<void> loadMoreSessionsForVisibleListIfNeeded() async {}

  @override
  String getAvatarTitle(ConversationListItem item) => item.latestSession.title;

  @override
  String getConversationSecondaryText(ConversationListItem item) =>
      item.latestSession.lastMessage;

  @override
  void watchConversationAvatar(ConversationListItem item) {
    _tick.value;
  }

  @override
  bool canOpenAccountInfo(ConversationListItem item) => false;

  @override
  bool isGroupConversation(ConversationListItem item) =>
      item.latestSession.type != 'private';

  @override
  String getAvatarSeed(ConversationListItem item) => item.groupKey;

  @override
  String getConversationAvatarUrl(ConversationListItem item) => '';

  @override
  List<SessionAvatarMember> getConversationAvatarMembers(
    ConversationListItem item,
  ) => const <SessionAvatarMember>[];

  @override
  void handleConversationTap(BuildContext context, ConversationListItem item) {}

  @override
  void showSessionMenu(BuildContext context, ConversationListItem item) {}

  @override
  String getConversationListTitle(ConversationListItem item) {
    _tick.value;
    return item.latestSession.title;
  }

  @override
  String formatTime(int timestamp) => '09:41';
}

class _FakeAgentService extends AgentService {
  @override
  Future<void> loadAgents({String? categoryId}) async {}
}

class _FakeOssService extends OssService {}

class _FakeSessionService extends SessionService {
  int createCalls = 0;
  String? lastPeerId;
  int? lastPeerType;
  Future<void>? createDelay;

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    return const SessionDetailResult(data: null);
  }

  @override
  Future<String?> createSession(String peerId, int peerType) async {
    createCalls++;
    lastPeerId = peerId;
    lastPeerType = peerType;
    final delay = createDelay;
    if (delay != null) {
      await delay;
    }
    return 'fresh-session-created';
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late _FakeImService imService;
  late _FakeSessionService sessionService;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    ChatDraftIndex.resetForTest();
    SharedPreferences.setMockInitialValues({});
    imService = _FakeImService();
    sessionService = _FakeSessionService();
    Get.put<ImService>(imService);
    Get.put<AuthService>(_FakeAuthService());
    Get.put<FriendService>(_FakeFriendService());
    Get.put<SessionService>(sessionService);
    Get.put<OssService>(_FakeOssService());
    Get.put<FriendQrFlowService>(FriendQrFlowService());
    Get.put(ContactsController());
    Get.put(ConversationsController());
  });

  tearDown(() {
    ChatDraftIndex.resetForTest();
    Get.reset();
  });

  Widget buildApp() {
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('en', 'US'),
      fallbackLocale: const Locale('en', 'US'),
      getPages: [
        GetPage(
          name: AppRoutes.privateChatCreating,
          page: () => const PrivateChatCreatingView(),
          // Keep test timing deterministic; production still uses the 250ms delay
          // gate inside ChatRouteNavigator before replacing with chat.
          transition: Transition.noTransition,
        ),
        GetPage(
          name: AppRoutes.chat,
          page: () => const Scaffold(body: Text('chat-page')),
          transition: Transition.noTransition,
        ),
      ],
      home: const ConversationsView(),
    );
  }

  testWidgets('messages page replaces online status with quick action menu', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    expect(find.text('Online'), findsNothing);
    expect(find.byIcon(Icons.add_rounded), findsOneWidget);

    await tester.tap(find.byIcon(Icons.add_rounded));
    await tester.pumpAndSettle();

    expect(find.text('Add Friend'), findsOneWidget);
    expect(find.text('New Group Chat'), findsOneWidget);
    expect(find.text('conversations_scan_user_qr'.tr), findsOneWidget);
    expect(find.text('My QR Code'), findsNothing);
  });

  testWidgets('messages quick action menu opens shared add-friend dialog', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.add_rounded));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Add Friend'));
    await tester.pumpAndSettle();

    expect(find.text('Search by username...'), findsOneWidget);
  });

  testWidgets('messages quick action menu opens shared new-group dialog', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.add_rounded));
    await tester.pumpAndSettle();
    await tester.tap(find.text('New Group Chat'));
    await tester.pumpAndSettle();

    expect(find.text('Group chat name'), findsOneWidget);
  });

  testWidgets(
    'muted conversation hides numeric badge and shows unread marker',
    (WidgetTester tester) async {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'muted-session-1',
          title: 'Muted Group',
          type: 'group',
          unreadCount: 8,
          isMuted: true,
          updatedAt: now,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
      ]);

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();
      await tester.pump(const Duration(milliseconds: 200));

      expect(find.text('8'), findsNothing);
      expect(
        find.byWidgetPredicate((widget) {
          if (widget is! Container ||
              widget.color != AppTheme.unreadBadgeColor) {
            return false;
          }
          final constraints = widget.constraints;
          if (constraints == null) {
            return false;
          }
          return constraints.minWidth == 6 &&
              constraints.maxWidth == 6 &&
              constraints.minHeight == 6 &&
              constraints.maxHeight == 6;
        }),
        findsOneWidget,
      );
    },
  );

  testWidgets('unmuted conversation shows numeric unread badge', (
    WidgetTester tester,
  ) async {
    final now = DateTime.now().millisecondsSinceEpoch;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'active-session-1',
        title: 'Active Group',
        type: 'group',
        unreadCount: 8,
        isMuted: false,
        updatedAt: now,
        lastMessage: 'hello',
        lastMessageTime: now,
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();
    await tester.pump(const Duration(milliseconds: 200));

    expect(find.text('8'), findsOneWidget);
  });

  testWidgets('conversation with unread mention shows mention badge', (
    WidgetTester tester,
  ) async {
    final now = DateTime.now().millisecondsSinceEpoch;
    Get.delete<ConversationsController>(force: true);
    Get.put<ConversationsController>(
      _FakeUnreadMentionConversationsController([
        ConversationListItem(
          groupKey: 'group:mention',
          latestSession: SessionModel(
            sessionId: 'mention-session-view',
            title: 'Mention Group',
            type: 'group',
            unreadCount: 1,
            updatedAt: now,
            lastMessage: 'ping',
            lastMessageTime: now,
          ),
          sessions: [
            SessionModel(
              sessionId: 'mention-session-view',
              title: 'Mention Group',
              type: 'group',
              unreadCount: 1,
              updatedAt: now,
              lastMessage: 'ping',
              lastMessageTime: now,
            ),
          ],
          unreadCount: 1,
          hasUnreadMention: true,
          badgeUnreadCount: 1,
          isPinned: false,
          pinnedAt: 0,
        ),
      ]),
    );

    await tester.pumpWidget(buildApp());
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('@You'), findsOneWidget);
    expect(find.text('1'), findsOneWidget);
  });

  testWidgets(
    'conversation with unsent draft shows draft badge without row highlight',
    (WidgetTester tester) async {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'draft-session-1',
          title: 'Draft Group',
          type: 'group',
          unreadCount: 0,
          updatedAt: now,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
      ]);

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();
      expect(find.text('Draft'), findsNothing);

      // 输入框侧登记草稿后，列表行应立即出现"Draft"标记
      ChatDraftIndex.update(sessionId: 'draft-session-1', hasDraft: true);
      await tester.pump();
      expect(find.text('Draft'), findsOneWidget);

      // 草稿不参与行高亮：行容器不应带 @提及 的警示底色
      final rowContainer = tester.widget<Container>(
        find
            .ancestor(
              of: find.text('Draft Group'),
              matching: find.byWidgetPredicate(
                (w) => w is Container && w.decoration is BoxDecoration,
              ),
            )
            .first,
      );
      final decoration = rowContainer.decoration as BoxDecoration?;
      expect(
        decoration?.color,
        isNot(AppTheme.warningColor.withValues(alpha: 0.08)),
      );

      // 草稿清空（发送/删空）后标记消失
      ChatDraftIndex.update(sessionId: 'draft-session-1', hasDraft: false);
      await tester.pump();
      expect(find.text('Draft'), findsNothing);
    },
  );

  testWidgets(
    'private conversation without resolved peer id builds without Obx misuse',
    (WidgetTester tester) async {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'private-missing-peer-1',
          title: 'Pending Identity',
          type: 'private',
          updatedAt: now,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
      ]);

      await tester.pumpWidget(buildApp());
      await tester.pump(const Duration(milliseconds: 100));

      expect(tester.takeException(), isNull);
      expect(find.byType(SessionAvatar), findsOneWidget);
    },
  );

  testWidgets(
    'disposing conversations controller does not crash active avatar observers',
    (WidgetTester tester) async {
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'group-observer-1',
          title: 'Teardown Group',
          type: 'group',
          updatedAt: now,
          lastMessage: 'bye',
          lastMessageTime: now,
        ),
      ]);

      await tester.pumpWidget(buildApp());
      await tester.pump();

      expect(find.byType(SessionAvatar), findsOneWidget);

      await Get.delete<ConversationsController>(force: true);
      await tester.pump();
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();

      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'conversation avatar state is preserved when list order changes',
    (WidgetTester tester) async {
      const aliceSessionId = 'session-alice';
      const alicePeerId = '2001';
      const bobSessionId = 'session-bob';
      const bobPeerId = '2002';
      const aliceGroupKey = 'private:1:$alicePeerId';
      final now = DateTime.now().millisecondsSinceEpoch;

      SessionModel buildPrivateSession({
        required String sessionId,
        required String peerId,
        required String peerNickname,
        required int updatedAt,
        required String lastMessage,
      }) {
        return SessionModel(
          sessionId: sessionId,
          type: 'private',
          peerId: peerId,
          peerType: 1,
          peerNickname: peerNickname,
          updatedAt: updatedAt,
          lastMessage: lastMessage,
          lastMessageTime: updatedAt,
        );
      }

      imService.sessions.assignAll([
        buildPrivateSession(
          sessionId: aliceSessionId,
          peerId: alicePeerId,
          peerNickname: 'Alice',
          updatedAt: now - 1000,
          lastMessage: 'older',
        ),
        buildPrivateSession(
          sessionId: bobSessionId,
          peerId: bobPeerId,
          peerNickname: 'Bob',
          updatedAt: now,
          lastMessage: 'newer',
        ),
      ]);

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      final aliceTileFinder = find.byKey(
        ConversationsView.sessionTileKey(aliceGroupKey),
      );
      final aliceAvatarFinder = find.descendant(
        of: aliceTileFinder,
        matching: find.byType(SessionAvatar),
      );
      final State<StatefulWidget> aliceAvatarStateBefore = tester.state(
        aliceAvatarFinder,
      );

      imService.sessions.assignAll([
        buildPrivateSession(
          sessionId: aliceSessionId,
          peerId: alicePeerId,
          peerNickname: 'Alice',
          updatedAt: now + 1000,
          lastMessage: 'latest',
        ),
        buildPrivateSession(
          sessionId: bobSessionId,
          peerId: bobPeerId,
          peerNickname: 'Bob',
          updatedAt: now,
          lastMessage: 'newer',
        ),
      ]);

      await tester.pump(const Duration(milliseconds: 100));
      await tester.pumpAndSettle();

      final State<StatefulWidget> aliceAvatarStateAfter = tester.state(
        aliceAvatarFinder,
      );

      expect(identical(aliceAvatarStateBefore, aliceAvatarStateAfter), isTrue);
    },
  );

  testWidgets(
    'conversation moves smoothly toward the first slot when a newer message arrives',
    (WidgetTester tester) async {
      const aliceSessionId = 'session-alice-animated';
      const alicePeerId = '3001';
      const bobSessionId = 'session-bob-animated';
      const aliceGroupKey = 'private:1:$alicePeerId';
      final now = DateTime.now().millisecondsSinceEpoch;

      SessionModel buildPrivateSession({
        required String sessionId,
        required String peerId,
        required String peerNickname,
        required int updatedAt,
        required String lastMessage,
      }) {
        return SessionModel(
          sessionId: sessionId,
          type: 'private',
          peerId: peerId,
          peerType: 1,
          peerNickname: peerNickname,
          updatedAt: updatedAt,
          lastMessage: lastMessage,
          lastMessageTime: updatedAt,
        );
      }

      imService.sessions.assignAll([
        buildPrivateSession(
          sessionId: aliceSessionId,
          peerId: alicePeerId,
          peerNickname: 'Alice',
          updatedAt: now - 1000,
          lastMessage: 'older',
        ),
        buildPrivateSession(
          sessionId: bobSessionId,
          peerId: '3002',
          peerNickname: 'Bob',
          updatedAt: now,
          lastMessage: 'newer',
        ),
      ]);

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      final aliceTileFinder = find.byKey(
        ConversationsView.sessionTileKey(aliceGroupKey),
      );
      final aliceMoveFinder = find.byKey(
        ConversationReorderSliverList.moveKey(aliceGroupKey),
      );

      final initialAliceY = tester.getTopLeft(aliceTileFinder).dy;

      imService.sessions.assignAll([
        buildPrivateSession(
          sessionId: aliceSessionId,
          peerId: alicePeerId,
          peerNickname: 'Alice',
          updatedAt: now + 1000,
          lastMessage: 'latest',
        ),
        buildPrivateSession(
          sessionId: bobSessionId,
          peerId: '3002',
          peerNickname: 'Bob',
          updatedAt: now,
          lastMessage: 'newer',
        ),
      ]);

      await tester.pump(const Duration(milliseconds: 130));
      final animatedAliceOffsetY = tester
          .widget<Transform>(aliceMoveFinder)
          .transform
          .storage[13];
      expect(animatedAliceOffsetY, greaterThan(0));

      await tester.pump(const Duration(milliseconds: 80));
      final midAliceOffsetY = tester
          .widget<Transform>(aliceMoveFinder)
          .transform
          .storage[13];
      expect(midAliceOffsetY, lessThan(animatedAliceOffsetY));

      await tester.pumpAndSettle();
      final settledAliceY = tester.getTopLeft(aliceTileFinder).dy;
      final settledAliceOffsetY = tester
          .widget<Transform>(aliceMoveFinder)
          .transform
          .storage[13];

      expect(settledAliceY, lessThan(initialAliceY));
      expect(settledAliceOffsetY.abs(), lessThan(0.01));
    },
  );

  testWidgets(
    'conversation reorder stays continuous when rank changes again mid-animation',
    (WidgetTester tester) async {
      const aliceSessionId = 'session-alice-continuous';
      const alicePeerId = '4001';
      const bobSessionId = 'session-bob-continuous';
      const bobPeerId = '4002';
      const aliceGroupKey = 'private:1:$alicePeerId';
      final now = DateTime.now().millisecondsSinceEpoch;

      SessionModel buildPrivateSession({
        required String sessionId,
        required String peerId,
        required String peerNickname,
        required int updatedAt,
      }) {
        return SessionModel(
          sessionId: sessionId,
          type: 'private',
          peerId: peerId,
          peerType: 1,
          peerNickname: peerNickname,
          updatedAt: updatedAt,
          lastMessage: '$peerNickname message',
          lastMessageTime: updatedAt,
        );
      }

      imService.sessions.assignAll([
        buildPrivateSession(
          sessionId: aliceSessionId,
          peerId: alicePeerId,
          peerNickname: 'Alice',
          updatedAt: now - 1000,
        ),
        buildPrivateSession(
          sessionId: bobSessionId,
          peerId: bobPeerId,
          peerNickname: 'Bob',
          updatedAt: now,
        ),
      ]);

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      final aliceTileFinder = find.byKey(
        ConversationsView.sessionTileKey(aliceGroupKey),
      );
      final aliceMoveFinder = find.byKey(
        ConversationReorderSliverList.moveKey(aliceGroupKey),
      );
      final bobTileFinder = find.byKey(
        ConversationsView.sessionTileKey('private:1:$bobPeerId'),
      );

      final initialAliceY = tester.getTopLeft(aliceTileFinder).dy;
      final initialBobY = tester.getTopLeft(bobTileFinder).dy;
      final swapDistance = initialAliceY - initialBobY;
      expect(swapDistance, greaterThan(0));

      imService.sessions.assignAll([
        buildPrivateSession(
          sessionId: aliceSessionId,
          peerId: alicePeerId,
          peerNickname: 'Alice',
          updatedAt: now + 1000,
        ),
        buildPrivateSession(
          sessionId: bobSessionId,
          peerId: bobPeerId,
          peerNickname: 'Bob',
          updatedAt: now,
        ),
      ]);

      await tester.pump(const Duration(milliseconds: 130));
      final firstLegOffset = tester
          .widget<Transform>(aliceMoveFinder)
          .transform
          .storage[13];
      expect(firstLegOffset, greaterThan(0));

      imService.sessions.assignAll([
        buildPrivateSession(
          sessionId: aliceSessionId,
          peerId: alicePeerId,
          peerNickname: 'Alice',
          updatedAt: now + 1000,
        ),
        buildPrivateSession(
          sessionId: bobSessionId,
          peerId: bobPeerId,
          peerNickname: 'Bob',
          updatedAt: now + 2000,
        ),
      ]);

      await tester.pump(const Duration(milliseconds: 130));
      final secondLegOffset = tester
          .widget<Transform>(aliceMoveFinder)
          .transform
          .storage[13];
      expect(secondLegOffset, lessThan(0));
      expect(secondLegOffset, greaterThan(-swapDistance + 1));

      await tester.pumpAndSettle();
      final settledAliceOffset = tester
          .widget<Transform>(aliceMoveFinder)
          .transform
          .storage[13];
      expect(settledAliceOffset.abs(), lessThan(0.01));
      expect(
        tester.getTopLeft(aliceTileFinder).dy,
        closeTo(initialAliceY, 0.1),
      );
    },
  );

  testWidgets('thread popup header shows + button and opens a fresh session', (
    WidgetTester tester,
  ) async {
    Get.put<AgentService>(_FakeAgentService());
    final now = DateTime.now().millisecondsSinceEpoch;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'private-session-1-main',
        title: 'Alice Main',
        type: 'private',
        peerId: '2001',
        peerType: 1,
        peerNickname: 'Alice',
        updatedAt: now,
        lastMessage: 'hello',
        lastMessageTime: now,
      ),
      SessionModel(
        sessionId: 'private-session-1-side',
        title: 'Alice Side',
        type: 'private',
        peerId: '2001',
        peerType: 1,
        peerNickname: 'Alice',
        updatedAt: now - 1000,
        lastMessage: 'older',
        lastMessageTime: now - 1000,
      ),
    ]);

    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    // '+' should only be in the thread popup header, not on the row itself.
    expect(find.byTooltip('New session'), findsNothing);

    await tester.tap(find.text('Alice'));
    await tester.pumpAndSettle();

    final plusButton = find.byTooltip('New session');
    expect(plusButton, findsOneWidget);

    await tester.tap(plusButton);
    await tester.pump();
    // Navigator waits defaultPageTransitionMilliseconds before chat replace.
    await tester.pump(
      const Duration(milliseconds: AppRoutes.defaultPageTransitionMilliseconds),
    );
    await tester.pumpAndSettle();

    expect(sessionService.createCalls, 1);
    expect(sessionService.lastPeerId, '2001');
    expect(sessionService.lastPeerType, 1);
    expect(Get.currentRoute, startsWith(AppRoutes.chat));
  });

  testWidgets(
    'thread popup + opens creating page while createSession is in flight',
    (WidgetTester tester) async {
      Get.put<AgentService>(_FakeAgentService());
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'private-session-1-main',
          title: 'Alice Main',
          type: 'private',
          peerId: '2001',
          peerType: 1,
          peerNickname: 'Alice',
          updatedAt: now,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
        SessionModel(
          sessionId: 'private-session-1-side',
          title: 'Alice Side',
          type: 'private',
          peerId: '2001',
          peerType: 1,
          peerNickname: 'Alice',
          updatedAt: now - 1000,
          lastMessage: 'older',
          lastMessageTime: now - 1000,
        ),
      ]);

      final createGate = Completer<void>();
      sessionService.createDelay = createGate.future;

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      await tester.tap(find.text('Alice'));
      await tester.pumpAndSettle();

      final plusButton = find.byTooltip('New session');
      expect(plusButton, findsOneWidget);

      await tester.tap(plusButton);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 1));

      // 先导航到创建中页（可输入），建会话在后台进行；不再依赖 sheet 上的 spinner。
      expect(
        find.byKey(const Key('private_chat_creating_input')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('private_chat_creating_status')),
        findsOneWidget,
      );
      expect(Get.currentRoute, AppRoutes.privateChatCreating);
      expect(sessionService.createCalls, 1);

      createGate.complete();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 1));
      await tester.pumpAndSettle();

      expect(Get.currentRoute, startsWith(AppRoutes.chat));
      expect(
        find.byKey(const Key('private_chat_creating_input')),
        findsNothing,
      );
      expect(find.byTooltip('New session'), findsNothing);
    },
  );

  // --- Guard tests: 极速接入 entry on the messages page, gated on whether
  // the account has any agent yet ---

  testWidgets(
    'empty conversation list with no agent shows quick access button',
    (WidgetTester tester) async {
      Get.put<AgentService>(_FakeAgentService());
      await Get.delete<ConversationsController>(force: true);
      Get.put(ConversationsController());

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('agent-quick-access-button')),
        findsOneWidget,
      );
    },
  );

  testWidgets(
    'empty conversation list with an existing agent hides quick access button',
    (WidgetTester tester) async {
      final agentService = _FakeAgentService();
      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-existing',
          agentName: 'Existing Bot',
          providerType: 3,
          sessionId: 'session-existing',
        ),
      ]);
      Get.put<AgentService>(agentService);
      await Get.delete<ConversationsController>(force: true);
      Get.put(ConversationsController());

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('agent-quick-access-button')), findsNothing);
    },
  );

  testWidgets(
    'non-empty conversation list with no agent shows quick access banner below the list',
    (WidgetTester tester) async {
      Get.put<AgentService>(_FakeAgentService());
      await Get.delete<ConversationsController>(force: true);
      Get.put(ConversationsController());

      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'friend-session-1',
          title: 'Friend Group',
          type: 'group',
          updatedAt: now,
          lastMessage: 'hi',
          lastMessageTime: now,
        ),
      ]);

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      expect(find.text('Friend Group'), findsOneWidget);
      expect(
        find.byKey(const Key('agent-quick-access-button')),
        findsOneWidget,
      );
    },
  );

  testWidgets(
    'non-empty conversation list with an existing agent hides quick access banner',
    (WidgetTester tester) async {
      final agentService = _FakeAgentService();
      agentService.agents.assignAll([
        AgentModel(
          id: 'agent-existing-2',
          agentName: 'Existing Bot 2',
          providerType: 3,
          sessionId: 'session-existing-2',
        ),
      ]);
      Get.put<AgentService>(agentService);
      await Get.delete<ConversationsController>(force: true);
      Get.put(ConversationsController());

      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'friend-session-2',
          title: 'Friend Group 2',
          type: 'group',
          updatedAt: now,
          lastMessage: 'hi',
          lastMessageTime: now,
        ),
      ]);

      await tester.pumpWidget(buildApp());
      await tester.pumpAndSettle();

      expect(find.text('Friend Group 2'), findsOneWidget);
      expect(find.byKey(const Key('agent-quick-access-button')), findsNothing);
    },
  );
}

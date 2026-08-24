import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_category_service.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/ai/agents_view.dart';
import 'package:grix/modules/home/bindings/home_binding.dart';
import 'package:grix/modules/home/conversations_view.dart';
import 'package:grix/modules/home/home_view.dart';
import 'package:grix/modules/chat/services/chat_pane_host.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';

class _FakeImService extends ImService {
  final RxBool _connected = true.obs;
  int refreshAgentOnlineStatesCalls = 0;
  int refreshSessionsNowCalls = 0;
  int refreshSessionsIfStaleCalls = 0;
  bool shouldRefreshStaleSessions = false;

  @override
  bool get isConnected => _connected.value;

  @override
  void connect(String wsUrl) {}

  @override
  void refreshAgentOnlineStates() {
    refreshAgentOnlineStatesCalls++;
  }

  @override
  Future<void> refreshSessionsNow() async {
    refreshSessionsNowCalls++;
  }

  @override
  Future<void> refreshSessionsIfStale({
    Duration maxAge = const Duration(seconds: 45),
  }) async {
    refreshSessionsIfStaleCalls++;
    if (shouldRefreshStaleSessions) {
      refreshSessionsNowCalls++;
    }
  }
}

class _FakeAuthService extends AuthService {
  final RxBool _loggedIn = true.obs;
  final Rxn<User> _userState = Rxn<User>(
    User(id: '1001', username: 'tester', nickname: 'Tester'),
  );

  @override
  bool get isLoggedIn => _loggedIn.value;

  @override
  User? get user => _userState.value;

  @override
  String? get userId => _userState.value?.id;
}

class _FakeFriendService extends FriendService {
  @override
  Future<void> loadFriendList() async {}

  @override
  Future<void> loadFriendRequests() async {}

  @override
  Future<String?> fetchUserProfile(String userId) async => null;
}

class _FakeAgentService extends AgentService {
  int loadAgentsCalls = 0;

  @override
  Future<void> loadAgents({String? categoryId}) async {
    loadAgentsCalls++;
  }
}

class _FakeAgentCategoryService extends AgentCategoryService {
  int loadCategoriesCalls = 0;

  @override
  Future<void> loadCategories() async {
    loadCategoriesCalls++;
  }

  @override
  Future<void> restoreCachedCategories() async {}

  @override
  Future<void> syncCategoriesFromRemote() async {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
    UserImageCacheManager.setDisabledForTest(true);

    Get.put<ImService>(_FakeImService());
    Get.put<AuthService>(_FakeAuthService());
    Get.put<FriendService>(_FakeFriendService());
    Get.put<AgentService>(_FakeAgentService());
    Get.put<AgentCategoryService>(_FakeAgentCategoryService());
    HomeBinding().dependencies();
  });

  tearDown(() async {
    UserImageCacheManager.setDisabledForTest(false);
    Get.reset();
  });

  testWidgets('wide desktop layout mounts the chat pane, narrow does not', (
    WidgetTester tester,
  ) async {
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    tester.view.physicalSize = const Size(1280, 800);
    await tester.pumpWidget(const GetMaterialApp(home: HomeView()));
    await tester.pumpAndSettle();
    expect(find.byType(ChatPaneNavigator), findsOneWidget);
    expect(find.byType(ConversationsView), findsOneWidget);

    tester.view.physicalSize = const Size(900, 800);
    await tester.pumpAndSettle();
    expect(find.byType(ChatPaneNavigator), findsNothing);
    expect(find.byType(ConversationsView), findsOneWidget);
  });

  testWidgets('messages tab stays mounted after switching tabs', (
    WidgetTester tester,
  ) async {
    tester.view.physicalSize = const Size(420, 920);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(const GetMaterialApp(home: HomeView()));
    await tester.pumpAndSettle();

    expect(find.byType(ConversationsView), findsOneWidget);
    expect(find.byType(AgentsView, skipOffstage: false), findsNothing);

    await tester.tap(find.text('nav_ai'));
    await tester.pumpAndSettle();

    expect(find.byType(AgentsView), findsOneWidget);
    final retainedMessagesTab = find.byType(
      ConversationsView,
      skipOffstage: false,
    );
    expect(retainedMessagesTab, findsOneWidget);
    expect(
      TickerMode.valuesOf(tester.element(retainedMessagesTab)).enabled,
      isTrue,
      reason: 'retained tabs must not freeze Material button ink animations',
    );
  });

  testWidgets(
    'entering messages tab skips forced refresh while sessions stay fresh',
    (WidgetTester tester) async {
      final imService = Get.find<ImService>() as _FakeImService;
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        SessionModel(
          sessionId: 'session_1',
          title: 'Alice',
          type: 'private',
          peerId: '1002',
          peerType: 1,
          updatedAt: now,
          unreadCount: 0,
          lastMessage: 'hello',
          lastMessageTime: now,
        ),
      ]);

      tester.view.physicalSize = const Size(420, 920);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);
      await tester.pumpWidget(const GetMaterialApp(home: HomeView()));
      await tester.pumpAndSettle();

      final initialRefreshNowCalls = imService.refreshSessionsNowCalls;
      final initialStaleCalls = imService.refreshSessionsIfStaleCalls;
      expect(initialStaleCalls, greaterThanOrEqualTo(1));

      await tester.tap(find.text('nav_ai'));
      await tester.pumpAndSettle();

      await tester.tap(find.text('nav_messages'));
      await tester.pumpAndSettle();

      expect(
        imService.refreshSessionsIfStaleCalls,
        greaterThan(initialStaleCalls),
      );
      expect(imService.refreshSessionsNowCalls, initialRefreshNowCalls);
    },
  );

  testWidgets('entering agents tab refreshes agent list and online states', (
    WidgetTester tester,
  ) async {
    final imService = Get.find<ImService>() as _FakeImService;
    final agentService = Get.find<AgentService>() as _FakeAgentService;

    tester.view.physicalSize = const Size(420, 920);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(const GetMaterialApp(home: HomeView()));
    await tester.pumpAndSettle();

    expect(agentService.loadAgentsCalls, 0);
    expect(imService.refreshAgentOnlineStatesCalls, 0);

    await tester.tap(find.text('nav_ai'));
    await tester.pumpAndSettle();

    expect(agentService.loadAgentsCalls, 1);
    expect(imService.refreshAgentOnlineStatesCalls, 1);

    await tester.tap(find.text('nav_messages'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('nav_ai'));
    await tester.pumpAndSettle();

    expect(agentService.loadAgentsCalls, 2);
    expect(imService.refreshAgentOnlineStatesCalls, 2);
  });

  testWidgets(
    'retapping messages tab scrolls the first unread conversation to the top',
    (WidgetTester tester) async {
      final imService = Get.find<ImService>() as _FakeImService;
      final now = DateTime.now().millisecondsSinceEpoch;
      imService.sessions.assignAll([
        for (var index = 0; index < 30; index++)
          SessionModel(
            sessionId: 'session_$index',
            title: index == 14
                ? 'Unread Conversation Target'
                : 'Conversation $index',
            type: 'group',
            updatedAt: now - index,
            unreadCount: index == 14 ? 3 : 0,
            lastMessage: 'message $index',
            lastMessageTime: now - index,
          ),
      ]);

      tester.view.physicalSize = const Size(420, 920);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);
      await tester.pumpWidget(const GetMaterialApp(home: HomeView()));
      await tester.pumpAndSettle();

      final scrollable = tester.state<ScrollableState>(
        find.byType(Scrollable).first,
      );
      expect(scrollable.position.pixels, 0);

      await tester.tap(find.text('nav_messages'));
      await tester.pumpAndSettle();

      final targetTile = find.byKey(
        ConversationsView.sessionTileKey('session:session_14'),
      );
      expect(targetTile, findsOneWidget);
      expect(scrollable.position.pixels, greaterThan(0));

      final appBarBottom = tester.getBottomLeft(find.byType(AppBar)).dy;
      final tileTop = tester.getTopLeft(targetTile).dy;
      expect(tileTop - appBarBottom, inInclusiveRange(0, 24));

      await tester.pump(const Duration(seconds: 16));
      tester.view.physicalSize = const Size(420, 920);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pumpAndSettle();
      Get.reset();
    },
  );
}

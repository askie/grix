import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_category_service.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/ai/agents_view.dart';
import 'package:grix/modules/home/bindings/home_binding.dart';
import 'package:grix/modules/home/conversations_view.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/modules/home/home_view.dart';
import 'package:grix/modules/home/services/home_sidebar_host.dart';
import 'package:grix/modules/account_info/account_info_view.dart';
import 'package:grix/modules/account_info/controllers/account_info_controller.dart';
import 'package:grix/modules/account_info/services/account_info_navigator.dart';
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
    SharedPreferences.setMockInitialValues(<String, Object>{});
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

  testWidgets('three-column sidebar is drag-resizable and persisted', (
    WidgetTester tester,
  ) async {
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    tester.view.physicalSize = const Size(1280, 800);

    await tester.pumpWidget(const GetMaterialApp(home: HomeView()));
    await tester.pumpAndSettle();

    final sidebar = find.ancestor(
      of: find.byType(ConversationsView),
      matching: find.byType(SizedBox),
    );
    expect(tester.getSize(sidebar.first).width, 380);

    final handle = find.byWidgetPredicate(
      (w) => w is MouseRegion && w.cursor == SystemMouseCursors.resizeColumn,
    );
    expect(handle, findsOneWidget);
    await tester.drag(handle, const Offset(80, 0));
    await tester.pumpAndSettle();
    expect(tester.getSize(sidebar.first).width, 460);

    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getDouble('home_three_column_sidebar_width'), 460);

    // Dragging below the minimum clamps instead of collapsing the sidebar.
    await tester.drag(handle, const Offset(-400, 0));
    await tester.pumpAndSettle();
    expect(tester.getSize(sidebar.first).width, 280);

    // A fresh home view restores the persisted width.
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpWidget(const GetMaterialApp(home: HomeView()));
    await tester.pumpAndSettle();
    expect(tester.getSize(sidebar.first).width, 280);
  });

  testWidgets('account info opens in the middle column and can go back', (
    WidgetTester tester,
  ) async {
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    tester.view.physicalSize = const Size(1280, 800);

    await tester.pumpWidget(
      GetMaterialApp(
        initialRoute: AppRoutes.home,
        getPages: [GetPage(name: AppRoutes.home, page: () => const HomeView())],
      ),
    );
    await tester.pumpAndSettle();
    expect(HomeSidebarHost.isAvailable, isTrue);

    AccountInfoNavigator.open(
      arguments: {'peer_id': 'peer_1', 'peer_type': '1', 'title': 'P'},
      parameters: {'peer_id': 'peer_1', 'peer_type': '1'},
    );
    await tester.pumpAndSettle();

    expect(HomeSidebarHost.showsAccountInfo, isTrue);
    expect(find.byType(AccountInfoView), findsOneWidget);
    // The chat pane on the right is untouched and the list stays mounted.
    expect(find.byType(ChatPaneNavigator), findsOneWidget);
    expect(find.byType(ConversationsView), findsOneWidget);
    final profileLeft = tester.getTopLeft(find.byType(AccountInfoView)).dx;
    final paneLeft = tester.getTopLeft(find.byType(ChatPaneNavigator)).dx;
    expect(profileLeft, lessThan(paneLeft));
    final firstTag = tester
        .widget<AccountInfoView>(find.byType(AccountInfoView))
        .controllerTag;
    expect(Get.isRegistered<AccountInfoController>(tag: firstTag), isTrue);

    // Opening another profile replaces the previous controller.
    AccountInfoNavigator.open(
      arguments: {'peer_id': 'peer_2', 'peer_type': '1', 'title': 'Q'},
      parameters: {'peer_id': 'peer_2', 'peer_type': '1'},
    );
    await tester.pumpAndSettle();
    expect(find.byType(AccountInfoView), findsOneWidget);
    expect(Get.isRegistered<AccountInfoController>(tag: firstTag), isFalse);
    final secondTag = tester
        .widget<AccountInfoView>(find.byType(AccountInfoView))
        .controllerTag;

    await tester.tap(find.byIcon(Icons.arrow_back_ios_rounded));
    await tester.pumpAndSettle();
    expect(HomeSidebarHost.showsAccountInfo, isFalse);
    expect(find.byType(AccountInfoView), findsNothing);
    expect(Get.isRegistered<AccountInfoController>(tag: secondTag), isFalse);

    // Narrowing the window unmounts the slot and releases the open profile.
    AccountInfoNavigator.open(
      arguments: {'peer_id': 'peer_3', 'peer_type': '1', 'title': 'R'},
      parameters: {'peer_id': 'peer_3', 'peer_type': '1'},
    );
    await tester.pumpAndSettle();
    tester.view.physicalSize = const Size(900, 800);
    await tester.pumpAndSettle();
    expect(HomeSidebarHost.isAvailable, isFalse);
    expect(HomeSidebarHost.showsAccountInfo, isFalse);
    expect(find.byType(AccountInfoView), findsNothing);
  });

  testWidgets('phone and medium widths never mount the chat pane', (
    WidgetTester tester,
  ) async {
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    for (final size in const [
      Size(390, 844),
      Size(767, 900),
      Size(1023, 800),
    ]) {
      tester.view.physicalSize = size;
      await tester.pumpWidget(const GetMaterialApp(home: HomeView()));
      await tester.pumpAndSettle();
      expect(
        find.byType(ChatPaneNavigator),
        findsNothing,
        reason: 'pane must not exist at ${size.width}',
      );
      expect(ChatPaneHost.isAvailable, isFalse);
      expect(find.byType(ConversationsView), findsOneWidget);
      expect(
        find.byType(BottomNavigationBar),
        size.width < 768 ? findsOneWidget : findsNothing,
        reason: 'bottom bar at ${size.width}',
      );
    }
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

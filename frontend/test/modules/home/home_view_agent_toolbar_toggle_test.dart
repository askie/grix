import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/settings/agent_toolbar_visibility_service.dart';
import 'package:grix/app/settings/theme_preference_service.dart';
import 'package:grix/data/providers/agent_category_service.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/egg_market_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/home/bindings/home_binding.dart';
import 'package:grix/modules/home/controllers/home_controller.dart';
import 'package:grix/modules/home/home_view.dart';
import 'package:grix/modules/system/agent_client_toolbar_view.dart';
import 'package:grix/modules/system/grix_connector_service.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeImService extends ImService {
  final RxBool _connected = true.obs;

  @override
  bool get isConnected => _connected.value;

  @override
  void connect(String wsUrl) {}

  @override
  void refreshAgentOnlineStates() {}

  @override
  Future<void> refreshSessionsNow() async {}

  @override
  Future<void> refreshSessionsIfStale({
    Duration maxAge = const Duration(seconds: 45),
  }) async {}
}

class _FakeAuthService extends AuthService {
  final RxBool _loggedIn = true.obs;
  // 带 email：否则进 home 会弹「绑定邮箱」引导，遮住本用例要点的工具栏按钮。
  final Rxn<User> _userState = Rxn<User>(
    User(
      id: '1001',
      username: 'tester',
      email: 'tester@example.com',
      nickname: 'Tester',
    ),
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
  @override
  Future<void> loadAgents({String? categoryId}) async {}
}

class _FakeAgentCategoryService extends AgentCategoryService {
  @override
  Future<void> loadCategories() async {}

  @override
  Future<void> restoreCachedCategories() async {}

  @override
  Future<void> syncCategoriesFromRemote() async {}
}

class _FakeEggMarketService extends EggMarketService {
  @override
  Future<EggSearchResult> searchEggs({
    required String keyword,
    int page = 1,
    int pageSize = 20,
    String locale = '',
  }) async => EggSearchResult(
    localeUsed: locale,
    page: page,
    pageSize: pageSize,
    hasMore: false,
    list: const [],
  );
}

class _RoutePushObserver extends NavigatorObserver {
  final pushedNames = <String?>[];

  @override
  void didPush(Route<dynamic> route, Route<dynamic>? previousRoute) {
    pushedNames.add(route.settings.name);
  }
}

Future<_RoutePushObserver> _pumpSwipeHome(
  WidgetTester tester, {
  double width = 420,
}) async {
  tester.view.physicalSize = Size(width, 920);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  final observer = _RoutePushObserver();
  await tester.pumpWidget(
    GetMaterialApp(
      initialRoute: AppRoutes.home,
      navigatorObservers: [observer],
      getPages: [
        GetPage(name: AppRoutes.home, page: () => const HomeView()),
        // Only the route push is under test, not the favorites page contents.
        GetPage(
          name: AppRoutes.favorites,
          page: () => const Scaffold(body: Text('favorites route')),
        ),
      ],
    ),
  );
  await tester.pumpAndSettle();
  return observer;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    UserImageCacheManager.setDisabledForTest(true);

    Get.put<ImService>(_FakeImService());
    Get.put<AuthService>(_FakeAuthService());
    Get.put<FriendService>(_FakeFriendService());
    Get.put<AgentService>(_FakeAgentService());
    Get.put<AgentCategoryService>(_FakeAgentCategoryService());
    Get.put<EggMarketService>(_FakeEggMarketService());
    Get.put<ThemePreferenceService>(ThemePreferenceService());
    await Get.putAsync<AgentToolbarVisibilityService>(
      () => AgentToolbarVisibilityService().init(),
    );
    HomeBinding().dependencies();
  });

  tearDown(() async {
    UserImageCacheManager.setDisabledForTest(false);
    Get.reset();
  });

  for (final entry in <HomeTab, HomeTab?>{
    HomeTab.conversations: null,
    HomeTab.agents: HomeTab.conversations,
    HomeTab.eggsPond: HomeTab.agents,
    HomeTab.contacts: HomeTab.eggsPond,
    HomeTab.settings: HomeTab.contacts,
    HomeTab.system: null,
  }.entries) {
    test('right-swipe target for ${entry.key.name}', () {
      expect(homeSwipeTargetTab(entry.key), entry.value);
    });
  }

  for (final entry in <HomeTab, HomeTab>{
    HomeTab.agents: HomeTab.conversations,
    HomeTab.eggsPond: HomeTab.agents,
    HomeTab.contacts: HomeTab.eggsPond,
    HomeTab.settings: HomeTab.contacts,
  }.entries) {
    testWidgets('narrow right swipe ${entry.key.name} to ${entry.value.name}', (
      WidgetTester tester,
    ) async {
      final observer = await _pumpSwipeHome(tester);
      final controller = Get.find<HomeController>();
      controller.handleTabTap(entry.key.index);
      await tester.pumpAndSettle();
      expect(controller.currentIndex.value, entry.key.index);
      final pushCount = observer.pushedNames.length;

      await tester.flingFrom(const Offset(60, 350), const Offset(240, 0), 1000);
      await tester.pumpAndSettle();

      expect(controller.currentIndex.value, entry.value.index);
      expect(observer.pushedNames.length, pushCount);
      expect(Get.currentRoute, AppRoutes.home);
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets('narrow repeated conversation flings push favorites only once', (
    WidgetTester tester,
  ) async {
    final observer = await _pumpSwipeHome(tester);

    await tester.flingFrom(const Offset(60, 350), const Offset(240, 0), 1000);
    // Fling again before settling the route transition.
    await tester.flingFrom(const Offset(60, 350), const Offset(240, 0), 1000);
    await tester.pumpAndSettle();

    expect(
      Get.find<HomeController>().currentIndex.value,
      HomeTab.conversations.index,
    );
    expect(Get.currentRoute, AppRoutes.favorites);
    expect(
      observer.pushedNames.where((name) => name == AppRoutes.favorites),
      hasLength(1),
    );
    expect(find.text('favorites route'), findsOneWidget);
    Get.back<void>();
    await tester.pumpAndSettle();
    expect(Get.currentRoute, AppRoutes.home);
    expect(tester.takeException(), isNull);
  });

  testWidgets('slow, left and vertical flings do not navigate', (
    WidgetTester tester,
  ) async {
    final observer = await _pumpSwipeHome(tester);
    final controller = Get.find<HomeController>();
    controller.handleTabTap(HomeTab.agents.index);
    await tester.pumpAndSettle();
    final pushCount = observer.pushedNames.length;

    for (final fling in [
      (const Offset(120, 0), 200.0),
      (const Offset(-120, 0), 1000.0),
      (const Offset(0, -120), 1000.0),
    ]) {
      await tester.flingFrom(const Offset(210, 350), fling.$1, fling.$2);
      await tester.pumpAndSettle();
      expect(controller.currentIndex.value, HomeTab.agents.index);
      expect(observer.pushedNames.length, pushCount);
    }
  });

  testWidgets('wide right flings do not switch tabs or push favorites', (
    WidgetTester tester,
  ) async {
    final observer = await _pumpSwipeHome(tester, width: kHomeWideBreakpoint);
    final controller = Get.find<HomeController>();
    final pushCount = observer.pushedNames.length;

    for (final tab in [HomeTab.conversations, HomeTab.agents]) {
      controller.handleTabTap(tab.index);
      await tester.pumpAndSettle();
      await tester.flingFrom(
        const Offset(100, 350),
        const Offset(240, 0),
        1000,
      );
      await tester.pumpAndSettle();
      expect(controller.currentIndex.value, tab.index);
      expect(observer.pushedNames.length, pushCount);
    }
  });

  testWidgets('desktop shows toggle left of favorites and hides the strip', (
    WidgetTester tester,
  ) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.macOS;
    Get.put(GrixConnectorService());

    try {
      tester.view.physicalSize = const Size(1200, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);
      await tester.pumpWidget(const GetMaterialApp(home: HomeView()));
      await tester.pumpAndSettle();

      final homeController = Get.find<HomeController>();

      // 默认展示工具栏。
      expect(homeController.agentToolbarVisible.value, isTrue);
      expect(find.byType(AgentClientToolbarView), findsOneWidget);
      expect(find.byIcon(Icons.visibility_outlined), findsOneWidget);

      // 切换按钮位于收藏按钮左侧。
      final toggleLeft = tester.getTopLeft(
        find.byIcon(Icons.visibility_outlined),
      );
      final favoritesLeft = tester.getTopLeft(
        find.byIcon(Icons.bookmark_border_rounded),
      );
      expect(toggleLeft.dx, lessThan(favoritesLeft.dx));

      // 点击后隐藏工具栏，图标切换为“已隐藏”态，并持久化到本地。
      await tester.tap(find.byIcon(Icons.visibility_outlined));
      await tester.pumpAndSettle();

      expect(homeController.agentToolbarVisible.value, isFalse);
      expect(find.byType(AgentClientToolbarView), findsNothing);
      expect(find.byIcon(Icons.visibility_off_outlined), findsOneWidget);
      final prefs = await SharedPreferences.getInstance();
      expect(prefs.getBool(AgentToolbarVisibilityService.prefsKey), isFalse);

      // 再次点击恢复展示。
      await tester.tap(find.byIcon(Icons.visibility_off_outlined));
      await tester.pumpAndSettle();

      expect(homeController.agentToolbarVisible.value, isTrue);
      expect(find.byType(AgentClientToolbarView), findsOneWidget);
      expect(prefs.getBool(AgentToolbarVisibilityService.prefsKey), isTrue);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });

  testWidgets('desktop restores hidden toolbar from persisted preference', (
    WidgetTester tester,
  ) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.macOS;

    try {
      SharedPreferences.setMockInitialValues({
        AgentToolbarVisibilityService.prefsKey: false,
      });
      await Get.delete<AgentToolbarVisibilityService>(force: true);
      await Get.putAsync<AgentToolbarVisibilityService>(
        () => AgentToolbarVisibilityService().init(),
      );
      // 同一测试套里 HomeController 已在前面用例中实例化，需强制重建才能读新服务值。
      await Get.delete<HomeController>(force: true);
      HomeBinding().dependencies();

      Get.put(GrixConnectorService());
      tester.view.physicalSize = const Size(1200, 900);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);
      await tester.pumpWidget(const GetMaterialApp(home: HomeView()));
      await tester.pumpAndSettle();

      final homeController = Get.find<HomeController>();
      expect(homeController.agentToolbarVisible.value, isFalse);
      expect(find.byType(AgentClientToolbarView), findsNothing);
      expect(find.byIcon(Icons.visibility_off_outlined), findsOneWidget);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });

  testWidgets('mobile does not show the agent toolbar toggle', (
    WidgetTester tester,
  ) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
    Get.put(GrixConnectorService());

    try {
      tester.view.physicalSize = const Size(420, 920);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);
      await tester.pumpWidget(const GetMaterialApp(home: HomeView()));
      await tester.pumpAndSettle();

      expect(find.byIcon(Icons.visibility_outlined), findsNothing);
      expect(find.byIcon(Icons.visibility_off_outlined), findsNothing);
      expect(find.byType(AgentClientToolbarView), findsNothing);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });
}

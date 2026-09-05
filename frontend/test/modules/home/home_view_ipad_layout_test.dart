import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/settings/agent_toolbar_visibility_service.dart';
import 'package:grix/data/providers/agent_category_service.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/chat/services/chat_pane_host.dart';
import 'package:grix/modules/home/bindings/home_binding.dart';
import 'package:grix/modules/home/home_view.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeImService extends ImService {
  @override
  bool get isConnected => true;

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
  // 带 email：否则进 home 会弹「绑定邮箱」引导，遮住布局本身。
  final Rxn<User> _userState = Rxn<User>(
    User(
      id: '1001',
      username: 'tester',
      email: 'tester@example.com',
      nickname: 'Tester',
    ),
  );

  @override
  bool get isLoggedIn => true;

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
    await Get.putAsync<AgentToolbarVisibilityService>(
      () => AgentToolbarVisibilityService().init(),
    );
    HomeBinding().dependencies();
  });

  tearDown(() async {
    UserImageCacheManager.setDisabledForTest(false);
    Get.reset();
  });

  Future<void> pumpHomeAt(WidgetTester tester, Size size) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(const GetMaterialApp(home: HomeView()));
    await tester.pumpAndSettle();
  }

  // iPad Pro 13 英寸的逻辑尺寸，以及 Stage Manager 能拖到的最窄宽度。
  const portrait = Size(1024, 1366);
  const landscape = Size(1366, 1024);
  const stageManagerNarrow = Size(320, 1024);

  testWidgets('iPad portrait uses the three-column layout', (tester) async {
    await pumpHomeAt(tester, portrait);

    expect(find.byType(ChatPaneNavigator), findsOneWidget);
    expect(find.byType(BottomNavigationBar), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('iPad landscape uses the three-column layout', (tester) async {
    await pumpHomeAt(tester, landscape);

    expect(find.byType(ChatPaneNavigator), findsOneWidget);
    expect(find.byType(BottomNavigationBar), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('narrowest Stage Manager width falls back to bottom navigation', (
    tester,
  ) async {
    await pumpHomeAt(tester, stageManagerNarrow);

    expect(find.byType(BottomNavigationBar), findsOneWidget);
    expect(find.byType(ChatPaneNavigator), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('resizing the window switches navigation without overflow', (
    tester,
  ) async {
    // Stage Manager 拖动窗口宽度：全屏 -> 最窄 -> 全屏，三种布局都不应溢出。
    await pumpHomeAt(tester, landscape);
    expect(find.byType(ChatPaneNavigator), findsOneWidget);

    tester.view.physicalSize = stageManagerNarrow;
    await tester.pumpAndSettle();
    expect(find.byType(BottomNavigationBar), findsOneWidget);
    expect(find.byType(ChatPaneNavigator), findsNothing);

    // 侧边导航（无聊天面板）的中间档。
    tester.view.physicalSize = const Size(820, 1024);
    await tester.pumpAndSettle();
    expect(find.byType(BottomNavigationBar), findsNothing);
    expect(find.byType(ChatPaneNavigator), findsNothing);

    tester.view.physicalSize = landscape;
    await tester.pumpAndSettle();
    expect(find.byType(ChatPaneNavigator), findsOneWidget);

    expect(tester.takeException(), isNull);
  });
}

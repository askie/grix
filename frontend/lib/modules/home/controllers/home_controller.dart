import 'package:get/get.dart';
import '../../../app/routes/root_route_navigator.dart';
import '../../../app/routes/app_routes.dart';
import '../../../app/settings/agent_toolbar_visibility_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../data/providers/auth_service.dart';
import '../../../data/providers/friend_service.dart';
import '../services/home_tab_url_sync.dart';
import '../services/home_sidebar_host.dart';
import 'dart:async';

class HomeController extends GetxController {
  HomeController({required HomeTabUrlSync urlSync}) : _urlSync = urlSync;

  final ImService imService = Get.find<ImService>();
  final AuthService authService = Get.find<AuthService>();
  final FriendService friendService = Get.find<FriendService>();
  final HomeTabUrlSync _urlSync;
  StreamSubscription<String>? _pathSyncSubscription;

  final currentIndex = 0.obs;
  final messagesTabRetapTick = 0.obs;

  /// 桌面端首页 Agent 工具栏显隐状态，默认展示。
  final agentToolbarVisible = true.obs;

  int get pendingFriendRequestCount =>
      friendService.friendRequests.where((r) => r.status == 0).length;

  @override
  void onInit() {
    super.onInit();
    _syncToolbarVisibilityFromService();
    _syncCurrentTabFromPath(_urlSync.currentPath);
    _normalizeCurrentHomePath();
    _pathSyncSubscription = _urlSync.onPathChanged.listen(
      _syncCurrentTabFromPath,
    );
  }

  void _syncToolbarVisibilityFromService() {
    if (!Get.isRegistered<AgentToolbarVisibilityService>()) return;
    agentToolbarVisible.value =
        Get.find<AgentToolbarVisibilityService>().visible;
  }

  @override
  void onReady() {
    super.onReady();
    _checkAuthAndConnect();
  }

  @override
  void onClose() {
    _pathSyncSubscription?.cancel();
    _urlSync.dispose();
    super.onClose();
  }

  void _checkAuthAndConnect() {
    if (!authService.isLoggedIn) {
      RootRouteNavigator.toLogin();
    } else if (!imService.isConnected) {
      imService.ensureConnected();
    }
  }

  void _syncCurrentTabFromPath(String path) {
    final homeTab = AppRoutes.homeTabForPath(path);
    if (homeTab == null) {
      return;
    }
    if (currentIndex.value == homeTab.index) {
      return;
    }
    currentIndex.value = homeTab.index;
  }

  void _normalizeCurrentHomePath() {
    if (AppRoutes.homeTabForPath(_urlSync.currentPath) != null) {
      return;
    }
    final homeTab = HomeTabX.fromIndex(currentIndex.value);
    _urlSync.replacePath(homeTab.routePath);
  }

  void changePage(int index) {
    final homeTab = HomeTabX.fromIndex(index);
    if (currentIndex.value != homeTab.index) {
      currentIndex.value = homeTab.index;
    }
    _urlSync.replacePath(homeTab.routePath);
  }

  /// 切换桌面端首页 Agent 工具栏的显隐，并持久化到本地。
  void toggleAgentToolbarVisibility() {
    final next = !agentToolbarVisible.value;
    agentToolbarVisible.value = next;
    if (Get.isRegistered<AgentToolbarVisibilityService>()) {
      Get.find<AgentToolbarVisibilityService>().setVisible(next);
    }
  }

  void handleTabTap(int index) {
    // A rail tap always brings the tab content back, even if a profile is
    // currently shown on top of it in the desktop middle column.
    HomeSidebarHost.closeAccountInfo();
    final homeTab = HomeTabX.fromIndex(index);
    final isCurrentTab = currentIndex.value == homeTab.index;
    if (isCurrentTab &&
        homeTab == HomeTab.conversations &&
        imService.totalUnread > 0) {
      messagesTabRetapTick.value++;
    }
    changePage(index);
  }
}

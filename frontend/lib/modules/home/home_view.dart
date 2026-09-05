import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../../app/routes/app_routes.dart';
import '../../data/providers/push_registration_service.dart';
import '../../platform/platform_capability.dart';
import '../../shared/widgets/app_icon.dart';
import 'controllers/home_controller.dart';
import 'conversations_view.dart';
import 'contacts_view.dart';
import '../../modules/ai/agents_view.dart';
import '../../modules/eggs/egg_market_view.dart';
import '../../modules/profile/profile_view.dart';
import '../../modules/system/system_settings_view.dart';
import '../../modules/system/agent_client_toolbar_view.dart';
import '../../modules/system/grix_connector_service.dart';
import '../auth/services/bind_email_prompt.dart';
import '../auth/services/bind_phone_prompt.dart';
import '../chat/services/chat_pane_host.dart';
import 'services/home_sidebar_host.dart';

/// From this width the home page leaves the single-column mobile layout.
/// The favorites swipe shortcut is a mobile affordance and stays below it.
const double kHomeWideBreakpoint = 768.0;

/// Minimum fling velocity (logical pixels per second) that counts as a
/// deliberate horizontal swipe between the home page and the favorites page.
const double kHomeSwipeVelocityThreshold = 300.0;

class HomeView extends StatefulWidget {
  const HomeView({super.key});

  @override
  State<HomeView> createState() => _HomeViewState();
}

class _HomeViewState extends State<HomeView> {
  final HomeController controller = Get.find<HomeController>();
  final PageStorageBucket _pageStorageBucket = PageStorageBucket();
  final Set<int> _initializedTabIndices = <int>{0};

  /// Three-column sidebar width; user-resizable and persisted locally.
  double _sidebarWidth = _kThreeColumnSidebarWidth;

  @override
  void initState() {
    super.initState();
    _loadSidebarWidth();
    // 进 home 后弹一次绑定引导：
    // 老 email 用户引导绑手机号（不强制，拒绝后永久不再弹）；
    // 手机号注册用户引导绑邮箱（未绑定时每次冷启动都会再弹一次）。
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      if (!mounted) return;
      await maybePromptBindEmail();
      if (!mounted) return;
      await maybePromptBindPhone();
    });
  }

  /// From this width the home page becomes three columns: rail, tab content
  /// (conversation list etc.) and a chat pane hosting the opened conversation.
  static const double _kThreeColumnBreakpoint = 1024.0;
  static const double _kThreeColumnSidebarWidth = 380.0;
  static const double _kThreeColumnSidebarMinWidth = 280.0;
  static const double _kThreeColumnPaneMinWidth = 360.0;
  static const double _kNavigationRailWidth = 64.0;
  static const String _kSidebarWidthPrefKey = 'home_three_column_sidebar_width';

  Future<void> _loadSidebarWidth() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final saved = prefs.getDouble(_kSidebarWidthPrefKey);
      if (saved == null || !mounted) return;
      setState(() => _sidebarWidth = saved);
    } catch (_) {
      // Storage unavailable: keep the default width.
    }
  }

  Future<void> _saveSidebarWidth() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setDouble(_kSidebarWidthPrefKey, _sidebarWidth);
    } catch (_) {}
  }

  double _clampSidebarWidth(double width, double totalWidth) {
    final maxWidth =
        totalWidth - _kNavigationRailWidth - _kThreeColumnPaneMinWidth;
    if (maxWidth <= _kThreeColumnSidebarMinWidth) {
      return _kThreeColumnSidebarMinWidth;
    }
    return width.clamp(_kThreeColumnSidebarMinWidth, maxWidth).toDouble();
  }

  Widget _buildSidebarResizeHandle(ThemeData theme, double totalWidth) {
    return MouseRegion(
      cursor: SystemMouseCursors.resizeColumn,
      child: GestureDetector(
        behavior: HitTestBehavior.translucent,
        onHorizontalDragUpdate: (details) {
          final next = _clampSidebarWidth(
            _sidebarWidth + details.delta.dx,
            totalWidth,
          );
          if (next == _sidebarWidth) return;
          setState(() => _sidebarWidth = next);
        },
        onHorizontalDragEnd: (_) => _saveSidebarWidth(),
        child: SizedBox(
          width: 6,
          child: Center(
            child: VerticalDivider(
              width: 0.5,
              thickness: 0.5,
              color: theme.colorScheme.outline.withValues(alpha: 0.2),
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _handleRequestNotificationPermission() async {
    if (!Get.isRegistered<PushRegistrationService>()) return;
    final pushService = Get.find<PushRegistrationService>();
    await pushService.requestPermissionWithGesture();
  }

  // 仅在已注册 PushRegistrationService 时才包 Obx，避免"空 Obx 无 observable"在测试/无服务环境抛错。
  Widget _buildNotificationBannerSlot(BuildContext context) {
    if (!kIsWeb || !Get.isRegistered<PushRegistrationService>()) {
      return const SizedBox.shrink();
    }
    return Obx(() {
      if (Get.find<PushRegistrationService>().permissionState.value !=
          'default') {
        return const SizedBox.shrink();
      }
      return Padding(
        padding: EdgeInsets.only(top: MediaQuery.of(context).padding.top + 8),
        child: _buildNotificationPermissionBanner(),
      );
    });
  }

  /// 桌面端首页顶部 Agent 工具栏（小图模式）
  Widget _buildAgentToolbarStrip() {
    if (!PlatformCapability.isDesktop) return const SizedBox.shrink();
    if (!Get.isRegistered<GrixConnectorService>()) {
      return const SizedBox.shrink();
    }
    return Obx(() {
      if (!controller.agentToolbarVisible.value) {
        return const SizedBox.shrink();
      }
      return AgentClientToolbarView(
        service: Get.find<GrixConnectorService>(),
        compact: true,
      );
    });
  }

  Widget _buildNotificationPermissionBanner() {
    return Container(
      margin: const EdgeInsets.fromLTRB(12, 0, 12, 4),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      decoration: BoxDecoration(
        color: const Color(0xFF1A73E8),
        borderRadius: BorderRadius.circular(10),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.12),
            blurRadius: 8,
            offset: const Offset(0, 2),
          ),
        ],
      ),
      child: Row(
        children: [
          const Icon(
            Icons.notifications_active_outlined,
            size: 18,
            color: Colors.white,
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              'notification_prompt_enable'.tr,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(
                color: Colors.white,
                fontSize: 13,
                fontWeight: FontWeight.w500,
              ),
            ),
          ),
          const SizedBox(width: 8),
          GestureDetector(
            onTap: _handleRequestNotificationPermission,
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
              decoration: BoxDecoration(
                color: Colors.white,
                borderRadius: BorderRadius.circular(6),
              ),
              child: Text(
                'notification_prompt_action'.tr,
                style: const TextStyle(
                  color: Color(0xFF1A73E8),
                  fontSize: 12,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildCurrentPage(int index) {
    switch (index) {
      case 0:
        return const ConversationsView();
      case 1:
        return const AgentsView();
      case 2:
        return EggMarketView();
      case 3:
        return const ContactsView();
      case 4:
        return const ProfileView();
      case 5:
        return const SystemSettingsView();
      default:
        return const SizedBox.shrink();
    }
  }

  int get _tabCount => PlatformCapability.isDesktop ? 6 : 5;

  Widget _buildTabStack(int activeIndex) {
    _initializedTabIndices.add(activeIndex);
    return Stack(
      fit: StackFit.expand,
      children: List<Widget>.generate(_tabCount, (index) {
        if (!_initializedTabIndices.contains(index)) {
          return const SizedBox.shrink();
        }
        final isActive = index == activeIndex;
        return Offstage(
          offstage: !isActive,
          child: KeyedSubtree(
            key: PageStorageKey<String>('home_tab_$index'),
            child: _buildCurrentPage(index),
          ),
        );
      }),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return LayoutBuilder(
      builder: (_, constraints) {
        final isWide = constraints.maxWidth >= kHomeWideBreakpoint;
        final isThreeColumn = constraints.maxWidth >= _kThreeColumnBreakpoint;

        final contentBody = Column(
          children: [
            _buildNotificationBannerSlot(context),
            _buildAgentToolbarStrip(),
            Expanded(
              child: PageStorage(
                bucket: _pageStorageBucket,
                child: Obx(() => _buildTabStack(controller.currentIndex.value)),
              ),
            ),
          ],
        );

        if (isThreeColumn) {
          return Scaffold(
            body: Row(
              children: [
                _buildNavigationRail(theme),
                SizedBox(
                  width: _clampSidebarWidth(
                    _sidebarWidth,
                    constraints.maxWidth,
                  ),
                  child: HomeSidebarSlot(child: contentBody),
                ),
                _buildSidebarResizeHandle(theme, constraints.maxWidth),
                const Expanded(child: ChatPaneNavigator()),
              ],
            ),
          );
        }

        if (isWide) {
          return Scaffold(
            body: Row(
              children: [
                _buildNavigationRail(theme),
                Expanded(child: contentBody),
              ],
            ),
          );
        }

        return Scaffold(
          body: GestureDetector(
            behavior: HitTestBehavior.translucent,
            onHorizontalDragEnd: _handleHorizontalDragEnd,
            child: contentBody,
          ),
          bottomNavigationBar: Container(
            decoration: BoxDecoration(
              border: Border(
                top: BorderSide(
                  color: theme.colorScheme.outline.withValues(alpha: 0.2),
                  width: 0.5,
                ),
              ),
            ),
            child: Obx(
              () => BottomNavigationBar(
                currentIndex: controller.currentIndex.value.clamp(0, 4),
                onTap: controller.handleTabTap,
                items: [
                  BottomNavigationBarItem(
                    icon: _NavBadgeIcon(
                      icon: const AppIcon('assets/icons/nav_messages.svg'),
                      getCount: () => controller.imService.notificationUnread,
                    ),
                    activeIcon: _NavBadgeIcon(
                      icon: const AppIcon('assets/icons/nav_messages.svg'),
                      getCount: () => controller.imService.notificationUnread,
                    ),
                    label: 'nav_messages'.tr,
                  ),
                  BottomNavigationBarItem(
                    icon: const AppIcon('assets/icons/nav_ai.svg'),
                    activeIcon: const AppIcon('assets/icons/nav_ai.svg'),
                    label: 'nav_ai'.tr,
                  ),
                  BottomNavigationBarItem(
                    icon: const AppIcon('assets/icons/nav_shrimp.svg'),
                    activeIcon: const AppIcon('assets/icons/nav_shrimp.svg'),
                    label: 'nav_pond'.tr,
                  ),
                  BottomNavigationBarItem(
                    icon: _NavBadgeIcon(
                      icon: const AppIcon('assets/icons/nav_contacts.svg'),
                      getCount: () => controller.pendingFriendRequestCount,
                    ),
                    activeIcon: _NavBadgeIcon(
                      icon: const AppIcon('assets/icons/nav_contacts.svg'),
                      getCount: () => controller.pendingFriendRequestCount,
                    ),
                    label: 'nav_contacts'.tr,
                  ),
                  BottomNavigationBarItem(
                    icon: const AppIcon('assets/icons/nav_me.svg'),
                    activeIcon: const AppIcon('assets/icons/nav_me.svg'),
                    label: 'nav_settings'.tr,
                  ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }

  /// Narrow layout only: a right swipe over the tab content opens the
  /// favorites page. Vertical scrolling and taps are untouched because the
  /// horizontal recognizer only wins the arena on horizontally dominant drags.
  void _handleHorizontalDragEnd(DragEndDetails details) {
    final velocity = details.primaryVelocity ?? 0;
    if (velocity <= kHomeSwipeVelocityThreshold) {
      return;
    }
    // Repeated flings must not stack multiple favorites pages.
    if (Get.currentRoute == AppRoutes.favorites) {
      return;
    }
    Get.toNamed(AppRoutes.favorites);
  }

  Widget _buildNavigationRail(ThemeData theme) {
    final navTheme = theme.navigationRailTheme;
    return Container(
      width: 64,
      decoration: BoxDecoration(
        border: Border(
          right: BorderSide(
            color: theme.colorScheme.outline.withValues(alpha: 0.2),
            width: 0.5,
          ),
        ),
      ),
      child: Obx(() {
        final index = controller.currentIndex.value;
        return Column(
          children: [
            SizedBox(height: MediaQuery.of(context).padding.top + 16),
            _buildRailItem(
              index: 0,
              activeIndex: index,
              icon: 'assets/icons/nav_messages.svg',
              label: 'nav_messages',
              navTheme: navTheme,
              theme: theme,
              badgeCount: () => controller.imService.notificationUnread,
            ),
            const SizedBox(height: 20),
            _buildRailItem(
              index: 1,
              activeIndex: index,
              icon: 'assets/icons/nav_ai.svg',
              label: 'nav_ai',
              navTheme: navTheme,
              theme: theme,
            ),
            const SizedBox(height: 20),
            _buildRailItem(
              index: 2,
              activeIndex: index,
              icon: 'assets/icons/nav_shrimp.svg',
              label: 'nav_pond',
              navTheme: navTheme,
              theme: theme,
            ),
            const SizedBox(height: 20),
            _buildRailItem(
              index: 3,
              activeIndex: index,
              icon: 'assets/icons/nav_contacts.svg',
              label: 'nav_contacts',
              navTheme: navTheme,
              theme: theme,
              badgeCount: () => controller.pendingFriendRequestCount,
            ),
            const SizedBox(height: 20),
            _buildRailItem(
              index: 4,
              activeIndex: index,
              icon: 'assets/icons/nav_me.svg',
              label: 'nav_settings',
              navTheme: navTheme,
              theme: theme,
            ),
            if (PlatformCapability.isDesktop) ...[
              const SizedBox(height: 20),
              _buildRailItem(
                index: 5,
                activeIndex: index,
                icon: 'assets/icons/nav_system.svg',
                label: 'nav_system',
                navTheme: navTheme,
                theme: theme,
              ),
            ],
          ],
        );
      }),
    );
  }

  Widget _buildRailItem({
    required int index,
    required int activeIndex,
    required String icon,
    required String label,
    required NavigationRailThemeData navTheme,
    required ThemeData theme,
    int Function()? badgeCount,
  }) {
    final selected = index == activeIndex;
    final color = selected
        ? navTheme.selectedIconTheme?.color ?? theme.colorScheme.primary
        : navTheme.unselectedIconTheme?.color ?? theme.colorScheme.onSurface;
    // 使用 GestureDetector 替代 Material+InkWell，避免点击时出现水波纹/高亮闪烁。
    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onTap: () => controller.handleTabTap(index),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 4),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (badgeCount != null)
              _NavBadgeIcon(icon: AppIcon(icon), getCount: badgeCount)
            else
              AppIcon(icon, color: color),
            const SizedBox(height: 2),
            Text(
              label.tr,
              style: TextStyle(
                fontSize: 11,
                fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
                color: color,
              ),
              overflow: TextOverflow.ellipsis,
            ),
          ],
        ),
      ),
    );
  }
}

class _NavBadgeIcon extends StatelessWidget {
  const _NavBadgeIcon({required this.icon, required this.getCount});

  final Widget icon;
  final int Function() getCount;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final count = getCount();
      if (count <= 0) return icon;
      return Stack(
        clipBehavior: Clip.none,
        children: [
          icon,
          Positioned(
            right: -8,
            top: -4,
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 1),
              decoration: BoxDecoration(
                color: Colors.red,
                borderRadius: BorderRadius.circular(10),
              ),
              constraints: const BoxConstraints(minWidth: 16, minHeight: 16),
              child: Text(
                count > 99 ? '99+' : count.toString(),
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 10,
                  fontWeight: FontWeight.w600,
                ),
                textAlign: TextAlign.center,
              ),
            ),
          ),
        ],
      );
    });
  }
}

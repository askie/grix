import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import '../../app/theme/app_palette.dart';
import '../../modules/auth/auth_service.dart';
import '../../modules/auth/change_password_dialog.dart';
import '../navigation/nav_items.dart';

/// 后台统一骨架，多端自适应：
///   - 宽屏（桌面 / iPad 横屏，宽度 ≥ [_wideBreakpoint]）：左侧固定侧边栏。
///   - 窄屏（手机 / iPad 竖屏，宽度 < [_wideBreakpoint]）：顶部 AppBar + 底部导航栏，
///     底部栏放前若干主入口，其余收入“更多”弹层。
class AdminScaffold extends StatelessWidget {
  const AdminScaffold({
    super.key,
    required this.title,
    required this.body,
    this.actions,
    this.bottom,
  });

  final String title;
  final Widget body;
  final List<Widget>? actions;

  /// 可选的底部组件，用于 TabBar 等场景。
  final PreferredSizeWidget? bottom;

  static const double _wideBreakpoint = 900;
  static const double _railWidth = 240;

  @override
  Widget build(BuildContext context) {
    final isWide = MediaQuery.of(context).size.width >= _wideBreakpoint;
    return isWide ? _buildWide(context) : _buildNarrow(context);
  }

  // ---- 宽屏：左侧边栏 ----
  Widget _buildWide(BuildContext context) {
    return Scaffold(
      body: Row(
        children: [
          SizedBox(
            width: _railWidth,
            child: _Sidebar(currentRoute: Get.currentRoute),
          ),
          const VerticalDivider(width: 1),
          Expanded(
            child: Column(
              children: [
                _DesktopTopBar(title: title, actions: actions),
                ?bottom,
                const Divider(height: 1),
                Expanded(child: body),
              ],
            ),
          ),
        ],
      ),
    );
  }

  // ---- 窄屏：底部导航栏 ----
  Widget _buildNarrow(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(title),
        bottom: bottom,
        actions: [...?actions],
      ),
      body: body,
      bottomNavigationBar: _BottomBar(currentRoute: Get.currentRoute),
    );
  }
}

Future<void> _logout() async {
  await AuthService.to.logout();
  Get.offAllNamed(AppRoutes.login);
}

void _goTo(String route) {
  if (Get.currentRoute != route) {
    Get.offAndToNamed(route);
  }
}

// ============ 底部导航栏（移动端） ============

class _BottomBar extends StatelessWidget {
  const _BottomBar({required this.currentRoute});

  final String currentRoute;

  List<NavItem> get _visibleItems {
    final profile = AuthService.to.profile.value;
    return kNavItems
        .where(
          (item) =>
              item.permissionKey.isEmpty ||
              profile == null ||
              profile.hasPermission(item.permissionKey),
        )
        .toList();
  }

  /// 底部栏直接展示的主入口。
  List<NavItem> get _primary {
    final visible = _visibleItems;
    if (visible.length <= kBottomBarPrimaryCount + 1) return visible;
    return visible.take(kBottomBarPrimaryCount).toList();
  }

  bool get _hasMore => _visibleItems.length > _primary.length;

  @override
  Widget build(BuildContext context) {
    final primary = _primary;
    final destinations = <NavigationDestination>[
      for (final item in primary)
        NavigationDestination(icon: Icon(item.icon), label: item.label),
      if (_hasMore)
        const NavigationDestination(icon: Icon(Icons.more_horiz), label: '更多'),
    ];

    int selected = primary.indexWhere((e) => e.route == currentRoute);
    if (selected < 0) {
      // 当前页在“更多”分组里：高亮“更多”，否则回退到第 0 项。
      selected = _hasMore ? primary.length : 0;
    }

    return NavigationBar(
      selectedIndex: selected,
      onDestinationSelected: (i) {
        if (i < primary.length) {
          _goTo(primary[i].route);
        } else {
          _showMoreSheet(context);
        }
      },
      destinations: destinations,
    );
  }

  void _showMoreSheet(BuildContext context) {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (ctx) {
        return SafeArea(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 20,
                  vertical: 4,
                ),
                child: Row(
                  children: [
                    Text('全部模块', style: Theme.of(ctx).textTheme.titleMedium),
                    const Spacer(),
                    TextButton.icon(
                      onPressed: () {
                        Navigator.of(ctx).pop();
                        showChangePasswordDialog(context);
                      },
                      icon: const Icon(Icons.lock_outline, size: 18),
                      label: const Text('修改密码'),
                    ),
                    TextButton.icon(
                      onPressed: () {
                        Navigator.of(ctx).pop();
                        _logout();
                      },
                      icon: const Icon(Icons.logout, size: 18),
                      label: const Text('退出登录'),
                    ),
                  ],
                ),
              ),
              const Divider(height: 1),
              Flexible(
                child: GridView.count(
                  crossAxisCount: 4,
                  shrinkWrap: true,
                  padding: const EdgeInsets.all(16),
                  childAspectRatio: 0.9,
                  children: [
                    for (final item in _visibleItems)
                      _MoreTile(
                        item: item,
                        selected: item.route == currentRoute,
                        onTap: () {
                          Navigator.of(ctx).pop();
                          _goTo(item.route);
                        },
                      ),
                  ],
                ),
              ),
            ],
          ),
        );
      },
    );
  }
}

class _MoreTile extends StatelessWidget {
  const _MoreTile({
    required this.item,
    required this.selected,
    required this.onTap,
  });

  final NavItem item;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final color = selected
        ? Theme.of(context).colorScheme.primary
        : Theme.of(context).colorScheme.onSurfaceVariant;
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(12),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(item.icon, color: color),
          const SizedBox(height: 6),
          Text(
            item.label,
            textAlign: TextAlign.center,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(fontSize: 12, color: color),
          ),
        ],
      ),
    );
  }
}

// ============ 顶部条（桌面） ============

class _DesktopTopBar extends StatelessWidget {
  const _DesktopTopBar({required this.title, this.actions});

  final String title;
  final List<Widget>? actions;

  @override
  Widget build(BuildContext context) {
    return Container(
      height: 56,
      color: Theme.of(context).scaffoldBackgroundColor,
      padding: const EdgeInsets.symmetric(horizontal: 20),
      child: Row(
        children: [
          Text(title, style: Theme.of(context).textTheme.titleMedium),
          const Spacer(),
          ...?actions,
        ],
      ),
    );
  }
}

// ============ 侧边栏（桌面） ============

class _Sidebar extends StatelessWidget {
  const _Sidebar({required this.currentRoute});

  final String currentRoute;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final auth = AuthService.to;
    return Container(
      color: AppPalette.surfaceAlt,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 22, 20, 18),
            child: Row(
              children: [
                Icon(
                  Icons.shield_moon_outlined,
                  color: theme.colorScheme.primary,
                ),
                const SizedBox(width: 10),
                Flexible(
                  child: Text(
                    '塘主',
                    style: theme.textTheme.titleMedium,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ),
          ),
          const Divider(height: 1),
          Expanded(
            child: Obx(() {
              final profile = auth.profile.value;
              final items = kNavItems
                  .where(
                    (item) =>
                        item.permissionKey.isEmpty ||
                        profile == null ||
                        profile.hasPermission(item.permissionKey),
                  )
                  .toList();
              return ListView(
                padding: const EdgeInsets.symmetric(vertical: 8),
                children: [
                  for (final item in items)
                    _NavTile(item: item, selected: currentRoute == item.route),
                ],
              );
            }),
          ),
          const Divider(height: 1),
          const _AdminFooter(),
        ],
      ),
    );
  }
}

class _NavTile extends StatelessWidget {
  const _NavTile({required this.item, required this.selected});

  final NavItem item;
  final bool selected;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 2),
      child: Material(
        color: selected ? AppPalette.brandSoft : Colors.transparent,
        borderRadius: BorderRadius.circular(8),
        child: ListTile(
          dense: true,
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
          leading: Icon(
            item.icon,
            color: selected ? theme.colorScheme.primary : null,
            size: 22,
          ),
          title: Text(
            item.label,
            style: TextStyle(
              color: selected ? theme.colorScheme.primary : null,
              fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
            ),
          ),
          onTap: () => _goTo(item.route),
        ),
      ),
    );
  }
}

class _AdminFooter extends StatelessWidget {
  const _AdminFooter();

  @override
  Widget build(BuildContext context) {
    final auth = AuthService.to;
    return Padding(
      padding: const EdgeInsets.all(12),
      child: Obx(() {
        final name = auth.profile.value?.displayName ?? '管理员';
        return InkWell(
          borderRadius: BorderRadius.circular(8),
          onTap: () => _showUserMenu(context),
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: 4),
            child: Row(
              children: [
                const CircleAvatar(
                  radius: 16,
                  child: Icon(Icons.person, size: 18),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    name,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.bodyMedium,
                  ),
                ),
                const Icon(Icons.arrow_drop_up, size: 20),
              ],
            ),
          ),
        );
      }),
    );
  }

  void _showUserMenu(BuildContext context) {
    final renderBox = context.findRenderObject() as RenderBox;
    final size = renderBox.size;
    final offset = renderBox.localToGlobal(Offset.zero);

    showMenu<String>(
      context: context,
      position: RelativeRect.fromLTRB(
        offset.dx + 8,
        offset.dy - 96, // 两个菜单项的高度
        offset.dx + size.width + 8,
        offset.dy + size.height,
      ),
      items: [
        const PopupMenuItem<String>(
          value: 'password',
          height: 40,
          child: Row(
            children: [
              Icon(Icons.lock_outline, size: 18),
              SizedBox(width: 10),
              Text('修改密码'),
            ],
          ),
        ),
        const PopupMenuItem<String>(
          value: 'logout',
          height: 40,
          child: Row(
            children: [
              Icon(Icons.logout, size: 18),
              SizedBox(width: 10),
              Text('退出登录'),
            ],
          ),
        ),
      ],
    ).then((value) {
      if (value == 'logout') {
        _logout();
      } else if (value == 'password') {
        showChangePasswordDialog(Get.context!);
      }
    });
  }
}

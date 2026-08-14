import 'package:flutter/material.dart';

import '../../app/routes/app_routes.dart';

/// 侧边/底部导航项定义。
class NavItem {
  const NavItem({
    required this.route,
    required this.label,
    required this.icon,
    required this.permissionKey,
    this.implemented = false,
  });

  final String route;
  final String label;
  final IconData icon;

  /// 对应后端权限 key，用于根据当前管理员权限过滤可见菜单。
  final String permissionKey;

  /// 是否已实现真实页面。
  final bool implemented;
}

/// 后台完整导航菜单。
const List<NavItem> kNavItems = <NavItem>[
  NavItem(
    route: AppRoutes.dashboard,
    label: '首页',
    icon: Icons.space_dashboard_outlined,
    permissionKey: '',
    implemented: true,
  ),
  NavItem(
    route: AppRoutes.users,
    label: '用户',
    icon: Icons.people_alt_outlined,
    permissionKey: 'users',
    implemented: true,
  ),
  NavItem(
    route: AppRoutes.reports,
    label: '举报',
    icon: Icons.flag_outlined,
    permissionKey: 'reports',
    implemented: true,
  ),
  NavItem(
    route: AppRoutes.moderation,
    label: '审查',
    icon: Icons.shield_outlined,
    permissionKey: 'moderation',
    implemented: true,
  ),
  NavItem(
    route: AppRoutes.visitorBans,
    label: '访客封禁',
    icon: Icons.person_off_outlined,
    permissionKey: 'visitor_bans',
    implemented: true,
  ),
  NavItem(
    route: AppRoutes.linkBlocklist,
    label: '链接',
    icon: Icons.link_off_outlined,
    permissionKey: 'link_blocklist',
    implemented: true,
  ),
  NavItem(
    route: AppRoutes.eggs,
    label: '虾蛋',
    icon: Icons.egg_outlined,
    permissionKey: 'eggs',
    implemented: true,
  ),
  NavItem(
    route: AppRoutes.featureGates,
    label: '开关',
    icon: Icons.toggle_on_outlined,
    permissionKey: 'feature_gates',
    implemented: true,
  ),
  NavItem(
    route: AppRoutes.appReleases,
    label: 'App',
    icon: Icons.system_update_outlined,
    permissionKey: 'app',
    implemented: true,
  ),
  NavItem(
    route: AppRoutes.connector,
    label: '插件',
    icon: Icons.cable_outlined,
    permissionKey: 'connector',
    implemented: true,
  ),
  NavItem(
    route: AppRoutes.gatewayWallets,
    label: '计费网关',
    icon: Icons.account_balance_wallet_outlined,
    permissionKey: 'gateway_billing',
    implemented: true,
  ),
  NavItem(
    route: AppRoutes.reachTasks,
    label: '触达',
    icon: Icons.campaign_outlined,
    permissionKey: 'app',
    implemented: true,
  ),
  NavItem(
    route: AppRoutes.admins,
    label: '管理员',
    icon: Icons.admin_panel_settings_outlined,
    permissionKey: 'admins',
    implemented: true,
  ),
  NavItem(
    route: AppRoutes.settings,
    label: '设置',
    icon: Icons.settings_outlined,
    permissionKey: 'settings',
    implemented: true,
  ),
];

/// 移动端底部栏主入口数量（其余进入"更多"）。
const int kBottomBarPrimaryCount = 4;

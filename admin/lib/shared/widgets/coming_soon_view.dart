import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../navigation/nav_items.dart';
import 'admin_scaffold.dart';

/// 未实现模块的占位页（保持统一导航骨架，便于先验证整体布局）。
class ComingSoonView extends StatelessWidget {
  const ComingSoonView({super.key});

  @override
  Widget build(BuildContext context) {
    final route = Get.currentRoute;
    final item = kNavItems.firstWhere(
      (e) => e.route == route,
      orElse: () => const NavItem(route: '', label: '模块', icon: Icons.widgets_outlined, permissionKey: ''),
    );
    return AdminScaffold(
      title: item.label,
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(item.icon, size: 56, color: Theme.of(context).disabledColor),
            const SizedBox(height: 16),
            Text('「${item.label}」即将上线',
                style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 8),
            Text('该模块正在开发中',
                style: TextStyle(color: Theme.of(context).hintColor)),
          ],
        ),
      ),
    );
  }
}

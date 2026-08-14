import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:intl/intl.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/filter_bottom_sheet.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'admin_user_item.dart';
import 'users_binding.dart';
import 'users_controller.dart';

/// 用户管理页：搜索 / 状态筛选 / 无限滚动 + 封禁、解封、解除审查禁言、解除登录锁定。
class UsersView extends StatelessWidget {
  const UsersView({super.key, this.onlineOnly = false});

  final bool onlineOnly;

  @override
  Widget build(BuildContext context) {
    final controller = Get.find<UsersController>(
      tag: onlineOnly ? UsersBinding.onlineUsersTag : null,
    );
    return AdminScaffold(
      title: controller.onlineOnly ? '当前在线用户' : '用户管理',
      actions: [
        IconButton(
          tooltip: '刷新',
          onPressed: controller.reload,
          icon: const Icon(Icons.refresh),
        ),
      ],
      body: Column(
        children: [
          _Toolbar(controller: controller),
          const Divider(height: 1),
          Expanded(
            child: LayoutBuilder(
              builder: (context, constraints) {
                final wide = constraints.maxWidth >= 760;
                return InfiniteListView<AdminUserItem>(
                  controller: controller,
                  emptyText: controller.onlineOnly ? '当前没有在线用户' : '没有符合条件的用户',
                  itemBuilder: (ctx, user, index) =>
                      _UserCard(user: user, controller: controller, wide: wide),
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

class _Toolbar extends StatelessWidget {
  const _Toolbar({required this.controller});

  final UsersController controller;

  /// 状态筛选的宽度阈值：宽于此值内联展示筛选，窄于此值用 BottomSheet。
  static const _wideThreshold = 600.0;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= _wideThreshold;
        return Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
          child: Row(
            children: [
              Expanded(
                child: TextField(
                  controller: controller.searchCtrl,
                  decoration: InputDecoration(
                    hintText: '搜索 ID / 账号 / 昵称 / 邮箱 / 手机号',
                    prefixIcon: const Icon(Icons.search),
                    isDense: true,
                    suffixIcon: IconButton(
                      icon: const Icon(Icons.arrow_forward),
                      onPressed: () =>
                          controller.applySearch(controller.searchCtrl.text),
                    ),
                  ),
                  onSubmitted: controller.applySearch,
                ),
              ),
              if (wide) ...[
                const SizedBox(width: 12),
                _InlineFilters(controller: controller),
              ] else ...[
                Obx(
                  () => FilterBadgeIcon(
                    activeCount: controller.statusFilter.value.isNotEmpty
                        ? 1
                        : 0,
                    onTap: () => _showFilterSheet(context),
                  ),
                ),
              ],
            ],
          ),
        );
      },
    );
  }

  void _showFilterSheet(BuildContext context) {
    FilterBottomSheet.show(
      context,
      title: '筛选条件',
      activeCount: controller.statusFilter.value.isNotEmpty ? 1 : 0,
      onReset: controller.resetFilters,
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('用户状态', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          Obx(
            () => SizedBox(
              width: double.infinity,
              child: SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: '', label: Text('全部')),
                  ButtonSegment(value: 'active', label: Text('正常')),
                  ButtonSegment(value: 'banned', label: Text('封禁')),
                ],
                selected: {controller.statusFilter.value},
                onSelectionChanged: (s) {
                  controller.changeStatus(s.first);
                },
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// 宽屏下的内联筛选控件。
class _InlineFilters extends StatelessWidget {
  const _InlineFilters({required this.controller});

  final UsersController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(
      () => SegmentedButton<String>(
        segments: const [
          ButtonSegment(value: '', label: Text('全部')),
          ButtonSegment(value: 'active', label: Text('正常')),
          ButtonSegment(value: 'banned', label: Text('封禁')),
        ],
        selected: {controller.statusFilter.value},
        onSelectionChanged: (s) => controller.changeStatus(s.first),
      ),
    );
  }
}

class _UserCard extends StatelessWidget {
  const _UserCard({
    required this.user,
    required this.controller,
    required this.wide,
  });

  final AdminUserItem user;
  final UsersController controller;
  final bool wide;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final df = DateFormat('yyyy-MM-dd HH:mm');
    return Card(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        child: Flex(
          direction: wide ? Axis.horizontal : Axis.vertical,
          crossAxisAlignment: wide
              ? CrossAxisAlignment.center
              : CrossAxisAlignment.start,
          children: [
            Expanded(
              flex: wide ? 1 : 0,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Flexible(
                        child: Text(
                          user.displayName,
                          style: theme.textTheme.titleMedium,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                      const SizedBox(width: 8),
                      _StatusChips(user: user),
                    ],
                  ),
                  const SizedBox(height: 4),
                  Text(
                    'ID ${user.id} · @${user.username}'
                    '${user.email.isNotEmpty ? ' · ${user.email}' : ''}'
                    '${user.phoneE164.isNotEmpty ? ' · ${user.phoneE164}' : ''}',
                    style: theme.textTheme.bodySmall,
                  ),
                  if (user.isBanned && user.bannedReason.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(top: 4),
                      child: Text(
                        '封禁原因：${user.bannedReason}',
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.error,
                        ),
                      ),
                    ),
                  if (user.createdAt != null)
                    Padding(
                      padding: const EdgeInsets.only(top: 4),
                      child: Text(
                        '注册于 ${df.format(user.createdAt!.toLocal())}',
                        style: theme.textTheme.bodySmall,
                      ),
                    ),
                ],
              ),
            ),
            SizedBox(width: wide ? 12 : 0, height: wide ? 0 : 10),
            _Actions(user: user, controller: controller),
          ],
        ),
      ),
    );
  }
}

class _StatusChips extends StatelessWidget {
  const _StatusChips({required this.user});

  final AdminUserItem user;

  @override
  Widget build(BuildContext context) {
    final chips = <Widget>[];
    if (user.isBanned) {
      chips.add(_chip('已封禁', StatusKind.danger));
    } else {
      chips.add(_chip('正常', StatusKind.success));
    }
    if (user.loginLocked) {
      chips.add(_chip('登录锁定', StatusKind.warning));
    }
    if (user.moderationMuted) {
      chips.add(
        _chip('审查禁言 ${user.moderationMuteSessionCount}', StatusKind.info),
      );
    }
    return Wrap(spacing: 6, runSpacing: 6, children: chips);
  }

  Widget _chip(String label, StatusKind kind) {
    final fg = AppStatusStyle.foreground(kind);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: AppStatusStyle.background(kind),
        borderRadius: BorderRadius.circular(AppRadius.chip),
      ),
      child: Text(
        label,
        style: TextStyle(fontSize: 12, color: fg, fontWeight: FontWeight.w600),
      ),
    );
  }
}

class _Actions extends StatelessWidget {
  const _Actions({required this.user, required this.controller});

  final AdminUserItem user;
  final UsersController controller;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        if (user.isBanned)
          OutlinedButton.icon(
            onPressed: () => controller.unban(user),
            icon: const Icon(Icons.lock_open, size: 18),
            label: const Text('解封'),
          )
        else
          OutlinedButton.icon(
            style: OutlinedButton.styleFrom(
              foregroundColor: Theme.of(context).colorScheme.error,
            ),
            onPressed: () => controller.ban(user),
            icon: const Icon(Icons.block, size: 18),
            label: const Text('封禁'),
          ),
        if (user.loginLocked)
          OutlinedButton.icon(
            onPressed: () => controller.unlockLogin(user),
            icon: const Icon(Icons.lock_clock, size: 18),
            label: const Text('解除登录锁定'),
          ),
        if (user.moderationMuted)
          OutlinedButton.icon(
            onPressed: () => controller.unmuteModeration(user),
            icon: const Icon(Icons.volume_up, size: 18),
            label: const Text('解除审查禁言'),
          ),
        if (user.phoneE164.isNotEmpty)
          OutlinedButton.icon(
            onPressed: () => controller.unbindPhone(user),
            icon: const Icon(Icons.phone_disabled, size: 18),
            label: const Text('解绑手机号'),
          ),
      ],
    );
  }
}

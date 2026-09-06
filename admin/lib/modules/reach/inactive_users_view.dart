import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'inactive_users_controller.dart';
import 'inactive_users_service.dart';

/// 「沉默用户触达」页：筛出近 N 天没有 agent 连接过的用户。
///
/// 名单走 /users/inactive-agent-users（users 权限）。发送入口已停用：原来走
/// /reach/direct 的邮件渠道只能从 no-reply 发件人发出，客户回信没人收得到，
/// 后端已经拿掉该渠道，这里只保留名单与筛选，跟进请走人工客服邮箱。
class InactiveUsersView extends GetView<InactiveUsersController> {
  const InactiveUsersView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '触达 · 沉默用户',
      actions: [
        IconButton(
          tooltip: '刷新',
          onPressed: controller.reloadFromFirstPage,
          icon: const Icon(Icons.refresh),
        ),
      ],
      body: Column(
        children: [
          _Toolbar(controller: controller),
          const Divider(height: 1),
          Expanded(
            child: InfiniteListView<InactiveAgentUser>(
              controller: controller,
              emptyText: '该条件下没有沉默用户',
              itemBuilder: (_, u, _) => _InactiveUserCard(u: u, c: controller),
            ),
          ),
          _SelectionBar(controller: controller),
        ],
      ),
    );
  }
}

class _Toolbar extends StatelessWidget {
  const _Toolbar({required this.controller});
  final InactiveUsersController controller;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
      child: Wrap(
        spacing: 16,
        runSpacing: 8,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: [
          Obx(
            () => DropdownButton<int>(
              value: controller.noAgentDays.value,
              underline: const SizedBox.shrink(),
              items: const [
                DropdownMenuItem(value: 7, child: Text('7 天没连过 agent')),
                DropdownMenuItem(value: 14, child: Text('14 天没连过 agent')),
                DropdownMenuItem(value: 30, child: Text('30 天没连过 agent')),
                DropdownMenuItem(value: 60, child: Text('60 天没连过 agent')),
                DropdownMenuItem(value: 90, child: Text('90 天没连过 agent')),
              ],
              onChanged: (v) => controller.applyDays(
                v ?? InactiveUsersController.defaultNoAgentDays,
              ),
            ),
          ),
          Obx(
            () => SegmentedButton<String>(
              segments: const [
                ButtonSegment(value: '', label: Text('全部区域')),
                ButtonSegment(value: 'cn', label: Text('国内')),
                ButtonSegment(value: 'global', label: Text('海外')),
              ],
              selected: {controller.region.value},
              onSelectionChanged: (s) => controller.changeRegion(s.first),
            ),
          ),
        ],
      ),
    );
  }
}

class _InactiveUserCard extends StatelessWidget {
  const _InactiveUserCard({required this.u, required this.c});
  final InactiveAgentUser u;
  final InactiveUsersController c;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final contacts = <String>[
      if (u.hasEmail) u.email,
      if (u.phoneMasked.isNotEmpty) u.phoneMasked,
    ];
    return Card(
      child: Obx(
        () => CheckboxListTile(
          value: c.selected.contains(u.userId),
          // 没绑邮箱的用户本期发不出去，直接禁掉勾选框。
          onChanged: u.hasEmail
              ? (v) => c.toggleSelected(u.userId, v ?? false)
              : null,
          controlAffinity: ListTileControlAffinity.leading,
          title: Text(
            u.nickname.isEmpty ? u.userId : u.nickname,
            style: theme.textTheme.titleSmall,
          ),
          subtitle: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const SizedBox(height: 2),
              Text(
                u.hasEmail
                    ? contacts.join(' · ')
                    : '未绑定邮箱，本期不发（${contacts.isEmpty ? '无联系方式' : contacts.join(' · ')}）',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: u.hasEmail ? null : AppPalette.danger,
                ),
              ),
              Text(
                '${u.agentTotal} 个 Agent · '
                '${u.neverConnected ? '从未连接过' : '最近连接 ${u.lastAgentConnectedAt}'} · '
                '注册 ${u.createdAt}',
                style: theme.textTheme.bodySmall,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _SelectionBar extends StatelessWidget {
  const _SelectionBar({required this.controller});
  final InactiveUsersController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      if (controller.items.isEmpty) return const SizedBox.shrink();
      final count = controller.selected.length;
      final selectable = controller.selectableCount;
      final allSelected = count > 0 && count >= selectable;
      return Material(
        elevation: 4,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          child: Row(
            children: [
              Checkbox(
                value: allSelected,
                tristate: true,
                onChanged: selectable == 0
                    ? null
                    : (_) => controller.selectAllLoaded(!allSelected),
              ),
              Expanded(
                child: Text(
                  '已选 $count / 可发 $selectable（已加载 ${controller.items.length}，共 ${controller.total.value}）',
                ),
              ),
              // 邮件渠道已停用：只能从 no-reply 发件人发出，客户回信没人收得到。
              Tooltip(
                message: '邮件渠道已停用，请改用人工客服邮箱一对一跟进',
                child: FilledButton.icon(
                  onPressed: null,
                  icon: const Icon(Icons.mail_outline, size: 18),
                  label: const Text('发邮件（已停用）'),
                ),
              ),
            ],
          ),
        ),
      );
    });
  }
}

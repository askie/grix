import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:intl/intl.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/filter_bottom_sheet.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'visitor_ban_item.dart';
import 'visitor_bans_controller.dart';

class VisitorBansView extends GetView<VisitorBansController> {
  const VisitorBansView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '访客封禁',
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
                final wide = constraints.maxWidth >= 820;
                return InfiniteListView<VisitorBanItem>(
                  controller: controller,
                  emptyText: '没有符合条件的访客封禁记录',
                  itemBuilder: (ctx, item, index) => _VisitorBanCard(
                    item: item,
                    controller: controller,
                    wide: wide,
                  ),
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

  final VisitorBansController controller;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= 680;
        return Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
          child: Row(
            children: [
              Expanded(
                child: TextField(
                  controller: controller.searchCtrl,
                  decoration: InputDecoration(
                    hintText: '搜索会话 / 访客 / 塘主 / 站点 / 页面',
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
              ] else
                Obx(
                  () => FilterBadgeIcon(
                    activeCount: controller.statusFilter.value != 'banned'
                        ? 1
                        : 0,
                    onTap: () => _showFilterSheet(context),
                  ),
                ),
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
      activeCount: controller.statusFilter.value != 'banned' ? 1 : 0,
      onReset: controller.resetFilters,
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('访客状态', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          Obx(
            () => SizedBox(
              width: double.infinity,
              child: SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: 'banned', label: Text('封禁')),
                  ButtonSegment(value: 'active', label: Text('活跃')),
                  ButtonSegment(value: 'closed', label: Text('关闭')),
                  ButtonSegment(value: 'all', label: Text('全部')),
                ],
                selected: {controller.statusFilter.value},
                onSelectionChanged: (s) => controller.changeStatus(s.first),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _InlineFilters extends StatelessWidget {
  const _InlineFilters({required this.controller});

  final VisitorBansController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(
      () => SegmentedButton<String>(
        segments: const [
          ButtonSegment(value: 'banned', label: Text('封禁')),
          ButtonSegment(value: 'active', label: Text('活跃')),
          ButtonSegment(value: 'closed', label: Text('关闭')),
          ButtonSegment(value: 'all', label: Text('全部')),
        ],
        selected: {controller.statusFilter.value},
        onSelectionChanged: (s) => controller.changeStatus(s.first),
      ),
    );
  }
}

class _VisitorBanCard extends StatelessWidget {
  const _VisitorBanCard({
    required this.item,
    required this.controller,
    required this.wide,
  });

  final VisitorBanItem item;
  final VisitorBansController controller;
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
                          item.visitorDisplayName,
                          style: theme.textTheme.titleMedium,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                      const SizedBox(width: 8),
                      _StatusChips(item: item),
                    ],
                  ),
                  const SizedBox(height: 4),
                  Text(
                    '访客 ${item.visitorId} · 会话 ${item.sessionId}',
                    style: theme.textTheme.bodySmall,
                    overflow: TextOverflow.ellipsis,
                  ),
                  const SizedBox(height: 4),
                  Text(
                    '塘主 ${item.ownerDisplayName} (${item.ownerUserId}) · 站点 ${item.siteName.isNotEmpty ? item.siteName : item.siteId}',
                    style: theme.textTheme.bodySmall,
                    overflow: TextOverflow.ellipsis,
                  ),
                  if (item.visitorEmail.isNotEmpty ||
                      item.lastInitIpPrefix.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(top: 4),
                      child: Text(
                        '${item.visitorEmail.isNotEmpty ? item.visitorEmail : ''}'
                        '${item.visitorEmail.isNotEmpty && item.lastInitIpPrefix.isNotEmpty ? ' · ' : ''}'
                        '${item.lastInitIpPrefix.isNotEmpty ? 'IP ${item.lastInitIpPrefix}' : ''}',
                        style: theme.textTheme.bodySmall,
                      ),
                    ),
                  if (item.lastPageUrl.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(top: 4),
                      child: Text(
                        item.lastPageUrl,
                        style: theme.textTheme.bodySmall,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                  if (item.updatedAt != null)
                    Padding(
                      padding: const EdgeInsets.only(top: 4),
                      child: Text(
                        '更新于 ${df.format(item.updatedAt!.toLocal())}',
                        style: theme.textTheme.bodySmall,
                      ),
                    ),
                ],
              ),
            ),
            SizedBox(width: wide ? 12 : 0, height: wide ? 0 : 10),
            if (item.isBanned)
              OutlinedButton.icon(
                onPressed: () => controller.unban(item),
                icon: const Icon(Icons.lock_open, size: 18),
                label: const Text('解封'),
              ),
          ],
        ),
      ),
    );
  }
}

class _StatusChips extends StatelessWidget {
  const _StatusChips({required this.item});

  final VisitorBanItem item;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 6,
      runSpacing: 6,
      children: [
        _chip(_statusLabel(item.status), _statusKind(item.status)),
        if (item.hasIpBan) _chip('含 IP 封禁', StatusKind.warning),
      ],
    );
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

  String _statusLabel(int status) {
    switch (status) {
      case 1:
        return '活跃';
      case 2:
        return '关闭';
      case 3:
        return '已封禁';
      default:
        return '未知';
    }
  }

  StatusKind _statusKind(int status) {
    switch (status) {
      case 1:
        return StatusKind.success;
      case 2:
        return StatusKind.info;
      case 3:
        return StatusKind.danger;
      default:
        return StatusKind.warning;
    }
  }
}

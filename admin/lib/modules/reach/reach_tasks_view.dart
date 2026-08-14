import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:intl/intl.dart';

import '../../app/routes/app_routes.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'reach_marketing_dialog.dart';
import 'reach_models.dart';
import 'reach_subscription_dialog.dart';
import 'reach_tasks_controller.dart';

class ReachTasksView extends GetView<ReachTasksController> {
  const ReachTasksView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '触达 · 任务',
      actions: [
        IconButton(
          tooltip: '模板管理',
          onPressed: () => Get.toNamed(AppRoutes.reachTemplates),
          icon: const Icon(Icons.description_outlined),
        ),
        IconButton(
          tooltip: '订阅概览',
          onPressed: () => ReachSubscriptionDialog.show(),
          icon: const Icon(Icons.people_outline),
        ),
        IconButton(
          tooltip: '创建营销任务',
          onPressed: () => ReachMarketingDialog.show(),
          icon: const Icon(Icons.add),
        ),
        IconButton(
          tooltip: '刷新',
          onPressed: controller.reloadFromFirstPage,
          icon: const Icon(Icons.refresh),
        ),
      ],
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            child: Obx(
              () => Row(
                children: [
                  _buildDropdown(
                    label: '状态',
                    value: controller.statusFilter.value,
                    items: const {
                      '': '全部',
                      'draft': '待发送',
                      'scheduled': '定时',
                      'sending': '发送中',
                      'sent': '已发送',
                      'paused': '已暂停',
                      'cancelled': '已取消',
                      'failed': '失败',
                    },
                    onChanged: controller.applyStatusFilter,
                  ),
                  const SizedBox(width: 12),
                  _buildDropdown(
                    label: '类型',
                    value: controller.kindFilter.value,
                    items: const {
                      '': '全部',
                      'system_event': '系统事件',
                      'marketing': '营销',
                    },
                    onChanged: controller.applyKindFilter,
                  ),
                ],
              ),
            ),
          ),
          Expanded(
            child: InfiniteListView<ReachTask>(
              controller: controller,
              emptyText: '暂无任务',
              itemBuilder: (_, task, _) => _TaskTile(task: task),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildDropdown({
    required String label,
    required String value,
    required Map<String, String> items,
    required ValueChanged<String> onChanged,
  }) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Text('$label: ', style: const TextStyle(fontSize: 13)),
        DropdownButton<String>(
          value: value,
          isDense: true,
          underline: const SizedBox.shrink(),
          items: items.entries
              .map((e) => DropdownMenuItem(value: e.key, child: Text(e.value)))
              .toList(),
          onChanged: (v) {
            if (v != null) onChanged(v);
          },
        ),
      ],
    );
  }
}

class _TaskTile extends StatelessWidget {
  const _TaskTile({required this.task});
  final ReachTask task;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      child: ListTile(
        title: Row(
          children: [
            _KindLabel(kind: task.kind),
            const SizedBox(width: 8),
            _StatusChip(status: task.status),
            if (task.abGroupId.isNotEmpty) ...[
              const SizedBox(width: 8),
              Chip(
                label: Text(
                  'A/B ${task.abVariant}',
                  style: const TextStyle(fontSize: 11),
                ),
                visualDensity: VisualDensity.compact,
                padding: EdgeInsets.zero,
              ),
            ],
          ],
        ),
        subtitle: Text(
          '${DateFormat('MM-dd HH:mm').format(task.createdAt)}'
          '${task.region.isNotEmpty ? '  ·  ${task.region}' : ''}',
          style: Theme.of(context).textTheme.bodySmall,
        ),
        trailing: const Icon(Icons.chevron_right, size: 20),
        onTap: () => Get.toNamed('/reach/tasks/${task.id}'),
      ),
    );
  }
}

class _KindLabel extends StatelessWidget {
  const _KindLabel({required this.kind});
  final String kind;

  @override
  Widget build(BuildContext context) {
    final label = switch (kind) {
      'system_event' => '系统',
      'marketing' => '营销',
      _ => kind,
    };
    return Text(
      label,
      style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w500),
    );
  }
}

class _StatusChip extends StatelessWidget {
  const _StatusChip({required this.status});
  final String status;

  @override
  Widget build(BuildContext context) {
    final (label, color) = switch (status) {
      'draft' => ('待发送', Colors.amber),
      'scheduled' => ('定时', Colors.purple),
      'sending' => ('发送中', Colors.blue),
      'sent' => ('已发送', Colors.green),
      'paused' => ('已暂停', Colors.orange),
      'cancelled' => ('已取消', Colors.red),
      'failed' => ('失败', Colors.red),
      _ => (status, Colors.grey),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 11,
          color: color,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }
}

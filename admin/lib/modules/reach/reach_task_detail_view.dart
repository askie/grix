import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:intl/intl.dart';

import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/async_view.dart';
import '../../shared/widgets/confirm_dialog.dart';
import 'reach_announcement_dialog.dart';
import 'reach_models.dart';
import 'reach_task_detail_controller.dart';

class ReachTaskDetailView extends GetView<ReachTaskDetailController> {
  const ReachTaskDetailView({super.key});

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final task = controller.task.value;
      return AdminScaffold(
        title: '任务详情',
        actions: [
          if (task != null) ..._buildActions(task),
          IconButton(
            tooltip: '刷新',
            onPressed: controller.loadDetail,
            icon: const Icon(Icons.refresh),
          ),
        ],
        body: AsyncView(
          loading: controller.loading.value,
          error: controller.error.value,
          isEmpty: task == null,
          onRetry: controller.loadDetail,
          emptyText: '任务不存在',
          builder: (_) => _buildBody(context, task!),
        ),
      );
    });
  }

  List<Widget> _buildActions(ReachTaskDetail task) {
    final actions = <Widget>[];
    if (task.isEditableAnnouncement) {
      actions.add(
        IconButton(
          tooltip: '编辑文案',
          onPressed: () async {
            final saved = await ReachAnnouncementDialog.show(
              taskId: task.id,
              content: task.content,
            );
            if (saved) controller.loadDetail();
          },
          icon: const Icon(Icons.edit_outlined),
        ),
      );
      actions.add(
        IconButton(
          tooltip: '发送公告',
          onPressed: () async {
            final ok = await ConfirmDialog.show(
              title: '发送公告',
              message: '确定向全部活跃用户群发此公告（站内信 + 邮件）？发送后无法撤回。',
              danger: true,
            );
            if (ok) controller.sendTask();
          },
          icon: const Icon(Icons.send_outlined),
        ),
      );
      actions.add(
        IconButton(
          tooltip: '作废',
          onPressed: () async {
            final ok = await ConfirmDialog.show(
              title: '作废公告',
              message: '确定作废此公告草稿？作废后该版本不会再发送公告。',
              danger: true,
            );
            if (ok) controller.cancelTask();
          },
          icon: Icon(Icons.cancel_outlined, color: Get.theme.colorScheme.error),
        ),
      );
    }
    if (task.status == 'scheduled') {
      actions.add(
        IconButton(
          tooltip: '取消',
          onPressed: () async {
            final ok = await ConfirmDialog.show(
              title: '取消任务',
              message: '确定取消此定时任务？取消后将不会发送。',
              danger: true,
            );
            if (ok) controller.cancelTask();
          },
          icon: Icon(Icons.cancel_outlined, color: Get.theme.colorScheme.error),
        ),
      );
    }
    if (task.status == 'sending') {
      actions.add(
        IconButton(
          tooltip: '暂停',
          onPressed: () => controller.pauseTask(),
          icon: const Icon(Icons.pause_circle_outline),
        ),
      );
      actions.add(
        IconButton(
          tooltip: '取消',
          onPressed: () async {
            final ok = await ConfirmDialog.show(
              title: '取消任务',
              message: '确定取消此任务？已发送的消息不会被撤回。',
              danger: true,
            );
            if (ok) controller.cancelTask();
          },
          icon: Icon(Icons.cancel_outlined, color: Get.theme.colorScheme.error),
        ),
      );
    }
    if (task.status == 'paused') {
      actions.add(
        IconButton(
          tooltip: '恢复',
          onPressed: () => controller.resumeTask(),
          icon: const Icon(Icons.play_circle_outline),
        ),
      );
      actions.add(
        IconButton(
          tooltip: '取消',
          onPressed: () async {
            final ok = await ConfirmDialog.show(
              title: '取消任务',
              message: '确定取消此任务？已发送的消息不会被撤回。',
              danger: true,
            );
            if (ok) controller.cancelTask();
          },
          icon: Icon(Icons.cancel_outlined, color: Get.theme.colorScheme.error),
        ),
      );
    }
    return actions;
  }

  Widget _buildBody(BuildContext context, ReachTaskDetail task) {
    final theme = Theme.of(context);
    final stats = controller.stats.value;
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        _SectionTitle('基本信息'),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                _InfoRow('ID', task.id),
                _InfoRow('类型', task.kind == 'marketing' ? '营销' : '系统事件'),
                _InfoRow('状态', task.statusLabel),
                if (task.eventKey.isNotEmpty) _InfoRow('事件', task.eventKey),
                if (task.templateId != '0' && task.templateId.isNotEmpty)
                  _InfoRow('模板ID', task.templateId),
                _InfoRow('通道', task.channels.join(', ')),
                if (task.region.isNotEmpty) _InfoRow('地区', task.region),
                if (task.abGroupId.isNotEmpty) ...[
                  _InfoRow('A/B组', task.abGroupId),
                  _InfoRow('变体', task.abVariant),
                ],
                _InfoRow(
                  '创建时间',
                  DateFormat('yyyy-MM-dd HH:mm:ss').format(task.createdAt),
                ),
                if (task.scheduledAt != null)
                  _InfoRow(
                    '计划时间',
                    DateFormat('yyyy-MM-dd HH:mm:ss').format(task.scheduledAt!),
                  ),
              ],
            ),
          ),
        ),
        if (task.kind == 'system_event' && !task.content.isEmpty) ...[
          const SizedBox(height: 16),
          _SectionTitle('公告文案'),
          _AnnouncementContentCard(content: task.content),
        ],
        if (stats != null) ...[
          const SizedBox(height: 16),
          _SectionTitle('统计'),
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      _StatCard('总发送', '${stats.totalLogs}', theme),
                      _StatCard('已打开', '${stats.opened}', theme),
                      _StatCard('已点击', '${stats.clicked}', theme),
                    ],
                  ),
                  const SizedBox(height: 8),
                  Row(
                    children: [
                      _StatCard(
                        '打开率',
                        '${(stats.openRate * 100).toStringAsFixed(1)}%',
                        theme,
                      ),
                      _StatCard(
                        '点击率',
                        '${(stats.clickRate * 100).toStringAsFixed(1)}%',
                        theme,
                      ),
                    ],
                  ),
                  if (stats.channels.isNotEmpty) ...[
                    const SizedBox(height: 12),
                    Text('通道分布', style: theme.textTheme.titleSmall),
                    const SizedBox(height: 4),
                    ...stats.channels.entries.map(
                      (e) => _InfoRow(e.key, '${e.value}'),
                    ),
                  ],
                  if (stats.regions.isNotEmpty) ...[
                    const SizedBox(height: 12),
                    Text('地区分布', style: theme.textTheme.titleSmall),
                    const SizedBox(height: 4),
                    ...stats.regions.entries.map(
                      (e) => _InfoRow(e.key, '${e.value}'),
                    ),
                  ],
                ],
              ),
            ),
          ),
        ],
        if (task.sendLogs.isNotEmpty) ...[
          const SizedBox(height: 16),
          _SectionTitle('发送日志 (最近 ${task.sendLogs.length} 条)'),
          ...task.sendLogs.map(
            (log) => Card(
              margin: const EdgeInsets.symmetric(vertical: 4),
              child: ListTile(
                dense: true,
                title: Text('用户 ${log.userId}  ·  ${log.channel}'),
                subtitle: Text(
                  '${log.status}'
                  '${log.openedAt != null ? '  ·  打开 ${DateFormat('MM-dd HH:mm').format(log.openedAt!)}' : ''}'
                  '${log.clickedAt != null ? '  ·  点击 ${DateFormat('MM-dd HH:mm').format(log.clickedAt!)}' : ''}',
                ),
                trailing: _SendLogStatusIcon(status: log.status),
              ),
            ),
          ),
        ],
      ],
    );
  }
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle(this.text);
  final String text;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Text(
        text,
        style: Theme.of(
          context,
        ).textTheme.titleSmall?.copyWith(fontWeight: FontWeight.w600),
      ),
    );
  }
}

class _InfoRow extends StatelessWidget {
  const _InfoRow(this.label, this.value);
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 3),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 80,
            child: Text(
              label,
              style: TextStyle(
                fontSize: 13,
                color: Theme.of(context).hintColor,
              ),
            ),
          ),
          Expanded(child: Text(value, style: const TextStyle(fontSize: 13))),
        ],
      ),
    );
  }
}

class _StatCard extends StatelessWidget {
  const _StatCard(this.label, this.value, this.theme);
  final String label;
  final String value;
  final ThemeData theme;

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 4),
        child: Column(
          children: [
            Text(
              value,
              style: theme.textTheme.headlineSmall?.copyWith(
                fontWeight: FontWeight.w600,
              ),
            ),
            Text(label, style: theme.textTheme.bodySmall),
          ],
        ),
      ),
    );
  }
}

class _AnnouncementContentCard extends StatelessWidget {
  const _AnnouncementContentCard({required this.content});
  final ReachAnnouncementContent content;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _buildLocale(context, '中文', content.zh),
            const Divider(height: 24),
            _buildLocale(context, 'English', content.en),
          ],
        ),
      ),
    );
  }

  Widget _buildLocale(
    BuildContext context,
    String label,
    ReachAnnouncementLocale locale,
  ) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          label,
          style: theme.textTheme.titleSmall?.copyWith(
            fontWeight: FontWeight.w600,
          ),
        ),
        const SizedBox(height: 6),
        _InfoRow('标题', locale.title),
        if (locale.body.isNotEmpty) _InfoRow('正文', locale.body),
        if (locale.emailSubject.isNotEmpty)
          _InfoRow('邮件主题', locale.emailSubject),
        if (locale.emailIntro.isNotEmpty) _InfoRow('邮件导语', locale.emailIntro),
      ],
    );
  }
}

class _SendLogStatusIcon extends StatelessWidget {
  const _SendLogStatusIcon({required this.status});
  final String status;

  @override
  Widget build(BuildContext context) {
    final (icon, color) = switch (status) {
      'sent' => (Icons.check_circle_outline, Colors.green),
      'failed' => (Icons.error_outline, Colors.red),
      'pending' => (Icons.schedule, Colors.orange),
      'skipped' => (Icons.skip_next, Colors.grey),
      _ => (Icons.help_outline, Colors.grey),
    };
    return Icon(icon, size: 20, color: color);
  }
}

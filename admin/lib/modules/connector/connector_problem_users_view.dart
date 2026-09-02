import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/filter_bottom_sheet.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'connector_problem_users_controller.dart';
import 'connector_service.dart';

/// 「问题用户」Tab：按版本列出升级仍失败的用户，勾选后手动发送升级失败告知。
class ConnectorProblemUsersTab
    extends GetView<ConnectorProblemUsersController> {
  const ConnectorProblemUsersTab({super.key});

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        _Toolbar(controller: controller),
        const Divider(height: 1),
        Expanded(
          child: Obx(() {
            if (controller.version.value.isEmpty) {
              return const Center(child: Text('请输入版本号后查询问题用户'));
            }
            return InfiniteListView<ConnectorProblemUser>(
              controller: controller,
              emptyText: '该版本没有待处理的问题用户',
              itemBuilder: (_, u, _) => _ProblemUserCard(u: u, c: controller),
            );
          }),
        ),
        _SelectionBar(controller: controller),
      ],
    );
  }
}

class _Toolbar extends StatelessWidget {
  const _Toolbar({required this.controller});
  final ConnectorProblemUsersController controller;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
      child: Row(
        children: [
          Expanded(
            child: TextField(
              controller: controller.versionCtrl,
              decoration: InputDecoration(
                hintText: '目标版本号，如 4.3.5',
                prefixIcon: const Icon(Icons.search),
                isDense: true,
                suffixIcon: IconButton(
                  icon: const Icon(Icons.arrow_forward),
                  onPressed: () =>
                      controller.applyVersion(controller.versionCtrl.text),
                ),
              ),
              onSubmitted: controller.applyVersion,
            ),
          ),
          const SizedBox(width: 12),
          Obx(
            () => FilterBadgeIcon(
              activeCount: controller.activeFilterCount,
              onTap: () => _showFilterSheet(context),
            ),
          ),
        ],
      ),
    );
  }

  void _showFilterSheet(BuildContext context) {
    FilterBottomSheet.show(
      context,
      title: '筛选条件',
      activeCount: controller.activeFilterCount,
      onReset: controller.resetFilters,
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('上报状态', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          Obx(
            () => SizedBox(
              width: double.infinity,
              child: SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: '', label: Text('失败+回滚')),
                  ButtonSegment(value: 'failed', label: Text('仅失败')),
                  ButtonSegment(value: 'rolled_back', label: Text('仅回滚')),
                ],
                selected: {controller.statusFilter.value},
                onSelectionChanged: (s) => controller.changeStatus(s.first),
              ),
            ),
          ),
          const SizedBox(height: 8),
          Obx(
            () => CheckboxListTile(
              contentPadding: EdgeInsets.zero,
              dense: true,
              title: const Text('包含平台不支持（WINDOWS_UPGRADE_UNSUPPORTED）'),
              subtitle: const Text('默认排除：平台不支持不是故障'),
              value: controller.includeUnsupported.value,
              onChanged: (v) => controller.toggleUnsupported(v ?? false),
            ),
          ),
        ],
      ),
    );
  }
}

class _ProblemUserCard extends StatelessWidget {
  const _ProblemUserCard({required this.u, required this.c});
  final ConnectorProblemUser u;
  final ConnectorProblemUsersController c;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final contacts = <String>[
      if (u.hasEmail) u.email,
      if (u.hasPhone) u.phoneMasked,
    ];
    return Card(
      child: Obx(
        () => CheckboxListTile(
          value: c.selected.contains(u.userId),
          onChanged: (v) => c.toggleSelected(u.userId, v ?? false),
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
                contacts.isEmpty ? '无邮箱/手机号，无法触达' : contacts.join(' · '),
                style: theme.textTheme.bodySmall?.copyWith(
                  color: contacts.isEmpty ? AppPalette.danger : null,
                ),
              ),
              Text(
                '${u.failedHosts} 台机器 · ${u.agentIds.length} 个 Agent · ${u.lastReportedAt}',
                style: theme.textTheme.bodySmall,
              ),
              if (u.errorCodes.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: 4),
                  child: Wrap(
                    spacing: 6,
                    runSpacing: 4,
                    children: [
                      for (final code in u.errorCodes)
                        Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 6,
                            vertical: 2,
                          ),
                          decoration: BoxDecoration(
                            color: AppPalette.dangerSoft,
                            borderRadius: BorderRadius.circular(4),
                          ),
                          child: Text(
                            code,
                            style: const TextStyle(
                              fontSize: 11,
                              color: AppPalette.danger,
                            ),
                          ),
                        ),
                    ],
                  ),
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
  final ConnectorProblemUsersController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      if (controller.items.isEmpty) return const SizedBox.shrink();
      final count = controller.selected.length;
      final allSelected = count > 0 && count >= controller.items.length;
      return Material(
        elevation: 4,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          child: Row(
            children: [
              Checkbox(
                value: allSelected,
                tristate: true,
                onChanged: (_) => controller.selectAllLoaded(!allSelected),
              ),
              Expanded(
                child: Text(
                  '已选 $count / 已加载 ${controller.items.length}（共 ${controller.total.value}）',
                ),
              ),
              FilledButton.icon(
                onPressed: count == 0 || controller.sending.value
                    ? null
                    : () => _openComposer(context),
                icon: const Icon(Icons.campaign_outlined, size: 18),
                label: Text(controller.sending.value ? '发送中…' : '通知选中'),
              ),
            ],
          ),
        ),
      );
    });
  }

  Future<void> _openComposer(BuildContext context) async {
    final title = TextEditingController(text: '连接器升级失败，需要手动处理');
    final body = TextEditingController(
      text:
          '你的 Grix 连接器在升级到 ${controller.version.value} 时失败了。\n\n'
          '请在电脑上重新运行安装命令完成升级；如果仍然失败，直接回复这条消息我们跟进。',
    );
    // 默认邮件：短信模板号还没报备，auto 保留为显式可选项。
    String channel = 'email';
    ConnectorNotifyPreview? preview;
    var previewing = false;

    final confirmed = await Get.dialog<bool>(
      StatefulBuilder(
        builder: (ctx, setState) => AlertDialog(
          title: Text('通知 ${controller.selected.length} 位用户'),
          scrollable: true,
          content: SizedBox(
            width: 520,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                SegmentedButton<String>(
                  segments: const [
                    ButtonSegment(value: 'auto', label: Text('自动')),
                    ButtonSegment(value: 'email', label: Text('邮件')),
                    ButtonSegment(value: 'sms', label: Text('短信')),
                  ],
                  selected: {channel},
                  onSelectionChanged: (s) => setState(() => channel = s.first),
                ),
                const SizedBox(height: 4),
                Text(
                  '默认只发邮件；自动 = 先邮件，失败或无邮箱再走短信（短信模板号未配置时会返回未配置）',
                  style: Theme.of(ctx).textTheme.bodySmall,
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: title,
                  decoration: const InputDecoration(labelText: '标题（邮件主题）'),
                ),
                const SizedBox(height: 8),
                TextField(
                  controller: body,
                  maxLines: 6,
                  decoration: const InputDecoration(
                    labelText: '正文（支持 Markdown）',
                  ),
                ),
                const SizedBox(height: 12),
                Row(
                  children: [
                    OutlinedButton.icon(
                      onPressed: previewing
                          ? null
                          : () async {
                              setState(() => previewing = true);
                              final p = await controller.preview(
                                title.text.trim(),
                                body.text.trim(),
                              );
                              // 预览是网络调用，等待期间弹窗可能已被关掉。
                              if (!ctx.mounted) return;
                              setState(() {
                                preview = p;
                                previewing = false;
                              });
                            },
                      icon: const Icon(Icons.visibility_outlined, size: 18),
                      label: Text(previewing ? '生成中…' : '预览'),
                    ),
                  ],
                ),
                if (preview != null) ...[
                  const SizedBox(height: 12),
                  _PreviewPanel(preview: preview!),
                ],
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Get.back(result: false),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Get.back(result: true),
              child: const Text('发送'),
            ),
          ],
        ),
      ),
    );

    if (confirmed == true) {
      final results = await controller.notify(
        channel: channel,
        title: title.text.trim(),
        body: body.text.trim(),
      );
      if (results != null) {
        await _showResults(results);
        await controller.reloadFromFirstPage();
      }
    }
    title.dispose();
    body.dispose();
  }

  Future<void> _showResults(List<ConnectorNotifyResult> results) {
    final sent = results.where((r) => r.isSent).length;
    return Get.dialog<void>(
      AlertDialog(
        title: Text('发送结果：$sent / ${results.length} 成功'),
        content: SizedBox(
          width: 460,
          child: ListView(
            shrinkWrap: true,
            children: [
              for (final r in results)
                ListTile(
                  dense: true,
                  contentPadding: EdgeInsets.zero,
                  title: Text(
                    '${r.userId} · ${r.statusLabel}${r.channel.isEmpty ? '' : ' · ${r.channel}'}',
                  ),
                  subtitle: r.error.isEmpty
                      ? null
                      : Text(
                          r.error,
                          style: const TextStyle(
                            color: AppPalette.danger,
                            fontSize: 12,
                          ),
                        ),
                  trailing: Icon(
                    r.isSent ? Icons.check_circle_outline : Icons.error_outline,
                    color: r.isSent ? AppPalette.success : AppPalette.danger,
                    size: 20,
                  ),
                ),
            ],
          ),
        ),
        actions: [
          TextButton(onPressed: () => Get.back(), child: const Text('知道了')),
        ],
      ),
    );
  }
}

class _PreviewPanel extends StatelessWidget {
  const _PreviewPanel({required this.preview});
  final ConnectorNotifyPreview preview;

  /// 后台没有 HTML 渲染器，去标签后给文本预览，足够核对文案。
  static String _stripHtml(String html) => html
      .replaceAll(RegExp(r'<br\s*/?>', caseSensitive: false), '\n')
      .replaceAll(
        RegExp(r'</(p|div|h[1-6]|li|tr)>', caseSensitive: false),
        '\n',
      )
      .replaceAll(RegExp(r'<[^>]+>'), '')
      .replaceAll('&nbsp;', ' ')
      .replaceAll('&amp;', '&')
      .replaceAll('&lt;', '<')
      .replaceAll('&gt;', '>')
      .replaceAll(RegExp(r'\n{3,}'), '\n\n')
      .trim();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('邮件预览', style: theme.textTheme.titleSmall),
        const SizedBox(height: 4),
        if (preview.emailError.isNotEmpty)
          Text(
            preview.emailError,
            style: const TextStyle(color: AppPalette.danger, fontSize: 12),
          )
        else ...[
          Text('主题：${preview.emailSubject}', style: theme.textTheme.bodySmall),
          const SizedBox(height: 4),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(10),
            decoration: BoxDecoration(
              color: AppPalette.infoSoft,
              borderRadius: BorderRadius.circular(6),
            ),
            child: SelectableText(
              _stripHtml(preview.emailHtml),
              style: theme.textTheme.bodySmall,
            ),
          ),
        ],
        const SizedBox(height: 12),
        Text('短信预览', style: theme.textTheme.titleSmall),
        const SizedBox(height: 4),
        if (preview.smsError.isNotEmpty)
          Text(
            '短信通道不可用：${preview.smsError}',
            style: const TextStyle(color: AppPalette.danger, fontSize: 12),
          ),
        Container(
          width: double.infinity,
          padding: const EdgeInsets.all(10),
          decoration: BoxDecoration(
            color: AppPalette.infoSoft,
            borderRadius: BorderRadius.circular(6),
          ),
          child: SelectableText(
            preview.smsText,
            style: theme.textTheme.bodySmall,
          ),
        ),
      ],
    );
  }
}

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'inactive_users_controller.dart';
import 'inactive_users_service.dart';

/// 「沉默用户触达」页：筛出近 N 天没有 agent 连接过的用户，勾选后发模板邮件。
///
/// 需要同时具备「用户」与「触达」权限：名单走 /users/inactive-agent-users（users 权限），
/// 发送走 /reach/direct（app 权限）。
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
          Obx(() => DropdownButton<int>(
                value: controller.noAgentDays.value,
                underline: const SizedBox.shrink(),
                items: const [
                  DropdownMenuItem(value: 7, child: Text('7 天没连过 agent')),
                  DropdownMenuItem(value: 14, child: Text('14 天没连过 agent')),
                  DropdownMenuItem(value: 30, child: Text('30 天没连过 agent')),
                  DropdownMenuItem(value: 60, child: Text('60 天没连过 agent')),
                  DropdownMenuItem(value: 90, child: Text('90 天没连过 agent')),
                ],
                onChanged: (v) => controller.applyDays(v ?? InactiveUsersController.defaultNoAgentDays),
              )),
          Obx(() => SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: '', label: Text('全部区域')),
                  ButtonSegment(value: 'cn', label: Text('国内')),
                  ButtonSegment(value: 'global', label: Text('海外')),
                ],
                selected: {controller.region.value},
                onSelectionChanged: (s) => controller.changeRegion(s.first),
              )),
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
      child: Obx(() => CheckboxListTile(
            value: c.selected.contains(u.userId),
            // 没绑邮箱的用户本期发不出去，直接禁掉勾选框。
            onChanged: u.hasEmail ? (v) => c.toggleSelected(u.userId, v ?? false) : null,
            controlAffinity: ListTileControlAffinity.leading,
            title: Text(u.nickname.isEmpty ? u.userId : u.nickname, style: theme.textTheme.titleSmall),
            subtitle: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              const SizedBox(height: 2),
              Text(
                u.hasEmail ? contacts.join(' · ') : '未绑定邮箱，本期不发（${contacts.isEmpty ? '无联系方式' : contacts.join(' · ')}）',
                style: theme.textTheme.bodySmall?.copyWith(color: u.hasEmail ? null : AppPalette.danger),
              ),
              Text(
                '${u.agentTotal} 个 Agent · '
                '${u.neverConnected ? '从未连接过' : '最近连接 ${u.lastAgentConnectedAt}'} · '
                '注册 ${u.createdAt}',
                style: theme.textTheme.bodySmall,
              ),
            ]),
          )),
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
          child: Row(children: [
            Checkbox(
              value: allSelected,
              tristate: true,
              onChanged: selectable == 0 ? null : (_) => controller.selectAllLoaded(!allSelected),
            ),
            Expanded(
              child: Text('已选 $count / 可发 $selectable（已加载 ${controller.items.length}，共 ${controller.total.value}）'),
            ),
            FilledButton.icon(
              onPressed: count == 0 || controller.sending.value ? null : () => _openComposer(context),
              icon: const Icon(Icons.mail_outline, size: 18),
              label: Text(controller.sending.value ? '发送中…' : '发邮件'),
            ),
          ]),
        ),
      );
    });
  }

  Future<void> _openComposer(BuildContext context) async {
    final title = TextEditingController(text: '你的 Grix Agent 还在等你');
    final body = TextEditingController(
      text: '好久没见你的 Agent 上线了。\n\n'
          '打开 Grix，把电脑上的连接器跑起来，就能继续让 Agent 帮你干活。',
    );
    final templateId = TextEditingController(
      text: controller.defaultTemplateId.value > 0 ? '${controller.defaultTemplateId.value}' : '',
    );
    ReachEmailPreview? preview;
    var previewing = false;

    final confirmed = await Get.dialog<bool>(StatefulBuilder(
      builder: (ctx, setState) => AlertDialog(
        title: Text('给 ${controller.selected.length} 位沉默用户发邮件'),
        scrollable: true,
        content: SizedBox(
          width: 520,
          child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text('本期只走邮件，不会兜底短信；海外用户没有订阅记录的会被后端按未订阅跳过。',
                style: Theme.of(ctx).textTheme.bodySmall),
            const SizedBox(height: 12),
            TextField(controller: title, decoration: const InputDecoration(labelText: '标题（邮件主题）')),
            const SizedBox(height: 8),
            TextField(controller: body, maxLines: 6, decoration: const InputDecoration(labelText: '正文（支持 Markdown，填进模板的 {body}）')),
            const SizedBox(height: 8),
            TextField(
              controller: templateId,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(labelText: '阿里云模板 ID（留空用后端默认模板）'),
            ),
            const SizedBox(height: 12),
            Row(children: [
              OutlinedButton.icon(
                onPressed: previewing
                    ? null
                    : () async {
                        setState(() => previewing = true);
                        final p = await controller.preview(
                          title.text.trim(),
                          body.text.trim(),
                          int.tryParse(templateId.text.trim()) ?? 0,
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
            ]),
            if (preview != null) ...[
              const SizedBox(height: 12),
              _PreviewPanel(preview: preview!),
            ],
          ]),
        ),
        actions: [
          TextButton(onPressed: () => Get.back(result: false), child: const Text('取消')),
          FilledButton(onPressed: () => Get.back(result: true), child: const Text('发送')),
        ],
      ),
    ));

    if (confirmed == true) {
      final results = await controller.send(
        title: title.text.trim(),
        body: body.text.trim(),
        templateId: int.tryParse(templateId.text.trim()) ?? controller.defaultTemplateId.value,
      );
      if (results != null) {
        await _showResults(results);
        await controller.reloadFromFirstPage();
      }
    }
    title.dispose();
    body.dispose();
    templateId.dispose();
  }

  Future<void> _showResults(List<InactiveReachResult> results) {
    final sent = results.where((r) => r.isSent).length;
    return Get.dialog<void>(AlertDialog(
      title: Text('发送结果：$sent / ${results.length} 成功'),
      content: SizedBox(
        width: 460,
        child: ListView(shrinkWrap: true, children: [
          for (final r in results)
            ListTile(
              dense: true,
              contentPadding: EdgeInsets.zero,
              title: Text('${r.userId} · ${r.statusLabel}${r.channel.isEmpty ? '' : ' · ${r.channel}'}'),
              subtitle: r.error.isEmpty ? null : Text(r.error, style: const TextStyle(color: AppPalette.danger, fontSize: 12)),
              trailing: Icon(
                r.isSent ? Icons.check_circle_outline : Icons.error_outline,
                color: r.isSent ? AppPalette.success : AppPalette.danger,
                size: 20,
              ),
            ),
        ]),
      ),
      actions: [TextButton(onPressed: () => Get.back(), child: const Text('知道了'))],
    ));
  }
}

class _PreviewPanel extends StatelessWidget {
  const _PreviewPanel({required this.preview});
  final ReachEmailPreview preview;

  /// 后台没有 HTML 渲染器，去标签后给文本预览，足够核对文案与变量是否替换掉了。
  static String _stripHtml(String html) => html
      .replaceAll(RegExp(r'<br\s*/?>', caseSensitive: false), '\n')
      .replaceAll(RegExp(r'</(p|div|h[1-6]|li|tr)>', caseSensitive: false), '\n')
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
    if (preview.error.isNotEmpty) {
      return Text('模板渲染失败：${preview.error}', style: const TextStyle(color: AppPalette.danger, fontSize: 12));
    }
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Text('邮件预览（模板 ${preview.templateId}）', style: theme.textTheme.titleSmall),
      const SizedBox(height: 4),
      Text('主题：${preview.subject}', style: theme.textTheme.bodySmall),
      const SizedBox(height: 4),
      Container(
        width: double.infinity,
        padding: const EdgeInsets.all(10),
        decoration: BoxDecoration(color: AppPalette.infoSoft, borderRadius: BorderRadius.circular(6)),
        child: SelectableText(_stripHtml(preview.html), style: theme.textTheme.bodySmall),
      ),
    ]);
  }
}

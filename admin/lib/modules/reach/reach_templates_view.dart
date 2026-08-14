import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:intl/intl.dart';

import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/confirm_dialog.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'reach_models.dart';
import 'reach_template_dialog.dart';
import 'reach_templates_controller.dart';

class ReachTemplatesView extends GetView<ReachTemplatesController> {
  const ReachTemplatesView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '触达 · 模板',
      actions: [
        IconButton(
          tooltip: '新建模板',
          onPressed: () => ReachTemplateDialog.show(),
          icon: const Icon(Icons.add),
        ),
        IconButton(
          tooltip: '刷新',
          onPressed: controller.reloadFromFirstPage,
          icon: const Icon(Icons.refresh),
        ),
      ],
      body: InfiniteListView<ReachTemplate>(
        controller: controller,
        emptyText: '暂无模板',
        itemBuilder: (_, tpl, _) => _TemplateTile(
          template: tpl,
          onDelete: () => _confirmDelete(tpl),
        ),
      ),
    );
  }

  Future<void> _confirmDelete(ReachTemplate tpl) async {
    final ok = await ConfirmDialog.show(
      title: '删除模板',
      message: '确定删除模板「${tpl.name}」？',
      danger: true,
    );
    if (ok) controller.deleteTemplate(tpl.id);
  }
}

class _TemplateTile extends StatelessWidget {
  const _TemplateTile({required this.template, required this.onDelete});
  final ReachTemplate template;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      child: ListTile(
        title: Text(template.name),
        subtitle: Text(
          '${template.title.isNotEmpty ? template.title : '无标题'}'
          '  ·  ${DateFormat('MM-dd HH:mm').format(template.createdAt)}',
          style: Theme.of(context).textTheme.bodySmall,
        ),
        trailing: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            IconButton(
              tooltip: '编辑',
              icon: const Icon(Icons.edit_outlined, size: 20),
              onPressed: () => ReachTemplateDialog.show(template: template),
            ),
            IconButton(
              tooltip: '删除',
              icon: Icon(Icons.delete_outline,
                  size: 20, color: Theme.of(context).colorScheme.error),
              onPressed: onDelete,
            ),
          ],
        ),
        onTap: () => ReachTemplateDialog.show(template: template),
      ),
    );
  }
}

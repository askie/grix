import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../shared/widgets/admin_scaffold.dart';
import 'system_controls_controller.dart';
import 'system_controls_models.dart';

class SystemControlsView extends GetView<SystemControlsController> {
  const SystemControlsView({super.key});

  @override
  Widget build(BuildContext context) => Obx(
    () => AdminScaffold(
      title: '系统控制',
      actions: [
        IconButton(
          tooltip: '刷新',
          onPressed: controller.busy ? null : controller.load,
          icon: const Icon(Icons.refresh),
        ),
      ],
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 640),
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              if (controller.loading.value) const LinearProgressIndicator(),
              if (controller.error.value != null) ...[
                Text(
                  '读取失败：${controller.error.value}',
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
                TextButton(
                  onPressed: controller.busy ? null : controller.load,
                  child: const Text('重试读取'),
                ),
              ],
              for (final item in controller.items) _control(context, item),
            ],
          ),
        ),
      ),
    ),
  );

  Widget _control(BuildContext context, SystemControl item) {
    final saving = controller.savingKey.value == item.key;
    final error = controller.saveErrors[item.key];
    final status = controller.error.value != null
        ? '上次确认'
        : controller.savedKeys.contains(item.key)
        ? '已保存'
        : '当前已生效';
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(item.label, style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 8),
            Text(item.description),
            if (item.valueType == 'boolean') ...[
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: Text(controller.drafts[item.key] == true ? '允许' : '禁止'),
                value: controller.drafts[item.key] ?? false,
                onChanged: controller.busy
                    ? null
                    : (value) => controller.edit(item, value),
              ),
              Text('$status：${item.value == true ? '允许' : '禁止'}'),
              if (controller.dirty(item)) const Text('有未保存的修改'),
              if (error != null)
                Text(
                  error,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              const SizedBox(height: 12),
              Align(
                alignment: Alignment.centerRight,
                child: FilledButton(
                  onPressed: controller.busy
                      ? null
                      : () => controller.save(item),
                  child: Text(
                    saving
                        ? '保存中…'
                        : error != null
                        ? '重试保存'
                        : '保存',
                  ),
                ),
              ),
            ] else
              Text('当前客户端不支持编辑此参数（${item.valueType}），请升级'),
          ],
        ),
      ),
    );
  }
}

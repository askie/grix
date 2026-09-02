import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/async_view.dart';
import 'moderation_settings_controller.dart';

/// 内容审查设置页：开关 / 累计禁言阈值 / 关键词列表。
class ModerationSettingsView extends GetView<ModerationSettingsController> {
  const ModerationSettingsView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '内容审查设置',
      body: Obx(
        () => AsyncView(
          loading: controller.loading.value,
          error: controller.error.value,
          isEmpty: false,
          onRetry: controller.load,
          builder: (_) => _Form(c: controller),
        ),
      ),
    );
  }
}

class _Form extends StatelessWidget {
  const _Form({required this.c});

  final ModerationSettingsController c;

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        Card(
          child: Column(
            children: [
              Obx(
                () => SwitchListTile(
                  title: const Text('启用内容审查'),
                  subtitle: const Text('开启后将按关键词命中进行撤回与禁言'),
                  value: c.enabled.value,
                  onChanged: (v) => c.enabled.value = v,
                ),
              ),
              const Divider(height: 1),
              Padding(
                padding: const EdgeInsets.all(16),
                child: Row(
                  children: [
                    const Expanded(
                      child: Text('累计命中禁言阈值', style: TextStyle(fontSize: 14)),
                    ),
                    SizedBox(
                      width: 100,
                      child: TextField(
                        controller: c.thresholdCtrl,
                        keyboardType: TextInputType.number,
                        textAlign: TextAlign.center,
                        decoration: const InputDecoration(isDense: true),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
        const SizedBox(height: 16),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  '敏感关键词',
                  style: TextStyle(fontSize: 14, fontWeight: FontWeight.w700),
                ),
                const SizedBox(height: 4),
                const Text(
                  '每行一个关键词',
                  style: TextStyle(
                    fontSize: 12,
                    color: AppPalette.textTertiary,
                  ),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: c.keywordsCtrl,
                  maxLines: 10,
                  minLines: 5,
                  decoration: const InputDecoration(
                    hintText: '例如：\n关键词1\n关键词2',
                    alignLabelWithHint: true,
                  ),
                ),
              ],
            ),
          ),
        ),
        const SizedBox(height: 20),
        Obx(
          () => FilledButton(
            onPressed: c.saving.value ? null : c.save,
            child: c.saving.value
                ? const SizedBox(
                    height: 20,
                    width: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Text('保存设置'),
          ),
        ),
      ],
    );
  }
}

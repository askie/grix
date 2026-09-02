import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/async_view.dart';
import 'link_blocklist_settings_controller.dart';

/// 链接黑名单设置页：总开关 / 自家域名白名单 / 缓存 TTL / 外部情报。
class LinkBlocklistSettingsView
    extends GetView<LinkBlocklistSettingsController> {
  const LinkBlocklistSettingsView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '链接黑名单设置',
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
  final LinkBlocklistSettingsController c;

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
                  title: const Text('启用链接安全防护'),
                  subtitle: const Text('开启后用户点击外链会经服务端校验'),
                  value: c.enabled.value,
                  onChanged: (v) => c.enabled.value = v,
                ),
              ),
              const Divider(height: 1),
              Obx(
                () => SwitchListTile(
                  title: const Text('外部威胁情报'),
                  subtitle: const Text('对接国内反诈库 / Google Safe Browsing（P2）'),
                  value: c.externalIntelEnable.value,
                  onChanged: (v) => c.externalIntelEnable.value = v,
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
                  '自家域名白名单（点击直通，不走校验）',
                  style: TextStyle(fontSize: 14, fontWeight: FontWeight.w700),
                ),
                const SizedBox(height: 4),
                const Text(
                  '每行一个域名，例如 grix.dhf.pub',
                  style: TextStyle(
                    fontSize: 12,
                    color: AppPalette.textTertiary,
                  ),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: c.whitelistCtrl,
                  maxLines: 8,
                  minLines: 4,
                  decoration: const InputDecoration(
                    hintText: 'grix.dhf.pub\nexample.com',
                    alignLabelWithHint: true,
                  ),
                ),
              ],
            ),
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
                  'Redis 结果缓存 TTL',
                  style: TextStyle(fontSize: 14, fontWeight: FontWeight.w700),
                ),
                const SizedBox(height: 12),
                Row(
                  children: [
                    const Expanded(child: Text('恶意 / 可疑（分钟）')),
                    SizedBox(
                      width: 120,
                      child: TextField(
                        controller: c.maliciousTtlCtrl,
                        keyboardType: TextInputType.number,
                        textAlign: TextAlign.center,
                        decoration: const InputDecoration(isDense: true),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                Row(
                  children: [
                    const Expanded(child: Text('干净链接（分钟）')),
                    SizedBox(
                      width: 120,
                      child: TextField(
                        controller: c.cleanTtlCtrl,
                        keyboardType: TextInputType.number,
                        textAlign: TextAlign.center,
                        decoration: const InputDecoration(isDense: true),
                      ),
                    ),
                  ],
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

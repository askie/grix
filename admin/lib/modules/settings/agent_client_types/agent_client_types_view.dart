import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../shared/widgets/admin_scaffold.dart';
import '../../../shared/widgets/async_view.dart';
import 'agent_client_types_controller.dart';

/// 系统设置：当前部署支持哪些智能体 client_type。
class AgentClientTypesSettingsView
    extends GetView<AgentClientTypesSettingsController> {
  const AgentClientTypesSettingsView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '支持的智能体类型',
      actions: [
        IconButton(
          tooltip: '刷新',
          onPressed: controller.load,
          icon: const Icon(Icons.refresh),
        ),
      ],
      body: Obx(
        () => AsyncView(
          loading: controller.loading.value,
          error: controller.error.value,
          isEmpty: !controller.loaded,
          onRetry: controller.load,
          builder: (_) => _Body(c: controller),
        ),
      ),
    );
  }
}

class _Body extends StatelessWidget {
  const _Body({required this.c});
  final AgentClientTypesSettingsController c;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 720),
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            Card(
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      '部署启用的智能体类型',
                      style: TextStyle(
                        fontSize: 15,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Obx(
                      () => Text(
                        c.allEnabledHint.value
                            ? '当前未单独配置：全部类型均可用（与历史行为一致）。保存后将按勾选列表生效；国内区可关掉海外 CLI。'
                            : '仅勾选的类型会出现在用户端创建/安装列表；已存在的未勾选类型 Agent 仍可正常使用。改动最多 1 分钟生效。',
                        style: const TextStyle(fontSize: 12, color: Colors.grey),
                      ),
                    ),
                    const SizedBox(height: 8),
                    Row(
                      children: [
                        TextButton(
                          onPressed: () => c.selectAll(true),
                          child: const Text('全选'),
                        ),
                        TextButton(
                          onPressed: () => c.selectAll(false),
                          child: const Text('全不选'),
                        ),
                      ],
                    ),
                    const Divider(height: 16),
                    Obx(
                      () => Column(
                        children: [
                          for (var i = 0; i < c.items.length; i++)
                            CheckboxListTile(
                              contentPadding: EdgeInsets.zero,
                              dense: true,
                              value: c.items[i].enabled,
                              title: Text(c.items[i].label),
                              subtitle: Text(c.items[i].type),
                              onChanged: (v) {
                                if (v == null) return;
                                c.toggle(i, v);
                              },
                            ),
                        ],
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
            const SizedBox(height: 40),
          ],
        ),
      ),
    );
  }
}

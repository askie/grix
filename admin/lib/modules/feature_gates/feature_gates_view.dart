import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/async_view.dart';
import '../../shared/widgets/confirm_dialog.dart';
import '../users/user_picker_dialog.dart';
import 'feature_gate_models.dart';
import 'feature_gate_service.dart';
import 'feature_gates_controller.dart';

class FeatureGatesView extends GetView<FeatureGatesController> {
  const FeatureGatesView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: 'Feature Gates',
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
          isEmpty: controller.gates.isEmpty && controller.available.isEmpty,
          onRetry: controller.load,
          emptyText: '暂无功能开关',
          builder: (_) => _Body(c: controller),
        ),
      ),
    );
  }
}

class _Body extends StatelessWidget {
  const _Body({required this.c});
  final FeatureGatesController c;

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        if (c.available.isNotEmpty) ...[
          _CreateSection(c: c),
          const SizedBox(height: 16),
        ],
        for (final gate in c.gates) ...[
          _GateCard(gate: gate, c: c),
          const SizedBox(height: 8),
        ],
      ],
    );
  }
}

class _CreateSection extends StatelessWidget {
  const _CreateSection({required this.c});
  final FeatureGatesController c;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Row(
          children: [
            const Text('创建开关：', style: TextStyle(fontWeight: FontWeight.w600)),
            const SizedBox(width: 8),
            Expanded(
              child: Wrap(
                spacing: 8,
                runSpacing: 8,
                children: c.available
                    .map(
                      (f) => ActionChip(
                        label: Text(f.displayName),
                        onPressed: () => c.create(f.key),
                      ),
                    )
                    .toList(),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _GateCard extends StatelessWidget {
  const _GateCard({required this.gate, required this.c});
  final FeatureGateInfo gate;
  final FeatureGatesController c;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    gate.displayName,
                    style: theme.textTheme.titleMedium,
                  ),
                ),
                _statusPill(gate),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              gate.publicOnly
                  ? 'key: ${gate.key}'
                  : 'key: ${gate.key}　白名单用户: ${gate.whitelistUserCount}',
              style: theme.textTheme.bodySmall,
            ),
            const SizedBox(height: 10),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                if (gate.status != 'enabled')
                  OutlinedButton(
                    onPressed: () => c.updateStatus(gate.key, 'enabled'),
                    child: const Text('全量开启'),
                  ),
                if (!gate.publicOnly && gate.status != 'whitelist')
                  OutlinedButton(
                    onPressed: () => c.updateStatus(gate.key, 'whitelist'),
                    child: const Text('白名单'),
                  ),
                if (gate.status != 'disabled')
                  OutlinedButton(
                    onPressed: () => c.updateStatus(gate.key, 'disabled'),
                    child: const Text('关闭'),
                  ),
                if (!gate.publicOnly) ...[
                  OutlinedButton.icon(
                    icon: const Icon(Icons.person_add, size: 18),
                    label: const Text('添加用户'),
                    onPressed: _addUsers,
                  ),
                  OutlinedButton.icon(
                    icon: const Icon(Icons.person_remove, size: 18),
                    label: const Text('移除用户'),
                    onPressed: _removeUsers,
                  ),
                ],
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _statusPill(FeatureGateInfo g) {
    Color fg, bg;
    switch (g.status) {
      case 'enabled':
        fg = AppPalette.success;
        bg = AppPalette.successSoft;
      case 'whitelist':
        fg = AppPalette.info;
        bg = AppPalette.infoSoft;
      default:
        fg = AppPalette.textSecondary;
        bg = AppPalette.border;
    }
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(6),
      ),
      child: Text(
        g.statusText,
        style: TextStyle(fontSize: 12, color: fg, fontWeight: FontWeight.w600),
      ),
    );
  }

  Future<void> _addUsers() async {
    final picked = await UserPickerDialog.show(
      title: '添加白名单用户：${gate.displayName}',
      mode: UserPickerMode.multiple,
      confirmText: '添加',
    );
    if (picked == null || picked.isEmpty) return;
    final ids = picked.map((u) => u.id).join(',');
    await c.addUsers(gate.key, ids);
  }

  Future<void> _removeUsers() async {
    if (gate.whitelistUserCount == 0) {
      Toast.error('白名单为空');
      return;
    }
    final picked = await UserPickerDialog.show(
      title: '移除白名单用户：${gate.displayName}',
      mode: UserPickerMode.multiple,
      confirmText: '移除',
      searchHint: '搜索白名单内的用户',
      emptyText: '白名单内没有匹配的用户',
      loader: ({String? query, int page = 1, int pageSize = 20}) =>
          FeatureGateService.listWhitelist(
            key: gate.key,
            query: query,
            page: page,
            pageSize: pageSize,
          ),
    );
    if (picked == null || picked.isEmpty) return;
    final ids = picked.map((u) => u.id).join(',');
    await c.removeUsers(gate.key, ids);
  }
}

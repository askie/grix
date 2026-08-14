import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/async_view.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'gateway_models.dart';
import 'gateway_reconciliation_controller.dart';

class GatewayReconciliationView extends GetView<GatewayReconciliationController> {
  const GatewayReconciliationView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '计费网关 · 对账报告',
      actions: [
        IconButton(tooltip: '刷新', onPressed: controller.reloadFromFirstPage, icon: const Icon(Icons.refresh)),
      ],
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(12),
            child: Row(
              children: [
                const Text('厂商：'),
                const SizedBox(width: 8),
                Obx(() => DropdownButton<String>(
                      value: controller.providerFilter.value.isEmpty ? null : controller.providerFilter.value,
                      hint: const Text('全部'),
                      items: const [
                        DropdownMenuItem(value: '', child: Text('全部')),
                        DropdownMenuItem(value: 'deepseek', child: Text('deepseek')),
                        DropdownMenuItem(value: 'volcano_ark', child: Text('volcano_ark')),
                      ],
                      onChanged: (v) => controller.changeProvider(v ?? ''),
                    )),
              ],
            ),
          ),
          Expanded(
            child: Obx(
              () => AsyncView(
                loading: controller.loading.value,
                error: controller.error.value,
                isEmpty: controller.isEmpty,
                onRetry: controller.reloadFromFirstPage,
                emptyText: '暂无对账报告',
                builder: (_) => InfiniteListView<GatewayReconciliationReport>(
                  controller: controller,
                  itemBuilder: (_, r, _) => _ReportTile(r: r),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _ReportTile extends StatelessWidget {
  const _ReportTile({required this.r});
  final GatewayReconciliationReport r;

  @override
  Widget build(BuildContext context) {
    Color fg, bg;
    switch (r.status) {
      case 'ok':
        fg = AppPalette.success;
        bg = AppPalette.successSoft;
      case 'warning':
        fg = AppPalette.warning;
        bg = AppPalette.warningSoft;
      case 'critical':
        fg = AppPalette.danger;
        bg = AppPalette.dangerSoft;
      default:
        fg = AppPalette.textSecondary;
        bg = AppPalette.border;
    }
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(child: Text(r.provider, style: Theme.of(context).textTheme.titleSmall)),
                if (r.autoAdjusted)
                  const Padding(
                    padding: EdgeInsets.only(right: 6),
                    child: Icon(Icons.autorenew, size: 16),
                  ),
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                  decoration: BoxDecoration(color: bg, borderRadius: BorderRadius.circular(6)),
                  child: Text(r.status, style: TextStyle(fontSize: 12, color: fg, fontWeight: FontWeight.w600)),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Text('厂商实际花费 ${r.vendorActualCost} · 流水理论花费 ${r.ledgerExpectedCost} · 差值 ${r.diff}${r.diffRatio != null ? ' (${r.diffRatio})' : ''}'),
            const SizedBox(height: 4),
            Text('窗口 ${r.windowStart} ~ ${r.windowEnd}', style: Theme.of(context).textTheme.bodySmall),
          ],
        ),
      ),
    );
  }
}

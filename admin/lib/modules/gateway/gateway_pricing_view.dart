import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/async_view.dart';
import '../../shared/widgets/confirm_dialog.dart';
import '../../shared/widgets/dialog_content_box.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'gateway_models.dart';
import 'gateway_pricing_controller.dart';

class GatewayPricingView extends GetView<GatewayPricingController> {
  const GatewayPricingView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '计费网关 · 价目表',
      actions: [
        IconButton(
          tooltip: '新增价目规则',
          onPressed: () => Get.dialog(_CreateRuleDialog(c: controller)),
          icon: const Icon(Icons.add),
        ),
        IconButton(
          tooltip: '刷新',
          onPressed: controller.reloadFromFirstPage,
          icon: const Icon(Icons.refresh),
        ),
      ],
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(12),
            child: Row(
              children: [
                const Text('厂商：'),
                const SizedBox(width: 8),
                Obx(
                  () => DropdownButton<String>(
                    value: controller.providerFilter.value.isEmpty
                        ? null
                        : controller.providerFilter.value,
                    hint: const Text('全部'),
                    items: const [
                      DropdownMenuItem(value: '', child: Text('全部')),
                      DropdownMenuItem(
                        value: 'deepseek',
                        child: Text('deepseek'),
                      ),
                      DropdownMenuItem(
                        value: 'volcano_ark',
                        child: Text('volcano_ark'),
                      ),
                      DropdownMenuItem(value: 'openai', child: Text('openai')),
                      DropdownMenuItem(
                        value: 'anthropic',
                        child: Text('anthropic'),
                      ),
                    ],
                    onChanged: (v) => controller.changeProvider(v ?? ''),
                  ),
                ),
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
                emptyText: '暂无价目规则',
                builder: (_) => InfiniteListView<GatewayPricingRule>(
                  controller: controller,
                  itemBuilder: (_, r, _) => _RuleTile(
                    r: r,
                    onRetire: r.isActive
                        ? () => _confirmRetire(context, controller, r)
                        : null,
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// 退休一条规则会同时影响计价和用户端的可选模型清单，属于动钱的操作，必须二次确认。
Future<void> _confirmRetire(
  BuildContext context,
  GatewayPricingController c,
  GatewayPricingRule r,
) async {
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: const Text('退休这条价目规则？'),
      content: Text(
        '${r.provider} / ${r.model}\n\n'
        '退休后：该规则不再参与计价，且不再出现在用户端的可选模型清单里。\n'
        '用于清掉上游根本不认的废模型名（用户选中会直接报错）。\n'
        '如果这是一个真实在用的模型，退休它会导致该模型无法调用。',
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(ctx).pop(false),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(ctx).pop(true),
          child: const Text('确认退休'),
        ),
      ],
    ),
  );
  if (ok == true) {
    await c.retireRule(r.id);
  }
}

class _RuleTile extends StatelessWidget {
  const _RuleTile({required this.r, this.onRetire});
  final GatewayPricingRule r;
  final VoidCallback? onRetire;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    '${r.provider} / ${r.model}',
                    style: Theme.of(context).textTheme.titleSmall,
                  ),
                ),
                _windowChip(r),
                const SizedBox(width: 6),
                _pill(r),
                if (onRetire != null) ...[
                  const SizedBox(width: 6),
                  IconButton(
                    icon: const Icon(Icons.archive_outlined, size: 18),
                    tooltip: '退休该规则（不再计价、不再出现在用户模型清单里）',
                    onPressed: onRetire,
                  ),
                ],
              ],
            ),
            const SizedBox(height: 6),
            Text(
              '命中缓存 ${r.cachedInputPricePerM} / 未命中 ${r.uncachedInputPricePerM} / 输出 ${r.outputPricePerM}  (USD/百万token)',
            ),
            const SizedBox(height: 4),
            Text(
              '原始币种 ${r.sourceCurrency} · ${r.createdBy == 'manual' ? '人工录入' : '对账自动调价'} · 生效于 ${r.effectiveFrom}${r.isActive ? '' : ' · 已于 ${r.effectiveTo} 失效'}',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
      ),
    );
  }

  Widget _pill(GatewayPricingRule r) {
    final color = r.isActive ? AppPalette.success : AppPalette.textSecondary;
    final bg = r.isActive ? AppPalette.successSoft : AppPalette.border;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(6),
      ),
      child: Text(
        r.isActive ? '生效中' : '已失效',
        style: TextStyle(
          fontSize: 12,
          color: color,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }

  Widget _windowChip(GatewayPricingRule r) {
    final isDefault = r.dailyWindowStartMin == null;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: AppPalette.infoSoft,
        borderRadius: BorderRadius.circular(6),
      ),
      child: Text(
        isDefault ? '全天' : r.windowLabel,
        style: TextStyle(
          fontSize: 12,
          color: AppPalette.info,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }
}

class _CreateRuleDialog extends StatefulWidget {
  const _CreateRuleDialog({required this.c});
  final GatewayPricingController c;

  @override
  State<_CreateRuleDialog> createState() => _CreateRuleDialogState();
}

class _CreateRuleDialogState extends State<_CreateRuleDialog> {
  final _providerCtrl = TextEditingController();
  final _modelCtrl = TextEditingController();
  final _cachedCtrl = TextEditingController();
  final _uncachedCtrl = TextEditingController();
  final _outputCtrl = TextEditingController();
  final _sourceCurrencyCtrl = TextEditingController(text: 'CNY');
  final _fxRateCtrl = TextEditingController(text: '1');
  final _windowStartCtrl = TextEditingController();
  final _windowEndCtrl = TextEditingController();

  @override
  void dispose() {
    _providerCtrl.dispose();
    _modelCtrl.dispose();
    _cachedCtrl.dispose();
    _uncachedCtrl.dispose();
    _outputCtrl.dispose();
    _sourceCurrencyCtrl.dispose();
    _fxRateCtrl.dispose();
    _windowStartCtrl.dispose();
    _windowEndCtrl.dispose();
    super.dispose();
  }

  /// 把 "HH:MM"(北京时间) 解析成当日分钟数[0,1440)；空串返回null；非法返回-1。
  int? _parseHHMM(String s) {
    s = s.trim();
    if (s.isEmpty) return null;
    final parts = s.split(':');
    if (parts.length != 2) return -1;
    final h = int.tryParse(parts[0]);
    final m = int.tryParse(parts[1]);
    if (h == null || m == null || h < 0 || h > 23 || m < 0 || m > 59) return -1;
    return h * 60 + m;
  }

  Future<void> _submit() async {
    // 先读局部变量再关闭对话框，避免依赖dispose时序。
    final provider = _providerCtrl.text.trim();
    final model = _modelCtrl.text.trim();
    final cached = _cachedCtrl.text.trim();
    final uncached = _uncachedCtrl.text.trim();
    final output = _outputCtrl.text.trim();
    final sourceCurrency = _sourceCurrencyCtrl.text.trim();
    final fxRate = _fxRateCtrl.text.trim();
    final startStr = _windowStartCtrl.text.trim();
    final endStr = _windowEndCtrl.text.trim();
    if (provider.isEmpty || model.isEmpty) return;

    final windowStart = _parseHHMM(startStr);
    final windowEnd = _parseHHMM(endStr);
    if (windowStart == -1 || windowEnd == -1) {
      Toast.error('时段格式应为 HH:MM，如 00:30');
      return;
    }
    if ((windowStart == null) != (windowEnd == null)) {
      Toast.error('分时价的起止时间要么都填、要么都留空(全天)');
      return;
    }
    Get.back();
    await widget.c.createRule(
      provider: provider,
      model: model,
      cached: cached,
      uncached: uncached,
      output: output,
      sourceCurrency: sourceCurrency,
      fxRate: fxRate,
      windowStartMin: windowStart,
      windowEndMin: windowEnd,
    );
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      insetPadding: kDialogInsetPadding,
      title: const Text('新增价目规则'),
      content: DialogContentBox(
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: _providerCtrl,
                decoration: const InputDecoration(labelText: '厂商，如 deepseek'),
              ),
              TextField(
                controller: _modelCtrl,
                decoration: const InputDecoration(labelText: '计价模型名'),
              ),
              const SizedBox(height: 4),
              const Align(
                alignment: Alignment.centerLeft,
                child: Text('以下三个价格是厂商官方原始币种下的数字，会自动按汇率换算成USD存库：'),
              ),
              TextField(
                controller: _cachedCtrl,
                decoration: const InputDecoration(
                  labelText: '缓存命中单价(每百万token)',
                ),
              ),
              TextField(
                controller: _uncachedCtrl,
                decoration: const InputDecoration(
                  labelText: '缓存未命中单价(每百万token)',
                ),
              ),
              TextField(
                controller: _outputCtrl,
                decoration: const InputDecoration(labelText: '输出单价(每百万token)'),
              ),
              TextField(
                controller: _sourceCurrencyCtrl,
                decoration: const InputDecoration(labelText: '官方原始报价币种'),
              ),
              TextField(
                controller: _fxRateCtrl,
                decoration: const InputDecoration(
                  labelText: '换算成USD的汇率（USD报价填1）',
                ),
              ),
              const SizedBox(height: 8),
              const Align(
                alignment: Alignment.centerLeft,
                child: Text(
                  '分时价时段（北京时间，留空=全天兜底价）：\n如 DeepSeek 错峰填 00:30–08:30、高峰填 09:00–12:00',
                  style: TextStyle(fontSize: 12),
                ),
              ),
              Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _windowStartCtrl,
                      decoration: const InputDecoration(labelText: '起(HH:MM)'),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: TextField(
                      controller: _windowEndCtrl,
                      decoration: const InputDecoration(labelText: '止(HH:MM)'),
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(onPressed: () => Get.back(), child: const Text('取消')),
        FilledButton(onPressed: _submit, child: const Text('保存')),
      ],
    );
  }
}

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/async_view.dart';
import '../../shared/widgets/confirm_dialog.dart';
import '../../shared/widgets/dialog_content_box.dart';
import '../../shared/widgets/user_ref.dart';
import 'gateway_models.dart';
import 'gateway_new_key_dialog.dart';
import 'gateway_wallet_detail_controller.dart';

class GatewayWalletDetailView extends GetView<GatewayWalletDetailController> {
  const GatewayWalletDetailView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '钱包详情',
      actions: [
        IconButton(tooltip: '刷新', onPressed: controller.load, icon: const Icon(Icons.refresh)),
      ],
      body: Obx(
        () => AsyncView(
          loading: controller.loading.value,
          error: controller.error.value,
          isEmpty: controller.wallet.value == null,
          onRetry: controller.load,
          emptyText: '钱包不存在',
          builder: (_) => _Body(c: controller),
        ),
      ),
    );
  }
}

class _Body extends StatelessWidget {
  const _Body({required this.c});
  final GatewayWalletDetailController c;

  @override
  Widget build(BuildContext context) {
    final w = c.wallet.value!;
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        _BalanceCard(wallet: w, c: c),
        const SizedBox(height: 16),
        _SectionTitle(
          title: '虚拟Key（${c.keys.length}）',
          action: TextButton.icon(
            icon: const Icon(Icons.vpn_key_outlined, size: 18),
            label: const Text('发新Key'),
            onPressed: () => _issueKey(context),
          ),
        ),
        for (final k in c.keys) _VirtualKeyTile(k: k, c: c),
        const SizedBox(height: 16),
        const _SectionTitle(title: '消费流水（近50条）'),
        for (final e in c.ledger) _LedgerTile(e: e),
        if (c.ledger.isEmpty) const _EmptyRow(text: '暂无消费流水'),
        const SizedBox(height: 16),
        const _SectionTitle(title: '充值流水（近50条）'),
        for (final t in c.topups) _TopupTile(t: t),
        if (c.topups.isEmpty) const _EmptyRow(text: '暂无充值流水'),
      ],
    );
  }

  Future<void> _issueKey(BuildContext context) async {
    final label = await ConfirmDialog.showWithReason(
      title: '发新虚拟Key',
      hint: '备注（如：Claude Code / Codex）',
      confirmText: '生成',
      danger: false,
    );
    if (label == null) return;
    final plain = await c.issueKey(label);
    if (plain == null || plain.isEmpty) return;
    // 明文只有这一次，弹窗禁止点遮罩误关，必须点"我已保存"才关闭。
    await GatewayNewKeyDialog.show(plain);
  }
}

class _BalanceCard extends StatelessWidget {
  const _BalanceCard({required this.wallet, required this.c});
  final GatewayWallet wallet;
  final GatewayWalletDetailController c;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  UserRef(wallet.ownerId,
                      showId: true,
                      style: Theme.of(context).textTheme.titleMedium),
                  const SizedBox(height: 4),
                  Text('钱包ID: ${wallet.id}', style: Theme.of(context).textTheme.bodySmall),
                  const SizedBox(height: 8),
                  Text(
                    '${wallet.balance} USD',
                    style: const TextStyle(fontSize: 22, fontWeight: FontWeight.w700),
                  ),
                ],
              ),
            ),
            FilledButton.icon(
              icon: const Icon(Icons.add_card, size: 18),
              label: const Text('充值'),
              onPressed: () => Get.dialog(_TopupDialog(c: c)),
            ),
          ],
        ),
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle({required this.title, this.action});
  final String title;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Row(
        children: [
          Expanded(child: Text(title, style: Theme.of(context).textTheme.titleSmall)),
          ?action,
        ],
      ),
    );
  }
}

class _EmptyRow extends StatelessWidget {
  const _EmptyRow({required this.text});
  final String text;
  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Text(text, style: TextStyle(color: AppPalette.textSecondary)),
      );
}

class _VirtualKeyTile extends StatelessWidget {
  const _VirtualKeyTile({required this.k, required this.c});
  final GatewayVirtualKey k;
  final GatewayWalletDetailController c;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 6),
      child: ListTile(
        dense: true,
        leading: Icon(k.isActive ? Icons.vpn_key : Icons.key_off, color: k.isActive ? AppPalette.success : AppPalette.textSecondary),
        title: Text(k.label.isEmpty ? '（未命名）' : k.label),
        subtitle: Text('...${k.keyHint} · ${k.status}'),
        trailing: k.isActive
            ? TextButton(
                onPressed: () => _revoke(context),
                child: const Text('吊销'),
              )
            : null,
      ),
    );
  }

  Future<void> _revoke(BuildContext context) async {
    final ok = await ConfirmDialog.show(
      title: '吊销这把Key？',
      message: '吊销后立即生效，不影响钱包下其它Key。',
      confirmText: '吊销',
      danger: true,
    );
    if (!ok) return;
    await c.revokeKey(k.id);
  }
}

class _LedgerTile extends StatelessWidget {
  const _LedgerTile({required this.e});
  final GatewayLedgerEntry e;

  @override
  Widget build(BuildContext context) {
    Color statusColor;
    switch (e.status) {
      case 'settled':
        statusColor = AppPalette.success;
      case 'failed':
        statusColor = AppPalette.danger;
      default:
        statusColor = AppPalette.warning;
    }
    return ListTile(
      dense: true,
      title: Text('${e.provider} / ${e.model}'),
      subtitle: Text('输入${e.promptTokens}(命中${e.cachedTokens}) 输出${e.completionTokens} · ${e.createdAt}'),
      trailing: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          Text('-${e.cost} USD'),
          Text(e.status, style: TextStyle(fontSize: 11, color: statusColor)),
        ],
      ),
    );
  }
}

class _TopupTile extends StatelessWidget {
  const _TopupTile({required this.t});
  final GatewayTopupRecord t;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      dense: true,
      title: Text('${t.sourceAmount} ${t.sourceCurrency} → +${t.creditedAmount} USD'),
      subtitle: Text('汇率 ${t.fxRateUsed} · ${t.channel} · ${t.createdAt}'),
    );
  }
}

class _TopupDialog extends StatefulWidget {
  const _TopupDialog({required this.c});
  final GatewayWalletDetailController c;

  @override
  State<_TopupDialog> createState() => _TopupDialogState();
}

class _TopupDialogState extends State<_TopupDialog> {
  final _currencyCtrl = TextEditingController(text: 'CNY');
  final _amountCtrl = TextEditingController();
  final _fxRateCtrl = TextEditingController();
  final _referenceCtrl = TextEditingController();

  @override
  void dispose() {
    _currencyCtrl.dispose();
    _amountCtrl.dispose();
    _fxRateCtrl.dispose();
    _referenceCtrl.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    // 先把所有字段读到局部变量，再 Get.back()，避免依赖"关闭对话框不会立刻dispose controller"的脆弱时序。
    final amount = _amountCtrl.text.trim();
    final fxRate = _fxRateCtrl.text.trim();
    final currency = _currencyCtrl.text.trim();
    final reference = _referenceCtrl.text.trim();
    if (amount.isEmpty || fxRate.isEmpty) return;
    Get.back();
    await widget.c.topup(
      sourceCurrency: currency,
      sourceAmount: amount,
      fxRate: fxRate,
      reference: reference,
    );
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      insetPadding: kDialogInsetPadding,
      title: const Text('充值'),
      content: DialogContentBox(
        maxWidth: 380,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(controller: _currencyCtrl, decoration: const InputDecoration(labelText: '支付币种（如 CNY）')),
            TextField(controller: _amountCtrl, decoration: const InputDecoration(labelText: '支付金额')),
            TextField(controller: _fxRateCtrl, decoration: const InputDecoration(labelText: '换算成USD的汇率（如 0.14）')),
            TextField(controller: _referenceCtrl, decoration: const InputDecoration(labelText: '支付渠道流水号（可选）')),
          ],
        ),
      ),
      actions: [
        TextButton(onPressed: () => Get.back(), child: const Text('取消')),
        FilledButton(onPressed: _submit, child: const Text('确认充值')),
      ],
    );
  }
}

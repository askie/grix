import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/async_view.dart';
import '../../shared/widgets/dialog_content_box.dart';
import '../../shared/widgets/infinite_list_view.dart';
import '../../shared/widgets/user_ref.dart';
import '../users/admin_user_item.dart';
import '../users/user_picker_dialog.dart';
import 'gateway_models.dart';
import 'gateway_new_key_dialog.dart';
import 'gateway_wallets_controller.dart';

/// 大模型计费网关：钱包列表。点一行进详情（充值/发Key/查流水），
/// 右上角两个图标分别进"价目表"和"对账报告"这两个只读/低频维护页面。
class GatewayWalletsView extends GetView<GatewayWalletsController> {
  const GatewayWalletsView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '计费网关 · 钱包',
      actions: [
        IconButton(
          tooltip: '上游厂商密钥',
          onPressed: () => Get.toNamed(AppRoutes.gatewayUpstreamCredentials),
          icon: const Icon(Icons.vpn_key_outlined),
        ),
        IconButton(
          tooltip: '价目表',
          onPressed: () => Get.toNamed(AppRoutes.gatewayPricingRules),
          icon: const Icon(Icons.sell_outlined),
        ),
        IconButton(
          tooltip: '对账报告',
          onPressed: () => Get.toNamed(AppRoutes.gatewayReconciliationReports),
          icon: const Icon(Icons.fact_check_outlined),
        ),
        IconButton(
          tooltip: '给用户发Key',
          onPressed: _issueKeyForUser,
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
            child: TextField(
              controller: controller.searchCtrl,
              decoration: const InputDecoration(
                prefixIcon: Icon(Icons.search),
                hintText: '按用户ID精确搜索',
                isDense: true,
                border: OutlineInputBorder(),
              ),
              onSubmitted: controller.applySearch,
            ),
          ),
          Expanded(
            child: Obx(
              () => AsyncView(
                loading: controller.loading.value,
                error: controller.error.value,
                isEmpty: controller.isEmpty,
                onRetry: controller.reloadFromFirstPage,
                emptyText: '暂无钱包',
                builder: (_) => InfiniteListView<GatewayWallet>(
                  controller: controller,
                  itemBuilder: (_, w, _) => _WalletTile(wallet: w),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  /// 选用户 → 同一弹窗内生成Key并复制；钱包不存在时后端自动创建。
  Future<void> _issueKeyForUser() async {
    final picked = await UserPickerDialog.show(
      title: '选择用户',
      mode: UserPickerMode.single,
      confirmText: '下一步',
    );
    if (picked == null || picked.isEmpty) return;
    // 明文只有这一次，弹窗禁止点遮罩误关，必须点"我已保存"才关闭。
    await Get.dialog(
      _IssueKeyDialog(user: picked.first, controller: controller),
      barrierDismissible: false,
    );
  }
}

class _WalletTile extends StatelessWidget {
  const _WalletTile({required this.wallet});
  final GatewayWallet wallet;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      child: ListTile(
        title: UserRef(wallet.ownerId),
        subtitle: Text('钱包ID: ${wallet.id}'),
        trailing: Text(
          '${wallet.balance} USD',
          style: const TextStyle(fontWeight: FontWeight.w600),
        ),
        // 从详情页返回后刷新列表：详情页可能充值/发Key改了余额，列表要跟着更新，别显示旧值。
        onTap: () async {
          await Get.toNamed('/gateway/wallets/${wallet.id}');
          if (Get.isRegistered<GatewayWalletsController>()) {
            Get.find<GatewayWalletsController>().reloadFromFirstPage();
          }
        },
      ),
    );
  }
}

/// 选完用户后的发Key表单：填备注生成；成功后关掉本弹窗，改用 [GatewayNewKeyDialog] 展示明文。
class _IssueKeyDialog extends StatefulWidget {
  const _IssueKeyDialog({required this.user, required this.controller});
  final AdminUserItem user;
  final GatewayWalletsController controller;

  @override
  State<_IssueKeyDialog> createState() => _IssueKeyDialogState();
}

class _IssueKeyDialogState extends State<_IssueKeyDialog> {
  final _labelCtrl = TextEditingController();
  bool _busy = false;

  @override
  void dispose() {
    _labelCtrl.dispose();
    super.dispose();
  }

  Future<void> _generate() async {
    setState(() => _busy = true);
    final plain = await widget.controller.issueKeyForUser(
      widget.user.id,
      _labelCtrl.text.trim(),
    );
    if (!mounted) return;
    setState(() => _busy = false);
    if (plain == null || plain.isEmpty) return;
    // 关掉表单弹窗，再弹出统一的明文结果（含 CC Switch Base URL / 帮助）。
    Get.back();
    await GatewayNewKeyDialog.show(plain);
  }

  @override
  Widget build(BuildContext context) {
    final user = widget.user;
    return AlertDialog(
      insetPadding: kDialogInsetPadding,
      title: const Text('给用户发Key'),
      content: DialogContentBox(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '${user.displayName}（ID ${user.id}）',
              style: Theme.of(context).textTheme.titleSmall,
            ),
            const SizedBox(height: 4),
            Text(
              '该用户还没有钱包时会自动创建。',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _labelCtrl,
              autofocus: true,
              decoration: const InputDecoration(
                labelText: '备注（如：Claude Code / Codex）',
              ),
              onSubmitted: (_) => _busy ? null : _generate(),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _busy ? null : () => Get.back(),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: _busy ? null : _generate,
          child: Text(_busy ? '生成中…' : '生成Key'),
        ),
      ],
    );
  }
}

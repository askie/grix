import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/async_view.dart';
import '../../shared/widgets/confirm_dialog.dart';
import '../../shared/widgets/dialog_content_box.dart';
import '../../shared/widgets/infinite_list_view.dart';
import 'gateway_credentials_controller.dart';
import 'gateway_models.dart';

/// 上游厂商官方Key管理页：动态增删厂商推理Key与对账AK/SK，密文落库，网关运行时轮询取用。
class GatewayCredentialsView extends GetView<GatewayCredentialsController> {
  const GatewayCredentialsView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '计费网关 · 上游厂商密钥',
      actions: [
        IconButton(
          tooltip: '新增凭据',
          onPressed: () => Get.dialog(_CreateCredentialDialog(c: controller)),
          icon: const Icon(Icons.add),
        ),
        IconButton(tooltip: '刷新', onPressed: controller.reloadFromFirstPage, icon: const Icon(Icons.refresh)),
      ],
      body: Column(
        children: [
          const Padding(
            padding: EdgeInsets.fromLTRB(12, 12, 12, 0),
            child: Align(
              alignment: Alignment.centerLeft,
              child: Text(
                '厂商官方Key由这里动态管理、密文存库，绝不下发用户；同一厂商可挂多把推理Key轮询分流。',
                style: TextStyle(fontSize: 12, color: AppPalette.textSecondary),
              ),
            ),
          ),
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
                emptyText: '还没有上游凭据，点右上角 + 添加',
                builder: (_) => InfiniteListView<GatewayUpstreamCredential>(
                  controller: controller,
                  itemBuilder: (_, cred, _) => _CredentialTile(cred: cred, c: controller),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _CredentialTile extends StatelessWidget {
  const _CredentialTile({required this.cred, required this.c});
  final GatewayUpstreamCredential cred;
  final GatewayCredentialsController c;

  @override
  Widget build(BuildContext context) {
    final enabledColor = cred.enabled ? AppPalette.success : AppPalette.textSecondary;
    final enabledBg = cred.enabled ? AppPalette.successSoft : AppPalette.border;
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
                  child: Text('${cred.provider}  ·  ${cred.isInference ? '推理转发' : '对账'}',
                      style: Theme.of(context).textTheme.titleSmall),
                ),
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                  decoration: BoxDecoration(color: enabledBg, borderRadius: BorderRadius.circular(6)),
                  child: Text(cred.enabled ? '启用中' : '已停用',
                      style: TextStyle(fontSize: 12, color: enabledColor, fontWeight: FontWeight.w600)),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Text('Key ****${cred.keyHint}${cred.label.isEmpty ? '' : '  ·  ${cred.label}'}'),
            if (cred.baseUrl.isNotEmpty || cred.region.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  [
                    if (cred.baseUrl.isNotEmpty) '端点 ${cred.baseUrl}',
                    if (cred.region.isNotEmpty) 'region ${cred.region}',
                  ].join('  ·  '),
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ),
            const SizedBox(height: 4),
            Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                TextButton(
                  onPressed: () => c.setEnabled(cred.id, !cred.enabled),
                  child: Text(cred.enabled ? '停用' : '启用'),
                ),
                TextButton(
                  style: TextButton.styleFrom(foregroundColor: AppPalette.danger),
                  onPressed: () async {
                    final ok = await ConfirmDialog.show(
                      title: '删除凭据',
                      message: '确定删除这把 ${cred.provider} 的 Key（****${cred.keyHint}）？删除后不可恢复。',
                      danger: true,
                      confirmText: '删除',
                    );
                    if (ok) await c.deleteCredential(cred.id);
                  },
                  child: const Text('删除'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _CreateCredentialDialog extends StatefulWidget {
  const _CreateCredentialDialog({required this.c});
  final GatewayCredentialsController c;

  @override
  State<_CreateCredentialDialog> createState() => _CreateCredentialDialogState();
}

class _CreateCredentialDialogState extends State<_CreateCredentialDialog> {
  String _provider = 'deepseek';
  String _purpose = 'inference';
  final _apiKeyCtrl = TextEditingController();
  final _apiSecretCtrl = TextEditingController();
  final _baseUrlCtrl = TextEditingController();
  final _regionCtrl = TextEditingController();
  final _labelCtrl = TextEditingController();

  @override
  void dispose() {
    _apiKeyCtrl.dispose();
    _apiSecretCtrl.dispose();
    _baseUrlCtrl.dispose();
    _regionCtrl.dispose();
    _labelCtrl.dispose();
    super.dispose();
  }

  bool get _isReconcile => _purpose == 'reconcile';

  Future<void> _submit() async {
    final apiKey = _apiKeyCtrl.text.trim();
    final apiSecret = _apiSecretCtrl.text.trim();
    final baseUrl = _baseUrlCtrl.text.trim();
    final region = _regionCtrl.text.trim();
    final label = _labelCtrl.text.trim();
    if (apiKey.isEmpty) {
      Toast.error(_isReconcile ? 'Access Key 必填' : 'API Key 必填');
      return;
    }
    if (_isReconcile && apiSecret.isEmpty) {
      Toast.error('对账凭据的 Secret Key 必填');
      return;
    }
    Get.back();
    await widget.c.createCredential(
      provider: _provider,
      purpose: _purpose,
      apiKey: apiKey,
      apiSecret: apiSecret,
      baseUrl: baseUrl,
      region: region,
      label: label,
    );
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      insetPadding: kDialogInsetPadding,
      title: const Text('新增上游凭据'),
      content: DialogContentBox(
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  const Text('厂商：'),
                  const SizedBox(width: 8),
                  DropdownButton<String>(
                    value: _provider,
                    items: const [
                      DropdownMenuItem(value: 'deepseek', child: Text('deepseek')),
                      DropdownMenuItem(value: 'volcano_ark', child: Text('volcano_ark')),
                    ],
                    onChanged: (v) => setState(() => _provider = v ?? 'deepseek'),
                  ),
                  const SizedBox(width: 20),
                  const Text('用途：'),
                  const SizedBox(width: 8),
                  DropdownButton<String>(
                    value: _purpose,
                    items: const [
                      DropdownMenuItem(value: 'inference', child: Text('推理转发')),
                      DropdownMenuItem(value: 'reconcile', child: Text('对账')),
                    ],
                    onChanged: (v) => setState(() => _purpose = v ?? 'inference'),
                  ),
                ],
              ),
              const SizedBox(height: 4),
              TextField(
                controller: _apiKeyCtrl,
                decoration: InputDecoration(
                  labelText: _isReconcile ? '火山费用中心 Access Key' : 'API Key（如 DeepSeek sk- / 火山 ARK ark-）',
                ),
              ),
              if (_isReconcile)
                TextField(
                  controller: _apiSecretCtrl,
                  decoration: const InputDecoration(labelText: '火山费用中心 Secret Key'),
                ),
              if (_isReconcile)
                TextField(
                  controller: _regionCtrl,
                  decoration: const InputDecoration(labelText: 'region（留空默认 cn-beijing）'),
                ),
              if (!_isReconcile)
                TextField(
                  controller: _baseUrlCtrl,
                  decoration: const InputDecoration(labelText: '端点覆盖（留空用默认，不确定就留空）'),
                ),
              TextField(controller: _labelCtrl, decoration: const InputDecoration(labelText: '备注（可选）')),
              const SizedBox(height: 8),
              const Text(
                '提交后明文即被加密存库、只回末4位；同一厂商可加多把推理Key做轮询。',
                style: TextStyle(fontSize: 12),
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

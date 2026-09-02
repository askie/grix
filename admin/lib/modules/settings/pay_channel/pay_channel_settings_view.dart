import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../shared/widgets/admin_scaffold.dart';
import '../../../shared/widgets/async_view.dart';
import 'pay_channel_settings_controller.dart';

/// 支付通道（支付宝 / PayPal）设置页。
///
/// 内容分两块：支付宝商户凭证、PayPal 应用凭证，各带启用开关、沙箱开关与
/// "测试连接"（用上一次已保存的凭证自检，改动需先保存才能测到最新值）。
class PayChannelSettingsView extends GetView<PayChannelSettingsController> {
  const PayChannelSettingsView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '支付通道设置',
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
          isEmpty: controller.current.value == null,
          onRetry: controller.load,
          builder: (_) => _Body(c: controller),
        ),
      ),
    );
  }
}

class _Body extends StatelessWidget {
  const _Body({required this.c});
  final PayChannelSettingsController c;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 720),
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            _alipayCard(),
            const SizedBox(height: 16),
            _paypalCard(),
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

  Widget _section(String title, String? subtitle, List<Widget> children) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              title,
              style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
            ),
            if (subtitle != null) ...[
              const SizedBox(height: 4),
              Text(
                subtitle,
                style: const TextStyle(fontSize: 12, color: Colors.grey),
              ),
            ],
            const SizedBox(height: 12),
            ...children,
          ],
        ),
      ),
    );
  }

  Widget _secretHelper(String hint) {
    if (hint.isEmpty) {
      return const Text(
        '（未配置）',
        style: TextStyle(fontSize: 12, color: Colors.orange),
      );
    }
    return Text(
      '当前末四位：$hint',
      style: const TextStyle(fontSize: 12, color: Colors.grey),
    );
  }

  Widget _alipayCard() {
    final cur = c.current.value!;
    return _section('支付宝', '境内收款，电脑网站支付。密钥留空表示保留原值；改完点“保存设置”后再“测试连接”。', [
      Obx(
        () => SwitchListTile(
          contentPadding: EdgeInsets.zero,
          title: const Text('启用支付宝'),
          value: c.alipayEnabled.value,
          onChanged: (v) => c.alipayEnabled.value = v,
        ),
      ),
      Obx(
        () => SwitchListTile(
          contentPadding: EdgeInsets.zero,
          title: const Text('沙箱环境'),
          subtitle: const Text('联调用沙箱网关；正式收款需关闭'),
          value: c.alipaySandbox.value,
          onChanged: (v) => c.alipaySandbox.value = v,
        ),
      ),
      const SizedBox(height: 8),
      TextField(
        controller: c.alipayAppIdCtrl,
        decoration: const InputDecoration(labelText: 'AppID', isDense: true),
      ),
      const SizedBox(height: 12),
      TextField(
        controller: c.alipayPrivateKeyCtrl,
        obscureText: true,
        maxLines: 3,
        minLines: 1,
        decoration: InputDecoration(
          labelText: '应用私钥（RSA2）',
          hintText: '留空表示保留原值',
          isDense: true,
          alignLabelWithHint: true,
          helper: _secretHelper(cur.alipay.privateKeyHint),
        ),
      ),
      const SizedBox(height: 12),
      TextField(
        controller: c.alipayPublicKeyCtrl,
        obscureText: true,
        maxLines: 3,
        minLines: 1,
        decoration: InputDecoration(
          labelText: '支付宝公钥',
          hintText: '留空表示保留原值',
          isDense: true,
          alignLabelWithHint: true,
          helper: _secretHelper(cur.alipay.alipayPublicKeyHint),
        ),
      ),
      const SizedBox(height: 12),
      Align(
        alignment: Alignment.centerLeft,
        child: Obx(
          () => OutlinedButton(
            onPressed: c.testingAlipay.value ? null : c.testAlipay,
            child: c.testingAlipay.value
                ? const SizedBox(
                    height: 16,
                    width: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Text('测试连接'),
          ),
        ),
      ),
    ]);
  }

  Widget _paypalCard() {
    final cur = c.current.value!;
    return _section('PayPal', '海外收款，Orders v2。密钥留空表示保留原值；改完点“保存设置”后再“测试连接”。', [
      Obx(
        () => SwitchListTile(
          contentPadding: EdgeInsets.zero,
          title: const Text('启用 PayPal'),
          value: c.paypalEnabled.value,
          onChanged: (v) => c.paypalEnabled.value = v,
        ),
      ),
      Obx(
        () => SwitchListTile(
          contentPadding: EdgeInsets.zero,
          title: const Text('沙箱环境'),
          subtitle: const Text('联调用沙箱 API；正式收款需关闭'),
          value: c.paypalSandbox.value,
          onChanged: (v) => c.paypalSandbox.value = v,
        ),
      ),
      const SizedBox(height: 8),
      TextField(
        controller: c.paypalClientIdCtrl,
        decoration: const InputDecoration(
          labelText: 'Client ID',
          isDense: true,
        ),
      ),
      const SizedBox(height: 12),
      TextField(
        controller: c.paypalClientSecretCtrl,
        obscureText: true,
        decoration: InputDecoration(
          labelText: 'Client Secret',
          hintText: '留空表示保留原值',
          isDense: true,
          helper: _secretHelper(cur.paypal.clientSecretHint),
        ),
      ),
      const SizedBox(height: 12),
      TextField(
        controller: c.paypalWebhookIdCtrl,
        decoration: const InputDecoration(
          labelText: 'Webhook ID',
          hintText: 'PayPal 开发者后台创建 Webhook 后拿到的 ID，用于校验回调签名',
          isDense: true,
        ),
      ),
      const SizedBox(height: 12),
      Align(
        alignment: Alignment.centerLeft,
        child: Obx(
          () => OutlinedButton(
            onPressed: c.testingPaypal.value ? null : c.testPaypal,
            child: c.testingPaypal.value
                ? const SizedBox(
                    height: 16,
                    width: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Text('测试连接'),
          ),
        ),
      ),
    ]);
  }
}

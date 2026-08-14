import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../shared/widgets/admin_scaffold.dart';
import '../../../shared/widgets/async_view.dart';
import 'sms_settings_controller.dart';

/// 短信登录注册设置页。
///
/// 内容分四块：
///   1. 总开关（注册 / 登录 × CN / Global）
///   2. 允许的国家区号白名单（每行一条；`*` 放行全部）
///   3. 阿里云 dysmsapi 配置（中国大陆通道）
///   4. AWS SNS 配置（全球通道）
///   5. 测试发送（向指定手机号发一条测试码验证 ak/sk + 模板号）
class SmsSettingsView extends GetView<SmsSettingsController> {
  const SmsSettingsView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '手机号短信设置',
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
  final SmsSettingsController c;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 720),
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            _switchesCard(),
            const SizedBox(height: 16),
            _allowedCountriesCard(),
            const SizedBox(height: 16),
            _aliyunCard(),
            const SizedBox(height: 16),
            _awsSnsCard(),
            const SizedBox(height: 16),
            _testCard(),
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
            Text(title,
                style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w700)),
            if (subtitle != null) ...[
              const SizedBox(height: 4),
              Text(subtitle,
                  style: const TextStyle(fontSize: 12, color: Colors.grey)),
            ],
            const SizedBox(height: 12),
            ...children,
          ],
        ),
      ),
    );
  }

  Widget _switchesCard() {
    return _section('总开关', '关闭后该方向的接口直接拒绝；可分别控制中国大陆和全球。', [
      Obx(() => SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('中国大陆 — 允许手机号注册'),
            subtitle: const Text('+86 号码新用户首次发码即注册'),
            value: c.registerCn.value,
            onChanged: (v) => c.registerCn.value = v,
          )),
      Obx(() => SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('中国大陆 — 允许手机号登录'),
            value: c.loginCn.value,
            onChanged: (v) => c.loginCn.value = v,
          )),
      const Divider(height: 24),
      Obx(() => SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('全球 — 允许手机号注册'),
            subtitle: const Text('非 +86 号码（含 +1/+44/+81 等）'),
            value: c.registerGlobal.value,
            onChanged: (v) => c.registerGlobal.value = v,
          )),
      Obx(() => SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('全球 — 允许手机号登录'),
            value: c.loginGlobal.value,
            onChanged: (v) => c.loginGlobal.value = v,
          )),
    ]);
  }

  Widget _allowedCountriesCard() {
    return _section(
      '允许的国家区号白名单',
      '每行一条，例如 +86 / +1 / +44；填 * 表示放行全部（仅用于该方向）。CN 通道默认仅 +86，Global 通道默认 *。',
      [
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('中国大陆通道',
                      style: TextStyle(fontWeight: FontWeight.w600)),
                  const SizedBox(height: 8),
                  TextField(
                    controller: c.allowedCnCtrl,
                    maxLines: 6,
                    minLines: 3,
                    decoration: const InputDecoration(
                      hintText: '+86',
                      alignLabelWithHint: true,
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(width: 16),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('全球通道',
                      style: TextStyle(fontWeight: FontWeight.w600)),
                  const SizedBox(height: 8),
                  TextField(
                    controller: c.allowedGlobalCtrl,
                    maxLines: 6,
                    minLines: 3,
                    decoration: const InputDecoration(
                      hintText: '*\n或逐个列：\n+1\n+44',
                      alignLabelWithHint: true,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ],
    );
  }

  Widget _akHelper(String hint) {
    if (hint.isEmpty) return const Text('（未配置）', style: TextStyle(fontSize: 12, color: Colors.orange));
    return Text('当前末四位：$hint',
        style: const TextStyle(fontSize: 12, color: Colors.grey));
  }

  Widget _aliyunCard() {
    final cur = c.current.value!;
    return _section('阿里云 dysmsapi（中国大陆通道）', '用于 +86 号码下发验证码', [
      TextField(
        controller: c.aliyunRegionCtrl,
        decoration: const InputDecoration(
          labelText: 'RegionID',
          hintText: 'cn-hangzhou',
          isDense: true,
        ),
      ),
      const SizedBox(height: 12),
      TextField(
        controller: c.aliyunAkCtrl,
        decoration: InputDecoration(
          labelText: 'AccessKey ID',
          hintText: '留空表示保留原值',
          isDense: true,
          helper: _akHelper(cur.aliyun.accessKeyIdHint),
        ),
      ),
      const SizedBox(height: 12),
      TextField(
        controller: c.aliyunSkCtrl,
        obscureText: true,
        decoration: InputDecoration(
          labelText: 'AccessKey Secret',
          hintText: '留空表示保留原值',
          isDense: true,
          helper: _akHelper(cur.aliyun.accessKeySecretHint),
        ),
      ),
      const SizedBox(height: 12),
      TextField(
        controller: c.aliyunSignCtrl,
        decoration: const InputDecoration(
          labelText: '签名（SignName）',
          isDense: true,
        ),
      ),
      const SizedBox(height: 12),
      Row(children: [
        Expanded(
          child: TextField(
            controller: c.aliyunTplRegCtrl,
            decoration: const InputDecoration(
              labelText: '注册模板（TemplateCode）',
              hintText: 'SMS_xxxxxxx',
              isDense: true,
            ),
          ),
        ),
        const SizedBox(width: 8),
        Expanded(
          child: TextField(
            controller: c.aliyunTplLoginCtrl,
            decoration: const InputDecoration(
              labelText: '登录模板',
              hintText: 'SMS_xxxxxxx',
              isDense: true,
            ),
          ),
        ),
        const SizedBox(width: 8),
        Expanded(
          child: TextField(
            controller: c.aliyunTplResetCtrl,
            decoration: const InputDecoration(
              labelText: '找回模板',
              hintText: 'SMS_xxxxxxx',
              isDense: true,
            ),
          ),
        ),
      ]),
    ]);
  }

  Widget _awsSnsCard() {
    final cur = c.current.value!;
    return _section('AWS SNS（全球通道）', '用于非 +86 号码下发验证码（Transactional）', [
      TextField(
        controller: c.awsRegionCtrl,
        decoration: const InputDecoration(
          labelText: 'Region',
          hintText: 'us-east-1',
          isDense: true,
        ),
      ),
      const SizedBox(height: 12),
      TextField(
        controller: c.awsAkCtrl,
        decoration: InputDecoration(
          labelText: 'AccessKey ID',
          hintText: '留空表示保留原值',
          isDense: true,
          helper: _akHelper(cur.awsSns.accessKeyIdHint),
        ),
      ),
      const SizedBox(height: 12),
      TextField(
        controller: c.awsSkCtrl,
        obscureText: true,
        decoration: InputDecoration(
          labelText: 'Secret Access Key',
          hintText: '留空表示保留原值',
          isDense: true,
          helper: _akHelper(cur.awsSns.accessKeySecretHint),
        ),
      ),
      const SizedBox(height: 12),
      TextField(
        controller: c.awsSenderCtrl,
        decoration: const InputDecoration(
          labelText: 'SenderID（可选）',
          hintText: '仅对部分国家生效',
          isDense: true,
        ),
      ),
    ]);
  }

  Widget _testCard() {
    return _section('测试发送', '直接调用当前已保存的 ak/sk 与模板号下发一条测试码，验证配置是否正确。区域按手机号区号自动判定。', [
      Row(children: [
        Expanded(
          child: TextField(
            controller: c.testPhoneCtrl,
            decoration: const InputDecoration(
              labelText: '手机号（E.164，含 +）',
              hintText: '+8613800138000',
              isDense: true,
            ),
          ),
        ),
        const SizedBox(width: 12),
        Obx(
          () => OutlinedButton(
            onPressed: c.testing.value ? null : c.sendTestCode,
            child: c.testing.value
                ? const SizedBox(
                    height: 16,
                    width: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Text('发送测试码'),
          ),
        ),
      ]),
    ]);
  }
}

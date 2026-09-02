import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import '../../shared/widgets/admin_scaffold.dart';
import '../../shared/widgets/async_view.dart';
import '../../shared/widgets/user_ref.dart';
import '../users/user_picker_dialog.dart';
import 'settings_controller.dart';
import 'settings_models.dart';

/// 系统设置页：认证设置 / 群组设置。
class SettingsView extends GetView<SettingsController> {
  const SettingsView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '系统设置',
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
          isEmpty: controller.auth.value == null,
          onRetry: controller.load,
          builder: (_) => _Body(c: controller),
        ),
      ),
    );
  }
}

class _Body extends StatelessWidget {
  const _Body({required this.c});

  final SettingsController c;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 640),
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            _authCard(),
            const SizedBox(height: 16),
            _smsEntryCard(),
            const SizedBox(height: 16),
            _pushEntryCard(),
            const SizedBox(height: 16),
            _payChannelEntryCard(),
            const SizedBox(height: 16),
            _groupCard(),
            const SizedBox(height: 16),
            _voiceModelsCard(),
            const SizedBox(height: 40),
          ],
        ),
      ),
    );
  }

  Widget _section(String title, List<Widget> children, Widget action) {
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
            const SizedBox(height: 8),
            ...children,
            const SizedBox(height: 12),
            Align(alignment: Alignment.centerRight, child: action),
          ],
        ),
      ),
    );
  }

  Widget _authCard() {
    return Obx(() {
      final a = c.auth.value!;
      return _section(
        '认证设置',
        [
          TextField(
            controller: c.customerIdCtrl,
            keyboardType: TextInputType.number,
            decoration: InputDecoration(
              labelText: '系统客户账户 ID（0 表示不启用）',
              suffixIcon: IconButton(
                tooltip: '从用户中选择',
                icon: const Icon(Icons.person_search),
                onPressed: () => _pickCustomerUser(c),
              ),
            ),
          ),
          // 输入框里只有裸 ID，下面实时预览这个 ID 是谁，点击可看详情。
          ValueListenableBuilder<TextEditingValue>(
            valueListenable: c.customerIdCtrl,
            builder: (context, v, child) {
              final id = v.text.trim();
              if (id.isEmpty || id == '0' || !RegExp(r'^\d+$').hasMatch(id)) {
                return const SizedBox.shrink();
              }
              return Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Align(
                  alignment: Alignment.centerLeft,
                  child: UserRef(id),
                ),
              );
            },
          ),
        ],
        Obx(
          () => FilledButton(
            onPressed: c.savingAuth.value ? null : c.saveAuth,
            child: c.savingAuth.value
                ? const SizedBox(
                    height: 18,
                    width: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Text('保存认证设置'),
          ),
        ),
      );
    });
  }

  Widget _smsEntryCard() {
    return Card(
      child: ListTile(
        leading: const Icon(Icons.sms_outlined),
        title: const Text('手机号短信登录注册'),
        subtitle: const Text('总开关 / 区号白名单 / 阿里云 / AWS SNS'),
        trailing: const Icon(Icons.chevron_right),
        onTap: () => Get.toNamed(AppRoutes.smsSettings),
      ),
    );
  }

  Widget _pushEntryCard() {
    return Card(
      child: ListTile(
        leading: const Icon(Icons.notifications_active_outlined),
        title: const Text('离线推送通道开关'),
        subtitle: const Text('iOS APNs / 安卓 FCM / 网页 WebPush / 极光 JPush'),
        trailing: const Icon(Icons.chevron_right),
        onTap: () => Get.toNamed(AppRoutes.pushSettings),
      ),
    );
  }

  Widget _payChannelEntryCard() {
    return Card(
      child: ListTile(
        leading: const Icon(Icons.payments_outlined),
        title: const Text('支付通道设置'),
        subtitle: const Text('支付宝 / PayPal 商户凭证，密钥加密存储'),
        trailing: const Icon(Icons.chevron_right),
        onTap: () => Get.toNamed(AppRoutes.payChannelSettings),
      ),
    );
  }

  Widget _groupCard() {
    return _section(
      '群组设置',
      [
        TextField(
          controller: c.inviteThresholdCtrl,
          keyboardType: TextInputType.number,
          decoration: const InputDecoration(
            labelText: '群成员邀请阈值',
            helperText: '群成员数达到该值后，新增成员需审批',
          ),
        ),
      ],
      Obx(
        () => FilledButton(
          onPressed: c.savingGroup.value ? null : c.saveGroup,
          child: c.savingGroup.value
              ? const SizedBox(
                  height: 18,
                  width: 18,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Text('保存群组设置'),
        ),
      ),
    );
  }

  Widget _voiceModelsCard() {
    return Obx(() {
      final rows = <Widget>[];
      for (var i = 0; i < c.voiceModels.length; i++) {
        rows.add(_voiceModelRow(i, c.voiceModels[i]));
      }
      if (rows.isEmpty) {
        rows.add(
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 8),
            child: Text(
              '暂无语音模型，点击下方“新增一项”添加',
              style: TextStyle(color: Colors.grey),
            ),
          ),
        );
      }
      return Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text(
                '语音模型清单',
                style: TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 4),
              const Text(
                'C 端创建“语音模型”AI 时只能从这里启用的项中选择；接入地址由这里统一配置，用户不可见、不可填。',
                style: TextStyle(fontSize: 12, color: Colors.grey),
              ),
              const SizedBox(height: 12),
              ...rows,
              const SizedBox(height: 8),
              Align(
                alignment: Alignment.centerLeft,
                child: OutlinedButton.icon(
                  onPressed: c.addVoiceModel,
                  icon: const Icon(Icons.add),
                  label: const Text('新增一项'),
                ),
              ),
              const SizedBox(height: 12),
              Align(
                alignment: Alignment.centerRight,
                child: Obx(
                  () => FilledButton(
                    onPressed: c.savingVoiceModels.value
                        ? null
                        : c.saveVoiceModels,
                    child: c.savingVoiceModels.value
                        ? const SizedBox(
                            height: 18,
                            width: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Text('保存语音模型清单'),
                  ),
                ),
              ),
            ],
          ),
        ),
      );
    });
  }

  Widget _voiceModelRow(int index, VoiceModelOption option) {
    final providers = c.supportedProviders.toList();
    final providerValue = providers.contains(option.provider)
        ? option.provider
        : null;
    return Container(
      key: ObjectKey(option),
      margin: const EdgeInsets.only(bottom: 12),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        border: Border.all(color: Colors.grey.shade300),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: TextFormField(
                  initialValue: option.label,
                  decoration: const InputDecoration(
                    labelText: '显示名（C 端下拉看到的文字）',
                    isDense: true,
                  ),
                  onChanged: (v) => option.label = v,
                ),
              ),
              const SizedBox(width: 8),
              Switch(
                value: option.enabled,
                onChanged: (v) {
                  option.enabled = v;
                  c.voiceModels.refresh();
                },
              ),
              Text(
                option.enabled ? '启用' : '停用',
                style: const TextStyle(fontSize: 12),
              ),
              IconButton(
                tooltip: '删除该项',
                icon: const Icon(Icons.delete_outline, color: Colors.red),
                onPressed: () => c.removeVoiceModel(index),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              Expanded(
                child: DropdownButtonFormField<String>(
                  initialValue: providerValue,
                  isDense: true,
                  decoration: const InputDecoration(labelText: '供应商'),
                  items: providers
                      .map((p) => DropdownMenuItem(value: p, child: Text(p)))
                      .toList(),
                  onChanged: (v) {
                    if (v == null) return;
                    option.provider = v;
                    c.voiceModels.refresh();
                  },
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: TextFormField(
                  initialValue: option.model,
                  decoration: const InputDecoration(
                    labelText: '模型',
                    isDense: true,
                  ),
                  onChanged: (v) => option.model = v,
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          TextFormField(
            initialValue: option.endpoint,
            decoration: const InputDecoration(
              labelText: '接入地址（ws:// 或 wss://）',
              isDense: true,
            ),
            onChanged: (v) => option.endpoint = v,
          ),
          const SizedBox(height: 12),
          _voicePresetsSection(index, option),
        ],
      ),
    );
  }

  Widget _voicePresetsSection(int modelIndex, VoiceModelOption option) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            const Text(
              '预定义音色',
              style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
            ),
            const SizedBox(width: 4),
            Text(
              '(${option.voices.length})',
              style: const TextStyle(fontSize: 12, color: Colors.grey),
            ),
            const Spacer(),
            TextButton.icon(
              onPressed: () {
                option.voices.add(VoicePreset());
                c.voiceModels.refresh();
              },
              icon: const Icon(Icons.add, size: 16),
              label: const Text('添加音色', style: TextStyle(fontSize: 12)),
            ),
          ],
        ),
        if (option.voices.isEmpty)
          const Padding(
            padding: EdgeInsets.only(bottom: 4),
            child: Text(
              '暂无预定义音色，C 端将只能手动输入',
              style: TextStyle(fontSize: 12, color: Colors.grey),
            ),
          ),
        for (var vi = 0; vi < option.voices.length; vi++)
          Padding(
            padding: const EdgeInsets.only(bottom: 4),
            child: Row(
              children: [
                Expanded(
                  child: TextFormField(
                    initialValue: option.voices[vi].id,
                    decoration: const InputDecoration(
                      labelText: '音色 ID',
                      isDense: true,
                    ),
                    onChanged: (v) => option.voices[vi].id = v,
                  ),
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: TextFormField(
                    initialValue: option.voices[vi].label,
                    decoration: const InputDecoration(
                      labelText: '显示名',
                      isDense: true,
                    ),
                    onChanged: (v) => option.voices[vi].label = v,
                  ),
                ),
                IconButton(
                  tooltip: '删除音色',
                  icon: const Icon(Icons.close, size: 18),
                  onPressed: () {
                    option.voices.removeAt(vi);
                    c.voiceModels.refresh();
                  },
                ),
              ],
            ),
          ),
      ],
    );
  }

  Future<void> _pickCustomerUser(SettingsController c) async {
    final picked = await UserPickerDialog.show(
      title: '选择系统客户账户',
      mode: UserPickerMode.single,
      confirmText: '使用',
      searchHint: '搜索 ID / 账号 / 昵称 / 邮箱',
    );
    if (picked == null || picked.isEmpty) return;
    c.customerIdCtrl.text = picked.first.id;
  }
}

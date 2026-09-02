import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../core/config/app_config.dart';
import '../../shared/widgets/confirm_dialog.dart';
import '../../shared/widgets/dialog_content_box.dart';

/// 新虚拟 Key 明文只展示一次的结果弹窗：Key + CC Switch 用的 Base URL + 配置帮助。
class GatewayNewKeyDialog extends StatelessWidget {
  const GatewayNewKeyDialog({super.key, required this.plainKey});

  final String plainKey;

  /// 以不可点遮罩方式弹出；调用方应在拿到明文后立刻调用。
  static Future<void> show(String plainKey) {
    return Get.dialog(
      GatewayNewKeyDialog(plainKey: plainKey),
      barrierDismissible: false,
    );
  }

  /// Claude Code / Anthropic：CC Switch 默认会再拼 `/v1/messages`，故不要带 `/v1`。
  static String anthropicBaseUrlForCcSwitch([String? origin]) =>
      '${origin ?? AppConfig.publicOrigin}/anthropic';

  /// Codex / OpenAI 兼容：CC Switch 默认会再拼 `/v1/...`，故不要带 `/v1`。
  static String openaiBaseUrlForCcSwitch([String? origin]) =>
      '${origin ?? AppConfig.publicOrigin}/openai';

  @override
  Widget build(BuildContext context) {
    final anthropic = anthropicBaseUrlForCcSwitch();
    final openai = openaiBaseUrlForCcSwitch();
    return AlertDialog(
      insetPadding: kDialogInsetPadding,
      title: const Text('新Key已生成'),
      content: DialogContentBox(
        maxWidth: 480,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('明文只在这一次显示，请立刻复制保存：'),
            const SizedBox(height: 8),
            _CopyRow(label: '虚拟 Key', value: plainKey, monospace: true),
            const SizedBox(height: 12),
            Text(
              'CC Switch 填写（Base URL 不要加 /v1）：',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            const SizedBox(height: 6),
            _CopyRow(label: 'Claude Code · Base URL', value: anthropic),
            const SizedBox(height: 6),
            _CopyRow(label: 'Codex · Base URL', value: openai),
          ],
        ),
      ),
      actions: [
        TextButton.icon(
          onPressed: () => _showCcSwitchHelp(context, plainKey: plainKey),
          icon: const Icon(Icons.help_outline, size: 18),
          label: const Text('CC Switch 配置帮助'),
        ),
        FilledButton(onPressed: () => Get.back(), child: const Text('我已保存')),
      ],
    );
  }
}

void _showCcSwitchHelp(BuildContext context, {required String plainKey}) {
  final anthropic = GatewayNewKeyDialog.anthropicBaseUrlForCcSwitch();
  final openai = GatewayNewKeyDialog.openaiBaseUrlForCcSwitch();
  Get.dialog(
    AlertDialog(
      insetPadding: kDialogInsetPadding,
      title: const Text('在 CC Switch 中配置'),
      content: DialogContentBox(
        maxWidth: 520,
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                'CC Switch 用来给 Claude Code / Codex 切换供应商。拿到虚拟 Key 后按下面填：',
                style: Theme.of(context).textTheme.bodyMedium,
              ),
              const SizedBox(height: 12),
              const _HelpStep(index: 1, text: '打开 CC Switch →「添加供应商」或编辑已有供应商。'),
              const _HelpStep(
                index: 2,
                text: 'Claude Code 选 Anthropic 协议；Codex 选 OpenAI 协议。',
              ),
              _HelpStep(
                index: 3,
                text: 'API Key 填刚生成的虚拟 Key：',
                copyValue: plainKey,
              ),
              _HelpStep(
                index: 4,
                text: 'Claude Code 的 Base URL 填：',
                copyValue: anthropic,
              ),
              _HelpStep(
                index: 5,
                text: 'Codex 的 Base URL 填：',
                copyValue: openai,
              ),
              const _HelpStep(
                index: 6,
                text:
                    '不要开启「完整 URL 模式」（默认前缀拼接即可）。不要在 Base URL 末尾再加 /v1，否则会拼成 /v1/v1/... 导致 404。',
              ),
              const _HelpStep(
                index: 7,
                text: '保存后重启 Claude Code / Codex CLI，环境变量才会生效。',
              ),
              const SizedBox(height: 12),
              Text('补充说明', style: Theme.of(context).textTheme.titleSmall),
              const SizedBox(height: 6),
              Text(
                '• Grix 中转按请求里的 model 路由到 DeepSeek / 豆包等上游；'
                '若客户端仍发 Claude 官方模型名，请在 App「我的Grix中转」里配置模型映射或兜底模型。\n'
                '• 全球区域名把 grix.dhf.pub 换成 gb.grix.im 即可。\n'
                '• 连接器自动下发的地址带 /v1，那是给连接器路由拼接用的；'
                'CC Switch / 官方 SDK 请用本页不带 /v1 的地址。',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
          ),
        ),
      ),
      actions: [
        FilledButton(onPressed: () => Get.back(), child: const Text('知道了')),
      ],
    ),
  );
}

class _CopyRow extends StatelessWidget {
  const _CopyRow({
    required this.label,
    required this.value,
    this.monospace = false,
  });

  final String label;
  final String value;
  final bool monospace;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(label, style: Theme.of(context).textTheme.labelMedium),
        const SizedBox(height: 2),
        Row(
          children: [
            Expanded(
              child: SelectableText(
                value,
                style: TextStyle(
                  fontFamily: monospace ? 'monospace' : null,
                  fontWeight: FontWeight.w600,
                  fontSize: 13,
                ),
              ),
            ),
            IconButton(
              tooltip: '复制',
              icon: const Icon(Icons.copy, size: 18),
              onPressed: () {
                Clipboard.setData(ClipboardData(text: value));
                Toast.success('已复制到剪贴板');
              },
            ),
          ],
        ),
      ],
    );
  }
}

class _HelpStep extends StatelessWidget {
  const _HelpStep({required this.index, required this.text, this.copyValue});

  final int index;
  final String text;
  final String? copyValue;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 22,
            child: Text(
              '$index.',
              style: Theme.of(
                context,
              ).textTheme.bodyMedium?.copyWith(fontWeight: FontWeight.w700),
            ),
          ),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(text),
                if (copyValue != null) ...[
                  const SizedBox(height: 4),
                  Row(
                    children: [
                      Expanded(
                        child: SelectableText(
                          copyValue!,
                          style: const TextStyle(
                            fontFamily: 'monospace',
                            fontWeight: FontWeight.w600,
                            fontSize: 12,
                          ),
                        ),
                      ),
                      IconButton(
                        tooltip: '复制',
                        icon: const Icon(Icons.copy, size: 16),
                        onPressed: () {
                          Clipboard.setData(ClipboardData(text: copyValue!));
                          Toast.success('已复制到剪贴板');
                        },
                      ),
                    ],
                  ),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}

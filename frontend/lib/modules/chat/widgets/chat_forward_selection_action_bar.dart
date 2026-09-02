import 'package:flutter/material.dart';
import 'package:get/get.dart';

class ChatForwardSelectionActionBar extends StatelessWidget {
  const ChatForwardSelectionActionBar({
    super.key,
    required this.selectedCount,
    required this.onCancel,
    required this.onMergeForward,
    required this.onSeparateForward,
  });

  final int selectedCount;
  final VoidCallback onCancel;
  final VoidCallback onMergeForward;
  final VoidCallback onSeparateForward;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final disabled = selectedCount <= 0;

    return Container(
      width: double.infinity,
      padding: EdgeInsets.only(
        left: 12,
        right: 12,
        top: 10,
        bottom: 10 + MediaQuery.of(context).padding.bottom,
      ),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        border: Border(
          top: BorderSide(
            color: theme.colorScheme.outline.withValues(alpha: 0.12),
          ),
        ),
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            'chat_forward_selected_count'.trParams({
              'count': selectedCount.toString(),
            }),
            style: TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w600,
              color: theme.colorScheme.secondary.withValues(alpha: 0.85),
            ),
          ),
          const SizedBox(height: 10),
          Row(
            children: [
              OutlinedButton(
                onPressed: onCancel,
                // 全局按钮主题把 minimumSize 设为 Size.fromHeight(...)，即最小宽度为
                // double.infinity（默认满宽）。取消按钮是 Row 里的非 Expanded 子项，
                // Row 会先用无界主轴宽度测量它，叠加无限最小宽度后被要求“无限宽”，
                // 在 release 包里导致整行按钮无法完成布局而静默不绘制（转发按钮消失）。
                // 这里覆盖为有界最小宽度，让取消按钮按内容自适应，行内即可正常排布。
                style: OutlinedButton.styleFrom(minimumSize: const Size(0, 40)),
                child: Text('common_cancel'.tr),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: FilledButton(
                  onPressed: disabled ? null : onMergeForward,
                  child: Text('chat_forward_merge'.tr),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: FilledButton(
                  onPressed: disabled ? null : onSeparateForward,
                  child: Text('chat_forward_separate'.tr),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

import 'package:flutter/material.dart';
import 'package:get/get.dart';

/// 主色调「极速接入」入口按钮：宽度最多占屏幕宽度的 66%，避免铺满整行。
/// 供 Agent 列表空态、消息列表空态复用，保持两处入口视觉一致。
///
/// 用 [MediaQuery] 而非 [LayoutBuilder] 取宽度：本按钮会出现在
/// `SliverFillRemaining(hasScrollBody: false)` 等需要计算 intrinsic 尺寸的
/// 容器里，LayoutBuilder 在该场景下会直接抛出布局异常。
class AgentQuickAccessButton extends StatelessWidget {
  const AgentQuickAccessButton({super.key, required this.onPressed});

  final VoidCallback onPressed;

  static const double _maxWidthFactor = 0.66;

  @override
  Widget build(BuildContext context) {
    final maxWidth = MediaQuery.sizeOf(context).width * _maxWidthFactor;
    return ConstrainedBox(
      constraints: BoxConstraints(maxWidth: maxWidth),
      child: FilledButton.icon(
        key: const Key('agent-quick-access-button'),
        onPressed: onPressed,
        icon: const Icon(Icons.bolt_rounded),
        label: Text('ai_agent_quick_entry'.tr),
      ),
    );
  }
}

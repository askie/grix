import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../../app/settings/chat_thinking_collapse_service.dart';
import '../../../../app/themes/app_theme.dart';
import '../models/chat_thinking_card_data.dart';

class ChatThinkingCardView extends StatefulWidget {
  const ChatThinkingCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
  });

  final ChatThinkingCardData card;
  final bool isMine;
  final double fontScale;

  @override
  State<ChatThinkingCardView> createState() => _ChatThinkingCardViewState();
}

class _ChatThinkingCardViewState extends State<ChatThinkingCardView> {
  // 全局服务不可用时（如独立 widget 测试）回退到本地折叠状态。
  bool _collapsedFallback = false;

  @override
  Widget build(BuildContext context) {
    final service = Get.isRegistered<ChatThinkingCollapseService>()
        ? Get.find<ChatThinkingCollapseService>()
        : null;

    if (service == null) {
      return _buildCard(
        context,
        collapsed: _collapsedFallback,
        onToggle: () =>
            setState(() => _collapsedFallback = !_collapsedFallback),
      );
    }

    return Obx(
      () => _buildCard(
        context,
        collapsed: service.collapsed,
        onToggle: service.toggle,
      ),
    );
  }

  Widget _buildCard(
    BuildContext context, {
    required bool collapsed,
    required VoidCallback onToggle,
  }) {
    final theme = Theme.of(context);
    final accentColor = theme.colorScheme.tertiary;

    final titleStyle = AppTheme.applyTextFont(
      theme.textTheme.labelMedium?.copyWith(
            fontSize: 11 * widget.fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ) ??
          TextStyle(
            fontSize: 11 * widget.fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ),
    );

    final contentStyle = AppTheme.applyTextFont(
      theme.textTheme.bodySmall?.copyWith(
            fontSize: 12 * widget.fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.7),
            height: 1.5,
          ) ??
          TextStyle(
            fontSize: 12 * widget.fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.7),
            height: 1.5,
          ),
    );

    final hintStyle = AppTheme.applyTextFont(
      theme.textTheme.labelSmall?.copyWith(
            fontSize: 10.5 * widget.fontScale,
            fontWeight: FontWeight.w600,
            color: accentColor.withValues(alpha: 0.82),
          ) ??
          TextStyle(
            fontSize: 10.5 * widget.fontScale,
            fontWeight: FontWeight.w600,
            color: accentColor.withValues(alpha: 0.82),
          ),
    );

    final expanded = !collapsed;
    final lastLine = _lastNonEmptyLine(widget.card.displayContent);

    return AnimatedSize(
      duration: const Duration(milliseconds: 160),
      curve: Curves.easeOutCubic,
      child: Container(
        width: double.infinity,
        constraints: const BoxConstraints(maxWidth: 360),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        decoration: BoxDecoration(
          color: accentColor.withValues(alpha: 0.06),
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: accentColor.withValues(alpha: 0.14)),
        ),
        child: Material(
          color: Colors.transparent,
          child: InkWell(
            borderRadius: BorderRadius.circular(10),
            onTap: onToggle,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Row(
                  children: [
                    Container(
                      width: 30,
                      height: 30,
                      decoration: BoxDecoration(
                        color: accentColor.withValues(alpha: 0.12),
                        borderRadius: BorderRadius.circular(9),
                      ),
                      child: Icon(
                        Icons.psychology_outlined,
                        size: 17,
                        color: accentColor,
                      ),
                    ),
                    const SizedBox(width: 9),
                    Expanded(
                      child: Text(
                        'chat_message_card_thinking_label'.tr,
                        style: titleStyle,
                      ),
                    ),
                    Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          expanded
                              ? 'chat_message_card_thinking_collapse'.tr
                              : 'chat_message_card_thinking_expand'.tr,
                          style: hintStyle,
                          overflow: TextOverflow.ellipsis,
                        ),
                        const SizedBox(width: 2),
                        Icon(
                          expanded
                              ? Icons.expand_less_rounded
                              : Icons.expand_more_rounded,
                          size: 18,
                          color: accentColor.withValues(alpha: 0.9),
                        ),
                      ],
                    ),
                  ],
                ),
                if (expanded) ...[
                  const SizedBox(height: 8),
                  Text(
                    widget.card.displayContent,
                    style: contentStyle,
                  ),
                ] else if (lastLine.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text(
                    lastLine,
                    style: contentStyle,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }

  /// 取内容中最后一个非空行，用于折叠态动态显示思考进度。
  String _lastNonEmptyLine(String text) {
    final lines = text.split('\n').where((line) => line.trim().isNotEmpty);
    return lines.isEmpty ? '' : lines.last.trim();
  }
}

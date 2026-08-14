import 'package:flutter/material.dart';

import '../../../../app/themes/app_theme.dart';
import '../models/chat_progress_card_data.dart';

/// 进度条卡片视图。
///
/// 上方一行说明文字 + 右侧百分比，下方一条进度条。
/// 当卡片为不确定态（[ChatProgressCardData.isIndeterminate]）时，进度条
/// 渲染为循环动画，且不展示百分比数字。
///
/// 卡片的「随进度更新」由消息层（同一 message_id 复发新内容触发原地刷新）
/// 负责，此视图始终按当前数据无状态渲染。
class ChatProgressCardView extends StatelessWidget {
  const ChatProgressCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
  });

  final ChatProgressCardData card;
  final bool isMine;
  final double fontScale;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final accentColor = _resolveAccentColor(theme);

    final labelStyle = AppTheme.applyTextFont(
      theme.textTheme.bodyMedium?.copyWith(
            fontSize: 13 * fontScale,
            color: theme.colorScheme.onSurface,
            height: 1.45,
            fontWeight: FontWeight.w600,
          ) ??
          TextStyle(
            fontSize: 13 * fontScale,
            color: theme.colorScheme.onSurface,
            height: 1.45,
            fontWeight: FontWeight.w600,
          ),
    );
    final percentStyle = AppTheme.applyTextFont(
      theme.textTheme.labelMedium?.copyWith(
            fontSize: 12 * fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.95),
            letterSpacing: 0.2,
          ) ??
          TextStyle(
            fontSize: 12 * fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.95),
            letterSpacing: 0.2,
          ),
    );

    final percent = card.clampedPercent;

    return Container(
      key: const Key('chat_message_card_progress'),
      constraints: const BoxConstraints(minWidth: 240, maxWidth: 360),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: accentColor.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: accentColor.withValues(alpha: 0.18)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Text(
                  card.displayLabel,
                  style: labelStyle,
                ),
              ),
              if (percent != null) ...[
                const SizedBox(width: 8),
                Text(
                  '$percent%',
                  key: const Key('chat_message_card_progress_percent'),
                  style: percentStyle,
                ),
              ],
            ],
          ),
          const SizedBox(height: 10),
          ClipRRect(
            borderRadius: BorderRadius.circular(999),
            child: LinearProgressIndicator(
              key: const Key('chat_message_card_progress_bar'),
              value: card.fraction,
              minHeight: 6,
              backgroundColor: accentColor.withValues(alpha: 0.14),
              valueColor: AlwaysStoppedAnimation<Color>(accentColor),
            ),
          ),
        ],
      ),
    );
  }

  Color _resolveAccentColor(ThemeData theme) {
    final percent = card.clampedPercent;
    if (percent != null && percent >= 100) {
      return Colors.green.shade700;
    }
    return isMine ? theme.colorScheme.primary : theme.colorScheme.secondary;
  }
}

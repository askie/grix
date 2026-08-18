import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../../app/themes/app_theme.dart';
import '../models/chat_agent_status_card_data.dart';
import '../services/chat_agent_card_text_localizer.dart';

class ChatAgentStatusCardView extends StatelessWidget {
  const ChatAgentStatusCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
  });

  final ChatAgentStatusCardData card;
  final bool isMine;
  final double fontScale;

  bool get _isSessionCard => card.displayCategory == 'session';

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final viewportWidth = MediaQuery.sizeOf(context).width;
    final accentColor = _resolveAccentColor(theme);
    final titleStyle = AppTheme.applyTextFont(
      theme.textTheme.labelMedium?.copyWith(
            fontSize: 11 * fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ) ??
          TextStyle(
            fontSize: 11 * fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ),
    );
    final bodyStyle = AppTheme.applyTextFont(
      theme.textTheme.bodySmall?.copyWith(
            fontSize: 12 * fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.8),
            height: 1.45,
          ) ??
          TextStyle(
            fontSize: 12 * fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.8),
            height: 1.45,
          ),
    );

    return Container(
      key: const Key('chat_message_card_agent_status'),
      constraints: BoxConstraints(
        minWidth: 240,
        maxWidth: viewportWidth * 0.8,
      ),
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
            children: [
              Container(
                width: 34,
                height: 34,
                decoration: BoxDecoration(
                  color: accentColor.withValues(alpha: 0.14),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Icon(_resolveIcon(), size: 18, color: accentColor),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      'chat_message_card_agent_status_label'.tr,
                      style: titleStyle,
                    ),
                    const SizedBox(height: 4),
                    Text(_resolveCategoryLabel(), style: bodyStyle),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Text(
            ChatAgentCardTextLocalizer.localize(card.displaySummary),
            style: bodyStyle,
          ),
          // 会话类卡片不展示关联 ID（内部会话编号，对用户无意义）。
          if (!_isSessionCard && card.displayReferenceId.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(
              '${'chat_message_card_agent_status_reference'.tr}: ${card.displayReferenceId}',
              style: bodyStyle,
            ),
          ],
          // 详情是否下发由后端决定：绑定成功卡已不携带技术详情，
          // where/status 查询卡仍携带工作区详情，前端有则照显。
          if (card.displayDetailText.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(
              ChatAgentCardTextLocalizer.localize(card.displayDetailText),
              style: bodyStyle,
            ),
          ],
        ],
      ),
    );
  }

  Color _resolveAccentColor(ThemeData theme) {
    switch (card.displayStatus) {
      case 'success':
        return Colors.green.shade700;
      case 'warning':
        return Colors.orange.shade700;
      case 'error':
        return theme.colorScheme.error;
      default:
        return isMine ? theme.colorScheme.primary : theme.colorScheme.secondary;
    }
  }

  IconData _resolveIcon() {
    switch (card.displayStatus) {
      case 'success':
        return Icons.task_alt_rounded;
      case 'warning':
        return Icons.warning_amber_rounded;
      case 'error':
        return Icons.error_outline_rounded;
      default:
        return Icons.info_outline_rounded;
    }
  }

  String _resolveCategoryLabel() {
    switch (card.displayCategory) {
      case 'approval':
        return 'chat_message_card_agent_status_category_approval'.tr;
      case 'question':
        return 'chat_message_card_agent_status_category_question'.tr;
      case 'access':
        return 'chat_message_card_agent_status_category_access'.tr;
      case 'session':
        return 'chat_message_card_agent_status_category_session'.tr;
      default:
        return card.displayCategory;
    }
  }
}

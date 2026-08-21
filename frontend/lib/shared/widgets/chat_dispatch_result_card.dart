import 'package:flutter/material.dart';

import '../../app/themes/app_theme.dart';
import '../utils/chat_message_content.dart';

class ChatDispatchResultCard extends StatelessWidget {
  const ChatDispatchResultCard({
    super.key,
    required this.result,
    required this.fontScale,
  });

  final ChatDispatchResultContent result;
  final double fontScale;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final statusColor = _statusColor(theme);
    final bodyStyle = AppTheme.applyTextFont(
      theme.textTheme.bodyMedium?.copyWith(
            fontSize: 14 * fontScale,
            color: theme.colorScheme.onSurface,
            height: 1.42,
          ) ??
          TextStyle(
            fontSize: 14 * fontScale,
            color: theme.colorScheme.onSurface,
            height: 1.42,
          ),
    );

    return Column(
      key: const Key('chat_dispatch_result_card'),
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Container(
          key: const Key('chat_dispatch_result_status_pill'),
          padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 3),
          decoration: BoxDecoration(
            color: statusColor.withValues(alpha: 0.12),
            borderRadius: BorderRadius.circular(999),
            border: Border.all(color: statusColor.withValues(alpha: 0.28)),
          ),
          child: Text(
            result.status,
            style: bodyStyle.copyWith(
              color: statusColor,
              fontSize: 12 * fontScale,
              fontWeight: FontWeight.w600,
              height: 1.2,
            ),
          ),
        ),
        const SizedBox(height: 8),
        Text(
          result.summary,
          key: const Key('chat_dispatch_result_summary'),
          style: bodyStyle.copyWith(fontWeight: FontWeight.w700),
        ),
        if (result.detail.isNotEmpty) ...[
          const SizedBox(height: 6),
          Text(
            result.detail,
            key: const Key('chat_dispatch_result_detail'),
            style: bodyStyle,
          ),
        ],
        const SizedBox(height: 14),
        Text(
          'ID：  ${result.sessionId}',
          key: const Key('chat_dispatch_result_id'),
          style: bodyStyle.copyWith(
            fontSize: 12 * fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.68),
          ),
        ),
      ],
    );
  }

  Color _statusColor(ThemeData theme) {
    switch (result.status.toLowerCase()) {
      case 'completed':
        return AppTheme.successColor;
      case 'failed':
        return theme.colorScheme.error;
      case 'blocked':
        return Colors.orange.shade700;
      default:
        return theme.colorScheme.primary;
    }
  }
}

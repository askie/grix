import 'package:flutter/material.dart';

import '../../../../app/themes/app_theme.dart';
import '../models/chat_conversation_card_data.dart';

class ChatConversationCardView extends StatelessWidget {
  const ChatConversationCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
    this.onTap,
  });

  final ChatConversationCardData card;
  final bool isMine;
  final double fontScale;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final accentColor =
        isMine ? theme.colorScheme.primary : theme.colorScheme.secondary;
    final icon = card.normalizedSessionType == 'group'
        ? Icons.groups_rounded
        : Icons.chat_bubble_outline_rounded;
    final titleStyle = AppTheme.applyTextFont(
      theme.textTheme.bodyMedium?.copyWith(
            fontSize: 13 * fontScale,
            fontWeight: FontWeight.w600,
            color: accentColor,
            decoration: TextDecoration.underline,
            decorationColor: accentColor.withValues(alpha: 0.55),
          ) ??
          TextStyle(
            fontSize: 13 * fontScale,
            fontWeight: FontWeight.w600,
            color: accentColor,
            decoration: TextDecoration.underline,
            decorationColor: accentColor.withValues(alpha: 0.55),
          ),
    );

    final cardBody = Container(
      key: const Key('chat_message_card_conversation'),
      constraints: const BoxConstraints(maxWidth: 260),
      padding: const EdgeInsets.symmetric(horizontal: 2, vertical: 2),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            icon,
            size: 14 * fontScale,
            color: accentColor.withValues(alpha: 0.85),
          ),
          const SizedBox(width: 4),
          Flexible(
            child: Text(
              card.displayTitle,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: titleStyle,
            ),
          ),
        ],
      ),
    );

    if (onTap == null) {
      return cardBody;
    }

    return Material(
      type: MaterialType.transparency,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(6),
        child: cardBody,
      ),
    );
  }
}

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../../app/themes/app_theme.dart';
import '../../../../shared/widgets/session_avatar.dart';
import '../models/chat_user_profile_card_data.dart';

class ChatUserProfileCardView extends StatelessWidget {
  const ChatUserProfileCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
    this.onTap,
  });

  final ChatUserProfileCardData card;
  final bool isMine;
  final double fontScale;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final accentColor =
        isMine ? theme.colorScheme.primary : theme.colorScheme.secondary;
    final titleStyle = AppTheme.applyTextFont(
      theme.textTheme.labelMedium?.copyWith(
            fontSize: 11 * fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.88),
            letterSpacing: 0.2,
          ) ??
          TextStyle(
            fontSize: 11 * fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.88),
            letterSpacing: 0.2,
          ),
    );
    final nameStyle = AppTheme.applyTextFont(
      theme.textTheme.titleMedium?.copyWith(
            fontSize: 16 * fontScale,
            fontWeight: FontWeight.w700,
            color: theme.colorScheme.onSurface,
          ) ??
          TextStyle(
            fontSize: 16 * fontScale,
            fontWeight: FontWeight.w700,
            color: theme.colorScheme.onSurface,
          ),
    );
    final seed = card.userId.trim().isNotEmpty ? card.userId : card.displayName;

    final cardBody = Container(
      key: const Key('chat_message_card_user_profile'),
      constraints: const BoxConstraints(minWidth: 220, maxWidth: 320),
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
          Text(
            'chat_message_card_user_profile_label'.tr,
            style: titleStyle,
          ),
          const SizedBox(height: 10),
          Row(
            children: [
              SessionAvatar(
                isGroup: false,
                avatarTitle: card.displayName,
                avatarColor: AppTheme.getAvatarColor(seed),
                avatarUrl: card.avatarUrl,
                size: 48,
                borderRadius: 12,
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Text(
                  card.displayName,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: nameStyle,
                ),
              ),
            ],
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
        borderRadius: BorderRadius.circular(12),
        child: cardBody,
      ),
    );
  }
}

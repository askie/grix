import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../controllers/chat_controller.dart';

/// Fixed bar pinned to the top of the message list showing the session's one
/// pinned message. Tapping it scrolls to and flashes the original message.
class ChatPinnedMessageBar extends StatelessWidget {
  const ChatPinnedMessageBar({super.key, required this.controller});

  final ChatController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final pinned = controller.pinnedMessage.value;
      if (pinned == null) {
        return const SizedBox.shrink();
      }
      final theme = Theme.of(context);
      return Material(
        color: theme.colorScheme.surface,
        child: InkWell(
          onTap: () => controller.jumpToPinnedMessage(),
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
            decoration: BoxDecoration(
              border: Border(
                bottom: BorderSide(
                  color: theme.colorScheme.onSurface.withValues(alpha: 0.08),
                ),
              ),
            ),
            child: Row(
              children: [
                Icon(
                  Icons.push_pin_rounded,
                  size: 16,
                  color: theme.colorScheme.secondary,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    pinned.summary.isEmpty ? 'chat_pin'.tr : pinned.summary,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 13,
                      color: theme.colorScheme.onSurface.withValues(
                        alpha: 0.78,
                      ),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      );
    });
  }
}

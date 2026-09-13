import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../controllers/chat_controller.dart';

/// Floating circular button at the message list's bottom-right corner that
/// appears when the reader scrolled more than ~one viewport away from the
/// bottom, or whenever the loaded window no longer contains the session's
/// latest messages. Tapping returns to the bottom (paging straight back to
/// the latest page when the newest messages were trimmed out of the window)
/// and resumes bottom-follow. Messages that arrived while away from the
/// bottom show up as a count badge.
class ChatScrollToBottomButton extends StatelessWidget {
  const ChatScrollToBottomButton({super.key, required this.controller});

  final ChatController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      if (!controller.scrollToBottomButtonVisible.value) {
        return const SizedBox.shrink();
      }
      final theme = Theme.of(context);
      final count = controller.scrollToBottomNewMessageCount.value;
      return Positioned(
        right: 12,
        bottom: 12,
        child: Semantics(
          button: true,
          label: 'chat_scroll_to_bottom'.tr,
          child: Badge(
            isLabelVisible: count > 0,
            label: Text(count > 99 ? '99+' : '$count'),
            child: Material(
              color: theme.colorScheme.surface,
              shape: CircleBorder(
                side: BorderSide(color: theme.colorScheme.outlineVariant),
              ),
              elevation: 4,
              shadowColor: Colors.black.withValues(alpha: 0.2),
              clipBehavior: Clip.antiAlias,
              child: InkWell(
                onTap: () =>
                    unawaited(controller.onScrollToBottomButtonPressed()),
                child: SizedBox(
                  width: 44,
                  height: 44,
                  child: Icon(
                    Icons.arrow_downward_rounded,
                    size: 22,
                    color: theme.colorScheme.onSurface,
                  ),
                ),
              ),
            ),
          ),
        ),
      );
    });
  }
}

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../controllers/chat_controller.dart';

/// Floating circular button (same chrome as [ChatScrollToBottomButton]) that
/// appears when one or more off-screen messages were edited in place. Shows an
/// upward arrow with a count badge; tap jumps to the earliest edit and flashes
/// it. Stacked above the scroll-to-bottom button on the bottom-right.
class ChatUpdatedAbovePill extends StatelessWidget {
  const ChatUpdatedAbovePill({super.key, required this.controller});

  final ChatController controller;

  static const Key buttonKey = ValueKey('chat-updated-above-button');

  static const double _inset = 12;
  static const double _size = 44;
  static const double _gap = 8;

  /// Bottom offset of the scroll-to-bottom button: inset + size.
  static const double _scrollToBottomOccupied =
      _inset + _size; // 12 + 44

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final count = controller.pendingUpdatedMessageIds.length;
      if (count == 0) {
        return const SizedBox.shrink();
      }
      final theme = Theme.of(context);
      final scrollButtonVisible =
          controller.scrollToBottomButtonVisible.value;
      final bottom = scrollButtonVisible
          ? _scrollToBottomOccupied + _gap
          : _inset;

      return Positioned(
        right: _inset,
        bottom: bottom,
        child: Semantics(
          button: true,
          label: 'chat_updated_above_pill'.trParams({'count': '$count'}),
          child: Badge(
            isLabelVisible: true,
            label: Text(count > 99 ? '99+' : '$count'),
            child: Material(
              key: buttonKey,
              color: theme.colorScheme.surface,
              shape: CircleBorder(
                side: BorderSide(color: theme.colorScheme.outlineVariant),
              ),
              elevation: 4,
              shadowColor: Colors.black.withValues(alpha: 0.2),
              clipBehavior: Clip.antiAlias,
              child: InkWell(
                onTap: () => controller.jumpToEarliestUpdatedMessage(),
                child: SizedBox(
                  width: _size,
                  height: _size,
                  child: Icon(
                    Icons.arrow_upward_rounded,
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

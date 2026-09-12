import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../controllers/chat_controller.dart';

/// Floating bottom pill telling the reader that one or more messages above
/// (or otherwise off-screen) were edited in place since they last looked,
/// with a tap target that scrolls to the earliest one and flashes it.
class ChatUpdatedAbovePill extends StatelessWidget {
  const ChatUpdatedAbovePill({super.key, required this.controller});

  final ChatController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final count = controller.pendingUpdatedMessageIds.length;
      if (count == 0) {
        return const SizedBox.shrink();
      }
      final theme = Theme.of(context);
      return Positioned(
        left: 0,
        right: 0,
        bottom: 12,
        child: Center(
          child: Material(
            color: Colors.transparent,
            child: InkWell(
              borderRadius: BorderRadius.circular(20),
              onTap: () => controller.jumpToEarliestUpdatedMessage(),
              child: Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: 14,
                  vertical: 8,
                ),
                decoration: BoxDecoration(
                  color: theme.colorScheme.primary,
                  borderRadius: BorderRadius.circular(20),
                  boxShadow: [
                    BoxShadow(
                      color: Colors.black.withValues(alpha: 0.15),
                      blurRadius: 8,
                      offset: const Offset(0, 2),
                    ),
                  ],
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      Icons.arrow_upward_rounded,
                      size: 16,
                      color: theme.colorScheme.onPrimary,
                    ),
                    const SizedBox(width: 6),
                    Text(
                      'chat_updated_above_pill'.trParams({'count': '$count'}),
                      style: TextStyle(
                        color: theme.colorScheme.onPrimary,
                        fontSize: 13,
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      );
    });
  }
}

import 'package:flutter/material.dart';

class ChatSelectableMessageBubble extends StatelessWidget {
  const ChatSelectableMessageBubble({
    super.key,
    required this.child,
    required this.isMine,
    required this.selectionMode,
    required this.selected,
    this.onTap,
    this.onLongPress,
  });

  final Widget child;
  final bool isMine;
  final bool selectionMode;
  final bool selected;
  final VoidCallback? onTap;
  final VoidCallback? onLongPress;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final highlightedChild = AnimatedContainer(
      duration: const Duration(milliseconds: 120),
      decoration: BoxDecoration(
        color: selectionMode && selected
            ? theme.primaryColor.withValues(alpha: 0.08)
            : Colors.transparent,
        borderRadius: BorderRadius.circular(12),
      ),
      child: child,
    );

    final content = selectionMode
        ? Stack(
            clipBehavior: Clip.none,
            children: [
              highlightedChild,
              Positioned(
                // 上移到气泡左上/右上角外缘（圆角空白处），避免压住首行文字。
                // Stack 已 clipBehavior: Clip.none，可绘制到气泡边界之外。
                top: -8,
                left: isMine ? null : 6,
                right: isMine ? 6 : null,
                child: _SelectionIndicator(
                  selected: selected,
                  color: theme.primaryColor,
                ),
              ),
            ],
          )
        : highlightedChild;

    if (onTap == null && onLongPress == null) {
      return content;
    }

    return GestureDetector(
      behavior: HitTestBehavior.translucent,
      onTap: onTap,
      onLongPress: onLongPress,
      child: content,
    );
  }
}

class _SelectionIndicator extends StatelessWidget {
  const _SelectionIndicator({
    required this.selected,
    required this.color,
  });

  final bool selected;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return AnimatedContainer(
      duration: const Duration(milliseconds: 120),
      width: 20,
      height: 20,
      decoration: BoxDecoration(
        color: selected ? color : Colors.white,
        shape: BoxShape.circle,
        border: Border.all(
          color: selected ? color : color.withValues(alpha: 0.55),
          width: 1.5,
        ),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.08),
            blurRadius: 4,
            offset: const Offset(0, 1),
          ),
        ],
      ),
      child: selected
          ? const Icon(
              Icons.check_rounded,
              size: 14,
              color: Colors.white,
            )
          : null,
    );
  }
}

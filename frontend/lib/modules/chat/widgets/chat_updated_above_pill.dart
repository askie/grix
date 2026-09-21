import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../controllers/chat_controller.dart';

/// Floating bottom pill telling the reader that one or more messages above
/// (or otherwise off-screen) were edited in place since they last looked,
/// with a tap target that scrolls to the earliest one and flashes it.
class ChatUpdatedAbovePill extends StatelessWidget {
  const ChatUpdatedAbovePill({super.key, required this.controller});

  final ChatController controller;

  /// Symmetric side gutter so the pill stays truly centered (matching the
  /// composing status capsule) while still clearing the scroll-to-bottom
  /// button (44px) and its 12px inset on the right.
  static const double _sideInset = 68;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final count = controller.pendingUpdatedMessageIds.length;
      if (count == 0) {
        return const SizedBox.shrink();
      }
      final theme = Theme.of(context);
      final label = 'chat_updated_above_pill'.trParams({'count': '$count'});
      final textStyle = TextStyle(
        color: theme.colorScheme.onPrimary,
        fontSize: 13,
        fontWeight: FontWeight.w500,
      );

      return Positioned(
        left: _sideInset,
        right: _sideInset,
        bottom: 12,
        child: Align(
          alignment: Alignment.center,
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
                    Flexible(
                      child: _PillMarqueeText(text: label, style: textStyle),
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

/// Single-line label that scrolls horizontally when it cannot fit the
/// available width (long locales / narrow screens).
class _PillMarqueeText extends StatefulWidget {
  const _PillMarqueeText({required this.text, required this.style});

  final String text;
  final TextStyle style;

  @override
  State<_PillMarqueeText> createState() => _PillMarqueeTextState();
}

class _PillMarqueeTextState extends State<_PillMarqueeText>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller;

  static const _cycle = Duration(seconds: 5);

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(vsync: this, duration: _cycle);
  }

  @override
  void didUpdateWidget(covariant _PillMarqueeText oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.text != widget.text) {
      _controller
        ..stop()
        ..value = 0;
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _syncAnimation({required bool needsScroll}) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      if (needsScroll) {
        if (!_controller.isAnimating) {
          _controller.repeat();
        }
      } else if (_controller.isAnimating || _controller.value != 0) {
        _controller
          ..stop()
          ..value = 0;
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final direction = Directionality.of(context);
    return LayoutBuilder(
      builder: (context, constraints) {
        final maxWidth = constraints.maxWidth;
        final painter = TextPainter(
          text: TextSpan(text: widget.text, style: widget.style),
          maxLines: 1,
          textDirection: direction,
        )..layout();
        final textWidth = painter.width;
        final textHeight = painter.height;
        final needsScroll = textWidth > maxWidth + 0.5;
        _syncAnimation(needsScroll: needsScroll);

        if (!needsScroll) {
          return Text(
            widget.text,
            style: widget.style,
            maxLines: 1,
            softWrap: false,
          );
        }

        final overflow = textWidth - maxWidth;
        final scrollSign = direction == TextDirection.rtl ? 1.0 : -1.0;

        return SizedBox(
          width: maxWidth,
          height: textHeight,
          child: ClipRect(
            child: AnimatedBuilder(
              animation: _controller,
              builder: (context, child) {
                final t = _controller.value;
                // Hold at start, scroll, hold at end, then jump back via repeat.
                final double progress;
                if (t < 0.18) {
                  progress = 0;
                } else if (t < 0.82) {
                  progress = (t - 0.18) / 0.64;
                } else {
                  progress = 1;
                }
                return Transform.translate(
                  offset: Offset(scrollSign * overflow * progress, 0),
                  child: child,
                );
              },
              child: SizedBox(
                width: textWidth,
                child: Text(
                  widget.text,
                  style: widget.style,
                  maxLines: 1,
                  softWrap: false,
                ),
              ),
            ),
          ),
        );
      },
    );
  }
}

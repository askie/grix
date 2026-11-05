import 'dart:math' as math;

import 'package:flutter/material.dart';

class SessionStatusIcon extends StatelessWidget {
  const SessionStatusIcon({
    super.key,
    required this.isPinned,
    required this.isActive,
    this.pinSize = 13,
    this.dotSize = 8,
    this.spacing = 4,
    this.pinColor,
  });

  final bool isPinned;
  final bool isActive;
  final double pinSize;
  final double dotSize;
  final double spacing;
  final Color? pinColor;

  @override
  Widget build(BuildContext context) {
    if (!isPinned && !isActive) return const SizedBox.shrink();

    final theme = Theme.of(context);
    final slotSize = math.max(pinSize, dotSize);

    final Widget content;
    if (isPinned) {
      content = Icon(
        Icons.push_pin_rounded,
        size: pinSize,
        color: isActive ? Colors.green : (pinColor ?? theme.primaryColor),
      );
    } else {
      content = Container(
        width: dotSize,
        height: dotSize,
        decoration: const BoxDecoration(
          color: Colors.green,
          shape: BoxShape.circle,
        ),
      );
    }

    return Padding(
      padding: EdgeInsets.only(right: spacing),
      child: SizedBox(
        width: slotSize,
        height: slotSize,
        child: Center(child: content),
      ),
    );
  }
}


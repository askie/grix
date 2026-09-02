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

    if (isPinned && isActive) {
      return Padding(
        padding: EdgeInsets.only(right: spacing),
        child: Icon(Icons.push_pin_rounded, size: pinSize, color: Colors.green),
      );
    }

    if (isPinned) {
      return Padding(
        padding: EdgeInsets.only(right: spacing),
        child: Icon(
          Icons.push_pin_rounded,
          size: pinSize,
          color: pinColor ?? theme.primaryColor,
        ),
      );
    }

    return Container(
      width: dotSize,
      height: dotSize,
      margin: EdgeInsets.only(right: spacing),
      decoration: const BoxDecoration(
        color: Colors.green,
        shape: BoxShape.circle,
      ),
    );
  }
}

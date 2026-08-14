import 'package:flutter/material.dart';

class ProgressDetailBottomSheet extends StatelessWidget {
  const ProgressDetailBottomSheet({
    super.key,
    required this.description,
    required this.percent,
    required this.detail,
    required this.accentColor,
  });

  final String description;
  final double percent;
  final String detail;
  final Color accentColor;

  static Future<void> show(
    BuildContext context, {
    required String description,
    required double percent,
    required String detail,
    required Color accentColor,
  }) {
    return showModalBottomSheet<void>(
      context: context,
      builder: (_) => ProgressDetailBottomSheet(
        description: description,
        percent: percent,
        detail: detail,
        accentColor: accentColor,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final clamped = percent.clamp(0.0, 100.0);

    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 16, 20, 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // Handle bar
            Center(
              child: Container(
                width: 36,
                height: 4,
                margin: const EdgeInsets.only(bottom: 16),
                decoration: BoxDecoration(
                  color: theme.dividerColor,
                  borderRadius: BorderRadius.circular(2),
                ),
              ),
            ),
            // Description
            Text(
              description,
              style: TextStyle(
                fontSize: 14,
                color: theme.colorScheme.onSurface.withValues(alpha: 0.7),
              ),
            ),
            const SizedBox(height: 12),
            // Large percentage display
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                Text(
                  clamped.toStringAsFixed(clamped == clamped.truncateToDouble() ? 0 : 1),
                  style: TextStyle(
                    fontSize: 36,
                    fontWeight: FontWeight.w700,
                    color: accentColor,
                    height: 1.0,
                  ),
                ),
                Padding(
                  padding: const EdgeInsets.only(left: 2, bottom: 4),
                  child: Text(
                    '%',
                    style: TextStyle(
                      fontSize: 16,
                      fontWeight: FontWeight.w600,
                      color: accentColor,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            // Linear progress bar
            ClipRRect(
              borderRadius: BorderRadius.circular(3),
              child: SizedBox(
                height: 6,
                child: LinearProgressIndicator(
                  value: clamped / 100.0,
                  backgroundColor: accentColor.withValues(alpha: 0.12),
                  valueColor: AlwaysStoppedAnimation(accentColor),
                ),
              ),
            ),
            // Detail text
            if (detail.isNotEmpty) ...[
              const SizedBox(height: 16),
              Text(
                detail,
                style: TextStyle(
                  fontSize: 13,
                  color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
                  height: 1.6,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

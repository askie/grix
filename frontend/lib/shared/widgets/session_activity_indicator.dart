import 'package:flutter/material.dart';
import '../../app/themes/app_theme.dart';
import 'stream_pending_indicator.dart';

class SessionActivityIndicator extends StatelessWidget {
  final String label;
  final Widget? trailing;

  const SessionActivityIndicator({
    super.key,
    required this.label,
    this.trailing,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final backgroundColor = theme.brightness == Brightness.dark
        ? AppTheme.darkCard
        : Colors.white;
    final textColor = _resolveTextColor(backgroundColor);

    return Align(
      alignment: Alignment.center,
      child: Container(
        margin: const EdgeInsets.symmetric(vertical: 4, horizontal: 8),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        decoration: BoxDecoration(
          color: backgroundColor,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(
            color: theme.colorScheme.outline.withValues(alpha: 0.7),
          ),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: 0.05),
              blurRadius: 2,
              offset: const Offset(0, 1),
            ),
          ],
        ),
        constraints: BoxConstraints(
          maxWidth: MediaQuery.of(context).size.width * 0.8,
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const StreamPendingIndicator(color: Colors.green),
            const SizedBox(width: 10),
            Flexible(
              child: Text(
                label,
                style: AppTheme.applyTextFont(
                  TextStyle(
                    color: textColor.withValues(alpha: 0.76),
                    fontSize: 13,
                    fontWeight: FontWeight.w500,
                  ),
                ),
              ),
            ),
            if (trailing != null) ...[const SizedBox(width: 10), trailing!],
          ],
        ),
      ),
    );
  }

  Color _resolveTextColor(Color backgroundColor) {
    return AppTheme.readableTextColorForBackground(backgroundColor);
  }
}

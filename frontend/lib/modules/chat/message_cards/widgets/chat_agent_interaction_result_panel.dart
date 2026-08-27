import 'package:flutter/material.dart';

import '../../../../app/themes/app_theme.dart';

enum ChatAgentInteractionResultTone { pending, success, warning, error, info }

class ChatAgentInteractionResultPanel extends StatelessWidget {
  const ChatAgentInteractionResultPanel({
    super.key,
    required this.summary,
    required this.fontScale,
    required this.accentColor,
    this.tone = ChatAgentInteractionResultTone.info,
    this.detailText = '',
    this.referenceLabel = '',
    this.referenceValue = '',
  });

  final String summary;
  final double fontScale;
  final Color accentColor;
  final ChatAgentInteractionResultTone tone;
  final String detailText;
  final String referenceLabel;
  final String referenceValue;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final resolvedColor = _resolveToneColor(theme);
    final bodyStyle = AppTheme.applyTextFont(
      theme.textTheme.bodySmall?.copyWith(
            fontSize: 12 * fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.8),
            height: 1.45,
          ) ??
          TextStyle(
            fontSize: 12 * fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.8),
            height: 1.45,
          ),
    );

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: resolvedColor.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: resolvedColor.withValues(alpha: 0.18)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            summary,
            style: bodyStyle.copyWith(
              color: resolvedColor,
              fontWeight: FontWeight.w600,
            ),
          ),
          if (referenceLabel.isNotEmpty && referenceValue.isNotEmpty) ...[
            const SizedBox(height: 6),
            Text('$referenceLabel: $referenceValue', style: bodyStyle),
          ],
          if (detailText.trim().isNotEmpty) ...[
            const SizedBox(height: 6),
            Text(detailText.trim(), style: bodyStyle),
          ],
        ],
      ),
    );
  }

  Color _resolveToneColor(ThemeData theme) {
    switch (tone) {
      case ChatAgentInteractionResultTone.pending:
      case ChatAgentInteractionResultTone.info:
        return accentColor;
      case ChatAgentInteractionResultTone.success:
        return AppTheme.statusSuccessColor(theme.brightness);
      case ChatAgentInteractionResultTone.warning:
        return AppTheme.statusWarningColor(theme.brightness);
      case ChatAgentInteractionResultTone.error:
        return theme.colorScheme.error;
    }
  }
}

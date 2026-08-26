import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../../app/themes/app_theme.dart';
import '../models/chat_tool_execution_card_data.dart';

class ChatToolExecutionCardView extends StatefulWidget {
  const ChatToolExecutionCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
  });

  final ChatToolExecutionCardData card;
  final bool isMine;
  final double fontScale;

  @override
  State<ChatToolExecutionCardView> createState() =>
      _ChatToolExecutionCardViewState();
}

class _ChatToolExecutionCardViewState extends State<ChatToolExecutionCardView> {
  bool _isExpanded = false;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final viewportWidth = MediaQuery.sizeOf(context).width;
    final accentColor =
        widget.isMine ? theme.colorScheme.primary : theme.colorScheme.secondary;
    final canExpand = widget.card.displayDetailText.isNotEmpty;

    final titleStyle = AppTheme.applyTextFont(
      theme.textTheme.labelMedium?.copyWith(
            fontSize: 11 * widget.fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ) ??
          TextStyle(
            fontSize: 11 * widget.fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ),
    );
    final summaryStyle = AppTheme.applyTextFont(
      theme.textTheme.bodyMedium?.copyWith(
            fontSize: 12.5 * widget.fontScale,
            fontWeight: FontWeight.w600,
            color: theme.colorScheme.onSurface,
            height: 1.35,
          ) ??
          TextStyle(
            fontSize: 12.5 * widget.fontScale,
            fontWeight: FontWeight.w600,
            color: theme.colorScheme.onSurface,
            height: 1.35,
          ),
    );
    final detailStyle = AppTheme.applyTextFont(
      theme.textTheme.bodySmall?.copyWith(
            fontSize: 11 * widget.fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.78),
            height: 1.45,
          ) ??
          TextStyle(
            fontSize: 11 * widget.fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.78),
            height: 1.45,
          ),
    );
    final codeStyle = theme.textTheme.bodyMedium?.copyWith(
          fontSize: 12 * widget.fontScale,
          color: theme.colorScheme.onSurface,
          height: 1.45,
          fontFamily: 'monospace',
          fontFamilyFallback: AppTheme.textFontFallbackOrNull,
        ) ??
        TextStyle(
          fontSize: 12 * widget.fontScale,
          color: theme.colorScheme.onSurface,
          height: 1.45,
          fontFamily: 'monospace',
          fontFamilyFallback: AppTheme.textFontFallbackOrNull,
        );
    final hintStyle = AppTheme.applyTextFont(
      theme.textTheme.labelSmall?.copyWith(
            fontSize: 10.5 * widget.fontScale,
            fontWeight: FontWeight.w600,
            color: accentColor.withValues(alpha: 0.82),
          ) ??
          TextStyle(
            fontSize: 10.5 * widget.fontScale,
            fontWeight: FontWeight.w600,
            color: accentColor.withValues(alpha: 0.82),
          ),
    );

    final summarySection = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Row(
          children: [
            Container(
              width: 30,
              height: 30,
              decoration: BoxDecoration(
                color: accentColor.withValues(alpha: 0.14),
                borderRadius: BorderRadius.circular(9),
              ),
              child: Icon(
                Icons.build_circle_outlined,
                size: 17,
                color: accentColor,
              ),
            ),
            const SizedBox(width: 9),
            Expanded(
              child: Text(
                'chat_message_card_tool_execution_label'.tr,
                style: titleStyle,
              ),
            ),
            if (canExpand)
              Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    _isExpanded
                        ? 'chat_message_card_tool_execution_collapse'.tr
                        : 'chat_message_card_tool_execution_expand'.tr,
                    style: hintStyle,
                  ),
                  const SizedBox(width: 2),
                  Icon(
                    _isExpanded
                        ? Icons.expand_less_rounded
                        : Icons.expand_more_rounded,
                    size: 18,
                    color: accentColor.withValues(alpha: 0.9),
                  ),
                ],
              ),
          ],
        ),
        const SizedBox(height: 8),
        Text(
          widget.card.displaySummaryText,
          maxLines: _isExpanded ? null : 1,
          overflow: _isExpanded ? TextOverflow.visible : TextOverflow.ellipsis,
          style: summaryStyle,
        ),
      ],
    );

    return AnimatedSize(
      duration: const Duration(milliseconds: 160),
      curve: Curves.easeOutCubic,
      child: Container(
        key: const Key('chat_message_card_tool_execution'),
        constraints: BoxConstraints(
          minWidth: 240,
          maxWidth: viewportWidth * 0.8,
        ),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        decoration: BoxDecoration(
          color: accentColor.withValues(alpha: 0.08),
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: accentColor.withValues(alpha: 0.18)),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            if (canExpand)
              Material(
                color: Colors.transparent,
                child: InkWell(
                  key: const Key('chat_message_card_tool_execution_toggle'),
                  borderRadius: BorderRadius.circular(10),
                  onTap: () => setState(() => _isExpanded = !_isExpanded),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 2),
                    child: summarySection,
                  ),
                ),
              )
            else
              summarySection,
            if (canExpand && _isExpanded) ...[
              const SizedBox(height: 10),
              _ToolExecutionDetailBox(
                title: 'chat_message_card_tool_execution_details'.tr,
                body: widget.card.displayDetailText,
                bodyStyle: _shouldUseCodeStyle(widget.card.displayDetailText)
                    ? codeStyle
                    : detailStyle,
              ),
            ],
          ],
        ),
      ),
    );
  }

  bool _shouldUseCodeStyle(String text) {
    final normalized = text.trimLeft();
    return normalized.startsWith('```') || normalized.contains('\n');
  }
}

class _ToolExecutionDetailBox extends StatelessWidget {
  const _ToolExecutionDetailBox({
    required this.title,
    required this.body,
    required this.bodyStyle,
  });

  final String title;
  final String body;
  final TextStyle bodyStyle;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final titleStyle = AppTheme.applyTextFont(
      theme.textTheme.labelSmall?.copyWith(
            fontSize: 10,
            fontWeight: FontWeight.w700,
            letterSpacing: 0.2,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.62),
          ) ??
          TextStyle(
            fontSize: 10,
            fontWeight: FontWeight.w700,
            letterSpacing: 0.2,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.62),
          ),
    );

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface.withValues(alpha: 0.58),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(title, style: titleStyle),
          const SizedBox(height: 6),
          Text(body, style: bodyStyle),
        ],
      ),
    );
  }
}

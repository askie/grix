import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../../app/themes/app_theme.dart';
import '../models/chat_tool_execution_group_card_data.dart';

class ChatToolExecutionGroupCardView extends StatefulWidget {
  const ChatToolExecutionGroupCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
  });

  final ChatToolExecutionGroupCardData card;
  final bool isMine;
  final double fontScale;

  @override
  State<ChatToolExecutionGroupCardView> createState() =>
      _ChatToolExecutionGroupCardViewState();
}

class _ChatToolExecutionGroupCardViewState
    extends State<ChatToolExecutionGroupCardView> {
  bool _isChildrenExpanded = false;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final viewportWidth = MediaQuery.sizeOf(context).width;
    final accentColor = widget.isMine
        ? theme.colorScheme.primary
        : theme.colorScheme.secondary;
    final count = widget.card.count;
    final displayCard = widget.card.displayCard;

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

    final badgeStyle = AppTheme.applyTextFont(
      theme.textTheme.labelSmall?.copyWith(
            fontSize: 10 * widget.fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.88),
          ) ??
          TextStyle(
            fontSize: 10 * widget.fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.88),
          ),
    );

    return AnimatedSize(
      duration: const Duration(milliseconds: 200),
      curve: Curves.easeOutCubic,
      child: Container(
        key: const Key('chat_message_card_tool_execution_group'),
        width: double.infinity,
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
            Material(
              color: Colors.transparent,
              child: InkWell(
                key: const Key('chat_message_card_tool_execution_group_toggle'),
                borderRadius: BorderRadius.circular(10),
                onTap: () =>
                    setState(() => _isChildrenExpanded = !_isChildrenExpanded),
                child: Padding(
                  padding: const EdgeInsets.symmetric(vertical: 2),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
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
                              'chat_message_card_tool_execution_group_label'.tr,
                              style: titleStyle,
                            ),
                          ),
                          _CountBadge(
                            count: count,
                            style: badgeStyle,
                            accentColor: accentColor,
                          ),
                          const SizedBox(width: 8),
                          Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Text(
                                _isChildrenExpanded
                                    ? 'chat_message_card_tool_execution_group_hide_all'
                                          .tr
                                    : 'chat_message_card_tool_execution_group_show_all'
                                          .tr,
                                style: hintStyle,
                              ),
                              const SizedBox(width: 2),
                              Icon(
                                _isChildrenExpanded
                                    ? Icons.expand_less_rounded
                                    : Icons.expand_more_rounded,
                                size: 18,
                                color: accentColor.withValues(alpha: 0.9),
                              ),
                            ],
                          ),
                        ],
                      ),
                      if (!_isChildrenExpanded) ...[
                        const SizedBox(height: 8),
                        Text(
                          displayCard.displaySummaryText,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: summaryStyle,
                        ),
                      ],
                    ],
                  ),
                ),
              ),
            ),
            if (_isChildrenExpanded) ...[
              const SizedBox(height: 8),
              _ChildrenList(
                children: widget.card.children,
                fontScale: widget.fontScale,
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _CountBadge extends StatelessWidget {
  const _CountBadge({
    required this.count,
    required this.style,
    required this.accentColor,
  });

  final int count;
  final TextStyle style;
  final Color accentColor;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: accentColor.withValues(alpha: 0.14),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Text('$count', style: style),
    );
  }
}

class _ChildrenList extends StatelessWidget {
  const _ChildrenList({required this.children, required this.fontScale});

  final List<dynamic> children;
  final double fontScale;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final childStyle = AppTheme.applyTextFont(
      theme.textTheme.bodySmall?.copyWith(
            fontSize: 11 * fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.78),
            height: 1.45,
          ) ??
          TextStyle(
            fontSize: 11 * fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.78),
            height: 1.45,
          ),
    );
    final indexStyle = AppTheme.applyTextFont(
      theme.textTheme.labelSmall?.copyWith(
            fontSize: 10 * fontScale,
            fontWeight: FontWeight.w700,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.45),
          ) ??
          TextStyle(
            fontSize: 10 * fontScale,
            fontWeight: FontWeight.w700,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.45),
          ),
    );

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (var i = 0; i < children.length; i++)
          Padding(
            padding: EdgeInsets.only(
              top: i == 0 ? 0 : 5,
              bottom: i == children.length - 1 ? 0 : 0,
            ),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                SizedBox(
                  width: 30,
                  child: Text('${i + 1}.', style: indexStyle),
                ),
                Expanded(
                  child: Text(
                    children[i].displaySummaryText,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: childStyle,
                  ),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

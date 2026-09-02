import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';

class ChatMarkdownMermaidJourneyView extends StatelessWidget {
  const ChatMarkdownMermaidJourneyView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
  });

  final ChatMermaidJourneyDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;

  static const double _barMaxWidth = 120;
  static const double _barHeight = 20;

  @override
  Widget build(BuildContext context) {
    final surface = _resolveSurfaceColor(backgroundColor);
    final borderColor = _resolveBorderColor(textStyle.color);

    return Container(
      width: double.infinity,
      decoration: BoxDecoration(
        color: surface,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: borderColor.withValues(alpha: 0.18)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (diagram.title.isNotEmpty)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 14, 16, 0),
              child: Text(
                diagram.title,
                style: textStyle.copyWith(
                  fontWeight: FontWeight.w700,
                  fontSize: (textStyle.fontSize ?? 13) + 1,
                ),
              ),
            ),
          for (final section in diagram.sections) ...[
            if (section.title.isNotEmpty)
              Container(
                width: double.infinity,
                padding: const EdgeInsets.symmetric(
                  horizontal: 16,
                  vertical: 8,
                ),
                decoration: BoxDecoration(
                  color: borderColor.withValues(alpha: 0.04),
                  border: Border(
                    top: BorderSide(color: borderColor.withValues(alpha: 0.12)),
                    bottom: BorderSide(
                      color: borderColor.withValues(alpha: 0.12),
                    ),
                  ),
                ),
                child: Text(
                  section.title,
                  style: textStyle.copyWith(
                    fontWeight: FontWeight.w700,
                    fontSize: (textStyle.fontSize ?? 13),
                  ),
                ),
              ),
            for (final task in section.tasks) _buildTaskRow(task, borderColor),
          ],
          const SizedBox(height: 8),
        ],
      ),
    );
  }

  Widget _buildTaskRow(ChatMermaidJourneyTask task, Color borderColor) {
    final barWidth = (task.score / 5.0) * _barMaxWidth;
    final barColor = _scoreColor(task.score);

    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
      child: Row(
        children: [
          Expanded(
            flex: 3,
            child: Text(
              task.label,
              style: textStyle.copyWith(
                fontSize: (textStyle.fontSize ?? 13) - 1,
              ),
            ),
          ),
          const SizedBox(width: 8),
          SizedBox(
            width: _barMaxWidth + 32,
            child: Row(
              children: [
                Container(
                  width: barWidth,
                  height: _barHeight,
                  decoration: BoxDecoration(
                    color: barColor,
                    borderRadius: BorderRadius.circular(4),
                  ),
                ),
                const SizedBox(width: 6),
                Text(
                  '${task.score}',
                  style: textStyle.copyWith(
                    fontWeight: FontWeight.w700,
                    fontSize: (textStyle.fontSize ?? 13) - 1,
                    color: barColor,
                  ),
                ),
              ],
            ),
          ),
          if (task.actors.isNotEmpty) ...[
            const SizedBox(width: 8),
            Flexible(
              child: Text(
                task.actors.join(', '),
                style: textStyle.copyWith(
                  fontSize: (textStyle.fontSize ?? 13) - 2,
                  color: textStyle.color?.withValues(alpha: 0.6),
                ),
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ],
        ],
      ),
    );
  }

  Color _scoreColor(int score) {
    switch (score) {
      case 1:
        return const Color(0xFFDC2626);
      case 2:
        return const Color(0xFFF97316);
      case 3:
        return const Color(0xFFEAB308);
      case 4:
        return const Color(0xFF65A30D);
      case 5:
        return const Color(0xFF16A34A);
      default:
        return const Color(0xFF6B7280);
    }
  }

  Color _resolveSurfaceColor(Color background) {
    final brightness = ThemeData.estimateBrightnessForColor(background);
    return brightness == Brightness.dark
        ? Colors.white.withValues(alpha: 0.04)
        : Colors.white.withValues(alpha: 0.9);
  }

  Color _resolveBorderColor(Color? textColor) =>
      (textColor ?? const Color(0xFF2A2214)).withValues(alpha: 0.86);
}

import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';

/// 时间线图渲染:纵向时间轴,左侧为时间段彩色标签,右侧为该时间段下的事件卡片;
/// 多个分组(section)按调色板着色并显示分组标题。
class ChatMarkdownMermaidTimelineView extends StatelessWidget {
  const ChatMarkdownMermaidTimelineView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
  });

  final ChatMermaidTimelineDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;

  static const double _railWidth = 14;
  static const double _periodLabelWidth = 88;

  @override
  Widget build(BuildContext context) {
    final surface = _resolveSurfaceColor(backgroundColor);
    final borderColor = _resolveBorderColor(textStyle.color);

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.only(bottom: 8),
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
              padding: const EdgeInsets.fromLTRB(16, 14, 16, 4),
              child: Text(
                diagram.title,
                style: textStyle.copyWith(
                  fontWeight: FontWeight.w700,
                  fontSize: (textStyle.fontSize ?? 13) + 1,
                ),
              ),
            ),
          for (var s = 0; s < diagram.sections.length; s += 1)
            _buildSection(diagram.sections[s], s, borderColor),
        ],
      ),
    );
  }

  Widget _buildSection(
    ChatMermaidTimelineSection section,
    int sectionIndex,
    Color borderColor,
  ) {
    final accent = _sectionColor(sectionIndex);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (section.title.isNotEmpty)
          Container(
            width: double.infinity,
            margin: const EdgeInsets.only(top: 8),
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
            color: accent.withValues(alpha: 0.10),
            child: Text(
              section.title,
              style: textStyle.copyWith(
                fontWeight: FontWeight.w700,
                fontSize: (textStyle.fontSize ?? 13),
                color: accent,
              ),
            ),
          ),
        for (var p = 0; p < section.periods.length; p += 1)
          _buildPeriodRow(
            section.periods[p],
            accent,
            isLast: p == section.periods.length - 1,
          ),
      ],
    );
  }

  Widget _buildPeriodRow(
    ChatMermaidTimelinePeriod period,
    Color accent, {
    required bool isLast,
  }) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
      child: IntrinsicHeight(
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // 时间段标签
            SizedBox(
              width: _periodLabelWidth,
              child: Container(
                padding:
                    const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                decoration: BoxDecoration(
                  color: accent.withValues(alpha: 0.14),
                  borderRadius: BorderRadius.circular(8),
                  border: Border.all(color: accent.withValues(alpha: 0.5)),
                ),
                child: Text(
                  period.label,
                  style: textStyle.copyWith(
                    fontWeight: FontWeight.w700,
                    fontSize: (textStyle.fontSize ?? 13) - 1,
                    color: accent,
                  ),
                ),
              ),
            ),
            // 时间轴(圆点 + 连接线)
            SizedBox(
              width: _railWidth,
              child: Column(
                children: [
                  const SizedBox(height: 6),
                  Container(
                    width: 8,
                    height: 8,
                    decoration: BoxDecoration(
                      color: accent,
                      shape: BoxShape.circle,
                    ),
                  ),
                  if (!isLast)
                    Expanded(
                      child: Container(
                        width: 2,
                        color: accent.withValues(alpha: 0.3),
                      ),
                    ),
                ],
              ),
            ),
            const SizedBox(width: 4),
            // 事件卡片
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (period.events.isEmpty)
                    const SizedBox(height: 4)
                  else
                    for (final event in period.events)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 4),
                        child: Container(
                          width: double.infinity,
                          padding: const EdgeInsets.symmetric(
                            horizontal: 10,
                            vertical: 6,
                          ),
                          decoration: BoxDecoration(
                            color: accent.withValues(alpha: 0.06),
                            borderRadius: BorderRadius.circular(8),
                            border: Border.all(
                              color: accent.withValues(alpha: 0.22),
                            ),
                          ),
                          child: Text(
                            event,
                            style: textStyle.copyWith(
                              fontSize: (textStyle.fontSize ?? 13) - 1,
                            ),
                          ),
                        ),
                      ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Color _resolveSurfaceColor(Color background) {
    final brightness = ThemeData.estimateBrightnessForColor(background);
    return brightness == Brightness.dark
        ? Colors.white.withValues(alpha: 0.04)
        : Colors.white.withValues(alpha: 0.9);
  }

  Color _resolveBorderColor(Color? textColor) =>
      (textColor ?? const Color(0xFF2A2214)).withValues(alpha: 0.86);

  static Color _sectionColor(int index) {
    const palette = <Color>[
      Color(0xFF0F766E),
      Color(0xFF1D4ED8),
      Color(0xFFB45309),
      Color(0xFF166534),
      Color(0xFF9A3412),
      Color(0xFF7C3AED),
      Color(0xFFDB2777),
      Color(0xFF0891B2),
    ];
    return palette[index % palette.length];
  }
}

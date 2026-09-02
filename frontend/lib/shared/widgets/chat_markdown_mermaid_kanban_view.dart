import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';

/// 看板图渲染:横向排列各列(阶段),每列含彩色表头与任务卡;
/// 任务卡展示描述及可选的优先级/负责人/工单元数据标记。
class ChatMarkdownMermaidKanbanView extends StatelessWidget {
  const ChatMarkdownMermaidKanbanView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
  });

  final ChatMermaidKanbanDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;

  static const double _columnWidth = 168;

  @override
  Widget build(BuildContext context) {
    final surface = _resolveSurfaceColor(backgroundColor);
    final borderColor = _resolveBorderColor(textStyle.color);

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: surface,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: borderColor.withValues(alpha: 0.18)),
      ),
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            for (var i = 0; i < diagram.columns.length; i += 1)
              Padding(
                padding: EdgeInsets.only(
                  right: i == diagram.columns.length - 1 ? 0 : 10,
                ),
                child: _buildColumn(diagram.columns[i], i, borderColor),
              ),
          ],
        ),
      ),
    );
  }

  Widget _buildColumn(
    ChatMermaidKanbanColumn column,
    int index,
    Color borderColor,
  ) {
    final accent = _columnColor(index);
    return SizedBox(
      width: _columnWidth,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 7),
            decoration: BoxDecoration(
              color: accent.withValues(alpha: 0.14),
              borderRadius: const BorderRadius.vertical(
                top: Radius.circular(8),
              ),
              border: Border.all(color: accent.withValues(alpha: 0.4)),
            ),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    column.title,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: textStyle.copyWith(
                      fontWeight: FontWeight.w700,
                      fontSize: (textStyle.fontSize ?? 13) - 1,
                      color: accent,
                    ),
                  ),
                ),
                Text(
                  '${column.items.length}',
                  style: textStyle.copyWith(
                    fontSize: (textStyle.fontSize ?? 13) - 2,
                    color: accent.withValues(alpha: 0.8),
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ],
            ),
          ),
          Container(
            padding: const EdgeInsets.all(6),
            decoration: BoxDecoration(
              color: accent.withValues(alpha: 0.04),
              borderRadius: const BorderRadius.vertical(
                bottom: Radius.circular(8),
              ),
              border: Border.all(color: borderColor.withValues(alpha: 0.12)),
            ),
            child: Column(
              children: [
                if (column.items.isEmpty)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 6),
                    child: Text(
                      '—',
                      style: textStyle.copyWith(
                        color: textStyle.color?.withValues(alpha: 0.4),
                      ),
                    ),
                  )
                else
                  for (final item in column.items) _buildItemCard(item),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildItemCard(ChatMermaidKanbanItem item) {
    final chips = <Widget>[];
    if (item.priority != null) {
      final color = _priorityColor(item.priority!);
      chips.add(_chip(item.priority!, color, color.withValues(alpha: 0.12)));
    }
    if (item.assigned != null) {
      chips.add(
        _chip(
          '@${item.assigned}',
          const Color(0xFF374151),
          const Color(0x11000000),
        ),
      );
    }
    if (item.ticket != null) {
      chips.add(
        _chip(
          '#${item.ticket}',
          const Color(0xFF1D4ED8),
          const Color(0x111D4ED8),
        ),
      );
    }

    return Container(
      width: double.infinity,
      margin: const EdgeInsets.only(bottom: 6),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 7),
      decoration: BoxDecoration(
        color: backgroundColor.computeLuminance() > 0.5
            ? Colors.white
            : Colors.white.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(6),
        border: Border.all(color: const Color(0x14000000)),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.04),
            blurRadius: 3,
            offset: const Offset(0, 1),
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            item.text,
            style: textStyle.copyWith(fontSize: (textStyle.fontSize ?? 13) - 1),
          ),
          if (chips.isNotEmpty) ...[
            const SizedBox(height: 5),
            Wrap(spacing: 4, runSpacing: 4, children: chips),
          ],
        ],
      ),
    );
  }

  Widget _chip(String text, Color fg, Color bg) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(
        text,
        style: textStyle.copyWith(
          fontSize: (textStyle.fontSize ?? 13) - 4,
          color: fg,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }

  Color _priorityColor(String priority) {
    switch (priority.toLowerCase()) {
      case 'very high':
        return const Color(0xFFB91C1C);
      case 'high':
        return const Color(0xFFEA580C);
      case 'low':
        return const Color(0xFF2563EB);
      case 'very low':
        return const Color(0xFF6B7280);
      default:
        return const Color(0xFF7C3AED);
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

  static Color _columnColor(int index) {
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

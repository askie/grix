import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';

/// 数据包图渲染:按位绘制网格,每行 bitsPerRow 比特;字段按其比特范围占据单元,
/// 跨行时分段绘制,顶部标注起止比特号,单元内居中显示字段名。
class ChatMarkdownMermaidPacketView extends StatelessWidget {
  const ChatMarkdownMermaidPacketView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
  });

  final ChatMermaidPacketDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;

  static const double _numberStrip = 12;
  static const double _cellHeight = 30;
  static const double _rowHeight = _numberStrip + _cellHeight;

  @override
  Widget build(BuildContext context) {
    final surface = _resolveSurfaceColor(backgroundColor);
    final borderColor = _resolveBorderColor(textStyle.color);

    final maxEnd = diagram.fields.fold<int>(0, (m, f) => f.end > m ? f.end : m);
    final totalBits = maxEnd + 1;
    final rows = (totalBits / diagram.bitsPerRow).ceil().clamp(1, 1000);
    final canvasHeight = rows * _rowHeight + 2;

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
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
              padding: const EdgeInsets.only(bottom: 10),
              child: Text(
                diagram.title,
                style: textStyle.copyWith(
                  fontWeight: FontWeight.w700,
                  fontSize: (textStyle.fontSize ?? 13) + 1,
                ),
              ),
            ),
          SizedBox(
            width: double.infinity,
            height: canvasHeight,
            child: CustomPaint(
              painter: _PacketPainter(diagram: diagram, textStyle: textStyle),
            ),
          ),
        ],
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

  static Color fieldColor(int index) {
    const palette = <Color>[
      Color(0xFF1D4ED8),
      Color(0xFF0F766E),
      Color(0xFFB45309),
      Color(0xFF7C3AED),
      Color(0xFF166534),
      Color(0xFFDB2777),
      Color(0xFF9A3412),
      Color(0xFF0891B2),
    ];
    return palette[index % palette.length];
  }
}

class _PacketPainter extends CustomPainter {
  _PacketPainter({required this.diagram, required this.textStyle});

  final ChatMermaidPacketDiagram diagram;
  final TextStyle textStyle;

  @override
  void paint(Canvas canvas, Size size) {
    final bitsPerRow = diagram.bitsPerRow;
    if (bitsPerRow < 1) return;
    final cellW = size.width / bitsPerRow;

    final numberStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 5,
      color: textStyle.color?.withValues(alpha: 0.6),
    );

    for (final field in diagram.fields) {
      final color = ChatMarkdownMermaidPacketView.fieldColor(field.order);
      var bit = field.start;
      while (bit <= field.end) {
        final row = bit ~/ bitsPerRow;
        final rowFirst = row * bitsPerRow;
        final rowLast = rowFirst + bitsPerRow - 1;
        final segEnd = field.end < rowLast ? field.end : rowLast;
        final col0 = bit - rowFirst;
        final col1 = segEnd - rowFirst;
        final x = col0 * cellW;
        final w = (col1 - col0 + 1) * cellW;
        final y =
            row * ChatMarkdownMermaidPacketView._rowHeight +
            ChatMarkdownMermaidPacketView._numberStrip;
        final rect = Rect.fromLTWH(
          x,
          y,
          w,
          ChatMarkdownMermaidPacketView._cellHeight,
        );

        canvas.drawRect(rect, Paint()..color = color.withValues(alpha: 0.18));
        canvas.drawRect(
          rect,
          Paint()
            ..color = color.withValues(alpha: 0.7)
            ..style = PaintingStyle.stroke
            ..strokeWidth = 1,
        );
        // 字段名
        _drawText(
          canvas,
          field.label,
          rect.deflate(3),
          textStyle.copyWith(
            fontSize: (textStyle.fontSize ?? 13) - 3,
            color: color,
            fontWeight: FontWeight.w600,
          ),
          center: true,
        );
        // 起始比特号(段左上)
        _drawText(
          canvas,
          '$bit',
          Rect.fromLTWH(
            x + 1,
            y - ChatMarkdownMermaidPacketView._numberStrip,
            cellW * 2,
            ChatMarkdownMermaidPacketView._numberStrip,
          ),
          numberStyle,
        );
        // 段末比特号(若为字段结尾,右上对齐)
        if (segEnd == field.end && segEnd != bit) {
          _drawText(
            canvas,
            '$segEnd',
            Rect.fromLTWH(
              rect.right - cellW * 2 - 1,
              y - ChatMarkdownMermaidPacketView._numberStrip,
              cellW * 2,
              ChatMarkdownMermaidPacketView._numberStrip,
            ),
            numberStyle,
            alignRight: true,
          );
        }
        bit = segEnd + 1;
      }
    }
  }

  void _drawText(
    Canvas canvas,
    String text,
    Rect rect,
    TextStyle style, {
    bool center = false,
    bool alignRight = false,
  }) {
    if (rect.width <= 0 || rect.height <= 0 || text.isEmpty) return;
    final painter = TextPainter(
      text: TextSpan(text: text, style: style),
      textDirection: TextDirection.ltr,
      maxLines: 1,
      ellipsis: '…',
    )..layout(maxWidth: rect.width);
    final dx = center
        ? rect.left + (rect.width - painter.width) / 2
        : alignRight
        ? rect.right - painter.width
        : rect.left;
    final dy = center
        ? rect.top + (rect.height - painter.height) / 2
        : rect.top;
    canvas.save();
    canvas.clipRect(rect);
    painter.paint(canvas, Offset(dx, dy));
    canvas.restore();
  }

  @override
  bool shouldRepaint(covariant _PacketPainter oldDelegate) {
    return oldDelegate.diagram != diagram || oldDelegate.textStyle != textStyle;
  }
}

import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';

class ChatMarkdownMermaidPieView extends StatelessWidget {
  const ChatMarkdownMermaidPieView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
  });

  final ChatMermaidPieDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;

  static const double _chartSize = 200;
  static const double _legendDotSize = 10;

  @override
  Widget build(BuildContext context) {
    final total = diagram.total;
    if (total <= 0) {
      return const SizedBox.shrink();
    }

    final surface = _resolveSurfaceColor(backgroundColor);
    final borderColor = _resolveBorderColor(textStyle.color);

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: surface,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: borderColor.withValues(alpha: 0.18)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          if (diagram.title.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Text(
                diagram.title,
                style: textStyle.copyWith(
                  fontWeight: FontWeight.w700,
                  fontSize: (textStyle.fontSize ?? 13) + 1,
                ),
              ),
            ),
          SizedBox(
            width: _chartSize,
            height: _chartSize,
            child: CustomPaint(
              painter: _PieChartPainter(slices: diagram.slices, total: total),
            ),
          ),
          const SizedBox(height: 14),
          Wrap(
            spacing: 16,
            runSpacing: 8,
            alignment: WrapAlignment.center,
            children: [
              for (final slice in diagram.slices)
                _buildLegendItem(slice, total),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildLegendItem(ChatMermaidPieSlice slice, double total) {
    final percent = (slice.value / total * 100).toStringAsFixed(1);
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Container(
          width: _legendDotSize,
          height: _legendDotSize,
          decoration: BoxDecoration(
            color: _sliceColor(slice.order),
            shape: BoxShape.circle,
          ),
        ),
        const SizedBox(width: 6),
        Text(
          '${slice.label} ($percent%)',
          style: textStyle.copyWith(fontSize: (textStyle.fontSize ?? 13) - 1),
        ),
      ],
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

  static Color _sliceColor(int index) {
    const palette = <Color>[
      Color(0xFF0F766E),
      Color(0xFF1D4ED8),
      Color(0xFFB45309),
      Color(0xFF166534),
      Color(0xFF9A3412),
      Color(0xFF7C3AED),
      Color(0xFFDB2777),
      Color(0xFF0891B2),
      Color(0xFF65A30D),
      Color(0xFFDC2626),
    ];
    return palette[index % palette.length];
  }
}

class _PieChartPainter extends CustomPainter {
  const _PieChartPainter({required this.slices, required this.total});

  final List<ChatMermaidPieSlice> slices;
  final double total;

  @override
  void paint(Canvas canvas, Size size) {
    final center = Offset(size.width / 2, size.height / 2);
    final radius = math.min(size.width, size.height) / 2;
    var startAngle = -math.pi / 2;

    for (final slice in slices) {
      final sweepAngle = (slice.value / total) * 2 * math.pi;
      final paint = Paint()
        ..color = ChatMarkdownMermaidPieView._sliceColor(slice.order)
        ..style = PaintingStyle.fill;
      canvas.drawArc(
        Rect.fromCircle(center: center, radius: radius),
        startAngle,
        sweepAngle,
        true,
        paint,
      );

      // Separator line
      canvas.drawArc(
        Rect.fromCircle(center: center, radius: radius),
        startAngle,
        sweepAngle,
        true,
        Paint()
          ..color = Colors.white.withValues(alpha: 0.5)
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1.5,
      );

      startAngle += sweepAngle;
    }
  }

  @override
  bool shouldRepaint(covariant _PieChartPainter oldDelegate) {
    return oldDelegate.slices != slices || oldDelegate.total != total;
  }
}

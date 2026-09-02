import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';

/// 雷达图渲染:绘制同心格网(圆形/多边形)、放射轴线与轴标签,
/// 每条曲线以闭合多边形填充+描边表示;可选图例列在下方。
class ChatMarkdownMermaidRadarView extends StatelessWidget {
  const ChatMarkdownMermaidRadarView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
  });

  final ChatMermaidRadarDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;

  static const double _canvasSize = 260;

  @override
  Widget build(BuildContext context) {
    final surface = _resolveSurfaceColor(backgroundColor);
    final borderColor = _resolveBorderColor(textStyle.color);
    final axisLabelStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 3,
      color: textStyle.color?.withValues(alpha: 0.8),
      fontWeight: FontWeight.w600,
    );

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
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
              padding: const EdgeInsets.only(bottom: 8),
              child: Text(
                diagram.title,
                style: textStyle.copyWith(
                  fontWeight: FontWeight.w700,
                  fontSize: (textStyle.fontSize ?? 13) + 1,
                ),
              ),
            ),
          SizedBox(
            width: _canvasSize,
            height: _canvasSize,
            child: CustomPaint(
              painter: _RadarPainter(
                diagram: diagram,
                axisLabelStyle: axisLabelStyle,
                gridColor: borderColor.withValues(alpha: 0.45),
              ),
            ),
          ),
          if (diagram.showLegend && diagram.curves.isNotEmpty) ...[
            const SizedBox(height: 10),
            Wrap(
              spacing: 14,
              runSpacing: 6,
              alignment: WrapAlignment.center,
              children: [
                for (final curve in diagram.curves)
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Container(
                        width: 10,
                        height: 10,
                        decoration: BoxDecoration(
                          color: curveColor(curve.order),
                          borderRadius: BorderRadius.circular(2),
                        ),
                      ),
                      const SizedBox(width: 6),
                      Text(
                        curve.label,
                        style: textStyle.copyWith(
                          fontSize: (textStyle.fontSize ?? 13) - 1,
                        ),
                      ),
                    ],
                  ),
              ],
            ),
          ],
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

  static Color curveColor(int index) {
    const palette = <Color>[
      Color(0xFF2563EB),
      Color(0xFFDC2626),
      Color(0xFF16A34A),
      Color(0xFFD97706),
      Color(0xFF7C3AED),
      Color(0xFF0891B2),
      Color(0xFFDB2777),
      Color(0xFF4D7C0F),
    ];
    return palette[index % palette.length];
  }
}

class _RadarPainter extends CustomPainter {
  _RadarPainter({
    required this.diagram,
    required this.axisLabelStyle,
    required this.gridColor,
  });

  final ChatMermaidRadarDiagram diagram;
  final TextStyle axisLabelStyle;
  final Color gridColor;

  @override
  void paint(Canvas canvas, Size size) {
    final axes = diagram.axes;
    final n = axes.length;
    if (n < 3) {
      return;
    }
    final center = Offset(size.width / 2, size.height / 2);
    final radius = size.width / 2 - 34; // 留出轴标签空间
    if (radius <= 0) {
      return;
    }

    double angleAt(int i) => -math.pi / 2 + i * 2 * math.pi / n;
    Offset pointAt(int i, double r) =>
        center + Offset(math.cos(angleAt(i)) * r, math.sin(angleAt(i)) * r);

    final gridPaint = Paint()
      ..color = gridColor
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1;

    // 同心格网
    for (var t = 1; t <= diagram.ticks; t += 1) {
      final r = radius * t / diagram.ticks;
      if (diagram.graticule == ChatMermaidRadarGraticule.circle) {
        canvas.drawCircle(center, r, gridPaint);
      } else {
        final path = Path();
        for (var i = 0; i < n; i += 1) {
          final p = pointAt(i, r);
          if (i == 0) {
            path.moveTo(p.dx, p.dy);
          } else {
            path.lineTo(p.dx, p.dy);
          }
        }
        path.close();
        canvas.drawPath(path, gridPaint);
      }
    }

    // 放射轴线 + 轴标签
    for (var i = 0; i < n; i += 1) {
      final end = pointAt(i, radius);
      canvas.drawLine(center, end, gridPaint);

      final labelPainter = TextPainter(
        text: TextSpan(text: axes[i].label, style: axisLabelStyle),
        textDirection: TextDirection.ltr,
        maxLines: 1,
        ellipsis: '…',
      )..layout(maxWidth: 64);
      final labelAnchor = pointAt(i, radius + 4);
      final cosA = math.cos(angleAt(i));
      final sinA = math.sin(angleAt(i));
      // 依据方向把标签锚到合适侧
      final dx =
          labelAnchor.dx -
          (cosA < -0.3
              ? labelPainter.width
              : (cosA > 0.3 ? 0 : labelPainter.width / 2));
      final dy =
          labelAnchor.dy -
          (sinA < -0.3
              ? labelPainter.height
              : (sinA > 0.3 ? 0 : labelPainter.height / 2));
      labelPainter.paint(canvas, Offset(dx, dy));
    }

    // 各曲线
    final span = diagram.maxValue - diagram.minValue;
    final denom = span.abs() < 1e-9 ? 1.0 : span;
    for (final curve in diagram.curves) {
      final color = ChatMarkdownMermaidRadarView.curveColor(curve.order);
      final path = Path();
      final points = <Offset>[];
      for (var i = 0; i < n; i += 1) {
        final value = i < curve.values.length
            ? curve.values[i]
            : diagram.minValue;
        final norm = ((value - diagram.minValue) / denom).clamp(0.0, 1.0);
        final p = pointAt(i, radius * norm);
        points.add(p);
        if (i == 0) {
          path.moveTo(p.dx, p.dy);
        } else {
          path.lineTo(p.dx, p.dy);
        }
      }
      path.close();
      canvas.drawPath(
        path,
        Paint()
          ..color = color.withValues(alpha: 0.18)
          ..style = PaintingStyle.fill,
      );
      canvas.drawPath(
        path,
        Paint()
          ..color = color
          ..style = PaintingStyle.stroke
          ..strokeWidth = 2,
      );
      for (final p in points) {
        canvas.drawCircle(p, 2.5, Paint()..color = color);
      }
    }
  }

  @override
  bool shouldRepaint(covariant _RadarPainter oldDelegate) {
    return oldDelegate.diagram != diagram ||
        oldDelegate.axisLabelStyle != axisLabelStyle ||
        oldDelegate.gridColor != gridColor;
  }
}

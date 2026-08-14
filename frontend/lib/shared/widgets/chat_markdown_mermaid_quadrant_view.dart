import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';

/// 象限图渲染:固定尺寸正方形画布,四象限着不同底色,中线十字分隔;
/// 四个象限标题居中显示,数据点按 x/y(0..1)定位并标注;
/// 轴端标签显示在画布外侧(x 轴在下方两端,y 轴在左侧旋转显示)。
class ChatMarkdownMermaidQuadrantView extends StatelessWidget {
  const ChatMarkdownMermaidQuadrantView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
  });

  final ChatMermaidQuadrantDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;

  static const double _chartSize = 280;
  static const double _dotSize = 9;
  static const double _yGutter = 22;

  // 象限底色:q1 右上、q2 左上、q3 左下、q4 右下。
  static const Color _q1 = Color(0xFFE0F2F1);
  static const Color _q2 = Color(0xFFE3F2FD);
  static const Color _q3 = Color(0xFFFFF3E0);
  static const Color _q4 = Color(0xFFF3E5F5);

  @override
  Widget build(BuildContext context) {
    final surface = _resolveSurfaceColor(backgroundColor);
    final borderColor = _resolveBorderColor(textStyle.color);
    final quadrantLabelStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 1,
      fontWeight: FontWeight.w600,
      color: const Color(0xFF374151),
    );
    final axisStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 2,
      color: textStyle.color?.withValues(alpha: 0.7),
      fontWeight: FontWeight.w600,
    );

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
          Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              _buildYAxisCaption(axisStyle),
              _buildChart(quadrantLabelStyle, borderColor),
            ],
          ),
          if (diagram.xAxisLeft.isNotEmpty || diagram.xAxisRight.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(left: _yGutter, top: 6),
              child: SizedBox(
                width: _chartSize,
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  children: [
                    Flexible(
                      child: Text(diagram.xAxisLeft,
                          style: axisStyle, overflow: TextOverflow.ellipsis),
                    ),
                    Flexible(
                      child: Text(diagram.xAxisRight,
                          style: axisStyle,
                          textAlign: TextAlign.right,
                          overflow: TextOverflow.ellipsis),
                    ),
                  ],
                ),
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildYAxisCaption(TextStyle axisStyle) {
    final hasY =
        diagram.yAxisBottom.isNotEmpty || diagram.yAxisTop.isNotEmpty;
    if (!hasY) {
      return const SizedBox(width: _yGutter);
    }
    final caption = diagram.yAxisTop.isNotEmpty && diagram.yAxisBottom.isNotEmpty
        ? '${diagram.yAxisBottom} → ${diagram.yAxisTop}'
        : (diagram.yAxisTop.isNotEmpty
            ? diagram.yAxisTop
            : diagram.yAxisBottom);
    return SizedBox(
      width: _yGutter,
      height: _chartSize,
      child: Center(
        child: RotatedBox(
          quarterTurns: 3,
          child: Text(
            caption,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: axisStyle,
          ),
        ),
      ),
    );
  }

  Widget _buildChart(TextStyle quadrantLabelStyle, Color borderColor) {
    return SizedBox(
      width: _chartSize,
      height: _chartSize,
      child: Stack(
        children: [
          // 背景与中线
          Positioned.fill(
            child: CustomPaint(
              painter: _QuadrantBackgroundPainter(
                lineColor: borderColor.withValues(alpha: 0.35),
              ),
            ),
          ),
          // 象限标题
          _quadrantLabel(diagram.quadrant2, 0, 0, quadrantLabelStyle), // 左上
          _quadrantLabel(
              diagram.quadrant1, _chartSize / 2, 0, quadrantLabelStyle), // 右上
          _quadrantLabel(
              diagram.quadrant3, 0, _chartSize / 2, quadrantLabelStyle), // 左下
          _quadrantLabel(diagram.quadrant4, _chartSize / 2, _chartSize / 2,
              quadrantLabelStyle), // 右下
          // 数据点
          for (final point in diagram.points) ..._buildPoint(point),
        ],
      ),
    );
  }

  Widget _quadrantLabel(String label, double left, double top, TextStyle style) {
    if (label.isEmpty) {
      return const SizedBox.shrink();
    }
    return Positioned(
      left: left,
      top: top + 8,
      width: _chartSize / 2,
      child: Text(
        label,
        textAlign: TextAlign.center,
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
        style: style,
      ),
    );
  }

  List<Widget> _buildPoint(ChatMermaidQuadrantPoint point) {
    final px = point.x * _chartSize;
    final py = (1 - point.y) * _chartSize; // 屏幕坐标 y 向下,需翻转
    final color = _pointColor(point.order);
    return [
      Positioned(
        left: px - _dotSize / 2,
        top: py - _dotSize / 2,
        child: Container(
          width: _dotSize,
          height: _dotSize,
          decoration: BoxDecoration(
            color: color,
            shape: BoxShape.circle,
            border: Border.all(color: Colors.white, width: 1.2),
          ),
        ),
      ),
      Positioned(
        left: px + _dotSize / 2 + 2,
        top: py - 8,
        width: _chartSize / 2,
        child: Text(
          point.label,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: textStyle.copyWith(
            fontSize: (textStyle.fontSize ?? 13) - 3,
            color: color,
            fontWeight: FontWeight.w600,
          ),
        ),
      ),
    ];
  }

  Color _resolveSurfaceColor(Color background) {
    final brightness = ThemeData.estimateBrightnessForColor(background);
    return brightness == Brightness.dark
        ? Colors.white.withValues(alpha: 0.04)
        : Colors.white.withValues(alpha: 0.9);
  }

  Color _resolveBorderColor(Color? textColor) =>
      (textColor ?? const Color(0xFF2A2214)).withValues(alpha: 0.86);

  static Color _pointColor(int index) {
    const palette = <Color>[
      Color(0xFFDC2626),
      Color(0xFF1D4ED8),
      Color(0xFF166534),
      Color(0xFFB45309),
      Color(0xFF7C3AED),
      Color(0xFFDB2777),
      Color(0xFF0891B2),
      Color(0xFF4D7C0F),
    ];
    return palette[index % palette.length];
  }
}

class _QuadrantBackgroundPainter extends CustomPainter {
  const _QuadrantBackgroundPainter({required this.lineColor});

  final Color lineColor;

  @override
  void paint(Canvas canvas, Size size) {
    final halfW = size.width / 2;
    final halfH = size.height / 2;

    void fill(Rect rect, Color color) {
      canvas.drawRect(rect, Paint()..color = color);
    }

    // q2 左上、q1 右上、q3 左下、q4 右下
    fill(Rect.fromLTWH(0, 0, halfW, halfH),
        ChatMarkdownMermaidQuadrantView._q2);
    fill(Rect.fromLTWH(halfW, 0, halfW, halfH),
        ChatMarkdownMermaidQuadrantView._q1);
    fill(Rect.fromLTWH(0, halfH, halfW, halfH),
        ChatMarkdownMermaidQuadrantView._q3);
    fill(Rect.fromLTWH(halfW, halfH, halfW, halfH),
        ChatMarkdownMermaidQuadrantView._q4);

    final linePaint = Paint()
      ..color = lineColor
      ..strokeWidth = 1.2;
    canvas.drawLine(Offset(halfW, 0), Offset(halfW, size.height), linePaint);
    canvas.drawLine(Offset(0, halfH), Offset(size.width, halfH), linePaint);

    final borderPaint = Paint()
      ..color = lineColor
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.2;
    canvas.drawRect(Offset.zero & size, borderPaint);
  }

  @override
  bool shouldRepaint(covariant _QuadrantBackgroundPainter oldDelegate) =>
      oldDelegate.lineColor != lineColor;
}

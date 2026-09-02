import 'package:flutter/material.dart';

class CircularProgressButton extends StatelessWidget {
  const CircularProgressButton({
    super.key,
    required this.centerText,
    required this.percent,
    required this.ringColor,
    required this.size,
    this.strokeWidth = 2.5,
    this.disabled = false,
    this.onTap,
    this.innerPercent,
    this.innerColor,
  });

  final String centerText;
  final double percent;
  final Color ringColor;
  final double size;
  final double strokeWidth;
  final bool disabled;
  final VoidCallback? onTap;

  /// 内圈进度（0~100）。null 表示不绘制内圈。
  final double? innerPercent;

  /// 内圈颜色。innerPercent 非空时生效，默认绿色。
  final Color? innerColor;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final trackColor = ringColor.withValues(alpha: 0.12);
    final fgColor = disabled ? theme.disabledColor : ringColor;
    final textColor = disabled
        ? theme.disabledColor
        : theme.colorScheme.onSurface;
    final innerFg = innerPercent == null
        ? null
        : (disabled
              ? theme.disabledColor
              : (innerColor ?? Colors.green.shade400));
    final innerTrack = innerFg?.withValues(alpha: 0.12);

    return SizedBox(
      width: size,
      height: size,
      child: Material(
        color: Colors.transparent,
        child: InkWell(
          borderRadius: BorderRadius.circular(size / 2),
          onTap: disabled ? null : onTap,
          child: CustomPaint(
            painter: _CircularProgressPainter(
              percent: percent.clamp(0.0, 100.0),
              ringColor: fgColor,
              trackColor: trackColor,
              strokeWidth: strokeWidth,
              innerPercent: innerPercent?.clamp(0.0, 100.0),
              innerRingColor: innerFg,
              innerTrackColor: innerTrack,
            ),
            child: Center(
              child: Text(
                centerText,
                style: TextStyle(
                  fontSize: _resolveFontSize(centerText),
                  fontWeight: FontWeight.w600,
                  color: textColor,
                  height: 1.0,
                ),
                textAlign: TextAlign.center,
                maxLines: 1,
              ),
            ),
          ),
        ),
      ),
    );
  }

  double _resolveFontSize(String text) {
    // CJK characters are wider, use smaller font
    final hasCJK = text.codeUnits.any(
      (c) => (c >= 0x4E00 && c <= 0x9FFF) || (c >= 0x3400 && c <= 0x4DBF),
    );
    if (hasCJK) return (size * 0.28).clamp(9.0, 13.0);
    return (size * 0.32).clamp(9.0, 14.0);
  }
}

class _CircularProgressPainter extends CustomPainter {
  _CircularProgressPainter({
    required this.percent,
    required this.ringColor,
    required this.trackColor,
    required this.strokeWidth,
    this.innerPercent,
    this.innerRingColor,
    this.innerTrackColor,
  });

  final double percent;
  final Color ringColor;
  final Color trackColor;
  final double strokeWidth;
  final double? innerPercent;
  final Color? innerRingColor;
  final Color? innerTrackColor;

  @override
  void paint(Canvas canvas, Size size) {
    final center = Offset(size.width / 2, size.height / 2);
    final radius = (size.shortestSide / 2) - strokeWidth;

    // Track ring
    final trackPaint = Paint()
      ..color = trackColor
      ..style = PaintingStyle.stroke
      ..strokeWidth = strokeWidth
      ..strokeCap = StrokeCap.round;
    canvas.drawCircle(center, radius, trackPaint);

    // Progress arc
    if (percent > 0) {
      final sweepAngle = (percent / 100.0) * 2 * 3.14159265;
      final progressPaint = Paint()
        ..color = ringColor
        ..style = PaintingStyle.stroke
        ..strokeWidth = strokeWidth
        ..strokeCap = StrokeCap.round;
      canvas.drawArc(
        Rect.fromCircle(center: center, radius: radius),
        -3.14159265 / 2, // start from top
        sweepAngle,
        false,
        progressPaint,
      );
    }

    // Inner ring (time progress)
    final inner = innerPercent;
    final innerFg = innerRingColor;
    final innerBg = innerTrackColor;
    if (inner != null && innerFg != null && innerBg != null) {
      final innerStroke = strokeWidth * 0.8;
      // 内圈与外圈中线之间留一条空隙：外圈内缘 = radius - strokeWidth/2；
      // 内圈外缘 = innerRadius + innerStroke/2；空隙取 1.5 像素。
      final innerRadius = radius - strokeWidth / 2 - 1.5 - innerStroke / 2;
      if (innerRadius > 0) {
        final innerTrackPaint = Paint()
          ..color = innerBg
          ..style = PaintingStyle.stroke
          ..strokeWidth = innerStroke
          ..strokeCap = StrokeCap.round;
        canvas.drawCircle(center, innerRadius, innerTrackPaint);

        if (inner > 0) {
          final innerSweep = (inner / 100.0) * 2 * 3.14159265;
          final innerProgressPaint = Paint()
            ..color = innerFg
            ..style = PaintingStyle.stroke
            ..strokeWidth = innerStroke
            ..strokeCap = StrokeCap.round;
          canvas.drawArc(
            Rect.fromCircle(center: center, radius: innerRadius),
            -3.14159265 / 2,
            innerSweep,
            false,
            innerProgressPaint,
          );
        }
      }
    }
  }

  @override
  bool shouldRepaint(_CircularProgressPainter oldDelegate) {
    return oldDelegate.percent != percent ||
        oldDelegate.ringColor != ringColor ||
        oldDelegate.trackColor != trackColor ||
        oldDelegate.strokeWidth != strokeWidth ||
        oldDelegate.innerPercent != innerPercent ||
        oldDelegate.innerRingColor != innerRingColor ||
        oldDelegate.innerTrackColor != innerTrackColor;
  }
}

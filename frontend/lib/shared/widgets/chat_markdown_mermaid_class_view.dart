import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';
import 'chat_markdown_mermaid_zoomable_viewport.dart';

class ChatMarkdownMermaidClassView extends StatelessWidget {
  const ChatMarkdownMermaidClassView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
    this.zoomController,
    this.exportBoundaryKey,
  });

  final ChatMermaidClassDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;
  final ChatMarkdownMermaidZoomController? zoomController;
  final GlobalKey? exportBoundaryKey;

  static const double _classBoxMinWidth = 140;
  static const double _horizontalSpacing = 56;
  static const double _verticalSpacing = 48;
  static const double _memberLineHeight = 22;
  static const double _headerHeight = 36;
  static const double _paddingH = 14;

  @override
  Widget build(BuildContext context) {
    final textDirection = Directionality.of(context);
    final memberStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 2,
      height: 1.3,
    );
    final titleStyle = textStyle.copyWith(
      fontWeight: FontWeight.w700,
      fontSize: (textStyle.fontSize ?? 13) - 1,
    );

    // Measure class box sizes
    final boxes = <String, _ClassBox>{};
    for (final cls in diagram.classes) {
      final titleWidth = _measureText(cls.label, titleStyle, textDirection);
      var maxMemberWidth = 0.0;
      for (final member in cls.members) {
        final w = _measureText(member, memberStyle, textDirection);
        if (w > maxMemberWidth) maxMemberWidth = w;
      }
      final boxWidth = math.max(
        _classBoxMinWidth,
        math.max(titleWidth, maxMemberWidth) + (_paddingH * 2),
      );
      final boxHeight = _headerHeight +
          (cls.members.isEmpty
              ? 0
              : cls.members.length * _memberLineHeight + 12);
      boxes[cls.id] = _ClassBox(width: boxWidth, height: boxHeight);
    }

    // Simple grid layout
    final columns = math.max(1, math.min(diagram.classes.length, 4));
    final positions = <String, Offset>{};
    var canvasWidth = 0.0;
    var canvasHeight = 0.0;

    for (var i = 0; i < diagram.classes.length; i++) {
      final cls = diagram.classes[i];
      final col = i % columns;
      final row = i ~/ columns;
      final box = boxes[cls.id]!;

      // Calculate column x offset
      var xOffset = 0.0;
      for (var c = 0; c < col; c++) {
        final colClasses = diagram.classes
            .where((cl) => diagram.classes.indexOf(cl) % columns == c);
        var maxW = _classBoxMinWidth;
        for (final cc in colClasses) {
          maxW = math.max(maxW, boxes[cc.id]!.width);
        }
        xOffset += maxW + _horizontalSpacing;
      }

      var yOffset = 0.0;
      for (var r = 0; r < row; r++) {
        var maxH = _headerHeight;
        for (var c = 0; c < columns; c++) {
          final idx = r * columns + c;
          if (idx < diagram.classes.length) {
            maxH = math.max(maxH, boxes[diagram.classes[idx].id]!.height);
          }
        }
        yOffset += maxH + _verticalSpacing;
      }

      positions[cls.id] = Offset(xOffset, yOffset);
      canvasWidth = math.max(canvasWidth, xOffset + box.width);
      canvasHeight = math.max(canvasHeight, yOffset + box.height);
    }

    canvasWidth += 24;
    canvasHeight += 24;

    final edgeColor = _resolveEdgeColor(textStyle.color);
    final nodeFill = _resolveNodeFill(backgroundColor);
    final viewportHeight = math.min(canvasHeight, 420.0);

    return ChatMarkdownMermaidZoomableViewport(
      viewportHeight: viewportHeight,
      canvasSize: Size(canvasWidth, canvasHeight),
      zoomController: zoomController,
      exportBoundaryKey: exportBoundaryKey,
      minScale: 0.6,
      maxScale: 2.5,
      showControls: false,
      controlsFillColor:
          ThemeData.estimateBrightnessForColor(backgroundColor) ==
                  Brightness.dark
              ? const Color(0xFF111827).withValues(alpha: 0.92)
              : Colors.white.withValues(alpha: 0.96),
      controlsBorderColor: edgeColor.withValues(alpha: 0.2),
      controlsIconColor: edgeColor,
      child: Stack(
        clipBehavior: Clip.none,
        children: [
          Positioned.fill(
            child: CustomPaint(
              painter: _ClassRelationPainter(
                diagram: diagram,
                positions: positions,
                boxes: boxes,
                color: edgeColor,
              ),
            ),
          ),
          for (final cls in diagram.classes)
            Positioned(
              left: positions[cls.id]!.dx,
              top: positions[cls.id]!.dy,
              child: _ClassBoxWidget(
                classItem: cls,
                width: boxes[cls.id]!.width,
                titleStyle: titleStyle,
                memberStyle: memberStyle,
                fillColor: nodeFill,
                borderColor: edgeColor,
              ),
            ),
        ],
      ),
    );
  }

  double _measureText(String text, TextStyle style, TextDirection direction) {
    final painter = TextPainter(
      text: TextSpan(text: text, style: style),
      textDirection: direction,
      maxLines: 1,
    )..layout();
    return painter.width;
  }

  Color _resolveEdgeColor(Color? textColor) =>
      (textColor ?? const Color(0xFF2A2214)).withValues(alpha: 0.88);

  Color _resolveNodeFill(Color background) {
    final brightness = ThemeData.estimateBrightnessForColor(background);
    return brightness == Brightness.dark
        ? Colors.white.withValues(alpha: 0.08)
        : Colors.white.withValues(alpha: 0.94);
  }
}

class _ClassBox {
  const _ClassBox({required this.width, required this.height});

  final double width;
  final double height;

  Offset center(Offset position) =>
      Offset(position.dx + width / 2, position.dy + height / 2);
}

class _ClassBoxWidget extends StatelessWidget {
  const _ClassBoxWidget({
    required this.classItem,
    required this.width,
    required this.titleStyle,
    required this.memberStyle,
    required this.fillColor,
    required this.borderColor,
  });

  final ChatMermaidClassItem classItem;
  final double width;
  final TextStyle titleStyle;
  final TextStyle memberStyle;
  final Color fillColor;
  final Color borderColor;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: width,
      decoration: BoxDecoration(
        color: fillColor,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: borderColor, width: 1.2),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
            decoration: BoxDecoration(
              border: classItem.members.isNotEmpty
                  ? Border(
                      bottom:
                          BorderSide(color: borderColor.withValues(alpha: 0.3)),
                    )
                  : null,
            ),
            child: Text(
              classItem.label,
              style: titleStyle,
              textAlign: TextAlign.center,
            ),
          ),
          if (classItem.members.isNotEmpty)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  for (final member in classItem.members)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 1),
                      child: Text(member, style: memberStyle),
                    ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class _ClassRelationPainter extends CustomPainter {
  const _ClassRelationPainter({
    required this.diagram,
    required this.positions,
    required this.boxes,
    required this.color,
  });

  final ChatMermaidClassDiagram diagram;
  final Map<String, Offset> positions;
  final Map<String, _ClassBox> boxes;
  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    for (final relation in diagram.relations) {
      final sourcePos = positions[relation.sourceId];
      final targetPos = positions[relation.targetId];
      if (sourcePos == null || targetPos == null) continue;

      final sourceBox = boxes[relation.sourceId]!;
      final targetBox = boxes[relation.targetId]!;
      final from = sourceBox.center(sourcePos);
      final to = targetBox.center(targetPos);

      final isDashed =
          relation.relationType == ChatMermaidClassRelationType.dependency ||
              relation.relationType == ChatMermaidClassRelationType.realization;

      final paint = Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.6
        ..strokeCap = StrokeCap.round;

      final clippedFrom = _clipToBox(from, to, sourcePos, sourceBox);
      final clippedTo = _clipToBox(to, from, targetPos, targetBox);

      if (isDashed) {
        _drawDashedLine(canvas, clippedFrom, clippedTo, paint);
      } else {
        canvas.drawLine(clippedFrom, clippedTo, paint);
      }

      _drawRelationHead(
        canvas,
        clippedFrom,
        clippedTo,
        relation.relationType,
        paint,
      );
    }
  }

  Offset _clipToBox(Offset from, Offset to, Offset boxPos, _ClassBox box) {
    final center = box.center(boxPos);
    final dx = to.dx - from.dx;
    final dy = to.dy - from.dy;
    if (dx == 0 && dy == 0) return center;

    final halfW = box.width / 2;
    final halfH = box.height / 2;

    double? t;
    if (dx != 0) {
      final tx1 = (-halfW - 0) / dx;
      final tx2 = (halfW - 0) / dx;
      for (final candidate in [tx1, tx2]) {
        if (candidate > 0) {
          final y = candidate * dy;
          if (y.abs() <= halfH) {
            t = t == null ? candidate : math.min(t, candidate);
          }
        }
      }
    }
    if (dy != 0) {
      final ty1 = (-halfH - 0) / dy;
      final ty2 = (halfH - 0) / dy;
      for (final candidate in [ty1, ty2]) {
        if (candidate > 0) {
          final x = candidate * dx;
          if (x.abs() <= halfW) {
            t = t == null ? candidate : math.min(t, candidate);
          }
        }
      }
    }
    if (t == null) return center;
    return Offset(center.dx + t * dx, center.dy + t * dy);
  }

  void _drawDashedLine(Canvas canvas, Offset from, Offset to, Paint paint) {
    final direction = to - from;
    final distance = direction.distance;
    if (distance == 0) return;
    final unit = direction / distance;
    const dashLen = 6.0;
    const gapLen = 4.0;
    var drawn = 0.0;
    while (drawn < distance) {
      final start = from + unit * drawn;
      final end = from + unit * math.min(drawn + dashLen, distance);
      canvas.drawLine(start, end, paint);
      drawn += dashLen + gapLen;
    }
  }

  void _drawRelationHead(
    Canvas canvas,
    Offset from,
    Offset to,
    ChatMermaidClassRelationType type,
    Paint paint,
  ) {
    final direction = to - from;
    if (direction.distance == 0) return;
    final unit = direction / direction.distance;
    const arrowLength = 12.0;
    const arrowWidth = 6.0;
    final base = to - (unit * arrowLength);
    final normal = Offset(-unit.dy, unit.dx);

    switch (type) {
      case ChatMermaidClassRelationType.inheritance:
      case ChatMermaidClassRelationType.realization:
        // Hollow triangle
        final path = Path()
          ..moveTo(to.dx, to.dy)
          ..lineTo(
            base.dx + normal.dx * arrowWidth,
            base.dy + normal.dy * arrowWidth,
          )
          ..lineTo(
            base.dx - normal.dx * arrowWidth,
            base.dy - normal.dy * arrowWidth,
          )
          ..close();
        canvas.drawPath(
          path,
          Paint()
            ..color = Colors.white
            ..style = PaintingStyle.fill,
        );
        canvas.drawPath(path, paint);
        break;
      case ChatMermaidClassRelationType.composition:
        // Filled diamond
        final mid = to - (unit * arrowLength / 2);
        final path = Path()
          ..moveTo(to.dx, to.dy)
          ..lineTo(
            mid.dx + normal.dx * arrowWidth,
            mid.dy + normal.dy * arrowWidth,
          )
          ..lineTo(base.dx, base.dy)
          ..lineTo(
            mid.dx - normal.dx * arrowWidth,
            mid.dy - normal.dy * arrowWidth,
          )
          ..close();
        canvas.drawPath(
          path,
          Paint()
            ..color = paint.color
            ..style = PaintingStyle.fill,
        );
        break;
      case ChatMermaidClassRelationType.aggregation:
        // Hollow diamond
        final mid = to - (unit * arrowLength / 2);
        final path = Path()
          ..moveTo(to.dx, to.dy)
          ..lineTo(
            mid.dx + normal.dx * arrowWidth,
            mid.dy + normal.dy * arrowWidth,
          )
          ..lineTo(base.dx, base.dy)
          ..lineTo(
            mid.dx - normal.dx * arrowWidth,
            mid.dy - normal.dy * arrowWidth,
          )
          ..close();
        canvas.drawPath(
          path,
          Paint()
            ..color = Colors.white
            ..style = PaintingStyle.fill,
        );
        canvas.drawPath(path, paint);
        break;
      case ChatMermaidClassRelationType.association:
      case ChatMermaidClassRelationType.dependency:
        // Arrow head
        final path = Path()
          ..moveTo(to.dx, to.dy)
          ..lineTo(
            base.dx + normal.dx * arrowWidth,
            base.dy + normal.dy * arrowWidth,
          )
          ..lineTo(
            base.dx - normal.dx * arrowWidth,
            base.dy - normal.dy * arrowWidth,
          )
          ..close();
        canvas.drawPath(
          path,
          Paint()
            ..color = paint.color
            ..style = PaintingStyle.fill,
        );
        break;
    }
  }

  @override
  bool shouldRepaint(covariant _ClassRelationPainter oldDelegate) {
    return oldDelegate.diagram != diagram || oldDelegate.color != color;
  }
}

import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';
import 'chat_markdown_mermaid_zoomable_viewport.dart';

class ChatMarkdownMermaidErView extends StatelessWidget {
  const ChatMarkdownMermaidErView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
    this.zoomController,
    this.exportBoundaryKey,
  });

  final ChatMermaidErDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;
  final ChatMarkdownMermaidZoomController? zoomController;
  final GlobalKey? exportBoundaryKey;

  static const double _entityMinWidth = 110;
  static const double _entityHeight = 42;
  static const double _horizontalSpacing = 80;
  static const double _verticalSpacing = 56;

  @override
  Widget build(BuildContext context) {
    final textDirection = Directionality.of(context);
    final entityStyle = textStyle.copyWith(
      fontWeight: FontWeight.w700,
      fontSize: (textStyle.fontSize ?? 13) - 1,
    );
    final labelStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 2,
      fontStyle: FontStyle.italic,
    );

    // Measure and layout
    final entityWidths = <String, double>{};
    for (final entity in diagram.entities) {
      final w = _measureText(entity.id, entityStyle, textDirection) + 28;
      entityWidths[entity.id] = math.max(_entityMinWidth, w);
    }

    final columns = math.max(1, math.min(diagram.entities.length, 4));
    final positions = <String, Offset>{};
    var canvasWidth = 0.0;
    var canvasHeight = 0.0;

    for (var i = 0; i < diagram.entities.length; i++) {
      final entity = diagram.entities[i];
      final col = i % columns;
      final row = i ~/ columns;
      final x = col * (_entityMinWidth + _horizontalSpacing);
      final y = row * (_entityHeight + _verticalSpacing);
      positions[entity.id] = Offset(x, y);
      canvasWidth = math.max(
        canvasWidth,
        x + (entityWidths[entity.id] ?? _entityMinWidth),
      );
      canvasHeight = math.max(canvasHeight, y + _entityHeight);
    }

    canvasWidth += 24;
    canvasHeight += 24;

    final edgeColor = _resolveEdgeColor(textStyle.color);
    final nodeFill = _resolveNodeFill(backgroundColor);
    final labelFill = _resolveLabelFill(backgroundColor);
    final viewportHeight = math.min(canvasHeight, 380.0);

    return ChatMarkdownMermaidZoomableViewport(
      viewportHeight: viewportHeight,
      canvasSize: Size(canvasWidth, canvasHeight),
      zoomController: zoomController,
      exportBoundaryKey: exportBoundaryKey,
      minScale: 0.6,
      maxScale: 2.5,
      showControls: false,
      controlsFillColor: labelFill.withValues(alpha: 0.96),
      controlsBorderColor: edgeColor.withValues(alpha: 0.2),
      controlsIconColor: edgeColor,
      child: Stack(
        clipBehavior: Clip.none,
        children: [
          Positioned.fill(
            child: CustomPaint(
              painter: _ErRelationPainter(
                diagram: diagram,
                positions: positions,
                entityWidths: entityWidths,
                entityHeight: _entityHeight,
                color: edgeColor,
                labelStyle: labelStyle,
                labelFill: labelFill,
              ),
            ),
          ),
          for (final entity in diagram.entities)
            Positioned(
              left: positions[entity.id]!.dx,
              top: positions[entity.id]!.dy,
              child: Container(
                width: entityWidths[entity.id],
                height: _entityHeight,
                decoration: BoxDecoration(
                  color: nodeFill,
                  borderRadius: BorderRadius.circular(10),
                  border: Border.all(color: edgeColor, width: 1.2),
                ),
                alignment: Alignment.center,
                child: Text(entity.id, style: entityStyle),
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

  Color _resolveLabelFill(Color background) {
    final brightness = ThemeData.estimateBrightnessForColor(background);
    return brightness == Brightness.dark
        ? const Color(0xFF111827).withValues(alpha: 0.96)
        : Colors.white.withValues(alpha: 0.96);
  }
}

class _ErRelationPainter extends CustomPainter {
  const _ErRelationPainter({
    required this.diagram,
    required this.positions,
    required this.entityWidths,
    required this.entityHeight,
    required this.color,
    required this.labelStyle,
    required this.labelFill,
  });

  final ChatMermaidErDiagram diagram;
  final Map<String, Offset> positions;
  final Map<String, double> entityWidths;
  final double entityHeight;
  final Color color;
  final TextStyle labelStyle;
  final Color labelFill;

  @override
  void paint(Canvas canvas, Size size) {
    for (final relation in diagram.relations) {
      final sourcePos = positions[relation.sourceId];
      final targetPos = positions[relation.targetId];
      if (sourcePos == null || targetPos == null) continue;

      final sourceW = entityWidths[relation.sourceId] ?? 110;
      final targetW = entityWidths[relation.targetId] ?? 110;
      final from = Offset(
        sourcePos.dx + sourceW / 2,
        sourcePos.dy + entityHeight / 2,
      );
      final to = Offset(
        targetPos.dx + targetW / 2,
        targetPos.dy + entityHeight / 2,
      );

      final paint = Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.6
        ..strokeCap = StrokeCap.round;

      canvas.drawLine(from, to, paint);

      // Draw cardinality markers
      _drawCardinalityMarker(
        canvas,
        from,
        to,
        relation.sourceCardinality,
        paint,
      );
      _drawCardinalityMarker(
        canvas,
        to,
        from,
        relation.targetCardinality,
        paint,
      );

      // Draw label at midpoint
      if (relation.label.isNotEmpty) {
        final mid = Offset((from.dx + to.dx) / 2, (from.dy + to.dy) / 2);
        final textPainter = TextPainter(
          text: TextSpan(text: relation.label, style: labelStyle),
          textDirection: TextDirection.ltr,
          maxLines: 1,
        )..layout();
        final labelRect = Rect.fromCenter(
          center: mid - const Offset(0, 12),
          width: textPainter.width + 10,
          height: textPainter.height + 6,
        );
        canvas.drawRRect(
          RRect.fromRectAndRadius(labelRect, const Radius.circular(4)),
          Paint()
            ..color = labelFill
            ..style = PaintingStyle.fill,
        );
        textPainter.paint(
          canvas,
          Offset(labelRect.left + 5, labelRect.top + 3),
        );
      }
    }
  }

  void _drawCardinalityMarker(
    Canvas canvas,
    Offset from,
    Offset to,
    ChatMermaidErCardinality cardinality,
    Paint paint,
  ) {
    final direction = to - from;
    if (direction.distance == 0) return;
    final unit = direction / direction.distance;
    final markerPos = from + unit * 20;
    final normal = Offset(-unit.dy, unit.dx);

    switch (cardinality) {
      case ChatMermaidErCardinality.exactlyOne:
        // Two vertical lines (||)
        final p1 = markerPos - unit * 3;
        final p2 = markerPos + unit * 3;
        canvas.drawLine(p1 + normal * 6, p1 - normal * 6, paint);
        canvas.drawLine(p2 + normal * 6, p2 - normal * 6, paint);
        break;
      case ChatMermaidErCardinality.zeroOrOne:
        // Circle + line
        canvas.drawCircle(
          markerPos - unit * 4,
          4,
          Paint()
            ..color = color
            ..style = PaintingStyle.stroke
            ..strokeWidth = 1.4,
        );
        final linePos = markerPos + unit * 4;
        canvas.drawLine(linePos + normal * 6, linePos - normal * 6, paint);
        break;
      case ChatMermaidErCardinality.oneOrMore:
        // Line + crow's foot
        final linePos = markerPos - unit * 4;
        canvas.drawLine(linePos + normal * 6, linePos - normal * 6, paint);
        final tipPos = markerPos + unit * 6;
        canvas.drawLine(tipPos, markerPos + normal * 7, paint);
        canvas.drawLine(tipPos, markerPos - normal * 7, paint);
        break;
      case ChatMermaidErCardinality.zeroOrMore:
        // Circle + crow's foot
        canvas.drawCircle(
          markerPos - unit * 5,
          4,
          Paint()
            ..color = color
            ..style = PaintingStyle.stroke
            ..strokeWidth = 1.4,
        );
        final tipPos = markerPos + unit * 5;
        canvas.drawLine(tipPos, markerPos + normal * 7, paint);
        canvas.drawLine(tipPos, markerPos - normal * 7, paint);
        break;
    }
  }

  @override
  bool shouldRepaint(covariant _ErRelationPainter oldDelegate) {
    return oldDelegate.diagram != diagram || oldDelegate.color != color;
  }
}

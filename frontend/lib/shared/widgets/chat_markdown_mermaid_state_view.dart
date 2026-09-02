import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';
import '../mermaid/chat_mermaid_node_style_tokens.dart';
import '../mermaid/chat_mermaid_state_layout.dart';
import 'chat_markdown_mermaid_pill_label.dart';
import 'chat_markdown_mermaid_zoomable_viewport.dart';

class ChatMarkdownMermaidStateView extends StatelessWidget {
  const ChatMarkdownMermaidStateView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
    this.zoomController,
    this.exportBoundaryKey,
  });

  final ChatMermaidStateDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;
  final ChatMarkdownMermaidZoomController? zoomController;
  final GlobalKey? exportBoundaryKey;

  static const ChatMermaidStateLayoutEngine _layoutEngine =
      ChatMermaidStateLayoutEngine();

  @override
  Widget build(BuildContext context) {
    final textDirection = Directionality.of(context);
    final labelStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 1,
      fontWeight: FontWeight.w600,
    );
    final stateStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 2,
      fontWeight: FontWeight.w700,
      height: 1.18,
    );
    final layout = _layoutEngine.layout(
      diagram: diagram,
      stateStyle: stateStyle,
      labelStyle: labelStyle,
      textDirection: textDirection,
    );

    final background = backgroundColor;
    final edgeColor = _resolveEdgeColor(textStyle.color);
    final nodeFill = _resolveNodeFill(background);
    final labelFill = _resolveLabelFill(background);
    final viewportHeight = math
        .max(1, math.min(layout.canvasSize.height, 360))
        .toDouble();

    return ChatMarkdownMermaidZoomableViewport(
      viewportHeight: viewportHeight,
      canvasSize: layout.canvasSize,
      zoomController: zoomController,
      exportBoundaryKey: exportBoundaryKey,
      minScale: 0.8,
      maxScale: 2.2,
      showControls: false,
      controlsFillColor: labelFill.withValues(alpha: 0.96),
      controlsBorderColor: edgeColor.withValues(alpha: 0.2),
      controlsIconColor: edgeColor,
      child: Stack(
        clipBehavior: Clip.none,
        children: [
          Positioned.fill(
            child: CustomPaint(
              painter: _ChatMermaidStateTransitionPainter(
                layout: layout,
                color: edgeColor,
              ),
            ),
          ),
          for (final node in diagram.nodes)
            Positioned.fromRect(
              rect: layout.nodeRects[node.id]!,
              child: _ChatMermaidStateNodeCard(
                node: node,
                textStyle: stateStyle,
                fillColor: nodeFill,
                borderColor: edgeColor,
              ),
            ),
          for (final transition in layout.transitions)
            if (transition.transition.label != null &&
                transition.transition.label!.isNotEmpty &&
                transition.labelSize != Size.zero)
              Positioned(
                left:
                    transition.labelAnchor.dx -
                    (transition.labelSize.width / 2),
                top:
                    transition.labelAnchor.dy -
                    (transition.labelSize.height / 2),
                child: ChatMarkdownMermaidPillLabel(
                  text: transition.transition.label!,
                  style: labelStyle,
                  fillColor: labelFill,
                  borderColor: edgeColor.withValues(alpha: 0.32),
                  minWidth: transition.labelSize.width,
                  maxWidth: transition.labelSize.width,
                ),
              ),
        ],
      ),
    );
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

class _ChatMermaidStateNodeCard extends StatelessWidget {
  const _ChatMermaidStateNodeCard({
    required this.node,
    required this.textStyle,
    required this.fillColor,
    required this.borderColor,
  });

  final ChatMermaidStateNode node;
  final TextStyle textStyle;
  final Color fillColor;
  final Color borderColor;

  @override
  Widget build(BuildContext context) {
    if (node.kind == ChatMermaidStateNodeKind.regular) {
      return CustomPaint(
        painter: _ChatMermaidStateNodePainter(
          kind: node.kind,
          fillColor: fillColor,
          borderColor: borderColor,
        ),
        child: Padding(
          padding: ChatMermaidNodeStyleTokens.stateNodePadding,
          child: Center(
            child: Text(
              node.label,
              style: textStyle,
              textAlign: TextAlign.center,
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ),
      );
    }

    return CustomPaint(
      painter: _ChatMermaidStateNodePainter(
        kind: node.kind,
        fillColor: fillColor,
        borderColor: borderColor,
      ),
    );
  }
}

class _ChatMermaidStateNodePainter extends CustomPainter {
  const _ChatMermaidStateNodePainter({
    required this.kind,
    required this.fillColor,
    required this.borderColor,
  });

  final ChatMermaidStateNodeKind kind;
  final Color fillColor;
  final Color borderColor;

  @override
  void paint(Canvas canvas, Size size) {
    switch (kind) {
      case ChatMermaidStateNodeKind.regular:
        final rrect = RRect.fromRectAndRadius(
          Offset.zero & size,
          const Radius.circular(14),
        );
        canvas.drawRRect(
          rrect,
          Paint()
            ..color = fillColor
            ..style = PaintingStyle.fill,
        );
        canvas.drawRRect(
          rrect,
          Paint()
            ..color = borderColor
            ..style = PaintingStyle.stroke
            ..strokeWidth = 1.35,
        );
        break;
      case ChatMermaidStateNodeKind.start:
        canvas.drawOval(
          Offset.zero & size,
          Paint()
            ..color = borderColor
            ..style = PaintingStyle.fill,
        );
        break;
      case ChatMermaidStateNodeKind.end:
        final outerRect = Offset.zero & size;
        final innerRect = outerRect.deflate(4);
        final strokePaint = Paint()
          ..color = borderColor
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1.6;
        canvas.drawOval(outerRect, strokePaint);
        canvas.drawOval(
          innerRect,
          Paint()
            ..color = borderColor
            ..style = PaintingStyle.fill,
        );
        break;
    }
  }

  @override
  bool shouldRepaint(covariant _ChatMermaidStateNodePainter oldDelegate) {
    return oldDelegate.kind != kind ||
        oldDelegate.fillColor != fillColor ||
        oldDelegate.borderColor != borderColor;
  }
}

class _ChatMermaidStateTransitionPainter extends CustomPainter {
  const _ChatMermaidStateTransitionPainter({
    required this.layout,
    required this.color,
  });

  final ChatMermaidStateLayout layout;
  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    for (final transition in layout.transitions) {
      if (transition.points.length < 2) {
        continue;
      }
      final paint = Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.8
        ..strokeCap = StrokeCap.round
        ..strokeJoin = StrokeJoin.round;
      final path = Path()
        ..moveTo(transition.points.first.dx, transition.points.first.dy);
      for (final point in transition.points.skip(1)) {
        path.lineTo(point.dx, point.dy);
      }
      canvas.drawPath(path, paint);
      _drawArrowHead(canvas, transition.points, paint);
    }
  }

  void _drawArrowHead(Canvas canvas, List<Offset> points, Paint paint) {
    final tip = points.last;
    final tail = points[points.length - 2];
    final direction = tip - tail;
    if (direction.distance == 0) {
      return;
    }

    final unit = direction / direction.distance;
    const arrowLength = 10.0;
    const arrowWidth = 5.0;
    final base = tip - (unit * arrowLength);
    final normal = Offset(-unit.dy, unit.dx);
    final arrowPath = Path()
      ..moveTo(tip.dx, tip.dy)
      ..lineTo(
        base.dx + (normal.dx * arrowWidth),
        base.dy + (normal.dy * arrowWidth),
      )
      ..lineTo(
        base.dx - (normal.dx * arrowWidth),
        base.dy - (normal.dy * arrowWidth),
      )
      ..close();
    canvas.drawPath(
      arrowPath,
      Paint()
        ..color = paint.color
        ..style = PaintingStyle.fill,
    );
  }

  @override
  bool shouldRepaint(covariant _ChatMermaidStateTransitionPainter oldDelegate) {
    return oldDelegate.layout != layout || oldDelegate.color != color;
  }
}

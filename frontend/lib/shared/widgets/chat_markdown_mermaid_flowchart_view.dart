import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_flowchart_layout.dart';
import '../mermaid/chat_mermaid_model.dart';
import '../mermaid/chat_mermaid_node_style_tokens.dart';
import 'chat_markdown_mermaid_pill_label.dart';
import 'chat_markdown_mermaid_zoomable_viewport.dart';

class ChatMarkdownMermaidFlowchartView extends StatelessWidget {
  const ChatMarkdownMermaidFlowchartView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
    this.zoomController,
    this.exportBoundaryKey,
  });

  final ChatMermaidFlowchart diagram;
  final TextStyle textStyle;
  final Color backgroundColor;
  final ChatMarkdownMermaidZoomController? zoomController;
  final GlobalKey? exportBoundaryKey;

  static const ChatMermaidFlowchartLayoutEngine _layoutEngine =
      ChatMermaidFlowchartLayoutEngine();
  static const double _minScale = 0.8;
  static const double _maxScale = 2.2;
  static const double _zoomStep = 0.2;

  @override
  Widget build(BuildContext context) {
    final textDirection = Directionality.of(context);
    final labelStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 1,
      fontWeight: FontWeight.w600,
    );
    // 流程图节点用系统 UI 字体，不继承代码块的 monospace 字体。
    // 关键：渲染端 Text 的实际字体 = DefaultTextStyle.of(context) 合并自身样式
    // （fontFamily 未指定时由 DefaultTextStyle 注入，如 Roboto / 系统 UI 字体），
    // 而布局引擎用裸 TextPainter 测量时 fontFamily=null 走的是引擎默认字体——
    // 两者字形宽度不同会导致渲染换行比测量多一行、超出固定高度节点框被遮挡。
    // 因此测量必须用「渲染实际生效的同一套字体」：先取 DefaultTextStyle 再合并
    // 节点自有的字号/字重/行高，测量与渲染口径一致，杜绝多行溢出遮挡。
    final nodeStyle = DefaultTextStyle.of(context).style.merge(
      TextStyle(
        color: textStyle.color,
        fontSize: (textStyle.fontSize ?? 13) - 2,
        fontWeight: FontWeight.w600,
        height: 1.2,
      ),
    );
    final layout = _layoutEngine.layout(
      diagram: diagram,
      textStyle: nodeStyle,
      labelStyle: labelStyle,
      textDirection: textDirection,
    );

    final background = backgroundColor;
    final edgeColor = _resolveEdgeColor(textStyle.color);
    final nodeFill = _resolveNodeFill(background);
    final labelFill = _resolveLabelFill(background);
    final subgraphFill = _resolveSubgraphFill(background);
    final viewportHeight = math
        .max(1, math.min(layout.canvasSize.height, 360))
        .toDouble();

    return ChatMarkdownMermaidZoomableViewport(
      viewportHeight: viewportHeight,
      canvasSize: layout.canvasSize,
      zoomController: zoomController,
      minScale: _minScale,
      maxScale: _maxScale,
      zoomStep: _zoomStep,
      showControls: false,
      controlsFillColor: labelFill.withValues(alpha: 0.96),
      controlsBorderColor: edgeColor.withValues(alpha: 0.2),
      controlsIconColor: edgeColor,
      child: RepaintBoundary(
        key: exportBoundaryKey,
        child: SizedBox(
          width: layout.canvasSize.width,
          height: layout.canvasSize.height,
          child: Stack(
            clipBehavior: Clip.none,
            children: [
              for (final subgraph in layout.subgraphRects)
                Positioned.fromRect(
                  rect: subgraph.rect,
                  child: _ChatMermaidSubgraphCard(
                    fillColor: subgraphFill,
                    borderColor: edgeColor.withValues(alpha: 0.28),
                  ),
                ),
              Positioned.fill(
                child: CustomPaint(
                  painter: _ChatMermaidEdgePainter(
                    layout: layout,
                    color: edgeColor,
                  ),
                ),
              ),
              for (final node in diagram.nodes)
                _buildNode(
                  node: node,
                  rect: layout.nodeRects[node.id]!,
                  nodeTextStyle: nodeStyle,
                  defaultFillColor: nodeFill,
                  defaultBorderColor: edgeColor,
                ),
              for (final routedEdge in layout.edges)
                if (routedEdge.edge.label != null &&
                    routedEdge.edge.label!.isNotEmpty &&
                    routedEdge.labelSize != Size.zero)
                  Positioned(
                    left:
                        routedEdge.labelAnchor.dx -
                        (routedEdge.labelSize.width / 2),
                    top:
                        routedEdge.labelAnchor.dy -
                        (routedEdge.labelSize.height / 2),
                    child: ChatMarkdownMermaidPillLabel(
                      text: routedEdge.edge.label!,
                      style: labelStyle,
                      fillColor: labelFill,
                      borderColor: edgeColor.withValues(alpha: 0.32),
                      minWidth: routedEdge.labelSize.width,
                      maxWidth: routedEdge.labelSize.width,
                    ),
                  ),
              for (final subgraph in layout.subgraphRects)
                Positioned.fromRect(
                  rect: subgraph.labelRect,
                  child: ChatMarkdownMermaidPillLabel(
                    text: subgraph.subgraph.label,
                    style: labelStyle,
                    fillColor: labelFill,
                    borderColor: edgeColor.withValues(alpha: 0.32),
                    horizontalPadding: 5,
                    verticalPadding: 2,
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildNode({
    required ChatMermaidNode node,
    required Rect rect,
    required TextStyle nodeTextStyle,
    required Color defaultFillColor,
    required Color defaultBorderColor,
  }) {
    final fillColor = node.fillColor != null
        ? Color(node.fillColor!)
        : defaultFillColor;
    final borderColor = node.strokeColor != null
        ? Color(node.strokeColor!)
        : defaultBorderColor;
    final resolvedTextStyle = node.textColor != null
        ? nodeTextStyle.copyWith(color: Color(node.textColor!))
        : nodeTextStyle;
    return Positioned.fromRect(
      rect: rect,
      child: _ChatMermaidNodeCard(
        label: node.label,
        shape: node.shape,
        // 与布局测量完全相同的已解析样式（含 DefaultTextStyle 注入的字体），
        // 保证渲染换行与测量一致，不会溢出节点框。
        textStyle: resolvedTextStyle,
        fillColor: fillColor,
        borderColor: borderColor,
      ),
    );
  }

  Color _resolveEdgeColor(Color? textColor) =>
      (textColor ?? const Color(0xFF2A2214)).withValues(alpha: 0.88);

  Color _resolveNodeFill(Color background) {
    final brightness = ThemeData.estimateBrightnessForColor(background);
    return brightness == Brightness.dark
        ? const Color(0xFF1F2937)
        : Colors.white;
  }

  Color _resolveLabelFill(Color background) {
    final brightness = ThemeData.estimateBrightnessForColor(background);
    return brightness == Brightness.dark
        ? const Color(0xFF111827).withValues(alpha: 0.96)
        : Colors.white.withValues(alpha: 0.96);
  }

  Color _resolveSubgraphFill(Color background) {
    final brightness = ThemeData.estimateBrightnessForColor(background);
    return brightness == Brightness.dark
        ? const Color(0xFF111827)
        : const Color(0xFFF7F4EC);
  }
}

class _ChatMermaidNodeCard extends StatelessWidget {
  const _ChatMermaidNodeCard({
    required this.label,
    required this.shape,
    required this.textStyle,
    required this.fillColor,
    required this.borderColor,
  });

  final String label;
  final ChatMermaidNodeShape shape;
  final TextStyle textStyle;
  final Color fillColor;
  final Color borderColor;

  @override
  Widget build(BuildContext context) {
    final contentPadding = ChatMermaidNodeStyleTokens.flowchartPaddingForShape(
      shape,
    );
    return CustomPaint(
      painter: _ChatMermaidNodePainter(
        shape: shape,
        fillColor: fillColor,
        borderColor: borderColor,
      ),
      child: Padding(
        padding: contentPadding,
        child: Center(
          child: Text(
            label,
            style: textStyle,
            textAlign: TextAlign.center,
            // 布局引擎用未缩放的 TextPainter 测量节点高度；渲染端必须同样关闭
            // 系统字体缩放，否则放大字号会多挤出一行、超出固定高度的节点框，
            // 末行文字被框底切掉（遮挡）。画布本身可捏合放大，无需系统缩放。
            textScaler: TextScaler.noScaling,
          ),
        ),
      ),
    );
  }
}

class _ChatMermaidSubgraphCard extends StatelessWidget {
  const _ChatMermaidSubgraphCard({
    required this.fillColor,
    required this.borderColor,
  });

  final Color fillColor;
  final Color borderColor;

  @override
  Widget build(BuildContext context) {
    return CustomPaint(
      painter: _ChatMermaidSubgraphPainter(
        fillColor: fillColor,
        borderColor: borderColor,
      ),
    );
  }
}

class _ChatMermaidNodePainter extends CustomPainter {
  const _ChatMermaidNodePainter({
    required this.shape,
    required this.fillColor,
    required this.borderColor,
  });

  final ChatMermaidNodeShape shape;
  final Color fillColor;
  final Color borderColor;

  @override
  void paint(Canvas canvas, Size size) {
    final path = _buildPath(size);
    final fillPaint = Paint()
      ..color = fillColor
      ..style = PaintingStyle.fill;
    final borderPaint = Paint()
      ..color = borderColor
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.35;

    canvas.drawPath(path, fillPaint);
    canvas.drawPath(path, borderPaint);
  }

  Path _buildPath(Size size) {
    switch (shape) {
      case ChatMermaidNodeShape.rectangle:
        return Path()..addRRect(
          RRect.fromRectAndRadius(
            Offset.zero & size,
            const Radius.circular(10),
          ),
        );
      case ChatMermaidNodeShape.rounded:
        return Path()..addRRect(
          RRect.fromRectAndRadius(
            Offset.zero & size,
            const Radius.circular(999),
          ),
        );
      case ChatMermaidNodeShape.diamond:
        return Path()
          ..moveTo(size.width / 2, 0)
          ..lineTo(size.width, size.height / 2)
          ..lineTo(size.width / 2, size.height)
          ..lineTo(0, size.height / 2)
          ..close();
      case ChatMermaidNodeShape.circle:
        return Path()..addOval(Offset.zero & size);
      case ChatMermaidNodeShape.stadium:
        // Pill shape: rounded ends, straight sides
        final radius = size.height / 2;
        return Path()..addRRect(
          RRect.fromRectAndRadius(Offset.zero & size, Radius.circular(radius)),
        );
      case ChatMermaidNodeShape.subroutine:
        // Rectangle with vertical lines inside at edges
        const inset = 6.0;
        return Path()
          ..moveTo(inset, 0)
          ..lineTo(size.width - inset, 0)
          ..moveTo(inset, size.height)
          ..lineTo(size.width - inset, size.height)
          ..addRRect(
            RRect.fromRectAndRadius(
              Offset.zero & size,
              const Radius.circular(8),
            ),
          )
          ..moveTo(inset, 0)
          ..lineTo(inset, size.height)
          ..moveTo(size.width - inset, 0)
          ..lineTo(size.width - inset, size.height);
      case ChatMermaidNodeShape.cylindrical:
        // Cylinder: ellipse top, rectangle body, ellipse bottom
        final ellipseHeight = size.height * 0.2;
        return Path()
          ..moveTo(0, ellipseHeight)
          ..lineTo(0, size.height - ellipseHeight)
          ..quadraticBezierTo(0, size.height, size.width / 2, size.height)
          ..quadraticBezierTo(
            size.width,
            size.height,
            size.width,
            size.height - ellipseHeight,
          )
          ..lineTo(size.width, ellipseHeight)
          ..quadraticBezierTo(size.width, 0, size.width / 2, 0)
          ..quadraticBezierTo(0, 0, 0, ellipseHeight)
          ..close();
      case ChatMermaidNodeShape.hexagon:
        // Six-sided polygon
        final inset = size.width * 0.15;
        return Path()
          ..moveTo(inset, 0)
          ..lineTo(size.width - inset, 0)
          ..lineTo(size.width, size.height / 2)
          ..lineTo(size.width - inset, size.height)
          ..lineTo(inset, size.height)
          ..lineTo(0, size.height / 2)
          ..close();
    }
  }

  @override
  bool shouldRepaint(covariant _ChatMermaidNodePainter oldDelegate) {
    return oldDelegate.shape != shape ||
        oldDelegate.fillColor != fillColor ||
        oldDelegate.borderColor != borderColor;
  }
}

class _ChatMermaidSubgraphPainter extends CustomPainter {
  const _ChatMermaidSubgraphPainter({
    required this.fillColor,
    required this.borderColor,
  });

  final Color fillColor;
  final Color borderColor;

  @override
  void paint(Canvas canvas, Size size) {
    final rrect = RRect.fromRectAndRadius(
      Offset.zero & size,
      const Radius.circular(16),
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
        ..strokeWidth = 1.1,
    );
  }

  @override
  bool shouldRepaint(covariant _ChatMermaidSubgraphPainter oldDelegate) {
    return oldDelegate.fillColor != fillColor ||
        oldDelegate.borderColor != borderColor;
  }
}

class _ChatMermaidEdgePainter extends CustomPainter {
  const _ChatMermaidEdgePainter({required this.layout, required this.color});

  final ChatMermaidFlowchartLayout layout;
  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    for (final routedEdge in layout.edges) {
      if (routedEdge.points.length < 2) {
        continue;
      }
      final paint = Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = _strokeWidth(routedEdge.edge.style)
        ..strokeCap = StrokeCap.round
        ..strokeJoin = StrokeJoin.round;
      final path = _buildRoundedPolyline(routedEdge.points);
      _drawPath(canvas, path, paint, routedEdge.edge.style);
      if (_hasEndpoint(routedEdge.edge.style)) {
        _drawArrowHead(canvas, routedEdge.points, paint, routedEdge.edge.style);
      }
    }
  }

  void _drawPath(
    Canvas canvas,
    Path path,
    Paint paint,
    ChatMermaidEdgeStyle style,
  ) {
    if (style == ChatMermaidEdgeStyle.dashedArrow) {
      _drawDashedPath(canvas, path, paint);
      return;
    }
    canvas.drawPath(path, paint);
  }

  /// 折线拐角用圆弧过渡，观感接近 mermaid 的平滑曲线；首尾段保持直线，
  /// 箭头方向才准确。
  Path _buildRoundedPolyline(List<Offset> points) {
    const radius = 10.0;
    final path = Path()..moveTo(points.first.dx, points.first.dy);
    for (var i = 1; i + 1 < points.length; i++) {
      final prev = points[i - 1];
      final corner = points[i];
      final next = points[i + 1];
      final inVec = corner - prev;
      final outVec = next - corner;
      final inLen = inVec.distance;
      final outLen = outVec.distance;
      if (inLen == 0 || outLen == 0) {
        continue;
      }
      final r = math.min(radius, math.min(inLen, outLen) / 2);
      final entry = corner - inVec / inLen * r;
      final exit = corner + outVec / outLen * r;
      path
        ..lineTo(entry.dx, entry.dy)
        ..quadraticBezierTo(corner.dx, corner.dy, exit.dx, exit.dy);
    }
    path.lineTo(points.last.dx, points.last.dy);
    return path;
  }

  void _drawDashedPath(Canvas canvas, Path path, Paint paint) {
    for (final metric in path.computeMetrics()) {
      var distance = 0.0;
      while (distance < metric.length) {
        final dashLength = math.min(8, metric.length - distance);
        final segment = metric.extractPath(distance, distance + dashLength);
        canvas.drawPath(segment, paint);
        distance += 14;
      }
    }
  }

  void _drawArrowHead(
    Canvas canvas,
    List<Offset> points,
    Paint paint,
    ChatMermaidEdgeStyle style,
  ) {
    final tip = points.last;
    final tail = points[points.length - 2];
    final direction = tip - tail;
    if (direction.distance == 0) {
      return;
    }

    final unit = direction / direction.distance;
    final arrowLength = math.max(8.0, paint.strokeWidth * 5.5);
    final arrowWidth = math.max(5.0, paint.strokeWidth * 2.8);
    final base = tip - (unit * arrowLength);
    final normal = Offset(-unit.dy, unit.dx);

    // Draw arrow or circle, or cross based on style
    if (style == ChatMermaidEdgeStyle.circle) {
      // Draw circle end
      canvas.drawCircle(
        Offset(base.dx + arrowWidth / 2, base.dy),
        arrowWidth / 2,
        Paint()
          ..color = paint.color
          ..style = PaintingStyle.fill,
      );
    } else if (style == ChatMermaidEdgeStyle.cross) {
      // Draw X cross end
      final halfSize = arrowWidth / 2;
      final crossPath = Path()
        ..moveTo(base.dx, base.dy - halfSize)
        ..lineTo(base.dx + halfSize, base.dy + halfSize)
        ..moveTo(base.dx, base.dy)
        ..lineTo(base.dx + halfSize, base.dy - halfSize);
      canvas.drawPath(
        crossPath,
        Paint()
          ..color = paint.color
          ..style = PaintingStyle.stroke
          ..strokeWidth = paint.strokeWidth,
      );
    } else {
      // Draw standard arrow head
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
  }

  bool _hasEndpoint(ChatMermaidEdgeStyle style) =>
      style != ChatMermaidEdgeStyle.solidLine;

  double _strokeWidth(ChatMermaidEdgeStyle style) {
    switch (style) {
      case ChatMermaidEdgeStyle.solidArrow:
      case ChatMermaidEdgeStyle.solidLine:
      case ChatMermaidEdgeStyle.circle:
      case ChatMermaidEdgeStyle.cross:
        return 1.8;
      case ChatMermaidEdgeStyle.dashedArrow:
        return 1.8;
      case ChatMermaidEdgeStyle.thickArrow:
        return 2.6;
    }
  }

  @override
  bool shouldRepaint(covariant _ChatMermaidEdgePainter oldDelegate) {
    return oldDelegate.layout != layout || oldDelegate.color != color;
  }
}

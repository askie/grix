import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';
import 'chat_markdown_mermaid_zoomable_viewport.dart';

class ChatMarkdownMermaidMindmapView extends StatelessWidget {
  const ChatMarkdownMermaidMindmapView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
    this.zoomController,
    this.exportBoundaryKey,
  });

  final ChatMermaidMindmapDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;
  final ChatMarkdownMermaidZoomController? zoomController;
  final GlobalKey? exportBoundaryKey;

  static const double _nodeHorizontalPadding = 14;
  static const double _nodeVerticalPadding = 8;
  static const double _horizontalSpacing = 32;
  static const double _verticalSpacing = 12;

  @override
  Widget build(BuildContext context) {
    final textDirection = Directionality.of(context);
    final nodeStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 1,
      fontWeight: FontWeight.w600,
    );

    final layout = _layoutTree(diagram.root, nodeStyle, textDirection, 0);
    final canvasWidth = layout.totalWidth + 48;
    final canvasHeight = layout.totalHeight + 48;
    final viewportHeight = math.min(canvasHeight, 420.0);

    final edgeColor = _resolveEdgeColor(textStyle.color);

    return ChatMarkdownMermaidZoomableViewport(
      viewportHeight: viewportHeight,
      canvasSize: Size(canvasWidth, canvasHeight),
      zoomController: zoomController,
      minScale: 0.5,
      maxScale: 2.5,
      showControls: false,
      controlsFillColor:
          ThemeData.estimateBrightnessForColor(backgroundColor) ==
              Brightness.dark
          ? const Color(0xFF111827).withValues(alpha: 0.92)
          : Colors.white.withValues(alpha: 0.96),
      controlsBorderColor: edgeColor.withValues(alpha: 0.2),
      controlsIconColor: (textStyle.color ?? const Color(0xFF2A2214))
          .withValues(alpha: 0.88),
      child: RepaintBoundary(
        key: exportBoundaryKey,
        child: CustomPaint(
          painter: _MindmapPainter(
            layout: layout,
            offset: const Offset(24, 24),
            nodeStyle: nodeStyle,
            edgeColor: edgeColor,
            backgroundColor: backgroundColor,
            depthColors: _depthColors,
          ),
          size: Size(canvasWidth, canvasHeight),
        ),
      ),
    );
  }

  _MindmapNodeLayout _layoutTree(
    ChatMermaidMindmapNode node,
    TextStyle style,
    TextDirection textDirection,
    int depth,
  ) {
    final depthStyle = depth == 0
        ? style.copyWith(
            fontWeight: FontWeight.w700,
            fontSize: (style.fontSize ?? 12) + 1,
          )
        : style;

    final textPainter = TextPainter(
      text: TextSpan(text: node.label, style: depthStyle),
      textDirection: textDirection,
      maxLines: 1,
    )..layout();

    final nodeWidth = textPainter.width + (_nodeHorizontalPadding * 2);
    final nodeHeight = textPainter.height + (_nodeVerticalPadding * 2);

    if (node.children.isEmpty) {
      return _MindmapNodeLayout(
        node: node,
        depth: depth,
        nodeWidth: nodeWidth,
        nodeHeight: nodeHeight,
        totalWidth: nodeWidth,
        totalHeight: nodeHeight,
        children: const [],
        childrenYOffsets: const [],
      );
    }

    final childLayouts = node.children
        .map((c) => _layoutTree(c, style, textDirection, depth + 1))
        .toList(growable: false);

    var childrenTotalHeight = 0.0;
    for (var i = 0; i < childLayouts.length; i++) {
      childrenTotalHeight += childLayouts[i].totalHeight;
      if (i < childLayouts.length - 1) {
        childrenTotalHeight += _verticalSpacing;
      }
    }

    var maxChildWidth = 0.0;
    for (final child in childLayouts) {
      maxChildWidth = math.max(maxChildWidth, child.totalWidth);
    }

    final totalHeight = math.max(nodeHeight, childrenTotalHeight);
    final totalWidth = nodeWidth + _horizontalSpacing + maxChildWidth;

    final childOffsets = <double>[];
    var yOffset = (totalHeight - childrenTotalHeight) / 2;
    for (final child in childLayouts) {
      childOffsets.add(yOffset);
      yOffset += child.totalHeight + _verticalSpacing;
    }

    return _MindmapNodeLayout(
      node: node,
      depth: depth,
      nodeWidth: nodeWidth,
      nodeHeight: nodeHeight,
      totalWidth: totalWidth,
      totalHeight: totalHeight,
      children: childLayouts,
      childrenYOffsets: childOffsets,
    );
  }

  Color _resolveEdgeColor(Color? textColor) =>
      (textColor ?? const Color(0xFF2A2214)).withValues(alpha: 0.6);

  static const List<Color> _depthColors = [
    Color(0xFF0F766E),
    Color(0xFF1D4ED8),
    Color(0xFFB45309),
    Color(0xFF7C3AED),
    Color(0xFF166534),
    Color(0xFFDB2777),
    Color(0xFF9A3412),
    Color(0xFF0891B2),
  ];
}

class _MindmapNodeLayout {
  const _MindmapNodeLayout({
    required this.node,
    required this.depth,
    required this.nodeWidth,
    required this.nodeHeight,
    required this.totalWidth,
    required this.totalHeight,
    required this.children,
    required this.childrenYOffsets,
  });

  final ChatMermaidMindmapNode node;
  final int depth;
  final double nodeWidth;
  final double nodeHeight;
  final double totalWidth;
  final double totalHeight;
  final List<_MindmapNodeLayout> children;
  final List<double> childrenYOffsets;
}

class _MindmapPainter extends CustomPainter {
  const _MindmapPainter({
    required this.layout,
    required this.offset,
    required this.nodeStyle,
    required this.edgeColor,
    required this.backgroundColor,
    required this.depthColors,
  });

  final _MindmapNodeLayout layout;
  final Offset offset;
  final TextStyle nodeStyle;
  final Color edgeColor;
  final Color backgroundColor;
  final List<Color> depthColors;

  @override
  void paint(Canvas canvas, Size size) {
    _paintNode(canvas, layout, offset);
  }

  void _paintNode(Canvas canvas, _MindmapNodeLayout layout, Offset origin) {
    final nodeCenterY =
        origin.dy + (layout.totalHeight - layout.nodeHeight) / 2;
    final nodeRect = Rect.fromLTWH(
      origin.dx,
      nodeCenterY,
      layout.nodeWidth,
      layout.nodeHeight,
    );

    // Draw node background
    final depthColor = depthColors[layout.depth % depthColors.length];
    final isDark =
        ThemeData.estimateBrightnessForColor(backgroundColor) ==
        Brightness.dark;
    final fillColor = isDark
        ? depthColor.withValues(alpha: 0.25)
        : depthColor.withValues(alpha: 0.1);
    final borderColor = depthColor.withValues(alpha: isDark ? 0.6 : 0.4);

    final rrect = RRect.fromRectAndRadius(
      nodeRect,
      Radius.circular(
        layout.node.shape == ChatMermaidNodeShape.circle ? 999 : 10,
      ),
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
        ..strokeWidth = 1.3,
    );

    // Draw text
    final depthTextStyle = layout.depth == 0
        ? nodeStyle.copyWith(
            fontWeight: FontWeight.w700,
            fontSize: (nodeStyle.fontSize ?? 12) + 1,
            color: isDark ? Colors.white : depthColor,
          )
        : nodeStyle.copyWith(
            color: isDark ? Colors.white.withValues(alpha: 0.9) : depthColor,
          );
    final textPainter = TextPainter(
      text: TextSpan(text: layout.node.label, style: depthTextStyle),
      textDirection: TextDirection.ltr,
      maxLines: 1,
    )..layout();
    textPainter.paint(
      canvas,
      Offset(
        nodeRect.left + ChatMarkdownMermaidMindmapView._nodeHorizontalPadding,
        nodeRect.top + ChatMarkdownMermaidMindmapView._nodeVerticalPadding,
      ),
    );

    // Draw children
    final childX =
        origin.dx +
        layout.nodeWidth +
        ChatMarkdownMermaidMindmapView._horizontalSpacing;
    final nodeRightCenter = Offset(
      nodeRect.right,
      nodeRect.top + layout.nodeHeight / 2,
    );

    for (var i = 0; i < layout.children.length; i++) {
      final child = layout.children[i];
      final childOrigin = Offset(
        childX,
        origin.dy + layout.childrenYOffsets[i],
      );
      final childNodeCenterY =
          childOrigin.dy + (child.totalHeight - child.nodeHeight) / 2;
      final childLeftCenter = Offset(
        childOrigin.dx,
        childNodeCenterY + child.nodeHeight / 2,
      );

      // Draw curved connection
      final path = Path()
        ..moveTo(nodeRightCenter.dx, nodeRightCenter.dy)
        ..cubicTo(
          nodeRightCenter.dx +
              ChatMarkdownMermaidMindmapView._horizontalSpacing * 0.5,
          nodeRightCenter.dy,
          childLeftCenter.dx -
              ChatMarkdownMermaidMindmapView._horizontalSpacing * 0.5,
          childLeftCenter.dy,
          childLeftCenter.dx,
          childLeftCenter.dy,
        );
      canvas.drawPath(
        path,
        Paint()
          ..color = edgeColor
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1.5
          ..strokeCap = StrokeCap.round,
      );

      _paintNode(canvas, child, childOrigin);
    }
  }

  @override
  bool shouldRepaint(covariant _MindmapPainter oldDelegate) {
    return oldDelegate.layout != layout || oldDelegate.edgeColor != edgeColor;
  }
}

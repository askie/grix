import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';

/// 块图渲染:按 columns 列做网格布局(支持列跨度、space 占位、复合块嵌套子网格),
/// 用对应形状绘制每个块,并在有连接的块之间绘制箭头连线。
class ChatMarkdownMermaidBlockView extends StatelessWidget {
  const ChatMarkdownMermaidBlockView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
  });

  final ChatMermaidBlockDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;

  static const double _maxWidth = 360;
  static const double _minWidth = 220;

  @override
  Widget build(BuildContext context) {
    final surface = _resolveSurfaceColor(backgroundColor);
    final borderColor = _resolveBorderColor(textStyle.color);

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: surface,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: borderColor.withValues(alpha: 0.18)),
      ),
      child: LayoutBuilder(
        builder: (context, constraints) {
          final available = constraints.maxWidth.isFinite
              ? constraints.maxWidth
              : _maxWidth;
          final width = available.clamp(_minWidth, _maxWidth).toDouble();
          final layout = _BlockLayoutEngine.compute(diagram, width);
          return SizedBox(
            width: width,
            height: layout.height,
            child: CustomPaint(
              painter: _BlockPainter(
                diagram: diagram,
                layout: layout,
                textStyle: textStyle,
              ),
            ),
          );
        },
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

  static const Color accent = Color(0xFF1D4ED8);
}

class _BlockBox {
  const _BlockBox(this.item, this.rect, this.depth);
  final ChatMermaidBlockItem item;
  final Rect rect;
  final int depth;
}

class _BlockLayoutResult {
  const _BlockLayoutResult(this.boxes, this.idRects, this.height);
  final List<_BlockBox> boxes;
  final Map<String, Rect> idRects;
  final double height;
}

class _BlockLayoutEngine {
  static const double _gap = 6;
  static const double _leafHeight = 42;
  static const double _headerHeight = 20;
  static const double _pad = 6;

  static _BlockLayoutResult compute(
    ChatMermaidBlockDiagram diagram,
    double width,
  ) {
    final boxes = <_BlockBox>[];
    final idRects = <String, Rect>{};
    final height = _layoutLevel(
      diagram.items,
      diagram.columns,
      0,
      0,
      width,
      0,
      boxes,
      idRects,
    );
    return _BlockLayoutResult(boxes, idRects, math.max(height, _leafHeight));
  }

  static double _layoutLevel(
    List<ChatMermaidBlockItem> items,
    int columns,
    double originX,
    double originY,
    double availWidth,
    int depth,
    List<_BlockBox> boxes,
    Map<String, Rect> idRects,
  ) {
    final cols = columns < 1 ? 1 : columns;
    final colW = availWidth / cols;
    var col = 0;
    var rowTop = originY;
    var rowMaxH = 0.0;

    for (final item in items) {
      var span = item.width < 1 ? 1 : item.width;
      if (span > cols) span = cols;
      if (col + span > cols && col > 0) {
        rowTop += rowMaxH + _gap;
        col = 0;
        rowMaxH = 0;
      }
      final x = originX + col * colW;
      final w = span * colW - _gap;
      double itemH;
      if (item.isComposite) {
        final innerX = x + _gap / 2 + _pad;
        final innerY = rowTop + _headerHeight;
        final innerW = math.max(w - _pad * 2, 10.0);
        final childH = _layoutLevel(
          item.children,
          item.compositeColumns,
          innerX,
          innerY,
          innerW,
          depth + 1,
          boxes,
          idRects,
        );
        itemH = _headerHeight + childH + _pad;
      } else {
        itemH = _leafHeight;
      }
      final rect = Rect.fromLTWH(x + _gap / 2, rowTop, w, itemH);
      boxes.add(_BlockBox(item, rect, depth));
      if (!item.isSpace) {
        idRects[item.id] = rect;
      }
      col += span;
      if (itemH > rowMaxH) rowMaxH = itemH;
    }
    return (rowTop - originY) + rowMaxH;
  }
}

class _BlockPainter extends CustomPainter {
  _BlockPainter({
    required this.diagram,
    required this.layout,
    required this.textStyle,
  });

  final ChatMermaidBlockDiagram diagram;
  final _BlockLayoutResult layout;
  final TextStyle textStyle;

  @override
  void paint(Canvas canvas, Size size) {
    // 先画容器(depth 小)再画子块,保证子块在上层。
    final boxes = [...layout.boxes]..sort((a, b) => a.depth.compareTo(b.depth));
    for (final box in boxes) {
      if (box.item.isSpace) continue;
      if (box.item.isComposite) {
        _drawComposite(canvas, box);
      } else {
        _drawLeaf(canvas, box);
      }
    }
    _drawEdges(canvas);
  }

  void _drawComposite(Canvas canvas, _BlockBox box) {
    const accent = ChatMarkdownMermaidBlockView.accent;
    final rrect =
        RRect.fromRectAndRadius(box.rect, const Radius.circular(8));
    canvas.drawRRect(rrect, Paint()..color = accent.withValues(alpha: 0.05));
    canvas.drawRRect(
      rrect,
      Paint()
        ..color = accent.withValues(alpha: 0.5)
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.2,
    );
    if (box.item.label.isNotEmpty) {
      _drawText(
        canvas,
        box.item.label,
        Rect.fromLTWH(box.rect.left + 6, box.rect.top + 2,
            box.rect.width - 12, 16),
        textStyle.copyWith(
          fontSize: (textStyle.fontSize ?? 13) - 3,
          fontWeight: FontWeight.w700,
          color: accent,
        ),
        TextAlign.left,
      );
    }
  }

  void _drawLeaf(Canvas canvas, _BlockBox box) {
    const accent = ChatMarkdownMermaidBlockView.accent;
    final rect = box.rect.deflate(2);
    final fill = Paint()..color = accent.withValues(alpha: 0.12);
    final stroke = Paint()
      ..color = accent.withValues(alpha: 0.75)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.2;
    final path = _shapePath(box.item.shape, rect);
    canvas.drawPath(path, fill);
    canvas.drawPath(path, stroke);
    _drawText(
      canvas,
      box.item.label,
      rect.deflate(4),
      textStyle.copyWith(fontSize: (textStyle.fontSize ?? 13) - 2),
      TextAlign.center,
      maxLines: 2,
      center: true,
    );
  }

  Path _shapePath(ChatMermaidNodeShape shape, Rect r) {
    final path = Path();
    switch (shape) {
      case ChatMermaidNodeShape.circle:
        path.addOval(Rect.fromCircle(
          center: r.center,
          radius: math.min(r.width, r.height) / 2,
        ));
        break;
      case ChatMermaidNodeShape.stadium:
        path.addRRect(RRect.fromRectAndRadius(
            r, Radius.circular(r.height / 2)));
        break;
      case ChatMermaidNodeShape.rounded:
        path.addRRect(RRect.fromRectAndRadius(r, const Radius.circular(10)));
        break;
      case ChatMermaidNodeShape.diamond:
        path
          ..moveTo(r.center.dx, r.top)
          ..lineTo(r.right, r.center.dy)
          ..lineTo(r.center.dx, r.bottom)
          ..lineTo(r.left, r.center.dy)
          ..close();
        break;
      case ChatMermaidNodeShape.hexagon:
        final inset = r.width * 0.16;
        path
          ..moveTo(r.left + inset, r.top)
          ..lineTo(r.right - inset, r.top)
          ..lineTo(r.right, r.center.dy)
          ..lineTo(r.right - inset, r.bottom)
          ..lineTo(r.left + inset, r.bottom)
          ..lineTo(r.left, r.center.dy)
          ..close();
        break;
      case ChatMermaidNodeShape.cylindrical:
        final ry = math.min(r.height * 0.16, 8.0);
        path
          ..moveTo(r.left, r.top + ry)
          ..arcToPoint(Offset(r.right, r.top + ry),
              radius: Radius.elliptical(r.width / 2, ry), clockwise: true)
          ..lineTo(r.right, r.bottom - ry)
          ..arcToPoint(Offset(r.left, r.bottom - ry),
              radius: Radius.elliptical(r.width / 2, ry), clockwise: true)
          ..close();
        break;
      case ChatMermaidNodeShape.subroutine:
        path.addRect(r);
        break;
      case ChatMermaidNodeShape.rectangle:
        path.addRRect(RRect.fromRectAndRadius(r, const Radius.circular(4)));
        break;
    }
    return path;
  }

  void _drawEdges(Canvas canvas) {
    const accent = ChatMarkdownMermaidBlockView.accent;
    final linePaint = Paint()
      ..color = accent.withValues(alpha: 0.8)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.4;
    for (final edge in diagram.edges) {
      final from = layout.idRects[edge.sourceId];
      final to = layout.idRects[edge.targetId];
      if (from == null || to == null) continue;
      final start = _edgePoint(from, to.center);
      final end = _edgePoint(to, from.center);
      canvas.drawLine(start, end, linePaint);
      _drawArrowHead(canvas, start, end, accent);
      if (edge.label != null) {
        final mid = Offset((start.dx + end.dx) / 2, (start.dy + end.dy) / 2);
        _drawText(
          canvas,
          edge.label!,
          Rect.fromCenter(center: mid, width: 80, height: 14),
          textStyle.copyWith(
            fontSize: (textStyle.fontSize ?? 13) - 4,
            color: accent,
            backgroundColor: Colors.white.withValues(alpha: 0.85),
          ),
          TextAlign.center,
          center: true,
        );
      }
    }
  }

  /// 从矩形中心朝 target 方向、落到矩形边界的交点。
  Offset _edgePoint(Rect rect, Offset target) {
    final c = rect.center;
    final dx = target.dx - c.dx;
    final dy = target.dy - c.dy;
    if (dx == 0 && dy == 0) return c;
    final hw = rect.width / 2;
    final hh = rect.height / 2;
    final scale = 1 /
        math.max(
          (dx.abs() / hw).clamp(1e-6, double.infinity),
          (dy.abs() / hh).clamp(1e-6, double.infinity),
        );
    return Offset(c.dx + dx * scale, c.dy + dy * scale);
  }

  void _drawArrowHead(Canvas canvas, Offset start, Offset end, Color color) {
    final angle = math.atan2(end.dy - start.dy, end.dx - start.dx);
    const size = 6.0;
    final p1 = Offset(
      end.dx - size * math.cos(angle - math.pi / 6),
      end.dy - size * math.sin(angle - math.pi / 6),
    );
    final p2 = Offset(
      end.dx - size * math.cos(angle + math.pi / 6),
      end.dy - size * math.sin(angle + math.pi / 6),
    );
    final path = Path()
      ..moveTo(end.dx, end.dy)
      ..lineTo(p1.dx, p1.dy)
      ..lineTo(p2.dx, p2.dy)
      ..close();
    canvas.drawPath(path, Paint()..color = color);
  }

  void _drawText(
    Canvas canvas,
    String text,
    Rect rect,
    TextStyle style,
    TextAlign align, {
    int maxLines = 1,
    bool center = false,
  }) {
    if (rect.width <= 0 || rect.height <= 0 || text.isEmpty) return;
    final painter = TextPainter(
      text: TextSpan(text: text, style: style),
      textDirection: TextDirection.ltr,
      textAlign: align,
      maxLines: maxLines,
      ellipsis: '…',
    )..layout(maxWidth: rect.width);
    final dx = align == TextAlign.center
        ? rect.left + (rect.width - painter.width) / 2
        : rect.left;
    final dy = center
        ? rect.top + (rect.height - painter.height) / 2
        : rect.top;
    canvas.save();
    canvas.clipRect(rect);
    painter.paint(canvas, Offset(dx, dy < rect.top ? rect.top : dy));
    canvas.restore();
  }

  @override
  bool shouldRepaint(covariant _BlockPainter oldDelegate) {
    return oldDelegate.diagram != diagram ||
        oldDelegate.layout != layout ||
        oldDelegate.textStyle != textStyle;
  }
}

import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';

/// 矩形树图渲染:用 squarified 算法把各节点按取值面积平铺为嵌套矩形;
/// 分组节点保留顶部标题条并递归布局其子节点,叶节点显示标签与取值。
class ChatMarkdownMermaidTreemapView extends StatelessWidget {
  const ChatMarkdownMermaidTreemapView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
  });

  final ChatMermaidTreemapDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;

  static const double _canvasWidth = 320;
  static const double _canvasHeight = 220;

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
      child: Center(
        child: SizedBox(
          width: _canvasWidth,
          height: _canvasHeight,
          child: CustomPaint(
            painter: _TreemapPainter(diagram: diagram, textStyle: textStyle),
          ),
        ),
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

  static Color sectionColor(int index) {
    const palette = <Color>[
      Color(0xFF0F766E),
      Color(0xFF1D4ED8),
      Color(0xFFB45309),
      Color(0xFF166534),
      Color(0xFF9A3412),
      Color(0xFF7C3AED),
      Color(0xFFDB2777),
      Color(0xFF0891B2),
    ];
    return palette[index % palette.length];
  }
}

class _TmItem {
  const _TmItem(this.node, this.color);
  final ChatMermaidTreemapNode node;
  final Color color;
}

class _TreemapPainter extends CustomPainter {
  _TreemapPainter({required this.diagram, required this.textStyle});

  final ChatMermaidTreemapDiagram diagram;
  final TextStyle textStyle;

  static const double _headerHeight = 16;
  static const double _pad = 2;

  @override
  void paint(Canvas canvas, Size size) {
    final roots = <_TmItem>[];
    for (var i = 0; i < diagram.roots.length; i += 1) {
      roots.add(
          _TmItem(diagram.roots[i], ChatMarkdownMermaidTreemapView.sectionColor(i)));
    }
    _layoutInto(canvas, roots, Offset.zero & size, 0);
  }

  void _layoutInto(Canvas canvas, List<_TmItem> items, Rect rect, int depth) {
    final positive = items.where((it) => it.node.value > 0).toList()
      ..sort((a, b) => b.node.value.compareTo(a.node.value));
    if (positive.isEmpty || rect.width <= 1 || rect.height <= 1) {
      return;
    }
    _squarify(positive, rect, (item, r) {
      _drawNode(canvas, item, r, depth);
      if (!item.node.isLeaf &&
          r.width > 28 &&
          r.height > _headerHeight + 14) {
        final inner = Rect.fromLTRB(
          r.left + _pad,
          r.top + _headerHeight,
          r.right - _pad,
          r.bottom - _pad,
        );
        final childItems =
            item.node.children.map((c) => _TmItem(c, item.color)).toList();
        _layoutInto(canvas, childItems, inner, depth + 1);
      }
    });
  }

  void _drawNode(Canvas canvas, _TmItem item, Rect r, int depth) {
    if (r.width <= 0 || r.height <= 0) {
      return;
    }
    final color = item.color;
    if (item.node.isLeaf) {
      canvas.drawRect(r, Paint()..color = color.withValues(alpha: 0.55));
      canvas.drawRect(
        r,
        Paint()
          ..color = Colors.white.withValues(alpha: 0.8)
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1,
      );
      if (r.width > 30 && r.height > 18) {
        _drawLeafLabel(canvas, item.node, r);
      }
    } else {
      canvas.drawRect(r, Paint()..color = color.withValues(alpha: 0.08));
      canvas.drawRect(
        r,
        Paint()
          ..color = color.withValues(alpha: 0.55)
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1,
      );
      if (r.width > 24 && r.height > _headerHeight) {
        _drawText(
          canvas,
          item.node.label,
          Rect.fromLTWH(r.left + 4, r.top + 1, r.width - 8, _headerHeight - 1),
          textStyle.copyWith(
            fontSize: (textStyle.fontSize ?? 13) - 3,
            fontWeight: FontWeight.w700,
            color: color,
          ),
          TextAlign.left,
        );
      }
    }
  }

  void _drawLeafLabel(Canvas canvas, ChatMermaidTreemapNode node, Rect r) {
    final valueText = _formatValue(node.value);
    final label = '${node.label}\n$valueText';
    _drawText(
      canvas,
      label,
      r.deflate(3),
      textStyle.copyWith(
        fontSize: (textStyle.fontSize ?? 13) - 3,
        color: Colors.white,
        fontWeight: FontWeight.w600,
        height: 1.15,
      ),
      TextAlign.center,
      maxLines: 3,
    );
  }

  void _drawText(
    Canvas canvas,
    String text,
    Rect rect,
    TextStyle style,
    TextAlign align, {
    int maxLines = 1,
  }) {
    if (rect.width <= 0 || rect.height <= 0) {
      return;
    }
    final painter = TextPainter(
      text: TextSpan(text: text, style: style),
      textDirection: TextDirection.ltr,
      textAlign: align,
      maxLines: maxLines,
      ellipsis: '…',
    )..layout(maxWidth: rect.width);
    final dy = align == TextAlign.center
        ? rect.top + (rect.height - painter.height) / 2
        : rect.top;
    final clampedDy = dy < rect.top ? rect.top : dy;
    canvas.save();
    canvas.clipRect(rect);
    final dx = align == TextAlign.center
        ? rect.left + (rect.width - painter.width) / 2
        : rect.left;
    painter.paint(canvas, Offset(dx, clampedDy));
    canvas.restore();
  }

  String _formatValue(double value) {
    if (value == value.roundToDouble()) {
      return value.toInt().toString();
    }
    return value.toString();
  }

  // squarified treemap:把 items(按取值降序)平铺到 rect。
  void _squarify(
    List<_TmItem> items,
    Rect rect,
    void Function(_TmItem item, Rect r) place,
  ) {
    final totalValue =
        items.fold<double>(0, (sum, it) => sum + it.node.value);
    if (totalValue <= 0) {
      return;
    }
    final scale = (rect.width * rect.height) / totalValue;

    var area = rect;
    final row = <_TmItem>[];
    var i = 0;
    while (i < items.length) {
      final shorter = math.min(area.width, area.height);
      final candidate = [...row, items[i]];
      final worstWith = _worstAspect(candidate, shorter, scale);
      final worstWithout =
          row.isEmpty ? double.infinity : _worstAspect(row, shorter, scale);
      if (row.isEmpty || worstWith <= worstWithout) {
        row.add(items[i]);
        i += 1;
      } else {
        area = _placeRow(row, area, scale, place);
        row.clear();
      }
    }
    if (row.isNotEmpty) {
      _placeRow(row, area, scale, place);
    }
  }

  double _worstAspect(List<_TmItem> row, double shorter, double scale) {
    var sum = 0.0;
    var maxA = 0.0;
    var minA = double.infinity;
    for (final it in row) {
      final a = it.node.value * scale;
      sum += a;
      if (a > maxA) maxA = a;
      if (a < minA) minA = a;
    }
    if (sum <= 0 || minA <= 0) {
      return double.infinity;
    }
    final s2 = shorter * shorter;
    final sum2 = sum * sum;
    return math.max(s2 * maxA / sum2, sum2 / (s2 * minA));
  }

  Rect _placeRow(
    List<_TmItem> row,
    Rect area,
    double scale,
    void Function(_TmItem item, Rect r) place,
  ) {
    final rowArea = row.fold<double>(0, (sum, it) => sum + it.node.value * scale);
    if (rowArea <= 0) {
      return area;
    }
    final horizontal = area.width >= area.height;
    if (horizontal) {
      final stripWidth = rowArea / area.height;
      var y = area.top;
      for (final it in row) {
        final h = (it.node.value * scale) / stripWidth;
        place(it, Rect.fromLTWH(area.left, y, stripWidth, h));
        y += h;
      }
      return Rect.fromLTRB(area.left + stripWidth, area.top, area.right, area.bottom);
    } else {
      final stripHeight = rowArea / area.width;
      var x = area.left;
      for (final it in row) {
        final w = (it.node.value * scale) / stripHeight;
        place(it, Rect.fromLTWH(x, area.top, w, stripHeight));
        x += w;
      }
      return Rect.fromLTRB(area.left, area.top + stripHeight, area.right, area.bottom);
    }
  }

  @override
  bool shouldRepaint(covariant _TreemapPainter oldDelegate) {
    return oldDelegate.diagram != diagram ||
        oldDelegate.textStyle != textStyle;
  }
}

import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';

/// 桑基流向图渲染:按最长路径分层把节点排到各列,节点高度正比于其吞吐量
/// (流入/流出的较大者),连接以贝塞尔流量带绘制,带宽正比于流量值。
class ChatMarkdownMermaidSankeyView extends StatelessWidget {
  const ChatMarkdownMermaidSankeyView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
  });

  final ChatMermaidSankeyDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;

  static const double _canvasHeight = 260;
  static const double _canvasWidth = 320;

  @override
  Widget build(BuildContext context) {
    final surface = _resolveSurfaceColor(backgroundColor);
    final borderColor = _resolveBorderColor(textStyle.color);
    final labelStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 3,
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
      child: Center(
        child: SizedBox(
          width: _canvasWidth,
          height: _canvasHeight,
          child: CustomPaint(
            painter: _SankeyPainter(
              diagram: diagram,
              labelStyle: labelStyle,
            ),
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

  static Color nodeColor(int index) {
    const palette = <Color>[
      Color(0xFF0F766E),
      Color(0xFF1D4ED8),
      Color(0xFFB45309),
      Color(0xFF166534),
      Color(0xFF9A3412),
      Color(0xFF7C3AED),
      Color(0xFFDB2777),
      Color(0xFF0891B2),
      Color(0xFF4D7C0F),
      Color(0xFFDC2626),
    ];
    return palette[index % palette.length];
  }
}

class _SankeyPainter extends CustomPainter {
  _SankeyPainter({required this.diagram, required this.labelStyle});

  final ChatMermaidSankeyDiagram diagram;
  final TextStyle labelStyle;

  static const double _nodeWidth = 12;
  static const double _nodeGap = 8;
  static const double _labelPad = 4;

  @override
  void paint(Canvas canvas, Size size) {
    final nodes = diagram.nodes;
    final links = diagram.links;
    if (nodes.isEmpty || links.isEmpty) {
      return;
    }

    final indexOf = <String, int>{
      for (var i = 0; i < nodes.length; i += 1) nodes[i].id: i,
    };

    // 出/入流量与邻接
    final outValue = List<double>.filled(nodes.length, 0);
    final inValue = List<double>.filled(nodes.length, 0);
    final outEdges = List.generate(nodes.length, (_) => <int>[]);
    for (final link in links) {
      final s = indexOf[link.sourceId]!;
      final t = indexOf[link.targetId]!;
      outValue[s] += link.value;
      inValue[t] += link.value;
      outEdges[s].add(t);
    }

    // 最长路径分层(DAG;迭代次数封顶以防环)
    final layer = List<int>.filled(nodes.length, 0);
    for (var iter = 0; iter < nodes.length; iter += 1) {
      var changed = false;
      for (var s = 0; s < nodes.length; s += 1) {
        for (final t in outEdges[s]) {
          if (layer[t] < layer[s] + 1) {
            layer[t] = layer[s] + 1;
            changed = true;
          }
        }
      }
      if (!changed) {
        break;
      }
    }
    final maxLayer = layer.reduce((a, b) => a > b ? a : b);

    final throughput = List<double>.generate(
      nodes.length,
      (i) => outValue[i] > inValue[i] ? outValue[i] : inValue[i],
    );

    // 各列节点与列内总量
    final columns = List.generate(maxLayer + 1, (_) => <int>[]);
    for (var i = 0; i < nodes.length; i += 1) {
      columns[layer[i]].add(i);
    }
    var maxColumnSum = 0.0;
    var maxColumnCount = 1;
    for (final column in columns) {
      var sum = 0.0;
      for (final i in column) {
        sum += throughput[i];
      }
      if (sum > maxColumnSum) maxColumnSum = sum;
      if (column.length > maxColumnCount) maxColumnCount = column.length;
    }
    if (maxColumnSum <= 0) {
      return;
    }

    final usableHeight = size.height - (maxColumnCount - 1) * _nodeGap;
    final valueScale = usableHeight / maxColumnSum;
    final columnSpacing =
        maxLayer == 0 ? 0.0 : (size.width - _nodeWidth) / maxLayer;

    // 节点矩形
    final nodeRect = List<Rect>.filled(nodes.length, Rect.zero);
    for (var l = 0; l <= maxLayer; l += 1) {
      final column = columns[l]..sort((a, b) => a.compareTo(b));
      var colTotal = (column.length - 1) * _nodeGap;
      for (final i in column) {
        colTotal += throughput[i] * valueScale;
      }
      var y = (size.height - colTotal) / 2;
      final x = l * columnSpacing;
      for (final i in column) {
        final h = throughput[i] * valueScale;
        nodeRect[i] = Rect.fromLTWH(x, y, _nodeWidth, h);
        y += h + _nodeGap;
      }
    }

    // 流量带:按连接顺序消费各节点的出/入游标
    final outCursor = [for (final r in nodeRect) r.top];
    final inCursor = [for (final r in nodeRect) r.top];
    for (final link in links) {
      final s = indexOf[link.sourceId]!;
      final t = indexOf[link.targetId]!;
      final h = link.value * valueScale;
      final x0 = nodeRect[s].right;
      final x1 = nodeRect[t].left;
      final y0 = outCursor[s];
      final y1 = inCursor[t];
      outCursor[s] += h;
      inCursor[t] += h;

      final midX = (x0 + x1) / 2;
      final path = Path()
        ..moveTo(x0, y0)
        ..cubicTo(midX, y0, midX, y1, x1, y1)
        ..lineTo(x1, y1 + h)
        ..cubicTo(midX, y1 + h, midX, y0 + h, x0, y0 + h)
        ..close();
      canvas.drawPath(
        path,
        Paint()
          ..color = ChatMarkdownMermaidSankeyView.nodeColor(s)
              .withValues(alpha: 0.32)
          ..style = PaintingStyle.fill,
      );
    }

    // 节点矩形
    for (var i = 0; i < nodes.length; i += 1) {
      final rect = nodeRect[i];
      if (rect.height <= 0) {
        continue;
      }
      canvas.drawRRect(
        RRect.fromRectAndRadius(rect, const Radius.circular(2)),
        Paint()..color = ChatMarkdownMermaidSankeyView.nodeColor(i),
      );
    }

    // 节点标签:最后一列画在左侧,其余画在右侧;垂直居中于节点。
    for (var i = 0; i < nodes.length; i += 1) {
      final rect = nodeRect[i];
      final painter = TextPainter(
        text: TextSpan(text: nodes[i].id, style: labelStyle),
        textDirection: TextDirection.ltr,
        maxLines: 1,
        ellipsis: '…',
      );
      final isLast = layer[i] == maxLayer;
      final maxLabelWidth = (isLast ? rect.left : size.width - rect.right) -
          _labelPad -
          2;
      painter.layout(maxWidth: maxLabelWidth.clamp(24.0, size.width));
      final dy = rect.center.dy - painter.height / 2;
      final dx = isLast
          ? rect.left - _labelPad - painter.width
          : rect.right + _labelPad;
      painter.paint(canvas, Offset(dx, dy));
    }
  }

  @override
  bool shouldRepaint(covariant _SankeyPainter oldDelegate) {
    return oldDelegate.diagram != diagram ||
        oldDelegate.labelStyle != labelStyle;
  }
}

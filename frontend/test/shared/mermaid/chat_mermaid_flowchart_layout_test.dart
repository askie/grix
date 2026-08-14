import 'package:flutter/material.dart';
import 'dart:math' as math;

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_flowchart_layout.dart';
import 'package:grix/shared/mermaid/chat_mermaid_layout_tokens.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/mermaid/chat_mermaid_node_style_tokens.dart';

void main() {
  const layoutEngine = ChatMermaidFlowchartLayoutEngine();
  const textStyle = TextStyle(fontSize: 12);

  test('layout engine defaults follow centralized flowchart spacing tokens',
      () {
    expect(
      layoutEngine.levelSeparation,
      ChatMermaidLayoutTokens.flowchartLevelSeparation,
    );
    expect(
      layoutEngine.nodeSeparation,
      ChatMermaidLayoutTokens.flowchartNodeSeparation,
    );
  });

  test('节点文字较多时高度随内容自适应，足以容纳完整文字且不被遮挡', () {
    const longLabel =
        '这是一个非常长的流程节点说明文字用于验证当内容很多时节点高度会随之增大'
        '而不会把文字遮挡或截断需要换行完整显示出来';
    const nodeStyle = TextStyle(
      fontSize: 12,
      fontWeight: FontWeight.w600,
      height: 1.2,
    );
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.topDown,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'A',
          label: longLabel,
          shape: ChatMermaidNodeShape.rectangle,
          order: 0,
        ),
      ],
      edges: <ChatMermaidEdge>[],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    final layout = layoutEngine.layout(
      diagram: diagram,
      textStyle: nodeStyle,
      labelStyle: nodeStyle,
      textDirection: TextDirection.ltr,
    );

    final rect = layout.nodeRects['A']!;
    final padding = ChatMermaidNodeStyleTokens.flowchartPaddingForShape(
      ChatMermaidNodeShape.rectangle,
    );
    // 渲染端文字可用宽度 = 节点宽度 − 水平内边距。
    final availableWidth = rect.width - padding.horizontal;
    final painter = TextPainter(
      text: const TextSpan(text: longLabel, style: nodeStyle),
      textDirection: TextDirection.ltr,
    )..layout(maxWidth: availableWidth);

    // 用例前提：文字确实多行（否则无法验证遮挡问题）。
    expect(painter.computeLineMetrics().length, greaterThan(2));
    // 关键断言：节点高度足以容纳按渲染可用宽度排版的完整文字，不会被遮挡。
    expect(
      rect.height,
      greaterThanOrEqualTo(painter.height + padding.vertical - 0.5),
    );
  });

  test('节点宽度小于 maxNodeTextWidth 时三行文字高度仍然足够不被遮挡', () {
    // 这条文字在 maxNodeTextWidth=200 约束下可能只换两行，
    // 但在实际节点宽度（更窄）下会换出三行；验证修复后节点高度按渲染宽度计算。
    const label = '第一行内容稍长\n第二行内容稍长\n第三行内容稍长';
    const nodeStyle = TextStyle(
      fontSize: 12,
      fontWeight: FontWeight.w600,
      height: 1.2,
    );
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.topDown,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'A',
          label: label,
          shape: ChatMermaidNodeShape.rectangle,
          order: 0,
        ),
      ],
      edges: <ChatMermaidEdge>[],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    final layout = layoutEngine.layout(
      diagram: diagram,
      textStyle: nodeStyle,
      labelStyle: nodeStyle,
      textDirection: TextDirection.ltr,
    );

    final rect = layout.nodeRects['A']!;
    final padding = ChatMermaidNodeStyleTokens.flowchartPaddingForShape(
      ChatMermaidNodeShape.rectangle,
    );
    final availableWidth = rect.width - padding.horizontal;
    final painter = TextPainter(
      text: const TextSpan(text: label, style: nodeStyle),
      textDirection: TextDirection.ltr,
    )..layout(maxWidth: availableWidth);

    expect(
      rect.height,
      greaterThanOrEqualTo(painter.height + padding.vertical - 0.5),
    );
  });

  test('top-down aligned nodes use a straight forward edge', () {
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.topDown,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'A',
          label: '开始',
          shape: ChatMermaidNodeShape.rounded,
          order: 0,
        ),
        ChatMermaidNode(
          id: 'B',
          label: '是否登录?',
          shape: ChatMermaidNodeShape.diamond,
          order: 1,
        ),
      ],
      edges: <ChatMermaidEdge>[
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'B',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 0,
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    final layout = layoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    final edge = layout.edges.single;
    expect(edge.points, hasLength(2));
    expect(edge.points.first.dx, closeTo(edge.points.last.dx, 0.001));
  });

  test('top-down edge label keeps a visible gap from vertical line', () {
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.topDown,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'A',
          label: '开始',
          shape: ChatMermaidNodeShape.rounded,
          order: 0,
        ),
        ChatMermaidNode(
          id: 'B',
          label: '是否登录?',
          shape: ChatMermaidNodeShape.diamond,
          order: 1,
        ),
      ],
      edges: <ChatMermaidEdge>[
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'B',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 0,
          label: '条件',
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    final layout = layoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    final edge = layout.edges.single;
    final lineX = edge.points.first.dx;
    final labelLeft = edge.labelAnchor.dx - (edge.labelSize.width / 2);
    expect(labelLeft - lineX, greaterThanOrEqualTo(layoutEngine.edgeLabelGap));
  });

  test('top-down edge label avoids overlapping a nearby diamond node', () {
    const tightLayoutEngine = ChatMermaidFlowchartLayoutEngine(
      levelSeparation: 16,
    );
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.topDown,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'A',
          label: '图片审核',
          shape: ChatMermaidNodeShape.diamond,
          order: 0,
        ),
        ChatMermaidNode(
          id: 'B',
          label: '图片违规',
          shape: ChatMermaidNodeShape.rectangle,
          order: 1,
        ),
      ],
      edges: <ChatMermaidEdge>[
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'B',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 0,
          label: '发现问题',
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    final layout = tightLayoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    final edge = layout.edges.single;
    final labelRect = Rect.fromCenter(
      center: edge.labelAnchor,
      width: edge.labelSize.width,
      height: edge.labelSize.height,
    );

    expect(labelRect.overlaps(layout.nodeRects['A']!), isFalse);
    expect(labelRect.overlaps(layout.nodeRects['B']!), isFalse);
  });

  test('incoming edge label avoids overlapping the target diamond node', () {
    const tightLayoutEngine = ChatMermaidFlowchartLayoutEngine(
      levelSeparation: 16,
    );
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.topDown,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'A',
          label: '敏感词检测',
          shape: ChatMermaidNodeShape.diamond,
          order: 0,
        ),
        ChatMermaidNode(
          id: 'B',
          label: '图片审核',
          shape: ChatMermaidNodeShape.diamond,
          order: 1,
        ),
        ChatMermaidNode(
          id: 'C',
          label: '图片违规',
          shape: ChatMermaidNodeShape.rectangle,
          order: 2,
        ),
      ],
      edges: <ChatMermaidEdge>[
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'B',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 0,
          label: '发现问题',
        ),
        ChatMermaidEdge(
          sourceId: 'B',
          targetId: 'C',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 1,
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    final layout = tightLayoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    final incomingEdge = layout.edges.first;
    final labelRect = Rect.fromCenter(
      center: incomingEdge.labelAnchor,
      width: incomingEdge.labelSize.width,
      height: incomingEdge.labelSize.height,
    );

    expect(labelRect.overlaps(layout.nodeRects['A']!), isFalse);
    expect(labelRect.overlaps(layout.nodeRects['B']!), isFalse);
  });

  test('top-down chained nodes with uneven widths keep the first edge straight',
      () {
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.topDown,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'submit',
          label: '📝 作者提交内容',
          shape: ChatMermaidNodeShape.stadium,
          order: 0,
        ),
        ChatMermaidNode(
          id: 'review',
          label: '🤖 AI自动审核',
          shape: ChatMermaidNodeShape.rounded,
          order: 1,
        ),
        ChatMermaidNode(
          id: 'result',
          label: '输出结果',
          shape: ChatMermaidNodeShape.rectangle,
          order: 2,
        ),
      ],
      edges: <ChatMermaidEdge>[
        ChatMermaidEdge(
          sourceId: 'submit',
          targetId: 'review',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 0,
        ),
        ChatMermaidEdge(
          sourceId: 'review',
          targetId: 'result',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 1,
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    final layout = layoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    final firstEdge = layout.edges.firstWhere(
      (edge) =>
          edge.edge.sourceId == 'submit' && edge.edge.targetId == 'review',
    );
    expect(firstEdge.points, hasLength(2));
    expect(firstEdge.points.first.dx, closeTo(firstEdge.points.last.dx, 0.001));
  });

  test('left-right aligned nodes use a straight forward edge', () {
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.leftRight,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'A',
          label: '开始',
          shape: ChatMermaidNodeShape.rounded,
          order: 0,
        ),
        ChatMermaidNode(
          id: 'B',
          label: '处理',
          shape: ChatMermaidNodeShape.rectangle,
          order: 1,
        ),
      ],
      edges: <ChatMermaidEdge>[
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'B',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 0,
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    final layout = layoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    final edge = layout.edges.single;
    expect(edge.points, hasLength(2));
    expect(edge.points.first.dy, closeTo(edge.points.last.dy, 0.001));
  });

  test('left-right edge label keeps a visible gap from horizontal line', () {
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.leftRight,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'A',
          label: '开始',
          shape: ChatMermaidNodeShape.rounded,
          order: 0,
        ),
        ChatMermaidNode(
          id: 'B',
          label: '处理',
          shape: ChatMermaidNodeShape.rectangle,
          order: 1,
        ),
      ],
      edges: <ChatMermaidEdge>[
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'B',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 0,
          label: '晴天',
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    final layout = layoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    final edge = layout.edges.single;
    final lineY = edge.points.first.dy;
    final labelBottom = edge.labelAnchor.dy + (edge.labelSize.height / 2);
    expect(
        lineY - labelBottom, greaterThanOrEqualTo(layoutEngine.edgeLabelGap));
  });

  test('top-down sibling branches with wide nodes do not overlap after lane alignment',
      () {
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.topDown,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'A',
          label: '入口',
          shape: ChatMermaidNodeShape.rounded,
          order: 0,
        ),
        ChatMermaidNode(
          id: 'B',
          label: '第一条分支需要执行一段非常长的处理说明，节点会接近最大宽度',
          shape: ChatMermaidNodeShape.rectangle,
          order: 1,
        ),
        ChatMermaidNode(
          id: 'C',
          label: '第二条分支也有一段非常长的处理说明，用来逼近相同的节点宽度',
          shape: ChatMermaidNodeShape.rectangle,
          order: 2,
        ),
        ChatMermaidNode(
          id: 'D',
          label: '结束一',
          shape: ChatMermaidNodeShape.rectangle,
          order: 3,
        ),
        ChatMermaidNode(
          id: 'E',
          label: '结束二',
          shape: ChatMermaidNodeShape.rectangle,
          order: 4,
        ),
      ],
      edges: <ChatMermaidEdge>[
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'B',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 0,
        ),
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'C',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 1,
        ),
        ChatMermaidEdge(
          sourceId: 'B',
          targetId: 'D',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 2,
        ),
        ChatMermaidEdge(
          sourceId: 'C',
          targetId: 'E',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 3,
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    final layout = layoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    expect(layout.nodeRects['B']!.overlaps(layout.nodeRects['C']!), isFalse);
    expect(layout.nodeRects['D']!.overlaps(layout.nodeRects['E']!), isFalse);
  });

  test('left-right sibling branches with tall nodes do not overlap after lane alignment',
      () {
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.leftRight,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'A',
          label: '入口',
          shape: ChatMermaidNodeShape.rounded,
          order: 0,
        ),
        ChatMermaidNode(
          id: 'B',
          label: '第一条分支说明很长很长很长很长，需要换成多行才能完整展示',
          shape: ChatMermaidNodeShape.rectangle,
          order: 1,
        ),
        ChatMermaidNode(
          id: 'C',
          label: '第二条分支说明同样很长很长很长很长，也会被换成多行展示',
          shape: ChatMermaidNodeShape.rectangle,
          order: 2,
        ),
        ChatMermaidNode(
          id: 'D',
          label: '结束一',
          shape: ChatMermaidNodeShape.rectangle,
          order: 3,
        ),
        ChatMermaidNode(
          id: 'E',
          label: '结束二',
          shape: ChatMermaidNodeShape.rectangle,
          order: 4,
        ),
      ],
      edges: <ChatMermaidEdge>[
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'B',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 0,
        ),
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'C',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 1,
        ),
        ChatMermaidEdge(
          sourceId: 'B',
          targetId: 'D',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 2,
        ),
        ChatMermaidEdge(
          sourceId: 'C',
          targetId: 'E',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 3,
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    final layout = layoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    expect(layout.nodeRects['B']!.overlaps(layout.nodeRects['C']!), isFalse);
    expect(layout.nodeRects['D']!.overlaps(layout.nodeRects['E']!), isFalse);
  });

  test('top-down sibling branches stay separated even when upstream node spacing is compressed',
      () {
    const compressedLayoutEngine = ChatMermaidFlowchartLayoutEngine(
      nodeSeparation: -80,
      levelSeparation: 48,
    );
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.topDown,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'A',
          label: '入口',
          shape: ChatMermaidNodeShape.rounded,
          order: 0,
        ),
        ChatMermaidNode(
          id: 'B',
          label: '第一条分支需要执行一段非常长的处理说明，节点会接近最大宽度',
          shape: ChatMermaidNodeShape.rectangle,
          order: 1,
        ),
        ChatMermaidNode(
          id: 'C',
          label: '第二条分支也有一段非常长的处理说明，用来逼近相同的节点宽度',
          shape: ChatMermaidNodeShape.rectangle,
          order: 2,
        ),
      ],
      edges: <ChatMermaidEdge>[
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'B',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 0,
        ),
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'C',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 1,
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    final layout = compressedLayoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    expect(layout.nodeRects['B']!.overlaps(layout.nodeRects['C']!), isFalse);
  });

  test('left-right sibling branches stay separated even when upstream node spacing is compressed',
      () {
    const compressedLayoutEngine = ChatMermaidFlowchartLayoutEngine(
      nodeSeparation: -80,
      levelSeparation: 48,
    );
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.leftRight,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'A',
          label: '入口',
          shape: ChatMermaidNodeShape.rounded,
          order: 0,
        ),
        ChatMermaidNode(
          id: 'B',
          label: '第一条分支说明很长很长很长很长，需要换成多行才能完整展示',
          shape: ChatMermaidNodeShape.rectangle,
          order: 1,
        ),
        ChatMermaidNode(
          id: 'C',
          label: '第二条分支说明同样很长很长很长很长，也会被换成多行展示',
          shape: ChatMermaidNodeShape.rectangle,
          order: 2,
        ),
      ],
      edges: <ChatMermaidEdge>[
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'B',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 0,
        ),
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'C',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 1,
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    final layout = compressedLayoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    expect(layout.nodeRects['B']!.overlaps(layout.nodeRects['C']!), isFalse);
  });

  test('subgraph diagram reserves top/left padding so nothing is clipped', () {
    const diagram = ChatMermaidFlowchart(
      direction: ChatMermaidFlowDirection.topDown,
      nodes: <ChatMermaidNode>[
        ChatMermaidNode(
          id: 'A',
          label: '开始',
          shape: ChatMermaidNodeShape.rounded,
          order: 0,
        ),
        ChatMermaidNode(
          id: 'B',
          label: '处理',
          shape: ChatMermaidNodeShape.rectangle,
          order: 1,
        ),
      ],
      edges: <ChatMermaidEdge>[
        ChatMermaidEdge(
          sourceId: 'A',
          targetId: 'B',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 0,
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[
        ChatMermaidFlowSubgraph(
          id: 'group',
          label: '分组标题',
          order: 0,
          depth: 0,
          nodeIds: <String>['A', 'B'],
        ),
      ],
    );

    final layout = layoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    // subgraph 卡片顶部曾用 top-34 落到负坐标，归一化后必须留出 padding 留白。
    final subgraph = layout.subgraphRects.single;
    expect(subgraph.rect.top, greaterThanOrEqualTo(layoutEngine.padding.top));
    expect(subgraph.rect.left, greaterThanOrEqualTo(layoutEngine.padding.left));
    expect(subgraph.labelRect.top, greaterThanOrEqualTo(0));

    // 画布需完整容纳所有元素，不得在右/下侧越界。
    for (final rect in layout.nodeRects.values) {
      expect(rect.top, greaterThanOrEqualTo(0));
      expect(rect.left, greaterThanOrEqualTo(0));
    }
    expect(subgraph.rect.bottom, lessThanOrEqualTo(layout.canvasSize.height));
    expect(subgraph.rect.right, lessThanOrEqualTo(layout.canvasSize.width));
  });

  test('菱形节点放大外接框，长文字落入内切区不超出斜边', () {
    final result = _renderMeasureNode(
      layoutEngine,
      _longShapeLabel,
      ChatMermaidNodeShape.diamond,
      _shapeNodeStyle,
    );
    // 文字块以菱形中心为中心，四角落在菱形内的条件：w/W + h/H <= 1。
    final ratio = result.textWidth / result.rect.width +
        result.textHeight / result.rect.height;
    expect(ratio, lessThanOrEqualTo(1.0));
  });

  test('圆形节点直径足够，长文字对角线不超出圆周', () {
    final result = _renderMeasureNode(
      layoutEngine,
      _longShapeLabel,
      ChatMermaidNodeShape.circle,
      _shapeNodeStyle,
    );
    // 圆能容纳文字块的条件：文字块对角线 <= 直径（圆形 width == height）。
    final diagonal = math.sqrt(
      (result.textWidth * result.textWidth) +
          (result.textHeight * result.textHeight),
    );
    expect(diagonal, lessThanOrEqualTo(result.rect.width + 0.001));
  });

  test('六边形节点文字落在可用区域内不超出', () {
    final result = _renderMeasureNode(
      layoutEngine,
      _longShapeLabel,
      ChatMermaidNodeShape.hexagon,
      _shapeNodeStyle,
    );
    // 六边形左右各有 0.15 宽的斜切，文字块上下边缘处可用宽度最小，
    // 约为 W*(1 - 0.3 * h/H)，文字宽需不超过该值。
    final usableWidth = result.rect.width *
        (1 - (0.3 * result.textHeight / result.rect.height));
    expect(result.textWidth, lessThanOrEqualTo(usableWidth + 0.001));
  });
}

/// 用于形状内切容纳测试的较长多行文字。
const String _longShapeLabel = '是否已满足全部前置校验条件并通过相关审批流程后继续';

const TextStyle _shapeNodeStyle = TextStyle(
  fontSize: 12,
  fontWeight: FontWeight.w600,
  height: 1.2,
);

/// 按节点最终尺寸与渲染端可用宽度，复现文字块的实际渲染尺寸。
///
/// 返回节点矩形以及文字在「节点宽度 − 水平内边距」约束下排版得到的实际宽高，
/// 与渲染端 `Center(Text(...))` 的真实占用一致，用于判断文字是否落在形状内。
({Rect rect, double textWidth, double textHeight}) _renderMeasureNode(
  ChatMermaidFlowchartLayoutEngine engine,
  String label,
  ChatMermaidNodeShape shape,
  TextStyle style,
) {
  final diagram = ChatMermaidFlowchart(
    direction: ChatMermaidFlowDirection.topDown,
    nodes: <ChatMermaidNode>[
      ChatMermaidNode(id: 'A', label: label, shape: shape, order: 0),
    ],
    edges: const <ChatMermaidEdge>[],
    subgraphs: const <ChatMermaidFlowSubgraph>[],
  );
  final layout = engine.layout(
    diagram: diagram,
    textStyle: style,
    labelStyle: style,
    textDirection: TextDirection.ltr,
  );
  final rect = layout.nodeRects['A']!;
  final padding = ChatMermaidNodeStyleTokens.flowchartPaddingForShape(shape);
  final painter = TextPainter(
    text: TextSpan(text: label, style: style),
    textDirection: TextDirection.ltr,
  )..layout(maxWidth: rect.width - padding.horizontal);
  return (rect: rect, textWidth: painter.width, textHeight: painter.height);
}

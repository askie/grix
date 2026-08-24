import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:graphview/GraphView.dart';

import 'chat_mermaid_model.dart';
import 'chat_mermaid_node_style_tokens.dart';

class ChatMermaidStateLayout {
  const ChatMermaidStateLayout({
    required this.canvasSize,
    required this.nodeRects,
    required this.transitions,
  });

  final Size canvasSize;
  final Map<String, Rect> nodeRects;
  final List<ChatMermaidStateTransitionLayout> transitions;
}

class ChatMermaidStateTransitionLayout {
  const ChatMermaidStateTransitionLayout({
    required this.transition,
    required this.points,
    required this.labelAnchor,
    this.labelSize = Size.zero,
  });

  final ChatMermaidStateTransition transition;
  final List<Offset> points;
  final Offset labelAnchor;
  final Size labelSize;
}

class ChatMermaidStateLayoutEngine {
  const ChatMermaidStateLayoutEngine({
    this.padding = const EdgeInsets.all(20),
    this.maxNodeTextWidth = 200,
    this.alignmentTolerance = 1,
    this.edgeLabelGap = 6,
    this.maxLabelTextWidth = 220,
  });

  final EdgeInsets padding;
  final double maxNodeTextWidth;
  final double alignmentTolerance;
  final double edgeLabelGap;
  final double maxLabelTextWidth;

  ChatMermaidStateLayout layout({
    required ChatMermaidStateDiagram diagram,
    required TextStyle stateStyle,
    required TextStyle labelStyle,
    required TextDirection textDirection,
  }) {
    final graph = Graph();
    final graphNodes = <String, Node>{};
    final nodeRects = <String, Rect>{};

    for (final node in diagram.nodes) {
      final graphNode = Node.Id(node.id)
        ..size = _measureNode(
          node: node,
          style: stateStyle,
          textDirection: textDirection,
        );
      graphNodes[node.id] = graphNode;
      graph.addNode(graphNode);
    }

    for (final transition in diagram.transitions) {
      if (transition.isSelfTransition) {
        continue;
      }
      final source = graphNodes[transition.sourceId];
      final target = graphNodes[transition.targetId];
      if (source == null || target == null) {
        continue;
      }
      graph.addEdge(source, target);
    }

    final configuration = SugiyamaConfiguration()
      ..orientation = SugiyamaConfiguration.ORIENTATION_TOP_BOTTOM
      ..levelSeparation = 92
      ..nodeSeparation = 42
      // 与 graphview 1.2.0 行为保持一致：按加边顺序反转回边。
      ..cycleRemovalStrategy = CycleRemovalStrategy.dfs;
    final algorithm = SugiyamaAlgorithm(configuration);
    algorithm.run(graph, padding.left, padding.top);

    var maxRight = 0.0;
    var maxBottom = 0.0;
    for (final node in diagram.nodes) {
      final graphNode = graphNodes[node.id]!;
      nodeRects[node.id] = Rect.fromLTWH(
        graphNode.x,
        graphNode.y,
        graphNode.width,
        graphNode.height,
      );
    }
    final normalizedNodeRects = _centerNodesWithinVerticalLanes(nodeRects);
    nodeRects
      ..clear()
      ..addAll(normalizedNodeRects);
    for (final rect in nodeRects.values) {
      maxRight = math.max(maxRight, rect.right);
      maxBottom = math.max(maxBottom, rect.bottom);
    }

    final transitions = <ChatMermaidStateTransitionLayout>[];
    final selfTurnByNode = <String, int>{};
    var backEdgeCount = 0;
    for (final transition in diagram.transitions) {
      final sourceRect = nodeRects[transition.sourceId];
      final targetRect = nodeRects[transition.targetId];
      if (sourceRect == null || targetRect == null) {
        continue;
      }

      late final ChatMermaidStateTransitionLayout routed;
      if (transition.isSelfTransition) {
        final selfTurn = selfTurnByNode.update(
          transition.sourceId,
          (value) => value + 1,
          ifAbsent: () => 0,
        );
        routed = _routeSelfTransition(
          transition: transition,
          rect: sourceRect,
          selfTurn: selfTurn,
          labelStyle: labelStyle,
          textDirection: textDirection,
        );
      } else {
        final isBackEdge = targetRect.center.dy < sourceRect.center.dy;
        routed = _routeTransition(
          transition: transition,
          sourceRect: sourceRect,
          targetRect: targetRect,
          graphRight: maxRight,
          backEdgeIndex: isBackEdge ? backEdgeCount++ : 0,
          labelStyle: labelStyle,
          textDirection: textDirection,
        );
      }

      maxRight = math.max(maxRight, _maxPointDx(routed.points) + padding.right);
      maxBottom =
          math.max(maxBottom, _maxPointDy(routed.points) + padding.bottom);
      if (routed.labelSize != Size.zero) {
        maxRight = math.max(
          maxRight,
          routed.labelAnchor.dx + routed.labelSize.width + padding.right,
        );
        maxBottom = math.max(
          maxBottom,
          routed.labelAnchor.dy + routed.labelSize.height + padding.bottom,
        );
      }
      transitions.add(routed);
    }

    return ChatMermaidStateLayout(
      canvasSize: Size(maxRight + padding.right, maxBottom + padding.bottom),
      nodeRects: Map.unmodifiable(nodeRects),
      transitions: List.unmodifiable(transitions),
    );
  }

  Size _measureNode({
    required ChatMermaidStateNode node,
    required TextStyle style,
    required TextDirection textDirection,
  }) {
    switch (node.kind) {
      case ChatMermaidStateNodeKind.start:
        return const Size.square(18);
      case ChatMermaidStateNodeKind.end:
        return const Size.square(24);
      case ChatMermaidStateNodeKind.regular:
        const contentPadding = ChatMermaidNodeStyleTokens.stateNodePadding;
        final painter = TextPainter(
          text: TextSpan(text: node.label, style: style),
          textDirection: textDirection,
          maxLines: 3,
          ellipsis: '...',
        )..layout(maxWidth: maxNodeTextWidth);
        return Size(
          (painter.width + contentPadding.horizontal + 4)
              .clamp(90, 220)
              .toDouble(),
          (painter.height + contentPadding.vertical + 4)
              .clamp(44, 132)
              .toDouble(),
        );
    }
  }

  ChatMermaidStateTransitionLayout _routeTransition({
    required ChatMermaidStateTransition transition,
    required Rect sourceRect,
    required Rect targetRect,
    required double graphRight,
    required int backEdgeIndex,
    required TextStyle labelStyle,
    required TextDirection textDirection,
  }) {
    final labelSize = _measureLabel(
      label: transition.label,
      style: labelStyle,
      textDirection: textDirection,
    );

    final isHorizontalRank =
        (sourceRect.center.dy - targetRect.center.dy).abs() <= 12;
    if (isHorizontalRank) {
      final start = sourceRect.center.dx <= targetRect.center.dx
          ? Offset(sourceRect.right, sourceRect.center.dy)
          : Offset(sourceRect.left, sourceRect.center.dy);
      final end = sourceRect.center.dx <= targetRect.center.dx
          ? Offset(targetRect.left, targetRect.center.dy)
          : Offset(targetRect.right, targetRect.center.dy);
      if (_isAligned(start.dy, end.dy)) {
        final midX = (start.dx + end.dx) / 2;
        return ChatMermaidStateTransitionLayout(
          transition: transition,
          points: <Offset>[start, end],
          labelAnchor: _labelAnchorAboveHorizontal(
            midX: midX,
            lineY: start.dy,
            labelSize: labelSize,
          ),
          labelSize: labelSize,
        );
      }
      final midX = (start.dx + end.dx) / 2;
      return ChatMermaidStateTransitionLayout(
        transition: transition,
        points: <Offset>[
          start,
          Offset(midX, start.dy),
          Offset(midX, end.dy),
          end,
        ],
        labelAnchor: _labelAnchorRightOfVertical(
          lineX: midX,
          midY: (start.dy + end.dy) / 2,
          labelSize: labelSize,
        ),
        labelSize: labelSize,
      );
    }

    final isBackEdge = targetRect.center.dy < sourceRect.center.dy;
    if (isBackEdge) {
      final laneX = graphRight + 44 + (backEdgeIndex * 30);
      final start = Offset(sourceRect.right, sourceRect.center.dy);
      final end = Offset(targetRect.right, targetRect.center.dy);
      return ChatMermaidStateTransitionLayout(
        transition: transition,
        points: <Offset>[
          start,
          Offset(laneX, start.dy),
          Offset(laneX, end.dy),
          end,
        ],
        labelAnchor: _labelAnchorRightOfVertical(
          lineX: laneX,
          midY: (start.dy + end.dy) / 2,
          labelSize: labelSize,
        ),
        labelSize: labelSize,
      );
    }

    final start = Offset(sourceRect.center.dx, sourceRect.bottom);
    final end = Offset(targetRect.center.dx, targetRect.top);
    if (_isAligned(start.dx, end.dx)) {
      final midY = (start.dy + end.dy) / 2;
      return ChatMermaidStateTransitionLayout(
        transition: transition,
        points: <Offset>[start, end],
        labelAnchor: _labelAnchorRightOfVertical(
          lineX: start.dx,
          midY: midY,
          labelSize: labelSize,
        ),
        labelSize: labelSize,
      );
    }
    final midY = (start.dy + end.dy) / 2;
    return ChatMermaidStateTransitionLayout(
      transition: transition,
      points: <Offset>[
        start,
        Offset(start.dx, midY),
        Offset(end.dx, midY),
        end,
      ],
      labelAnchor: _labelAnchorAboveHorizontal(
        midX: (start.dx + end.dx) / 2,
        lineY: midY,
        labelSize: labelSize,
      ),
      labelSize: labelSize,
    );
  }

  ChatMermaidStateTransitionLayout _routeSelfTransition({
    required ChatMermaidStateTransition transition,
    required Rect rect,
    required int selfTurn,
    required TextStyle labelStyle,
    required TextDirection textDirection,
  }) {
    final labelSize = _measureLabel(
      label: transition.label,
      style: labelStyle,
      textDirection: textDirection,
    );
    final loopX = rect.right + 42 + (selfTurn * 12);
    final topY = rect.top - 26 - (selfTurn * 10);
    final start = Offset(rect.right, rect.center.dy - 4);
    final end = Offset(rect.center.dx + math.min(14, rect.width / 3), rect.top);
    return ChatMermaidStateTransitionLayout(
      transition: transition,
      points: <Offset>[
        start,
        Offset(loopX, start.dy),
        Offset(loopX, topY),
        Offset(end.dx, topY),
        end,
      ],
      labelAnchor: _labelAnchorAboveHorizontal(
        midX: (loopX + end.dx) / 2,
        lineY: topY,
        labelSize: labelSize,
      ),
      labelSize: labelSize,
    );
  }

  Size _measureLabel({
    required String? label,
    required TextStyle style,
    required TextDirection textDirection,
  }) {
    if (label == null || label.isEmpty) {
      return Size.zero;
    }
    final painter = TextPainter(
      text: TextSpan(text: label, style: style),
      textDirection: textDirection,
    )..layout(maxWidth: maxLabelTextWidth);
    return Size(painter.width + 10, painter.height + 6);
  }

  double _maxPointDx(List<Offset> points) => points.fold<double>(
        0,
        (current, point) => math.max(current, point.dx),
      );

  double _maxPointDy(List<Offset> points) => points.fold<double>(
        0,
        (current, point) => math.max(current, point.dy),
      );

  bool _isAligned(double start, double end) =>
      (start - end).abs() <= alignmentTolerance;

  Offset _labelAnchorAboveHorizontal({
    required double midX,
    required double lineY,
    required Size labelSize,
  }) {
    return Offset(midX, lineY - (labelSize.height / 2) - edgeLabelGap);
  }

  Offset _labelAnchorRightOfVertical({
    required double lineX,
    required double midY,
    required Size labelSize,
  }) {
    return Offset(lineX + (labelSize.width / 2) + edgeLabelGap, midY);
  }

  Map<String, Rect> _centerNodesWithinVerticalLanes(
      Map<String, Rect> nodeRects) {
    final entries = nodeRects.entries.toList()
      ..sort((left, right) => left.value.left.compareTo(right.value.left));
    final normalized = <String, Rect>{};

    for (final lane in _groupNodeRectEntries(entries)) {
      final laneLeft = lane
              .map((entry) => entry.value.left)
              .reduce((sum, value) => sum + value) /
          lane.length;
      final laneWidth = lane.map((entry) => entry.value.width).reduce(math.max);
      for (final entry in lane) {
        normalized[entry.key] = Rect.fromLTWH(
          laneLeft + ((laneWidth - entry.value.width) / 2),
          entry.value.top,
          entry.value.width,
          entry.value.height,
        );
      }
    }

    return normalized;
  }

  List<List<MapEntry<String, Rect>>> _groupNodeRectEntries(
    List<MapEntry<String, Rect>> entries,
  ) {
    final groups = <List<MapEntry<String, Rect>>>[];
    for (final entry in entries) {
      if (groups.isEmpty) {
        groups.add(<MapEntry<String, Rect>>[entry]);
        continue;
      }
      final previous = groups.last.last;
      if ((entry.value.left - previous.value.left).abs() <=
          alignmentTolerance) {
        groups.last.add(entry);
        continue;
      }
      groups.add(<MapEntry<String, Rect>>[entry]);
    }
    return groups;
  }
}

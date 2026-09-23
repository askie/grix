import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_flowchart_layout.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/mermaid/chat_mermaid_parser.dart';

/// 老郭 2026-09-23 报的架构图：一个根扇出 8 个节点，5 个 features 各连 3 个
/// 下游。原来 recording 被排到最右、连线横跨全图，17 条水平段挤在一个层间
/// 空隙里溢出到下一层节点顶上被遮住。
const _fanoutSource = r'''
flowchart TD
  P[map_page 组合根<br/>只装配, <400 行] --> S[startup/ 启动编排器<br/>协议→缓存起图→bootstrap并行→定位→首次引导]
  P --> C[prompts/ 弹窗协调器<br/>唯一弹窗出口·优先级·单模态·记账]
  P --> G[entitlements/ 权益门<br/>唯一权益判断入口]
  P --> F1[features/recording 录制]
  P --> F2[features/spots 采点]
  P --> F3[features/layers 底图/图层/三维]
  P --> F4[features/today 今日出菇]
  P --> F5[features/search · weather · social]
  F1 & F2 & F3 & F4 & F5 --> C
  F1 & F2 & F3 & F4 & F5 --> G
  F1 & F2 & F3 & F4 & F5 --> E[engine/ 地图引擎抽象<br/>保持现状]
  E --> JS[WebView js/ 模块<br/>state·bridge·map_core·tiles·forest·today·labels·interaction·weather]
''';

void main() {
  const layoutEngine = ChatMermaidFlowchartLayoutEngine();
  const textStyle = TextStyle(fontSize: 12);
  const laneGap = 12.0;
  const laneInset = 14.0;

  ChatMermaidFlowchartLayout runLayout(String text) {
    final result = const ChatMermaidParser().parse(text);
    expect(result.diagram, isA<ChatMermaidFlowchart>(), reason: result.error);
    return layoutEngine.layout(
      diagram: result.diagram! as ChatMermaidFlowchart,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );
  }

  /// 折线的中间段（去掉贴着端点节点的首尾段）。
  Iterable<(Offset, Offset)> middleSegments(ChatMermaidRoutedEdge edge) sync* {
    final points = edge.points;
    for (var i = 1; i + 2 < points.length; i++) {
      yield (points[i], points[i + 1]);
    }
  }

  bool segmentHitsRect((Offset, Offset) segment, Rect rect) {
    final (a, b) = segment;
    final lo = Offset(math.min(a.dx, b.dx), math.min(a.dy, b.dy));
    final hi = Offset(math.max(a.dx, b.dx), math.max(a.dy, b.dy));
    return hi.dx > rect.left + 0.5 &&
        lo.dx < rect.right - 0.5 &&
        hi.dy > rect.top + 0.5 &&
        lo.dy < rect.bottom - 0.5;
  }

  bool isHorizontal((Offset, Offset) s) => (s.$1.dy - s.$2.dy).abs() < 0.5;

  bool orthogonalCross((Offset, Offset) h, (Offset, Offset) v) {
    final hx = (math.min(h.$1.dx, h.$2.dx), math.max(h.$1.dx, h.$2.dx));
    final vy = (math.min(v.$1.dy, v.$2.dy), math.max(v.$1.dy, v.$2.dy));
    final x = v.$1.dx;
    final y = h.$1.dy;
    return x > hx.$1 + 0.5 &&
        x < hx.$2 - 0.5 &&
        y > vy.$1 + 0.5 &&
        y < vy.$2 - 0.5;
  }

  bool edgesCross(ChatMermaidRoutedEdge a, ChatMermaidRoutedEdge b) {
    List<(Offset, Offset)> segmentsOf(ChatMermaidRoutedEdge e) => [
      for (var i = 0; i + 1 < e.points.length; i++)
        (e.points[i], e.points[i + 1]),
    ];
    for (final sa in segmentsOf(a)) {
      for (final sb in segmentsOf(b)) {
        if (isHorizontal(sa) && !isHorizontal(sb) && orthogonalCross(sa, sb)) {
          return true;
        }
        if (!isHorizontal(sa) && isHorizontal(sb) && orthogonalCross(sb, sa)) {
          return true;
        }
      }
    }
    return false;
  }

  group('扇入扇出密集的架构图', () {
    late ChatMermaidFlowchartLayout layout;

    setUp(() {
      layout = runLayout(_fanoutSource);
    });

    test('同一父节点的 features 在层内连成一片，无出边的 startup 靠边', () {
      final row = ['S', 'F1', 'F2', 'F3', 'F4', 'F5']
        ..sort(
          (a, b) =>
              layout.nodeRects[a]!.left.compareTo(layout.nodeRects[b]!.left),
        );
      expect(row.first == 'S' || row.last == 'S', isTrue, reason: '$row');
    });

    test('下游 C/E/G 落在 features 一排的横向范围之内', () {
      double left(String id) => layout.nodeRects[id]!.left;
      double right(String id) => layout.nodeRects[id]!.right;
      final featuresLeft = [
        'F1',
        'F2',
        'F3',
        'F4',
        'F5',
      ].map(left).reduce(math.min);
      final featuresRight = [
        'F1',
        'F2',
        'F3',
        'F4',
        'F5',
      ].map(right).reduce(math.max);
      for (final id in ['C', 'E', 'G']) {
        expect(left(id), greaterThanOrEqualTo(featuresLeft - 1), reason: id);
        expect(right(id), lessThanOrEqualTo(featuresRight + 1), reason: id);
      }
    });

    test('连线的中间段不穿过任何节点，且与节点边框至少留出箭头长度', () {
      for (final edge in layout.edges) {
        for (final segment in middleSegments(edge)) {
          for (final entry in layout.nodeRects.entries) {
            final rect = entry.value;
            expect(
              segmentHitsRect(segment, rect),
              isFalse,
              reason:
                  '${edge.edge.sourceId}→${edge.edge.targetId} 段 $segment 穿过 ${entry.key} $rect',
            );
            if (!isHorizontal(segment)) {
              continue;
            }
            final y = segment.$1.dy;
            final lo = math.min(segment.$1.dx, segment.$2.dx);
            final hi = math.max(segment.$1.dx, segment.$2.dx);
            if (hi <= rect.left || lo >= rect.right) {
              continue;
            }
            final distance = math.min(
              (y - rect.top).abs(),
              (y - rect.bottom).abs(),
            );
            expect(
              distance,
              greaterThanOrEqualTo(laneInset - 0.5),
              reason:
                  '${edge.edge.sourceId}→${edge.edge.targetId} 水平段 y=$y 贴着 ${entry.key} $rect',
            );
          }
        }
      }
    });

    test('同一空隙里 x 区间重叠的水平段按整车道错开，不被压扁', () {
      final horizontals = <(String, (Offset, Offset))>[
        for (final edge in layout.edges)
          for (final segment in middleSegments(edge))
            if (isHorizontal(segment))
              ('${edge.edge.sourceId}→${edge.edge.targetId}', segment),
      ];
      for (var i = 0; i < horizontals.length; i++) {
        for (var j = i + 1; j < horizontals.length; j++) {
          final a = horizontals[i].$2;
          final b = horizontals[j].$2;
          final aLo = math.min(a.$1.dx, a.$2.dx),
              aHi = math.max(a.$1.dx, a.$2.dx);
          final bLo = math.min(b.$1.dx, b.$2.dx),
              bHi = math.max(b.$1.dx, b.$2.dx);
          if (aHi < bLo - 1 || bHi < aLo - 1) {
            continue;
          }
          expect(
            (a.$1.dy - b.$1.dy).abs(),
            greaterThanOrEqualTo(laneGap - 1),
            reason:
                '${horizontals[i].$1} 与 ${horizontals[j].$1} 车道间距不足: $a / $b',
          );
        }
      }
    });

    test('同一 features 出发、同侧目标的三条线互不交叉', () {
      final byPair = <String, ChatMermaidRoutedEdge>{
        for (final edge in layout.edges)
          '${edge.edge.sourceId}→${edge.edge.targetId}': edge,
      };
      for (final source in ['F1', 'F2', 'F3', 'F4', 'F5']) {
        final sx = layout.nodeRects[source]!.center.dx;
        final sameSide = <ChatMermaidRoutedEdge>[];
        for (final target in ['C', 'E', 'G']) {
          final edge = byPair['$source→$target']!;
          final tx = layout.nodeRects[target]!.center.dx;
          if ((tx - sx).abs() < 1) {
            continue;
          }
          sameSide.add(edge);
        }
        final rights = sameSide
            .where((e) => layout.nodeRects[e.edge.targetId]!.center.dx > sx)
            .toList();
        final lefts = sameSide
            .where((e) => layout.nodeRects[e.edge.targetId]!.center.dx < sx)
            .toList();
        for (final side in [rights, lefts]) {
          for (var i = 0; i < side.length; i++) {
            for (var j = i + 1; j < side.length; j++) {
              expect(
                edgesCross(side[i], side[j]),
                isFalse,
                reason:
                    '${side[i].edge.sourceId}→${side[i].edge.targetId} 与 '
                    '${side[j].edge.sourceId}→${side[j].edge.targetId} 交叉: '
                    '${side[i].points} / ${side[j].points}',
              );
            }
          }
        }
      }
    });
  });

  test('层内排序按邻居中位数：两个父节点的孩子各自成组，不交错', () {
    final layout = runLayout(r'''
flowchart TD
  A --> a1
  B --> b1
  A --> a2
  B --> b2
  A --> a3
  B --> b3
''');
    final row = ['a1', 'b1', 'a2', 'b2', 'a3', 'b3']
      ..sort(
        (x, y) =>
            layout.nodeRects[x]!.left.compareTo(layout.nodeRects[y]!.left),
      );
    final firstLetters = row.map((id) => id[0]).join();
    expect(
      firstLetters == 'aaabbb' || firstLetters == 'bbbaaa',
      isTrue,
      reason: '层内顺序 $row',
    );
  });
}

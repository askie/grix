import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_flowchart_layout.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/mermaid/chat_mermaid_parser.dart';

void main() {
  const layoutEngine = ChatMermaidFlowchartLayoutEngine();
  const textStyle = TextStyle(fontSize: 12);
  const laneGap = 12.0;

  ChatMermaidFlowchart parse(String text) {
    final result = const ChatMermaidParser().parse(text);
    final diagram = result.diagram;
    expect(
      diagram,
      isA<ChatMermaidFlowchart>(),
      reason: '解析失败: ${result.error}',
    );
    return diagram! as ChatMermaidFlowchart;
  }

  ChatMermaidFlowchartLayout runLayout(ChatMermaidFlowchart diagram) {
    return layoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );
  }

  ChatMermaidRoutedEdge? findEdge(
    ChatMermaidFlowchartLayout layout,
    String sourceId,
    String targetId,
  ) {
    for (final routed in layout.edges) {
      if (routed.edge.sourceId == sourceId &&
          routed.edge.targetId == targetId) {
        return routed;
      }
    }
    return null;
  }

  void expectAdjacentLayerZBend({
    required ChatMermaidFlowchartLayout layout,
    required String sourceId,
    required String targetId,
    Rect? subgraphBounds,
  }) {
    final sourceRect = layout.nodeRects[sourceId]!;
    final targetRect = layout.nodeRects[targetId]!;
    final routed = findEdge(layout, sourceId, targetId);
    expect(routed, isNotNull, reason: '$sourceId → $targetId 未路由');
    final points = routed!.points;
    expect(
      points.length,
      lessThanOrEqualTo(4),
      reason: '$sourceId → $targetId 折点过多: ${points.length} $points',
    );

    final layerLo = sourceRect.right;
    final layerHi = targetRect.left;
    final yLo =
        math.min(sourceRect.top, targetRect.top) - laneGap - 1;
    final yHi =
        math.max(sourceRect.bottom, targetRect.bottom) + laneGap + 1;

    for (final point in points) {
      expect(
        point.dx,
        greaterThanOrEqualTo(layerLo - 0.5),
        reason: '$sourceId→$targetId x 超出层轴下界: $point',
      );
      expect(
        point.dx,
        lessThanOrEqualTo(layerHi + 0.5),
        reason: '$sourceId→$targetId x 超出层轴上界: $point',
      );
      expect(
        point.dy,
        greaterThanOrEqualTo(yLo),
        reason: '$sourceId→$targetId y 过高（绕到框外）: $point',
      );
      expect(
        point.dy,
        lessThanOrEqualTo(yHi),
        reason: '$sourceId→$targetId y 过低: $point',
      );
      if (subgraphBounds != null) {
        expect(
          subgraphBounds.contains(point),
          isTrue,
          reason: '$sourceId→$targetId 点离开分组框: $point 框 $subgraphBounds',
        );
      }
    }
  }

  group('LR 静态生境图：相邻层 Z 弯不穿分组框外', () {
    const source = r'''
flowchart LR
  subgraph A[静态生境适宜性 A]
    A1[气候 19 项 bioclim<br/>WorldClim/CHELSA 1km] --> SDM
    A2[地形 DEM 30m<br/>海拔/坡向/坡度/TWI] --> SDM
    A3[宿主林型 GLC_FCS30<br/>针叶/阔叶/混交 + 林龄 CLCD] --> SDM
    A4[土壤 SoilGrids 250m<br/>pH/有机碳/质地] --> SDM
    SDM[MaxEnt / 随机森林<br/>按物种训练] --> RA[每物种 30m 概率栅格]
  end
  subgraph B[季节窗 B]
    B1[出现记录的日期分布<br/>按物种×气候区] --> RB[每物种月份曲线]
  end
  subgraph C[动态出菇指数 C]
    C1[ERA5-Land / Open-Meteo<br/>降水滞后 3–10 天<br/>土壤温湿度 / 积温 / 无霜] --> RC[逐日格点指数]
  end
  RA --> P[P = A × B × C]
  RB --> P
  RC --> P
  OBS[(出现记录库<br/>GBIF/iNat/文献/App 采点)] --> SDM
  OBS --> B1
  P --> Q[查询 API / 分物种分月瓦片]
''';

    late ChatMermaidFlowchartLayout layout;
    late Rect boxA;
    late Rect boxC;

    setUp(() {
      layout = runLayout(parse(source));
      boxA = layout.subgraphRects
          .firstWhere((b) => b.subgraph.id == 'A')
          .rect;
      boxC = layout.subgraphRects
          .firstWhere((b) => b.subgraph.id == 'C')
          .rect;
    });

    test('导出路由预览 SVG 到 /tmp（肉眼验收）', () {
      final svg = _layoutPreviewSvg(layout);
      const path = '/tmp/mermaid-flowchart-z-bend-fix.svg';
      File(path).writeAsStringSync(svg);
      expect(File(path).lengthSync(), greaterThan(500));
    });

    test('A1/A2/A3/A4→SDM 与 C1→RC 为短 Z 弯且留在框内', () {
      for (final sourceId in <String>['A1', 'A2', 'A3', 'A4']) {
        expectAdjacentLayerZBend(
          layout: layout,
          sourceId: sourceId,
          targetId: 'SDM',
          subgraphBounds: boxA,
        );
      }
      expectAdjacentLayerZBend(
        layout: layout,
        sourceId: 'C1',
        targetId: 'RC',
        subgraphBounds: boxC,
      );
    });
  });

  test('TD：同层宽窄节点各连下一层同一节点，均为 ≤4 点 Z 弯', () {
    const source = r'''
flowchart TD
  subgraph G[层]
    N[窄] --> T[目标]
    W[很宽很宽很宽很宽很宽很宽很宽] --> T
  end
''';
    final layout = runLayout(parse(source));
    for (final sourceId in <String>['N', 'W']) {
      final routed = findEdge(layout, sourceId, 'T');
      expect(routed, isNotNull);
      expect(
        routed!.points.length,
        lessThanOrEqualTo(4),
        reason: '$sourceId→T: ${routed.points}',
      );
    }
  });
}

String _layoutPreviewSvg(ChatMermaidFlowchartLayout layout) {
  final w = layout.canvasSize.width + 20;
  final h = layout.canvasSize.height + 20;
  final buffer = StringBuffer();
  buffer.writeln(
    '<svg xmlns="http://www.w3.org/2000/svg" width="$w" height="$h" '
    'viewBox="0 0 $w $h">',
  );
  buffer.writeln('<rect width="100%" height="100%" fill="white"/>');
  for (final box in layout.subgraphRects) {
    final r = box.rect.shift(const Offset(10, 10));
    buffer.writeln(
      '<rect x="${r.left}" y="${r.top}" width="${r.width}" height="${r.height}" '
      'fill="none" stroke="#9aa0a6" stroke-width="1.5"/>',
    );
  }
  for (final rect in layout.nodeRects.values) {
    final r = rect.shift(const Offset(10, 10));
    buffer.writeln(
      '<rect x="${r.left}" y="${r.top}" width="${r.width}" height="${r.height}" '
      'fill="#f8f9fa" stroke="#333"/>',
    );
  }
  for (final routed in layout.edges) {
    final points = routed.points
        .map((p) => p + const Offset(10, 10))
        .toList(growable: false);
    for (var i = 0; i + 1 < points.length; i++) {
      final a = points[i];
      final b = points[i + 1];
      buffer.writeln(
        '<line x1="${a.dx}" y1="${a.dy}" x2="${b.dx}" y2="${b.dy}" '
        'stroke="#1a73e8" stroke-width="2"/>',
      );
    }
  }
  buffer.writeln('</svg>');
  return buffer.toString();
}

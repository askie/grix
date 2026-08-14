import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_flowchart_layout.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';

void main() {
  const layoutEngine = ChatMermaidFlowchartLayoutEngine();
  const textStyle = TextStyle(fontSize: 12);

  const shapes = ChatMermaidNodeShape.values;
  const directions = ChatMermaidFlowDirection.values;
  const labels = <String>[
    '开始',
    '结束',
    '判断是否满足条件',
    '执行一段比较长的处理逻辑用于撑大节点宽度',
    '校验输入参数是否合法并返回结果',
    'A',
    '重试',
    '汇总输出最终结果数据',
  ];

  test('randomized DAG flowcharts (with subgraphs + free nodes) never overlap nodes', () {
    // 生成空间此前只覆盖了「平铺（depth=0）分组」，审查用更狠的 fuzz 证明这远
    // 不够：父提交在嵌套分组场景下的中招率（400 张 63 张）比平铺场景（500 张
    // 29 处）严重得多，而嵌套 + 边直接指向分组 id 正是这一单和上一单改动的核心
    // 路径（expandEndpoint 展开、双 hub 虚拟节点）。补上这两类，并把「分组框
    // 不重叠」这条关键不变量也断言进来——它此前完全没有回归网，靠审查跑对照
    // 实验才发现「只留最后一轮去重」会让分组框重叠悄悄回归。
    final rng = math.Random(20260606);
    var nodeOverlapCases = 0;
    var boxOverlapCases = 0;

    for (var caseIndex = 0; caseIndex < 500; caseIndex++) {
      final nodeCount = 5 + rng.nextInt(8); // 5..12
      final nodes = <ChatMermaidNode>[
        for (var i = 0; i < nodeCount; i++)
          ChatMermaidNode(
            id: 'n$i',
            label: labels[rng.nextInt(labels.length)],
            shape: shapes[rng.nextInt(shapes.length)],
            order: i,
          ),
      ];

      // 生成 DAG 边：每个节点连向若干后继。
      final edges = <ChatMermaidEdge>[];
      var order = 0;
      for (var i = 0; i < nodeCount; i++) {
        final fanOut = 1 + rng.nextInt(3);
        for (var k = 0; k < fanOut; k++) {
          final remaining = nodeCount - i - 1;
          if (remaining <= 0) {
            break;
          }
          final target = i + 1 + rng.nextInt(remaining);
          edges.add(
            ChatMermaidEdge(
              sourceId: 'n$i',
              targetId: 'n$target',
              style: ChatMermaidEdgeStyle.solidArrow,
              order: order++,
            ),
          );
        }
      }

      // 随机把一部分节点划进 0~2 个分组，其余节点留作「自由节点」（不属于任何
      // 分组）。此前这个属性测试从不生成 subgraph，天生测不到「分组消解把成员
      // 推走时撞上自由节点」这一类缺陷——200 张随机图里 19 张实际中招，这个
      // 测试却全程绿灯。补上分组 + 自由节点混排，让生成空间覆盖到这个交互面。
      final subgraphs = <ChatMermaidFlowSubgraph>[];
      final groupCount = rng.nextInt(3); // 0..2 个分组
      final available = List<int>.generate(nodeCount, (i) => i)..shuffle(rng);
      var cursor = 0;
      for (var g = 0; g < groupCount; g++) {
        final remaining = available.length - cursor;
        if (remaining < 2) {
          break; // 剩余节点不够组成一个分组（至少 2 个成员），留作自由节点。
        }
        final memberCount = 2 + rng.nextInt(math.min(3, remaining - 1));
        final memberIds = [
          for (var m = 0; m < memberCount; m++) 'n${available[cursor + m]}',
        ];
        cursor += memberCount;
        subgraphs.add(
          ChatMermaidFlowSubgraph(
            id: 'g$g',
            label: '分组$g',
            order: g,
            depth: 0,
            nodeIds: memberIds,
          ),
        );

        // 约 40% 概率给这个分组再嵌套一层子分组：从父分组的成员里挑一个真子
        // 集。子分组的 nodeIds 是父分组 nodeIds 的严格子集——这与 parser 的行为
        // 一致（parser 把节点塞进所有活动分组，嵌套子分组的成员必然是祖先成员
        // 的真子集），_computeSubgraphParents 正是靠这个包含关系推断父子。
        if (memberIds.length >= 2 && rng.nextDouble() < 0.4) {
          final childCount = 1 + rng.nextInt(memberIds.length - 1);
          subgraphs.add(
            ChatMermaidFlowSubgraph(
              id: 'g${g}_inner',
              label: '分组$g的子组',
              order: subgraphs.length,
              depth: 1,
              nodeIds: memberIds.take(childCount).toList(),
            ),
          );
        }
      }

      // 约 30% 概率追加一条「边直接连在分组框上」（`nX --> gY` 或反向），走的是
      // expandEndpoint 展开 + 双 hub 虚拟节点这条路径——这是上一单改动的核心，
      // 此前的生成器完全没触碰过。
      if (subgraphs.isNotEmpty && rng.nextDouble() < 0.3) {
        final targetGroup = subgraphs[rng.nextInt(subgraphs.length)];
        final freeCandidates = List<int>.generate(nodeCount, (i) => i)
            .where((i) => !subgraphs.any((s) => s.nodeIds.contains('n$i')))
            .toList();
        if (freeCandidates.isNotEmpty) {
          final sourceId = 'n${freeCandidates[rng.nextInt(freeCandidates.length)]}';
          edges.add(
            ChatMermaidEdge(
              sourceId: sourceId,
              targetId: targetGroup.id,
              style: ChatMermaidEdgeStyle.solidArrow,
              order: order++,
            ),
          );
        }
      }

      final diagram = ChatMermaidFlowchart(
        direction: directions[rng.nextInt(directions.length)],
        nodes: nodes,
        edges: edges,
        subgraphs: subgraphs,
      );

      final layout = layoutEngine.layout(
        diagram: diagram,
        textStyle: textStyle,
        labelStyle: textStyle,
        textDirection: TextDirection.ltr,
      );

      final rects = layout.nodeRects.entries.toList();
      for (var i = 0; i < rects.length; i++) {
        for (var j = i + 1; j < rects.length; j++) {
          // 用极小内缩避免把"恰好相切"误判为重叠。
          final a = rects[i].value.deflate(0.01);
          final b = rects[j].value.deflate(0.01);
          if (a.overlaps(b)) {
            nodeOverlapCases++;
            debugPrint(
              'node overlap case=$caseIndex dir=${diagram.direction} '
              '${rects[i].key}=${rects[i].value} '
              '${rects[j].key}=${rects[j].value}',
            );
          }
        }
      }

      // 分组框两两不重叠，父子包含关系除外（父框本就该包住子框）。这条不变量
      // 此前完全没有回归网——上一单（88d54105）专门修的就是这个，靠审查跑对照
      // 实验才发现「只补最后一轮去重」会让它悄悄回归。
      final boxes = layout.subgraphRects;
      for (var i = 0; i < boxes.length; i++) {
        for (var j = i + 1; j < boxes.length; j++) {
          final a = boxes[i];
          final b = boxes[j];
          final aIsParentOfB =
              b.subgraph.nodeIds.every(a.subgraph.nodeIds.contains);
          final bIsParentOfA =
              a.subgraph.nodeIds.every(b.subgraph.nodeIds.contains);
          if (aIsParentOfB || bIsParentOfA) {
            continue;
          }
          if (a.rect.deflate(0.01).overlaps(b.rect.deflate(0.01))) {
            boxOverlapCases++;
            debugPrint(
              'box overlap case=$caseIndex dir=${diagram.direction} '
              '${a.subgraph.id}=${a.rect} ${b.subgraph.id}=${b.rect}',
            );
          }
        }
      }
    }

    expect(nodeOverlapCases, 0, reason: '共发现 $nodeOverlapCases 处节点叠加');
    expect(boxOverlapCases, 0, reason: '共发现 $boxOverlapCases 处非父子分组框叠加');
  });
}

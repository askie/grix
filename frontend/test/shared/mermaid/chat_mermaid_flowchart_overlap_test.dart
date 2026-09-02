import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_flowchart_layout.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';

void main() {
  const layoutEngine = ChatMermaidFlowchartLayoutEngine();
  const textStyle = TextStyle(fontSize: 12);

  // 通用断言：任意两个节点矩形不允许重叠。
  void expectNoNodeOverlap(ChatMermaidFlowchartLayout layout) {
    final entries = layout.nodeRects.entries.toList();
    for (var i = 0; i < entries.length; i++) {
      for (var j = i + 1; j < entries.length; j++) {
        final a = entries[i];
        final b = entries[j];
        expect(
          a.value.overlaps(b.value),
          isFalse,
          reason: '节点 ${a.key} 与 ${b.key} 发生叠加: ${a.value} / ${b.value}',
        );
      }
    }
  }

  ChatMermaidFlowchartLayout runLayout(ChatMermaidFlowchart diagram) {
    return layoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );
  }

  test('top-down diamond branch then merge keeps nodes separated', () {
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
          label: '是否满足条件并且需要进一步审核',
          shape: ChatMermaidNodeShape.diamond,
          order: 1,
        ),
        ChatMermaidNode(
          id: 'C',
          label: '执行通过分支的较长处理逻辑',
          shape: ChatMermaidNodeShape.rectangle,
          order: 2,
        ),
        ChatMermaidNode(
          id: 'D',
          label: '执行拒绝分支的较长处理逻辑',
          shape: ChatMermaidNodeShape.rectangle,
          order: 3,
        ),
        ChatMermaidNode(
          id: 'E',
          label: '结束',
          shape: ChatMermaidNodeShape.rounded,
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
          sourceId: 'B',
          targetId: 'C',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 1,
          label: '是',
        ),
        ChatMermaidEdge(
          sourceId: 'B',
          targetId: 'D',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 2,
          label: '否',
        ),
        ChatMermaidEdge(
          sourceId: 'C',
          targetId: 'E',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 3,
        ),
        ChatMermaidEdge(
          sourceId: 'D',
          targetId: 'E',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 4,
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    expectNoNodeOverlap(runLayout(diagram));
  });

  test('top-down fan-out with three wide siblings keeps nodes separated', () {
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
          label: '第一条分支处理逻辑非常长用来接近最大节点宽度',
          shape: ChatMermaidNodeShape.rectangle,
          order: 1,
        ),
        ChatMermaidNode(
          id: 'C',
          label: '第二条分支处理逻辑同样很长用来接近最大节点宽度',
          shape: ChatMermaidNodeShape.rectangle,
          order: 2,
        ),
        ChatMermaidNode(
          id: 'D',
          label: '第三条分支处理逻辑也很长用来接近最大节点宽度',
          shape: ChatMermaidNodeShape.rectangle,
          order: 3,
        ),
        ChatMermaidNode(
          id: 'E',
          label: '汇总结果输出',
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
          sourceId: 'A',
          targetId: 'D',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 2,
        ),
        ChatMermaidEdge(
          sourceId: 'B',
          targetId: 'E',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 3,
        ),
        ChatMermaidEdge(
          sourceId: 'C',
          targetId: 'E',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 4,
        ),
        ChatMermaidEdge(
          sourceId: 'D',
          targetId: 'E',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 5,
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    expectNoNodeOverlap(runLayout(diagram));
  });

  test('left-right diamond branch then merge keeps nodes separated', () {
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
          label: '是否满足条件并且需要进一步审核',
          shape: ChatMermaidNodeShape.diamond,
          order: 1,
        ),
        ChatMermaidNode(
          id: 'C',
          label: '执行通过分支说明很长很长需要多行展示完整内容',
          shape: ChatMermaidNodeShape.rectangle,
          order: 2,
        ),
        ChatMermaidNode(
          id: 'D',
          label: '执行拒绝分支说明同样很长很长需要多行展示完整内容',
          shape: ChatMermaidNodeShape.rectangle,
          order: 3,
        ),
        ChatMermaidNode(
          id: 'E',
          label: '结束',
          shape: ChatMermaidNodeShape.rounded,
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
          sourceId: 'B',
          targetId: 'C',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 1,
          label: '是',
        ),
        ChatMermaidEdge(
          sourceId: 'B',
          targetId: 'D',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 2,
          label: '否',
        ),
        ChatMermaidEdge(
          sourceId: 'C',
          targetId: 'E',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 3,
        ),
        ChatMermaidEdge(
          sourceId: 'D',
          targetId: 'E',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 4,
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    expectNoNodeOverlap(runLayout(diagram));
  });

  test('top-down graph with back edge keeps nodes separated', () {
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
          label: '处理请求并校验输入参数是否合法',
          shape: ChatMermaidNodeShape.rectangle,
          order: 1,
        ),
        ChatMermaidNode(
          id: 'C',
          label: '是否需要重试',
          shape: ChatMermaidNodeShape.diamond,
          order: 2,
        ),
        ChatMermaidNode(
          id: 'D',
          label: '完成',
          shape: ChatMermaidNodeShape.rounded,
          order: 3,
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
          sourceId: 'B',
          targetId: 'C',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 1,
        ),
        ChatMermaidEdge(
          sourceId: 'C',
          targetId: 'B',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 2,
          label: '重试',
        ),
        ChatMermaidEdge(
          sourceId: 'C',
          targetId: 'D',
          style: ChatMermaidEdgeStyle.solidArrow,
          order: 3,
          label: '完成',
        ),
      ],
      subgraphs: <ChatMermaidFlowSubgraph>[],
    );

    expectNoNodeOverlap(runLayout(diagram));
  });
}

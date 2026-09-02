import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_flowchart_view.dart';

void main() {
  // 该图节点高度由布局引擎用「未缩放」的 TextPainter 测量。若渲染端的 Text
  // 跟随系统字体缩放（textScaler>1），放大的文字会多挤出一行、超出固定高度的
  // 节点框，末行被框底切掉（线上反馈的"节点文字被遮挡"）。
  // 修复：渲染端节点与边标签强制 TextScaler.noScaling，与测量端口径一致。
  const multilineNodeLabel =
      'build_realtime_model\n'
      'openai RealtimeModel gpt-realtime\n'
      'turn_detection=server_vad\n'
      'create_response=False';

  const diagram = ChatMermaidFlowchart(
    direction: ChatMermaidFlowDirection.topDown,
    nodes: <ChatMermaidNode>[
      ChatMermaidNode(
        id: 'A',
        label: multilineNodeLabel,
        shape: ChatMermaidNodeShape.rectangle,
        order: 0,
      ),
      ChatMermaidNode(
        id: 'B',
        label: 'AgentSession 启动',
        shape: ChatMermaidNodeShape.rounded,
        order: 1,
      ),
    ],
    edges: <ChatMermaidEdge>[
      ChatMermaidEdge(
        sourceId: 'A',
        targetId: 'B',
        style: ChatMermaidEdgeStyle.solidArrow,
        order: 0,
        label: '800ms内大脑没来',
      ),
    ],
    subgraphs: <ChatMermaidFlowSubgraph>[],
  );

  testWidgets('系统字体放大时流程图节点文字仍关闭缩放，不会溢出节点框被遮挡', (WidgetTester tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: MediaQuery(
          // 模拟老郭设备：系统字体放大到 1.6 倍。
          data: MediaQueryData(textScaler: TextScaler.linear(1.6)),
          child: Scaffold(
            body: Center(
              child: SizedBox(
                width: 800,
                height: 600,
                child: ChatMarkdownMermaidFlowchartView(
                  diagram: diagram,
                  textStyle: TextStyle(fontSize: 14, color: Color(0xFF2A2214)),
                  backgroundColor: Color(0xFFFFFFFF),
                ),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    // 收集所有渲染段落，必须存在且全部关闭系统缩放。
    final paragraphs = tester.renderObjectList<RenderParagraph>(
      find.byType(RichText),
    );
    expect(paragraphs, isNotEmpty);
    for (final paragraph in paragraphs) {
      expect(
        paragraph.textScaler,
        TextScaler.noScaling,
        reason: '流程图文字必须关闭系统字体缩放，否则放大后会溢出固定高度的节点框被遮挡',
      );
    }
  });
}

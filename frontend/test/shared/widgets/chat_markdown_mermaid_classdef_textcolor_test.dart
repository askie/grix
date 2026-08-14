import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/mermaid/chat_mermaid_parser.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_flowchart_view.dart';

void main() {
  const source = '''
flowchart TD
    classDef redNode fill:#e53e3e,color:#ffffff,stroke:#c53030

    A[开始]:::redNode --> B[处理数据]:::redNode
    B --> C{判断条件}:::redNode
    C -->|是| D[执行操作 A]:::redNode
    C -->|否| E[执行操作 B]:::redNode
    D --> F[结束]:::redNode
    E --> F:::redNode
''';

  test('修复 #1: parser 把 classDef color:#ffffff 写入 ChatMermaidNode.textColor', () {
    final result = const ChatMermaidParser().parse(source);
    expect(result.isSupported, true);
    final diagram = result.diagram as ChatMermaidFlowchart;
    expect(diagram.nodes, hasLength(6));
    for (final node in diagram.nodes) {
      expect(node.fillColor, isNotNull);
      expect(Color(node.fillColor!).value, const Color(0xFFE53E3E).value);
      expect(node.strokeColor, isNotNull);
      expect(Color(node.strokeColor!).value, const Color(0xFFC53030).value);
      expect(node.textColor, isNotNull,
          reason: '节点 ${node.id} 必须有 textColor（修复前一直为 null）');
      expect(Color(node.textColor!).value, const Color(0xFFFFFFFF).value,
          reason: '节点 ${node.id} textColor 应为白色');
    }
  });

  testWidgets('修复 #2: 渲染时节点 RichText 的文字色被覆盖为白色', (tester) async {
    final result = const ChatMermaidParser().parse(source);
    final diagram = result.diagram as ChatMermaidFlowchart;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          backgroundColor: const Color(0xFFFFFFFF),
          body: SizedBox(
            width: 900,
            height: 700,
            child: ChatMarkdownMermaidFlowchartView(
              diagram: diagram,
              // 全局默认文字色用深色，便于核对节点白色文字是被 classDef 覆盖。
              textStyle: const TextStyle(
                fontSize: 14,
                color: Color(0xFF2A2214),
              ),
              backgroundColor: const Color(0xFFFFFFFF),
            ),
          ),
        ),
      ),
    );
    await tester.pump(const Duration(milliseconds: 100));

    const expectedLabels = <String>{
      '开始',
      '处理数据',
      '判断条件',
      '执行操作 A',
      '执行操作 B',
      '结束',
    };
    final paragraphs =
        tester.renderObjectList<RenderParagraph>(find.byType(RichText)).toList();
    expect(paragraphs, isNotEmpty);
    final seen = <String>{};
    for (final paragraph in paragraphs) {
      final span = paragraph.text;
      if (span is! TextSpan) continue;
      final text = span.toPlainText();
      if (!expectedLabels.contains(text)) continue;
      seen.add(text);
      final color = span.style?.color;
      expect(
        color?.value,
        const Color(0xFFFFFFFF).value,
        reason: '节点 "$text" 文字色应被 classDef color:#ffffff 覆盖为白色，'
            '实际 $color。修复前会保持全局默认 #2A2214。',
      );
    }
    expect(
      seen,
      expectedLabels,
      reason: '所有 6 个节点的文字都应作为 RichText 出现并被验证，实际命中：$seen',
    );
  });
}

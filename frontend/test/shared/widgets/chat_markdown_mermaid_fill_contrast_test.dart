import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/mermaid/chat_mermaid_parser.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_flowchart_view.dart';

void main() {
  const source = '''
flowchart TD
    A[开始] --> B[结束]
    style A fill:#ffffff
    style B fill:#1f2937
''';

  test('只指定 fill 时按填充色明暗推导文字色', () {
    expect(
      ChatMarkdownMermaidFlowchartView.resolveNodeTextColor(
        fillColor: 0xFFFFFFFF,
        textColor: null,
        fallback: Colors.white,
      ),
      const Color(0xFF111827),
    );
    expect(
      ChatMarkdownMermaidFlowchartView.resolveNodeTextColor(
        fillColor: 0xFF1F2937,
        textColor: null,
        fallback: Colors.black,
      ),
      Colors.white,
    );
    expect(
      ChatMarkdownMermaidFlowchartView.resolveNodeTextColor(
        fillColor: 0xFFFFFFFF,
        textColor: 0xFFFF0000,
        fallback: Colors.white,
      ),
      const Color(0xFFFF0000),
    );
    expect(
      ChatMarkdownMermaidFlowchartView.resolveNodeTextColor(
        fillColor: null,
        textColor: null,
        fallback: Colors.white,
      ),
      Colors.white,
    );
  });

  testWidgets('深色模式下 fill:#ffffff 节点文字不再是白色', (tester) async {
    final result = const ChatMermaidParser().parse(source);
    final diagram = result.diagram as ChatMermaidFlowchart;
    await tester.pumpWidget(
      MaterialApp(
        theme: ThemeData.dark(),
        home: Scaffold(
          body: SizedBox(
            width: 600,
            height: 400,
            child: ChatMarkdownMermaidFlowchartView(
              diagram: diagram,
              textStyle: const TextStyle(fontSize: 14, color: Colors.white),
              backgroundColor: const Color(0xFF181208),
            ),
          ),
        ),
      ),
    );
    await tester.pump(const Duration(milliseconds: 100));

    final start = tester.widget<Text>(find.text('开始'));
    expect(start.style?.color, const Color(0xFF111827));
    final end = tester.widget<Text>(find.text('结束'));
    expect(end.style?.color, Colors.white);
  });
}

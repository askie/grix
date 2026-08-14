import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_ast.dart';
import 'package:grix/shared/markdown/chat_markdown_semantics.dart';

void main() {
  const analyzer = ChatMarkdownSemanticAnalyzer();

  test('plain paragraph document does not require rich rendering', () {
    const document = ChatMarkdownDocument(
      children: [
        ChatMarkdownNode(
          type: ChatMarkdownNodeType.paragraph,
          children: [
            ChatMarkdownNode(
              type: ChatMarkdownNodeType.text,
              attrs: {'text': 'plain text'},
            ),
          ],
        ),
      ],
    );

    final summary = analyzer.analyze(document);

    expect(summary.requiresRichRendering, isFalse);
    expect(summary.features, isEmpty);
  });

  test('collects block and inline markdown features from canonical ast', () {
    const document = ChatMarkdownDocument(
      children: [
        ChatMarkdownNode(type: ChatMarkdownNodeType.heading),
        ChatMarkdownNode(type: ChatMarkdownNodeType.table),
        ChatMarkdownNode(type: ChatMarkdownNodeType.mathBlock),
        ChatMarkdownNode(type: ChatMarkdownNodeType.mermaidBlock),
        ChatMarkdownNode(
          type: ChatMarkdownNodeType.paragraph,
          children: [
            ChatMarkdownNode(type: ChatMarkdownNodeType.link),
            ChatMarkdownNode(type: ChatMarkdownNodeType.strong),
          ],
        ),
      ],
    );

    final summary = analyzer.analyze(document);

    expect(summary.requiresRichRendering, isTrue);
    expect(summary.hasFeature(ChatMarkdownFeature.heading), isTrue);
    expect(summary.hasTables, isTrue);
    expect(summary.hasMath, isTrue);
    expect(summary.hasMermaidBlocks, isTrue);
    expect(summary.hasLinks, isTrue);
    expect(summary.hasInlineFormatting, isTrue);
  });
}

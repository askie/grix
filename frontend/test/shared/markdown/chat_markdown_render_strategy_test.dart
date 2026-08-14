import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';
import 'package:grix/shared/markdown/chat_markdown_pipeline.dart';
import 'package:grix/shared/widgets/chat_markdown_render_strategy.dart';

void main() {
  final pipeline = ChatMarkdownPipeline(
    normalizer: const ChatMarkdownNormalizer(),
    parser: ChatMarkdownDialect.buildParserAdapter(),
  );
  const strategy = ChatMarkdownRenderStrategy();

  test('selects native ast for mermaid blocks', () {
    final result = pipeline.prepareFinalRender(
      '```mermaid\ngraph TD\nA --> B\n```',
    );

    expect(
      strategy.select(document: result.document, semantics: result.semantics),
      ChatMarkdownRenderMode.nativeAst,
    );
  });

  test('selects native ast for task list documents', () {
    final result = pipeline.prepareFinalRender('- [x] done\n- [ ] todo');

    expect(
      strategy.select(document: result.document, semantics: result.semantics),
      ChatMarkdownRenderMode.nativeAst,
    );
  });

  test(
    'selects native ast for structural markdown without fallback-only nodes',
    () {
      final result = pipeline.prepareFinalRender(
        '# Heading\n\n- item 1\n- item 2\n\n**bold** text',
      );

      expect(
        strategy.select(document: result.document, semantics: result.semantics),
        ChatMarkdownRenderMode.nativeAst,
      );
    },
  );

  test('selects native ast for task lists containing external links', () {
    final result = pipeline.prepareFinalRender(
      '- [x] [OpenAI](https://openai.com)',
    );

    expect(
      strategy.select(document: result.document, semantics: result.semantics),
      ChatMarkdownRenderMode.nativeAst,
    );
  });

  test('selects native ast for image-only documents', () {
    final result = pipeline.prepareFinalRender(
      '![diagram](https://example.com/a.png)',
    );

    expect(
      strategy.select(document: result.document, semantics: result.semantics),
      ChatMarkdownRenderMode.nativeAst,
    );
  });

  test(
    'selects native ast for fenced code blocks with syntax highlighting',
    () {
      final result = pipeline.prepareFinalRender('```json\n{"a":1}\n```');

      expect(
        strategy.select(document: result.document, semantics: result.semantics),
        ChatMarkdownRenderMode.nativeAst,
      );
    },
  );

  test('selects native ast for table documents', () {
    final result = pipeline.prepareFinalRender(
      '| a | b |\n|---|---|\n| 1 | 2 |',
    );

    expect(
      strategy.select(document: result.document, semantics: result.semantics),
      ChatMarkdownRenderMode.nativeAst,
    );
  });

  test('selects native ast for footnote documents', () {
    final result = pipeline.prepareFinalRender('[^1] note\n\n[^1]: footnote');

    expect(
      strategy.select(document: result.document, semantics: result.semantics),
      ChatMarkdownRenderMode.nativeAst,
    );
  });

  test('falls back for html documents', () {
    final result = pipeline.prepareFinalRender('<div>unsafe html</div>');

    expect(
      strategy.select(document: result.document, semantics: result.semantics),
      ChatMarkdownRenderMode.fallbackPlainText,
    );
  });

  test('document strategy renders markdown around html safely', () {
    final result = pipeline.prepareFinalRender(
      '# Heading\n\n<div>embedded html</div>',
    );
    const documentStrategy = ChatMarkdownRenderStrategy(
      renderHtmlAsPlainText: false,
    );

    expect(
      documentStrategy.select(
        document: result.document,
        semantics: result.semantics,
      ),
      ChatMarkdownRenderMode.nativeAst,
    );
  });
}

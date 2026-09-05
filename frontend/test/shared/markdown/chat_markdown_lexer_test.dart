import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_lexer.dart';
import 'package:grix/shared/markdown/chat_markdown_segment.dart';

void main() {
  const lexer = ChatMarkdownLexer();

  test('lexes fenced code blocks with language and content', () {
    const input = 'alpha\n```json\n{"key": 1}\n```\nomega';

    final segments = lexer.lex(input);

    expect(segments.length, 3);
    expect(segments[0].type, ChatMarkdownSegmentType.text);
    expect(segments[1].type, ChatMarkdownSegmentType.fencedCode);
    expect(segments[1].language, 'json');
    expect(segments[1].content, contains('"key": 1'));
    expect(segments[1].closed, isTrue);
    expect(segments[2].type, ChatMarkdownSegmentType.text);
  });

  test('marks unclosed fenced code blocks as not closed', () {
    const input = '```markdown\n| a | b |\n|---|---|';

    final segments = lexer.lex(input);

    expect(segments, hasLength(1));
    expect(segments.single.type, ChatMarkdownSegmentType.fencedCode);
    expect(segments.single.closed, isFalse);
    expect(segments.single.fenceMarker, '```');
  });

  test('lexes inline code, links and images as protected segments', () {
    const input =
        'Use `code` and [site](https://example.com/path(a)) then ![img](https://img.example.com/a.png).';

    final segments = lexer.lex(input);

    expect(
      segments.where(
        (segment) => segment.type == ChatMarkdownSegmentType.inlineCode,
      ),
      hasLength(1),
    );
    expect(
      segments.where(
        (segment) => segment.type == ChatMarkdownSegmentType.linkDestination,
      ),
      hasLength(1),
    );
    expect(
      segments.where(
        (segment) => segment.type == ChatMarkdownSegmentType.imageDestination,
      ),
      hasLength(1),
    );
    expect(
      segments
          .where(
            (segment) =>
                segment.type == ChatMarkdownSegmentType.linkDestination,
          )
          .single
          .destination,
      'https://example.com/path(a)',
    );
  });

  test('lexes escaped characters as separate protected segments', () {
    const input = r'escaped \* star';

    final segments = lexer.lex(input);

    expect(
      segments.where(
        (segment) => segment.type == ChatMarkdownSegmentType.escaped,
      ),
      hasLength(1),
    );
  });

  test(
    'keeps angle-bracket placeholders and autolinks out of html segments',
    () {
      const placeholders = <String>[
        '<hermes 名>',
        '<name>',
        '<文件路径>',
        '<https://example.com>',
        '<user@example.com>',
        '<path>',
        '<agent id>',
      ];

      for (final placeholder in placeholders) {
        final segments = lexer.lex('before $placeholder after');

        expect(
          segments.where(
            (segment) => segment.type == ChatMarkdownSegmentType.htmlLike,
          ),
          isEmpty,
          reason: '$placeholder must not lex as html',
        );
      }
    },
  );

  test('still lexes real html tags and declarations as html segments', () {
    const htmlSamples = <String>[
      '<div>',
      '</div>',
      '<br/>',
      '<img src="x" alt=\'y\'>',
      '<!-- c -->',
      '<!DOCTYPE html>',
      '<?php echo 1; ?>',
      '<![CDATA[raw]]>',
      '<DIV>',
      '<video src="a.mp4">',
    ];

    for (final sample in htmlSamples) {
      final segments = lexer.lex('before $sample after');
      final htmlSegments = segments.where(
        (segment) => segment.type == ChatMarkdownSegmentType.htmlLike,
      );

      expect(htmlSegments, hasLength(1), reason: '$sample must lex as html');
      expect(htmlSegments.single.text, sample);
    }
  });

  test('unterminated html-like shapes stay plain text', () {
    for (final input in <String>['<div', '<!-- open', '<?php']) {
      final segments = lexer.lex(input);

      expect(
        segments.where(
          (segment) => segment.type == ChatMarkdownSegmentType.htmlLike,
        ),
        isEmpty,
        reason: '$input must not lex as html',
      );
    }
  });
}

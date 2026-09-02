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
}

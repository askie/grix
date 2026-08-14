import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_validator.dart';

void main() {
  const validator = ChatMarkdownValidator();

  group('snapshot', () {
    test('counts fenced code blocks', () {
      const text = '```python\nprint("hi")\n```\n\n```js\nconsole.log("hi")\n```';

      final snap = validator.snapshot(text);

      expect(snap.fencedCodeBlockCount, equals(2));
      expect(snap.unclosedFenceCount, equals(0));
    });

    test('detects unclosed fences', () {
      const text = '```python\nprint("hi")\n';

      final snap = validator.snapshot(text);

      expect(snap.fencedCodeBlockCount, equals(1));
      expect(snap.unclosedFenceCount, equals(1));
    });

    test('counts strong markers', () {
      const text = '**bold** and **more bold**';

      final snap = validator.snapshot(text);

      expect(snap.strongMarkerCount, equals(4));
    });

    test('counts strike markers', () {
      const text = '~~deleted~~ text';

      final snap = validator.snapshot(text);

      expect(snap.strikeMarkerCount, equals(2));
    });

    test('counts math fences', () {
      const text = r'Some text' '\n' r'$$' '\n' r'x = 1' '\n' r'$$';

      final snap = validator.snapshot(text);

      expect(snap.mathFenceCount, equals(2));
    });

    test('counts heading lines', () {
      const text = '# Title\n## Subtitle\nNot a heading';

      final snap = validator.snapshot(text);

      expect(snap.headingLineCount, equals(2));
    });
  });

  group('validate', () {
    test('no issues for unchanged text', () {
      const text = '**bold** text';

      final result = validator.validate(
        originalText: text,
        normalizedText: text,
        document: null,
      );

      expect(result.hasErrors, isFalse);
      expect(result.hasWarnings, isFalse);
      expect(result.issues, isEmpty);
    });

    test('warns on heading loss', () {
      const original = '# Title\n## Subtitle';
      const normalized = 'Title\nSubtitle';

      final result = validator.validate(
        originalText: original,
        normalizedText: normalized,
        document: null,
      );

      expect(result.hasWarnings, isTrue);
      expect(result.issues.any((i) => i.code == 'heading_loss'), isTrue);
    });

    test('errors on unexpected code block gain', () {
      const original = 'plain text';
      const normalized = 'plain text\n```\ncode\n```\n```\nmore\n```';

      final result = validator.validate(
        originalText: original,
        normalizedText: normalized,
        document: null,
      );

      expect(result.hasErrors, isTrue);
      expect(result.issues.any((i) => i.code == 'code_block_gain'), isTrue);
    });

    test('allows minor marker rebalancing', () {
      const original = '**bold** text **more';
      const normalized = '**bold** text **more**';

      final result = validator.validate(
        originalText: original,
        normalizedText: normalized,
        document: null,
      );

      // Only 1 marker difference (rebalancing) should not warn
      expect(
        result.issues.where((i) => i.code == 'strong_marker_divergence'),
        isEmpty,
      );
    });
  });
}
